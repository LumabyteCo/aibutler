package db_test

import (
	"context"
	"testing"

	"github.com/LumabyteCo/aibutler/testutil"
)

// expectedTables lists all tables from all migrations.
var expectedTables = []string{
	// Migration 001: core schema
	"sessions",
	"key_facts",
	"captured_thoughts",
	"user_tasks",
	"user_reminders",
	"user_contacts",
	"user_expenses",
	"user_budgets",
	"user_health",
	"user_habits",
	"user_habit_logs",
	"user_subscriptions",
	"user_recipes",
	"user_journal",
	"user_places",
	"user_media",
	"user_maintenance",
	"user_meals",
	"task_contexts",
	"token_usage",
	"agents",
	"resource_access_log",
	// Migration 002
	"messages",
	// Migration 006: Living Memory P2
	"entities",
	"entity_relationships",
	"session_transcripts",
	// Migration 007: Agent Orchestration P2
	"agent_delegations",
	"background_agents",
	"custom_agent_roles",
	// Migration 008: Plugin System
	"plugins",
	"plugin_kv",
	"plugin_audit",
	// Migration 009: Protocol & Memory Exposure
	"memory_imports",
	"memory_digests",
	"a2a_delegations",
	// Migration 011: Channels P2
	"whatsapp_messages",
	"browser_history",
	// Migration 012: Web Dashboard
	"agent_card_config",
	"dashboard_sessions",
	// Migration 013: Reliability
	"cron_executions",
	"rate_limit_log",
	// Migration 014: Streaming & Compaction
	"session_compactions",
	// Migration 015: Transactions & AI Services
	"transactions",
	"transaction_audit",
	// Migration 016: Response Cache
	"response_cache",
	// Migration 017: Extended Channels (Teams, Webhook)
	"teams_messages",
	"webhook_messages",
	// Migration 018: Enterprise RBAC / SSO / Compliance
	"rbac_users",
	"rbac_permissions",
	"oidc_sessions",
	"compliance_audit",
	// Migration 019: Advanced Security (WebAuthn, Security Events)
	"webauthn_credentials",
	"security_events",
	// Migration 020: Action recording
	"actions",
	// Tables from core migrations not previously listed
	"oauth_tokens",
	"agent_registry",
	"swarm_runs",
	"swarm_workspaces",
	"swarm_trace",
	"learned_instructions",
	"memory_vectors",
	"schedules",
	"schedule_runs",
	"channel_messages",
	"finance_watchlist",
	"iot_devices",
	"mcp_servers",
	"schema_migrations",
}

func TestSchemaCreatesAllTables(t *testing.T) {
	database := testutil.TestDB(t)
	ctx := context.Background()
	conn := database.Conn()

	for _, table := range expectedTables {
		var name string
		err := conn.QueryRowContext(ctx,
			"SELECT name FROM sqlite_master WHERE type='table' AND name=?", table).Scan(&name)
		if err != nil {
			t.Errorf("table %q not found: %v", table, err)
		}
	}
}

func TestSchemaVersion(t *testing.T) {
	database := testutil.TestDB(t)
	ctx := context.Background()

	version, err := database.SchemaVersion(ctx)
	if err != nil {
		t.Fatalf("schema version: %v", err)
	}
	if version != 20 {
		t.Errorf("schema version = %d, want 20", version)
	}
}

func TestSchemaMigrationsTablePopulated(t *testing.T) {
	database := testutil.TestDB(t)
	ctx := context.Background()
	conn := database.Conn()

	var count int
	err := conn.QueryRowContext(ctx, "SELECT COUNT(*) FROM schema_migrations").Scan(&count)
	if err != nil {
		t.Fatalf("schema_migrations query: %v", err)
	}
	if count < 1 {
		t.Error("expected at least 1 row in schema_migrations")
	}
}

func TestSchemaIdempotent(t *testing.T) {
	database := testutil.TestDB(t)
	ctx := context.Background()

	// Applying schema again should not error (IF NOT EXISTS).
	if err := database.ApplySchema(ctx); err != nil {
		t.Fatalf("second ApplySchema: %v", err)
	}
}

