package voice_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"testing"

	"github.com/LumabyteCo/aibutler/internal/tool"
	"github.com/LumabyteCo/aibutler/internal/voice"
)

func newTestPipeline(mode string) *voice.Pipeline {
	stt := &voice.StubSTTProvider{Text: "[stub transcription]", Language: "en"}
	tts := &voice.StubTTSProvider{}
	return voice.NewPipeline(stt, tts, voice.NewNormalizer(), mode)
}

func TestTranscribeToolExecute(t *testing.T) {
	reg := tool.NewRegistry()
	voice.RegisterVoiceTools(reg, newTestPipeline("text"))

	tr, ok := reg.Get("voice.transcribe")
	if !ok {
		t.Fatal("voice.transcribe tool not registered")
	}
	if tr.Name() != "voice.transcribe" {
		t.Errorf("name = %q, want voice.transcribe", tr.Name())
	}
	if tr.Capability() != "voice.transcribe" {
		t.Errorf("capability = %q, want voice.transcribe", tr.Capability())
	}
	if tr.Description() == "" {
		t.Error("description should not be empty")
	}
	if tr.Schema() == "" {
		t.Error("schema should not be empty")
	}

	audio := base64.StdEncoding.EncodeToString([]byte("fake audio data"))
	result, err := tr.Execute(context.Background(), `{"audio_base64":"`+audio+`","format":"wav"}`)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}

	var out map[string]interface{}
	if err := json.Unmarshal([]byte(result), &out); err != nil {
		t.Fatalf("parse result: %v", err)
	}
	if out["text"] != "[stub transcription]" {
		t.Errorf("text = %v, want '[stub transcription]'", out["text"])
	}
	if out["language"] != "en" {
		t.Errorf("language = %v, want 'en'", out["language"])
	}
}

func TestTranscribeToolMissingAudio(t *testing.T) {
	reg := tool.NewRegistry()
	voice.RegisterVoiceTools(reg, newTestPipeline("text"))

	tr, _ := reg.Get("voice.transcribe")
	_, err := tr.Execute(context.Background(), `{}`)
	if err == nil {
		t.Error("expected error for missing audio_base64")
	}
}

func TestTranscribeToolInvalidBase64(t *testing.T) {
	reg := tool.NewRegistry()
	voice.RegisterVoiceTools(reg, newTestPipeline("text"))

	tr, _ := reg.Get("voice.transcribe")
	_, err := tr.Execute(context.Background(), `{"audio_base64":"not valid base64!!!"}`)
	if err == nil {
		t.Error("expected error for invalid base64")
	}
}

func TestTranscribeToolDefaultFormat(t *testing.T) {
	reg := tool.NewRegistry()
	voice.RegisterVoiceTools(reg, newTestPipeline("text"))

	tr, _ := reg.Get("voice.transcribe")
	audio := base64.StdEncoding.EncodeToString([]byte("fake"))
	result, err := tr.Execute(context.Background(), `{"audio_base64":"`+audio+`"}`)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result == "" {
		t.Error("expected non-empty result")
	}
}

func TestTranscribeToolInvalidJSON(t *testing.T) {
	reg := tool.NewRegistry()
	voice.RegisterVoiceTools(reg, newTestPipeline("text"))

	tr, _ := reg.Get("voice.transcribe")
	_, err := tr.Execute(context.Background(), `not json`)
	if err == nil {
		t.Error("expected error for invalid JSON input")
	}
}

func TestSpeakToolExecute(t *testing.T) {
	reg := tool.NewRegistry()
	voice.RegisterVoiceTools(reg, newTestPipeline("voice"))

	sp, ok := reg.Get("voice.speak")
	if !ok {
		t.Fatal("voice.speak tool not registered")
	}
	if sp.Name() != "voice.speak" {
		t.Errorf("name = %q, want voice.speak", sp.Name())
	}
	if sp.Capability() != "voice.speak" {
		t.Errorf("capability = %q, want voice.speak", sp.Capability())
	}

	result, err := sp.Execute(context.Background(), `{"text":"Hello world","language":"en"}`)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}

	var out map[string]interface{}
	if err := json.Unmarshal([]byte(result), &out); err != nil {
		t.Fatalf("parse result: %v", err)
	}
	if out["text"] != "Hello world" {
		t.Errorf("text = %v, want 'Hello world'", out["text"])
	}
}

func TestSpeakToolTextOnlyMode(t *testing.T) {
	reg := tool.NewRegistry()
	voice.RegisterVoiceTools(reg, newTestPipeline("text"))

	sp, _ := reg.Get("voice.speak")
	result, err := sp.Execute(context.Background(), `{"text":"Hello"}`)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}

	var out map[string]interface{}
	json.Unmarshal([]byte(result), &out)
	if out["status"] != "skipped" {
		t.Errorf("expected skipped status in text-only mode, got %v", out["status"])
	}
}

func TestSpeakToolMissingText(t *testing.T) {
	reg := tool.NewRegistry()
	voice.RegisterVoiceTools(reg, newTestPipeline("voice"))

	sp, _ := reg.Get("voice.speak")
	_, err := sp.Execute(context.Background(), `{"text":""}`)
	if err == nil {
		t.Error("expected error for empty text")
	}
}

func TestSpeakToolDefaultLanguage(t *testing.T) {
	reg := tool.NewRegistry()
	voice.RegisterVoiceTools(reg, newTestPipeline("voice"))

	sp, _ := reg.Get("voice.speak")
	result, err := sp.Execute(context.Background(), `{"text":"Hi"}`)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result == "" {
		t.Error("expected non-empty result")
	}
}

func TestSpeakToolInvalidJSON(t *testing.T) {
	reg := tool.NewRegistry()
	voice.RegisterVoiceTools(reg, newTestPipeline("voice"))

	sp, _ := reg.Get("voice.speak")
	_, err := sp.Execute(context.Background(), `not json`)
	if err == nil {
		t.Error("expected error for invalid JSON input")
	}
}
