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

// stubResolver maps (sourceType, id) → content for hydration tests.
type stubResolver struct{ byType map[string]map[int64]string }

func (r *stubResolver) ResolveContent(_ context.Context, sourceType string, ids []int64) (map[int64]string, error) {
	out := make(map[int64]string)
	m, ok := r.byType[sourceType]
	if !ok {
		return out, nil
	}
	for _, id := range ids {
		if c, ok := m[id]; ok {
			out[id] = c
		}
	}
	return out, nil
}

func setup(t *testing.T) (*hybrid.Searcher, *fts.Store, *entity.Store) {
	t.Helper()
	db := testutil.TestDB(t)
	conn := db.Conn()
	ftsStore := fts.NewStore(conn)
	entityStore := entity.NewStore(conn)
	return hybrid.NewSearcher(ftsStore, entityStore), ftsStore, entityStore
}

func TestSearchEmpty(t *testing.T) {
	s, _, _ := setup(t)
	ctx := context.Background()

	results, err := s.Search(ctx, "anything", 10)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("got %d results, want 0", len(results))
	}
}

func TestSearchEmptyQuery(t *testing.T) {
	s, _, _ := setup(t)
	ctx := context.Background()

	results, err := s.Search(ctx, "", 10)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if results != nil {
		t.Errorf("got %v, want nil", results)
	}
}

func TestSearchFTSOnly(t *testing.T) {
	db := testutil.TestDB(t)
	conn := db.Conn()
	ctx := context.Background()

	ftsStore := fts.NewStore(conn)
	s := hybrid.NewSearcher(ftsStore, nil)

	conn.ExecContext(ctx, `INSERT INTO captured_thoughts (content, source, created_at) VALUES ('Go concurrency patterns', 'user', '2026-01-01T00:00:00Z')`)

	results, err := s.Search(ctx, "Go", 10)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("got %d results, want 1", len(results))
	}
	if results[0].Source != "thought" {
		t.Errorf("source = %q, want thought", results[0].Source)
	}
}

func TestSearchEntityOnly(t *testing.T) {
	db := testutil.TestDB(t)
	conn := db.Conn()
	ctx := context.Background()

	entityStore := entity.NewStore(conn)
	s := hybrid.NewSearcher(nil, entityStore)

	entityStore.SaveOrUpdate(ctx, entity.TypePerson, "Sarah Connor", "", nil)

	results, err := s.Search(ctx, "Sarah", 10)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("got %d results, want 1", len(results))
	}
	if results[0].Source != "entity" {
		t.Errorf("source = %q, want entity", results[0].Source)
	}
	if results[0].Type != "person" {
		t.Errorf("type = %q, want person", results[0].Type)
	}
}

func TestSearchCombined(t *testing.T) {
	db := testutil.TestDB(t)
	conn := db.Conn()
	ctx := context.Background()

	ftsStore := fts.NewStore(conn)
	entityStore := entity.NewStore(conn)
	s := hybrid.NewSearcher(ftsStore, entityStore)

	// Thought about Sarah.
	conn.ExecContext(ctx, `INSERT INTO captured_thoughts (content, source, created_at) VALUES ('Met with Sarah about the project', 'user', '2026-01-01T00:00:00Z')`)
	// Entity for Sarah.
	entityStore.SaveOrUpdate(ctx, entity.TypePerson, "Sarah", "", nil)

	results, err := s.Search(ctx, "Sarah", 10)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(results) < 2 {
		t.Fatalf("got %d results, want >= 2", len(results))
	}

	// Check both sources are present.
	sources := make(map[string]bool)
	for _, r := range results {
		sources[r.Source] = true
	}
	if !sources["thought"] || !sources["entity"] {
		t.Errorf("expected both thought and entity sources, got %v", sources)
	}
}

func TestSearchLimit(t *testing.T) {
	db := testutil.TestDB(t)
	conn := db.Conn()
	ctx := context.Background()

	ftsStore := fts.NewStore(conn)
	entityStore := entity.NewStore(conn)
	s := hybrid.NewSearcher(ftsStore, entityStore)

	for i := 0; i < 10; i++ {
		conn.ExecContext(ctx, `INSERT INTO captured_thoughts (content, source, created_at) VALUES ('Go patterns discussion', 'user', '2026-01-01T00:00:00Z')`)
	}

	results, err := s.Search(ctx, "Go", 3)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(results) != 3 {
		t.Errorf("got %d results, want 3", len(results))
	}
}

func TestSearchSortedByScore(t *testing.T) {
	db := testutil.TestDB(t)
	conn := db.Conn()
	ctx := context.Background()

	ftsStore := fts.NewStore(conn)
	entityStore := entity.NewStore(conn)
	s := hybrid.NewSearcher(ftsStore, entityStore)

	conn.ExecContext(ctx, `INSERT INTO captured_thoughts (content, source, created_at) VALUES ('Go programming', 'user', '2026-01-01T00:00:00Z')`)
	entityStore.SaveOrUpdate(ctx, entity.TypeProject, "Go Project", "", nil)

	results, err := s.Search(ctx, "Go", 10)
	if err != nil {
		t.Fatalf("search: %v", err)
	}

	// Should be sorted by score descending.
	for i := 1; i < len(results); i++ {
		if results[i].Score > results[i-1].Score {
			t.Errorf("results not sorted: [%d].Score=%f > [%d].Score=%f",
				i, results[i].Score, i-1, results[i-1].Score)
		}
	}
}

