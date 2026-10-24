package voice

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// SystemTTSProvider uses the operating system's built-in speech engine:
// `say` on macOS, `espeak-ng`/`espeak` on Linux, and System.Speech via
// PowerShell on Windows. It is fully offline and needs no model download.
type SystemTTSProvider struct {
	voice   string
	timeout time.Duration
}

// NewSystemTTSProvider creates a native TTS provider. voice is optional
// (macOS: "Samantha"; Linux: "en").
func NewSystemTTSProvider(voice string) *SystemTTSProvider {
	return &SystemTTSProvider{voice: voice, timeout: 60 * time.Second}
}

// Available reports whether the platform's speech binary exists.
func (p *SystemTTSProvider) Available() bool {
	switch runtime.GOOS {
	case "darwin":
		_, err := exec.LookPath("say")
		return err == nil
	case "linux":
		if _, err := exec.LookPath("espeak-ng"); err == nil {
			return true
		}
		_, err := exec.LookPath("espeak")
		return err == nil
	case "windows":
		_, err := exec.LookPath("powershell")
		return err == nil
	}
	return false
}

// Synthesize renders text to a WAV buffer using the OS engine.
func (p *SystemTTSProvider) Synthesize(ctx context.Context, text, lang string) (*SynthesizeResult, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, fmt.Errorf("voice: empty text")
	}
	if !p.Available() {
		return nil, fmt.Errorf("voice: no system speech engine available on %s", runtime.GOOS)
	}

	dir, err := os.MkdirTemp("", "aibutler-tts-*")
	if err != nil {
		return nil, fmt.Errorf("voice: TTS temp dir: %w", err)
	}
	defer os.RemoveAll(dir)
	out := filepath.Join(dir, "speech.wav")

	runCtx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()

	switch runtime.GOOS {
	case "darwin":
		args := []string{"-o", out, "--data-format=LEI16@22050"}
		if p.voice != "" {
			args = append(args, "-v", p.voice)
		}
		args = append(args, text)
		if err := exec.CommandContext(runCtx, "say", args...).Run(); err != nil {
			return nil, fmt.Errorf("voice: say failed: %w", err)
		}
	case "linux":
		bin := "espeak-ng"
		if _, err := exec.LookPath(bin); err != nil {
			bin = "espeak"
		}
		args := []string{"-w", out}
		voice := p.voice
		if voice == "" {
			voice = lang
		}
		if voice != "" {
			args = append(args, "-v", voice)
		}
		args = append(args, text)
		if err := exec.CommandContext(runCtx, bin, args...).Run(); err != nil {
			return nil, fmt.Errorf("voice: espeak failed: %w", err)
		}
	case "windows":
		if err := synthesizeWindows(runCtx, dir, out, text); err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("voice: system TTS unsupported on %s", runtime.GOOS)
	}

	data, err := os.ReadFile(out)
	if err != nil {
		return nil, fmt.Errorf("voice: read TTS output: %w", err)
	}
	if len(data) < 44 {
		return nil, fmt.Errorf("voice: system TTS produced no audio")
	}
	return &SynthesizeResult{
		Data:     data,
		Format:   FormatWAV,
		Duration: wavDuration(data),
	}, nil
}

// synthesizeWindows uses System.Speech from a temp script so the spoken text
// is read from a file rather than interpolated into PowerShell code.
func synthesizeWindows(ctx context.Context, dir, out, text string) error {
	textPath := filepath.Join(dir, "speech.txt")
	if err := os.WriteFile(textPath, []byte(text), 0o600); err != nil {
		return fmt.Errorf("voice: write TTS text: %w", err)
	}
	scriptPath := filepath.Join(dir, "speak.ps1")
	script := `Add-Type -AssemblyName System.Speech
$s = New-Object System.Speech.Synthesis.SpeechSynthesizer
$s.SetOutputToWaveFile('` + strings.ReplaceAll(out, "'", "''") + `')
$s.Speak([IO.File]::ReadAllText('` + strings.ReplaceAll(textPath, "'", "''") + `'))
$s.Dispose()
`
	if err := os.WriteFile(scriptPath, []byte(script), 0o600); err != nil {
		return fmt.Errorf("voice: write TTS script: %w", err)
	}
	cmd := exec.CommandContext(ctx, "powershell", "-NoProfile", "-NonInteractive",
		"-ExecutionPolicy", "Bypass", "-File", scriptPath)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("voice: PowerShell TTS failed: %w (output: %s)", err, truncateForError(string(out)))
	}
	return nil
}
