package voice

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"
)

// HandlerFunc processes one spoken command and returns an optional reply.
// The voice channel supplies a handler that submits the transcript to the
// normal message router and returns an empty string (the router's Send path
// speaks the response). Standalone callers can return text and let the
// loop speak it.
type HandlerFunc func(ctx context.Context, text string) (string, error)

// Loop is the wake-word → capture → transcribe → handle → speak cycle.
type Loop struct {
	Wake          *WakeWordDetector
	Recorder      Recorder
	STT           STTProvider
	TTS           TTSProvider
	Player        Player
	Handle        HandlerFunc
	CommandWindow time.Duration
	Ack           bool
	// RetryDelay is how long to wait after a microphone/capture error
	// before listening again. Defaults to 2s.
	RetryDelay time.Duration
}

// Run blocks until ctx is canceled or a fatal capture error occurs.
func (l *Loop) Run(ctx context.Context) error {
	if l == nil || l.Wake == nil {
		return fmt.Errorf("voice loop: wake-word detector is required")
	}
	if l.Recorder == nil {
		return fmt.Errorf("voice loop: microphone recorder is required")
	}
	if l.STT == nil {
		return fmt.Errorf("voice loop: local STT provider is required")
	}
	if l.CommandWindow <= 0 {
		l.CommandWindow = 8 * time.Second
	}
	if !l.Wake.Configured() {
		return fmt.Errorf("voice loop: wake detector needs a local STT provider and audio source")
	}
	if l.RetryDelay <= 0 {
		l.RetryDelay = 2 * time.Second
	}

	for {
		if _, err := l.Wake.WaitForWake(ctx); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			// Microphone hiccups (device busy, permission flap, USB
			// re-enumeration) should not kill the channel.
			log.Printf("voice loop: wake listener: %v (retrying in %s)", err, l.RetryDelay)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(l.RetryDelay):
			}
			continue
		}

		// A short tone tells the user the assistant is listening. The
		// detector is not reading during capture, so the assistant's own
		// speech cannot re-trigger it.
		if l.Ack && l.Player != nil && l.Player.Available() {
			_ = l.Player.Play(ctx, toneWAV(120*time.Millisecond, 880), FormatWAV)
		}

		audio, format, err := l.Recorder.Record(ctx, l.CommandWindow)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			log.Printf("voice loop: capture: %v", err)
			continue
		}
		if !HasSpeech(audio, 250) {
			continue
		}
		res, err := l.STT.Transcribe(ctx, audio, format)
		if err != nil {
			log.Printf("voice loop: transcribe: %v", err)
			continue
		}
		text := strings.TrimSpace(res.Text)
		if text == "" || isDismissal(text) {
			continue
		}
		if l.Handle == nil {
			continue
		}
		reply, err := l.Handle(ctx, text)
		if err != nil {
			log.Printf("voice loop: handle %q: %v", text, err)
			continue
		}
		reply = strings.TrimSpace(reply)
		if reply == "" || l.TTS == nil || l.Player == nil {
			continue
		}
		out, err := l.TTS.Synthesize(ctx, reply, res.Language)
		if err != nil {
			log.Printf("voice loop: synthesize: %v", err)
			continue
		}
		if err := l.Player.Play(ctx, out.Data, out.Format); err != nil {
			log.Printf("voice loop: play: %v", err)
		}
	}
}

func isDismissal(text string) bool {
	t := normalizeSpeech(text)
	switch t {
	case "never mind", "nevermind", "cancel", "stop", "forget it", "not now", "no thanks":
		return true
	}
	return false
}
