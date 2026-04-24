package streaming

import (
	"github.com/LumabyteCo/aibutler/internal/agent"
)

// StreamConfig holds streaming pipeline tuning parameters.
type StreamConfig struct {
	BufferSize int // channel buffer size for StreamEvent (default 64)
}

// DefaultStreamConfig returns sensible defaults for streaming.
func DefaultStreamConfig() StreamConfig {
	return StreamConfig{
		BufferSize: 64,
	}
}

// NewEventChannel creates a buffered StreamEvent channel with the configured size.
func NewEventChannel(cfg StreamConfig) chan agent.StreamEvent {
	if cfg.BufferSize <= 0 {
		cfg.BufferSize = 64
	}
	return make(chan agent.StreamEvent, cfg.BufferSize)
}

// BackpressureRelay reads events from src and writes to dst.
// If the consumer is slow (dst full), ping events are dropped but
// content, tool, and error events are never dropped.
func BackpressureRelay(src <-chan agent.StreamEvent, dst chan<- agent.StreamEvent, done <-chan struct{}) {
	defer close(dst)

	for {
		select {
		case event, ok := <-src:
			if !ok {
				return
			}
			if isPingEvent(event) {
				// Try non-blocking send; drop ping if consumer is slow.
				select {
				case dst <- event:
				default:
					// Ping dropped due to backpressure.
				}
			} else {
				// Content/tool/error events are never dropped; block until sent.
				select {
				case dst <- event:
				case <-done:
					return
				}
			}
		case <-done:
			return
		}
	}
}

// isPingEvent returns true if the event is a keep-alive ping.
func isPingEvent(e agent.StreamEvent) bool {
	return e.Type == "ping"
}
