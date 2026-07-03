// Package changelog is the human-reviewable ledger of every change the
// system makes to its own behavior — skill creation/patching/archival,
// heuristic changes, context-policy changes. Each entry links the eval run
// that justified the change and the human decision that approved it, and is
// mirrored into the existing audit trail so one query still answers "what
// did the system do" across every surface.
package changelog

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/LumabyteCo/aibutler/internal/capability"
)

// Change kinds.
const (
	KindSkillCreated  = "skill_created"
	KindSkillPatched  = "skill_patched"
	KindSkillArchived = "skill_archived"
	KindHeuristic     = "heuristic_changed"
	KindContextPolicy = "core_memory_policy_changed"
)

// Entry is one recorded self-change.
type Entry struct {
	ID         int64  `json:"id"`
	Kind       string `json:"kind"`
	SubjectID  string `json:"subject_id"`
	BeforeHash string `json:"before_hash,omitempty"`
	AfterHash  string `json:"after_hash,omitempty"`
	EvalRunID  int64  `json:"eval_run_id,omitempty"`
	ApprovedBy string `json:"approved_by,omitempty"`
	CreatedAt  string `json:"created_at"`
}

// AccessLogger mirrors entries into resource_access_log.
type AccessLogger interface {
	LogAccess(ctx context.Context, entry capability.AuditEntry) error
}

// Ledger records and lists self-changes.
type Ledger struct {
	db      *sql.DB
	auditor AccessLogger // optional
}

// New creates a ledger. auditor may be nil (entries still land in
// agent_changes; the audit mirror is best-effort).
func New(db *sql.DB, auditor AccessLogger) *Ledger {
	return &Ledger{db: db, auditor: auditor}
}

// Record appends an entry and mirrors it to the audit trail.
func (l *Ledger) Record(ctx context.Context, e Entry) (int64, error) {
	if e.Kind == "" || e.SubjectID == "" {
		return 0, fmt.Errorf("changelog: kind and subject are required")
	}
	res, err := l.db.ExecContext(ctx,
		`INSERT INTO agent_changes (kind, subject_id, before_hash, after_hash, eval_run_id, approved_by)
		 VALUES (?, ?, NULLIF(?, ''), NULLIF(?, ''), NULLIF(?, 0), NULLIF(?, ''))`,
		e.Kind, e.SubjectID, e.BeforeHash, e.AfterHash, e.EvalRunID, e.ApprovedBy)
	if err != nil {
		return 0, fmt.Errorf("changelog: record: %w", err)
	}
	id, _ := res.LastInsertId()

	if l.auditor != nil {
		_ = l.auditor.LogAccess(ctx, capability.AuditEntry{
			AgentType:      "self_change",
			ResourceType:   "behavior",
			Service:        "changelog",
			Action:         e.Kind,
			Target:         e.SubjectID,
			CapabilityUsed: "skill.propose",
			Status:         "recorded",
		})
	}
	return id, nil
}

// List returns recent entries, newest first.
func (l *Ledger) List(ctx context.Context, limit int) ([]Entry, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := l.db.QueryContext(ctx,
		`SELECT id, kind, subject_id, COALESCE(before_hash, ''), COALESCE(after_hash, ''),
		        COALESCE(eval_run_id, 0), COALESCE(approved_by, ''), created_at
		 FROM agent_changes ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("changelog: list: %w", err)
	}
	defer rows.Close()
	var out []Entry
	for rows.Next() {
		var e Entry
		if err := rows.Scan(&e.ID, &e.Kind, &e.SubjectID, &e.BeforeHash, &e.AfterHash,
			&e.EvalRunID, &e.ApprovedBy, &e.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
