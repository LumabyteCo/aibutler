package memory

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/LumabyteCo/aibutler/internal/memory/entity"
)

// VectorIndexer persists an embedding for a stored memory item so that
// semantic search can find it later. It is optional — when nil (the default),
// Store inserts still succeed and the memory remains searchable via FTS5.
//
// Implementations are in the cli package (which has access to the embedder)
// or in tests (mock). Keeping this as an interface here breaks the
// memory → vector → embedder dependency cycle.
type VectorIndexer interface {
	IndexContent(ctx context.Context, source string, sourceID int64, content string) error
}

// Thought represents a captured thought from the user.
type Thought struct {
	ID        int64    `json:"id"`
	Content   string   `json:"content"`
	Source    string   `json:"source"`
	SessionID string   `json:"session_id"`
	Tags      []string `json:"tags"`
	CreatedAt string   `json:"created_at"`
}

// KeyFact represents an extracted fact about the user.
type KeyFact struct {
	ID            int64  `json:"id"`
	Fact          string `json:"fact"`
	Category      string `json:"category"`
	SourceSession string `json:"source_session"`
	ExtractedAt   string `json:"extracted_at"`
}

// ThoughtQuery holds optional filters for thought retrieval.
type ThoughtQuery struct {
	Tags     []string // Filter by any matching tag (OR)
	Since    string   // ISO date string (created_at >= ?)
	Until    string   // ISO date string (created_at <= ?)
	Limit    int      // Max results (default 50)
	Contains string   // Full-text LIKE search in content
}

// Store manages living memory persistence.
type Store struct {
	db *sql.DB

	mu      sync.RWMutex
	indexer VectorIndexer
}

// NewStore creates a memory store.
func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}

// SetIndexer wires (or replaces) the vector indexer used by SaveThought and
// SaveTranscript. Passing nil disables indexing. Safe to call concurrently
// with reads/writes on the store.
func (s *Store) SetIndexer(i VectorIndexer) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.indexer = i
}

// indexAsync fires the indexer for a freshly saved item. Errors are logged
// but never surfaced — a failed embedding call must never turn into a failed
// save, because FTS5 + keyword recall still work without vectors.
func (s *Store) indexAsync(ctx context.Context, source string, id int64, content string) {
	s.mu.RLock()
	idx := s.indexer
	s.mu.RUnlock()
	if idx == nil || content == "" {
		return
	}
	if err := idx.IndexContent(ctx, source, id, content); err != nil {
		log.Printf("memory: vector index %s/%d failed: %v", source, id, err)
	}
}

// SaveThought persists a captured thought.
func (s *Store) SaveThought(ctx context.Context, content, source, sessionID string, tags []string) (int64, error) {
	if source == "" {
		source = "user"
	}
	now := time.Now().UTC().Format(time.RFC3339)

	var tagsJSON string
	if len(tags) > 0 {
		b, _ := json.Marshal(tags)
		tagsJSON = string(b)
	}

	result, err := s.db.ExecContext(ctx,
		`INSERT INTO captured_thoughts (content, source, session_id, tags, created_at) VALUES (?, ?, ?, ?, ?)`,
		content, source, sessionID, tagsJSON, now)
	if err != nil {
		return 0, fmt.Errorf("memory.save_thought: %w", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return 0, err
	}
	s.indexAsync(ctx, "thought", id, content)
	return id, nil
}

// GetThoughts retrieves thoughts with optional filtering.
func (s *Store) GetThoughts(ctx context.Context, opts ThoughtQuery) ([]Thought, error) {
	query := `SELECT id, content, source, COALESCE(session_id, ''), COALESCE(tags, ''), created_at FROM captured_thoughts`
	var conditions []string
	var args []interface{}

	if len(opts.Tags) > 0 {
		var tagConds []string
		for _, tag := range opts.Tags {
			tagConds = append(tagConds, `tags LIKE ?`)
			args = append(args, `%"`+tag+`"%`)
		}
		conditions = append(conditions, "("+strings.Join(tagConds, " OR ")+")")
	}

	if opts.Since != "" {
		conditions = append(conditions, `created_at >= ?`)
		args = append(args, opts.Since)
	}
	if opts.Until != "" {
		conditions = append(conditions, `created_at <= ?`)
		args = append(args, opts.Until)
	}
	if opts.Contains != "" {
		conditions = append(conditions, `content LIKE ?`)
		args = append(args, "%"+opts.Contains+"%")
	}

	if len(conditions) > 0 {
		query += " WHERE " + strings.Join(conditions, " AND ")
	}
	query += " ORDER BY created_at DESC"

	limit := opts.Limit
	if limit <= 0 {
		limit = 50
	}
	query += fmt.Sprintf(" LIMIT %d", limit)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("memory.get_thoughts: %w", err)
	}
	defer rows.Close()

	var thoughts []Thought
	for rows.Next() {
		var t Thought
		var tagsStr string
		if err := rows.Scan(&t.ID, &t.Content, &t.Source, &t.SessionID, &tagsStr, &t.CreatedAt); err != nil {
			return nil, fmt.Errorf("memory.get_thoughts: scan: %w", err)
		}
		if tagsStr != "" {
			_ = json.Unmarshal([]byte(tagsStr), &t.Tags)
		}
		thoughts = append(thoughts, t)
	}
	return thoughts, rows.Err()
}

