package voice

import (
	"context"
	"errors"
	"io"
	"sync"
	"testing"
	"time"
)

// --- fakes ---------------------------------------------------------------

type fakeSource struct {
	mu       sync.Mutex
	segments [][]byte
	i        int
}

func (s *fakeSource) Next(ctx context.Context) ([]byte, AudioFormat, error) {
	select {
	case <-ctx.Done():
		return nil, "", ctx.Err()
	default:
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.i >= len(s.segments) {
		return nil, "", io.EOF
	}
	seg := s.segments[s.i]
	s.i++
	return seg, FormatWAV, nil
}

type infiniteSource struct {
	seg []byte
}

func (s *infiniteSource) Next(ctx context.Context) ([]byte, AudioFormat, error) {
	select {
	case <-ctx.Done():
		return nil, "", ctx.Err()
	case <-time.After(5 * time.Millisecond):
		return s.seg, FormatWAV, nil
	}
}

type fakeSTT struct {
	mu    sync.Mutex
	texts []string
	calls int
}

func (f *fakeSTT) Transcribe(context.Context, []byte, AudioFormat) (*TranscribeResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	i := f.calls
	f.calls++
	if i < len(f.texts) {
		return &TranscribeResult{Text: f.texts[i], Language: "en"}, nil
	}
	return &TranscribeResult{Text: ""}, nil
}

func (f *fakeSTT) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

type fakeRecorder struct {
	data      []byte
	available bool
}

func (r *fakeRecorder) Record(context.Context, time.Duration) ([]byte, AudioFormat, error) {
	return r.data, FormatWAV, nil
}
func (r *fakeRecorder) Available() bool { return r.available }
func (r *fakeRecorder) Name() string    { return "fake" }

type fakeTTS struct{ data []byte }

func (t *fakeTTS) Synthesize(context.Context, string, string) (*SynthesizeResult, error) {
	return &SynthesizeResult{Data: t.data, Format: FormatWAV, Duration: 100 * time.Millisecond}, nil
}

type fakePlayer struct {
	mu    sync.Mutex
	plays int
	ch    chan struct{}
}

func (p *fakePlayer) Play(context.Context, []byte, AudioFormat) error {
	p.mu.Lock()
	p.plays++
	p.mu.Unlock()
	if p.ch != nil {
		select {
		case p.ch <- struct{}{}:
		default:
		}
	}
	return nil
}
func (p *fakePlayer) Available() bool { return true }

// --- wake detector -------------------------------------------------------

func TestWakeDetectorSkipsSilenceAndMatches(t *testing.T) {
	silence := pcm16WAV(make([]int16, 16000), 16000)
	speech := toneWAV(500*time.Millisecond, 440)
	stt := &fakeSTT{texts: []string{"Hey Buggler, turn off the light"}}
	src := &fakeSource{segments: [][]byte{silence, speech}}

	det := NewWakeWordDetector("hey butler")
	det.Configure(stt, src)

	got, err := det.WaitForWake(context.Background())
	if err != nil {
		t.Fatalf("WaitForWake: %v", err)
	}
	if got != "Hey Buggler, turn off the light" {
		t.Errorf("transcript = %q", got)
	}
	if stt.callCount() != 1 {
		t.Errorf("STT calls = %d, want 1 (silence must be skipped)", stt.callCount())
	}
}

func TestWakeDetectorRequiresConfiguration(t *testing.T) {
	det := NewWakeWordDetector("hey butler")
	if _, err := det.WaitForWake(context.Background()); err == nil {
		t.Fatal("unconfigured detector must fail")
	}
}

func TestWakeDetectorHonorsContext(t *testing.T) {
	det := NewWakeWordDetector("hey butler")
	det.Configure(&fakeSTT{}, &blockingSource{})

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err := det.WaitForWake(ctx)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("error = %v, want context deadline", err)
	}
}

type blockingSource struct{}

func (b *blockingSource) Next(ctx context.Context) ([]byte, AudioFormat, error) {
	<-ctx.Done()
	return nil, "", ctx.Err()
}

// --- loop ----------------------------------------------------------------

func TestLoopRoutesTranscriptToHandler(t *testing.T) {
	wakeSrc := &infiniteSource{seg: toneWAV(300*time.Millisecond, 440)}
	stt := &fakeSTT{texts: []string{"hey butler", "turn off the living room light"}}
	det := NewWakeWordDetector("hey butler")
	det.Configure(stt, wakeSrc)

	handled := make(chan string, 1)
	loop := &Loop{
		Wake:     det,
		Recorder: &fakeRecorder{data: toneWAV(500*time.Millisecond, 330), available: true},
		STT:      stt,
		Handle: func(_ context.Context, text string) (string, error) {
			handled <- text
			return "", nil // channel mode: Send speaks the reply
		},
		CommandWindow: 2 * time.Second,
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = loop.Run(ctx) }()

	select {
	case text := <-handled:
		if text != "turn off the living room light" {
			t.Errorf("handled %q", text)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("loop never routed a command")
	}
}

type flakySource struct {
	mu    sync.Mutex
	fails int
	seg   []byte
}

func (s *flakySource) Next(ctx context.Context) ([]byte, AudioFormat, error) {
	select {
	case <-ctx.Done():
		return nil, "", ctx.Err()
	default:
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.fails > 0 {
		s.fails--
		return nil, "", errors.New("microphone blip")
	}
	return s.seg, FormatWAV, nil
}

func TestLoopRetriesCaptureErrors(t *testing.T) {
	src := &flakySource{fails: 2, seg: toneWAV(300*time.Millisecond, 440)}
	stt := &fakeSTT{texts: []string{"hey butler", "status report"}}
	det := NewWakeWordDetector("hey butler")
	det.Configure(stt, src)

	handled := make(chan string, 1)
	loop := &Loop{
		Wake:          det,
		Recorder:      &fakeRecorder{data: toneWAV(300*time.Millisecond, 330), available: true},
		STT:           stt,
		RetryDelay:    10 * time.Millisecond,
		CommandWindow: time.Second,
		Handle: func(_ context.Context, text string) (string, error) {
			handled <- text
			return "", nil
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = loop.Run(ctx) }()

	select {
	case text := <-handled:
		if text != "status report" {
			t.Errorf("handled %q", text)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("loop did not recover from capture errors")
	}
}

func TestLoopSpeaksStandaloneReply(t *testing.T) {
	wakeSrc := &infiniteSource{seg: toneWAV(300*time.Millisecond, 440)}
	stt := &fakeSTT{texts: []string{"hey butler", "lock the door"}}
	det := NewWakeWordDetector("hey butler")
	det.Configure(stt, wakeSrc)

	player := &fakePlayer{ch: make(chan struct{}, 1)}
	loop := &Loop{
		Wake:     det,
		Recorder: &fakeRecorder{data: toneWAV(500*time.Millisecond, 330), available: true},
		STT:      stt,
		TTS:      &fakeTTS{data: toneWAV(200*time.Millisecond, 660)},
		Player:   player,
		Handle: func(context.Context, string) (string, error) {
			return "The door is locked.", nil
		},
		CommandWindow: 2 * time.Second,
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = loop.Run(ctx) }()

	select {
	case <-player.ch:
	case <-time.After(3 * time.Second):
		t.Fatal("standalone reply was never spoken")
	}
}
