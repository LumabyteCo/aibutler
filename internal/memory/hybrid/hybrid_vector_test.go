package hybrid_test

import (
	"context"
	"testing"

	"github.com/LumabyteCo/aibutler/internal/memory/entity"
	"github.com/LumabyteCo/aibutler/internal/memory/fts"
	"github.com/LumabyteCo/aibutler/internal/memory/hybrid"
	"github.com/LumabyteCo/aibutler/internal/memory/vector"
	"github.com/LumabyteCo/aibutler/testutil"
)

// fakeVectorSearcher implements hybrid.VectorSearcher for testing.
type fakeVectorSearcher struct {
	results []vector.SearchResult
}

func (f *fakeVectorSearcher) Search(_ context.Context, _ []float32, _ int) ([]vector.SearchResult, error) {
	return f.results, nil
}

// fakeEmbedFunc returns a deterministic embedding for testing.
func fakeEmbedFunc(_ context.Context, _ string) ([]float32, error) {
	return []float32{0.1, 0.2, 0.3, 0.4}, nil
}

func TestSearchWithVector(t *testing.T) {
	db := testutil.TestDB(t)
	conn := db.Conn()
	ctx := context.Background()

	ftsStore := fts.NewStore(conn)
	entityStore := entity.NewStore(conn)
	s := hybrid.NewSearcher(ftsStore, entityStore)

	// Insert a thought via FTS.
	conn.ExecContext(ctx, `INSERT INTO captured_thoughts (content, source, created_at)
		VALUES ('Go concurrency patterns are useful', 'user', '2026-01-01T00:00:00Z')`)

	// Insert an entity.
	entityStore.SaveOrUpdate(ctx, entity.TypePerson, "Sarah", "", nil)

	// Enable vector search with fake results. Vector hits are fused on rank and
	// canonicalized to their underlying source type (no "vector:" prefix); a hit
	// that also matches FTS merges into one result rather than duplicating.
	fakeVec := &fakeVectorSearcher{
		results: []vector.SearchResult{
			{SourceType: "thought", SourceID: 1, Distance: 0.2},
			{SourceType: "transcript", SourceID: 5, Distance: 0.8},
		},
	}
	s.SetVectorSearch(fakeVec, fakeEmbedFunc)

	results, err := s.Search(ctx, "Go patterns", 20)
	if err != nil {
		t.Fatalf("search: %v", err)
	}

	// Should have results from FTS plus the vector-only transcript hit.
	if len(results) < 2 {
		t.Fatalf("got %d results, want >= 2 (FTS + vector)", len(results))
	}

	// The transcript hit (id 5) can only originate from the vector backend —
	// nothing was inserted into session_transcripts — so its presence proves
	// vector results are fused into the output.
	hasVector := false
	for _, r := range results {
		if r.Source == "transcript" && r.ID == 5 {
			hasVector = true
			break
		}
	}
	if !hasVector {
		t.Error("expected the vector-only transcript hit (id 5) in fused output")
	}

	// Results should be sorted by score descending.
	for i := 1; i < len(results); i++ {
		if results[i].Score > results[i-1].Score {
			t.Errorf("results not sorted: [%d].Score=%f > [%d].Score=%f",
				i, results[i].Score, i-1, results[i-1].Score)
		}
	}
}

func TestSearchVectorOnly(t *testing.T) {
	db := testutil.TestDB(t)
	conn := db.Conn()
	_ = conn // ensure db is initialized

	s := hybrid.NewSearcher(nil, nil)

	fakeVec := &fakeVectorSearcher{
		results: []vector.SearchResult{
			{SourceType: "thought", SourceID: 1, Distance: 0.1},
		},
	}
	s.SetVectorSearch(fakeVec, fakeEmbedFunc)

	results, err := s.Search(context.Background(), "test", 10)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("got %d results, want 1", len(results))
	}
	// Vector hits canonicalize to their underlying type; no "vector:" prefix.
	if results[0].Source != "thought" {
		t.Errorf("source = %q, want thought", results[0].Source)
	}
	// Single backend, rank 1 → RRF score 1/(k+1) with k=60.
	want := 1.0 / (60.0 + 1.0)
	if results[0].Score < want-1e-9 || results[0].Score > want+1e-9 {
		t.Errorf("score = %f, want %f (RRF, k=60, rank 1)", results[0].Score, want)
	}
}

func TestSearchVectorDisabledWithoutEmbed(t *testing.T) {
	db := testutil.TestDB(t)
	conn := db.Conn()
	ctx := context.Background()

	ftsStore := fts.NewStore(conn)
	s := hybrid.NewSearcher(ftsStore, nil)

	conn.ExecContext(ctx, `INSERT INTO captured_thoughts (content, source, created_at)
		VALUES ('test content', 'user', '2026-01-01T00:00:00Z')`)

	// Set vector searcher but NO embed function — vector search should be skipped.
	fakeVec := &fakeVectorSearcher{
		results: []vector.SearchResult{
			{SourceType: "thought", SourceID: 99, Distance: 0.1},
		},
	}
	s.SetVectorSearch(fakeVec, nil)

	results, err := s.Search(ctx, "test", 10)
	if err != nil {
		t.Fatalf("search: %v", err)
	}

	// Should only have FTS results; the vector-only id (99) must not appear.
	for _, r := range results {
		if r.ID == 99 {
			t.Error("vector results should not appear when embed function is nil")
		}
	}
}

func TestSearchThreeSourcesCombined(t *testing.T) {
	db := testutil.TestDB(t)
	conn := db.Conn()
	ctx := context.Background()

	ftsStore := fts.NewStore(conn)
	entityStore := entity.NewStore(conn)
	s := hybrid.NewSearcher(ftsStore, entityStore)

	// FTS source.
	conn.ExecContext(ctx, `INSERT INTO captured_thoughts (content, source, created_at)
		VALUES ('Sarah presented the project roadmap', 'user', '2026-01-01T00:00:00Z')`)

	// Entity source.
	entityStore.SaveOrUpdate(ctx, entity.TypePerson, "Sarah", "", nil)

	// Vector source: a transcript hit, which no other backend can produce here
	// (nothing was inserted into session_transcripts), so it uniquely proves the
	// vector backend contributed to the fused output.
	fakeVec := &fakeVectorSearcher{
		results: []vector.SearchResult{
			{SourceType: "transcript", SourceID: 7, Distance: 0.3},
		},
	}
	s.SetVectorSearch(fakeVec, fakeEmbedFunc)

	results, err := s.Search(ctx, "Sarah", 20)
	if err != nil {
		t.Fatalf("search: %v", err)
	}

	// All three backends should be represented (vector surfaces as "transcript").
	sources := make(map[string]bool)
	for _, r := range results {
		sources[r.Source] = true
	}
	if !sources["thought"] {
		t.Error("missing FTS thought result")
	}
	if !sources["entity"] {
		t.Error("missing entity result")
	}
	if !sources["transcript"] {
		t.Error("missing vector-contributed transcript result")
	}
}
