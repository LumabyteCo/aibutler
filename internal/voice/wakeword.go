package voice

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"
)

// AudioSegmentSource yields short raw audio windows for wake-word
// scanning. RecorderSource adapts a microphone Recorder to this interface.
type AudioSegmentSource interface {
	Next(ctx context.Context) ([]byte, AudioFormat, error)
}

// RecorderSource repeatedly records fixed windows from a Recorder.
type RecorderSource struct {
	Recorder Recorder
	Segment  time.Duration
}

// Next records one segment.
func (s *RecorderSource) Next(ctx context.Context) ([]byte, AudioFormat, error) {
	if s == nil || s.Recorder == nil {
		return nil, "", fmt.Errorf("wake word: no recorder configured")
	}
	d := s.Segment
	if d <= 0 {
		d = 2 * time.Second
	}
	return s.Recorder.Record(ctx, d)
}

// WakeWordDetector scans microphone segments with a LOCAL speech-to-text
// provider and fires when the configured phrase is heard. No audio leaves
// the machine and no native wake-word library is required — the trade-off
// is higher latency (one STT pass per speech segment).
type WakeWordDetector struct {
	keyword string

	stt     STTProvider
	source  AudioSegmentSource
	minRMS  float64
	ch      chan struct{}
	startMu sync.Mutex
}

// NewWakeWordDetector creates a detector for keyword (default "hey butler").
// Configure it with a local STT provider and audio source before use.
func NewWakeWordDetector(keyword string) *WakeWordDetector {
	if strings.TrimSpace(keyword) == "" {
		keyword = "hey butler"
	}
	return &WakeWordDetector{
		keyword: keyword,
		minRMS:  250,
		ch:      make(chan struct{}, 1),
	}
}

// Keyword returns the configured wake phrase.
func (w *WakeWordDetector) Keyword() string { return w.keyword }

// Configure attaches the STT provider and segment source.
func (w *WakeWordDetector) Configure(stt STTProvider, source AudioSegmentSource) {
	w.stt = stt
	w.source = source
}

// Configured reports whether the detector has what it needs to run.
func (w *WakeWordDetector) Configured() bool {
	return w != nil && w.stt != nil && w.source != nil
}

// SetMinRMS tunes the voice-activity gate (16-bit PCM amplitude).
func (w *WakeWordDetector) SetMinRMS(v float64) {
	if v > 0 {
		w.minRMS = v
	}
}

// WaitForWake blocks until the keyword is heard or ctx is canceled. It
// returns the transcript that matched. Silence is skipped before STT, so
// an idle room costs no model calls.
func (w *WakeWordDetector) WaitForWake(ctx context.Context) (string, error) {
	if !w.Configured() {
		return "", fmt.Errorf("wake word: local STT provider and audio source are required (set voice.stt_provider to local or command)")
	}
	for {
		audio, format, err := w.source.Next(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return "", ctx.Err()
			}
			return "", fmt.Errorf("wake word: capture: %w", err)
		}
		if !HasSpeech(audio, w.minRMS) {
			continue
		}
		res, err := w.stt.Transcribe(ctx, audio, format)
		if err != nil {
			log.Printf("wake word: transcribe: %v", err)
			continue
		}
		if MatchWakeWord(res.Text, w.keyword) {
			return res.Text, nil
		}
	}
}

// Start runs the detector until ctx ends, signaling Detected on each wake.
// Callers that drive command capture directly should use WaitForWake so the
// detector does not listen to the assistant's own TTS output.
func (w *WakeWordDetector) Start(ctx context.Context) error {
	for {
		if _, err := w.WaitForWake(ctx); err != nil {
			return err
		}
		select {
		case w.ch <- struct{}{}:
		default: // a pending signal already exists
		}
	}
}

// Detected signals wake-word hits for Start-based callers.
func (w *WakeWordDetector) Detected() <-chan struct{} { return w.ch }
