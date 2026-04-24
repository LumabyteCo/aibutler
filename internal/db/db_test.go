package db_test

import (
	"context"
	"testing"

	"github.com/LumabyteCo/aibutler/testutil"
)

func TestOpenInMemory(t *testing.T) {
	database := testutil.TestDB(t)
	if database.Conn() == nil {
		t.Fatal("expected non-nil connection")
	}
}

func TestPragmasApplied(t *testing.T) {
	database := testutil.TestDB(t)
	ctx := context.Background()
	conn := database.Conn()

	tests := []struct {
		pragma   string
		expected string
	}{
		// In-memory DBs use "memory" journal mode; WAL applies to file-based DBs.
		{"PRAGMA journal_mode", "memory"},
		{"PRAGMA foreign_keys", "1"},
	}

	for _, tt := range tests {
		var value string
		if err := conn.QueryRowContext(ctx, tt.pragma).Scan(&value); err != nil {
			t.Fatalf("%s: %v", tt.pragma, err)
		}
		if value != tt.expected {
			t.Errorf("%s = %q, want %q", tt.pragma, value, tt.expected)
		}
	}
}

func TestIntegrityCheck(t *testing.T) {
	database := testutil.TestDB(t)
	if err := database.IntegrityCheck(context.Background()); err != nil {
		t.Fatalf("integrity check failed: %v", err)
	}
}
