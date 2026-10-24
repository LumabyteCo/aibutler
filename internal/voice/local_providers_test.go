package voice

import (
	"context"
	"encoding/json"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestNormalizeLocalSTTEndpoint(t *testing.T) {
	cases := map[string]string{
		"http://localhost:8080":                            "http://localhost:8080/v1/audio/transcriptions",
		"http://localhost:8080/":                           "http://localhost:8080/v1/audio/transcriptions",
		"http://localhost:8080/v1":                         "http://localhost:8080/v1/audio/transcriptions",
		"http://localhost:8080/v1/audio/transcriptions":    "http://localhost:8080/v1/audio/transcriptions",
		"https://stt.example/api/v1/audio/transcriptions/": "https://stt.example/api/v1/audio/transcriptions",
		"": "",
	}
	for in, want := range cases {
		if got := normalizeLocalSTTEndpoint(in); got != want {
			t.Errorf("normalizeLocalSTTEndpoint(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestLocalSTTProviderTranscribe(t *testing.T) {
	var gotPath, gotAuth, gotModel, gotFile string
	var gotData []byte

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		_, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if err != nil {
			t.Fatalf("content type: %v", err)
		}
		mr := multipart.NewReader(r.Body, params["boundary"])
		for {
			part, err := mr.NextPart()
			if err != nil {
				break
			}
			switch part.FormName() {
			case "model":
				b, _ := io.ReadAll(part)
				gotModel = string(b)
			case "file":
				gotFile = part.FileName()
				gotData, _ = io.ReadAll(part)
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"text": " Hey Buggler, lights off. ", "language": "en", "duration": 2.5,
		})
	}))
	defer srv.Close()

	p := NewLocalSTTProvider(srv.URL, "secret", "whisper-1", nil)
	res, err := p.Transcribe(context.Background(), []byte("RIFFfake"), FormatWAV)
	if err != nil {
		t.Fatalf("Transcribe: %v", err)
	}
	if gotPath != "/v1/audio/transcriptions" {
		t.Errorf("path = %q", gotPath)
	}
	if gotAuth != "Bearer secret" {
		t.Errorf("auth = %q", gotAuth)
	}
	if gotModel != "whisper-1" {
		t.Errorf("model = %q", gotModel)
	}
	if gotFile != "audio.wav" || len(gotData) == 0 {
		t.Errorf("file = %q (bytes=%d)", gotFile, len(gotData))
	}
	if res.Text != "Hey Buggler, lights off." {
		t.Errorf("text = %q (should be trimmed)", res.Text)
	}
	if res.Language != "en" || res.Duration != 2500*time.Millisecond {
		t.Errorf("language/duration = %q/%v", res.Language, res.Duration)
	}
}

func TestCommandSTTProvider(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "transcribe.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nprintf 'hey butler turn off the light' > \"$1/out.txt\"\n"), 0o700); err != nil {
		t.Fatal(err)
	}

	p := NewCommandSTTProvider([]string{script, "{dir}"})
	res, err := p.Transcribe(context.Background(), []byte("RIFFfake"), FormatWAV)
	if err != nil {
		t.Fatalf("Transcribe: %v", err)
	}
	if res.Text != "hey butler turn off the light" {
		t.Errorf("text = %q", res.Text)
	}
}

func TestCommandSTTProviderEnv(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "env.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nprintf \"%s\" \"$VOICE_TEST_ENV\" > \"$1/out.txt\"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	p := NewCommandSTTProvider([]string{script, "{dir}"})
	p.SetEnv(map[string]string{"VOICE_TEST_ENV": "hello-env"})
	res, err := p.Transcribe(context.Background(), []byte("x"), FormatWAV)
	if err != nil {
		t.Fatalf("Transcribe: %v", err)
	}
	if res.Text != "hello-env" {
		t.Errorf("text = %q, want env value", res.Text)
	}
}

func TestCommandSTTProviderErrors(t *testing.T) {
	if _, err := NewCommandSTTProvider(nil).Transcribe(context.Background(), nil, FormatWAV); err == nil {
		t.Error("empty command must fail")
	}

	if _, err := NewCommandSTTProvider([]string{"/bin/false"}).Transcribe(context.Background(), []byte("x"), FormatWAV); err == nil {
		t.Error("failing command must fail")
	}

	if _, err := NewCommandSTTProvider([]string{"/bin/true"}).Transcribe(context.Background(), []byte("x"), FormatWAV); err == nil {
		t.Error("command without a transcript must fail")
	}
}

func TestSystemTTSOnDarwin(t *testing.T) {
	p := NewSystemTTSProvider("")
	if !p.Available() {
		t.Skip("no system speech engine on this platform")
	}
	res, err := p.Synthesize(context.Background(), "Offline voice is working.", "en")
	if err != nil {
		t.Fatalf("Synthesize: %v", err)
	}
	if len(res.Data) < 44 || string(res.Data[0:4]) != "RIFF" {
		t.Errorf("expected a WAV buffer, got %d bytes", len(res.Data))
	}
	if res.Format != FormatWAV || res.Duration <= 0 {
		t.Errorf("format/duration = %s/%v", res.Format, res.Duration)
	}
}

func TestWAVHelpers(t *testing.T) {
	silence := pcm16WAV(make([]int16, 16000), 16000)
	if d := wavDuration(silence); d < 900*time.Millisecond || d > 1100*time.Millisecond {
		t.Errorf("silence duration = %v, want ~1s", d)
	}
	if HasSpeech(silence, 250) {
		t.Error("silence must not pass the speech gate")
	}

	tone := toneWAV(100*time.Millisecond, 880)
	if len(tone) < 44 || string(tone[0:4]) != "RIFF" {
		t.Fatal("tone is not a WAV")
	}
	if !HasSpeech(tone, 250) {
		t.Error("tone must pass the speech gate")
	}
	if wavDuration(tone) <= 0 {
		t.Error("tone duration must be positive")
	}
}

func TestMatchWakeWord(t *testing.T) {
	yes := []string{
		"Hey Butler",
		"hey, butler!",
		"Hey Buggler, turn off the living room light.",
		"hey but ler",
		"Butler?",
		"okay hey butler",
	}
	for _, s := range yes {
		if !MatchWakeWord(s, "hey butler") {
			t.Errorf("MatchWakeWord(%q) = false, want true", s)
		}
	}
	no := []string{
		"turn off the living room light",
		"hello there",
		"hey buddy",
		"turn on the butler light",
		"",
	}
	for _, s := range no {
		if MatchWakeWord(s, "hey butler") {
			t.Errorf("MatchWakeWord(%q) = true, want false", s)
		}
	}
	if !MatchWakeWord("hey computer", "computer") {
		t.Error("custom keyword should match")
	}
}
