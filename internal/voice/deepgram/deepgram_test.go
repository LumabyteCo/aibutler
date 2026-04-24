package deepgram_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/LumabyteCo/aibutler/internal/voice/deepgram"
)

type mockRegistry struct {
	tools []string
	exec  map[string]func(ctx context.Context, input string) (string, error)
}

func newMockRegistry() *mockRegistry {
	return &mockRegistry{exec: make(map[string]func(ctx context.Context, input string) (string, error))}
}

func (m *mockRegistry) Register(name, description, schema, capability string, exec func(ctx context.Context, input string) (string, error)) {
	m.tools = append(m.tools, name)
	m.exec[name] = exec
}

const fakeTranscriptResponse = `{
	"results": {
		"channels": [{
			"alternatives": [{"transcript": "hello world"}]
		}]
	}
}`

func TestTranscribeURL_Success(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "" {
			t.Error("missing Authorization header")
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(fakeTranscriptResponse))
	}))
	defer ts.Close()

	c := deepgram.NewClient("test-key")
	c.SetBaseURL(ts.URL)
	c.SetHTTPClient(ts.Client())

	transcript, err := c.TranscribeURL(context.Background(), "https://example.com/audio.mp3")
	if err != nil {
		t.Fatalf("TranscribeURL: unexpected error: %v", err)
	}
	if transcript != "hello world" {
		t.Errorf("expected 'hello world', got %q", transcript)
	}
}

func TestTranscribeURL_Error(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error":"Invalid credentials"}`))
	}))
	defer ts.Close()

	c := deepgram.NewClient("bad-key")
	c.SetBaseURL(ts.URL)
	c.SetHTTPClient(ts.Client())

	_, err := c.TranscribeURL(context.Background(), "https://example.com/audio.mp3")
	if err == nil {
		t.Fatal("expected error for 401 response")
	}
}

func TestTranscribe_Bytes(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(fakeTranscriptResponse))
	}))
	defer ts.Close()

	c := deepgram.NewClient("test-key")
	c.SetBaseURL(ts.URL)
	c.SetHTTPClient(ts.Client())

	transcript, err := c.Transcribe(context.Background(), []byte("fake-audio"), "audio/wav")
	if err != nil {
		t.Fatalf("Transcribe: unexpected error: %v", err)
	}
	if transcript != "hello world" {
		t.Errorf("expected 'hello world', got %q", transcript)
	}
}

func TestRegisterDeepgramTools(t *testing.T) {
	reg := newMockRegistry()
	c := deepgram.NewClient("key")
	deepgram.RegisterDeepgramTools(reg, c)

	found := false
	for _, name := range reg.tools {
		if name == "voice.deepgram.transcribe_url" {
			found = true
		}
	}
	if !found {
		t.Error("voice.deepgram.transcribe_url was not registered")
	}
}

func TestTranscribeURLTool_Execute(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(fakeTranscriptResponse))
	}))
	defer ts.Close()

	reg := newMockRegistry()
	c := deepgram.NewClient("key")
	c.SetBaseURL(ts.URL)
	c.SetHTTPClient(ts.Client())
	deepgram.RegisterDeepgramTools(reg, c)

	exec := reg.exec["voice.deepgram.transcribe_url"]
	if exec == nil {
		t.Fatal("tool not registered")
	}

	input, _ := json.Marshal(map[string]string{"url": "https://example.com/audio.mp3"})
	out, err := exec(context.Background(), string(input))
	if err != nil {
		t.Fatalf("tool exec: %v", err)
	}
	if !strings.Contains(out, "hello world") {
		t.Errorf("expected transcript in output, got %q", out)
	}
}
