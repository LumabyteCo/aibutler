package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/LumabyteCo/aibutler/internal/voice"
)

// CmdVoice handles the "voice" command and its subcommands.
func CmdVoice(app *App, args []string, w io.Writer) error {
	if len(args) == 0 || args[0] == "status" {
		return cmdVoiceStatus(app, w)
	}
	switch args[0] {
	case "providers":
		return cmdVoiceProviders(w)
	case "test":
		return cmdVoiceTest(app, args[1:], w)
	case "speak":
		return cmdVoiceSpeak(app, args[1:], w)
	case "simulate":
		return cmdVoiceSimulate(app, args[1:], w)
	default:
		return fmt.Errorf("unknown voice subcommand: %s (want status, providers, test, speak, or simulate)", args[0])
	}
}

func cmdVoiceStatus(app *App, w io.Writer) error {
	v := app.Config.Configurations.Voice

	fmt.Fprintln(w, "=== Voice Status ===")
	fmt.Fprintf(w, "  STT Provider:     %s\n", v.STTProvider)
	fmt.Fprintf(w, "  TTS Provider:     %s\n", v.TTSProvider)
	if v.STTProvider == "local" || v.STTProvider == "openai_compat" {
		endpoint := v.STTBaseURL
		if endpoint == "" {
			endpoint = "(not set — voice.stt_base_url)"
		}
		fmt.Fprintf(w, "  STT Endpoint:     %s\n", endpoint)
	}
	if v.STTProvider == "command" {
		if len(v.STTCommand) == 0 {
			fmt.Fprintln(w, "  STT Command:      (not set — voice.stt_command)")
		} else {
			fmt.Fprintf(w, "  STT Command:      %s\n", strings.Join(v.STTCommand, " "))
			if _, err := exec.LookPath(v.STTCommand[0]); err != nil {
				if !strings.ContainsRune(v.STTCommand[0], os.PathSeparator) {
					fmt.Fprintf(w, "                    ⚠ %q not found in PATH\n", v.STTCommand[0])
				}
			}
		}
	}
	if v.TTSProvider == "system" {
		sys := voice.NewSystemTTSProvider(v.TTSVoice)
		fmt.Fprintf(w, "  System TTS:       %s\n", boolStr(sys.Available(), "available", "unavailable (no OS speech engine)"))
	}

	// Show voice response mode from channel configs.
	voiceMode := "text"
	if chCfg, ok := app.Config.Configurations.Channels["webchat"]; ok && chCfg != nil {
		if chCfg.VoiceResponse != "" {
			voiceMode = chCfg.VoiceResponse
		}
	}
	fmt.Fprintf(w, "  Voice Response:   %s\n", voiceMode)
	fmt.Fprintf(w, "  Wake Word:        %q (%s)\n", defaultWakeWord(v.WakeWord), boolStr(v.WakeEnabled, "enabled", "disabled"))

	rec := voice.NewSystemRecorder()
	fmt.Fprintf(w, "  Microphone:       %s\n", boolStr(rec.Available(), "recorder "+rec.Name(), "unavailable (install sox or arecord)"))
	player := voice.NewSystemPlayer()
	fmt.Fprintf(w, "  Playback:         %s\n", boolStr(player.Available(), "available", "unavailable"))

	opts := app.Config.Options.Voice
	fmt.Fprintf(w, "  Max Audio Size:   %dMB\n", opts.MaxAudioSizeMB)
	fmt.Fprintf(w, "  STT Timeout:      %s\n", opts.STTTimeout)
	return nil
}

func cmdVoiceProviders(w io.Writer) error {
	fmt.Fprintln(w, "=== STT Providers ===")
	fmt.Fprintln(w, "  - whisper   OpenAI Whisper API (cloud)")
	fmt.Fprintln(w, "  - local     OpenAI-compatible endpoint (whisper.cpp server, faster-whisper)")
	fmt.Fprintln(w, "  - command   Local binary (whisper.cpp, Python whisper) — fully offline")
	fmt.Fprintln(w, "  - deepgram  Deepgram API (cloud)")
	fmt.Fprintln(w, "  - stub      Canned text for tests")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "=== TTS Providers ===")
	fmt.Fprintln(w, "  - system      macOS say / Linux espeak-ng / Windows SAPI — fully offline")
	fmt.Fprintln(w, "  - edge        Microsoft Edge TTS (network)")
	fmt.Fprintln(w, "  - stub        Placeholder audio for tests")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Voice channel: set active_channels to include \"voice\",")
	fmt.Fprintln(w, "voice.wake_enabled: true, and a local STT provider.")
	return nil
}

