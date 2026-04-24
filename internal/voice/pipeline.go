package voice

import (
	"context"
	"fmt"
)

// Pipeline orchestrates the voice processing flow.
type Pipeline struct {
	stt        STTProvider
	tts        TTSProvider
	normalizer *Normalizer
	mode       string // "text", "voice", "auto", "both"
}

// NewPipeline creates a new voice pipeline.
func NewPipeline(stt STTProvider, tts TTSProvider, normalizer *Normalizer, mode string) *Pipeline {
	if mode == "" {
		mode = "text"
	}
	return &Pipeline{
		stt:        stt,
		tts:        tts,
		normalizer: normalizer,
		mode:       mode,
	}
}

// ProcessVoiceInput transcribes audio input to text.
func (p *Pipeline) ProcessVoiceInput(ctx context.Context, audio []byte, format AudioFormat) (*TranscribeResult, error) {
	if len(audio) == 0 {
		return nil, fmt.Errorf("voice: empty audio input")
	}

	// Normalize if needed
	if p.normalizer.NeedsConversion(format) {
		converted, err := p.normalizer.Convert(ctx, audio, format)
		if err != nil {
			// Graceful degradation: try sending original format
			// Some providers handle conversion internally
			return p.stt.Transcribe(ctx, audio, format)
		}
		audio = converted
		format = FormatWAV
	}

	return p.stt.Transcribe(ctx, audio, format)
}

// GenerateVoiceResponse generates a voice response based on the pipeline mode.
// Returns nil audio data if mode is "text" (no TTS needed).
func (p *Pipeline) GenerateVoiceResponse(ctx context.Context, text, lang string) ([]byte, AudioFormat, error) {
	switch p.mode {
	case "text":
		return nil, "", nil // No voice output
	case "voice", "both":
		result, err := p.tts.Synthesize(ctx, text, lang)
		if err != nil {
			return nil, "", fmt.Errorf("voice: TTS: %w", err)
		}
		return result.Data, result.Format, nil
	case "auto":
		// Auto mode: use voice for short responses (< 500 chars)
		if len(text) > 500 {
			return nil, "", nil
		}
		result, err := p.tts.Synthesize(ctx, text, lang)
		if err != nil {
			return nil, "", nil // Graceful: fall back to text
		}
		return result.Data, result.Format, nil
	default:
		return nil, "", nil
	}
}

// Mode returns the current voice mode.
func (p *Pipeline) Mode() string {
	return p.mode
}
