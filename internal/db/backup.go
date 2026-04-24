package db

import (
	"context"
	"fmt"
	"os"

	"github.com/ncruces/go-sqlite3"
)

// Backup creates a consistent snapshot of the database to the given file path
// using SQLite's Online Backup API.
func (d *DB) Backup(_ context.Context, dstPath string) error {
	if d.path == ":memory:" {
		return fmt.Errorf("backup: cannot backup in-memory database")
	}

	srcConn, err := sqlite3.Open(d.path)
	if err != nil {
		return fmt.Errorf("backup: open source: %w", err)
	}
	defer srcConn.Close()

	// Remove destination if it exists to start fresh.
	os.Remove(dstPath)

	// BackupInit(srcDB, dstURI) — backs up "main" database into dstPath.
	backup, err := srcConn.BackupInit("main", dstPath)
	if err != nil {
		return fmt.Errorf("backup: init: %w", err)
	}
	defer backup.Close()

	// Step through all pages (-1 = copy everything at once).
	done, err := backup.Step(-1)
	if err != nil {
		return fmt.Errorf("backup: step: %w", err)
	}
	if !done {
		return fmt.Errorf("backup: step did not complete")
	}

	return nil
}
