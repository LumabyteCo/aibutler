package voice

import (
	"context"
	"time"
)

// TTSProvider synthesizes text to audio.
type TTSProvider interface {
	Synthesize(ctx context.Context, text, lang string) (*SynthesizeResult, error)
}

// StubTTSProvider returns placeholder audio for testing.
// Real implementations (Edge TTS, OpenAI TTS) live in sibling files.
type StubTTSProvider struct{}

func (s *StubTTSProvider) Synthesize(_ context.Context, text, _ string) (*SynthesizeResult, error) {
	// Return a minimal WAV header as placeholder
	return &SynthesizeResult{
		Data:     []byte("RIFF----WAVEfmt "), // Fake WAV header
		Format:   FormatWAV,
		Duration: time.Duration(len(text)/15) * time.Second, // ~15 chars/sec
	}, nil
}
