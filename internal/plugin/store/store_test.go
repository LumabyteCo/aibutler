package store_test

import (
	"context"
	"testing"

	"github.com/LumabyteCo/aibutler/internal/plugin/store"
	"github.com/LumabyteCo/aibutler/testutil"
)

func setupStore(t *testing.T) (*store.Store, *store.Store) {
	t.Helper()
	database := testutil.TestDB(t)
	ctx := context.Background()
	conn := database.Conn()

	// Insert two plugins for isolation tests.
	_, err := conn.ExecContext(ctx,
		`INSERT INTO plugins (name, version, manifest_hash, wasm_hash, status) VALUES ('plugin-a', '1.0', 'h1', 'w1', 'enabled')`)
	if err != nil {
		t.Fatalf("insert plugin-a: %v", err)
	}
	_, err = conn.ExecContext(ctx,
		`INSERT INTO plugins (name, version, manifest_hash, wasm_hash, status) VALUES ('plugin-b', '1.0', 'h2', 'w2', 'enabled')`)
	if err != nil {
		t.Fatalf("insert plugin-b: %v", err)
	}

	storeA := store.New(conn, 1)
	storeB := store.New(conn, 2)
	return storeA, storeB
}

func TestSetAndGet(t *testing.T) {
	s, _ := setupStore(t)
	ctx := context.Background()

	if err := s.Set(ctx, "key1", []byte("value1")); err != nil {
		t.Fatalf("set: %v", err)
	}
	got, err := s.Get(ctx, "key1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if string(got) != "value1" {
		t.Errorf("got %q, want value1", got)
	}
}

func TestGetNotFound(t *testing.T) {
	s, _ := setupStore(t)
	ctx := context.Background()

	_, err := s.Get(ctx, "nonexistent")
	if err != store.ErrNotFound {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

func TestSetOverwrites(t *testing.T) {
	s, _ := setupStore(t)
	ctx := context.Background()

	_ = s.Set(ctx, "key", []byte("v1"))
	_ = s.Set(ctx, "key", []byte("v2"))
	got, _ := s.Get(ctx, "key")
	if string(got) != "v2" {
		t.Errorf("got %q, want v2", got)
	}
}

func TestDelete(t *testing.T) {
	s, _ := setupStore(t)
	ctx := context.Background()

	_ = s.Set(ctx, "key", []byte("val"))
	if err := s.Delete(ctx, "key"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	_, err := s.Get(ctx, "key")
	if err != store.ErrNotFound {
		t.Errorf("after delete, err = %v, want ErrNotFound", err)
	}
}

func TestDeleteNonexistent(t *testing.T) {
	s, _ := setupStore(t)
	ctx := context.Background()

	// Should not error.
	if err := s.Delete(ctx, "nope"); err != nil {
		t.Errorf("delete nonexistent: %v", err)
	}
}

func TestList(t *testing.T) {
	s, _ := setupStore(t)
	ctx := context.Background()

	_ = s.Set(ctx, "b", []byte("2"))
	_ = s.Set(ctx, "a", []byte("1"))
	_ = s.Set(ctx, "c", []byte("3"))

	keys, err := s.List(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(keys) != 3 {
		t.Fatalf("list count = %d, want 3", len(keys))
	}
	// Should be sorted.
	if keys[0] != "a" || keys[1] != "b" || keys[2] != "c" {
		t.Errorf("keys = %v, want [a b c]", keys)
	}
}

func TestHas(t *testing.T) {
	s, _ := setupStore(t)
	ctx := context.Background()

	has, _ := s.Has(ctx, "missing")
	if has {
		t.Error("has(missing) = true, want false")
	}

	_ = s.Set(ctx, "present", []byte("x"))
	has, _ = s.Has(ctx, "present")
	if !has {
		t.Error("has(present) = false, want true")
	}
}

func TestSetRejectsLongKey(t *testing.T) {
	s, _ := setupStore(t)
	ctx := context.Background()

	longKey := make([]byte, 300)
	for i := range longKey {
		longKey[i] = 'k'
	}
	err := s.Set(ctx, string(longKey), []byte("val"))
	if err == nil {
		t.Error("expected error for key > 256 bytes")
	}
}

func TestSetRejectsLargeValue(t *testing.T) {
	s, _ := setupStore(t)
	ctx := context.Background()

	bigValue := make([]byte, 1024*1024+1) // 1MB + 1
	err := s.Set(ctx, "big", bigValue)
	if err == nil {
		t.Error("expected error for value > 1MB")
	}
}

func TestSetRejectsEmptyKey(t *testing.T) {
	s, _ := setupStore(t)
	ctx := context.Background()

	err := s.Set(ctx, "", []byte("val"))
	if err == nil {
		t.Error("expected error for empty key")
	}
}

func TestGetRejectsEmptyKey(t *testing.T) {
	s, _ := setupStore(t)
	ctx := context.Background()

	_, err := s.Get(ctx, "")
	if err == nil {
		t.Error("expected error for empty key in Get")
	}
}

func TestDeleteRejectsEmptyKey(t *testing.T) {
	s, _ := setupStore(t)
	ctx := context.Background()

	err := s.Delete(ctx, "")
	if err == nil {
		t.Error("expected error for empty key in Delete")
	}
}

func TestHasRejectsEmptyKey(t *testing.T) {
	s, _ := setupStore(t)
	ctx := context.Background()

	_, err := s.Has(ctx, "")
	if err == nil {
		t.Error("expected error for empty key in Has")
	}
}

func TestStoreIsolation(t *testing.T) {
	storeA, storeB := setupStore(t)
	ctx := context.Background()

	_ = storeA.Set(ctx, "secret", []byte("a-only"))

	// Plugin B should not see plugin A's keys.
	_, err := storeB.Get(ctx, "secret")
	if err != store.ErrNotFound {
		t.Errorf("plugin B should not see plugin A's key, err = %v", err)
	}

	keys, _ := storeB.List(ctx)
	if len(keys) != 0 {
		t.Errorf("plugin B keys = %v, want empty", keys)
	}
}
