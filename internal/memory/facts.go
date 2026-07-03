package memory

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/LumabyteCo/aibutler/internal/memory/entity"
)

// ErrNotFound reports that the referenced memory row does not exist. HTTP
// surfaces map it to 404.
var ErrNotFound = errors.New("not found")

// querier is the subset of *sql.DB / *sql.Tx the fact helpers need, so the
// same lookup can run standalone or inside a transaction.
type querier interface {
	QueryContext(ctx context.Context, query string, args ...interface{}) (*sql.Rows, error)
}

// Fact lifecycle states. Superseded and retracted rows are excluded from
// selection but retained so history stays inspectable.
const (
	FactStatusActive     = "active"
	FactStatusSuperseded = "superseded"
	FactStatusRetracted  = "retracted"
)

// Conflict resolutions recorded in memory_conflicts.
const (
	ResolutionAutoSupersede = "auto_supersede" // clear case: newer statement wins
	ResolutionNeedsReview   = "needs_review"   // superseded, but flagged for the user
	ResolutionUserCorrected = "user_corrected" // explicit correction by the user
	ResolutionUserRestored  = "user_restored"  // user rejected the replacement, old value restored
)

// Confidence priors by how a fact entered the system. A fact stated by the
// user in their own words is more trustworthy than one inferred from tool
// output; an explicit correction is definitive.
const (
	ConfidenceDefault    = 0.7
	ConfidenceUserStated = 0.75
	ConfidenceToolOutput = 0.6
	ConfidenceUserFixed  = 1.0

	// reassertBoost is added to confidence each time the same fact is
	// independently re-stated, capped at 1.0.
	reassertBoost = 0.1

	// reviewMargin: a new fact that is this much *less* confident than the
	// fact it replaces still wins (newest statement usually reflects
	// reality for single-valued attributes), but the conflict is flagged
	// needs_review instead of auto_supersede so the user can restore.
	reviewMargin = 0.15
)

// FactInput carries a fact into SaveFact with full provenance.
type FactInput struct {
	Fact          string
	Category      string
	FactKey       string // "" = multi-valued, no conflict detection
	SourceSession string
	SourceType    string // "thought" | "transcript" | "" (unknown)
	SourceID      int64
	Importance    int     // 0 → 5
	Confidence    float64 // 0 → ConfidenceDefault
}

