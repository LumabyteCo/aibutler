package memory_test

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/LumabyteCo/aibutler/internal/memory"
	"github.com/LumabyteCo/aibutler/testutil"
)

// mockIndexer is a VectorIndexer test double. If block is non-nil, IndexContent
// waits on it (or on ctx cancellation) before recording the call — letting tests
// hold the worker to exercise non-blocking saves and queue-full drops.
type mockIndexer struct {
	mu    sync.Mutex
	calls []string
	block chan struct{}
}

func (m *mockIndexer) IndexContent(ctx context.Context, source string, id int64, content string) error {
	if m.block != nil {
		select {
		case <-m.block:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	m.mu.Lock()
	m.calls = append(m.calls, fmt.Sprintf("%s/%d", source, id))
	m.mu.Unlock()
	return nil
}

func (m *mockIndexer) callCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.calls)
}

func newMemStore(t *testing.T) *memory.Store {
	t.Helper()
	db := testutil.TestDB(t)
	return memory.NewStore(db.Conn())
}

// TestAsyncIndexerProcessesSaves: every saved item is embedded in the background,
// and Close drains the queue deterministically.
func TestAsyncIndexerProcessesSaves(t *testing.T) {
	ctx := context.Background()
	s := newMemStore(t)
	mi := &mockIndexer{}
	s.SetIndexer(mi)

	for i := 0; i < 3; i++ {
		if _, err := s.SaveThought(ctx, fmt.Sprintf("thought %d", i), "user", "", nil); err != nil {
			t.Fatalf("save %d: %v", i, err)
		}
	}

	s.Close() // drains queued work, then waits for the worker to exit

	if got := mi.callCount(); got != 3 {
		t.Errorf("indexed %d items, want 3", got)
	}
	st := s.IndexStats()
	if st.Queued != 3 {
		t.Errorf("stats.Queued = %d, want 3", st.Queued)
	}
	if st.Indexed != 3 {
		t.Errorf("stats.Indexed = %d, want 3", st.Indexed)
	}
	if st.Dropped != 0 || st.Failed != 0 {
		t.Errorf("unexpected dropped/failed: %+v", st)
	}
}

// TestSaveDoesNotBlockOnSlowEmbedding: a save returns promptly even while the
// embedding call is stuck — the whole point of making indexing async. With the
// old synchronous path this would block until the (never-completing) embed.
func TestSaveDoesNotBlockOnSlowEmbedding(t *testing.T) {
	ctx := context.Background()
	s := newMemStore(t)
	mi := &mockIndexer{block: make(chan struct{})}
	s.SetIndexer(mi)

	start := time.Now()
	if _, err := s.SaveThought(ctx, "slow to embed", "user", "", nil); err != nil {
		t.Fatalf("save: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Errorf("SaveThought blocked for %v; expected near-instant (async)", elapsed)
	}

	close(mi.block) // let the in-flight job complete, then shut down cleanly
	s.Close()
}

// TestDropsWhenQueueFull: with the worker wedged on the first job, the bounded
// queue fills and further saves are dropped (and counted) rather than blocking.
// Queued + Dropped accounts for every save.
func TestDropsWhenQueueFull(t *testing.T) {
	ctx := context.Background()
	s := newMemStore(t)
	mi := &mockIndexer{block: make(chan struct{})}
	s.SetIndexer(mi)

	const n = 400
	for i := 0; i < n; i++ {
		if _, err := s.SaveThought(ctx, fmt.Sprintf("t%d", i), "user", "", nil); err != nil {
			t.Fatalf("save %d: %v", i, err)
		}
	}

	st := s.IndexStats()
	if st.Dropped == 0 {
		t.Errorf("expected drops with a full queue, got Dropped=0 (Queued=%d)", st.Queued)
	}
	if st.Queued+st.Dropped != int64(n) {
		t.Errorf("Queued(%d)+Dropped(%d)=%d, want %d", st.Queued, st.Dropped, st.Queued+st.Dropped, n)
	}

	close(mi.block)
	s.Close()
}

// TestCloseIdempotentAndNoIndexer: with no indexer wired, saves succeed, nothing
// is queued, and the worker never starts. Close is safe and idempotent.
func TestCloseIdempotentAndNoIndexer(t *testing.T) {
	ctx := context.Background()
	s := newMemStore(t)

	if _, err := s.SaveThought(ctx, "no indexer wired", "user", "", nil); err != nil {
		t.Fatalf("save: %v", err)
	}
	if st := s.IndexStats(); st.Queued != 0 || st.Pending != 0 {
		t.Errorf("expected nothing queued/pending without an indexer, got %+v", st)
	}

	s.Close()
	s.Close() // idempotent — must not panic
}
