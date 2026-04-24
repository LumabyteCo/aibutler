package fts_test

import (
	"context"
	"testing"

	"github.com/LumabyteCo/aibutler/internal/memory/fts"
	"github.com/LumabyteCo/aibutler/testutil"
)

func newFTSStore(t *testing.T) *fts.Store {
	t.Helper()
	db := testutil.TestDB(t)
	return fts.NewStore(db.Conn())
}

func TestSearchThoughtsEmpty(t *testing.T) {
	store := newFTSStore(t)
	ctx := context.Background()

	results, err := store.SearchThoughts(ctx, "anything", 10)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("got %d results, want 0", len(results))
	}
}

func TestSearchThoughtsEmptyQuery(t *testing.T) {
	store := newFTSStore(t)
	ctx := context.Background()

	results, err := store.SearchThoughts(ctx, "", 10)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if results != nil {
		t.Errorf("got %v, want nil", results)
	}
}

func TestSearchThoughtsFindsMatch(t *testing.T) {
	db := testutil.TestDB(t)
	conn := db.Conn()
	store := fts.NewStore(conn)
	ctx := context.Background()

	// Insert thoughts (trigger auto-populates FTS).
	conn.ExecContext(ctx, `INSERT INTO captured_thoughts (content, source, created_at) VALUES (?, 'user', '2026-01-01T00:00:00Z')`, "Learning Go programming language")
	conn.ExecContext(ctx, `INSERT INTO captured_thoughts (content, source, created_at) VALUES (?, 'user', '2026-01-02T00:00:00Z')`, "Buy groceries for dinner")
	conn.ExecContext(ctx, `INSERT INTO captured_thoughts (content, source, created_at) VALUES (?, 'user', '2026-01-03T00:00:00Z')`, "Go concurrency patterns are powerful")

	results, err := store.SearchThoughts(ctx, "Go programming", 10)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected results for 'Go programming'")
	}
	for _, r := range results {
		if r.Source != "thought" {
			t.Errorf("source = %q, want 'thought'", r.Source)
		}
		if r.ID == 0 {
			t.Error("expected non-zero ID")
		}
	}
}

func TestSearchThoughtsLimit(t *testing.T) {
	db := testutil.TestDB(t)
	conn := db.Conn()
	store := fts.NewStore(conn)
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		conn.ExecContext(ctx, `INSERT INTO captured_thoughts (content, source, created_at) VALUES (?, 'user', '2026-01-01T00:00:00Z')`, "Go patterns for concurrency")
	}

	results, err := store.SearchThoughts(ctx, "Go", 2)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(results) > 2 {
		t.Errorf("got %d results, want <= 2", len(results))
	}
}

func TestSearchThoughtsDefaultLimit(t *testing.T) {
	db := testutil.TestDB(t)
	conn := db.Conn()
	store := fts.NewStore(conn)
	ctx := context.Background()

	for i := 0; i < 25; i++ {
		conn.ExecContext(ctx, `INSERT INTO captured_thoughts (content, source, created_at) VALUES (?, 'user', '2026-01-01T00:00:00Z')`, "Go patterns")
	}

	results, err := store.SearchThoughts(ctx, "Go", 0)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(results) > 20 {
		t.Errorf("default limit should cap at 20, got %d", len(results))
	}
}

func TestSearchTranscripts(t *testing.T) {
	db := testutil.TestDB(t)
	conn := db.Conn()
	store := fts.NewStore(conn)
	ctx := context.Background()

	// Insert transcripts (trigger auto-populates FTS).
	conn.ExecContext(ctx, `INSERT INTO session_transcripts (session_id, role, content, turn_number, created_at) VALUES ('s1', 'user', 'Tell me about Kubernetes', 1, '2026-01-01T00:00:00Z')`)
	conn.ExecContext(ctx, `INSERT INTO session_transcripts (session_id, role, content, turn_number, created_at) VALUES ('s1', 'assistant', 'Kubernetes is a container orchestrator', 2, '2026-01-01T00:00:01Z')`)
	conn.ExecContext(ctx, `INSERT INTO session_transcripts (session_id, role, content, turn_number, created_at) VALUES ('s1', 'user', 'How about Docker', 3, '2026-01-01T00:00:02Z')`)

	results, err := store.SearchTranscripts(ctx, "Kubernetes", 10)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected results for 'Kubernetes'")
	}
	for _, r := range results {
		if r.Source != "transcript" {
			t.Errorf("source = %q, want 'transcript'", r.Source)
		}
	}
}