// SaveFact persists a fact with provenance, dedup, and conflict handling.
//
// Order of operations:
//  1. Canonical-form dedup among ACTIVE facts in the same category — a
//     re-assertion bumps freshness/confidence and returns the existing id.
//  2. If FactKey is set and a different active fact holds the same key, the
//     old fact is marked superseded (never deleted), the new fact becomes the
//     single active holder, and the contradiction is recorded in
//     memory_conflicts — auto_supersede when the new statement is at least as
//     trustworthy, needs_review when it is markedly less confident so the
//     user can restore the old value from the review queue.
//
// The invariant after every call: at most one active fact per FactKey.
func (s *Store) SaveFact(ctx context.Context, in FactInput) (int64, error) {
	if in.Fact == "" {
		return 0, fmt.Errorf("memory.save_fact: fact is required")
	}
	if in.Importance <= 0 {
		in.Importance = 5
	}
	if in.Importance > 10 {
		in.Importance = 10
	}
	if in.Confidence <= 0 {
		in.Confidence = ConfidenceDefault
	}
	if in.Confidence > 1.0 {
		in.Confidence = 1.0
	}
	if in.SourceID == 0 {
		// Half-provenance (a type without a row) is meaningless — store neither.
		in.SourceType = ""
	}
	now := time.Now().UTC().Format(time.RFC3339)
	canonical := entity.CanonicalFact(in.Fact)

	// Everything — dedup scan included — runs in one transaction. With the
	// single-connection pool this serializes concurrent SaveFact calls, so
	// two parallel saves of the same statement can't both miss the dedup
	// check and insert twins (or, worse, record a self-supersede "conflict"
	// between two copies of the same value).
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("memory.save_fact: begin: %w", err)
	}
	defer tx.Rollback()

	// 1. Re-assertion of an existing active fact: refresh instead of insert.
	// If the earlier copy was stored without a key (e.g. through the legacy
	// wrapper), adopt this call's key so the attribute joins conflict
	// detection from now on.
	existingID, found, err := s.findCanonicalFact(ctx, tx, canonical, in.Category)
	if err != nil {
		return 0, err
	}
	if found {
		if _, err := tx.ExecContext(ctx,
			`UPDATE key_facts
			 SET extracted_at = ?, access_count = access_count + 1,
			     confidence = MIN(1.0, confidence + ?),
			     fact_key = COALESCE(fact_key, NULLIF(?, ''))
			 WHERE id = ?`, now, reassertBoost, in.FactKey, existingID); err != nil {
			return 0, fmt.Errorf("memory.save_fact: touch: %w", err)
		}
		if err := tx.Commit(); err != nil {
			return 0, fmt.Errorf("memory.save_fact: commit: %w", err)
		}
		return existingID, nil
	}

	// 2. Conflict check: another active fact holds this single-valued key.
	var oldID int64
	var oldConfidence float64
	hasConflict := false
	if in.FactKey != "" {
		err := tx.QueryRowContext(ctx,
			`SELECT id, confidence FROM key_facts
			 WHERE fact_key = ? AND status = ? LIMIT 1`,
			in.FactKey, FactStatusActive).Scan(&oldID, &oldConfidence)
		switch {
		case err == sql.ErrNoRows:
			// no conflict
		case err != nil:
			return 0, fmt.Errorf("memory.save_fact: conflict lookup: %w", err)
		default:
			hasConflict = true
		}
	}

	result, err := tx.ExecContext(ctx,
		`INSERT INTO key_facts
		 (fact, category, source_session, extracted_at, fact_key, importance,
		  confidence, source_type, source_id, status)
		 VALUES (?, ?, ?, ?, NULLIF(?, ''), ?, ?, NULLIF(?, ''), ?, ?)`,
		in.Fact, in.Category, in.SourceSession, now, in.FactKey, in.Importance,
		in.Confidence, in.SourceType, nullableID(in.SourceID), FactStatusActive)
	if err != nil {
		return 0, fmt.Errorf("memory.save_fact: insert: %w", err)
	}
	newID, err := result.LastInsertId()
	if err != nil {
		return 0, err
	}

	if hasConflict {
		resolution := ResolutionAutoSupersede
		if in.Confidence+reviewMargin < oldConfidence {
			resolution = ResolutionNeedsReview
		}
		if _, err := tx.ExecContext(ctx,
			`UPDATE key_facts SET status = ?, superseded_by = ? WHERE id = ?`,
			FactStatusSuperseded, newID, oldID); err != nil {
			return 0, fmt.Errorf("memory.save_fact: supersede: %w", err)
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO memory_conflicts (old_fact_id, new_fact_id, fact_key, detected_at, resolution)
			 VALUES (?, ?, ?, ?, ?)`,
			oldID, newID, in.FactKey, now, resolution); err != nil {
			return 0, fmt.Errorf("memory.save_fact: record conflict: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("memory.save_fact: commit: %w", err)
	}
	return newID, nil
}

// nullableID maps 0 to NULL so "no provenance row" is NULL, not row 0.
func nullableID(id int64) interface{} {
	if id == 0 {
		return nil
	}
	return id
}

// CorrectFact replaces a fact with a user-supplied correction. The old fact is
// superseded (kept for history), the correction becomes the active fact with
// definitive confidence, and the conflict ledger records it as user-driven.
func (s *Store) CorrectFact(ctx context.Context, id int64, corrected string) (int64, error) {
	if corrected == "" {
		return 0, fmt.Errorf("memory.correct_fact: corrected text is required")
	}
	old, err := s.getFact(ctx, id)
	if err != nil {
		return 0, err
	}
	now := time.Now().UTC().Format(time.RFC3339)

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("memory.correct_fact: begin: %w", err)
	}
	defer tx.Rollback()

	// Corrections of keyless facts must target the live row: if the fact was
	// already superseded (the panel can be stale), the correction would
	// otherwise resurrect a dead value beside the live one.
	if old.FactKey == "" && old.Status != FactStatusActive {
		return 0, fmt.Errorf("memory.correct_fact: fact %d was already updated — reload and retry", id)
	}

	importance := old.Importance
	if importance < 7 {
		importance = 7 // the user cared enough to fix it
	}
	result, err := tx.ExecContext(ctx,
		`INSERT INTO key_facts
		 (fact, category, source_session, extracted_at, fact_key, importance,
		  confidence, status, pinned)
		 VALUES (?, ?, ?, ?, NULLIF(?, ''), ?, ?, ?, ?)`,
		corrected, old.Category, old.SourceSession, now, old.FactKey,
		importance, ConfidenceUserFixed, FactStatusActive, boolToInt(old.Pinned))
	if err != nil {
		return 0, fmt.Errorf("memory.correct_fact: insert: %w", err)
	}
	newID, err := result.LastInsertId()
	if err != nil {
		return 0, err
	}
	if old.FactKey != "" {
		// Supersede EVERY other active holder of the key, not just the row the
		// user clicked — the clicked row may itself have been superseded since
		// the panel loaded, and the correction must leave exactly one active
		// fact for the key regardless of how stale the caller's view was.
		if _, err := tx.ExecContext(ctx,
			`UPDATE key_facts SET status = ?, superseded_by = ?
			 WHERE fact_key = ? AND status = ? AND id != ?`,
			FactStatusSuperseded, newID, old.FactKey, FactStatusActive, newID); err != nil {
			return 0, fmt.Errorf("memory.correct_fact: supersede: %w", err)
		}
	} else {
		if _, err := tx.ExecContext(ctx,
			`UPDATE key_facts SET status = ?, superseded_by = ? WHERE id = ?`,
			FactStatusSuperseded, newID, id); err != nil {
			return 0, fmt.Errorf("memory.correct_fact: supersede: %w", err)
		}
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO memory_conflicts (old_fact_id, new_fact_id, fact_key, detected_at, resolution, reviewed)
		 VALUES (?, ?, ?, ?, ?, 1)`,
		id, newID, old.FactKey, now, ResolutionUserCorrected); err != nil {
		return 0, fmt.Errorf("memory.correct_fact: record: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("memory.correct_fact: commit: %w", err)
	}
	return newID, nil
}

// ForgetFact permanently deletes a fact. Conflict rows referencing it cascade
// via foreign keys; facts that pointed at it via superseded_by are set NULL by
// the schema. This is deletion, not retraction — use RetractFact to keep the
// row but exclude it from use.
func (s *Store) ForgetFact(ctx context.Context, id int64) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM key_facts WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("memory.forget_fact: %w", err)
	}
	if n, _ := result.RowsAffected(); n == 0 {
		return fmt.Errorf("memory.forget_fact: fact %d: %w", id, ErrNotFound)
	}
	return nil
}

