package voice

import (
	"context"
	"encoding/json"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestAudioFormats(t *testing.T) {
	tests := []struct {
		format AudioFormat
		want   string
	}{
		{FormatOGG, "ogg"},
		{FormatWAV, "wav"},
		{FormatWebM, "webm"},
		{FormatMP3, "mp3"},
		{FormatFLAC, "flac"},
	}
	for _, tt := range tests {
		if string(tt.format) != tt.want {
			t.Errorf("Format %v = %q, want %q", tt.format, string(tt.format), tt.want)
		}
	}
}

func TestStubSTTTranscribe(t *testing.T) {
	stub := &StubSTTProvider{Text: "hello world", Language: "en"}
	result, err := stub.Transcribe(context.Background(), []byte("audio"), FormatWAV)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Text != "hello world" {
		t.Errorf("Text = %q, want %q", result.Text, "hello world")
	}
	if result.Language != "en" {
		t.Errorf("Language = %q, want %q", result.Language, "en")
	}
	if result.Duration != 2*time.Second {
		t.Errorf("Duration = %v, want %v", result.Duration, 2*time.Second)
	}
}

func TestStubTTSSynthesize(t *testing.T) {
	stub := &StubTTSProvider{}
	result, err := stub.Synthesize(context.Background(), "hello world", "en")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Format != FormatWAV {
		t.Errorf("Format = %q, want %q", result.Format, FormatWAV)
	}
	if len(result.Data) == 0 {
		t.Error("Data is empty")
	}
	// Verify it starts with RIFF (WAV magic bytes)
	if !strings.HasPrefix(string(result.Data), "RIFF") {
		t.Errorf("Data should start with RIFF header, got %q", string(result.Data[:4]))
	}
}

func TestWhisperProviderRequestFormat(t *testing.T) {
	var gotModel, gotFormat string
	var gotFileData []byte
	var gotFileName string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify auth header
		auth := r.Header.Get("Authorization")
		if auth != "Bearer test-key" {
			t.Errorf("Authorization = %q, want %q", auth, "Bearer test-key")
		}

		// Parse multipart
		ct := r.Header.Get("Content-Type")
		mediaType, params, err := mime.ParseMediaType(ct)
		if err != nil {
			t.Errorf("parse content-type: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if !strings.HasPrefix(mediaType, "multipart/") {
			t.Errorf("expected multipart, got %q", mediaType)
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		mr := multipart.NewReader(r.Body, params["boundary"])
		for {
			part, err := mr.NextPart()
			if err == io.EOF {
				break
			}
			if err != nil {
				t.Errorf("read part: %v", err)
				break
			}
			switch part.FormName() {
			case "file":
				gotFileName = part.FileName()
				gotFileData, _ = io.ReadAll(part)
			case "model":
				b, _ := io.ReadAll(part)
				gotModel = string(b)
			case "response_format":
				b, _ := io.ReadAll(part)
				gotFormat = string(b)
			}
		}

		// Return valid response
		json.NewEncoder(w).Encode(map[string]any{
			"text":     "transcribed text",
			"language": "en",
			"duration": 3.5,
		})
	}))
	defer srv.Close()

	// Create provider pointing at test server
	wp := NewWhisperProvider("test-key", nil)
	wp.client = srv.Client()

	// Override the URL by creating a custom request -- we need to use the test server.
	// Instead, let's create a provider that uses the test server URL.
	// We'll test via a helper that replaces the endpoint.
	// For simplicity, we'll use a custom transport.
	wp.client = &http.Client{
		Timeout: 5 * time.Second,
		Transport: &rewriteTransport{
			base: srv.Client().Transport,
			url:  srv.URL,
		},
	}

	audio := []byte("fake audio data")
	result, err := wp.Transcribe(context.Background(), audio, FormatOGG)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify request fields
	if gotModel != "whisper-1" {
		t.Errorf("model = %q, want %q", gotModel, "whisper-1")
	}
	if gotFormat != "verbose_json" {
		t.Errorf("response_format = %q, want %q", gotFormat, "verbose_json")
	}
	if gotFileName != "audio.ogg" {
		t.Errorf("filename = %q, want %q", gotFileName, "audio.ogg")
	}
	if string(gotFileData) != "fake audio data" {
		t.Errorf("file data = %q, want %q", string(gotFileData), "fake audio data")
	}

	// Verify response parsing
	if result.Text != "transcribed text" {
		t.Errorf("Text = %q, want %q", result.Text, "transcribed text")
	}
	if result.Language != "en" {
		t.Errorf("Language = %q, want %q", result.Language, "en")
	}
	if result.Duration != 3500*time.Millisecond {
		t.Errorf("Duration = %v, want %v", result.Duration, 3500*time.Millisecond)
	}
}

// rewriteTransport redirects all requests to the test server URL.
type rewriteTransport struct {
	base http.RoundTripper
	url  string
}

func (rt *rewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req.URL.Scheme = "http"
	req.URL.Host = strings.TrimPrefix(rt.url, "http://")
	if rt.base != nil {
		return rt.base.RoundTrip(req)
	}
	return http.DefaultTransport.RoundTrip(req)
}

func TestWhisperProviderError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error":{"message":"invalid audio"}}`))
	}))
	defer srv.Close()

	wp := NewWhisperProvider("test-key", nil)
	wp.client = &http.Client{
		Timeout: 5 * time.Second,
		Transport: &rewriteTransport{url: srv.URL},
	}

	_, err := wp.Transcribe(context.Background(), []byte("bad"), FormatWAV)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "400") {
		t.Errorf("error should contain status 400, got: %v", err)
	}
	if !strings.Contains(err.Error(), "invalid audio") {
		t.Errorf("error should contain body, got: %v", err)
	}
}

