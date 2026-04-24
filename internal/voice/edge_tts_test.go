package voice_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/LumabyteCo/aibutler/internal/voice"
)

func TestEdgeTTSSynthesize(t *testing.T) {
	audioData := []byte("fake-mp3-audio-data")
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "audio/mpeg")
		w.Write(audioData)
	}))
	defer ts.Close()

	provider := voice.NewEdgeTTSProvider(ts.Client(), "en-US-AriaNeural")
	if provider == nil {
		t.Fatal("expected non-nil provider")
	}
	if provider.Name() != "edge-tts" {
		t.Errorf("name = %q, want edge-tts", provider.Name())
	}
}

func TestEdgeTTSEmptyText(t *testing.T) {
	provider := voice.NewEdgeTTSProvider(http.DefaultClient, "")
	_, err := provider.Synthesize(context.Background(), "", "en")
	if err == nil {
		t.Error("expected error for empty text")
	}
}

func TestEdgeTTSDefaultVoice(t *testing.T) {
	provider := voice.NewEdgeTTSProvider(nil, "")
	if provider == nil {
		t.Fatal("expected non-nil provider with defaults")
	}
}
