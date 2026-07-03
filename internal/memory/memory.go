package memory

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"
	"sync/atomic"
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

// KeyFact represents an extracted fact about the user, with the quality
// metadata that makes it correctable: where it came from (provenance), how
// trustworthy it is (confidence), how salient it is (importance), whether a
// later statement replaced it (status/superseded_by), and how often it gets
// used (access tracking, a promotion signal for context selection).
type KeyFact struct {
	ID            int64   `json:"id"`
	Fact          string  `json:"fact"`
	Category      string  `json:"category"`
	SourceSession string  `json:"source_session"`
	ExtractedAt   string  `json:"extracted_at"`
	FactKey       string  `json:"fact_key,omitempty"`
	Importance    int     `json:"importance"`
	Confidence    float64 `json:"confidence"`
	SourceType    string  `json:"source_type,omitempty"`
	SourceID      int64   `json:"source_id,omitempty"`
	LastAccessed  string  `json:"last_accessed,omitempty"`
	AccessCount   int     `json:"access_count"`
	Status        string  `json:"status"`
	SupersededBy  int64   `json:"superseded_by,omitempty"`
	Pinned        bool    `json:"pinned"`
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

	// Async vector indexing. Embedding a saved item is a network round-trip to
	// the embedding provider, so it must never block the save. Saves enqueue onto
	// jobs; a single background worker (started lazily when an indexer is first
	// wired) drains it using baseCtx — a store-owned context that outlives the
	// per-request context (which is cancelled at turn end). All of these fields
	// are guarded by mu; the counters below are atomic.
	jobs       chan indexJob
	baseCtx    context.Context
	baseCancel context.CancelFunc
	closed     bool
	startOnce  sync.Once
	closeOnce  sync.Once
	workerDone chan struct{} // closed by the worker goroutine when it exits

	statQueued  atomic.Int64
	statIndexed atomic.Int64
	statFailed  atomic.Int64
	statDropped atomic.Int64
}

// indexJob is a single unit of background embedding work.
type indexJob struct {
	source  string
	id      int64
	content string
}

// indexQueueSize bounds the embedding backlog held in memory. When the queue is
// full, new items are dropped (and counted) rather than blocking saves — FTS5
// keyword recall still works for a dropped item; semantic recall does not until
// a backfill/reindex path exists (backlog M2.6). Bulk imports that outrun the
// single worker therefore shed the tail — they must not rely on async indexing.
const indexQueueSize = 256

// indexJobTimeout bounds a single embedding call so one stuck provider request
// cannot wedge the worker indefinitely. It is also the real upper bound on how
// long an orphaned worker survives after Close gives up waiting on it.
const indexJobTimeout = 60 * time.Second

// shutdownDrainTimeout bounds the graceful drain in Close before it cancels
// in-flight work; shutdownGraceTimeout then bounds the wait after cancellation
// before Close orphans a still-stuck worker (which exits on its own within
// indexJobTimeout). Close returns within roughly the sum of the two.
const (
	shutdownDrainTimeout = 5 * time.Second
	shutdownGraceTimeout = 2 * time.Second
)

// NewStore creates a memory store.
func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}

// IndexStats is a snapshot of the async vector-indexing pipeline. Pending is the
// current queue depth; Dropped counts items shed because the queue was full.
type IndexStats struct {
	Queued  int64 `json:"queued"`
	Indexed int64 `json:"indexed"`
	Failed  int64 `json:"failed"`
	Dropped int64 `json:"dropped"`
	Pending int   `json:"pending"`
}

// IndexStats returns a snapshot of the async indexing counters, for surfacing
// how much memory is (or isn't) embedded yet.
func (s *Store) IndexStats() IndexStats {
	s.mu.RLock()
	jobs := s.jobs
	s.mu.RUnlock()
	pending := 0
	if jobs != nil {
		pending = len(jobs)
	}
	return IndexStats{
		Queued:  s.statQueued.Load(),
		Indexed: s.statIndexed.Load(),
		Failed:  s.statFailed.Load(),
		Dropped: s.statDropped.Load(),
		Pending: pending,
	}
}

