package compliance_test

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/LumabyteCo/aibutler/internal/compliance"
	"github.com/LumabyteCo/aibutler/testutil"
)

func TestLogEntry(t *testing.T) {
	db := testutil.TestDB(t)
	logger := compliance.New(db.Conn())
	ctx := context.Background()

	err := logger.Log(ctx, "alice", "login", "auth", `{"method":"password"}`, "192.168.1.1", "success")
	if err != nil {
		t.Fatalf("log: %v", err)
	}

	entries, err := logger.Query(ctx, compliance.AuditFilter{UserID: "alice"})
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("got %d entries, want 1", len(entries))
	}
	if entries[0].Action != "login" {
		t.Errorf("action = %q, want %q", entries[0].Action, "login")
	}
	if entries[0].Outcome != "success" {
		t.Errorf("outcome = %q, want %q", entries[0].Outcome, "success")
	}
}

func TestQueryWithFilter(t *testing.T) {
	db := testutil.TestDB(t)
	logger := compliance.New(db.Conn())
	ctx := context.Background()

	_ = logger.Log(ctx, "alice", "login", "auth", "", "10.0.0.1", "success")
	_ = logger.Log(ctx, "bob", "login", "auth", "", "10.0.0.2", "success")
	_ = logger.Log(ctx, "alice", "tool.execute", "tools", "", "10.0.0.1", "success")
	_ = logger.Log(ctx, "alice", "config.change", "config", "", "10.0.0.1", "denied")

	// Filter by user.
	entries, err := logger.Query(ctx, compliance.AuditFilter{UserID: "alice"})
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(entries) != 3 {
		t.Errorf("alice entries = %d, want 3", len(entries))
	}

	// Filter by action.
	entries, err = logger.Query(ctx, compliance.AuditFilter{Action: "login"})
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(entries) != 2 {
		t.Errorf("login entries = %d, want 2", len(entries))
	}

	// Filter with limit.
	entries, err = logger.Query(ctx, compliance.AuditFilter{UserID: "alice", Limit: 1})
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(entries) != 1 {
		t.Errorf("limited entries = %d, want 1", len(entries))
	}
}

func TestExportJSON(t *testing.T) {
	db := testutil.TestDB(t)
	logger := compliance.New(db.Conn())
	ctx := context.Background()

	_ = logger.Log(ctx, "alice", "login", "auth", `{"ip":"1.2.3.4"}`, "1.2.3.4", "success")

	var buf bytes.Buffer
	if err := logger.Export(ctx, "json", &buf); err != nil {
		t.Fatalf("export json: %v", err)
	}

	var entries []compliance.AuditEntry
	if err := json.Unmarshal(buf.Bytes(), &entries); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(entries) != 1 {
		t.Errorf("exported %d entries, want 1", len(entries))
	}
}

func TestDeleteUserDataGDPR(t *testing.T) {
	db := testutil.TestDB(t)
	logger := compliance.New(db.Conn())
	ctx := context.Background()

	_ = logger.Log(ctx, "alice", "login", "auth", "", "10.0.0.1", "success")
	_ = logger.Log(ctx, "alice", "tool.execute", "tools", "", "10.0.0.1", "success")
	_ = logger.Log(ctx, "bob", "login", "auth", "", "10.0.0.2", "success")

	if err := logger.DeleteUserData(ctx, "alice"); err != nil {
		t.Fatalf("delete user data: %v", err)
	}

	entries, err := logger.Query(ctx, compliance.AuditFilter{UserID: "alice"})
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("alice entries = %d after GDPR delete, want 0", len(entries))
	}

	// Bob's data should be untouched.
	entries, err = logger.Query(ctx, compliance.AuditFilter{UserID: "bob"})
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(entries) != 1 {
		t.Errorf("bob entries = %d, want 1", len(entries))
	}
}

func TestRedactPII(t *testing.T) {
	logger := compliance.New(nil) // redaction doesn't need DB

	tests := []struct {
		input    string
		contains string // substring that should be present after redaction
		excludes string // substring that should NOT be present
	}{
		{
			input:    "Contact user@example.com for details",
			contains: "u***@e***",
			excludes: "user@example.com",
		},
		{
			input:    "Call +1234567890 now",
			contains: "+1***890",
			excludes: "1234567890",
		},
		{
			input:    "Key is sk-ant-abc123xyz",
			contains: "sk-ant-***",
			excludes: "abc123xyz",
		},
		{
			input:    "Auth: Bearer eyJhbGciOiJSUzI1NiJ9.payload.sig",
			contains: "Bearer [REDACTED]",
			excludes: "eyJhbGci",
		},
	}

	for _, tc := range tests {
		result := logger.RedactPII(tc.input)
		if !strings.Contains(result, tc.contains) {
			t.Errorf("RedactPII(%q) = %q, want to contain %q", tc.input, result, tc.contains)
		}
		if tc.excludes != "" && strings.Contains(result, tc.excludes) {
			t.Errorf("RedactPII(%q) = %q, should not contain %q", tc.input, result, tc.excludes)
		}
	}
}

func TestRetentionPurge(t *testing.T) {
	db := testutil.TestDB(t)
	logger := compliance.New(db.Conn())
	ctx := context.Background()

	// Insert entries with old timestamps directly.
	conn := db.Conn()
	_, err := conn.ExecContext(ctx,
		`INSERT INTO compliance_audit (timestamp, user_id, action, resource, outcome)
		 VALUES (datetime('now', '-100 days'), 'old-user', 'login', 'auth', 'success')`)
	if err != nil {
		t.Fatalf("insert old entry: %v", err)
	}
	_, err = conn.ExecContext(ctx,
		`INSERT INTO compliance_audit (timestamp, user_id, action, resource, outcome)
		 VALUES (datetime('now', '-1 day'), 'recent-user', 'login', 'auth', 'success')`)
	if err != nil {
		t.Fatalf("insert recent entry: %v", err)
	}

	purged, err := logger.RetentionPurge(ctx, 90*24*time.Hour) // purge > 90 days
	if err != nil {
		t.Fatalf("retention purge: %v", err)
	}
	if purged != 1 {
		t.Errorf("purged = %d, want 1", purged)
	}

	entries, err := logger.Query(ctx, compliance.AuditFilter{})
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(entries) != 1 {
		t.Errorf("remaining entries = %d, want 1", len(entries))
	}
}
