package swarm

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// Workspace is a scoped ephemeral key-value store for a swarm run.
type Workspace struct {
	db *sql.DB
}

// NewWorkspace creates a swarm workspace.
func NewWorkspace(db *sql.DB) *Workspace {
	return &Workspace{db: db}
}

// Set stores or overwrites a key-value pair for the given run.
func (w *Workspace) Set(ctx context.Context, runID, key, value, writtenBy string) error {
	_, err := w.db.ExecContext(ctx,
		`INSERT INTO swarm_workspaces (run_id, key, value, written_by, written_at)
		 VALUES (?, ?, ?, ?, ?)
		 ON CONFLICT(run_id, key) DO UPDATE SET
		     value = excluded.value,
		     written_by = excluded.written_by,
		     written_at = excluded.written_at`,
		runID, key, value, writtenBy, time.Now().UTC().Format(time.RFC3339))
	if err != nil {
		return fmt.Errorf("workspace: set: %w", err)
	}
	return nil
}

// Get retrieves a value by key. Returns ("", nil) when key does not exist.
func (w *Workspace) Get(ctx context.Context, runID, key string) (string, error) {
	var value string
	err := w.db.QueryRowContext(ctx,
		`SELECT value FROM swarm_workspaces WHERE run_id = ? AND key = ?`,
		runID, key).Scan(&value)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("workspace: get: %w", err)
	}
	return value, nil
}

// List returns all key-value pairs for the given run.
func (w *Workspace) List(ctx context.Context, runID string) (map[string]string, error) {
	rows, err := w.db.QueryContext(ctx,
		`SELECT key, value FROM swarm_workspaces WHERE run_id = ? ORDER BY key`, runID)
	if err != nil {
		return nil, fmt.Errorf("workspace: list: %w", err)
	}
	defer rows.Close()
	result := make(map[string]string)
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		result[k] = v
	}
	return result, rows.Err()
}

// Purge deletes all workspace entries for the given run.
func (w *Workspace) Purge(ctx context.Context, runID string) error {
	_, err := w.db.ExecContext(ctx, `DELETE FROM swarm_workspaces WHERE run_id = ?`, runID)
	if err != nil {
		return fmt.Errorf("workspace: purge: %w", err)
	}
	return nil
}

// PurgeOlderThan removes entries written more than the given number of hours ago.
func (w *Workspace) PurgeOlderThan(ctx context.Context, hours int) (int64, error) {
	cutoff := time.Now().UTC().Add(-time.Duration(hours) * time.Hour).Format(time.RFC3339)
	res, err := w.db.ExecContext(ctx,
		`DELETE FROM swarm_workspaces WHERE written_at < ?`, cutoff)
	if err != nil {
		return 0, fmt.Errorf("workspace: purge older than: %w", err)
	}
	n, _ := res.RowsAffected()
	return n, nil
}
