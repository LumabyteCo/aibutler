package audit_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/LumabyteCo/aibutler/internal/audit"
	"github.com/LumabyteCo/aibutler/internal/capability"
	"github.com/LumabyteCo/aibutler/testutil"
)

// seedAgent creates session + agent rows to satisfy FK constraints on resource_access_log.
func seedAgent(t *testing.T, db *sql.DB, agentID string) {
	t.Helper()
	ctx := context.Background()
	now := time.Now().UTC().Format(time.RFC3339)
	sessID := "sess-" + agentID

	// Create session (agents FK → sessions).
	if _, err := db.ExecContext(ctx,
		`INSERT INTO sessions (id, channel, account_id, scope, created_at, updated_at) VALUES (?, 'terminal', 'user-1', 'default', ?, ?)`,
		sessID, now, now); err != nil {
		t.Fatalf("seed session %s: %v", sessID, err)
	}

	// Create agent (resource_access_log FK → agents). parent_id must be NULL, not empty string.
	_, err := db.ExecContext(ctx,
		`INSERT INTO agents (id, session_id, parent_id, type, state, task, capabilities, model, created_at, updated_at)
		 VALUES (?, ?, NULL, 'primary', 'completed', 'test', '[]', 'test-model', ?, ?)`,
		agentID, sessID, now, now)
	if err != nil {
		t.Fatalf("seed agent %s: %v", agentID, err)
	}
}

func TestSQLiteAuditorLogAccess(t *testing.T) {
	database := testutil.TestDB(t)
	seedAgent(t, database.Conn(), "agent-1")
	auditor := audit.NewSQLiteAuditor(database.Conn())

	ctx := context.Background()
	entry := capability.AuditEntry{
		AgentID:        "agent-1",
		AgentType:      "primary",
		SessionID:      "sess-1",
		ResourceType:   "tool",
		Service:        "task.add",
		Action:         "task.add",
		Target:         "user_tasks",
		CapabilityUsed: "data.tasks.write",
		Status:         "success",
		TokensConsumed: 100,
		CostUSD:        0.001,
	}

	err := auditor.LogAccess(ctx, entry)
	if err != nil {
		t.Fatalf("LogAccess: %v", err)
	}

	var count int
	err = database.Conn().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM resource_access_log WHERE agent_id = ?`, "agent-1").Scan(&count)
	if err != nil {
		t.Fatalf("query count: %v", err)
	}
	if count != 1 {
		t.Errorf("count = %d, want 1", count)
	}
}

func TestSQLiteAuditorLogAccessWithTimestamp(t *testing.T) {
	database := testutil.TestDB(t)
	seedAgent(t, database.Conn(), "agent-2")
	auditor := audit.NewSQLiteAuditor(database.Conn())

	ctx := context.Background()
	ts := time.Date(2025, 1, 15, 12, 0, 0, 0, time.UTC)
	entry := capability.AuditEntry{
		Timestamp: ts,
		AgentID:   "agent-2",
		Action:    "web.fetch",
		Status:    "success",
	}

	err := auditor.LogAccess(ctx, entry)
	if err != nil {
		t.Fatalf("LogAccess: %v", err)
	}

	var storedTS string
	err = database.Conn().QueryRowContext(ctx,
		`SELECT timestamp FROM resource_access_log WHERE agent_id = ?`, "agent-2").Scan(&storedTS)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if storedTS != "2025-01-15T12:00:00Z" {
		t.Errorf("timestamp = %q, want 2025-01-15T12:00:00Z", storedTS)
	}
}

func TestSQLiteAuditorLogAccessZeroTimestamp(t *testing.T) {
	database := testutil.TestDB(t)
	seedAgent(t, database.Conn(), "agent-3")
	auditor := audit.NewSQLiteAuditor(database.Conn())

	ctx := context.Background()
	entry := capability.AuditEntry{
		AgentID: "agent-3",
		Action:  "memory.write",
		Status:  "success",
	}

	err := auditor.LogAccess(ctx, entry)
	if err != nil {
		t.Fatalf("LogAccess: %v", err)
	}

	var storedTS string
	err = database.Conn().QueryRowContext(ctx,
		`SELECT timestamp FROM resource_access_log WHERE agent_id = ?`, "agent-3").Scan(&storedTS)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if storedTS == "" {
		t.Error("timestamp should not be empty")
	}
	_, err = time.Parse(time.RFC3339, storedTS)
	if err != nil {
		t.Errorf("invalid timestamp format: %v", err)
	}
}

func TestSQLiteAuditorLogAccessError(t *testing.T) {
	database := testutil.TestDB(t)
	seedAgent(t, database.Conn(), "agent-4")
	auditor := audit.NewSQLiteAuditor(database.Conn())

	ctx := context.Background()
	entry := capability.AuditEntry{
		AgentID: "agent-4",
		Action:  "tool.shell.exec",
		Status:  "error",
		Error:   "permission denied",
	}

	err := auditor.LogAccess(ctx, entry)
	if err != nil {
		t.Fatalf("LogAccess: %v", err)
	}

	var errorMsg string
	err = database.Conn().QueryRowContext(ctx,
		`SELECT error FROM resource_access_log WHERE agent_id = ?`, "agent-4").Scan(&errorMsg)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if errorMsg != "permission denied" {
		t.Errorf("error = %q, want 'permission denied'", errorMsg)
	}
}

func TestSQLiteAuditorMultipleEntries(t *testing.T) {
	database := testutil.TestDB(t)
	seedAgent(t, database.Conn(), "agent-multi")
	auditor := audit.NewSQLiteAuditor(database.Conn())

	ctx := context.Background()
	for i := 0; i < 5; i++ {
		err := auditor.LogAccess(ctx, capability.AuditEntry{
			AgentID: "agent-multi",
			Action:  "test.action",
			Status:  "success",
		})
		if err != nil {
			t.Fatalf("LogAccess %d: %v", i, err)
		}
	}

	var count int
	err := database.Conn().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM resource_access_log WHERE agent_id = ?`, "agent-multi").Scan(&count)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if count != 5 {
		t.Errorf("count = %d, want 5", count)
	}
}
