package elevenlabs_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/LumabyteCo/aibutler/internal/voice/elevenlabs"
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

func TestSynthesize_Success(t *testing.T) {
	fakeAudio := []byte("fake-mp3-data")
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "text-to-speech") {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Header.Get("xi-api-key") == "" {
			t.Error("missing xi-api-key header")
		}
		w.Header().Set("Content-Type", "audio/mpeg")
		w.WriteHeader(http.StatusOK)
		w.Write(fakeAudio)
	}))
	defer ts.Close()

	c := elevenlabs.NewClient("test-key", "voice123")
	c.SetBaseURL(ts.URL)
	c.SetHTTPClient(ts.Client())

	audio, err := c.Synthesize(context.Background(), "Hello world")
	if err != nil {
		t.Fatalf("Synthesize: unexpected error: %v", err)
	}
	if string(audio) != string(fakeAudio) {
		t.Errorf("expected %q, got %q", fakeAudio, audio)
	}
}

func TestListVoices_Success(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/voices" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		resp := `{"voices":[{"voice_id":"v1","name":"Alice"},{"voice_id":"v2","name":"Bob"}]}`
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(resp))
	}))
	defer ts.Close()

	c := elevenlabs.NewClient("test-key", "voice123")
	c.SetBaseURL(ts.URL)
	c.SetHTTPClient(ts.Client())

	voices, err := c.ListVoices(context.Background())
	if err != nil {
		t.Fatalf("ListVoices: unexpected error: %v", err)
	}
	if len(voices) != 2 {
		t.Errorf("expected 2 voices, got %d", len(voices))
	}
	if voices[0].Name != "Alice" {
		t.Errorf("expected Alice, got %s", voices[0].Name)
	}
}

func TestSynthesize_Error(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"detail":"Invalid API key"}`))
	}))
	defer ts.Close()

	c := elevenlabs.NewClient("bad-key", "voice123")
	c.SetBaseURL(ts.URL)
	c.SetHTTPClient(ts.Client())

	_, err := c.Synthesize(context.Background(), "Hello")
	if err == nil {
		t.Fatal("expected error for 401 response")
	}
	if !strings.Contains(err.Error(), "401") {
		t.Errorf("expected 401 in error, got: %v", err)
	}
}

func TestRegisterElevenLabsTools(t *testing.T) {
	reg := newMockRegistry()
	c := elevenlabs.NewClient("key", "voice")
	elevenlabs.RegisterElevenLabsTools(reg, c)

	want := map[string]bool{
		"voice.elevenlabs.synthesize":  false,
		"voice.elevenlabs.list_voices": false,
	}
	for _, name := range reg.tools {
		if _, ok := want[name]; ok {
			want[name] = true
		}
	}
	for name, found := range want {
		if !found {
			t.Errorf("tool %q was not registered", name)
		}
	}
}

func TestSynthesizeTool_Execute(t *testing.T) {
	fakeAudio := []byte("mp3bytes")
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write(fakeAudio)
	}))
	defer ts.Close()

	reg := newMockRegistry()
	c := elevenlabs.NewClient("key", "voice")
	c.SetBaseURL(ts.URL)
	c.SetHTTPClient(ts.Client())
	elevenlabs.RegisterElevenLabsTools(reg, c)

	synthExec := reg.exec["voice.elevenlabs.synthesize"]
	input, _ := json.Marshal(map[string]string{"text": "Hi there"})
	out, err := synthExec(context.Background(), string(input))
	if err != nil {
		t.Fatalf("synthesize tool exec: %v", err)
	}
	if !strings.Contains(out, "audio_base64") {
		t.Errorf("expected audio_base64 in output, got %q", out)
	}
}
