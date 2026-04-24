package voice

import (
	"context"
	"fmt"
)

// WakeWordDetector listens for a wake word to activate voice interaction.
// This is a stub implementation — real wake word detection requires a native
// companion app using Porcupine/Picovoice or similar.
type WakeWordDetector struct {
	keyword string
	ch      chan struct{}
}

// NewWakeWordDetector creates a wake word detector for the given keyword.
func NewWakeWordDetector(keyword string) *WakeWordDetector {
	if keyword == "" {
		keyword = "hey butler"
	}
	return &WakeWordDetector{
		keyword: keyword,
		ch:      make(chan struct{}),
	}
}

// Keyword returns the configured wake word.
func (w *WakeWordDetector) Keyword() string {
	return w.keyword
}

// Start begins listening for the wake word.
// Returns an error because Porcupine/Picovoice is not available.
func (w *WakeWordDetector) Start(_ context.Context) error {
	return fmt.Errorf("wake word detection not available — Porcupine/Picovoice requires native companion app; keyword=%q", w.keyword)
}

// Detected returns a channel that signals when the wake word is detected.
// In this stub, the channel is never written to.
func (w *WakeWordDetector) Detected() <-chan struct{} {
	return w.ch
}
