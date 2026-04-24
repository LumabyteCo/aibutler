package cli

import (
	"context"
	"fmt"
	"io"
	"time"
)

// CmdGDPR handles GDPR data management operations.
// Usage: aibutler gdpr delete-user <user_id>
func CmdGDPR(app *App, args []string, w io.Writer) error {
	if app.ComplianceLogger == nil {
		return fmt.Errorf("compliance module not initialized. Ensure database is available.")
	}

	if len(args) == 0 {
		fmt.Fprintln(w, "Usage: aibutler gdpr <subcommand>")
		fmt.Fprintln(w, "")
		fmt.Fprintln(w, "Subcommands:")
		fmt.Fprintln(w, "  delete-user <user_id>    Delete all personal data for a user (right to erasure)")
		fmt.Fprintln(w, "  export <user_id>         Export all personal data for a user (right to portability)")
		fmt.Fprintln(w, "  purge <days>             Purge audit logs older than N days (retention policy)")
		return nil
	}

	ctx := context.Background()

	switch args[0] {
	case "delete-user":
		if len(args) < 2 {
			return fmt.Errorf("usage: aibutler gdpr delete-user <user_id>")
		}
		userID := args[1]
		if err := app.ComplianceLogger.DeleteUserData(ctx, userID); err != nil {
			return fmt.Errorf("gdpr delete-user: %w", err)
		}
		fmt.Fprintf(w, "All personal data for user %s has been deleted.\n", userID)

	case "export":
		if len(args) < 2 {
			return fmt.Errorf("usage: aibutler gdpr export <user_id>")
		}
		userID := args[1]
		if err := app.ComplianceLogger.Export(ctx, "json", w); err != nil {
			return fmt.Errorf("gdpr export: %w", err)
		}
		fmt.Fprintf(w, "\nExported all data for user %s.\n", userID)

	case "purge":
		if len(args) < 2 {
			return fmt.Errorf("usage: aibutler gdpr purge <days>")
		}
		// Parse days and call retention purge.
		var days int
		if _, err := fmt.Sscanf(args[1], "%d", &days); err != nil {
			return fmt.Errorf("invalid days: %s", args[1])
		}
		count, err := app.ComplianceLogger.RetentionPurge(ctx, time.Duration(days)*24*time.Hour)
		if err != nil {
			return fmt.Errorf("gdpr purge: %w", err)
		}
		fmt.Fprintf(w, "Purged %d audit log entries older than %d days.\n", count, days)

	default:
		return fmt.Errorf("unknown subcommand: %s. Use: delete-user, export, purge", args[0])
	}

	return nil
}
