package instruction

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// Instruction categories.
const (
	CategoryStyle      = "style"
	CategoryBehavior   = "behavior"
	CategoryRule       = "rule"
	CategoryKnowledge  = "knowledge"
	CategoryPreference = "preference"
)

// Scopes.
const (
	ScopeGlobal  = "global"
	ScopeChannel = "channel"
	ScopeSession = "session"
)

// Sources.
const (
	SourceExplicit = "explicit"
	SourceAuto     = "auto-detected"
)

// Instruction represents a learned directive from the user.
type Instruction struct {
	ID         int64  `json:"id"`
	Content    string `json:"content"`
	Category   string `json:"category"`
	Priority   int    `json:"priority"`
	Scope      string `json:"scope"`
	ScopeValue string `json:"scope_value,omitempty"`
	Active     bool   `json:"active"`
	Source     string `json:"source"`
	SourceText string `json:"source_text,omitempty"`
	CreatedAt  string `json:"created_at"`
	UpdatedAt  string `json:"updated_at"`
	ExpiresAt  string `json:"expires_at,omitempty"`
}

// ListQuery holds optional filters for listing instructions.
type ListQuery struct {
	Category   string
	Scope      string
	ActiveOnly bool
}

// Store manages learned instruction persistence.
type Store struct {
	db *sql.DB
}

// NewStore creates an instruction store.
func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}

// Save persists a new instruction. Returns the ID.
func (s *Store) Save(ctx context.Context, content, category string, priority int, scope, scopeValue, source, sourceText string) (int64, error) {
	if content == "" {
		return 0, fmt.Errorf("instruction: content is required")
	}
	if category == "" {
		category = CategoryRule
	}
	if scope == "" {
		scope = ScopeGlobal
	}
	if source == "" {
		source = SourceExplicit
	}
	now := time.Now().UTC().Format(time.RFC3339)

	result, err := s.db.ExecContext(ctx,
		`INSERT INTO learned_instructions (content, category, priority, scope, scope_value, source, source_text, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		content, category, priority, scope, scopeValue, source, sourceText, now, now)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return 0, fmt.Errorf("instruction: duplicate instruction for this scope")
		}
		return 0, fmt.Errorf("instruction.save: %w", err)
	}
	return result.LastInsertId()
}

// Get returns an instruction by ID.
func (s *Store) Get(ctx context.Context, id int64) (*Instruction, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, content, category, priority, scope, COALESCE(scope_value, ''), active, source,
		        COALESCE(source_text, ''), created_at, updated_at, COALESCE(expires_at, '')
		 FROM learned_instructions WHERE id = ?`, id)

	var inst Instruction
	var active int
	err := row.Scan(&inst.ID, &inst.Content, &inst.Category, &inst.Priority,
		&inst.Scope, &inst.ScopeValue, &active, &inst.Source,
		&inst.SourceText, &inst.CreatedAt, &inst.UpdatedAt, &inst.ExpiresAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("instruction.get: %w", err)
	}
	inst.Active = active == 1
	return &inst, nil
}

// List returns instructions matching the query.
func (s *Store) List(ctx context.Context, q ListQuery) ([]Instruction, error) {
	query := `SELECT id, content, category, priority, scope, COALESCE(scope_value, ''), active, source,
	                 COALESCE(source_text, ''), created_at, updated_at, COALESCE(expires_at, '')
	          FROM learned_instructions`
	var conditions []string
	var args []interface{}

	if q.ActiveOnly {
		conditions = append(conditions, `active = 1`)
	}
	if q.Category != "" {
		conditions = append(conditions, `category = ?`)
		args = append(args, q.Category)
	}
	if q.Scope != "" {
		conditions = append(conditions, `scope = ?`)
		args = append(args, q.Scope)
	}
	if len(conditions) > 0 {
		query += " WHERE " + strings.Join(conditions, " AND ")
	}
	query += " ORDER BY priority DESC, created_at ASC"

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("instruction.list: %w", err)
	}
	defer rows.Close()

	return scanInstructions(rows)
}

