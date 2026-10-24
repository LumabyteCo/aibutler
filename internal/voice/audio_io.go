package voice

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// Recorder captures a bounded audio window from the default microphone.
type Recorder interface {
	Record(ctx context.Context, max time.Duration) ([]byte, AudioFormat, error)
	Available() bool
	Name() string
}

// SystemRecorder uses sox (preferred; can trim trailing silence) or arecord.
type SystemRecorder struct {
	kind string // "sox", "arecord", or "" when unavailable
}

// NewSystemRecorder detects an installed recorder.
func NewSystemRecorder() *SystemRecorder {
	if _, err := exec.LookPath("sox"); err == nil {
		return &SystemRecorder{kind: "sox"}
	}
	if _, err := exec.LookPath("arecord"); err == nil {
		return &SystemRecorder{kind: "arecord"}
	}
	return &SystemRecorder{}
}

func (r *SystemRecorder) Available() bool { return r != nil && r.kind != "" }
func (r *SystemRecorder) Name() string    { return r.kind }

// Record captures at most max of 16kHz mono PCM and returns a WAV buffer.
// sox is told to stop after 1.2s of trailing silence, so command capture
// does not always wait the full window.
func (r *SystemRecorder) Record(ctx context.Context, max time.Duration) ([]byte, AudioFormat, error) {
	if !r.Available() {
		return nil, "", fmt.Errorf("voice: no microphone recorder — install sox (macOS/Linux) or arecord (ALSA)")
	}
	if max <= 0 {
		max = 6 * time.Second
	}
	secs := int(max.Seconds())
	if secs < 1 {
		secs = 1
	}

	var args []string
	switch r.kind {
	case "sox":
		args = []string{
			"-q", "-d",
			"-r", "16000", "-c", "1", "-b", "16", "-e", "signed-integer",
			"-t", "wav", "-",
			"trim", "0", fmt.Sprintf("%d", secs),
			"silence", "1", "0.1", "3%", "1", "1.2", "3%",
		}
	case "arecord":
		args = []string{
			"-q", "-f", "S16_LE", "-r", "16000", "-c", "1",
			"-d", fmt.Sprintf("%d", secs), "-t", "wav",
		}
	default:
		return nil, "", fmt.Errorf("voice: unsupported recorder %q", r.kind)
	}

	runCtx, cancel := context.WithTimeout(ctx, max+10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(runCtx, r.kind, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, "", fmt.Errorf("voice: %s capture failed: %w (%s)", r.kind, err, truncateForError(stderr.String()))
	}
	if stdout.Len() < 44 {
		return nil, "", fmt.Errorf("voice: %s captured no audio (%s)", r.kind, truncateForError(stderr.String()))
	}
	return stdout.Bytes(), FormatWAV, nil
}

// Player plays synthesized audio through the OS mixer.
type Player interface {
	Play(ctx context.Context, data []byte, format AudioFormat) error
	Available() bool
}

// SystemPlayer plays audio with afplay (macOS), aplay/paplay (Linux), or
// PowerShell's SoundPlayer (Windows).
type SystemPlayer struct{}

// NewSystemPlayer returns the platform player.
func NewSystemPlayer() *SystemPlayer { return &SystemPlayer{} }

// Available reports whether a supported player exists.
func (p *SystemPlayer) Available() bool {
	switch runtime.GOOS {
	case "darwin":
		_, err := exec.LookPath("afplay")
		return err == nil
	case "linux":
		if _, err := exec.LookPath("aplay"); err == nil {
			return true
		}
		_, err := exec.LookPath("paplay")
		return err == nil
	case "windows":
		_, err := exec.LookPath("powershell")
		return err == nil
	}
	return false
}

// Play writes the audio to a temp file and invokes the platform player.
func (p *SystemPlayer) Play(ctx context.Context, data []byte, format AudioFormat) error {
	if len(data) == 0 {
		return fmt.Errorf("voice: empty audio")
	}
	if !p.Available() {
		return fmt.Errorf("voice: no audio player available on %s", runtime.GOOS)
	}
	ext := strings.TrimPrefix(string(format), ".")
	if ext == "" {
		ext = "wav"
	}
	f, err := os.CreateTemp("", "aibutler-play-*."+ext)
	if err != nil {
		return fmt.Errorf("voice: playback temp file: %w", err)
	}
	path := f.Name()
	defer os.Remove(path)
	if _, err := f.Write(data); err != nil {
		f.Close()
		return fmt.Errorf("voice: write playback file: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("voice: close playback file: %w", err)
	}

	runCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.CommandContext(runCtx, "afplay", path)
	case "linux":
		if _, err := exec.LookPath("aplay"); err == nil {
			cmd = exec.CommandContext(runCtx, "aplay", "-q", path)
		} else {
			cmd = exec.CommandContext(runCtx, "paplay", path)
		}
	case "windows":
		script := fmt.Sprintf(`(New-Object Media.SoundPlayer '%s').PlaySync()`, strings.ReplaceAll(path, "'", "''"))
		cmd = exec.CommandContext(runCtx, "powershell", "-NoProfile", "-NonInteractive", "-Command", script)
	default:
		return fmt.Errorf("voice: playback unsupported on %s", runtime.GOOS)
	}
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("voice: playback failed: %w (%s)", err, truncateForError(string(out)))
	}
	return nil
}
