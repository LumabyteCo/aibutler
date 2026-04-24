package voice

import "time"

// AudioFormat identifies an audio container format.
type AudioFormat string

const (
	FormatOGG  AudioFormat = "ogg"
	FormatWAV  AudioFormat = "wav"
	FormatWebM AudioFormat = "webm"
	FormatMP3  AudioFormat = "mp3"
	FormatFLAC AudioFormat = "flac"
)

// TranscribeResult holds the output of speech-to-text.
type TranscribeResult struct {
	Text     string        `json:"text"`
	Language string        `json:"language"` // Auto-detected language code
	Duration time.Duration `json:"duration"`
}

// SynthesizeResult holds the output of text-to-speech.
type SynthesizeResult struct {
	Data     []byte        `json:"data"`
	Format   AudioFormat   `json:"format"`
	Duration time.Duration `json:"duration"`
}