func TestNormalizerNeedsConversion(t *testing.T) {
	n := NewNormalizer()

	tests := []struct {
		format AudioFormat
		want   bool
	}{
		{FormatWAV, false},
		{FormatOGG, false},
		{FormatMP3, false},
		{FormatWebM, true},
		{FormatFLAC, true},
	}
	for _, tt := range tests {
		got := n.NeedsConversion(tt.format)
		if got != tt.want {
			t.Errorf("NeedsConversion(%q) = %v, want %v", tt.format, got, tt.want)
		}
	}
}

func TestNormalizerConvertNoFFmpeg(t *testing.T) {
	// Save and clear PATH to ensure ffmpeg is not found
	t.Setenv("PATH", "")

	n := NewNormalizer()
	_, err := n.Convert(context.Background(), []byte("data"), FormatWebM)
	if err == nil {
		t.Fatal("expected error when ffmpeg not available")
	}
	if !strings.Contains(err.Error(), "ffmpeg not available") {
		t.Errorf("error should mention ffmpeg, got: %v", err)
	}
}

func TestPipelineProcessVoiceInput(t *testing.T) {
	stub := &StubSTTProvider{Text: "recognized speech", Language: "fr"}
	p := NewPipeline(stub, &StubTTSProvider{}, NewNormalizer(), "text")

	result, err := p.ProcessVoiceInput(context.Background(), []byte("audio"), FormatWAV)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Text != "recognized speech" {
		t.Errorf("Text = %q, want %q", result.Text, "recognized speech")
	}
	if result.Language != "fr" {
		t.Errorf("Language = %q, want %q", result.Language, "fr")
	}
}

func TestPipelineProcessVoiceInputNeedsConversion(t *testing.T) {
	// WebM needs conversion; ffmpeg won't be available (clear PATH),
	// so pipeline should fall back to raw transcription via the stub.
	t.Setenv("PATH", "")

	stub := &StubSTTProvider{Text: "fallback result", Language: "de"}
	p := NewPipeline(stub, &StubTTSProvider{}, NewNormalizer(), "text")

	result, err := p.ProcessVoiceInput(context.Background(), []byte("webm data"), FormatWebM)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Text != "fallback result" {
		t.Errorf("Text = %q, want %q", result.Text, "fallback result")
	}
}

func TestPipelineGenerateVoiceText(t *testing.T) {
	p := NewPipeline(&StubSTTProvider{}, &StubTTSProvider{}, NewNormalizer(), "text")

	data, format, err := p.GenerateVoiceResponse(context.Background(), "hello", "en")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if data != nil {
		t.Errorf("expected nil data for text mode, got %d bytes", len(data))
	}
	if format != "" {
		t.Errorf("expected empty format for text mode, got %q", format)
	}
}

func TestPipelineGenerateVoiceBoth(t *testing.T) {
	p := NewPipeline(&StubSTTProvider{}, &StubTTSProvider{}, NewNormalizer(), "both")

	data, format, err := p.GenerateVoiceResponse(context.Background(), "hello", "en")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if data == nil {
		t.Fatal("expected TTS data for both mode, got nil")
	}
	if format != FormatWAV {
		t.Errorf("Format = %q, want %q", format, FormatWAV)
	}
}

func TestPipelineModeAuto(t *testing.T) {
	p := NewPipeline(&StubSTTProvider{}, &StubTTSProvider{}, NewNormalizer(), "auto")

	// Short text: should produce voice
	data, format, err := p.GenerateVoiceResponse(context.Background(), "short reply", "en")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if data == nil {
		t.Fatal("expected TTS data for short text in auto mode, got nil")
	}
	if format != FormatWAV {
		t.Errorf("Format = %q, want %q", format, FormatWAV)
	}

	// Long text (>500 chars): should return nil
	longText := strings.Repeat("a", 501)
	data, format, err = p.GenerateVoiceResponse(context.Background(), longText, "en")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if data != nil {
		t.Errorf("expected nil data for long text in auto mode, got %d bytes", len(data))
	}
	if format != "" {
		t.Errorf("expected empty format for long text, got %q", format)
	}
}

func TestPipelineEmptyInput(t *testing.T) {
	p := NewPipeline(&StubSTTProvider{}, &StubTTSProvider{}, NewNormalizer(), "text")

	_, err := p.ProcessVoiceInput(context.Background(), nil, FormatWAV)
	if err == nil {
		t.Fatal("expected error for nil audio")
	}
	if !strings.Contains(err.Error(), "empty audio") {
		t.Errorf("error should mention empty audio, got: %v", err)
	}

	_, err = p.ProcessVoiceInput(context.Background(), []byte{}, FormatWAV)
	if err == nil {
		t.Fatal("expected error for empty audio")
	}
	if !strings.Contains(err.Error(), "empty audio") {
		t.Errorf("error should mention empty audio, got: %v", err)
	}
}

func TestPipelineDefaultMode(t *testing.T) {
	p := NewPipeline(&StubSTTProvider{}, &StubTTSProvider{}, NewNormalizer(), "")
	if p.Mode() != "text" {
		t.Errorf("Mode() = %q, want %q", p.Mode(), "text")
	}
}
