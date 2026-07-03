// Package fts provides FTS5 full-text search for Living Memory.
package fts

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/LumabyteCo/aibutler/internal/memory/bank"
)

// SearchResult represents a single FTS5 search result.
type SearchResult struct {
	ID      int64   `json:"id"`
	Content string  `json:"content"`
	Rank    float64 `json:"rank"`   // BM25 relevance score (lower = more relevant)
	Source  string  `json:"source"` // "thought" or "transcript"
}

// Store provides FTS5 search operations.
type Store struct {
	db *sql.DB
}

// NewStore creates an FTS5 search store.
func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}

// SearchThoughts performs BM25-ranked full-text search on captured thoughts.
func (s *Store) SearchThoughts(ctx context.Context, query string, limit int) ([]SearchResult, error) {
	if query == "" {
		return nil, nil
	}
	if limit <= 0 {
		limit = 20
	}

	ftsQuery := sanitizeFTSQuery(query)

	rows, err := s.db.QueryContext(ctx,
		`SELECT ct.id, ct.content, rank
		 FROM captured_thoughts_fts
		 JOIN captured_thoughts ct ON ct.id = captured_thoughts_fts.rowid
		 WHERE captured_thoughts_fts MATCH ? AND ct.bank = ?
		 ORDER BY rank
		 LIMIT ?`, ftsQuery, bank.FromContext(ctx), limit)
	if err != nil {
		return nil, fmt.Errorf("fts.search_thoughts: %w", err)
	}
	defer rows.Close()

	var results []SearchResult
	for rows.Next() {
		var r SearchResult
		if err := rows.Scan(&r.ID, &r.Content, &r.Rank); err != nil {
			return nil, fmt.Errorf("fts.search_thoughts: scan: %w", err)
		}
		r.Source = "thought"
		results = append(results, r)
	}
	return results, rows.Err()
}

// SearchTranscripts performs BM25-ranked full-text search on session transcripts.
func (s *Store) SearchTranscripts(ctx context.Context, query string, limit int) ([]SearchResult, error) {
	if query == "" {
		return nil, nil
	}
	if limit <= 0 {
		limit = 20
	}

	ftsQuery := sanitizeFTSQuery(query)

	rows, err := s.db.QueryContext(ctx,
		`SELECT st.id, st.content, rank
		 FROM session_transcripts_fts
		 JOIN session_transcripts st ON st.id = session_transcripts_fts.rowid
		 WHERE session_transcripts_fts MATCH ? AND st.bank = ?
		 ORDER BY rank
		 LIMIT ?`, ftsQuery, bank.FromContext(ctx), limit)
	if err != nil {
		return nil, fmt.Errorf("fts.search_transcripts: %w", err)
	}
	defer rows.Close()

	var results []SearchResult
	for rows.Next() {
		var r SearchResult
		if err := rows.Scan(&r.ID, &r.Content, &r.Rank); err != nil {
			return nil, fmt.Errorf("fts.search_transcripts: scan: %w", err)
		}
		r.Source = "transcript"
		results = append(results, r)
	}
	return results, rows.Err()
}

// SearchAll searches both thoughts and transcripts, merging results by rank.
func (s *Store) SearchAll(ctx context.Context, query string, limit int) ([]SearchResult, error) {
	if limit <= 0 {
		limit = 20
	}

	thoughts, err := s.SearchThoughts(ctx, query, limit)
	if err != nil {
		return nil, err
	}
	transcripts, err := s.SearchTranscripts(ctx, query, limit)
	if err != nil {
		return nil, err
	}

	// Merge and sort by rank (lower = more relevant in BM25).
	merged := append(thoughts, transcripts...)
	sortByRank(merged)

	if len(merged) > limit {
		merged = merged[:limit]
	}
	return merged, nil
}

// IndexExistingThoughts rebuilds the FTS index from the content table.
// Use this to index thoughts that were inserted before FTS triggers existed.
func (s *Store) IndexExistingThoughts(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO captured_thoughts_fts(captured_thoughts_fts) VALUES ('rebuild')`)
	if err != nil {
		return fmt.Errorf("fts.index_existing_thoughts: %w", err)
	}
	return nil
}

// sanitizeFTSQuery converts a user query into a safe FTS5 query.
// Wraps each token in quotes to prevent FTS5 syntax errors from special chars.
func sanitizeFTSQuery(query string) string {
	words := strings.Fields(query)
	if len(words) == 0 {
		return query
	}

	var quoted []string
	for _, w := range words {
		// Strip FTS5 operators that could cause syntax errors.
		w = strings.TrimFunc(w, func(r rune) bool {
			return r == '"' || r == '\'' || r == '*' || r == '^'
		})
		if w == "AND" || w == "OR" || w == "NOT" || w == "NEAR" || w == "" {
			continue
		}
		quoted = append(quoted, `"`+w+`"`)
	}
	if len(quoted) == 0 {
		return `"` + query + `"`
	}
	return strings.Join(quoted, " ")
}

// sortByRank sorts results by BM25 rank (ascending — lower is more relevant).
func sortByRank(results []SearchResult) {
	for i := 1; i < len(results); i++ {
		for j := i; j > 0 && results[j].Rank < results[j-1].Rank; j-- {
			results[j], results[j-1] = results[j-1], results[j]
		}
	}
}
