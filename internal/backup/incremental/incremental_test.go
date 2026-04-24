package incremental

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/ncruces/go-sqlite3"
	_ "github.com/ncruces/go-sqlite3/embed"
)

// createTestDB creates a temp SQLite database with a test table.
func createTestDB(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	conn, err := sqlite3.Open(dbPath)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := conn.Exec("CREATE TABLE test (id INTEGER PRIMARY KEY, value TEXT)"); err != nil {
		t.Fatalf("create table: %v", err)
	}
	if err := conn.Exec("INSERT INTO test (value) VALUES ('hello')"); err != nil {
		t.Fatalf("insert: %v", err)
	}
	conn.Close()
	return dbPath
}

func TestSnapshot(t *testing.T) {
	dbPath := createTestDB(t)
	backupDir := filepath.Join(t.TempDir(), "backups")

	mgr := New(dbPath, backupDir)
	ctx := context.Background()

	path, err := mgr.Snapshot(ctx)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}

	// Verify snapshot file exists.
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat snapshot: %v", err)
	}
	if info.Size() == 0 {
		t.Error("snapshot file is empty")
	}

	// Verify snapshot is a valid SQLite database.
	conn, err := sqlite3.Open(path)
	if err != nil {
		t.Fatalf("open snapshot: %v", err)
	}
	defer conn.Close()

	stmt, _, err := conn.Prepare("SELECT value FROM test LIMIT 1")
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	defer stmt.Close()
	if stmt.Step() {
		got := stmt.ColumnText(0)
		if got != "hello" {
			t.Errorf("got %q, want %q", got, "hello")
		}
	} else {
		t.Error("no rows in snapshot")
	}
}

func TestList(t *testing.T) {
	dbPath := createTestDB(t)
	backupDir := filepath.Join(t.TempDir(), "backups")

	mgr := New(dbPath, backupDir)
	ctx := context.Background()

	// Create a couple of snapshots.
	for i := 0; i < 3; i++ {
		if _, err := mgr.Snapshot(ctx); err != nil {
			t.Fatalf("snapshot %d: %v", i, err)
		}
	}

	snaps, err := mgr.List(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(snaps) != 3 {
		t.Fatalf("got %d snapshots, want 3", len(snaps))
	}
}

func TestPrune(t *testing.T) {
	dbPath := createTestDB(t)
	backupDir := filepath.Join(t.TempDir(), "backups")

	mgr := New(dbPath, backupDir)
	ctx := context.Background()

	// Create 5 snapshots.
	for i := 0; i < 5; i++ {
		if _, err := mgr.Snapshot(ctx); err != nil {
			t.Fatalf("snapshot %d: %v", i, err)
		}
	}

	removed, err := mgr.Prune(ctx, 2)
	if err != nil {
		t.Fatalf("prune: %v", err)
	}
	if removed != 3 {
		t.Errorf("removed %d, want 3", removed)
	}

	snaps, _ := mgr.List(ctx)
	if len(snaps) != 2 {
		t.Errorf("after prune: %d snapshots, want 2", len(snaps))
	}
}

func TestRestore(t *testing.T) {
	dbPath := createTestDB(t)
	backupDir := filepath.Join(t.TempDir(), "backups")

	mgr := New(dbPath, backupDir)
	ctx := context.Background()

	// Take a snapshot.
	snapPath, err := mgr.Snapshot(ctx)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}

	// Corrupt the original database by deleting it.
	os.Remove(dbPath)

	// Restore from snapshot.
	if err := mgr.Restore(ctx, snapPath); err != nil {
		t.Fatalf("restore: %v", err)
	}

	// Verify restored database has the data.
	conn, err := sqlite3.Open(dbPath)
	if err != nil {
		t.Fatalf("open restored: %v", err)
	}
	defer conn.Close()

	stmt, _, err := conn.Prepare("SELECT value FROM test LIMIT 1")
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	defer stmt.Close()
	if stmt.Step() {
		got := stmt.ColumnText(0)
		if got != "hello" {
			t.Errorf("got %q, want %q", got, "hello")
		}
	} else {
		t.Error("no rows after restore")
	}
}
