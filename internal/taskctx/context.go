package taskctx

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

// TaskContext represents a multi-step task's persistent state.
type TaskContext struct {
	ID        int
	SessionID string
	TaskType  string
	State     string // "gathering", "processing", "awaiting_input", "completed", "abandoned"
	Context   map[string]interface{}
	CreatedAt time.Time
	UpdatedAt time.Time
	ExpiresAt *time.Time
}

// Store manages task execution contexts in SQLite.
type Store struct {
	db *sql.DB
}

// NewStore creates a task context store.
func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}

// Save creates or updates a task context. Enforces one-active-per-session
// by marking any existing active context as abandoned.
func (s *Store) Save(ctx context.Context, tc *TaskContext) (int, error) {
	now := time.Now().UTC()

	// If creating new (ID=0), abandon any existing active context for this session.
	if tc.ID == 0 {
		_, err := s.db.ExecContext(ctx,
			`UPDATE task_contexts SET state = 'abandoned', updated_at = ? WHERE session_id = ? AND state NOT IN ('completed', 'abandoned')`,
			now.Format(time.RFC3339), tc.SessionID)
		if err != nil {
			return 0, fmt.Errorf("taskctx.save: abandon existing: %w", err)
		}
	}

	ctxJSON, err := json.Marshal(tc.Context)
	if err != nil {
		return 0, fmt.Errorf("taskctx.save: marshal context: %w", err)
	}

	var expiresAt *string
	if tc.ExpiresAt != nil {
		s := tc.ExpiresAt.Format(time.RFC3339)
		expiresAt = &s
	} else {
		// Default: expire in 24 hours.
		exp := now.Add(24 * time.Hour).Format(time.RFC3339)
		expiresAt = &exp
	}

	if tc.ID == 0 {
		result, err := s.db.ExecContext(ctx,
			`INSERT INTO task_contexts (session_id, task_type, state, context, created_at, updated_at, expires_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
			tc.SessionID, tc.TaskType, tc.State, string(ctxJSON), now.Format(time.RFC3339), now.Format(time.RFC3339), expiresAt)
		if err != nil {
			return 0, fmt.Errorf("taskctx.save: insert: %w", err)
		}
		id, _ := result.LastInsertId()
		return int(id), nil
	}

	_, err = s.db.ExecContext(ctx,
		`UPDATE task_contexts SET state = ?, context = ?, updated_at = ?, expires_at = ? WHERE id = ?`,
		tc.State, string(ctxJSON), now.Format(time.RFC3339), expiresAt, tc.ID)
	if err != nil {
		return 0, fmt.Errorf("taskctx.save: update: %w", err)
	}
	return tc.ID, nil
}

// Load retrieves the active task context for a session.
func (s *Store) Load(ctx context.Context, sessionID string) (*TaskContext, error) {
	now := time.Now().UTC().Format(time.RFC3339)

	var tc TaskContext
	var ctxJSON, createdAt, updatedAt string
	var expiresAt sql.NullString

	err := s.db.QueryRowContext(ctx,
		`SELECT id, session_id, task_type, state, context, created_at, updated_at, expires_at
		 FROM task_contexts
		 WHERE session_id = ? AND state NOT IN ('completed', 'abandoned')
		   AND (expires_at IS NULL OR expires_at > ?)
		 ORDER BY id DESC LIMIT 1`,
		sessionID, now).Scan(
		&tc.ID, &tc.SessionID, &tc.TaskType, &tc.State,
		&ctxJSON, &createdAt, &updatedAt, &expiresAt)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("taskctx.load: %w", err)
	}

	tc.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	tc.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
	if expiresAt.Valid {
		t, _ := time.Parse(time.RFC3339, expiresAt.String)
		tc.ExpiresAt = &t
	}

	tc.Context = make(map[string]interface{})
	_ = json.Unmarshal([]byte(ctxJSON), &tc.Context)

	return &tc, nil
}

// Complete marks a task context as completed.
func (s *Store) Complete(ctx context.Context, id int) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE task_contexts SET state = 'completed', updated_at = ? WHERE id = ?`,
		time.Now().UTC().Format(time.RFC3339), id)
	return err
}
