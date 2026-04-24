package testutil

import (
	"context"
	"testing"
	"time"

	"github.com/LumabyteCo/aibutler/internal/config"
	"github.com/LumabyteCo/aibutler/internal/db"
)

// TestDB creates an in-memory SQLite database with the full v1 schema.
// Automatically cleaned up when the test ends.
// No encryption (for speed). Test encryption separately in db package.
func TestDB(t *testing.T) *db.DB {
	t.Helper()
	database, err := db.Open(db.Config{Path: ":memory:"})
	if err != nil {
		t.Fatalf("TestDB: open: %v", err)
	}
	if err := database.ApplySchema(context.Background()); err != nil {
		t.Fatalf("TestDB: schema: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	return database
}

// TestDBSeeded creates a TestDB pre-loaded with sample data.
func TestDBSeeded(t *testing.T) *db.DB {
	t.Helper()
	database := TestDB(t)
	ctx := context.Background()
	conn := database.Conn()

	// Seed a session
	_, err := conn.ExecContext(ctx,
		`INSERT INTO sessions (id, channel, account_id) VALUES ('sess-1', 'terminal', 'user-1')`)
	if err != nil {
		t.Fatalf("TestDBSeeded: sessions: %v", err)
	}

	// Seed key facts
	_, err = conn.ExecContext(ctx,
		`INSERT INTO key_facts (fact, category, source_session) VALUES
		('User prefers dark mode', 'preference', 'sess-1'),
		('User name is Alex', 'identity', 'sess-1')`)
	if err != nil {
		t.Fatalf("TestDBSeeded: key_facts: %v", err)
	}

	// Seed tasks
	_, err = conn.ExecContext(ctx,
		`INSERT INTO user_tasks (list_name, content, status, priority) VALUES
		('default', 'Buy groceries', 'pending', 0),
		('work', 'Review PR #42', 'in_progress', 1)`)
	if err != nil {
		t.Fatalf("TestDBSeeded: user_tasks: %v", err)
	}

	// Seed contacts
	_, err = conn.ExecContext(ctx,
		`INSERT INTO user_contacts (name, email, relationship) VALUES
		('Sarah', 'sarah@example.com', 'friend'),
		('Bob', 'bob@work.com', 'coworker')`)
	if err != nil {
		t.Fatalf("TestDBSeeded: user_contacts: %v", err)
	}

	return database
}

// TestConfig returns a Config with test-friendly defaults.
func TestConfig() *config.Config {
	cfg := config.Default()
	// Use a temp-friendly skills dir.
	cfg.Configurations.Prompts.SkillsDir = ""
	// Short timeouts for tests.
	cfg.Options.Models.RequestTimeout = 5 * time.Second
	cfg.Options.Agents.SubagentTimeout = 5 * time.Second
	return cfg
}