func TestSearchAll(t *testing.T) {
	db := testutil.TestDB(t)
	conn := db.Conn()
	store := fts.NewStore(conn)
	ctx := context.Background()

	conn.ExecContext(ctx, `INSERT INTO captured_thoughts (content, source, created_at) VALUES ('Kubernetes deployment strategy', 'user', '2026-01-01T00:00:00Z')`)
	conn.ExecContext(ctx, `INSERT INTO session_transcripts (session_id, role, content, turn_number, created_at) VALUES ('s1', 'user', 'Explain Kubernetes pods', 1, '2026-01-01T00:00:00Z')`)

	results, err := store.SearchAll(ctx, "Kubernetes", 10)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(results) < 2 {
		t.Fatalf("got %d results, want >= 2", len(results))
	}

	// Should have both sources.
	sources := make(map[string]bool)
	for _, r := range results {
		sources[r.Source] = true
	}
	if !sources["thought"] || !sources["transcript"] {
		t.Errorf("expected both thought and transcript sources, got %v", sources)
	}
}

func TestSearchAllRanking(t *testing.T) {
	db := testutil.TestDB(t)
	conn := db.Conn()
	store := fts.NewStore(conn)
	ctx := context.Background()

	// Insert 3 thoughts and 2 transcripts all matching "database".
	conn.ExecContext(ctx, `INSERT INTO captured_thoughts (content, source, created_at) VALUES ('SQLite database optimization', 'user', '2026-01-01T00:00:00Z')`)
	conn.ExecContext(ctx, `INSERT INTO captured_thoughts (content, source, created_at) VALUES ('database migration patterns', 'user', '2026-01-02T00:00:00Z')`)
	conn.ExecContext(ctx, `INSERT INTO session_transcripts (session_id, role, content, turn_number, created_at) VALUES ('s1', 'user', 'database indexing best practices', 1, '2026-01-01T00:00:00Z')`)

	results, err := store.SearchAll(ctx, "database", 10)
	if err != nil {
		t.Fatalf("search: %v", err)
	}

	// Results should be sorted by rank (ascending = more relevant first).
	for i := 1; i < len(results); i++ {
		if results[i].Rank < results[i-1].Rank {
			t.Errorf("results not sorted by rank: [%d].Rank=%f < [%d].Rank=%f",
				i, results[i].Rank, i-1, results[i-1].Rank)
		}
	}
}

func TestSearchAllLimit(t *testing.T) {
	db := testutil.TestDB(t)
	conn := db.Conn()
	store := fts.NewStore(conn)
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		conn.ExecContext(ctx, `INSERT INTO captured_thoughts (content, source, created_at) VALUES ('Go patterns', 'user', '2026-01-01T00:00:00Z')`)
		conn.ExecContext(ctx, `INSERT INTO session_transcripts (session_id, role, content, turn_number, created_at) VALUES ('s1', 'user', 'Go concurrency', ?, '2026-01-01T00:00:00Z')`, i)
	}

	results, err := store.SearchAll(ctx, "Go", 3)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(results) != 3 {
		t.Errorf("got %d results, want 3", len(results))
	}
}

func TestIndexExistingThoughts(t *testing.T) {
	db := testutil.TestDB(t)
	conn := db.Conn()
	store := fts.NewStore(conn)
	ctx := context.Background()

	// Disable the trigger temporarily and insert directly.
	conn.ExecContext(ctx, `DROP TRIGGER IF EXISTS captured_thoughts_fts_insert`)
	conn.ExecContext(ctx, `INSERT INTO captured_thoughts (content, source, created_at) VALUES ('pre-existing thought about Rust', 'user', '2026-01-01T00:00:00Z')`)

	// Should not be findable yet.
	results, _ := store.SearchThoughts(ctx, "Rust", 10)
	if len(results) != 0 {
		t.Fatal("expected 0 results before indexing")
	}

	// Re-create trigger and run backfill.
	conn.ExecContext(ctx, `CREATE TRIGGER captured_thoughts_fts_insert AFTER INSERT ON captured_thoughts BEGIN INSERT INTO captured_thoughts_fts(rowid, content) VALUES (new.id, new.content); END`)
	if err := store.IndexExistingThoughts(ctx); err != nil {
		t.Fatalf("index: %v", err)
	}

	results, err := store.SearchThoughts(ctx, "Rust", 10)
	if err != nil {
		t.Fatalf("search after index: %v", err)
	}
	if len(results) != 1 {
		t.Errorf("got %d results, want 1", len(results))
	}
}
