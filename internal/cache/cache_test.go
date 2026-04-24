package cache

import (
	"context"
	"testing"
	"time"

	"github.com/LumabyteCo/aibutler/testutil"
)

func TestCache_SetAndGet(t *testing.T) {
	db := testutil.TestDB(t)
	c := New(db.Conn(), DefaultConfig())
	ctx := context.Background()

	err := c.Set(ctx, "key1", "value1", 5*time.Minute)
	if err != nil {
		t.Fatalf("Set: %v", err)
	}

	val, found, err := c.Get(ctx, "key1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !found {
		t.Fatal("expected key1 to be found")
	}
	if val != "value1" {
		t.Errorf("Get = %q, want 'value1'", val)
	}
}

func TestCache_TTLExpiry(t *testing.T) {
	db := testutil.TestDB(t)
	c := New(db.Conn(), DefaultConfig())
	ctx := context.Background()

	// Set with a very short TTL (already expired).
	err := c.Set(ctx, "expired", "oldvalue", -1*time.Second)
	if err != nil {
		t.Fatalf("Set: %v", err)
	}

	_, found, err := c.Get(ctx, "expired")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if found {
		t.Error("expected expired key to not be found")
	}
}

func TestCache_Miss(t *testing.T) {
	db := testutil.TestDB(t)
	c := New(db.Conn(), DefaultConfig())
	ctx := context.Background()

	_, found, err := c.Get(ctx, "nonexistent")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if found {
		t.Error("expected miss for nonexistent key")
	}
}

func TestCache_PruneExpired(t *testing.T) {
	db := testutil.TestDB(t)
	c := New(db.Conn(), DefaultConfig())
	ctx := context.Background()

	// Insert expired and valid entries.
	_ = c.Set(ctx, "expired1", "val", -1*time.Second)
	_ = c.Set(ctx, "expired2", "val", -1*time.Second)
	_ = c.Set(ctx, "valid", "val", 5*time.Minute)

	pruned, err := c.Prune(ctx)
	if err != nil {
		t.Fatalf("Prune: %v", err)
	}
	if pruned != 2 {
		t.Errorf("Prune = %d, want 2", pruned)
	}

	// Valid entry should still be accessible.
	_, found, _ := c.Get(ctx, "valid")
	if !found {
		t.Error("valid entry should still be accessible after prune")
	}
}

func TestCache_StatsTracking(t *testing.T) {
	db := testutil.TestDB(t)
	c := New(db.Conn(), DefaultConfig())
	ctx := context.Background()

	_ = c.Set(ctx, "key", "value", 5*time.Minute)

	// One hit.
	_, _, _ = c.Get(ctx, "key")
	// One miss.
	_, _, _ = c.Get(ctx, "missing")

	stats, err := c.Stats(ctx)
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}

	if stats.HitCount != 1 {
		t.Errorf("HitCount = %d, want 1", stats.HitCount)
	}
	if stats.MissCount != 1 {
		t.Errorf("MissCount = %d, want 1", stats.MissCount)
	}
	if stats.HitRate != 0.5 {
		t.Errorf("HitRate = %f, want 0.5", stats.HitRate)
	}
	if stats.TotalEntries != 1 {
		t.Errorf("TotalEntries = %d, want 1", stats.TotalEntries)
	}
}

func TestHashKey(t *testing.T) {
	// Same inputs should produce same hash.
	h1 := HashKey("tool", "input", "params")
	h2 := HashKey("tool", "input", "params")
	if h1 != h2 {
		t.Errorf("same inputs produced different hashes: %q vs %q", h1, h2)
	}

	// Different inputs should produce different hashes.
	h3 := HashKey("tool", "input", "different")
	if h1 == h3 {
		t.Error("different inputs produced same hash")
	}

	// Hash should be 64 hex characters (SHA-256).
	if len(h1) != 64 {
		t.Errorf("hash length = %d, want 64", len(h1))
	}
}