// RetractFact marks a fact retracted without deleting it.
func (s *Store) RetractFact(ctx context.Context, id int64) error {
	result, err := s.db.ExecContext(ctx,
		`UPDATE key_facts SET status = ? WHERE id = ?`, FactStatusRetracted, id)
	if err != nil {
		return fmt.Errorf("memory.retract_fact: %w", err)
	}
	if n, _ := result.RowsAffected(); n == 0 {
		return fmt.Errorf("memory.retract_fact: fact %d: %w", id, ErrNotFound)
	}
	return nil
}

// ForgetResult reports what a cascade deletion removed.
type ForgetResult struct {
	Facts   int64 `json:"facts"`
	Vectors int64 `json:"vectors"`
	Rows    int64 `json:"rows"`
}

// ForgetThought deletes a captured thought AND everything derived from it:
// key facts whose provenance points at it (their conflict rows cascade), its
// embedding, and its full-text index entry (removed by the FTS delete
// trigger). One transaction — either the thought and all derivations
// disappear, or nothing does.
func (s *Store) ForgetThought(ctx context.Context, id int64) (ForgetResult, error) {
	return s.forgetSourceRow(ctx, "captured_thoughts", "thought", id)
}

// ForgetTranscript deletes one transcript row and everything derived from it,
// mirroring ForgetThought.
func (s *Store) ForgetTranscript(ctx context.Context, id int64) (ForgetResult, error) {
	return s.forgetSourceRow(ctx, "session_transcripts", "transcript", id)
}

// forgetSourceRow implements provenance-cascade deletion for a memory source
// table. table names are fixed by the two exported callers — never
// caller-supplied.
func (s *Store) forgetSourceRow(ctx context.Context, table, sourceType string, id int64) (ForgetResult, error) {
	var res ForgetResult
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return res, fmt.Errorf("memory.forget: begin: %w", err)
	}
	defer tx.Rollback()

	// Derived facts first (memory_conflicts rows cascade with them).
	r, err := tx.ExecContext(ctx,
		`DELETE FROM key_facts WHERE source_type = ? AND source_id = ?`, sourceType, id)
	if err != nil {
		return res, fmt.Errorf("memory.forget: facts: %w", err)
	}
	res.Facts, _ = r.RowsAffected()

	r, err = tx.ExecContext(ctx,
		`DELETE FROM memory_vectors WHERE source_type = ? AND source_id = ?`, sourceType, id)
	if err != nil {
		return res, fmt.Errorf("memory.forget: vectors: %w", err)
	}
	res.Vectors, _ = r.RowsAffected()

	// The source row itself; the FTS sync trigger removes the index entry.
	var q string
	switch table {
	case "captured_thoughts":
		q = `DELETE FROM captured_thoughts WHERE id = ?`
	case "session_transcripts":
		q = `DELETE FROM session_transcripts WHERE id = ?`
	default:
		return res, fmt.Errorf("memory.forget: unknown table %q", table)
	}
	r, err = tx.ExecContext(ctx, q, id)
	if err != nil {
		return res, fmt.Errorf("memory.forget: source row: %w", err)
	}
	res.Rows, _ = r.RowsAffected()
	if res.Rows == 0 {
		return res, fmt.Errorf("memory.forget: %s %d: %w", sourceType, id, ErrNotFound)
	}

	if err := tx.Commit(); err != nil {
		return res, fmt.Errorf("memory.forget: commit: %w", err)
	}
	return res, nil
}

