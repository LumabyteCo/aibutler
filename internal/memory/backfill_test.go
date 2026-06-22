package memory_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/LumabyteCo/aibutler/internal/memory"
	"github.com/LumabyteCo/aibutler/internal/memory/vector"
	"github.com/LumabyteCo/aibutler/testutil"
)

// fakeBatchEmbedder returns one deterministic, non-empty vector per input and
// counts how many batches it was called with.
type fakeBatchEmbedder struct {
	dim   int
	calls int
}

func (f *fakeBatchEmbedder) embedBatch(_ context.Context, texts []string) ([][]float32, error) {
	f.calls++
	out := make([][]float32, len(texts))
	for i := range texts {
		v := make([]float32, f.dim)
		if f.dim > 0 {
			v[0] = float32(len(texts[i])) + 1 // non-zero
		}
		out[i] = v
	}
	return out, nil
}

func TestBackfillEmbedsMissingItems(t *testing.T) {
	ctx := context.Background()
	db := testutil.TestDB(t)
	conn := db.Conn()
	vecStore := vector.NewStore(conn)

	for _, c := range []string{"first thought", "second thought", "third"} {
		if _, err := conn.ExecContext(ctx,
			`INSERT INTO captured_thoughts (content, source, created_at) VALUES (?, 'user', '2026-01-01T00:00:00Z')`, c); err != nil {
			t.Fatalf("insert thought: %v", err)
		}
	}
	for _, c := range []string{"hello there", "general kenobi"} {
		if _, err := conn.ExecContext(ctx,
			`INSERT INTO session_transcripts (session_id, role, content, turn_number, created_at) VALUES ('s1','user',?,0,'2026-01-01T00:00:00Z')`, c); err != nil {
			t.Fatalf("insert transcript: %v", err)
		}
	}

	fe := &fakeBatchEmbedder{dim: 8}
	bf := memory.NewBackfiller(conn, vecStore, fe.embedBatch, "test-model")

	res, err := bf.BackfillMissing(ctx)
	if err != nil {
		t.Fatalf("backfill: %v", err)
	}
	if res.Embedded != 5 {
		t.Errorf("Embedded = %d, want 5", res.Embedded)
	}
	if res.BySource["thought"] != 3 || res.BySource["transcript"] != 2 {
		t.Errorf("BySource = %v, want thought:3 transcript:2", res.BySource)
	}
	if n, _ := vecStore.Count(ctx); n != 5 {
		t.Errorf("vector count = %d, want 5", n)
	}

	// Idempotent: a second run finds nothing missing and embeds nothing new.
	res2, err := bf.BackfillMissing(ctx)
	if err != nil {
		t.Fatalf("backfill #2: %v", err)
	}
	if res2.Embedded != 0 {
		t.Errorf("second run Embedded = %d, want 0 (idempotent)", res2.Embedded)
	}
	if n, _ := vecStore.Count(ctx); n != 5 {
		t.Errorf("vector count after re-run = %d, want 5", n)
	}
}

func TestBackfillBatchesLargeSets(t *testing.T) {
	ctx := context.Background()
	db := testutil.TestDB(t)
	conn := db.Conn()
	vecStore := vector.NewStore(conn)

	const n = 150 // > defaultBackfillBatch (64) → multiple provider calls
	for i := 0; i < n; i++ {
		if _, err := conn.ExecContext(ctx,
			`INSERT INTO captured_thoughts (content, source, created_at) VALUES (?, 'user', '2026-01-01T00:00:00Z')`,
			fmt.Sprintf("thought number %d", i)); err != nil {
			t.Fatalf("insert: %v", err)
		}
	}

	fe := &fakeBatchEmbedder{dim: 4}
	bf := memory.NewBackfiller(conn, vecStore, fe.embedBatch, "m")
	res, err := bf.BackfillMissing(ctx)
	if err != nil {
		t.Fatalf("backfill: %v", err)
	}
	if res.Embedded != n {
		t.Errorf("Embedded = %d, want %d", res.Embedded, n)
	}
	if fe.calls < 2 {
		t.Errorf("expected multiple batched embed calls for %d items, got %d", n, fe.calls)
	}
}

// errEmbedder always fails, to verify a failed batch is counted (not embedded)
// and never aborts the run.
type errEmbedder struct{}

func (errEmbedder) embedBatch(_ context.Context, _ []string) ([][]float32, error) {
	return nil, fmt.Errorf("provider down")
}

func TestBackfillCountsFailures(t *testing.T) {
	ctx := context.Background()
	db := testutil.TestDB(t)
	conn := db.Conn()
	vecStore := vector.NewStore(conn)

	if _, err := conn.ExecContext(ctx,
		`INSERT INTO captured_thoughts (content, source, created_at) VALUES ('x','user','2026-01-01T00:00:00Z')`); err != nil {
		t.Fatalf("insert: %v", err)
	}

	bf := memory.NewBackfiller(conn, vecStore, errEmbedder{}.embedBatch, "m")
	res, err := bf.BackfillMissing(ctx)
	if err != nil {
		t.Fatalf("backfill should not return a hard error on embed failure: %v", err)
	}
	if res.Embedded != 0 || res.Failed != 1 {
		t.Errorf("got Embedded=%d Failed=%d, want 0/1", res.Embedded, res.Failed)
	}
	if n, _ := vecStore.Count(ctx); n != 0 {
		t.Errorf("vector count = %d, want 0 (embed failed)", n)
	}
}
