package plugin_test

import (
	"context"
	"testing"

	"github.com/LumabyteCo/aibutler/internal/plugin"
	"github.com/LumabyteCo/aibutler/testutil"
)

func TestAuditWriteSuccess(t *testing.T) {
	database := testutil.TestDB(t)
	ctx := context.Background()
	conn := database.Conn()

	// Insert a plugin for the FK.
	_, err := conn.ExecContext(ctx,
		`INSERT INTO plugins (name, version, manifest_hash, wasm_hash) VALUES ('test-plugin', '1.0', 'h', 'w')`)
	if err != nil {
		t.Fatalf("insert plugin: %v", err)
	}

	w := plugin.NewSQLiteAuditWriter(conn)
	if err := w.WriteAudit(ctx, 1, "tool_call", "tool.call", "success"); err != nil {
		t.Fatalf("write audit: %v", err)
	}

	var action, status string
	err = conn.QueryRowContext(ctx,
		"SELECT action, status FROM plugin_audit WHERE plugin_id = 1").Scan(&action, &status)
	if err != nil {
		t.Fatalf("query audit: %v", err)
	}
	if action != "tool_call" || status != "success" {
		t.Errorf("got (%q, %q), want (tool_call, success)", action, status)
	}
}

func TestAuditWriteWithError(t *testing.T) {
	database := testutil.TestDB(t)
	ctx := context.Background()
	conn := database.Conn()

	_, _ = conn.ExecContext(ctx,
		`INSERT INTO plugins (name, version, manifest_hash, wasm_hash) VALUES ('test-plugin', '1.0', 'h', 'w')`)

	w := plugin.NewSQLiteAuditWriter(conn)
	if err := w.WriteAuditWithError(ctx, 1, "credential_get", "credential.read:key", "denied", "no capability"); err != nil {
		t.Fatalf("write: %v", err)
	}

	var errMsg string
	err := conn.QueryRowContext(ctx,
		"SELECT error_message FROM plugin_audit WHERE status = 'denied'").Scan(&errMsg)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if errMsg != "no capability" {
		t.Errorf("error_message = %q", errMsg)
	}
}

func TestAuditQueryByPlugin(t *testing.T) {
	database := testutil.TestDB(t)
	ctx := context.Background()
	conn := database.Conn()

	_, _ = conn.ExecContext(ctx,
		`INSERT INTO plugins (name, version, manifest_hash, wasm_hash) VALUES ('p1', '1.0', 'h1', 'w1')`)
	_, _ = conn.ExecContext(ctx,
		`INSERT INTO plugins (name, version, manifest_hash, wasm_hash) VALUES ('p2', '1.0', 'h2', 'w2')`)

	w := plugin.NewSQLiteAuditWriter(conn)
	_ = w.WriteAudit(ctx, 1, "tool_call", "", "success")
	_ = w.WriteAudit(ctx, 1, "log", "", "success")
	_ = w.WriteAudit(ctx, 2, "config_get", "", "success")

	var count int
	err := conn.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM plugin_audit WHERE plugin_id = 1").Scan(&count)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if count != 2 {
		t.Errorf("plugin 1 audit count = %d, want 2", count)
	}
}

func TestAuditQueryByTimestamp(t *testing.T) {
	database := testutil.TestDB(t)
	ctx := context.Background()
	conn := database.Conn()

	_, _ = conn.ExecContext(ctx,
		`INSERT INTO plugins (name, version, manifest_hash, wasm_hash) VALUES ('p', '1.0', 'h', 'w')`)

	w := plugin.NewSQLiteAuditWriter(conn)
	_ = w.WriteAudit(ctx, 1, "test", "", "success")

	var count int
	err := conn.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM plugin_audit WHERE timestamp >= datetime('now', '-1 minute')").Scan(&count)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if count != 1 {
		t.Errorf("recent audit count = %d, want 1", count)
	}
}
