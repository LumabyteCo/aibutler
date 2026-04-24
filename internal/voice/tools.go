package voice

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"

	"github.com/LumabyteCo/aibutler/internal/tool"
)

// RegisterVoiceTools registers voice.transcribe and voice.speak tools.
func RegisterVoiceTools(registry *tool.Registry, pipeline *Pipeline) {
	registry.Register(&transcribeTool{pipeline: pipeline})
	registry.Register(&speakTool{pipeline: pipeline})
}

// transcribeTool implements voice.transcribe — converts audio to text.
type transcribeTool struct {
	pipeline *Pipeline
}

func (t *transcribeTool) Name() string      { return "voice.transcribe" }
func (t *transcribeTool) Capability() string { return "voice.transcribe" }
func (t *transcribeTool) Description() string {
	return "Transcribe audio data to text using the configured STT provider (e.g., Whisper). Input is base64-encoded audio with format specification."
}
func (t *transcribeTool) Schema() string {
	return `{"type":"object","properties":{"audio_base64":{"type":"string","description":"Base64-encoded audio data"},"format":{"type":"string","description":"Audio format: ogg, wav, mp3, webm, flac","default":"wav"}},"required":["audio_base64"]}`
}

func (t *transcribeTool) Execute(ctx context.Context, input string) (string, error) {
	var args struct {
		AudioBase64 string `json:"audio_base64"`
		Format      string `json:"format"`
	}
	if err := json.Unmarshal([]byte(input), &args); err != nil {
		return "", fmt.Errorf("voice.transcribe: invalid input: %w", err)
	}
	if args.AudioBase64 == "" {
		return "", fmt.Errorf("voice.transcribe: audio_base64 is required")
	}
	if args.Format == "" {
		args.Format = "wav"
	}

	audio, err := base64.StdEncoding.DecodeString(args.AudioBase64)
	if err != nil {
		return "", fmt.Errorf("voice.transcribe: invalid base64: %w", err)
	}

	result, err := t.pipeline.ProcessVoiceInput(ctx, audio, AudioFormat(args.Format))
	if err != nil {
		return "", fmt.Errorf("voice.transcribe: %w", err)
	}

	out, _ := json.Marshal(map[string]interface{}{
		"text":     result.Text,
		"language": result.Language,
		"duration": result.Duration.Seconds(),
	})
	return string(out), nil
}

// speakTool implements voice.speak — converts text to audio.
type speakTool struct {
	pipeline *Pipeline
}

func (t *speakTool) Name() string      { return "voice.speak" }
func (t *speakTool) Capability() string { return "voice.speak" }
func (t *speakTool) Description() string {
	return "Convert text to speech audio using the configured TTS provider. Returns base64-encoded audio data."
}
func (t *speakTool) Schema() string {
	return `{"type":"object","properties":{"text":{"type":"string","description":"Text to synthesize into speech"},"language":{"type":"string","description":"Language code (e.g., en, ar, es)","default":"en"}},"required":["text"]}`
}

func (t *speakTool) Execute(ctx context.Context, input string) (string, error) {
	var args struct {
		Text     string `json:"text"`
		Language string `json:"language"`
	}
	if err := json.Unmarshal([]byte(input), &args); err != nil {
		return "", fmt.Errorf("voice.speak: invalid input: %w", err)
	}
	if args.Text == "" {
		return "", fmt.Errorf("voice.speak: text is required")
	}
	if args.Language == "" {
		args.Language = "en"
	}

	audioData, format, err := t.pipeline.GenerateVoiceResponse(ctx, args.Text, args.Language)
	if err != nil {
		return "", fmt.Errorf("voice.speak: %w", err)
	}
	if audioData == nil {
		return `{"status":"skipped","reason":"voice mode is text-only"}`, nil
	}

	out, _ := json.Marshal(map[string]interface{}{
		"audio_base64": base64.StdEncoding.EncodeToString(audioData),
		"format":       string(format),
		"text":         args.Text,
	})
	return string(out), nil
}
