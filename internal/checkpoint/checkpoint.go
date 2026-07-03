// Package checkpoint stores pre-images of files before agent-driven
// mutations, so any change the agent makes can be inspected and undone.
//
// The store is content-addressed and transactional: a snapshot records the
// file's bytes (or its absence) before a mutating tool touches it; restore
// writes the pre-image back — after snapshotting the current state, so a
// restore is itself undoable. Small pre-images live in the database; large
// ones spill to a file under the data directory with only the hash in the
// row.
package checkpoint

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// ErrNotFound reports a missing checkpoint row.
var ErrNotFound = errors.New("checkpoint not found")

// maxInlineBytes is the largest pre-image stored inline in the database.
// Larger files spill to the data directory.
const maxInlineBytes = 1 << 20 // 1 MiB

// maxSnapshotBytes caps what a snapshot will preserve at all. Mutations to
// files beyond this record hash+size only (restore unavailable) rather than
// filling the disk with copies.
const maxSnapshotBytes = 64 << 20 // 64 MiB

// Checkpoint is one recorded pre-image.
type Checkpoint struct {
	ID          int64  `json:"id"`
	RunID       string `json:"run_id,omitempty"`
	Tool        string `json:"tool"`
	Path        string `json:"path"`
	PreHash     string `json:"pre_hash,omitempty"`
	Absent      bool   `json:"absent"`
	CreatedAt   string `json:"created_at"`
	Restorable  bool   `json:"restorable"`
	SizeInline  int    `json:"-"`
	spilledPath string
}

// PathValidator re-checks that a path is inside the allowed roots before a
// restore writes to it. Wired to the file tools' boundary check so restore
// can never write outside what the file tools themselves could touch.
type PathValidator func(path string) error

// Store persists checkpoints.
type Store struct {
	db       *sql.DB
	spillDir string // "" disables spill; oversized files become hash-only
	validate PathValidator
}

// New creates a checkpoint store. spillDir (usually <dataDir>/checkpoints)
// holds pre-images too large for inline storage; validate guards restores.
func New(db *sql.DB, spillDir string, validate PathValidator) *Store {
	return &Store{db: db, spillDir: spillDir, validate: validate}
}