// PinFact sets or clears the pinned flag. The flag is stored now; scored
// context selection (which gives pinned facts absolute priority) consumes it
// in a follow-up change.
func (s *Store) PinFact(ctx context.Context, id int64, pinned bool) error {
	result, err := s.db.ExecContext(ctx,
		`UPDATE key_facts SET pinned = ? WHERE id = ?`, boolToInt(pinned), id)
	if err != nil {
		return fmt.Errorf("memory.pin_fact: %w", err)
	}
	if n, _ := result.RowsAffected(); n == 0 {
		return fmt.Errorf("memory.pin_fact: fact %d: %w", id, ErrNotFound)
	}
	return nil
}

// SetFactImportance sets a fact's 1-10 salience.
func (s *Store) SetFactImportance(ctx context.Context, id int64, importance int) error {
	if importance < 1 || importance > 10 {
		return fmt.Errorf("memory.set_importance: importance must be 1-10, got %d", importance)
	}
	result, err := s.db.ExecContext(ctx,
		`UPDATE key_facts SET importance = ? WHERE id = ?`, importance, id)
	if err != nil {
		return fmt.Errorf("memory.set_importance: %w", err)
	}
	if n, _ := result.RowsAffected(); n == 0 {
		return fmt.Errorf("memory.set_importance: fact %d: %w", id, ErrNotFound)
	}
	return nil
}

// TouchFactAccess bumps access tracking for facts that were retrieved into a
// turn. Frequency of use is a promotion signal for the always-in-context
// working set. One statement for the whole batch; callers treat errors as
// best-effort.
func (s *Store) TouchFactAccess(ctx context.Context, ids []int64) error {
	if len(ids) == 0 {
		return nil
	}
	now := time.Now().UTC().Format(time.RFC3339)
	placeholders := strings.Repeat("?,", len(ids))
	placeholders = placeholders[:len(placeholders)-1]
	args := make([]interface{}, 0, len(ids)+1)
	args = append(args, now)
	for _, id := range ids {
		args = append(args, id)
	}
	// The query text varies only in the number of bound placeholders — ids
	// are always parameters, never concatenated values.
	q := `UPDATE key_facts SET access_count = access_count + 1, last_accessed = ? WHERE id IN (` + placeholders + `)`
	if _, err := s.db.ExecContext(ctx, q, args...); err != nil {
		return fmt.Errorf("memory.touch_access: %w", err)
	}
	return nil
}

// Conflict is one recorded contradiction between two facts.
type Conflict struct {
	ID         int64  `json:"id"`
	OldFactID  int64  `json:"old_fact_id"`
	NewFactID  int64  `json:"new_fact_id"`
	OldFact    string `json:"old_fact"`
	NewFact    string `json:"new_fact"`
	FactKey    string `json:"fact_key"`
	DetectedAt string `json:"detected_at"`
	Resolution string `json:"resolution"`
	Reviewed   bool   `json:"reviewed"`
}

// GetConflicts lists recorded contradictions. pendingOnly restricts to the
// user's review queue — unreviewed needs_review rows — so routine
// auto-supersede history (which nothing ever marks reviewed) can't crowd
// flagged items out of the LIMIT window.
func (s *Store) GetConflicts(ctx context.Context, pendingOnly bool, limit int) ([]Conflict, error) {
	if limit <= 0 {
		limit = 50
	}
	q := `SELECT c.id, c.old_fact_id, c.new_fact_id,
	             COALESCE(o.fact, '(deleted)'), COALESCE(n.fact, '(deleted)'),
	             COALESCE(c.fact_key, ''), c.detected_at, c.resolution, c.reviewed
	      FROM memory_conflicts c
	      LEFT JOIN key_facts o ON o.id = c.old_fact_id
	      LEFT JOIN key_facts n ON n.id = c.new_fact_id`
	if pendingOnly {
		q += ` WHERE c.reviewed = 0 AND c.resolution = 'needs_review'`
	}
	q += ` ORDER BY c.reviewed ASC, c.detected_at DESC LIMIT ?`

	rows, err := s.db.QueryContext(ctx, q, limit)
	if err != nil {
		return nil, fmt.Errorf("memory.get_conflicts: %w", err)
	}
	defer rows.Close()

	var out []Conflict
	for rows.Next() {
		var c Conflict
		var reviewed int
		if err := rows.Scan(&c.ID, &c.OldFactID, &c.NewFactID, &c.OldFact, &c.NewFact,
			&c.FactKey, &c.DetectedAt, &c.Resolution, &reviewed); err != nil {
			return nil, fmt.Errorf("memory.get_conflicts: scan: %w", err)
		}
		c.Reviewed = reviewed != 0
		out = append(out, c)
	}
	return out, rows.Err()
}

