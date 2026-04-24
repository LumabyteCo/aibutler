package voice

import (
	"context"
	"fmt"
	"os/exec"
)

// TUIMode provides a terminal voice interface using arecord/sox for mic capture,
// STT for transcription, agent processing, TTS for speech, and audio playback.
// This is opt-in and requires external tools (arecord or sox) to be installed.
type TUIMode struct {
	pipeline  *Pipeline
	recorder  string // "arecord" or "sox" (auto-detected)
	available bool
}

// NewTUIMode creates a TUI voice mode. It checks for arecord/sox availability.
func NewTUIMode(pipeline *Pipeline) *TUIMode {
	t := &TUIMode{pipeline: pipeline}

	// Check for recording tools.
	if path, err := exec.LookPath("arecord"); err == nil && path != "" {
		t.recorder = "arecord"
		t.available = true
	} else if path, err := exec.LookPath("sox"); err == nil && path != "" {
		t.recorder = "sox"
		t.available = true
	}

	return t
}

// Available returns true if the required audio recording tools are installed.
func (t *TUIMode) Available() bool {
	return t.available
}

// Recorder returns the name of the detected recording tool.
func (t *TUIMode) Recorder() string {
	return t.recorder
}

// Start begins the TUI voice interaction loop.
// Returns an error if recording tools are not available.
func (t *TUIMode) Start(_ context.Context) error {
	if !t.available {
		return fmt.Errorf("voice TUI: audio recording tools not available — install 'arecord' (ALSA) or 'sox' to use voice mode")
	}

	// The TUI voice loop would:
	// 1. Record audio from microphone using arecord/sox
	// 2. Send audio to STT pipeline
	// 3. Process transcribed text through the agent
	// 4. Send agent response to TTS pipeline
	// 5. Play audio response
	// This is a capability stub — real implementation requires terminal I/O and audio device access.
	return fmt.Errorf("voice TUI: interactive mode not yet implemented — audio capture requires terminal I/O integration")
}
