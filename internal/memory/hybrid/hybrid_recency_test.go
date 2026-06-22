package hybrid_test

import (
	"context"
	"testing"
	"time"

	"github.com/LumabyteCo/aibutler/internal/memory/hybrid"
	"github.com/LumabyteCo/aibutler/internal/memory/vector"
)

// stubRecency returns fixed timestamps for given ids (any source type).
type stubRecency struct{ ts map[int64]time.Time }

func (r *stubRecency) ResolveTimestamps(_ context.Context, _ string, ids []int64) (map[int64]time.Time, error) {
	out := make(map[int64]time.Time)
	for _, id := range ids {
		if t, ok := r.ts[id]; ok {
			out[id] = t
		}
	}
	return out, nil
}

// TestRecencyDecayPromotesRecent: id 1 has the better base RRF rank (vector
// rank 0), but id 2 is far more recent — with decay wired, id 2 wins.
func TestRecencyDecayPromotesRecent(t *testing.T) {
	ctx := context.Background()
	s := hybrid.NewSearcher(nil, nil)
	s.SetVectorSearch(&fakeVectorSearcher{results: []vector.SearchResult{
		{SourceType: "thought", SourceID: 1, Distance: 0.1}, // rank 0 → higher base RRF
		{SourceType: "thought", SourceID: 2, Distance: 0.2}, // rank 1
	}}, fakeEmbedFunc)

	now := time.Now().UTC()
	s.SetRecencyDecay(&stubRecency{ts: map[int64]time.Time{
		1: now.Add(-365 * 24 * time.Hour), // ~1 year old
		2: now,                            // brand new
	}}, 0) // default 90-day half-life

	results, err := s.Search(ctx, "q", 10)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("got %d results, want 2", len(results))
	}
	if results[0].ID != 2 {
		t.Errorf("top result ID = %d, want 2 (recent item promoted over older, higher-ranked one)", results[0].ID)
	}
}

// TestRecencyDecayDisabledKeepsRRFOrder: without SetRecencyDecay, ordering is
// pure RRF — id 1 (rank 0) stays first regardless of age.
func TestRecencyDecayDisabledKeepsRRFOrder(t *testing.T) {
	ctx := context.Background()
	s := hybrid.NewSearcher(nil, nil)
	s.SetVectorSearch(&fakeVectorSearcher{results: []vector.SearchResult{
		{SourceType: "thought", SourceID: 1, Distance: 0.1},
		{SourceType: "thought", SourceID: 2, Distance: 0.2},
	}}, fakeEmbedFunc)

	results, err := s.Search(ctx, "q", 10)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(results) != 2 || results[0].ID != 1 {
		t.Errorf("without recency decay, expected id 1 first (pure RRF), got %+v", results)
	}
}
