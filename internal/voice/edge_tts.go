package voice

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

// EdgeTTSProvider generates speech using Microsoft Edge TTS (free tier).
type EdgeTTSProvider struct {
	client *http.Client
	voice  string
}

// NewEdgeTTSProvider creates a TTS provider using Edge TTS.
// Default voice is "en-US-AriaNeural".
func NewEdgeTTSProvider(client *http.Client, voice string) *EdgeTTSProvider {
	if client == nil {
		client = http.DefaultClient
	}
	if voice == "" {
		voice = "en-US-AriaNeural"
	}
	return &EdgeTTSProvider{client: client, voice: voice}
}

// Synthesize converts text to speech audio (MP3 bytes).
// Implements TTSProvider interface.
func (e *EdgeTTSProvider) Synthesize(ctx context.Context, text, lang string) (*SynthesizeResult, error) {
	if text == "" {
		return nil, fmt.Errorf("tts: empty text")
	}

	voice := e.voice
	if lang != "" && lang != "en" {
		voice = lang + "-" + voice
	}

	u := fmt.Sprintf("https://api.edgetts.com/v1/synthesize?voice=%s&text=%s",
		url.QueryEscape(voice), url.QueryEscape(text))

	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return nil, fmt.Errorf("tts: %w", err)
	}

	resp, err := e.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("tts: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("tts: API returned %d: %s", resp.StatusCode, string(body))
	}

	audio, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("tts: read audio: %w", err)
	}

	return &SynthesizeResult{
		Data:     audio,
		Format:   FormatMP3,
		Duration: time.Duration(len(text)/15) * time.Second,
	}, nil
}

// Name returns the provider name.
func (e *EdgeTTSProvider) Name() string {
	return "edge-tts"
}

// Verify interface compliance.
var _ TTSProvider = (*EdgeTTSProvider)(nil)
