package cache

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"strings"
	"sync/atomic"
	"time"
)

// Config holds response cache settings.
type Config struct {
	Enabled    bool
	DefaultTTL time.Duration          // default 5min
	MaxEntries int                    // default 1000
	ToolTTLs   map[string]time.Duration // per-tool overrides (e.g., "web.fetch": 5min, "shell.exec": 0)
}

// DefaultConfig returns a cache configuration with sensible defaults.
func DefaultConfig() Config {
	return Config{
		Enabled:    true,
		DefaultTTL: 5 * time.Minute,
		MaxEntries: 1000,
		ToolTTLs:   map[string]time.Duration{},
	}
}

// CacheStats holds cache performance metrics.
type CacheStats struct {
	TotalEntries int
	HitCount     int64
	MissCount    int64
	HitRate      float64
}

// Cache provides a SQLite-backed response cache with TTL support.
type Cache struct {
	db       *sql.DB
	cfg      Config
	hitCount int64
	missCount int64
}

// New creates a new Cache backed by the provided database connection.
func New(db *sql.DB, cfg Config) *Cache {
	if cfg.DefaultTTL <= 0 {
		cfg.DefaultTTL = 5 * time.Minute
	}
	if cfg.MaxEntries <= 0 {
		cfg.MaxEntries = 1000
	}
	return &Cache{
		db:  db,
		cfg: cfg,
	}
}

// Get retrieves a cached value by key. Returns the value, whether it was found,
// and any error. Expired entries are not returned.
func (c *Cache) Get(ctx context.Context, key string) (string, bool, error) {
	if !c.cfg.Enabled {
		atomic.AddInt64(&c.missCount, 1)
		return "", false, nil
	}

	var value string
	err := c.db.QueryRowContext(ctx,
		`SELECT value FROM response_cache WHERE key = ? AND expires_at > datetime('now')`,
		key,
	).Scan(&value)

	if err == sql.ErrNoRows {
		atomic.AddInt64(&c.missCount, 1)
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("cache get: %w", err)
	}

	// Update hit count in the database.
	_, _ = c.db.ExecContext(ctx,
		`UPDATE response_cache SET hit_count = hit_count + 1 WHERE key = ?`, key)

	atomic.AddInt64(&c.hitCount, 1)
	return value, true, nil
}

// Set stores a value in the cache with the given TTL.
// If the cache exceeds MaxEntries, the oldest expired entries are pruned first.
func (c *Cache) Set(ctx context.Context, key, value string, ttl time.Duration) error {
	if !c.cfg.Enabled {
		return nil
	}

	expiresAt := time.Now().Add(ttl)

	_, err := c.db.ExecContext(ctx,
		`INSERT OR REPLACE INTO response_cache (key, value, hit_count, created_at, expires_at)
		 VALUES (?, ?, 0, datetime('now'), ?)`,
		key, value, expiresAt.UTC().Format("2006-01-02 15:04:05"),
	)
	if err != nil {
		return fmt.Errorf("cache set: %w", err)
	}

	return nil
}

// SetWithTool stores a value with tool name metadata for per-tool TTL tracking.
func (c *Cache) SetWithTool(ctx context.Context, key, value, toolName string, ttl time.Duration) error {
	if !c.cfg.Enabled {
		return nil
	}

	expiresAt := time.Now().Add(ttl)

	_, err := c.db.ExecContext(ctx,
		`INSERT OR REPLACE INTO response_cache (key, value, tool_name, hit_count, created_at, expires_at)
		 VALUES (?, ?, ?, 0, datetime('now'), ?)`,
		key, value, toolName, expiresAt.UTC().Format("2006-01-02 15:04:05"),
	)
	if err != nil {
		return fmt.Errorf("cache set: %w", err)
	}

	return nil
}

// Delete removes a cached entry by key.
func (c *Cache) Delete(ctx context.Context, key string) error {
	_, err := c.db.ExecContext(ctx,
		`DELETE FROM response_cache WHERE key = ?`, key)
	if err != nil {
		return fmt.Errorf("cache delete: %w", err)
	}
	return nil
}

// Prune removes all expired entries and returns the number removed.
func (c *Cache) Prune(ctx context.Context) (int, error) {
	result, err := c.db.ExecContext(ctx,
		`DELETE FROM response_cache WHERE expires_at <= datetime('now')`)
	if err != nil {
		return 0, fmt.Errorf("cache prune: %w", err)
	}
	n, _ := result.RowsAffected()
	return int(n), nil
}

// Stats returns current cache performance metrics.
func (c *Cache) Stats(ctx context.Context) (*CacheStats, error) {
	var total int
	err := c.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM response_cache WHERE expires_at > datetime('now')`).Scan(&total)
	if err != nil {
		return nil, fmt.Errorf("cache stats: %w", err)
	}

	hits := atomic.LoadInt64(&c.hitCount)
	misses := atomic.LoadInt64(&c.missCount)

	var hitRate float64
	if total := hits + misses; total > 0 {
		hitRate = float64(hits) / float64(total)
	}

	return &CacheStats{
		TotalEntries: total,
		HitCount:     hits,
		MissCount:    misses,
		HitRate:      hitRate,
	}, nil
}

// TTLForTool returns the TTL for a given tool name, falling back to DefaultTTL.
// Returns 0 if the tool is explicitly configured with 0 (no caching).
func (c *Cache) TTLForTool(toolName string) time.Duration {
	if ttl, ok := c.cfg.ToolTTLs[toolName]; ok {
		return ttl
	}
	return c.cfg.DefaultTTL
}

// HashKey creates a SHA-256 hash from concatenated parts, suitable for cache keys.
func HashKey(parts ...string) string {
	h := sha256.New()
	h.Write([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(h.Sum(nil))
}
