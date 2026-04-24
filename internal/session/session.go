package session

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"time"

	"github.com/LumabyteCo/aibutler/internal/agent"
	"github.com/LumabyteCo/aibutler/internal/config"
)

// Session represents an active conversation session.
type Session struct {
	ID        string
	Channel   string
	AccountID string
	Scope     string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// SessionRecorder receives notifications when sessions are created.
type SessionRecorder interface {
	RecordSession()
}

// Manager handles session lifecycle and message persistence.
type Manager struct {
	db       *sql.DB
	cfg      *config.Config
	recorder SessionRecorder
}

// NewManager creates a session manager.
func NewManager(db *sql.DB, cfg *config.Config) *Manager {
	return &Manager{db: db, cfg: cfg}
}

// SetRecorder sets the telemetry recorder for session tracking.
func (m *Manager) SetRecorder(r SessionRecorder) {
	m.recorder = r
}

// Create starts a new session and returns its ID.
func (m *Manager) Create(ctx context.Context, channel, accountID, scope string) (string, error) {
	id := fmt.Sprintf("sess-%d", time.Now().UnixNano())
	if scope == "" {
		scope = "default"
	}

	now := time.Now().UTC().Format(time.RFC3339)
	_, err := m.db.ExecContext(ctx,
		`INSERT INTO sessions (id, channel, account_id, scope, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)`,
		id, channel, accountID, scope, now, now)
	if err != nil {
		return "", fmt.Errorf("session.create: %w", err)
	}
	if m.recorder != nil {
		m.recorder.RecordSession()
	}
	return id, nil
}

// Get retrieves a session by ID.
func (m *Manager) Get(ctx context.Context, sessionID string) (*Session, error) {
	var s Session
	var createdAt, updatedAt string
	err := m.db.QueryRowContext(ctx,
		`SELECT id, channel, account_id, scope, created_at, updated_at FROM sessions WHERE id = ?`,
		sessionID).Scan(&s.ID, &s.Channel, &s.AccountID, &s.Scope, &createdAt, &updatedAt)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("session.get: not found: %s", sessionID)
	}
	if err != nil {
		return nil, fmt.Errorf("session.get: %w", err)
	}
	s.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	s.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
	return &s, nil
}

// Close updates a session's updated_at timestamp.
func (m *Manager) Close(ctx context.Context, sessionID string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := m.db.ExecContext(ctx,
		`UPDATE sessions SET updated_at = ? WHERE id = ?`,
		now, sessionID)
	if err != nil {
		return fmt.Errorf("session.close: %w", err)
	}
	return nil
}

// AddMessage persists a message to the messages table.
func (m *Manager) AddMessage(ctx context.Context, sessionID string, msg agent.Message) error {
	_, err := m.db.ExecContext(ctx,
		`INSERT INTO messages (session_id, role, content, tool_id) VALUES (?, ?, ?, ?)`,
		sessionID, msg.Role, msg.Content, nullString(msg.ToolID))
	if err != nil {
		return fmt.Errorf("session.add_message: %w", err)
	}
	return nil
}

// Messages retrieves all messages for a session in chronological order.
func (m *Manager) Messages(ctx context.Context, sessionID string) ([]agent.Message, error) {
	rows, err := m.db.QueryContext(ctx,
		`SELECT role, content, COALESCE(tool_id, '') FROM messages WHERE session_id = ? ORDER BY id ASC`,
		sessionID)
	if err != nil {
		return nil, fmt.Errorf("session.messages: %w", err)
	}
	defer rows.Close()

	return scanMessages(rows)
}

// RecentMessages retrieves the last N messages for a session.
func (m *Manager) RecentMessages(ctx context.Context, sessionID string, limit int) ([]agent.Message, error) {
	// Subquery to get the last N, then re-order chronologically.
	rows, err := m.db.QueryContext(ctx,
		`SELECT role, content, COALESCE(tool_id, '') FROM (
			SELECT id, role, content, tool_id FROM messages
			WHERE session_id = ? ORDER BY id DESC LIMIT ?
		) sub ORDER BY id ASC`,
		sessionID, limit)
	if err != nil {
		return nil, fmt.Errorf("session.recent_messages: %w", err)
	}
	defer rows.Close()

	return scanMessages(rows)
}

// SlidingWindow returns recent messages based on the config's cost strategy.
func (m *Manager) SlidingWindow(ctx context.Context, sessionID string) ([]agent.Message, error) {
	limit := m.cfg.SlidingWindowSize()
	return m.RecentMessages(ctx, sessionID, limit)
}

func scanMessages(rows *sql.Rows) ([]agent.Message, error) {
	var msgs []agent.Message
	for rows.Next() {
		var msg agent.Message
		if err := rows.Scan(&msg.Role, &msg.Content, &msg.ToolID); err != nil {
			return nil, fmt.Errorf("scan message: %w", err)
		}
		msgs = append(msgs, msg)
	}
	return msgs, rows.Err()
}

// Delete removes a session and all its messages.
func (m *Manager) Delete(ctx context.Context, sessionID string) error {
	_, err := m.db.ExecContext(ctx, `DELETE FROM messages WHERE session_id = ?`, sessionID)
	if err != nil {
		return fmt.Errorf("session.delete messages: %w", err)
	}
	_, err = m.db.ExecContext(ctx, `DELETE FROM sessions WHERE id = ?`, sessionID)
	if err != nil {
		return fmt.Errorf("session.delete: %w", err)
	}
	return nil
}

// CleanupExpired removes sessions older than the given duration.
// Returns the number of sessions cleaned up.
func (m *Manager) CleanupExpired(ctx context.Context, maxAge time.Duration) (int, error) {
	cutoff := time.Now().UTC().Add(-maxAge).Format(time.RFC3339)

	// Get expired session IDs.
	rows, err := m.db.QueryContext(ctx,
		`SELECT id FROM sessions WHERE updated_at < ?`, cutoff)
	if err != nil {
		return 0, fmt.Errorf("session.cleanup: query: %w", err)
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			continue
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("session.cleanup: scan: %w", err)
	}

	// Delete each session and its messages.
	for _, id := range ids {
		_ = m.Delete(ctx, id)
	}
	return len(ids), nil
}

// StartCleanupLoop runs periodic session cleanup in the background.
// It cleans up sessions older than maxAge every interval.
// Stops when ctx is cancelled.
func (m *Manager) StartCleanupLoop(ctx context.Context, interval, maxAge time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				count, err := m.CleanupExpired(ctx, maxAge)
				if err != nil {
					log.Printf("session cleanup: %v", err)
				} else if count > 0 {
					log.Printf("session cleanup: removed %d expired session(s)", count)
				}
			}
		}
	}()
}

// Count returns the total number of active sessions.
func (m *Manager) Count(ctx context.Context) (int, error) {
	var count int
	err := m.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sessions`).Scan(&count)
	return count, err
}

func nullString(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}
