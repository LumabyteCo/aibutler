package voice

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"time"
)

// STTProvider transcribes audio to text.
type STTProvider interface {
	Transcribe(ctx context.Context, audio []byte, format AudioFormat) (*TranscribeResult, error)
}

// WhisperProvider uses the OpenAI Whisper API for transcription.
type WhisperProvider struct {
	apiKey string
	client *http.Client
}

// NewWhisperProvider creates a Whisper STT provider.
// If client is nil, a default client with 30s timeout is used.
func NewWhisperProvider(apiKey string, client *http.Client) *WhisperProvider {
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	return &WhisperProvider{
		apiKey: apiKey,
		client: client,
	}
}

// Transcribe sends audio to the OpenAI Whisper API.
func (w *WhisperProvider) Transcribe(ctx context.Context, audio []byte, format AudioFormat) (*TranscribeResult, error) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)

	part, err := writer.CreateFormFile("file", "audio."+string(format))
	if err != nil {
		return nil, fmt.Errorf("whisper: create form: %w", err)
	}
	if _, err := part.Write(audio); err != nil {
		return nil, fmt.Errorf("whisper: write audio: %w", err)
	}

	writer.WriteField("model", "whisper-1")
	writer.WriteField("response_format", "verbose_json")
	writer.Close()

	req, err := http.NewRequestWithContext(ctx, "POST",
		"https://api.openai.com/v1/audio/transcriptions", &body)
	if err != nil {
		return nil, fmt.Errorf("whisper: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+w.apiKey)
	req.Header.Set("Content-Type", writer.FormDataContentType())

	resp, err := w.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("whisper: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("whisper: API returned %d: %s", resp.StatusCode, respBody)
	}

	var result struct {
		Text     string  `json:"text"`
		Language string  `json:"language"`
		Duration float64 `json:"duration"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("whisper: decode: %w", err)
	}

	return &TranscribeResult{
		Text:     result.Text,
		Language: result.Language,
		Duration: time.Duration(result.Duration * float64(time.Second)),
	}, nil
}

// StubSTTProvider returns canned transcription results for testing.
type StubSTTProvider struct {
	Text     string
	Language string
}

func (s *StubSTTProvider) Transcribe(_ context.Context, _ []byte, _ AudioFormat) (*TranscribeResult, error) {
	return &TranscribeResult{
		Text:     s.Text,
		Language: s.Language,
		Duration: 2 * time.Second,
	}, nil
}
