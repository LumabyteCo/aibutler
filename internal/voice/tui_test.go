package voice_test

import (
	"context"
	"strings"
	"testing"

	"github.com/LumabyteCo/aibutler/internal/voice"
)

func TestTUIMode_Unavailable(t *testing.T) {
	// Create TUI mode with a nil pipeline (won't matter since tools won't be found).
	tui := voice.NewTUIMode(nil)

	// In CI/test environments, arecord/sox are typically not installed.
	// Either way, Start should return an error (either "not available" or "not yet implemented").
	err := tui.Start(context.Background())
	if err == nil {
		t.Fatal("expected error from TUI Start, got nil")
	}
	// The error should mention either unavailability or not-yet-implemented.
	errStr := err.Error()
	if !strings.Contains(errStr, "not available") && !strings.Contains(errStr, "not yet implemented") {
		t.Errorf("expected TUI unavailability error, got: %v", err)
	}
}

func TestWakeWordDetector_Stub(t *testing.T) {
	detector := voice.NewWakeWordDetector("hey butler")

	if detector.Keyword() != "hey butler" {
		t.Errorf("expected keyword='hey butler', got %s", detector.Keyword())
	}

	err := detector.Start(context.Background())
	if err == nil {
		t.Fatal("expected error from wake word Start, got nil")
	}
	if !strings.Contains(err.Error(), "not available") {
		t.Errorf("expected not-available error, got: %v", err)
	}

	// Detected channel should exist but never fire.
	ch := detector.Detected()
	if ch == nil {
		t.Fatal("expected non-nil Detected channel")
	}
}