// Update modifies an instruction's mutable fields. Only non-zero values are applied.
func (s *Store) Update(ctx context.Context, id int64, content string, priority int, category string, active *bool) error {
	var sets []string
	var args []interface{}
	now := time.Now().UTC().Format(time.RFC3339)

	if content != "" {
		sets = append(sets, "content = ?")
		args = append(args, content)
	}
	if priority > 0 {
		sets = append(sets, "priority = ?")
		args = append(args, priority)
	}
	if category != "" {
		sets = append(sets, "category = ?")
		args = append(args, category)
	}
	if active != nil {
		val := 0
		if *active {
			val = 1
		}
		sets = append(sets, "active = ?")
		args = append(args, val)
	}

	if len(sets) == 0 {
		return nil
	}

	sets = append(sets, "updated_at = ?")
	args = append(args, now)
	args = append(args, id)

	_, err := s.db.ExecContext(ctx,
		fmt.Sprintf("UPDATE learned_instructions SET %s WHERE id = ?", strings.Join(sets, ", ")),
		args...)
	if err != nil {
		return fmt.Errorf("instruction.update: %w", err)
	}
	return nil
}

// Remove deletes an instruction permanently.
func (s *Store) Remove(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM learned_instructions WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("instruction.remove: %w", err)
	}
	return nil
}

// ActiveForPrompt returns all active, non-expired instructions applicable to the given scope.
// Global instructions are always returned. Channel and session instructions are returned
// only when the scope matches.
func (s *Store) ActiveForPrompt(ctx context.Context, channel, sessionID string) ([]Instruction, error) {
	// Clean up expired instructions first.
	s.ExpireStale(ctx)

	now := time.Now().UTC().Format(time.RFC3339)
	query := `SELECT id, content, category, priority, scope, COALESCE(scope_value, ''), active, source,
	                 COALESCE(source_text, ''), created_at, updated_at, COALESCE(expires_at, '')
	          FROM learned_instructions
	          WHERE active = 1 AND (expires_at IS NULL OR expires_at > ?)
	            AND (scope = 'global'
	              OR (scope = 'channel' AND scope_value = ?)
	              OR (scope = 'session' AND scope_value = ?))
	          ORDER BY priority DESC, created_at ASC`

	rows, err := s.db.QueryContext(ctx, query, now, channel, sessionID)
	if err != nil {
		return nil, fmt.Errorf("instruction.active_for_prompt: %w", err)
	}
	defer rows.Close()

	return scanInstructions(rows)
}

// Count returns the number of active instructions.
func (s *Store) Count(ctx context.Context) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM learned_instructions WHERE active = 1`).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("instruction.count: %w", err)
	}
	return count, nil
}

// ExpireStale deactivates instructions past their expires_at time. Returns count of expired.
func (s *Store) ExpireStale(ctx context.Context) (int, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	result, err := s.db.ExecContext(ctx,
		`UPDATE learned_instructions SET active = 0 WHERE active = 1 AND expires_at IS NOT NULL AND expires_at <= ?`, now)
	if err != nil {
		return 0, fmt.Errorf("instruction.expire: %w", err)
	}
	n, _ := result.RowsAffected()
	return int(n), nil
}

func scanInstructions(rows *sql.Rows) ([]Instruction, error) {
	var instructions []Instruction
	for rows.Next() {
		var inst Instruction
		var active int
		if err := rows.Scan(&inst.ID, &inst.Content, &inst.Category, &inst.Priority,
			&inst.Scope, &inst.ScopeValue, &active, &inst.Source,
			&inst.SourceText, &inst.CreatedAt, &inst.UpdatedAt, &inst.ExpiresAt); err != nil {
			return nil, fmt.Errorf("instruction.scan: %w", err)
		}
		inst.Active = active == 1
		instructions = append(instructions, inst)
	}
	return instructions, rows.Err()
}
