package cli

import (
	"fmt"
	"io"
)

// CmdVoice handles the "voice" command and its subcommands.
func CmdVoice(app *App, args []string, w io.Writer) error {
	if len(args) == 0 || args[0] == "status" {
		return cmdVoiceStatus(app, w)
	}
	switch args[0] {
	case "providers":
		return cmdVoiceProviders(w)
	default:
		return fmt.Errorf("unknown voice subcommand: %s", args[0])
	}
}

func cmdVoiceStatus(app *App, w io.Writer) error {
	v := app.Config.Configurations.Voice

	fmt.Fprintln(w, "=== Voice Status ===")
	fmt.Fprintf(w, "  STT Provider:     %s\n", v.STTProvider)
	fmt.Fprintf(w, "  TTS Provider:     %s\n", v.TTSProvider)

	// Show voice response mode from channel configs.
	voiceMode := "text"
	if chCfg, ok := app.Config.Configurations.Channels["webchat"]; ok && chCfg != nil {
		if chCfg.VoiceResponse != "" {
			voiceMode = chCfg.VoiceResponse
		}
	}
	fmt.Fprintf(w, "  Voice Response:   %s\n", voiceMode)

	opts := app.Config.Options.Voice
	fmt.Fprintf(w, "  Max Audio Size:   %dMB\n", opts.MaxAudioSizeMB)
	fmt.Fprintf(w, "  STT Timeout:      %s\n", opts.STTTimeout)

	return nil
}

func cmdVoiceProviders(w io.Writer) error {
	fmt.Fprintln(w, "=== STT Providers ===")
	fmt.Fprintln(w, "  - whisper (default)")
	fmt.Fprintln(w, "  - stub")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "=== TTS Providers ===")
	fmt.Fprintln(w, "  - stub (default)")
	return nil
}
