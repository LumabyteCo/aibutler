// Package vector provides embedding storage and KNN search for Living Memory.
// Vector distance functions (cosine, L2) are registered as pure Go SQL functions
// via the db package's driver.Open init callback, enabling standard SQL queries
// like: SELECT ... ORDER BY vec_distance_cosine(embedding, ?) LIMIT K
package vector

import (
	"context"
	"database/sql"
	"encoding/binary"
	"fmt"
	"math"
)

// Embedder generates vector embeddings from text.
// Implementations: OpenAI, Gemini, local models via MCP.
type Embedder interface {
	Embed(ctx context.Context, text string) ([]float32, error)
	EmbedBatch(ctx context.Context, texts []string) ([][]float32, error)
	Dimension() int
}

// Store manages vector embeddings in SQLite.
type Store struct {
	db *sql.DB
}

// NewStore creates a vector store.
func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}

// Save stores an embedding for a source item.
func (s *Store) Save(ctx context.Context, sourceType string, sourceID int64, embedding []float32, model string) error {
	blob := Float32ToBlob(embedding)
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO memory_vectors (source_type, source_id, embedding, model, dimension)
		 VALUES (?, ?, ?, ?, ?)`,
		sourceType, sourceID, blob, model, len(embedding))
	if err != nil {
		return fmt.Errorf("vector.save: %w", err)
	}
	return nil
}

// Upsert saves or replaces an embedding for a source item.
//
// The source-row existence guard runs in the same transaction as the write:
// embedding jobs are queued asynchronously, so by the time one executes the
// user may have permanently deleted the source row — deletion promises that
// every derived artifact goes with it, and a late upsert must not resurrect
// the embedding of forgotten content. Unknown source types (e.g. "entity")
// pass through unguarded.
func (s *Store) Upsert(ctx context.Context, sourceType string, sourceID int64, embedding []float32, model string) error {
	blob := Float32ToBlob(embedding)

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("vector.upsert: begin: %w", err)
	}
	defer tx.Rollback()

	exists := 1
	switch sourceType {
	case "thought":
		err = tx.QueryRowContext(ctx,
			`SELECT EXISTS(SELECT 1 FROM captured_thoughts WHERE id = ?)`, sourceID).Scan(&exists)
	case "transcript":
		err = tx.QueryRowContext(ctx,
			`SELECT EXISTS(SELECT 1 FROM session_transcripts WHERE id = ?)`, sourceID).Scan(&exists)
	}
	if err != nil {
		return fmt.Errorf("vector.upsert: source check: %w", err)
	}
	if exists == 0 {
		// Source row was deleted while this job sat in the queue — drop the
		// embedding silently; there is nothing left for it to describe.
		return nil
	}

	_, err = tx.ExecContext(ctx,
		`INSERT INTO memory_vectors (source_type, source_id, embedding, model, dimension)
		 VALUES (?, ?, ?, ?, ?)
		 ON CONFLICT(source_type, source_id) DO UPDATE SET
		   embedding = excluded.embedding,
		   model = excluded.model,
		   dimension = excluded.dimension,
		   created_at = datetime('now')`,
		sourceType, sourceID, blob, model, len(embedding))
	if err != nil {
		return fmt.Errorf("vector.upsert: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("vector.upsert: commit: %w", err)
	}
	return nil
}

// SearchResult represents a KNN search result.
type SearchResult struct {
	SourceType string  `json:"source_type"`
	SourceID   int64   `json:"source_id"`
	Distance   float64 `json:"distance"`
}

// Search performs brute-force KNN search using cosine distance.
func (s *Store) Search(ctx context.Context, query []float32, limit int) ([]SearchResult, error) {
	if limit <= 0 {
		limit = 10
	}
	if len(query) == 0 {
		return nil, nil
	}

	blob := Float32ToBlob(query)
	rows, err := s.db.QueryContext(ctx,
		`SELECT source_type, source_id, vec_distance_cosine(embedding, ?) as distance
		 FROM memory_vectors
		 ORDER BY distance ASC
		 LIMIT ?`, blob, limit)
	if err != nil {
		return nil, fmt.Errorf("vector.search: %w", err)
	}
	defer rows.Close()

	var results []SearchResult
	for rows.Next() {
		var r SearchResult
		if err := rows.Scan(&r.SourceType, &r.SourceID, &r.Distance); err != nil {
			return nil, fmt.Errorf("vector.search scan: %w", err)
		}
		results = append(results, r)
	}
	return results, rows.Err()
}

// SearchByType performs KNN search filtered by source type.
func (s *Store) SearchByType(ctx context.Context, query []float32, sourceType string, limit int) ([]SearchResult, error) {
	if limit <= 0 {
		limit = 10
	}
	if len(query) == 0 {
		return nil, nil
	}

	blob := Float32ToBlob(query)
	rows, err := s.db.QueryContext(ctx,
		`SELECT source_type, source_id, vec_distance_cosine(embedding, ?) as distance
		 FROM memory_vectors
		 WHERE source_type = ?
		 ORDER BY distance ASC
		 LIMIT ?`, blob, sourceType, limit)
	if err != nil {
		return nil, fmt.Errorf("vector.search_by_type: %w", err)
	}
	defer rows.Close()

	var results []SearchResult
	for rows.Next() {
		var r SearchResult
		if err := rows.Scan(&r.SourceType, &r.SourceID, &r.Distance); err != nil {
			return nil, fmt.Errorf("vector.search_by_type scan: %w", err)
		}
		results = append(results, r)
	}
	return results, rows.Err()
}

// Delete removes the embedding for a source item.
func (s *Store) Delete(ctx context.Context, sourceType string, sourceID int64) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM memory_vectors WHERE source_type = ? AND source_id = ?`,
		sourceType, sourceID)
	if err != nil {
		return fmt.Errorf("vector.delete: %w", err)
	}
	return nil
}

// Count returns the total number of stored embeddings.
func (s *Store) Count(ctx context.Context) (int64, error) {
	var count int64
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM memory_vectors`).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("vector.count: %w", err)
	}
	return count, nil
}

// Float32ToBlob converts a float32 slice to a little-endian byte slice.
func Float32ToBlob(v []float32) []byte {
	b := make([]byte, len(v)*4)
	for i, f := range v {
		binary.LittleEndian.PutUint32(b[i*4:], math.Float32bits(f))
	}
	return b
}

// BlobToFloat32 converts a little-endian byte slice to a float32 slice.
func BlobToFloat32(b []byte) []float32 {
	if len(b)%4 != 0 {
		return nil
	}
	n := len(b) / 4
	result := make([]float32, n)
	for i := range n {
		result[i] = math.Float32frombits(binary.LittleEndian.Uint32(b[i*4:]))
	}
	return result
}