// MarkConflictReviewed records that the user has seen a conflict.
func (s *Store) MarkConflictReviewed(ctx context.Context, id int64) error {
	result, err := s.db.ExecContext(ctx,
		`UPDATE memory_conflicts SET reviewed = 1 WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("memory.mark_reviewed: %w", err)
	}
	if n, _ := result.RowsAffected(); n == 0 {
		return fmt.Errorf("memory.mark_reviewed: conflict %d: %w", id, ErrNotFound)
	}
	return nil
}

// RestoreConflict rejects a replacement: the new fact is retracted, the old
// fact becomes the active holder again, and the conflict is closed as
// user_restored. One transaction; the one-active-per-key invariant is
// re-established defensively before reactivation.
func (s *Store) RestoreConflict(ctx context.Context, id int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("memory.restore: begin: %w", err)
	}
	defer tx.Rollback()

	var oldID, newID int64
	var factKey sql.NullString
	err = tx.QueryRowContext(ctx,
		`SELECT old_fact_id, new_fact_id, fact_key FROM memory_conflicts WHERE id = ?`, id).
		Scan(&oldID, &newID, &factKey)
	if err == sql.ErrNoRows {
		return fmt.Errorf("memory.restore: conflict %d: %w", id, ErrNotFound)
	}
	if err != nil {
		return fmt.Errorf("memory.restore: %w", err)
	}

	// The rejected replacement is retracted (kept for history, never used).
	if _, err := tx.ExecContext(ctx,
		`UPDATE key_facts SET status = ? WHERE id = ?`, FactStatusRetracted, newID); err != nil {
		return fmt.Errorf("memory.restore: retract new: %w", err)
	}
	// Defensive: no other active holder of the key may remain before the old
	// value is reactivated.
	if factKey.Valid && factKey.String != "" {
		if _, err := tx.ExecContext(ctx,
			`UPDATE key_facts SET status = ? WHERE fact_key = ? AND status = ?`,
			FactStatusSuperseded, factKey.String, FactStatusActive); err != nil {
			return fmt.Errorf("memory.restore: clear key: %w", err)
		}
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE key_facts SET status = ?, superseded_by = NULL WHERE id = ?`,
		FactStatusActive, oldID); err != nil {
		return fmt.Errorf("memory.restore: reactivate old: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE memory_conflicts SET reviewed = 1, resolution = ? WHERE id = ?`,
		ResolutionUserRestored, id); err != nil {
		return fmt.Errorf("memory.restore: close conflict: %w", err)
	}
	return tx.Commit()
}

// getFact loads one fact row with all quality columns.
func (s *Store) getFact(ctx context.Context, id int64) (KeyFact, error) {
	var f KeyFact
	var pinned int
	err := s.db.QueryRowContext(ctx,
		`SELECT id, fact, COALESCE(category, ''), COALESCE(source_session, ''),
		        extracted_at, COALESCE(fact_key, ''), importance, confidence,
		        COALESCE(source_type, ''), COALESCE(source_id, 0),
		        COALESCE(last_accessed, ''), access_count, status,
		        COALESCE(superseded_by, 0), pinned
		 FROM key_facts WHERE id = ?`, id).
		Scan(&f.ID, &f.Fact, &f.Category, &f.SourceSession, &f.ExtractedAt,
			&f.FactKey, &f.Importance, &f.Confidence, &f.SourceType, &f.SourceID,
			&f.LastAccessed, &f.AccessCount, &f.Status, &f.SupersededBy, &pinned)
	if err == sql.ErrNoRows {
		return f, fmt.Errorf("memory.get_fact: fact %d: %w", id, ErrNotFound)
	}
	if err != nil {
		return f, fmt.Errorf("memory.get_fact: %w", err)
	}
	f.Pinned = pinned != 0
	return f, nil
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