// SetIndexer wires (or replaces) the vector indexer used by SaveThought and
// SaveTranscript. A non-nil indexer lazily starts the background indexing
// worker; passing nil disables future indexing (an already-running worker stays
// up but idles). Safe to call concurrently with reads/writes on the store.
func (s *Store) SetIndexer(i VectorIndexer) {
	s.mu.Lock()
	s.indexer = i
	// Create the queue/context in the SAME critical section that publishes the
	// indexer, so a concurrent save can never observe indexer != nil with
	// jobs == nil (which would be a silent, uncounted drop). The goroutine itself
	// is still spawned exactly once by startWorker.
	if i != nil && s.jobs == nil {
		s.jobs = make(chan indexJob, indexQueueSize)
		s.workerDone = make(chan struct{})
		s.baseCtx, s.baseCancel = context.WithCancel(context.Background())
	}
	s.mu.Unlock()
	if i != nil {
		s.startWorker()
	}
}

// startWorker spins up the single background indexing goroutine exactly once,
// over the queue/context/done channel SetIndexer created under the lock.
func (s *Store) startWorker() {
	s.startOnce.Do(func() {
		s.mu.RLock()
		jobs, ctx, done := s.jobs, s.baseCtx, s.workerDone
		s.mu.RUnlock()
		go s.indexWorker(jobs, ctx, done)
	})
}

// indexWorker drains the queue until it is closed, embedding each item under a
// per-job timeout derived from the store's background context. Errors are logged
// and counted but never propagate — a failed embedding must not lose the
// underlying memory, which stays searchable via FTS5. It closes done on exit so
// Close can wait on it.
func (s *Store) indexWorker(jobs <-chan indexJob, ctx context.Context, done chan struct{}) {
	defer close(done)
	for job := range jobs {
		s.mu.RLock()
		idx := s.indexer
		s.mu.RUnlock()
		if idx == nil {
			// Indexer was cleared (SetIndexer(nil)) after this job was queued;
			// count it so the job is accounted for rather than vanishing.
			s.statDropped.Add(1)
			continue
		}
		jctx, cancel := context.WithTimeout(ctx, indexJobTimeout)
		err := idx.IndexContent(jctx, job.source, job.id, job.content)
		cancel()
		if err != nil {
			s.statFailed.Add(1)
			log.Printf("memory: vector index %s/%d failed: %v", job.source, job.id, err)
			continue
		}
		s.statIndexed.Add(1)
	}
}

// enqueueIndex schedules background embedding for a freshly saved item. It never
// blocks the caller: a no-op when no indexer is wired, and on a full queue the
// item is dropped (and counted) rather than stalling the save.
func (s *Store) enqueueIndex(source string, id int64, content string) {
	if content == "" {
		return
	}
	// RLock pairs with Close's write lock: Close marks closed + closes the
	// channel only after acquiring the write lock, which waits for every in-flight
	// send below to finish — so no send can race a close (send-on-closed panic).
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.indexer == nil || s.jobs == nil || s.closed {
		return
	}
	select {
	case s.jobs <- indexJob{source: source, id: id, content: content}:
		s.statQueued.Add(1)
	default:
		s.statDropped.Add(1)
		log.Printf("memory: vector index queue full (cap %d), dropped %s/%d", indexQueueSize, source, id)
	}
}

