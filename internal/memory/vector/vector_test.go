package vector_test

import (
	"context"
	"testing"

	"github.com/LumabyteCo/aibutler/internal/memory/vector"
	"github.com/LumabyteCo/aibutler/testutil"
)

func TestSaveAndSearch(t *testing.T) {
	db := testutil.TestDB(t)
	s := vector.NewStore(db.Conn())
	ctx := context.Background()

	// Save embeddings for two thoughts.
	err := s.Save(ctx, "thought", 1, []float32{1, 0, 0}, "test-model")
	if err != nil {
		t.Fatalf("save 1: %v", err)
	}
	err = s.Save(ctx, "thought", 2, []float32{0, 1, 0}, "test-model")
	if err != nil {
		t.Fatalf("save 2: %v", err)
	}

	// Search for nearest to [1, 0, 0] — should find thought 1 first.
	results, err := s.Search(ctx, []float32{1, 0, 0}, 10)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("got %d results, want 2", len(results))
	}
	if results[0].SourceID != 1 {
		t.Errorf("nearest = source_id %d, want 1", results[0].SourceID)
	}
	if results[0].SourceType != "thought" {
		t.Errorf("source_type = %q, want thought", results[0].SourceType)
	}
	if results[0].Distance > 0.01 {
		t.Errorf("distance to identical = %f, want ~0", results[0].Distance)
	}
}

func TestSearchEmpty(t *testing.T) {
	db := testutil.TestDB(t)
	s := vector.NewStore(db.Conn())
	ctx := context.Background()

	results, err := s.Search(ctx, []float32{1, 0, 0}, 10)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("got %d results, want 0", len(results))
	}
}

func TestSearchEmptyQuery(t *testing.T) {
	db := testutil.TestDB(t)
	s := vector.NewStore(db.Conn())
	ctx := context.Background()

	results, err := s.Search(ctx, nil, 10)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if results != nil {
		t.Errorf("got %v, want nil", results)
	}
}

func TestSearchByType(t *testing.T) {
	db := testutil.TestDB(t)
	s := vector.NewStore(db.Conn())
	ctx := context.Background()

	s.Save(ctx, "thought", 1, []float32{1, 0, 0}, "test-model")
	s.Save(ctx, "transcript", 2, []float32{0.9, 0.1, 0}, "test-model")
	s.Save(ctx, "entity", 3, []float32{0.8, 0.2, 0}, "test-model")

	// Search only thoughts.
	results, err := s.SearchByType(ctx, []float32{1, 0, 0}, "thought", 10)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("got %d results, want 1", len(results))
	}
	if results[0].SourceType != "thought" {
		t.Errorf("source_type = %q, want thought", results[0].SourceType)
	}
}

func TestUpsert(t *testing.T) {
	db := testutil.TestDB(t)
	s := vector.NewStore(db.Conn())
	ctx := context.Background()

	// Upsert refuses to write an embedding whose source row is gone (late
	// async jobs must not resurrect deleted content), so the thought row has
	// to exist for the update path under test to run.
	if _, err := db.Conn().ExecContext(ctx,
		`INSERT INTO captured_thoughts (id, content, source) VALUES (1, 'test thought', 'test')`); err != nil {
		t.Fatalf("seed thought: %v", err)
	}

	// First save.
	err := s.Save(ctx, "thought", 1, []float32{1, 0, 0}, "model-a")
	if err != nil {
		t.Fatalf("save: %v", err)
	}

	// Upsert with different embedding.
	err = s.Upsert(ctx, "thought", 1, []float32{0, 1, 0}, "model-b")
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}

	// Should have only 1 row, with the updated embedding.
	count, _ := s.Count(ctx)
	if count != 1 {
		t.Errorf("count = %d, want 1", count)
	}

	// Search should find the updated embedding.
	results, err := s.Search(ctx, []float32{0, 1, 0}, 10)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("got %d results, want 1", len(results))
	}
	if results[0].Distance > 0.01 {
		t.Errorf("distance = %f, want ~0 (embedding should be updated)", results[0].Distance)
	}
}

func TestDelete(t *testing.T) {
	db := testutil.TestDB(t)
	s := vector.NewStore(db.Conn())
	ctx := context.Background()

	s.Save(ctx, "thought", 1, []float32{1, 0, 0}, "test-model")
	s.Save(ctx, "thought", 2, []float32{0, 1, 0}, "test-model")

	err := s.Delete(ctx, "thought", 1)
	if err != nil {
		t.Fatalf("delete: %v", err)
	}

	count, _ := s.Count(ctx)
	if count != 1 {
		t.Errorf("count = %d, want 1", count)
	}
}

func TestCount(t *testing.T) {
	db := testutil.TestDB(t)
	s := vector.NewStore(db.Conn())
	ctx := context.Background()

	count, err := s.Count(ctx)
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 0 {
		t.Errorf("empty count = %d, want 0", count)
	}

	s.Save(ctx, "thought", 1, []float32{1, 0, 0}, "test-model")
	s.Save(ctx, "transcript", 2, []float32{0, 1, 0}, "test-model")

	count, _ = s.Count(ctx)
	if count != 2 {
		t.Errorf("count = %d, want 2", count)
	}
}

func TestSearchLimit(t *testing.T) {
	db := testutil.TestDB(t)
	s := vector.NewStore(db.Conn())
	ctx := context.Background()

	for i := int64(1); i <= 10; i++ {
		s.Save(ctx, "thought", i, []float32{float32(i), 0, 0}, "test-model")
	}

	results, err := s.Search(ctx, []float32{1, 0, 0}, 3)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(results) != 3 {
		t.Errorf("got %d results, want 3", len(results))
	}
}

func TestFloat32BlobRoundTrip(t *testing.T) {
	original := []float32{1.5, -2.3, 0.0, 3.14159, -0.001}
	blob := vector.Float32ToBlob(original)
	decoded := vector.BlobToFloat32(blob)

	if len(decoded) != len(original) {
		t.Fatalf("length: got %d, want %d", len(decoded), len(original))
	}
	for i := range original {
		if decoded[i] != original[i] {
			t.Errorf("[%d]: got %f, want %f", i, decoded[i], original[i])
		}
	}
}

func TestBlobToFloat32InvalidLength(t *testing.T) {
	// 5 bytes is not a multiple of 4.
	result := vector.BlobToFloat32([]byte{1, 2, 3, 4, 5})
	if result != nil {
		t.Errorf("got %v, want nil", result)
	}
}

func TestSearchSortedByDistance(t *testing.T) {
	db := testutil.TestDB(t)
	s := vector.NewStore(db.Conn())
	ctx := context.Background()

	// Far from query.
	s.Save(ctx, "thought", 1, []float32{0, 0, 1}, "test-model")
	// Close to query.
	s.Save(ctx, "thought", 2, []float32{0.9, 0.1, 0}, "test-model")
	// Closest to query.
	s.Save(ctx, "thought", 3, []float32{1, 0, 0}, "test-model")

	query := []float32{1, 0, 0}
	results, err := s.Search(ctx, query, 10)
	if err != nil {
		t.Fatalf("search: %v", err)
	}

	// Results should be sorted by distance ascending.
	for i := 1; i < len(results); i++ {
		if results[i].Distance < results[i-1].Distance {
			t.Errorf("not sorted: [%d].Distance=%f < [%d].Distance=%f",
				i, results[i].Distance, i-1, results[i-1].Distance)
		}
	}

	if results[0].SourceID != 3 {
		t.Errorf("nearest = source_id %d, want 3", results[0].SourceID)
	}
}