// ThoughtCount returns the total number of captured thoughts.
func (s *Store) ThoughtCount(ctx context.Context) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM captured_thoughts`).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("memory.thought_count: %w", err)
	}
	return count, nil
}

// SaveKeyFact persists an extracted key fact. Dedupes on canonical form
// (lowercased, whitespace-collapsed, trailing punctuation stripped) within
// the same category — if the same fact has already been captured, we update
// the timestamp and return the existing ID instead of inserting a duplicate.
//
// This prevents "AI Butler" from being stored 7 separate times as a key fact
// when the same project gets mentioned across many sessions.
func (s *Store) SaveKeyFact(ctx context.Context, fact, category, sourceSession string) (int64, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	canonical := entity.CanonicalFact(fact)

	// Lookup by canonical form (whole-fact, case-insensitive, whitespace-
	// normalized). Same category only — "Cairo" as a location and "Cairo"
	// as a project name are legitimately distinct facts.
	var existingID int64
	lookupErr := s.db.QueryRowContext(ctx,
		`SELECT id FROM key_facts
		 WHERE LOWER(TRIM(fact)) = ? AND COALESCE(category, '') = COALESCE(?, '')
		 LIMIT 1`,
		canonical, category).Scan(&existingID)

	if lookupErr == nil {
		// Already stored — bump the timestamp so "most recent" queries work.
		if _, err := s.db.ExecContext(ctx,
			`UPDATE key_facts SET extracted_at = ? WHERE id = ?`, now, existingID); err != nil {
			return 0, fmt.Errorf("memory.save_key_fact: touch: %w", err)
		}
		return existingID, nil
	}

	result, err := s.db.ExecContext(ctx,
		`INSERT INTO key_facts (fact, category, source_session, extracted_at) VALUES (?, ?, ?, ?)`,
		fact, category, sourceSession, now)
	if err != nil {
		return 0, fmt.Errorf("memory.save_key_fact: %w", err)
	}
	return result.LastInsertId()
}

// GetKeyFacts retrieves key facts, optionally filtered by category.
func (s *Store) GetKeyFacts(ctx context.Context, category string, limit int) ([]KeyFact, error) {
	if limit <= 0 {
		limit = 10
	}

	var query string
	var args []interface{}
	if category != "" {
		query = `SELECT id, fact, COALESCE(category, ''), COALESCE(source_session, ''), extracted_at FROM key_facts WHERE category = ? ORDER BY extracted_at DESC LIMIT ?`
		args = []interface{}{category, limit}
	} else {
		query = `SELECT id, fact, COALESCE(category, ''), COALESCE(source_session, ''), extracted_at FROM key_facts ORDER BY extracted_at DESC LIMIT ?`
		args = []interface{}{limit}
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("memory.get_key_facts: %w", err)
	}
	defer rows.Close()

	var facts []KeyFact
	for rows.Next() {
		var f KeyFact
		if err := rows.Scan(&f.ID, &f.Fact, &f.Category, &f.SourceSession, &f.ExtractedAt); err != nil {
			return nil, fmt.Errorf("memory.get_key_facts: scan: %w", err)
		}
		facts = append(facts, f)
	}
	return facts, rows.Err()
}