// Close stops the background indexer. It first lets the worker drain queued jobs
// (up to shutdownDrainTimeout); if that runs long it cancels in-flight embedding
// and waits a further shutdownGraceTimeout; if the worker is still stuck (e.g. a
// provider that ignores context cancellation) Close orphans it and returns — the
// worker then exits on its own within indexJobTimeout. Close therefore returns
// within roughly shutdownDrainTimeout+shutdownGraceTimeout, and logs a summary if
// anything was dropped or failed. Idempotent; safe even if indexing never started.
func (s *Store) Close() {
	s.closeOnce.Do(func() {
		s.mu.Lock()
		jobs := s.jobs
		done := s.workerDone
		cancel := s.baseCancel
		if jobs == nil || s.closed {
			s.mu.Unlock()
			return
		}
		s.closed = true
		s.mu.Unlock()

		close(jobs) // worker drains the remaining buffered jobs, then exits + closes done

		select {
		case <-done:
		case <-time.After(shutdownDrainTimeout):
			if cancel != nil {
				cancel() // unblock any in-flight embedding so the worker can exit
			}
			select {
			case <-done:
			case <-time.After(shutdownGraceTimeout):
				log.Printf("memory: indexer worker did not stop within %v of cancel; orphaning it (it exits within %v)",
					shutdownGraceTimeout, indexJobTimeout)
			}
		}

		if st := s.IndexStats(); st.Dropped > 0 || st.Failed > 0 {
			log.Printf("memory: indexer shutdown — indexed=%d failed=%d dropped=%d pending=%d",
				st.Indexed, st.Failed, st.Dropped, st.Pending)
		}
	})
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
	s.enqueueIndex("thought", id, content)
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
	// Thin compatibility wrapper: no fact key (so no conflict detection) and
	// default confidence/importance. New callers use SaveFact directly.
	return s.SaveFact(ctx, FactInput{
		Fact:          fact,
		Category:      category,
		SourceSession: sourceSession,
	})
}

