// Package notification stores messages that could not be delivered live —
// currently scheduled-run results for webchat when no tab is connected.
// They are delivered on the next connection, then marked delivered.
package notification

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

const (
	maxPendingPerChannel = 200
	deliveredRetention   = 30 * 24 * time.Hour
)

// Notification is one durable outbound message.
type Notification struct {
	ID          int64
	Channel     string
	AccountID   string
	Text        string
	CreatedAt   string
	DeliveredAt *string
}

// Store persists pending notifications in SQLite.
type Store struct {
	db *sql.DB
}

// NewStore creates a notification store.
func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}

// Enqueue stores a message for later delivery. Empty text is a no-op.
// Delivered notifications older than 30 days are pruned, and each channel
// keeps at most 200 pending messages (oldest dropped) so an offline webchat
// cannot grow unbounded.
func (s *Store) Enqueue(ctx context.Context, channel, accountID, text string) (int64, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return 0, nil
	}
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO notifications (channel, account_id, text, created_at) VALUES (?, ?, ?, ?)`,
		channel, accountID, text, now)
	if err != nil {
		return 0, fmt.Errorf("notification.enqueue: %w", err)
	}
	id, _ := res.LastInsertId()

	cutoff := time.Now().UTC().Add(-deliveredRetention).Format(time.RFC3339)
	_, _ = s.db.ExecContext(ctx,
		`DELETE FROM notifications WHERE delivered_at IS NOT NULL AND delivered_at < ?`, cutoff)
	_, _ = s.db.ExecContext(ctx,
		`DELETE FROM notifications
		 WHERE channel = ? AND delivered_at IS NULL
		   AND id NOT IN (
		     SELECT id FROM notifications
		     WHERE channel = ? AND delivered_at IS NULL
		     ORDER BY id DESC LIMIT ?
		   )`, channel, channel, maxPendingPerChannel)
	return id, nil
}

// Pending returns undelivered messages for a channel, oldest first.
func (s *Store) Pending(ctx context.Context, channel string, limit int) ([]Notification, error) {
	if limit <= 0 {
		limit = 10
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, channel, account_id, text, created_at, delivered_at
		 FROM notifications
		 WHERE channel = ? AND delivered_at IS NULL
		 ORDER BY id ASC LIMIT ?`, channel, limit)
	if err != nil {
		return nil, fmt.Errorf("notification.pending: %w", err)
	}
	defer rows.Close()

	var out []Notification
	for rows.Next() {
		var n Notification
		var delivered sql.NullString
		if err := rows.Scan(&n.ID, &n.Channel, &n.AccountID, &n.Text, &n.CreatedAt, &delivered); err != nil {
			return nil, fmt.Errorf("notification.pending: scan: %w", err)
		}
		if delivered.Valid {
			n.DeliveredAt = &delivered.String
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

// MarkDelivered records successful delivery.
func (s *Store) MarkDelivered(ctx context.Context, id int64) error {
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := s.db.ExecContext(ctx,
		`UPDATE notifications SET delivered_at = ? WHERE id = ?`, now, id); err != nil {
		return fmt.Errorf("notification.mark_delivered: %w", err)
	}
	return nil
}

// CountPending returns the number of undelivered messages for a channel.
func (s *Store) CountPending(ctx context.Context, channel string) (int, error) {
	var n int
	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM notifications WHERE channel = ? AND delivered_at IS NULL`,
		channel).Scan(&n); err != nil {
		return 0, fmt.Errorf("notification.count: %w", err)
	}
	return n, nil
}
