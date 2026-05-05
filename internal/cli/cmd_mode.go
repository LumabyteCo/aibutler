package cli

import (
	"fmt"
	"io"
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
		if mode == "auto" {
			fmt.Fprintln(w, "  (behaves as single in v0.1)")
		}
		return nil
	}

	newMode := args[0]
	switch newMode {
	case "auto", "single":
		app.Config.Settings.AgentMode = newMode
		fmt.Fprintf(w, "Agent mode switched to: %s\n", newMode)
		if newMode == "auto" {
			fmt.Fprintln(w, "  (behaves as single in v0.1)")
		}
		return nil
	case "mission":
		// Mission mode runs the mission engine runtime: it polls for
		// planned missions and spawns a supervisor + worker pair for
		// each. The pair drives the mission to a terminal state and
		// then the runtime picks up the next pending mission.
		app.Config.Settings.AgentMode = newMode
		fmt.Fprintf(w, "Agent mode switched to: %s\n", newMode)
		fmt.Fprintln(w, "  Mission runtime will start on next `aibutler run`.")
		fmt.Fprintln(w, "  Create missions via the mission.create tool; the runtime drives them.")
		return nil
	case "multi", "swarm", "custom":
		// Downgrade to single in v0.1.
		fmt.Fprintf(w, "Mode %q is not available in v0.1, using single mode.\n", newMode)
		app.Config.Settings.AgentMode = "single"
		return nil
	default:
		return fmt.Errorf("unknown mode: %s (valid: auto, single, mission)", newMode)
	}
}
