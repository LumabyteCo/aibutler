package cli

import (
	"fmt"
	"io"
)

// CmdConfig handles the "config" command and its subcommands.
func CmdConfig(app *App, args []string, w io.Writer) error {
	if len(args) == 0 || args[0] == "show" {
		return cmdConfigShow(app, w)
	}
	return fmt.Errorf("unknown config subcommand: %s", args[0])
}

func cmdConfigShow(app *App, w io.Writer) error {
	c := app.Config
	s := c.Settings

	fmt.Fprintln(w, "=== Settings ===")
	fmt.Fprintf(w, "  Language:         %s\n", s.Language)
	fmt.Fprintf(w, "  Timezone:         %s\n", s.Timezone)
	fmt.Fprintf(w, "  Model:            %s\n", s.Model)
	fmt.Fprintf(w, "  Persona:          %s\n", s.PersonaName)
	fmt.Fprintf(w, "  Agent Mode:       %s\n", s.AgentMode)
	fmt.Fprintf(w, "  Cost Strategy:    %s\n", s.Cost.Strategy)
	fmt.Fprintf(w, "  Monthly Budget:   $%.2f\n", s.Cost.MonthlyBudget)
	fmt.Fprintf(w, "  Channels:         %v\n", s.ActiveChannels)

	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "=== Configurations ===")
	fmt.Fprintf(w, "  Schedule:         %s\n", boolStr(c.Configurations.Schedule.Enabled, "enabled", "disabled"))
	fmt.Fprintf(w, "  IoT Adapter:      %s\n", c.Configurations.IoT.Adapter)
	fmt.Fprintf(w, "  Voice STT:        %s\n", c.Configurations.Voice.STTProvider)
	fmt.Fprintf(w, "  Voice TTS:        %s\n", c.Configurations.Voice.TTSProvider)

	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "=== Options ===")
	fmt.Fprintf(w, "  Schedule Interval: %s\n", c.Options.Schedule.TickInterval)
	fmt.Fprintf(w, "  Max Concurrent:    %d\n", c.Options.Schedule.MaxConcurrent)
	fmt.Fprintf(w, "  Voice Max Audio:   %dMB\n", c.Options.Voice.MaxAudioSizeMB)

	return nil
}

func boolStr(b bool, ifTrue, ifFalse string) string {
	if b {
		return ifTrue
	}
	return ifFalse
}
