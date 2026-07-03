package checkpoint_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/LumabyteCo/aibutler/internal/checkpoint"
	"github.com/LumabyteCo/aibutler/testutil"
)

func newStore(t *testing.T) (*checkpoint.Store, string) {
	t.Helper()
	db := testutil.TestDB(t)
	dir := t.TempDir()
	s := checkpoint.New(db.Conn(), filepath.Join(dir, "spill"), func(path string) error {
		if !strings.HasPrefix(path, dir) {
			return os.ErrPermission
		}
		return nil
	})
	return s, dir
}

func TestSnapshotAndRestoreRoundTrip(t *testing.T) {
	s, dir := newStore(t)
	ctx := context.Background()
	path := filepath.Join(dir, "notes.txt")

	if err := os.WriteFile(path, []byte("original"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := s.Snapshot(ctx, "file.write", path); err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	// The mutation happens.
	if err := os.WriteFile(path, []byte("mutated"), 0o644); err != nil {
		t.Fatal(err)
	}

	cps, err := s.List(ctx, 10)
	if err != nil || len(cps) != 1 {
		t.Fatalf("list = %d entries (err %v), want 1", len(cps), err)
	}
	if !cps[0].Restorable || cps[0].Absent {
		t.Fatalf("unexpected checkpoint state: %+v", cps[0])
	}

	if _, err := s.Restore(ctx, cps[0].ID); err != nil {
		t.Fatalf("restore: %v", err)
	}
	got, _ := os.ReadFile(path)
	if string(got) != "original" {
		t.Errorf("restored content = %q, want original", got)
	}

	// The restore itself created a checkpoint of the mutated state —
	// restoring THAT brings the mutation back (undo of undo).
	cps, _ = s.List(ctx, 10)
	if len(cps) != 2 {
		t.Fatalf("expected 2 checkpoints after restore, got %d", len(cps))
	}
	if cps[0].Tool != "checkpoint.restore" {
		t.Errorf("newest checkpoint tool = %q, want checkpoint.restore", cps[0].Tool)
	}
	if _, err := s.Restore(ctx, cps[0].ID); err != nil {
		t.Fatalf("undo-of-undo: %v", err)
	}
	got, _ = os.ReadFile(path)
	if string(got) != "mutated" {
		t.Errorf("after undo-of-undo content = %q, want mutated", got)
	}
}

func TestSnapshotAbsentFileRestoreDeletes(t *testing.T) {
	s, dir := newStore(t)
	ctx := context.Background()
	path := filepath.Join(dir, "new-file.txt")

	// Pre-image: file does not exist.
	if err := s.Snapshot(ctx, "file.write", path); err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if err := os.WriteFile(path, []byte("created"), 0o644); err != nil {
		t.Fatal(err)
	}

	cps, _ := s.List(ctx, 10)
	if len(cps) != 1 || !cps[0].Absent || !cps[0].Restorable {
		t.Fatalf("unexpected checkpoint: %+v", cps)
	}
	if _, err := s.Restore(ctx, cps[0].ID); err != nil {
		t.Fatalf("restore: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("restore of an absent pre-image should delete the created file")
	}
}

func TestLargeFileSpillsAndRestores(t *testing.T) {
	s, dir := newStore(t)
	ctx := context.Background()
	path := filepath.Join(dir, "big.bin")

	big := make([]byte, (1<<20)+512) // just over the inline cap
	for i := range big {
		big[i] = byte(i % 251)
	}
	if err := os.WriteFile(path, big, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := s.Snapshot(ctx, "file.write", path); err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if err := os.WriteFile(path, []byte("small now"), 0o644); err != nil {
		t.Fatal(err)
	}

	cps, _ := s.List(ctx, 10)
	if len(cps) != 1 || !cps[0].Restorable {
		t.Fatalf("spilled checkpoint should be restorable: %+v", cps)
	}
	if _, err := s.Restore(ctx, cps[0].ID); err != nil {
		t.Fatalf("restore: %v", err)
	}
	got, _ := os.ReadFile(path)
	if len(got) != len(big) || got[1000] != big[1000] {
		t.Errorf("restored %d bytes, want %d", len(got), len(big))
	}
}

func TestRestoreRevalidatesPathBoundary(t *testing.T) {
	db := testutil.TestDB(t)
	dir := t.TempDir()
	allowed := true
	s := checkpoint.New(db.Conn(), "", func(path string) error {
		if !allowed {
			return os.ErrPermission
		}
		return nil
	})
	ctx := context.Background()
	path := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := s.Snapshot(ctx, "file.write", path); err != nil {
		t.Fatal(err)
	}
	cps, _ := s.List(ctx, 10)

	// Roots changed since the snapshot (e.g. config edited): restore refuses.
	allowed = false
	if _, err := s.Restore(ctx, cps[0].ID); err == nil {
		t.Fatal("restore must re-validate the path against current allowed roots")
	}
}

func TestPurgeOlderThanSweepsRowsAndOrphanedSpills(t *testing.T) {
	s, dir := newStore(t)
	ctx := context.Background()
	path := filepath.Join(dir, "f.txt")

	big := make([]byte, (1<<20)+1)
	if err := os.WriteFile(path, big, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := s.Snapshot(ctx, "file.write", path); err != nil {
		t.Fatal(err)
	}

	// Nothing is old enough yet.
	n, err := s.PurgeOlderThan(ctx, time.Hour)
	if err != nil || n != 0 {
		t.Fatalf("purge young = %d (err %v), want 0", n, err)
	}
	// Everything qualifies with a negative age cutoff in the future.
	n, err = s.PurgeOlderThan(ctx, -time.Hour)
	if err != nil || n != 1 {
		t.Fatalf("purge all = %d (err %v), want 1", n, err)
	}
	// Spill file swept once unreferenced.
	entries, _ := os.ReadDir(filepath.Join(dir, "spill"))
	for _, e := range entries {
		if len(e.Name()) == 64 {
			t.Errorf("orphaned spill file survived purge: %s", e.Name())
		}
	}
}

func TestRestoreMissingCheckpoint(t *testing.T) {
	s, _ := newStore(t)
	if _, err := s.Restore(context.Background(), 424242); err == nil {
		t.Fatal("expected error for missing checkpoint")
	}
}
