package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/LumabyteCo/aibutler/internal/config"
)

// CmdMode handles the "mode" command for switching agent modes.
func CmdMode(app *App, args []string, w io.Writer) error {
	if len(args) == 0 {
		// Show current mode.
		mode := app.Config.Settings.AgentMode
		if mode == "" {
			mode = "auto"
		}
		fmt.Fprintf(w, "Current agent mode: %s\n", mode)
		switch mode {
		case "auto":
			fmt.Fprintln(w, "  (resolves to multi at runtime: parallel tool execution)")
		case "multi":
			fmt.Fprintln(w, "  (parallel tool execution — the runtime behavior of auto)")
		case "mission":
			fmt.Fprintln(w, "  (mission runtime starts on next `aibutler run`)")
		}
		return nil
	}

	newMode := strings.ToLower(strings.TrimSpace(args[0]))
	switch newMode {
	case "auto", "single", "multi", "mission":
		app.Config.Settings.AgentMode = newMode
		if err := config.UpdateAgentMode(app.configPath, newMode); err != nil {
			return fmt.Errorf("mode: save config: %w", err)
		}
		fmt.Fprintf(w, "Agent mode switched to: %s\n", newMode)
		switch newMode {
		case "auto":
			fmt.Fprintln(w, "  (resolves to multi at runtime: parallel tool execution)")
		case "multi":
			fmt.Fprintln(w, "  (parallel tool execution — same as auto's runtime behavior)")
		case "mission":
			fmt.Fprintln(w, "  Mission runtime will start on next `aibutler run`.")
			fmt.Fprintln(w, "  Create missions via the mission.create tool; the runtime drives them.")
		}
		return nil
	case "swarm", "custom":
		// The agent package recognizes these modes, but their CLI/runtime
		// wiring isn't finished. Keep the downgrade, but say so honestly.
		fmt.Fprintf(w, "Mode %q is not available yet, using single mode.\n", newMode)
		app.Config.Settings.AgentMode = "single"
		if err := config.UpdateAgentMode(app.configPath, "single"); err != nil {
			return fmt.Errorf("mode: save config: %w", err)
		}
		return nil
	default:
		return fmt.Errorf("unknown mode: %s (valid: auto, single, multi, mission)", newMode)
	}
}
