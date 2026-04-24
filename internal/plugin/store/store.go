package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// ErrNotFound is returned when a key does not exist.
var ErrNotFound = errors.New("plugin store: key not found")

const (
	maxKeyLen   = 256         // Max key length in bytes
	maxValueLen = 1024 * 1024 // 1MB max value size
)

// Store provides plugin-scoped key-value storage backed by the plugin_kv table.
type Store struct {
	db       *sql.DB
	pluginID int64
}

// New creates a new plugin-scoped KV store.
func New(db *sql.DB, pluginID int64) *Store {
	return &Store{db: db, pluginID: pluginID}
}

// Get retrieves a value by key.
func (s *Store) Get(ctx context.Context, key string) ([]byte, error) {
	if err := validateKey(key); err != nil {
		return nil, err
	}
	var value []byte
	err := s.db.QueryRowContext(ctx,
		"SELECT value FROM plugin_kv WHERE plugin_id = ? AND key = ?",
		s.pluginID, key).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("plugin store: get %q: %w", key, err)
	}
	return value, nil
}

func validateKey(key string) error {
	if key == "" {
		return fmt.Errorf("plugin store: key cannot be empty")
	}
	if len(key) > maxKeyLen {
		return fmt.Errorf("plugin store: key too long (%d > %d)", len(key), maxKeyLen)
	}
	return nil
}

// Set stores a value. Creates or updates the key.
func (s *Store) Set(ctx context.Context, key string, value []byte) error {
	if err := validateKey(key); err != nil {
		return err
	}
	if len(value) > maxValueLen {
		return fmt.Errorf("plugin store: value too large (%d > %d bytes)", len(value), maxValueLen)
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO plugin_kv (plugin_id, key, value) VALUES (?, ?, ?)
		 ON CONFLICT(plugin_id, key) DO UPDATE SET value = excluded.value, updated_at = datetime('now')`,
		s.pluginID, key, value)
	if err != nil {
		return fmt.Errorf("plugin store: set %q: %w", key, err)
	}
	return nil
}

// Delete removes a key. No error if key doesn't exist.
func (s *Store) Delete(ctx context.Context, key string) error {
	if err := validateKey(key); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx,
		"DELETE FROM plugin_kv WHERE plugin_id = ? AND key = ?",
		s.pluginID, key)
	if err != nil {
		return fmt.Errorf("plugin store: delete %q: %w", key, err)
	}
	return nil
}

// List returns all keys for this plugin.
func (s *Store) List(ctx context.Context) ([]string, error) {
	rows, err := s.db.QueryContext(ctx,
		"SELECT key FROM plugin_kv WHERE plugin_id = ? ORDER BY key",
		s.pluginID)
	if err != nil {
		return nil, fmt.Errorf("plugin store: list: %w", err)
	}
	defer rows.Close()

	var keys []string
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			return nil, fmt.Errorf("plugin store: list scan: %w", err)
		}
		keys = append(keys, k)
	}
	return keys, rows.Err()
}

// Has returns true if the key exists.
func (s *Store) Has(ctx context.Context, key string) (bool, error) {
	if err := validateKey(key); err != nil {
		return false, err
	}
	var exists int
	err := s.db.QueryRowContext(ctx,
		"SELECT 1 FROM plugin_kv WHERE plugin_id = ? AND key = ? LIMIT 1",
		s.pluginID, key).Scan(&exists)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("plugin store: has %q: %w", key, err)
	}
	return true, nil
}