func TestSeededDBHasData(t *testing.T) {
	database := testutil.TestDBSeeded(t)
	ctx := context.Background()
	conn := database.Conn()

	var count int
	err := conn.QueryRowContext(ctx, "SELECT COUNT(*) FROM key_facts").Scan(&count)
	if err != nil {
		t.Fatalf("key_facts count: %v", err)
	}
	if count != 2 {
		t.Errorf("key_facts count = %d, want 2", count)
	}

	err = conn.QueryRowContext(ctx, "SELECT COUNT(*) FROM user_contacts").Scan(&count)
	if err != nil {
		t.Fatalf("user_contacts count: %v", err)
	}
	if count != 2 {
		t.Errorf("user_contacts count = %d, want 2", count)
	}
}

func TestUserDataCRUD(t *testing.T) {
	database := testutil.TestDB(t)
	ctx := context.Background()
	conn := database.Conn()

	// Insert a task
	_, err := conn.ExecContext(ctx,
		`INSERT INTO user_tasks (list_name, content, status) VALUES ('shopping', 'Buy milk', 'pending')`)
	if err != nil {
		t.Fatalf("insert task: %v", err)
	}

	// Read it back
	var content, status string
	err = conn.QueryRowContext(ctx,
		"SELECT content, status FROM user_tasks WHERE list_name='shopping'").Scan(&content, &status)
	if err != nil {
		t.Fatalf("read task: %v", err)
	}
	if content != "Buy milk" || status != "pending" {
		t.Errorf("got (%q, %q), want ('Buy milk', 'pending')", content, status)
	}

	// Update
	_, err = conn.ExecContext(ctx,
		`UPDATE user_tasks SET status='completed', completed_at=datetime('now') WHERE content='Buy milk'`)
	if err != nil {
		t.Fatalf("update task: %v", err)
	}

	// Verify
	err = conn.QueryRowContext(ctx,
		"SELECT status FROM user_tasks WHERE content='Buy milk'").Scan(&status)
	if err != nil {
		t.Fatalf("verify update: %v", err)
	}
	if status != "completed" {
		t.Errorf("status = %q, want 'completed'", status)
	}
}

func TestForeignKeys(t *testing.T) {
	database := testutil.TestDB(t)
	ctx := context.Background()
	conn := database.Conn()

	// Insert habit for FK test
	_, err := conn.ExecContext(ctx,
		`INSERT INTO user_habits (name, frequency) VALUES ('exercise', 'daily')`)
	if err != nil {
		t.Fatalf("insert habit: %v", err)
	}

	// Valid FK reference should succeed
	_, err = conn.ExecContext(ctx,
		`INSERT INTO user_habit_logs (habit_id, date) VALUES (1, '2026-03-06')`)
	if err != nil {
		t.Fatalf("valid FK insert: %v", err)
	}

	// Invalid FK should fail
	_, err = conn.ExecContext(ctx,
		`INSERT INTO user_habit_logs (habit_id, date) VALUES (999, '2026-03-06')`)
	if err == nil {
		t.Error("expected FK violation error, got nil")
	}
}

func TestUniqueConstraints(t *testing.T) {
	database := testutil.TestDB(t)
	ctx := context.Background()
	conn := database.Conn()

	// Insert a budget
	_, err := conn.ExecContext(ctx,
		`INSERT INTO user_budgets (category, amount, period) VALUES ('food', 500, 'monthly')`)
	if err != nil {
		t.Fatalf("insert budget: %v", err)
	}

	// Duplicate should fail (UNIQUE(category, period))
	_, err = conn.ExecContext(ctx,
		`INSERT INTO user_budgets (category, amount, period) VALUES ('food', 600, 'monthly')`)
	if err == nil {
		t.Error("expected unique violation, got nil")
	}
}

func TestArabicTextRoundTrip(t *testing.T) {
	database := testutil.TestDB(t)
	ctx := context.Background()
	conn := database.Conn()

	arabic := "مرحبا، كيف حالك اليوم؟"

	_, err := conn.ExecContext(ctx,
		`INSERT INTO key_facts (fact, category) VALUES (?, 'preference')`, arabic)
	if err != nil {
		t.Fatalf("insert arabic: %v", err)
	}

	var got string
	err = conn.QueryRowContext(ctx,
		"SELECT fact FROM key_facts WHERE category='preference'").Scan(&got)
	if err != nil {
		t.Fatalf("read arabic: %v", err)
	}
	if got != arabic {
		t.Errorf("arabic round-trip failed: got %q, want %q", got, arabic)
	}
}
