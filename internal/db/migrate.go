package db

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// Migration represents a single schema migration step.
type Migration struct {
	Version int
	Up      string
	Down    string
}

// SchemaVersion returns the current schema version via PRAGMA user_version.
func (d *DB) SchemaVersion(ctx context.Context) (int, error) {
	var version int
	err := d.conn.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version)
	if err != nil {
		return 0, fmt.Errorf("db: schema version: %w", err)
	}
	return version, nil
}

// ApplySchema runs all pending migrations from the current version to latest.
func (d *DB) ApplySchema(ctx context.Context) error {
	migrations, err := loadMigrations()
	if err != nil {
		return fmt.Errorf("db: load migrations: %w", err)
	}

	current, err := d.SchemaVersion(ctx)
	if err != nil {
		return err
	}

	for _, m := range migrations {
		if m.Version <= current {
			continue
		}
		if err := d.applyMigration(ctx, m, "up"); err != nil {
			return fmt.Errorf("db: migration %d up: %w", m.Version, err)
		}
	}
	return nil
}

// MigrateDown reverts the most recent migration.
func (d *DB) MigrateDown(ctx context.Context) error {
	migrations, err := loadMigrations()
	if err != nil {
		return fmt.Errorf("db: load migrations: %w", err)
	}

	current, err := d.SchemaVersion(ctx)
	if err != nil {
		return err
	}
	if current == 0 {
		return nil // nothing to revert
	}

	for _, m := range migrations {
		if m.Version == current {
			if err := d.applyMigration(ctx, m, "down"); err != nil {
				return fmt.Errorf("db: migration %d down: %w", m.Version, err)
			}
			return nil
		}
	}
	return fmt.Errorf("db: migration %d not found", current)
}

func (d *DB) applyMigration(ctx context.Context, m Migration, direction string) error {
	sqlContent := m.Up
	if direction == "down" {
		sqlContent = m.Down
	}

	tx, err := d.conn.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, sqlContent); err != nil {
		return fmt.Errorf("exec sql: %w", err)
	}

	newVersion := m.Version
	if direction == "down" {
		newVersion = m.Version - 1
	}
	if _, err := tx.ExecContext(ctx, fmt.Sprintf("PRAGMA user_version=%d", newVersion)); err != nil {
		return fmt.Errorf("set user_version: %w", err)
	}

	// Record in schema_migrations (only if the table exists — it's created by migration 1).
	if m.Version == 1 && direction == "up" {
		// The table was just created in this migration, so we can insert.
		if _, err := tx.ExecContext(ctx,
			"INSERT INTO schema_migrations (version, direction) VALUES (?, ?)",
			m.Version, direction); err != nil {
			return fmt.Errorf("record migration: %w", err)
		}
	} else if m.Version > 1 || direction == "down" {
		_, _ = tx.ExecContext(ctx,
			"INSERT INTO schema_migrations (version, direction) VALUES (?, ?)",
			m.Version, direction)
	}

	return tx.Commit()
}

func loadMigrations() ([]Migration, error) {
	entries, err := migrationFS.ReadDir("migrations")
	if err != nil {
		return nil, fmt.Errorf("read migrations dir: %w", err)
	}

	migrationMap := make(map[int]*Migration)

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		parts := strings.SplitN(name, "_", 2)
		if len(parts) < 2 {
			continue
		}

		version, err := strconv.Atoi(parts[0])
		if err != nil {
			continue
		}

		content, err := migrationFS.ReadFile("migrations/" + name)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", name, err)
		}

		m, ok := migrationMap[version]
		if !ok {
			m = &Migration{Version: version}
			migrationMap[version] = m
		}

		if strings.HasSuffix(name, ".up.sql") {
			m.Up = string(content)
		} else if strings.HasSuffix(name, ".down.sql") {
			m.Down = string(content)
		}
	}

	var migrations []Migration
	for _, m := range migrationMap {
		migrations = append(migrations, *m)
	}
	sort.Slice(migrations, func(i, j int) bool {
		return migrations[i].Version < migrations[j].Version
	})

	return migrations, nil
}
