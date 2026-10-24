package voicechannel_test

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/LumabyteCo/aibutler/internal/channel"
	voicechannel "github.com/LumabyteCo/aibutler/internal/channel/voice"
	"github.com/LumabyteCo/aibutler/internal/voice"
)

type fakeTTS struct {
	mu   sync.Mutex
	text string
}

func (t *fakeTTS) Synthesize(_ context.Context, text, _ string) (*voice.SynthesizeResult, error) {
	t.mu.Lock()
	t.text = text
	t.mu.Unlock()
	return &voice.SynthesizeResult{Data: []byte("RIFFfake"), Format: voice.FormatWAV}, nil
}

type fakePlayer struct {
	mu    sync.Mutex
	plays int
}

func (p *fakePlayer) Play(context.Context, []byte, voice.AudioFormat) error {
	p.mu.Lock()
	p.plays++
	p.mu.Unlock()
	return nil
}
func (p *fakePlayer) Available() bool { return true }

func TestSendSpeaksAndTruncates(t *testing.T) {
	tts := &fakeTTS{}
	player := &fakePlayer{}
	a := voicechannel.New(nil, tts, player)

	if err := a.Send(context.Background(), "local", channel.OutgoingMessage{Text: "  "}); err != nil {
		t.Fatalf("empty send: %v", err)
	}
	if player.plays != 0 {
		t.Errorf("empty text played %d times", player.plays)
	}

	long := strings.Repeat("word ", 300) // 1500 runes
	if err := a.Send(context.Background(), "local", channel.OutgoingMessage{Text: long}); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if player.plays != 1 {
		t.Errorf("plays = %d, want 1", player.plays)
	}
	if n := len([]rune(tts.text)); n > 601 {
		t.Errorf("spoken text has %d runes, want <= 601", n)
	}
}

func TestStartWithoutRecorder(t *testing.T) {
	loop := &voice.Loop{Recorder: &fakeRecorder{}, STT: &fakeSTT{}}
	a := voicechannel.New(loop, &fakeTTS{}, &fakePlayer{})
	if err := a.Start(context.Background(), func(context.Context, channel.Envelope) error { return nil }); err == nil {
		t.Fatal("Start must fail without a microphone recorder")
	}
}

func TestStartRoutesTranscript(t *testing.T) {
	rec := &fakeRecorder{available: true}
	stt := &fakeSTT{texts: []string{"hey butler", "turn on the kitchen light"}}
	det := voice.NewWakeWordDetector("hey butler")
	det.Configure(stt, &infiniteSource{})

	loop := &voice.Loop{
		Wake:          det,
		Recorder:      rec,
		STT:           stt,
		CommandWindow: 2 * time.Second,
	}
	a := voicechannel.New(loop, &fakeTTS{}, &fakePlayer{})

	got := make(chan channel.Envelope, 1)
	handler := func(_ context.Context, env channel.Envelope) error {
		got <- env
		return nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := a.Start(ctx, handler); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer a.Stop(context.Background())

	select {
	case env := <-got:
		if env.Channel != "voice" || env.AccountID != "local" {
			t.Errorf("envelope channel/account = %s/%s", env.Channel, env.AccountID)
		}
		if env.Text != "turn on the kitchen light" {
			t.Errorf("transcript = %q", env.Text)
		}
		if env.Type != channel.TypeVoice {
			t.Errorf("type = %q, want voice", env.Type)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("voice channel never routed a transcript")
	}
}

// --- fakes ---------------------------------------------------------------

type fakeRecorder struct{ available bool }

func (r *fakeRecorder) Record(context.Context, time.Duration) ([]byte, voice.AudioFormat, error) {
	return wavTone(), voice.FormatWAV, nil
}
func (r *fakeRecorder) Available() bool { return r.available }
func (r *fakeRecorder) Name() string    { return "fake" }

type fakeSTT struct {
	mu    sync.Mutex
	texts []string
	calls int
}

func (f *fakeSTT) Transcribe(context.Context, []byte, voice.AudioFormat) (*voice.TranscribeResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	i := f.calls
	f.calls++
	if i < len(f.texts) {
		return &voice.TranscribeResult{Text: f.texts[i], Language: "en"}, nil
	}
	return &voice.TranscribeResult{Text: ""}, nil
}

type infiniteSource struct{}

func (s *infiniteSource) Next(ctx context.Context) ([]byte, voice.AudioFormat, error) {
	select {
	case <-ctx.Done():
		return nil, "", ctx.Err()
	case <-time.After(5 * time.Millisecond):
		return wavTone(), voice.FormatWAV, nil
	}
}

// wavTone returns a tiny valid 16-bit WAV with non-zero samples, enough for
// the speech gate.
func wavTone() []byte {
	const samples = 1600 // 100ms @ 16kHz
	out := make([]byte, 44+samples*2)
	copy(out[0:4], "RIFF")
	putU32(out[4:8], uint32(36+samples*2))
	copy(out[8:12], "WAVE")
	copy(out[12:16], "fmt ")
	putU32(out[16:20], 16)
	putU16(out[20:22], 1)
	putU16(out[22:24], 1)
	putU32(out[24:28], 16000)
	putU32(out[28:32], 32000)
	putU16(out[32:34], 2)
	putU16(out[34:36], 16)
	copy(out[36:40], "data")
	putU32(out[40:44], uint32(samples*2))
	for i := 0; i < samples; i++ {
		putU16(out[44+i*2:], uint16(int16(2000)))
	}
	return out
}

func putU16(b []byte, v uint16) { b[0], b[1] = byte(v), byte(v>>8) }
func putU32(b []byte, v uint32) {
	b[0], b[1], b[2], b[3] = byte(v), byte(v>>8), byte(v>>16), byte(v>>24)
}
