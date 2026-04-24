package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/LumabyteCo/aibutler/internal/backup"
)

// CmdBackup handles the "backup" command and its subcommands.
func CmdBackup(app *App, args []string, w io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: aibutler backup <now|list|verify|export|import> [file]")
	}
	switch args[0] {
	case "now":
		return cmdBackupNow(app, w)
	case "list":
		return cmdBackupList(app, w)
	case "verify":
		return cmdBackupVerify(app, w)
	case "export":
		if len(args) < 2 {
			return fmt.Errorf("usage: aibutler backup export <file>")
		}
		return cmdBackupExport(app, args[1], w)
	case "import":
		if len(args) < 2 {
			return fmt.Errorf("usage: aibutler backup import <file>")
		}
		return cmdBackupImport(app, args[1], w)
	default:
		return fmt.Errorf("unknown backup subcommand: %s", args[0])
	}
}

func backupDir(app *App) string {
	return filepath.Join(app.dataDir, "backups")
}

func cmdBackupNow(app *App, w io.Writer) error {
	dir := backupDir(app)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("create backup dir: %w", err)
	}

	stamp := time.Now().UTC().Format("20060102-150405")
	dst := filepath.Join(dir, fmt.Sprintf("aibutler-%s.db", stamp))

	ctx := context.Background()
	key := backupKey(app, ctx)
	if err := backup.Export(app.DB.Conn(), dst, key); err != nil {
		return fmt.Errorf("backup: %w", err)
	}

	if len(key) > 0 {
		fmt.Fprintf(w, "Encrypted backup created: %s\n", dst)
	} else {
		fmt.Fprintf(w, "Backup created: %s\n", dst)
	}
	return nil
}

func cmdBackupList(app *App, w io.Writer) error {
	dir := backupDir(app)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Fprintln(w, "No backups found.")
			return nil
		}
		return fmt.Errorf("read backup dir: %w", err)
	}

	if len(entries) == 0 {
		fmt.Fprintln(w, "No backups found.")
		return nil
	}

	fmt.Fprintln(w, "Backups:")
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		fmt.Fprintf(w, "  %-40s %10d bytes  %s\n", e.Name(), info.Size(), info.ModTime().Format(time.RFC3339))
	}
	return nil
}

func cmdBackupVerify(app *App, w io.Writer) error {
	dir := backupDir(app)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Fprintln(w, "No backups to verify.")
			return nil
		}
		return fmt.Errorf("read backup dir: %w", err)
	}

	ok := 0
	bad := 0
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		info, err := e.Info()
		if err != nil || info.Size() == 0 {
			fmt.Fprintf(w, "  FAIL  %s (empty or unreadable)\n", e.Name())
			bad++
			continue
		}
		fmt.Fprintf(w, "  OK    %s (%d bytes)\n", e.Name(), info.Size())
		ok++
	}

	if ok+bad == 0 {
		fmt.Fprintln(w, "No backups to verify.")
		return nil
	}
	fmt.Fprintf(w, "\nVerified: %d ok, %d failed\n", ok, bad)
	return nil
}

func cmdBackupExport(app *App, dst string, w io.Writer) error {
	ctx := context.Background()
	key := backupKey(app, ctx)
	if err := backup.Export(app.DB.Conn(), dst, key); err != nil {
		return fmt.Errorf("export: %w", err)
	}
	if len(key) > 0 {
		fmt.Fprintf(w, "Encrypted database exported to: %s\n", dst)
	} else {
		fmt.Fprintf(w, "Database exported to: %s\n", dst)
	}
	return nil
}

func cmdBackupImport(app *App, src string, w io.Writer) error {
	info, err := os.Stat(src)
	if err != nil {
		return fmt.Errorf("import: %w", err)
	}
	if info.Size() == 0 {
		return fmt.Errorf("import: file %s is empty", src)
	}
	fmt.Fprintf(w, "File %s verified (%d bytes).\n", src, info.Size())
	fmt.Fprintln(w, "To import, stop AI Butler and copy this file to your database path.")
	fmt.Fprintln(w, "A restart is required after importing.")
	return nil
}

// backupKey retrieves the backup encryption key from the vault.
// Returns nil if no key is set (backups will be plaintext).
func backupKey(app *App, ctx context.Context) []byte {
	cred, err := app.Vault.Get(ctx, "backup_encryption_key")
	if err != nil || len(cred.Value) != 32 {
		return nil
	}
	return cred.Value
}

// CmdCleanup removes expired sessions and their messages.
func CmdCleanup(app *App, args []string, w io.Writer) error {
	ctx := context.Background()

	maxAge := 7 * 24 * time.Hour // Default: 7 days
	if len(args) > 0 && args[0] == "--all" {
		maxAge = 0 // Remove all sessions
	}

	count, err := app.Sessions.CleanupExpired(ctx, maxAge)
	if err != nil {
		return fmt.Errorf("cleanup: %w", err)
	}

	total, _ := app.Sessions.Count(ctx)
	if count == 0 {
		fmt.Fprintf(w, "No expired sessions found. Total active: %d\n", total)
	} else {
		fmt.Fprintf(w, "Cleaned up %d expired session(s). Remaining: %d\n", count, total)
	}
	return nil
}

// CmdIntegrity runs PRAGMA integrity_check and vault health check.
func CmdIntegrity(app *App, _ []string, w io.Writer) error {
	ctx := context.Background()

	fmt.Fprint(w, "Database integrity... ")
	if err := app.DB.IntegrityCheck(ctx); err != nil {
		fmt.Fprintf(w, "FAIL (%v)\n", err)
	} else {
		fmt.Fprintln(w, "OK")
	}

	fmt.Fprint(w, "Vault health...      ")
	if err := app.Vault.HealthCheck(ctx); err != nil {
		fmt.Fprintf(w, "FAIL (%v)\n", err)
	} else {
		fmt.Fprintln(w, "OK")
	}

	return nil
}