// findCanonicalFact returns the id of an existing ACTIVE key fact in the same
// category whose canonical form equals canonical, comparing canonical forms in
// Go (the transform cannot be expressed in SQL). Dedup deliberately ignores
// superseded/retracted rows: re-asserting a value that was replaced earlier
// ("I moved back to Cairo") must create a fresh active fact, not resurrect
// bookkeeping on the dead one.
//
// Dedup on the canonical form within the same category. We compare canonical
// forms in Go because CanonicalFact (lowercasing + trailing-punctuation
// stripping + internal-whitespace collapsing) can't be expressed in SQL — a
// naive `LOWER(TRIM(fact)) = ?` comparison strips neither punctuation nor
// inner whitespace, so it silently fails to match whenever the *stored* fact
// carries punctuation or doubled spaces, letting duplicates accumulate. Dedup
// keeps each category small, so this scan stays bounded. Same category only —
// "Cairo" as a location and "Cairo" as a project name are distinct facts.
func (s *Store) findCanonicalFact(ctx context.Context, canonical, category string) (int64, bool, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, fact FROM key_facts
		 WHERE COALESCE(category, '') = COALESCE(?, '') AND status = 'active'`, category)
	if err != nil {
		return 0, false, fmt.Errorf("memory.save_key_fact: lookup: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var stored string
		if err := rows.Scan(&id, &stored); err != nil {
			return 0, false, fmt.Errorf("memory.save_key_fact: scan: %w", err)
		}
		if entity.CanonicalFact(stored) == canonical {
			return id, true, nil
		}
	}
	if err := rows.Err(); err != nil {
		return 0, false, fmt.Errorf("memory.save_key_fact: rows: %w", err)
	}
	return 0, false, nil
}

// GetKeyFacts retrieves key facts, optionally filtered by category.
func (s *Store) GetKeyFacts(ctx context.Context, category string, limit int) ([]KeyFact, error) {
	if limit <= 0 {
		limit = 10
	}

	// Only active facts: superseded and retracted rows are history, not
	// current knowledge.
	var query string
	var args []interface{}
	if category != "" {
		query = `SELECT id, fact, COALESCE(category, ''), COALESCE(source_session, ''), extracted_at,
		                COALESCE(fact_key, ''), importance, confidence, status, pinned
		         FROM key_facts WHERE category = ? AND status = 'active'
		         ORDER BY extracted_at DESC LIMIT ?`
		args = []interface{}{category, limit}
	} else {
		query = `SELECT id, fact, COALESCE(category, ''), COALESCE(source_session, ''), extracted_at,
		                COALESCE(fact_key, ''), importance, confidence, status, pinned
		         FROM key_facts WHERE status = 'active'
		         ORDER BY extracted_at DESC LIMIT ?`
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
		var pinned int
		if err := rows.Scan(&f.ID, &f.Fact, &f.Category, &f.SourceSession, &f.ExtractedAt,
			&f.FactKey, &f.Importance, &f.Confidence, &f.Status, &pinned); err != nil {
			return nil, fmt.Errorf("memory.get_key_facts: scan: %w", err)
		}
		f.Pinned = pinned != 0
		facts = append(facts, f)
	}
	return facts, rows.Err()
}

// ResolveContent fetches the text content of memory items by source type and id.
// Hybrid search uses it to hydrate results that carry no text of their own —
// notably vector-only hits, since the embedding table stores ids, not content.
// It satisfies hybrid.ContentResolver (wired in the cli package to avoid an
// import cycle). Supported source types: "thought" (captured_thoughts) and
// "transcript" (session_transcripts); any other type yields an empty map.
func (s *Store) ResolveContent(ctx context.Context, sourceType string, ids []int64) (map[int64]string, error) {
	out := make(map[int64]string, len(ids))
	if len(ids) == 0 {
		return out, nil
	}

	// The base query is chosen from a fixed allowlist of tables (never user
	// input); ids are always bound parameters. Assembling it like GetThoughts —
	// a complete SELECT literal, then "+=" clauses — keeps the SELECT literal
	// clear of the no-raw-SQL-concatenation guard (audit_test.go) while staying
	// injection-safe.
	var query string
	switch sourceType {
	case "thought":
		query = "SELECT id, content FROM captured_thoughts"
	case "transcript":
		query = "SELECT id, content FROM session_transcripts"
	default:
		return out, nil
	}

	placeholders := make([]string, len(ids))
	args := make([]interface{}, len(ids))
	for i, id := range ids {
		placeholders[i] = "?"
		args[i] = id
	}
	query += " WHERE id IN (" + strings.Join(placeholders, ", ") + ")"

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("memory.resolve_content: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var id int64
		var content string
		if err := rows.Scan(&id, &content); err != nil {
			return nil, fmt.Errorf("memory.resolve_content: scan: %w", err)
		}
		out[id] = content
	}
	return out, rows.Err()
}

// ResolveTimestamps fetches the timestamp of memory items by source type and id,
// for recency-weighting search results. "thought"/"transcript" use created_at;
// "entity" uses last_seen. Unknown types yield an empty map. Satisfies
// hybrid.RecencyResolver (wired in cli to avoid an import cycle).
func (s *Store) ResolveTimestamps(ctx context.Context, sourceType string, ids []int64) (map[int64]time.Time, error) {
	out := make(map[int64]time.Time, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	// Base query chosen from a fixed allowlist of (table, column); ids are bound.
	var query string
	switch sourceType {
	case "thought":
		query = "SELECT id, created_at FROM captured_thoughts"
	case "transcript":
		query = "SELECT id, created_at FROM session_transcripts"
	case "entity":
		query = "SELECT id, last_seen FROM entities"
	default:
		return out, nil
	}

	placeholders := make([]string, len(ids))
	args := make([]interface{}, len(ids))
	for i, id := range ids {
		placeholders[i] = "?"
		args[i] = id
	}
	query += " WHERE id IN (" + strings.Join(placeholders, ", ") + ")"

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("memory.resolve_timestamps: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var tsStr string
		if err := rows.Scan(&id, &tsStr); err != nil {
			return nil, fmt.Errorf("memory.resolve_timestamps: scan: %w", err)
		}
		if t, ok := parseTimestamp(tsStr); ok {
			out[id] = t
		}
	}
	return out, rows.Err()
}

// parseTimestamp parses the timestamp formats stored in the memory tables:
// RFC3339 (written by Go) and SQLite's datetime('now') default.
func parseTimestamp(s string) (time.Time, bool) {
	for _, layout := range []string{time.RFC3339, "2006-01-02 15:04:05"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.UTC(), true
		}
	}
	return time.Time{}, false
}
