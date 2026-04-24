package backup_test

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/LumabyteCo/aibutler/internal/backup"
	"github.com/LumabyteCo/aibutler/testutil"
)

func TestCreateBackup(t *testing.T) {
	db := testutil.TestDB(t)
	dir := t.TempDir()
	dest := filepath.Join(dir, "backup.db")

	if err := backup.CreateBackup(db.Conn(), dest); err != nil {
		t.Fatalf("create backup: %v", err)
	}

	info, err := os.Stat(dest)
	if err != nil {
		t.Fatalf("stat backup: %v", err)
	}
	if info.Size() == 0 {
		t.Error("backup file is empty")
	}
}

func TestRotateBackups(t *testing.T) {
	dir := t.TempDir()

	// Create 5 backup files with unique names.
	for i := 0; i < 5; i++ {
		name := fmt.Sprintf("backup-%d.db", i)
		os.WriteFile(filepath.Join(dir, name), []byte("data"), 0644)
	}

	// Rotate to keep 3.
	if err := backup.RotateBackups(dir, 3); err != nil {
		t.Fatalf("rotate: %v", err)
	}

	entries, _ := os.ReadDir(dir)
	if len(entries) != 3 {
		t.Errorf("entries = %d, want 3", len(entries))
	}
}

func TestExportImport(t *testing.T) {
	db := testutil.TestDB(t)
	dir := t.TempDir()

	exportPath := filepath.Join(dir, "export.db")
	if err := backup.Export(db.Conn(), exportPath, nil); err != nil {
		t.Fatalf("export: %v", err)
	}

	importPath := filepath.Join(dir, "imported.db")
	if err := backup.Import(exportPath, importPath, nil); err != nil {
		t.Fatalf("import: %v", err)
	}

	info, _ := os.Stat(importPath)
	if info.Size() == 0 {
		t.Error("imported file is empty")
	}
}

func TestIntegrityCheck(t *testing.T) {
	db := testutil.TestDB(t)
	if err := backup.IntegrityCheck(db.Conn()); err != nil {
		t.Fatalf("integrity check: %v", err)
	}
}
