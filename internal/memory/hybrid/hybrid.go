// Package hybrid provides combined FTS5 + entity + vector search for Living Memory.
// Three-source retrieval fused with Reciprocal Rank Fusion (RRF): each backend
// returns a ranked list, every hit contributes 1/(k+rank) to its item's score,
// and contributions are summed across backends by a canonical (source, id) key.
//
// RRF fuses on RANK, not raw score, so the incompatible score scales of BM25
// (lower-is-better, unbounded), cosine distance, and entity mention counts never
// need to be reconciled. A side effect of summing by canonical key is dedup +
// boosting: an item surfaced by more than one backend collapses to a single
// result whose score is the sum of its per-backend contributions, so corroborated
// hits rank above single-backend hits instead of appearing twice.
package hybrid

import (
	"context"
	"math"
	"strings"
	"time"

	"github.com/LumabyteCo/aibutler/internal/memory/entity"
	"github.com/LumabyteCo/aibutler/internal/memory/fts"
	"github.com/LumabyteCo/aibutler/internal/memory/vector"
)

// rrfK is the Reciprocal Rank Fusion constant. 60 is the value from the original
// Cormack et al. RRF paper and the de-facto default across vector engines; it
// damps the weight of top ranks so lower-ranked corroborating hits still matter.
const rrfK = 60.0

// VectorSearcher abstracts the vector store for embedding-based search.
// This is optional — hybrid search works without it (FTS5 + entity only).
type VectorSearcher interface {
	Search(ctx context.Context, query []float32, limit int) ([]vector.SearchResult, error)
}

// EmbedFunc generates a query embedding. Nil means vector search is disabled.
type EmbedFunc func(ctx context.Context, text string) ([]float32, error)

// ContentResolver hydrates the text content of memory items by source type and
// id. Vector hits carry only an id (the embedding table stores no text), so a
// vector-only result would otherwise reach callers with empty Content and waste
// a result slot. This is optional — when nil, vector-only hits keep empty
// Content (no regression). Implemented by *memory.Store and wired in the cli
// package, because memory imports hybrid (so hybrid must not import memory).
type ContentResolver interface {
	ResolveContent(ctx context.Context, sourceType string, ids []int64) (map[int64]string, error)
}

// RecencyResolver returns the timestamp of memory items by source type and id,
// so search can down-weight older results. Implemented by *memory.Store and
// wired in cli (memory imports hybrid). Optional — when unset, no recency decay.
type RecencyResolver interface {
	ResolveTimestamps(ctx context.Context, sourceType string, ids []int64) (map[int64]time.Time, error)
}

// defaultRecencyHalfLife is the age at which a result's relevance weight halves.
// 90 days is gentle — long-horizon memory should still surface old-but-relevant
// items; recency only breaks ties and nudges ordering.
const defaultRecencyHalfLife = 90 * 24 * time.Hour

// recencyFloor is the smallest recency weight, so even very old items stay
// retrievable rather than decaying to zero.
const recencyFloor = 0.1

// Result represents a unified search result from hybrid search.
type Result struct {
	ID      int64   `json:"id"`
	Content string  `json:"content"`
	Score   float64 `json:"score"`  // RRF fused score (higher = more relevant; NOT 0-1 normalized)
	Source  string  `json:"source"` // canonical: "thought", "transcript", or "entity" (vector hits map onto their underlying type)
	Type    string  `json:"type"`   // entity type if source=entity, empty otherwise
}

// Searcher combines FTS5, entity, and vector search for unified results.
type Searcher struct {
	fts      *fts.Store
	entity   *entity.Store
	vec      VectorSearcher
	embed    EmbedFunc
	content  ContentResolver
	recency  RecencyResolver
	halfLife time.Duration
}

// NewSearcher creates a hybrid searcher.
// vec and embed are optional — pass nil to disable vector search.
func NewSearcher(ftsStore *fts.Store, entityStore *entity.Store) *Searcher {
	return &Searcher{fts: ftsStore, entity: entityStore}
}

// SetVectorSearch enables vector search with the given store and embedding function.
func (s *Searcher) SetVectorSearch(vec VectorSearcher, embed EmbedFunc) {
	s.vec = vec
	s.embed = embed
}

// SetContentResolver enables Content hydration for vector-only hits.
// Optional; when unset, vector-only hits keep empty Content.
func (s *Searcher) SetContentResolver(r ContentResolver) {
	s.content = r
}

// SetRecencyDecay enables recency down-weighting of results. halfLife <= 0 uses
// defaultRecencyHalfLife. Optional; when unset, scores are pure RRF relevance.
func (s *Searcher) SetRecencyDecay(r RecencyResolver, halfLife time.Duration) {
	s.recency = r
	if halfLife <= 0 {
		halfLife = defaultRecencyHalfLife
	}
	s.halfLife = halfLife
}

// dedupKey identifies one underlying memory item across backends. Vector hits
// use their underlying source type (e.g. "thought"), so a thought surfaced by
// both FTS and vector collapses to a single key and its RRF contributions sum.
type dedupKey struct {
	source string
	id     int64
}