// TestDedupAndRRFBoost verifies the two new fusion guarantees: an item surfaced
// by more than one backend (a) collapses to a single result (dedup), and
// (b) outranks a single-backend item because its RRF contributions sum.
func TestDedupAndRRFBoost(t *testing.T) {
	db := testutil.TestDB(t)
	conn := db.Conn()
	ctx := context.Background()

	ftsStore := fts.NewStore(conn)
	s := hybrid.NewSearcher(ftsStore, nil)

	// Two thoughts both matching "Go".
	conn.ExecContext(ctx, `INSERT INTO captured_thoughts (content, source, created_at) VALUES ('Go alpha note', 'user', '2026-01-01T00:00:00Z')`)
	conn.ExecContext(ctx, `INSERT INTO captured_thoughts (content, source, created_at) VALUES ('Go beta note', 'user', '2026-01-01T00:00:00Z')`)

	rows, err := conn.QueryContext(ctx, `SELECT id FROM captured_thoughts ORDER BY id`)
	if err != nil {
		t.Fatalf("query ids: %v", err)
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			t.Fatalf("scan id: %v", err)
		}
		ids = append(ids, id)
	}
	rows.Close()
	if len(ids) != 2 {
		t.Fatalf("expected 2 thoughts, got %d", len(ids))
	}
	id1, id2 := ids[0], ids[1]

	// Vector also returns id2, so id2 is found by BOTH FTS and vector.
	s.SetVectorSearch(&fakeVectorSearcher{results: []vector.SearchResult{
		{SourceType: "thought", SourceID: id2, Distance: 0.05},
	}}, fakeEmbedFunc)

	results, err := s.Search(ctx, "Go", 10)
	if err != nil {
		t.Fatalf("search: %v", err)
	}

	// Dedup: id2 appears in FTS and vector but must be ONE result → 2 total, not 3.
	if len(results) != 2 {
		t.Fatalf("got %d results, want 2 (id2 must be deduped across FTS+vector)", len(results))
	}
	// Boost: id2 (two backends) outranks id1 (one backend), regardless of BM25 order.
	if results[0].ID != id2 {
		t.Errorf("top result ID = %d, want %d (the item corroborated by FTS + vector)", results[0].ID, id2)
	}
	if results[1].ID != id1 {
		t.Errorf("second result ID = %d, want %d", results[1].ID, id1)
	}
	if results[0].Score <= results[1].Score {
		t.Errorf("expected boosted score for double-matched item: %f should exceed %f", results[0].Score, results[1].Score)
	}
	// Content for id2 comes from FTS (never blank), even though vector also matched it.
	if results[0].Content == "" {
		t.Error("top result content is empty; expected FTS content to win over the empty vector hit")
	}
}

// TestVectorContentHydration verifies a vector-only hit gets its Content filled
// in via the ContentResolver (it otherwise reaches callers blank).
func TestVectorContentHydration(t *testing.T) {
	ctx := context.Background()
	s := hybrid.NewSearcher(nil, nil) // no FTS, no entity — vector only

	s.SetVectorSearch(&fakeVectorSearcher{results: []vector.SearchResult{
		{SourceType: "thought", SourceID: 42, Distance: 0.1},
	}}, fakeEmbedFunc)
	s.SetContentResolver(&stubResolver{byType: map[string]map[int64]string{
		"thought": {42: "hydrated content"},
	}})

	results, err := s.Search(ctx, "anything", 10)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("got %d results, want 1", len(results))
	}
	if results[0].Source != "thought" {
		t.Errorf("source = %q, want thought (canonicalized from vector hit)", results[0].Source)
	}
	if results[0].Content != "hydrated content" {
		t.Errorf("content = %q, want %q", results[0].Content, "hydrated content")
	}
}

// TestVectorNoResolverEmptyContent confirms no regression / no panic when a
// vector-only hit has no resolver wired: it returns with empty Content.
func TestVectorNoResolverEmptyContent(t *testing.T) {
	ctx := context.Background()
	s := hybrid.NewSearcher(nil, nil)
	s.SetVectorSearch(&fakeVectorSearcher{results: []vector.SearchResult{
		{SourceType: "thought", SourceID: 7, Distance: 0.2},
	}}, fakeEmbedFunc)

	results, err := s.Search(ctx, "x", 10)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("got %d results, want 1", len(results))
	}
	if results[0].Content != "" {
		t.Errorf("content = %q, want empty (no resolver wired)", results[0].Content)
	}
}
