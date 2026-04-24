package cli

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/LumabyteCo/aibutler/internal/vault"
)

// CmdVault handles the "vault" command and its subcommands.
func CmdVault(app *App, args []string, w io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: aibutler vault <set|get|list|delete> [key] [value]")
	}
	switch args[0] {
	case "set":
		if len(args) < 3 {
			return fmt.Errorf("usage: aibutler vault set <key> <value>")
		}
		return cmdVaultSet(app, args[1], strings.Join(args[2:], " "), w)
	case "get":
		if len(args) < 2 {
			return fmt.Errorf("usage: aibutler vault get <key>")
		}
		return cmdVaultGet(app, args[1], w)
	case "list":
		return cmdVaultList(app, w)
	case "delete":
		if len(args) < 2 {
			return fmt.Errorf("usage: aibutler vault delete <key>")
		}
		return cmdVaultDelete(app, args[1], w)
	default:
		return fmt.Errorf("unknown vault subcommand: %s\nusage: aibutler vault <set|get|list|delete> [key] [value]", args[0])
	}
}

func cmdVaultSet(app *App, key, value string, w io.Writer) error {
	ctx := context.Background()
	cred := vault.Credential{
		Key:   key,
		Type:  vault.CredAPIKey,
		Value: []byte(value),
	}
	if err := app.Vault.Store(ctx, cred); err != nil {
		return fmt.Errorf("vault store: %w", err)
	}
	fmt.Fprintf(w, "Stored credential: %s\n", key)
	return nil
}

func cmdVaultGet(app *App, key string, w io.Writer) error {
	ctx := context.Background()
	cred, err := app.Vault.Get(ctx, key)
	if err != nil {
		return fmt.Errorf("vault get: %w", err)
	}
	fmt.Fprintf(w, "%s\n", string(cred.Value))
	return nil
}

func cmdVaultList(app *App, w io.Writer) error {
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

func cmdVaultDelete(app *App, key string, w io.Writer) error {
	ctx := context.Background()
	has, err := app.Vault.Has(ctx, key)
	if err != nil {
		return fmt.Errorf("vault check: %w", err)
	}
	if !has {
		return fmt.Errorf("credential %q not found", key)
	}
	if err := app.Vault.Delete(ctx, key); err != nil {
		return fmt.Errorf("vault delete: %w", err)
	}
	fmt.Fprintf(w, "Deleted credential: %s\n", key)
	return nil
}
