package cli

import (
	"context"
	"fmt"
	"io"
)

// CmdAuth handles the "auth" command and its subcommands.
func CmdAuth(app *App, args []string, w io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: aibutler auth <list|status|revoke> [service]")
	}
	switch args[0] {
	case "list":
		return cmdAuthList(app, w)
	case "status":
		return cmdAuthStatus(app, w)
	case "revoke":
		if len(args) < 2 {
			return fmt.Errorf("usage: aibutler auth revoke <service>")
		}
		return cmdAuthRevoke(app, args[1], w)
	default:
		return fmt.Errorf("unknown auth subcommand: %s", args[0])
	}
}

func cmdAuthList(app *App, w io.Writer) error {
	ctx := context.Background()
	keys, err := app.Vault.List(ctx)
	if err != nil {
		return fmt.Errorf("vault list: %w", err)
	}
	if len(keys) == 0 {
		fmt.Fprintln(w, "No stored credentials.")
		return nil
	}
	fmt.Fprintln(w, "Stored credentials:")
	for _, key := range keys {
		fmt.Fprintf(w, "  - %s\n", key)
	}
	return nil
}

func cmdAuthStatus(app *App, w io.Writer) error {
	ctx := context.Background()
	if err := app.Vault.HealthCheck(ctx); err != nil {
		fmt.Fprintf(w, "Vault status: unhealthy (%v)\n", err)
		return nil
	}
	fmt.Fprintln(w, "Vault status: healthy")
	return nil
}

func cmdAuthRevoke(app *App, service string, w io.Writer) error {
	ctx := context.Background()
	has, err := app.Vault.Has(ctx, service)
	if err != nil {
		return fmt.Errorf("vault check: %w", err)
	}
	if !has {
		return fmt.Errorf("credential %q not found", service)
	}
	if err := app.Vault.Delete(ctx, service); err != nil {
		return fmt.Errorf("vault delete: %w", err)
	}
	fmt.Fprintf(w, "Revoked credential: %s\n", service)
	return nil
}
