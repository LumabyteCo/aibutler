package incremental

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/ncruces/go-sqlite3"
)

// SnapshotInfo describes a database snapshot.
type SnapshotInfo struct {
	Path      string
	Size      int64
	CreatedAt time.Time
}

// Manager handles incremental database backups using SQLite's Online Backup API.
type Manager struct {
	dbPath    string
	backupDir string
	maxSnaps  int
}

// New creates an incremental backup manager.
// dbPath is the path to the live database. backupDir is where snapshots are stored.
func New(dbPath string, backupDir string) *Manager {
	return &Manager{
		dbPath:    dbPath,
		backupDir: backupDir,
		maxSnaps:  10,
	}
}

// Snapshot creates a timestamped backup of the database, returning the backup path.
func (m *Manager) Snapshot(_ context.Context) (string, error) {
	if err := os.MkdirAll(m.backupDir, 0700); err != nil {
		return "", fmt.Errorf("incremental: mkdir: %w", err)
	}

	ts := fmt.Sprintf("%d", time.Now().UnixNano())
	dstPath := filepath.Join(m.backupDir, fmt.Sprintf("snapshot_%s.db", ts))

	srcConn, err := sqlite3.Open(m.dbPath)
	if err != nil {
		return "", fmt.Errorf("incremental: open source: %w", err)
	}
	defer srcConn.Close()

	// Remove destination if it exists.
	os.Remove(dstPath)

	backup, err := srcConn.BackupInit("main", dstPath)
	if err != nil {
		return "", fmt.Errorf("incremental: backup init: %w", err)
	}
	defer backup.Close()

	done, err := backup.Step(-1)
	if err != nil {
		return "", fmt.Errorf("incremental: backup step: %w", err)
	}
	if !done {
		return "", fmt.Errorf("incremental: backup step did not complete")
	}

	return dstPath, nil
}

// List returns all snapshots in the backup directory, sorted newest first.
func (m *Manager) List(_ context.Context) ([]SnapshotInfo, error) {
	entries, err := os.ReadDir(m.backupDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("incremental: list: %w", err)
	}

	var snaps []SnapshotInfo
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		snaps = append(snaps, SnapshotInfo{
			Path:      filepath.Join(m.backupDir, e.Name()),
			Size:      info.Size(),
			CreatedAt: info.ModTime(),
		})
	}

	// Sort newest first.
	sort.Slice(snaps, func(i, j int) bool {
		return snaps[i].CreatedAt.After(snaps[j].CreatedAt)
	})

	return snaps, nil
}

// Prune removes old snapshots, keeping the most recent keepN. Returns count removed.
func (m *Manager) Prune(_ context.Context, keepN int) (int, error) {
	snaps, err := m.List(context.Background())
	if err != nil {
		return 0, err
	}

	if len(snaps) <= keepN {
		return 0, nil
	}

	removed := 0
	for _, snap := range snaps[keepN:] {
		if err := os.Remove(snap.Path); err != nil {
			continue
		}
		removed++
	}
	return removed, nil
}

// Restore copies a snapshot back over the live database.
// The caller is responsible for closing any open database connections first.
func (m *Manager) Restore(_ context.Context, path string) error {
	if _, err := os.Stat(path); err != nil {
		return fmt.Errorf("incremental: snapshot not found: %w", err)
	}

	srcConn, err := sqlite3.Open(path)
	if err != nil {
		return fmt.Errorf("incremental: open snapshot: %w", err)
	}
	defer srcConn.Close()

	// Remove the current database to replace it.
	os.Remove(m.dbPath)

	backup, err := srcConn.BackupInit("main", m.dbPath)
	if err != nil {
		return fmt.Errorf("incremental: restore init: %w", err)
	}
	defer backup.Close()

	done, err := backup.Step(-1)
	if err != nil {
		return fmt.Errorf("incremental: restore step: %w", err)
	}
	if !done {
		return fmt.Errorf("incremental: restore step did not complete")
	}

	return nil
}