// Search performs a combined search across FTS5, entity, and vector stores,
// fusing the per-backend ranked lists with Reciprocal Rank Fusion.
func (s *Searcher) Search(ctx context.Context, query string, limit int) ([]Result, error) {
	if query == "" {
		return nil, nil
	}
	if limit <= 0 {
		limit = 20
	}

	fused := make(map[dedupKey]*Result)
	order := make([]dedupKey, 0)

	// fuse folds one backend hit (0-based rank) into the running fusion. The
	// first writer of a key sets Source/Type/Content; later corroborating hits
	// only add their RRF contribution and fill Content/Type if still missing.
	fuse := func(source string, id int64, rank int, content, typ string) {
		key := dedupKey{source: source, id: id}
		r, ok := fused[key]
		if !ok {
			r = &Result{ID: id, Source: source, Type: typ, Content: content}
			fused[key] = r
			order = append(order, key)
		}
		r.Score += 1.0 / (rrfK + float64(rank+1)) // rank is 0-based → 1-based RRF
		if r.Content == "" && content != "" {
			r.Content = content
		}
		if r.Type == "" && typ != "" {
			r.Type = typ
		}
	}

	// FTS5 search (BM25-ranked; SearchAll returns best-first).
	if s.fts != nil {
		ftsResults, err := s.fts.SearchAll(ctx, query, limit)
		if err != nil {
			return nil, err
		}
		for i, r := range ftsResults {
			fuse(r.Source, r.ID, i, r.Content, "")
		}
	}

	// Entity name search (ordered by mention_count desc, so rank reflects it).
	if s.entity != nil {
		entities, err := s.entity.Search(ctx, query, limit)
		if err != nil {
			return nil, err
		}
		for i, e := range entities {
			fuse("entity", e.ID, i, formatEntity(e), string(e.Type))
		}
	}

	// Vector semantic search (best-first by cosine distance). Errors are
	// non-fatal: FTS5 + entity recall still works without embeddings.
	if s.vec != nil && s.embed != nil {
		if queryVec, err := s.embed(ctx, query); err == nil && len(queryVec) > 0 {
			if vecResults, err := s.vec.Search(ctx, queryVec, limit); err == nil {
				for i, r := range vecResults {
					fuse(r.SourceType, r.SourceID, i, "", "")
				}
			}
		}
	}

	results := make([]Result, 0, len(order))
	for _, k := range order {
		results = append(results, *fused[k])
	}

	// Recency decay (before sort+truncate, so it affects which items survive).
	s.applyRecency(ctx, results)
	sortByScore(results)

	if len(results) > limit {
		results = results[:limit]
	}

	// Hydrate Content for the surviving vector-only hits (best-effort).
	s.hydrate(ctx, results)

	return results, nil
}

// hydrate fills Content for results that have none (vector-only hits) using the
// ContentResolver, if one is wired. Best-effort: resolution errors are skipped
// so a content lookup never fails the whole search.
func (s *Searcher) hydrate(ctx context.Context, results []Result) {
	if s.content == nil {
		return
	}
	need := make(map[string][]int64)
	for i := range results {
		if results[i].Content == "" {
			switch results[i].Source {
			case "thought", "transcript":
				need[results[i].Source] = append(need[results[i].Source], results[i].ID)
			}
		}
	}
	if len(need) == 0 {
		return
	}
	for src, ids := range need {
		resolved, err := s.content.ResolveContent(ctx, src, ids)
		if err != nil {
			continue
		}
		for i := range results {
			if results[i].Content == "" && results[i].Source == src {
				if c, ok := resolved[results[i].ID]; ok {
					results[i].Content = c
				}
			}
		}
	}
}

// applyRecency multiplies each result's RRF score by an exponential recency
// weight (half-life decay, floored at recencyFloor so old items stay
// retrievable), using timestamps from the RecencyResolver. Best-effort: if the
// resolver is unset or a lookup fails, scores are left unchanged.
func (s *Searcher) applyRecency(ctx context.Context, results []Result) {
	if s.recency == nil || len(results) == 0 {
		return
	}
	halfLife := s.halfLife
	if halfLife <= 0 {
		halfLife = defaultRecencyHalfLife
	}

	bySource := make(map[string][]int64)
	for i := range results {
		bySource[results[i].Source] = append(bySource[results[i].Source], results[i].ID)
	}
	ts := make(map[dedupKey]time.Time)
	for src, ids := range bySource {
		m, err := s.recency.ResolveTimestamps(ctx, src, ids)
		if err != nil {
			continue
		}
		for id, t := range m {
			ts[dedupKey{source: src, id: id}] = t
		}
	}

	now := time.Now().UTC()
	for i := range results {
		t, ok := ts[dedupKey{source: results[i].Source, id: results[i].ID}]
		if !ok {
			continue // no timestamp → leave score unchanged
		}
		results[i].Score *= recencyWeight(now.Sub(t), halfLife)
	}
}

// recencyWeight returns 0.5^(age/halfLife), floored at recencyFloor. Zero or
// negative (future) ages clamp to 1.0.
func recencyWeight(age, halfLife time.Duration) float64 {
	if age <= 0 || halfLife <= 0 {
		return 1.0
	}
	w := math.Pow(0.5, float64(age)/float64(halfLife))
	if w < recencyFloor {
		return recencyFloor
	}
	return w
}

// formatEntity renders an entity match as a single display line.
func formatEntity(e entity.Entity) string {
	parts := []string{string(e.Type) + ": " + e.Name}
	if len(e.Attributes) > 0 {
		attrParts := make([]string, 0, len(e.Attributes))
		for k, v := range e.Attributes {
			attrParts = append(attrParts, k+"="+v)
		}
		parts = append(parts, strings.Join(attrParts, ", "))
	}
	return strings.Join(parts, " — ")
}

// sortByScore sorts results by fused score descending (higher = more relevant).
// Insertion sort with a strict comparison keeps it stable for equal scores,
// preserving first-seen (best-rank) order; result counts here are small.
func sortByScore(results []Result) {
	for i := 1; i < len(results); i++ {
		for j := i; j > 0 && results[j].Score > results[j-1].Score; j-- {
			results[j], results[j-1] = results[j-1], results[j]
		}
	}
}
