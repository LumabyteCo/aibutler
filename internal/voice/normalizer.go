package voice

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
)

// Normalizer handles audio format conversion.
type Normalizer struct{}

// NewNormalizer creates a new audio normalizer.
func NewNormalizer() *Normalizer {
	return &Normalizer{}
}

// NeedsConversion reports whether the given format needs conversion for STT.
// WAV is the universal target; most STT providers also accept OGG and MP3 directly.
func (n *Normalizer) NeedsConversion(format AudioFormat) bool {
	switch format {
	case FormatWAV, FormatMP3, FormatOGG:
		return false // Widely supported as-is
	default:
		return true // WebM, FLAC, etc. may need conversion
	}
}

// HasFFmpeg checks if ffmpeg is available.
func (n *Normalizer) HasFFmpeg() bool {
	_, err := exec.LookPath("ffmpeg")
	return err == nil
}

// Convert converts audio from one format to WAV using ffmpeg.
// Returns an error if ffmpeg is not available (graceful degradation).
func (n *Normalizer) Convert(ctx context.Context, data []byte, from AudioFormat) ([]byte, error) {
	if !n.HasFFmpeg() {
		return nil, fmt.Errorf("voice: ffmpeg not available for %s->wav conversion (install ffmpeg for voice support)", from)
	}

	cmd := exec.CommandContext(ctx, "ffmpeg",
		"-i", "pipe:0", // Read from stdin
		"-f", "wav", // Output WAV
		"-ar", "16000", // 16kHz sample rate (Whisper optimal)
		"-ac", "1", // Mono
		"pipe:1", // Write to stdout
	)
	cmd.Stdin = bytes.NewReader(data)

	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("voice: ffmpeg conversion failed: %w", err)
	}
	return output, nil
}
