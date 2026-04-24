package db

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/ncruces/go-sqlite3/driver"
	_ "github.com/ncruces/go-sqlite3/embed"
	_ "github.com/ncruces/go-sqlite3/vfs/adiantum"
)

// DB wraps a *sql.DB with AI Butler-specific configuration.
type DB struct {
	conn     *sql.DB
	path     string
	readonly bool
}

// Config holds database initialization parameters.
type Config struct {
	Path             string // Path to the database file (":memory:" for tests)
	Passphrase       string // Passphrase for Adiantum VFS encryption (empty = no encryption)
	ReadOnly         bool
	CacheSize        int // PRAGMA cache_size in KiB pages (default: -2000 = ~2MB)
	JournalSizeLimit int // PRAGMA journal_size_limit in bytes (default: 67108864 = 64MB)
	BusyTimeout      int // PRAGMA busy_timeout in ms (default: 5000)
}

// Open creates or opens an encrypted SQLite database.
func Open(cfg Config) (*DB, error) {
	dsn := cfg.Path

	// Use Adiantum VFS for encryption when a passphrase is provided.
	if cfg.Passphrase != "" && cfg.Path != ":memory:" {
		dsn = fmt.Sprintf("file:%s?vfs=adiantum&_pragma=hexkey(%x)", cfg.Path, cfg.Passphrase)
	}

	conn, err := driver.Open(dsn, registerVecFunctions)
	if err != nil {
		return nil, fmt.Errorf("db: open %s: %w", cfg.Path, err)
	}

	// Single connection for WAL mode consistency in single-user scenario.
	conn.SetMaxOpenConns(1)

	d := &DB{
		conn:     conn,
		path:     cfg.Path,
		readonly: cfg.ReadOnly,
	}

	if err := d.applyPragmas(cfg); err != nil {
		conn.Close()
		return nil, fmt.Errorf("db: pragmas: %w", err)
	}

	return d, nil
}

// Conn returns the underlying *sql.DB for use by other packages.
func (d *DB) Conn() *sql.DB {
	return d.conn
}

// Checkpoint performs a WAL checkpoint to flush the write-ahead log.
func (d *DB) Checkpoint() error {
	_, err := d.conn.Exec("PRAGMA wal_checkpoint(TRUNCATE)")
	return err
}

// Close flushes the WAL and closes the database connection.
func (d *DB) Close() error {
	d.Checkpoint() // best-effort WAL flush
	return d.conn.Close()
}

// IntegrityCheck runs PRAGMA integrity_check and returns an error if the database is corrupt.
func (d *DB) IntegrityCheck(ctx context.Context) error {
	var result string
	err := d.conn.QueryRowContext(ctx, "PRAGMA integrity_check").Scan(&result)
	if err != nil {
		return fmt.Errorf("db: integrity check query: %w", err)
	}
	if result != "ok" {
		return fmt.Errorf("db: integrity check failed: %s", result)
	}
	return nil
}

func (d *DB) applyPragmas(cfg Config) error {
	cacheSize := cfg.CacheSize
	if cacheSize == 0 {
		cacheSize = -2000 // ~2MB
	}
	journalSizeLimit := cfg.JournalSizeLimit
	if journalSizeLimit == 0 {
		journalSizeLimit = 67108864 // 64MB
	}
	busyTimeout := cfg.BusyTimeout
	if busyTimeout == 0 {
		busyTimeout = 5000
	}

	pragmas := []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA synchronous=NORMAL",
		"PRAGMA foreign_keys=ON",
		fmt.Sprintf("PRAGMA busy_timeout=%d", busyTimeout),
		fmt.Sprintf("PRAGMA cache_size=%d", cacheSize),
		fmt.Sprintf("PRAGMA journal_size_limit=%d", journalSizeLimit),
	}

	// 60s deadline — generous to accommodate first-time WASM SQLite runtime
	// initialization on cold starts, especially under the race detector on
	// CI runners where WASM compilation can take 5–15s. Each individual pragma
	// is nearly instant once the runtime is warm, so the real budget is
	// dominated by the one-time module compile on the first Open() call in a
	// process.
	execCtx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	for _, p := range pragmas {
		if _, err := d.conn.ExecContext(execCtx, p); err != nil {
			return fmt.Errorf("%s: %w", p, err)
		}
	}
	return nil
}
