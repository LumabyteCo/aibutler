package hybrid_test

import (
	"context"
	"testing"

	"github.com/LumabyteCo/aibutler/internal/memory/entity"
	"github.com/LumabyteCo/aibutler/internal/memory/fts"
	"github.com/LumabyteCo/aibutler/internal/memory/hybrid"
	"github.com/LumabyteCo/aibutler/testutil"
)

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
