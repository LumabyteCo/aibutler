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
	case "auto", "single", "multi", "swarm", "custom", "mission":
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
		case "swarm":
			fmt.Fprintln(w, "  (multi execution + the agent.swarm orchestration tool)")
			fmt.Fprintln(w, "  Restart `aibutler run` to apply.")
		case "custom":
			fmt.Fprintln(w, "  (custom role routing via configurations.agents.custom_roles)")
			fmt.Fprintln(w, "  Restart `aibutler run` to build the role router.")
		case "mission":
			fmt.Fprintln(w, "  Mission runtime will start on next `aibutler run`.")
			fmt.Fprintln(w, "  Create missions via the mission.create tool; the runtime drives them.")
		}
		return nil
	default:
		return fmt.Errorf("unknown mode: %s (valid: auto, single, multi, swarm, custom, mission)", newMode)
	}
}
