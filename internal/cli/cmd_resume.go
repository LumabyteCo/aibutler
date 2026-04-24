package cli

import (
	"context"
	"fmt"
	"io"

	"github.com/LumabyteCo/aibutler/internal/agent"
	"github.com/LumabyteCo/aibutler/internal/session"
)

// CmdResume resumes a previous session from JSONL persistence.
// Usage: aibutler resume [SESSION_ID]
func CmdResume(app *App, args []string, w io.Writer) error {
	if len(args) == 0 {
		// List available sessions to resume.
		persister := session.NewFilePersister(app.dataDir + "/sessions")
		sessions, err := persister.Sessions()
		if err != nil {
			return fmt.Errorf("resume: list sessions: %w", err)
		}
		if len(sessions) == 0 {
			fmt.Fprintln(w, "No sessions available to resume.")
			return nil
		}
		fmt.Fprintln(w, "Available sessions:")
		for _, s := range sessions {
			fmt.Fprintf(w, "  %s\n", s)
		}
		fmt.Fprintln(w, "\nUsage: aibutler resume <SESSION_ID>")
		return nil
	}

	sessionID := args[0]
	persister := session.NewFilePersister(app.dataDir + "/sessions")
	messages, err := persister.Load(sessionID)
	if err != nil {
		return fmt.Errorf("resume: load session %s: %w", sessionID, err)
	}

	fmt.Fprintf(w, "Resumed session %s (%d messages)\n", sessionID, len(messages))
	fmt.Fprintln(w, "Starting REPL with restored context...")

	// Load messages into session manager and start REPL.
	ctx := context.Background()
	for _, msg := range messages {
		if err := app.Sessions.AddMessage(ctx, sessionID, agent.Message{Role: msg.Role, Content: msg.Content}); err != nil {
			fmt.Fprintf(w, "warning: could not restore message: %v\n", err)
		}
	}

	// Start REPL with this session.
	return CmdRepl(app, []string{"--session", sessionID}, w)
}
