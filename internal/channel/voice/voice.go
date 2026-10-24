// Package voicechannel exposes the local voice loop as a normal Butler
// channel. Transcribed commands go through the same message router (and
// therefore the same hybrid local/cloud router) as webchat or Telegram.
package voicechannel

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/LumabyteCo/aibutler/internal/channel"
	"github.com/LumabyteCo/aibutler/internal/voice"
)

const maxSpokenRunes = 600

// Adapter is a channel.Channel backed by the wake-word loop.
type Adapter struct {
	loop   *voice.Loop
	tts    voice.TTSProvider
	player voice.Player

	mu     sync.Mutex
	cancel context.CancelFunc
	done   chan struct{}
}

// New creates the voice channel adapter.
func New(loop *voice.Loop, tts voice.TTSProvider, player voice.Player) *Adapter {
	return &Adapter{loop: loop, tts: tts, player: player}
}

// Name implements channel.Channel.
func (a *Adapter) Name() string { return "voice" }

// Start launches the wake-word loop and routes transcripts to handler.
func (a *Adapter) Start(ctx context.Context, handler channel.MessageHandler) error {
	if a.loop == nil {
		return fmt.Errorf("voice channel: loop is nil")
	}
	if a.loop.Recorder == nil || !a.loop.Recorder.Available() {
		return fmt.Errorf("voice channel: no microphone recorder — install sox or arecord")
	}

	loopCtx, cancel := context.WithCancel(ctx)
	a.mu.Lock()
	a.cancel = cancel
	a.done = make(chan struct{})
	done := a.done
	a.mu.Unlock()

	a.loop.Handle = func(hctx context.Context, text string) (string, error) {
		// The router's Send path speaks the reply; returning "" prevents
		// the loop from speaking it twice.
		return "", handler(hctx, channel.Envelope{
			ID:        fmt.Sprintf("voice-%d", time.Now().UnixNano()),
			Channel:   "voice",
			AccountID: "local",
			Type:      channel.TypeVoice,
			Text:      text,
			Timestamp: time.Now(),
		})
	}

	go func() {
		defer close(done)
		if err := a.loop.Run(loopCtx); err != nil && loopCtx.Err() == nil {
			// Surface the real reason in the run log; there is no caller
			// to return it to once Start has returned.
			log.Printf("voice channel stopped: %v", err)
		}
	}()
	return nil
}

// Send speaks the router's reply.
func (a *Adapter) Send(ctx context.Context, _ string, msg channel.OutgoingMessage) error {
	text := strings.TrimSpace(msg.Text)
	if text == "" {
		return nil
	}
	runes := []rune(text)
	if len(runes) > maxSpokenRunes {
		text = string(runes[:maxSpokenRunes]) + "…"
	}
	if a.tts == nil || a.player == nil {
		return fmt.Errorf("voice channel: TTS or player is not configured")
	}
	out, err := a.tts.Synthesize(ctx, text, "")
	if err != nil {
		return fmt.Errorf("voice channel: synthesize: %w", err)
	}
	if err := a.player.Play(ctx, out.Data, out.Format); err != nil {
		return fmt.Errorf("voice channel: play: %w", err)
	}
	return nil
}

// SendTyping is a no-op for audio.
func (a *Adapter) SendTyping(context.Context, string) error { return nil }

// Stop cancels the listening loop.
func (a *Adapter) Stop(context.Context) error {
	a.mu.Lock()
	cancel := a.cancel
	done := a.done
	a.cancel = nil
	a.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if done != nil {
		select {
		case <-done:
		case <-time.After(5 * time.Second):
		}
	}
	return nil
}