func cmdVoiceTest(app *App, args []string, w io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: aibutler voice test <audio-file>")
	}
	data, err := os.ReadFile(args[0])
	if err != nil {
		return fmt.Errorf("voice test: %w", err)
	}
	format := voiceFormatFor(args[0])
	start := time.Now()
	res, err := app.Voice.ProcessVoiceInput(context.Background(), data, format)
	if err != nil {
		return fmt.Errorf("voice test: %w", err)
	}
	fmt.Fprintf(w, "STT provider: %s\n", app.Config.Configurations.Voice.STTProvider)
	fmt.Fprintf(w, "Transcript:   %s\n", res.Text)
	if res.Language != "" {
		fmt.Fprintf(w, "Language:     %s\n", res.Language)
	}
	if res.Duration > 0 {
		fmt.Fprintf(w, "Audio:        %s\n", res.Duration.Round(time.Millisecond))
	}
	fmt.Fprintf(w, "Elapsed:      %s\n", time.Since(start).Round(time.Millisecond))
	return nil
}

func cmdVoiceSpeak(app *App, args []string, w io.Writer) error {
	var outPath string
	play := false
	var words []string
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--out":
			if i+1 >= len(args) {
				return fmt.Errorf("voice speak: --out requires a path")
			}
			i++
			outPath = args[i]
		case "--play":
			play = true
		default:
			words = append(words, args[i])
		}
	}
	text := strings.TrimSpace(strings.Join(words, " "))
	if text == "" {
		return fmt.Errorf("usage: aibutler voice speak <text> [--out file.wav] [--play]")
	}
	res, err := app.Voice.Synthesize(context.Background(), text, "")
	if err != nil {
		return fmt.Errorf("voice speak: %w", err)
	}
	if outPath == "" {
		f, err := os.CreateTemp("", "aibutler-speak-*.wav")
		if err != nil {
			return fmt.Errorf("voice speak: %w", err)
		}
		outPath = f.Name()
		f.Close()
	}
	if err := os.WriteFile(outPath, res.Data, 0o600); err != nil {
		return fmt.Errorf("voice speak: write %s: %w", outPath, err)
	}
	fmt.Fprintf(w, "Wrote %s (%s, %s)\n", outPath, byteCount(len(res.Data)), res.Duration.Round(time.Millisecond))
	if play {
		if err := voice.NewSystemPlayer().Play(context.Background(), res.Data, res.Format); err != nil {
			return fmt.Errorf("voice speak: play: %w", err)
		}
		fmt.Fprintln(w, "Played.")
	}
	return nil
}

// cmdVoiceSimulate runs transcription + wake-word matching on a file. It is
// the hardware-free way to validate the offline wake pipeline end to end.
func cmdVoiceSimulate(app *App, args []string, w io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: aibutler voice simulate <audio-file>")
	}
	data, err := os.ReadFile(args[0])
	if err != nil {
		return fmt.Errorf("voice simulate: %w", err)
	}
	res, err := app.Voice.ProcessVoiceInput(context.Background(), data, voiceFormatFor(args[0]))
	if err != nil {
		return fmt.Errorf("voice simulate: %w", err)
	}
	keyword := defaultWakeWord(app.Config.Configurations.Voice.WakeWord)
	matched := voice.MatchWakeWord(res.Text, keyword)
	fmt.Fprintf(w, "Transcript: %s\n", res.Text)
	fmt.Fprintf(w, "Wake word:  %q -> %s\n", keyword, boolStr(matched, "MATCHED", "not matched"))
	return nil
}

func defaultWakeWord(s string) string {
	if strings.TrimSpace(s) == "" {
		return "hey butler"
	}
	return s
}

func voiceFormatFor(path string) voice.AudioFormat {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".mp3":
		return voice.FormatMP3
	case ".ogg", ".oga":
		return voice.FormatOGG
	case ".webm":
		return voice.FormatWebM
	case ".flac":
		return voice.FormatFLAC
	default:
		return voice.FormatWAV
	}
}

func byteCount(n int) string {
	if n >= 1024*1024 {
		return fmt.Sprintf("%.1fMB", float64(n)/(1024*1024))
	}
	if n >= 1024 {
		return fmt.Sprintf("%.1fKB", float64(n)/1024)
	}
	return fmt.Sprintf("%dB", n)
}
