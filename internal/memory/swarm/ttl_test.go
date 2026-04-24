package swarm

import (
	"context"
	"testing"
	"time"

	"github.com/LumabyteCo/aibutler/testutil"
)

func TestStartTTLEnforcer(t *testing.T) {
	db := testutil.TestDB(t)
	ws := NewWorkspace(db.Conn())
	ctx := context.Background()

	// Write an entry with old timestamp by manipulating the DB directly.
	oldTime := time.Now().UTC().Add(-48 * time.Hour).Format(time.RFC3339)
	_, err := db.Conn().ExecContext(ctx,
		`INSERT INTO swarm_workspaces (run_id, key, value, written_by, written_at)
		 VALUES (?, ?, ?, ?, ?)`,
		"old-run", "key1", "value1", "test-agent", oldTime)
	if err != nil {
		t.Fatalf("insert old entry: %v", err)
	}

	// Write a fresh entry.
	if err := ws.Set(ctx, "new-run", "key2", "value2", "test-agent"); err != nil {
		t.Fatalf("set new entry: %v", err)
	}

	// Start enforcer with TTL of 24 hours and fast tick.
	stop := StartTTLEnforcer(ctx, ws, 100*time.Millisecond, 24)

	// Wait for at least one tick.
	time.Sleep(300 * time.Millisecond)
	stop()

	// Old entry should be purged.
	val, err := ws.Get(ctx, "old-run", "key1")
	if err != nil {
		t.Fatalf("get old: %v", err)
	}
	if val != "" {
		t.Errorf("old entry should be purged, got %q", val)
	}

	// New entry should still exist.
	val, err = ws.Get(ctx, "new-run", "key2")
	if err != nil {
		t.Fatalf("get new: %v", err)
	}
	if val != "value2" {
		t.Errorf("new entry should exist, got %q", val)
	}
}
