// Package hybrid provides combined FTS5 + entity + vector search for Living Memory.
// Three-source hybrid search with reciprocal rank fusion (RRF).
package hybrid

import (
	"context"
	"strings"

	"github.com/LumabyteCo/aibutler/internal/memory/entity"
	"github.com/LumabyteCo/aibutler/internal/memory/fts"
	"github.com/LumabyteCo/aibutler/internal/memory/vector"
)

// VectorSearcher abstracts the vector store for embedding-based search.
// This is optional — hybrid search works without it (FTS5 + entity only).
type VectorSearcher interface {
	Search(ctx context.Context, query []float32, limit int) ([]vector.SearchResult, error)
}

// EmbedFunc generates a query embedding. Nil means vector search is disabled.
type EmbedFunc func(ctx context.Context, text string) ([]float32, error)

// Result represents a unified search result from hybrid search.
type Result struct {
	ID      int64   `json:"id"`
	Content string  `json:"content"`
	Score   float64 `json:"score"`   // Normalized 0-1 (higher = more relevant)
	Source  string  `json:"source"`  // "thought", "transcript", "entity", or "vector"
	Type    string  `json:"type"`    // Entity type if source=entity, empty otherwise
}

// Searcher combines FTS5, entity, and vector search for unified results.
type Searcher struct {
	fts    *fts.Store
	entity *entity.Store
	vec    VectorSearcher
	embed  EmbedFunc
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

// Search performs a combined search across FTS5, entity, and vector stores.
// Results are merged and ranked by normalized score.
func (s *Searcher) Search(ctx context.Context, query string, limit int) ([]Result, error) {
	if query == "" {
		return nil, nil
	}
	if limit <= 0 {
		limit = 20
	}

	var results []Result

	// FTS5 search (BM25-ranked).
	if s.fts != nil {
		ftsResults, err := s.fts.SearchAll(ctx, query, limit)
		if err != nil {
			return nil, err
		}
		results = append(results, normalizeFTS(ftsResults)...)
	}

	// Entity name search.
	if s.entity != nil {
		entities, err := s.entity.Search(ctx, query, limit)
		if err != nil {
			return nil, err
		}
		results = append(results, normalizeEntities(entities)...)
	}

	// Vector semantic search (if embedding function available).
	if s.vec != nil && s.embed != nil {
		queryVec, err := s.embed(ctx, query)
		if err == nil && len(queryVec) > 0 {
			vecResults, err := s.vec.Search(ctx, queryVec, limit)
			if err == nil {
				results = append(results, normalizeVector(vecResults)...)
			}
		}
	}

	// Sort by score descending (higher = more relevant).
	sortByScore(results)

	if len(results) > limit {
		results = results[:limit]
	}
	return results, nil
}

// normalizeFTS converts FTS5 BM25 ranks to 0-1 scores.
// BM25 rank is negative (lower = more relevant). We normalize to 0-1 where 1 = most relevant.
func normalizeFTS(ftsResults []fts.SearchResult) []Result {
	if len(ftsResults) == 0 {
		return nil
	}

	// Find min/max ranks for normalization.
	minRank := ftsResults[0].Rank
	maxRank := ftsResults[0].Rank
	for _, r := range ftsResults[1:] {
		if r.Rank < minRank {
			minRank = r.Rank
		}
		if r.Rank > maxRank {
			maxRank = r.Rank
		}
	}

	results := make([]Result, len(ftsResults))
	for i, r := range ftsResults {
		var score float64
		if maxRank == minRank {
			score = 1.0
		} else {
			// Invert: lower rank → higher score.
			score = 1.0 - (r.Rank-minRank)/(maxRank-minRank)
		}
		results[i] = Result{
			ID:      r.ID,
			Content: r.Content,
			Score:   score,
			Source:  r.Source,
		}
	}
	return results
}

// normalizeEntities converts entity matches to results with a relevance score.
func normalizeEntities(entities []entity.Entity) []Result {
	results := make([]Result, len(entities))
	for i, e := range entities {
		// Score based on mention count (more mentions = more relevant).
		score := 0.5 // Base score for entity match.
		if e.MentionCount > 1 {
			// Logarithmic scaling: high mention counts give diminishing returns.
			score = 0.5 + 0.5*(1.0-1.0/float64(e.MentionCount))
		}

		var parts []string
		parts = append(parts, string(e.Type)+": "+e.Name)
		if len(e.Attributes) > 0 {
			var attrParts []string
			for k, v := range e.Attributes {
				attrParts = append(attrParts, k+"="+v)
			}
			parts = append(parts, strings.Join(attrParts, ", "))
		}

		results[i] = Result{
			ID:      e.ID,
			Content: strings.Join(parts, " — "),
			Score:   score,
			Source:  "entity",
			Type:    string(e.Type),
		}
	}
	return results
}

// normalizeVector converts vector search distances to 0-1 scores.
// Cosine distance range is [0, 2]; lower = more similar.
func normalizeVector(vecResults []vector.SearchResult) []Result {
	if len(vecResults) == 0 {
		return nil
	}

	results := make([]Result, len(vecResults))
	for i, r := range vecResults {
		// Convert cosine distance [0,2] to similarity [0,1].
		score := 1.0 - r.Distance/2.0
		if score < 0 {
			score = 0
		}
		results[i] = Result{
			ID:     r.SourceID,
			Score:  score,
			Source: "vector:" + r.SourceType, // e.g. "vector:thought", "vector:transcript"
		}
	}
	return results
}

// sortByScore sorts results by score descending (higher = more relevant).
func sortByScore(results []Result) {
	for i := 1; i < len(results); i++ {
		for j := i; j > 0 && results[j].Score > results[j-1].Score; j-- {
			results[j], results[j-1] = results[j-1], results[j]
		}
	}
}
