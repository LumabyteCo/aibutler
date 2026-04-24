package swarm_test

import (
	"context"
	"testing"
	"time"

	swarmws "github.com/LumabyteCo/aibutler/internal/memory/swarm"
	"github.com/LumabyteCo/aibutler/testutil"
)

func TestSet(t *testing.T) {
	db := testutil.TestDB(t)
	ws := swarmws.NewWorkspace(db.Conn())
	ctx := context.Background()

	err := ws.Set(ctx, "run-1", "key1", "value1", "agent-a")
	if err != nil {
		t.Fatalf("Set: %v", err)
	}
}

func TestGet(t *testing.T) {
	db := testutil.TestDB(t)
	ws := swarmws.NewWorkspace(db.Conn())
	ctx := context.Background()

	_ = ws.Set(ctx, "run-1", "key1", "hello", "agent-a")

	val, err := ws.Get(ctx, "run-1", "key1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if val != "hello" {
		t.Errorf("expected 'hello', got %q", val)
	}
}

func TestGetMissing(t *testing.T) {
	db := testutil.TestDB(t)
	ws := swarmws.NewWorkspace(db.Conn())
	ctx := context.Background()

	val, err := ws.Get(ctx, "run-1", "nonexistent")
	if err != nil {
		t.Fatalf("Get missing should not error: %v", err)
	}
	if val != "" {
		t.Errorf("expected empty string for missing key, got %q", val)
	}
}

func TestOverwrite(t *testing.T) {
	db := testutil.TestDB(t)
	ws := swarmws.NewWorkspace(db.Conn())
	ctx := context.Background()

	_ = ws.Set(ctx, "run-1", "key1", "first", "agent-a")
	_ = ws.Set(ctx, "run-1", "key1", "second", "agent-b")

	val, err := ws.Get(ctx, "run-1", "key1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if val != "second" {
		t.Errorf("expected 'second' after overwrite, got %q", val)
	}
}

func TestList(t *testing.T) {
	db := testutil.TestDB(t)
	ws := swarmws.NewWorkspace(db.Conn())
	ctx := context.Background()

	_ = ws.Set(ctx, "run-1", "key1", "v1", "agent-a")
	_ = ws.Set(ctx, "run-1", "key2", "v2", "agent-b")
	_ = ws.Set(ctx, "run-1", "key3", "v3", "agent-c")

	m, err := ws.List(ctx, "run-1")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(m) != 3 {
		t.Errorf("expected 3 entries, got %d", len(m))
	}
	if m["key1"] != "v1" || m["key2"] != "v2" || m["key3"] != "v3" {
		t.Errorf("unexpected values: %v", m)
	}
}

func TestListEmpty(t *testing.T) {
	db := testutil.TestDB(t)
	ws := swarmws.NewWorkspace(db.Conn())
	ctx := context.Background()

	m, err := ws.List(ctx, "nonexistent-run")
	if err != nil {
		t.Fatalf("List empty: %v", err)
	}
	if len(m) != 0 {
		t.Errorf("expected empty map, got %d entries", len(m))
	}
}

func TestPurge(t *testing.T) {
	db := testutil.TestDB(t)
	ws := swarmws.NewWorkspace(db.Conn())
	ctx := context.Background()

	_ = ws.Set(ctx, "run-1", "key1", "v1", "agent-a")
	_ = ws.Set(ctx, "run-1", "key2", "v2", "agent-b")
	// run-2 should not be affected.
	_ = ws.Set(ctx, "run-2", "key1", "v1", "agent-c")

	err := ws.Purge(ctx, "run-1")
	if err != nil {
		t.Fatalf("Purge: %v", err)
	}

	m, _ := ws.List(ctx, "run-1")
	if len(m) != 0 {
		t.Errorf("expected 0 entries after purge, got %d", len(m))
	}

	// run-2 should still have its entry.
	m2, _ := ws.List(ctx, "run-2")
	if len(m2) != 1 {
		t.Errorf("expected run-2 to still have 1 entry, got %d", len(m2))
	}
}

func TestPurgeOlderThan(t *testing.T) {
	db := testutil.TestDB(t)
	ws := swarmws.NewWorkspace(db.Conn())
	ctx := context.Background()

	// Insert old data directly with a past timestamp.
	conn := db.Conn()
	oldTime := time.Now().UTC().Add(-25 * time.Hour).Format(time.RFC3339)
	_, _ = conn.ExecContext(ctx,
		`INSERT INTO swarm_workspaces (run_id, key, value, written_by, written_at) VALUES (?, ?, ?, ?, ?)`,
		"run-old", "key1", "old-val", "agent-a", oldTime)

	// Add a recent entry.
	_ = ws.Set(ctx, "run-new", "key1", "new-val", "agent-b")

	n, err := ws.PurgeOlderThan(ctx, 24)
	if err != nil {
		t.Fatalf("PurgeOlderThan: %v", err)
	}
	if n < 1 {
		t.Errorf("expected at least 1 purged, got %d", n)
	}

	// New entry should still exist.
	val, _ := ws.Get(ctx, "run-new", "key1")
	if val != "new-val" {
		t.Errorf("recent entry should not have been purged")
	}
}

func TestMultiRunIsolation(t *testing.T) {
	db := testutil.TestDB(t)
	ws := swarmws.NewWorkspace(db.Conn())
	ctx := context.Background()

	_ = ws.Set(ctx, "run-1", "key", "run1-value", "agent-a")
	_ = ws.Set(ctx, "run-2", "key", "run2-value", "agent-b")

	v1, _ := ws.Get(ctx, "run-1", "key")
	v2, _ := ws.Get(ctx, "run-2", "key")

	if v1 != "run1-value" {
		t.Errorf("run-1 key: expected 'run1-value', got %q", v1)
	}
	if v2 != "run2-value" {
		t.Errorf("run-2 key: expected 'run2-value', got %q", v2)
	}
}