// Snapshot records the current state of path before a mutation by tool.
// Fail-closed contract: callers must abort the mutation if Snapshot errors —
// a rollback guarantee that silently skips is not a guarantee.
//
// The path is validated against the allowed roots here, before any read:
// this is the single authoritative check on the checkpoint side, so a
// boundary verdict can never be evaluated once for the snapshot and again —
// differently — for the mutation.
func (s *Store) Snapshot(ctx context.Context, tool, path string) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("checkpoint.snapshot: %w", err)
	}
	if s.validate != nil {
		if err := s.validate(abs); err != nil {
			return fmt.Errorf("checkpoint.snapshot: path not allowed: %w", err)
		}
	}

	info, statErr := os.Stat(abs)
	switch {
	case os.IsNotExist(statErr):
		// Pre-image is "absent" — restore will delete the created file.
		_, err = s.db.ExecContext(ctx,
			`INSERT INTO checkpoints (run_id, tool, path, absent) VALUES ('', ?, ?, 1)`,
			tool, abs)
		if err != nil {
			return fmt.Errorf("checkpoint.snapshot: %w", err)
		}
		return nil
	case statErr != nil:
		return fmt.Errorf("checkpoint.snapshot: stat: %w", statErr)
	case info.IsDir():
		return fmt.Errorf("checkpoint.snapshot: %s is a directory", abs)
	}

	if info.Size() > maxSnapshotBytes {
		// Too large to preserve: record the mutation happened (hash of size
		// marker) without content. Restore for this row is unavailable.
		_, err = s.db.ExecContext(ctx,
			`INSERT INTO checkpoints (run_id, tool, path, pre_hash, absent) VALUES ('', ?, ?, ?, 0)`,
			tool, abs, fmt.Sprintf("oversize:%d", info.Size()))
		if err != nil {
			return fmt.Errorf("checkpoint.snapshot: %w", err)
		}
		return nil
	}

	content, err := os.ReadFile(abs)
	if err != nil {
		return fmt.Errorf("checkpoint.snapshot: read: %w", err)
	}
	sum := sha256.Sum256(content)
	hash := hex.EncodeToString(sum[:])

	// Skip the insert when the newest checkpoint for this path already holds
	// the identical pre-image — retry loops would otherwise pile up copies.
	var lastHash string
	if err := s.db.QueryRowContext(ctx,
		`SELECT COALESCE(pre_hash, '') FROM checkpoints WHERE path = ? ORDER BY id DESC LIMIT 1`,
		abs).Scan(&lastHash); err == nil && lastHash == hash {
		return nil
	}

	if len(content) <= maxInlineBytes {
		_, err = s.db.ExecContext(ctx,
			`INSERT INTO checkpoints (run_id, tool, path, pre_hash, pre_content, absent) VALUES ('', ?, ?, ?, ?, 0)`,
			tool, abs, hash, content)
		if err != nil {
			return fmt.Errorf("checkpoint.snapshot: %w", err)
		}
		return nil
	}

	// Spill: content-addressed file in the data dir; the row carries the hash.
	if s.spillDir == "" {
		_, err = s.db.ExecContext(ctx,
			`INSERT INTO checkpoints (run_id, tool, path, pre_hash, absent) VALUES ('', ?, ?, ?, 0)`,
			tool, abs, hash)
		if err != nil {
			return fmt.Errorf("checkpoint.snapshot: %w", err)
		}
		return nil
	}
	if err := os.MkdirAll(s.spillDir, 0o700); err != nil {
		return fmt.Errorf("checkpoint.snapshot: spill dir: %w", err)
	}
	spill := filepath.Join(s.spillDir, hash)
	if _, err := os.Stat(spill); os.IsNotExist(err) {
		tmp := spill + ".tmp"
		if err := os.WriteFile(tmp, content, 0o600); err != nil {
			return fmt.Errorf("checkpoint.snapshot: spill write: %w", err)
		}
		if err := os.Rename(tmp, spill); err != nil {
			return fmt.Errorf("checkpoint.snapshot: spill rename: %w", err)
		}
	}
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO checkpoints (run_id, tool, path, pre_hash, absent) VALUES ('', ?, ?, ?, 0)`,
		tool, abs, hash)
	if err != nil {
		return fmt.Errorf("checkpoint.snapshot: %w", err)
	}
	return nil
}

// List returns recent checkpoints, newest first.
func (s *Store) List(ctx context.Context, limit int) ([]Checkpoint, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, run_id, tool, path, COALESCE(pre_hash, ''), absent,
		        created_at, pre_content IS NOT NULL, COALESCE(LENGTH(pre_content), 0)
		 FROM checkpoints ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("checkpoint.list: %w", err)
	}
	defer rows.Close()

	var out []Checkpoint
	for rows.Next() {
		var c Checkpoint
		var absent, hasInline int
		if err := rows.Scan(&c.ID, &c.RunID, &c.Tool, &c.Path, &c.PreHash,
			&absent, &c.CreatedAt, &hasInline, &c.SizeInline); err != nil {
			return nil, fmt.Errorf("checkpoint.list: scan: %w", err)
		}
		c.Absent = absent != 0
		c.Restorable = c.Absent || hasInline != 0 || s.spillExists(c.PreHash)
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *Store) spillExists(hash string) bool {
	if s.spillDir == "" || hash == "" || len(hash) != 64 {
		return false
	}
	_, err := os.Stat(filepath.Join(s.spillDir, hash))
	return err == nil
}

// Restore writes the pre-image recorded in checkpoint id back to its path.
// The current state is snapshotted first (tool "checkpoint.restore"), so a
// restore is itself undoable. The path is re-validated against the allowed
// roots at restore time — a checkpoint row can never authorize writing
// somewhere the file tools couldn't.
func (s *Store) Restore(ctx context.Context, id int64) (string, error) {
	var c Checkpoint
	var absent int
	var content []byte
	err := s.db.QueryRowContext(ctx,
		`SELECT id, tool, path, COALESCE(pre_hash, ''), absent, pre_content
		 FROM checkpoints WHERE id = ?`, id).
		Scan(&c.ID, &c.Tool, &c.Path, &c.PreHash, &absent, &content)
	if err == sql.ErrNoRows {
		return "", fmt.Errorf("checkpoint.restore: id %d: %w", id, ErrNotFound)
	}
	if err != nil {
		return "", fmt.Errorf("checkpoint.restore: %w", err)
	}
	c.Absent = absent != 0

	if s.validate != nil {
		if err := s.validate(c.Path); err != nil {
			return "", fmt.Errorf("checkpoint.restore: path no longer allowed: %w", err)
		}
	}

	// Resolve spilled content if not inline, verifying integrity against the
	// recorded hash — the spill dir lives on disk where other processes (or
	// the file tools themselves) could touch it.
	if !c.Absent && content == nil {
		if !s.spillExists(c.PreHash) {
			return "", fmt.Errorf("checkpoint.restore: pre-image for %s unavailable (oversize or purged)", c.Path)
		}
		content, err = os.ReadFile(filepath.Join(s.spillDir, c.PreHash))
		if err != nil {
			return "", fmt.Errorf("checkpoint.restore: read spill: %w", err)
		}
		sum := sha256.Sum256(content)
		if hex.EncodeToString(sum[:]) != c.PreHash {
			return "", fmt.Errorf("checkpoint.restore: spill for %s failed integrity check", c.Path)
		}
	}

	// Undo-of-undo: preserve the current state before overwriting it.
	if err := s.Snapshot(ctx, "checkpoint.restore", c.Path); err != nil {
		return "", fmt.Errorf("checkpoint.restore: pre-restore snapshot: %w", err)
	}

	if c.Absent {
		if err := os.Remove(c.Path); err != nil && !os.IsNotExist(err) {
			return "", fmt.Errorf("checkpoint.restore: remove: %w", err)
		}
		return fmt.Sprintf("Restored %s to its pre-mutation state (file removed — it did not exist before).", c.Path), nil
	}

	if err := os.MkdirAll(filepath.Dir(c.Path), 0o755); err != nil {
		return "", fmt.Errorf("checkpoint.restore: mkdir: %w", err)
	}
	if err := os.WriteFile(c.Path, content, 0o644); err != nil {
		return "", fmt.Errorf("checkpoint.restore: write: %w", err)
	}
	return fmt.Sprintf("Restored %s (%d bytes) to its state before %s.", c.Path, len(content), c.Tool), nil
}

// StartJanitor runs retention in the background: an immediate purge, then one
// per interval. The returned stop function is idempotent and blocks until the
// goroutine exits.
func (s *Store) StartJanitor(retention, interval time.Duration) func() {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		if _, err := s.PurgeOlderThan(ctx, retention); err != nil {
			fmt.Printf("checkpoint: retention purge failed: %v\n", err)
		}
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if _, err := s.PurgeOlderThan(ctx, retention); err != nil {
					fmt.Printf("checkpoint: retention purge failed: %v\n", err)
				}
			}
		}
	}()
	var once sync.Once
	return func() {
		once.Do(func() {
			cancel()
			<-done
		})
	}
}

// PurgeOlderThan removes checkpoint rows older than the retention window and
// deletes spill files no remaining row references. Returns rows removed.
func (s *Store) PurgeOlderThan(ctx context.Context, age time.Duration) (int64, error) {
	cutoff := time.Now().UTC().Add(-age).Format("2006-01-02 15:04:05")
	res, err := s.db.ExecContext(ctx,
		`DELETE FROM checkpoints WHERE created_at < ?`, cutoff)
	if err != nil {
		return 0, fmt.Errorf("checkpoint.purge: %w", err)
	}
	n, _ := res.RowsAffected()

	// Sweep orphaned spill files, including .tmp leftovers from interrupted
	// spill writes.
	if s.spillDir != "" {
		entries, err := os.ReadDir(s.spillDir)
		if err == nil {
			for _, e := range entries {
				name := e.Name()
				if e.IsDir() {
					continue
				}
				if strings.HasSuffix(name, ".tmp") {
					_ = os.Remove(filepath.Join(s.spillDir, name))
					continue
				}
				if len(name) != 64 {
					continue
				}
				var refs int
				if err := s.db.QueryRowContext(ctx,
					`SELECT COUNT(*) FROM checkpoints WHERE pre_hash = ?`, name).Scan(&refs); err == nil && refs == 0 {
					_ = os.Remove(filepath.Join(s.spillDir, name))
				}
			}
		}
	}
	return n, nil
}
