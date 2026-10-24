package voice

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// LocalSTTProvider transcribes through a local OpenAI-compatible endpoint —
// whisper.cpp's server, faster-whisper-server, LocalAI, or anything else
// exposing POST /v1/audio/transcriptions. Audio never leaves the machine.
type LocalSTTProvider struct {
	endpoint string
	apiKey   string
	model    string
	client   *http.Client
}

// NewLocalSTTProvider creates a local endpoint provider. endpoint may be a
// full transcription URL or a bare host; model defaults to "whisper-1".
func NewLocalSTTProvider(endpoint, apiKey, model string, client *http.Client) *LocalSTTProvider {
	if client == nil {
		client = &http.Client{Timeout: 2 * time.Minute}
	}
	if model == "" {
		model = "whisper-1"
	}
	return &LocalSTTProvider{
		endpoint: normalizeLocalSTTEndpoint(endpoint),
		apiKey:   apiKey,
		model:    model,
		client:   client,
	}
}

func normalizeLocalSTTEndpoint(raw string) string {
	s := strings.TrimRight(strings.TrimSpace(raw), "/")
	if s == "" {
		return ""
	}
	switch {
	case strings.HasSuffix(s, "/audio/transcriptions"):
		return s
	case strings.HasSuffix(s, "/v1"):
		return s + "/audio/transcriptions"
	default:
		return s + "/v1/audio/transcriptions"
	}
}

// Transcribe posts the audio as multipart/form-data, matching the OpenAI
// transcription contract.
func (p *LocalSTTProvider) Transcribe(ctx context.Context, audio []byte, format AudioFormat) (*TranscribeResult, error) {
	if p.endpoint == "" {
		return nil, fmt.Errorf("voice: local STT endpoint is empty — set voice.stt_base_url")
	}
	ext := strings.TrimPrefix(string(format), ".")
	if ext == "" {
		ext = "wav"
	}

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", "audio."+ext)
	if err != nil {
		return nil, fmt.Errorf("voice: local STT form: %w", err)
	}
	if _, err := part.Write(audio); err != nil {
		return nil, fmt.Errorf("voice: local STT write audio: %w", err)
	}
	_ = writer.WriteField("model", p.model)
	_ = writer.WriteField("response_format", "json")
	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("voice: local STT form close: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.endpoint, &body)
	if err != nil {
		return nil, fmt.Errorf("voice: local STT request: %w", err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	if p.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+p.apiKey)
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("voice: local STT request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("voice: local STT returned %d: %s", resp.StatusCode, strings.TrimSpace(string(msg)))
	}

	var result struct {
		Text     string  `json:"text"`
		Language string  `json:"language"`
		Duration float64 `json:"duration"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("voice: local STT decode: %w", err)
	}
	return &TranscribeResult{
		Text:     strings.TrimSpace(result.Text),
		Language: result.Language,
		Duration: time.Duration(result.Duration * float64(time.Second)),
	}, nil
}

// CommandSTTProvider shells out to a locally installed transcriber. The
// command receives {audio} (a temp WAV) and {dir} (a temp output directory)
// and must write exactly one .txt transcript into {dir}. No shell is
// involved; argv is executed directly, so paths with spaces are safe.
type CommandSTTProvider struct {
	argv    []string
	env     map[string]string
	timeout time.Duration
}

// NewCommandSTTProvider creates a command-backed transcriber.
func NewCommandSTTProvider(argv []string) *CommandSTTProvider {
	return &CommandSTTProvider{argv: argv, timeout: 2 * time.Minute}
}

// SetEnv adds environment variables for the transcriber process.
func (p *CommandSTTProvider) SetEnv(env map[string]string) {
	if p != nil {
		p.env = env
	}
}

// Configured reports whether a command was supplied.
func (p *CommandSTTProvider) Configured() bool {
	return p != nil && len(p.argv) > 0
}

// Transcribe runs the configured command and reads its .txt transcript.
func (p *CommandSTTProvider) Transcribe(ctx context.Context, audio []byte, format AudioFormat) (*TranscribeResult, error) {
	if p == nil || len(p.argv) == 0 {
		return nil, fmt.Errorf("voice: stt_command is empty — set voice.stt_command to a local transcriber")
	}
	dir, err := os.MkdirTemp("", "aibutler-stt-*")
	if err != nil {
		return nil, fmt.Errorf("voice: STT temp dir: %w", err)
	}
	defer os.RemoveAll(dir)

	ext := strings.TrimPrefix(string(format), ".")
	if ext == "" {
		ext = "wav"
	}
	audioPath := filepath.Join(dir, "input."+ext)
	if err := os.WriteFile(audioPath, audio, 0o600); err != nil {
		return nil, fmt.Errorf("voice: STT write audio: %w", err)
	}

	args := substituteSTTPlaceholders(p.argv, audioPath, dir)
	runCtx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()
	cmd := exec.CommandContext(runCtx, args[0], args[1:]...)
	if len(p.env) > 0 {
		env := os.Environ()
		for k, v := range p.env {
			env = append(env, k+"="+v)
		}
		cmd.Env = env
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("voice: stt command failed: %w (output: %s)", err, truncateForError(string(out)))
	}

	transcript, err := readTranscript(dir)
	if err != nil {
		return nil, err
	}
	text := strings.TrimSpace(string(transcript))
	if text == "" {
		return nil, fmt.Errorf("voice: stt command produced an empty transcript")
	}
	return &TranscribeResult{Text: text}, nil
}

func substituteSTTPlaceholders(argv []string, audioPath, dir string) []string {
	out := make([]string, len(argv))
	for i, a := range argv {
		a = strings.ReplaceAll(a, "{audio}", audioPath)
		a = strings.ReplaceAll(a, "{dir}", dir)
		out[i] = a
	}
	return out
}

// readTranscript finds the single .txt file the command produced.
func readTranscript(dir string) ([]byte, error) {
	matches, err := filepath.Glob(filepath.Join(dir, "*.txt"))
	if err != nil {
		return nil, fmt.Errorf("voice: scan transcript dir: %w", err)
	}
	switch len(matches) {
	case 0:
		return nil, fmt.Errorf("voice: stt command wrote no .txt transcript into %s", dir)
	case 1:
		data, err := os.ReadFile(matches[0])
		if err != nil {
			return nil, fmt.Errorf("voice: read transcript: %w", err)
		}
		return data, nil
	default:
		return nil, fmt.Errorf("voice: stt command wrote %d .txt transcripts into %s; expected one", len(matches), dir)
	}
}

func truncateForError(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 300 {
		return s[:300] + "…"
	}
	return s
}
