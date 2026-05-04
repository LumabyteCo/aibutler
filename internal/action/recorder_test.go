package action

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"testing"
	"time"

	_ "github.com/ncruces/go-sqlite3/driver"
)

// newMemDB creates an in-memory SQLite DB with just the actions table.
func newMemDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	_, err = db.Exec(`
CREATE TABLE actions (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    timestamp       DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    agent_id        TEXT,
    session_id      TEXT,
    action_type     TEXT NOT NULL,
    target          TEXT,
    payload_summary TEXT,
    payload_full    TEXT,
    duration_ms     INTEGER,
    status          TEXT NOT NULL,
    result_summary  TEXT,
    error           TEXT
);`)
	if err != nil {
		t.Fatal(err)
	}
	return db
}

func TestSQLiteRecorder_RecordAndQuery(t *testing.T) {
	db := newMemDB(t)
	r := NewSQLiteRecorder(db)

	a := Action{
		AgentID:        "agent-1",
		SessionID:      "s1",
		Type:           "applescript.exec",
		Target:         "Mail",
		PayloadSummary: "tell:Mail",
		PayloadFull:    `{"script":"tell application \"Mail\" to get count"}`,
		DurationMS:     42,
		Status:         "success",
		ResultSummary:  "12",
	}
	if err := r.Record(context.Background(), a); err != nil {
		t.Fatalf("Record: %v", err)
	}

	got, err := r.Query(context.Background(), QueryFilter{})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 row, got %d", len(got))
	}
	if got[0].AgentID != "agent-1" || got[0].Type != "applescript.exec" || got[0].Target != "Mail" {
		t.Errorf("row mismatch: %+v", got[0])
	}
	if got[0].DurationMS != 42 {
		t.Errorf("DurationMS = %d, want 42", got[0].DurationMS)
	}
}

func TestSQLiteRecorder_FillsTimestamp(t *testing.T) {
	db := newMemDB(t)
	r := NewSQLiteRecorder(db)

	a := Action{Type: "x", Status: "success"} // no Timestamp
	if err := r.Record(context.Background(), a); err != nil {
		t.Fatalf("Record: %v", err)
	}
	got, _ := r.Query(context.Background(), QueryFilter{})
	if got[0].Timestamp.IsZero() {
		t.Error("Timestamp should be auto-filled")
	}
}

func TestSQLiteRecorder_PayloadCapped(t *testing.T) {
	db := newMemDB(t)
	r := NewSQLiteRecorder(db)

	huge := strings.Repeat("x", maxPayloadFullBytes*2)
	a := Action{Type: "x", PayloadFull: huge, Status: "success"}
	_ = r.Record(context.Background(), a)

	got, _ := r.Query(context.Background(), QueryFilter{})
	if !strings.Contains(got[0].PayloadFull, "[truncated]") {
		t.Errorf("expected truncation marker in oversized payload, got %d-byte payload",
			len(got[0].PayloadFull))
	}
	if len(got[0].PayloadFull) > maxPayloadFullBytes+50 {
		t.Errorf("capped payload should be near %d bytes, got %d", maxPayloadFullBytes, len(got[0].PayloadFull))
	}
}

func TestSQLiteRecorder_PayloadRedacted(t *testing.T) {
	db := newMemDB(t)
	r := NewSQLiteRecorder(db)

	a := Action{
		Type:        "x",
		PayloadFull: `{"k":"sk-proj1234567890abcdefghij","other":"safe"}`,
		Status:      "success",
	}
	_ = r.Record(context.Background(), a)

	got, _ := r.Query(context.Background(), QueryFilter{})
	if strings.Contains(got[0].PayloadFull, "sk-proj1234567890abcdefghij") {
		t.Errorf("expected sensitive token to be redacted in payload, got: %s", got[0].PayloadFull)
	}
}

func TestSQLiteRecorder_QueryFilters(t *testing.T) {
	db := newMemDB(t)
	r := NewSQLiteRecorder(db)
	ctx := context.Background()

	// Record three actions with distinguishing fields.
	_ = r.Record(ctx, Action{AgentID: "a1", Type: "applescript.exec", Status: "success"})
	_ = r.Record(ctx, Action{AgentID: "a2", Type: "dbus.call", Status: "success"})
	_ = r.Record(ctx, Action{AgentID: "a1", Type: "applescript.exec", Status: "error", Error: "boom"})

	t.Run("filter by agent_id", func(t *testing.T) {
		got, _ := r.Query(ctx, QueryFilter{AgentID: "a1"})
		if len(got) != 2 {
			t.Errorf("expected 2 rows for a1, got %d", len(got))
		}
	})
	t.Run("filter by type", func(t *testing.T) {
		got, _ := r.Query(ctx, QueryFilter{Type: "dbus.call"})
		if len(got) != 1 {
			t.Errorf("expected 1 row for dbus.call, got %d", len(got))
		}
	})
	t.Run("filter by status", func(t *testing.T) {
		got, _ := r.Query(ctx, QueryFilter{Status: "error"})
		if len(got) != 1 || got[0].Error != "boom" {
			t.Errorf("expected error row with 'boom', got %+v", got)
		}
	})
}

func TestSQLiteRecorder_OrderNewestFirst(t *testing.T) {
	db := newMemDB(t)
	r := NewSQLiteRecorder(db)
	ctx := context.Background()

	t1 := time.Now().Add(-2 * time.Hour)
	t2 := time.Now().Add(-1 * time.Hour)
	t3 := time.Now()

	_ = r.Record(ctx, Action{Timestamp: t1, Type: "x", Status: "success", Target: "old"})
	_ = r.Record(ctx, Action{Timestamp: t2, Type: "x", Status: "success", Target: "mid"})
	_ = r.Record(ctx, Action{Timestamp: t3, Type: "x", Status: "success", Target: "new"})

	got, _ := r.Query(ctx, QueryFilter{})
	if len(got) != 3 {
		t.Fatalf("expected 3 rows, got %d", len(got))
	}
	if got[0].Target != "new" || got[2].Target != "old" {
		t.Errorf("expected newest-first ordering; got: %v / %v / %v", got[0].Target, got[1].Target, got[2].Target)
	}
}

func TestSQLiteRecorder_LimitClamping(t *testing.T) {
	db := newMemDB(t)
	r := NewSQLiteRecorder(db)
	ctx := context.Background()
	for i := 0; i < 10; i++ {
		_ = r.Record(ctx, Action{Type: "x", Status: "success"})
	}

	t.Run("default 50", func(t *testing.T) {
		got, _ := r.Query(ctx, QueryFilter{})
		if len(got) != 10 {
			t.Errorf("expected all 10 rows back (limit defaulted to 50), got %d", len(got))
		}
	})
	t.Run("explicit limit 3", func(t *testing.T) {
		got, _ := r.Query(ctx, QueryFilter{Limit: 3})
		if len(got) != 3 {
			t.Errorf("expected 3 rows, got %d", len(got))
		}
	})
	t.Run("over-cap 9999 clamped to 500", func(t *testing.T) {
		got, _ := r.Query(ctx, QueryFilter{Limit: 9999})
		if len(got) != 10 {
			t.Errorf("expected 10 rows back, got %d", len(got))
		}
	})
}

func TestNilRecorder_Safe(t *testing.T) {
	var r *SQLiteRecorder
	if err := r.Record(context.Background(), Action{Type: "x"}); err != nil {
		t.Errorf("nil recorder Record should be safe, got: %v", err)
	}
	if got, err := r.Query(context.Background(), QueryFilter{}); err != nil || got != nil {
		t.Errorf("nil recorder Query should return nil/nil, got %v / %v", got, err)
	}
}

func TestNopRecorder(t *testing.T) {
	var r Recorder = NopRecorder{}
	if err := r.Record(context.Background(), Action{Type: "x"}); err != nil {
		t.Errorf("NopRecorder should never error, got %v", err)
	}
}

// --- Tool registration tests ---

type mockRegistry struct {
	tools []string
	exec  map[string]func(ctx context.Context, input string) (string, error)
}

func newMockRegistry() *mockRegistry {
	return &mockRegistry{exec: make(map[string]func(ctx context.Context, input string) (string, error))}
}

func (m *mockRegistry) Register(name, _, _, _ string, exec func(ctx context.Context, input string) (string, error)) {
	m.tools = append(m.tools, name)
	m.exec[name] = exec
}

func TestRegisterListTool(t *testing.T) {
	db := newMemDB(t)
	r := NewSQLiteRecorder(db)
	_ = r.Record(context.Background(), Action{Type: "applescript.exec", Status: "success", Target: "Mail"})

	reg := newMockRegistry()
	RegisterListTool(reg, r)

	tool := reg.exec["actions.list"]
	if tool == nil {
		t.Fatal("actions.list not registered")
	}
	out, err := tool(context.Background(), `{}`)
	if err != nil {
		t.Fatalf("tool exec: %v", err)
	}
	var rows []Action
	if err := json.Unmarshal([]byte(out), &rows); err != nil {
		t.Fatalf("output not valid JSON: %v\noutput: %s", err, out)
	}
	if len(rows) != 1 || rows[0].Type != "applescript.exec" {
		t.Errorf("unexpected rows: %+v", rows)
	}
}

func TestListTool_FilterByType(t *testing.T) {
	db := newMemDB(t)
	r := NewSQLiteRecorder(db)
	_ = r.Record(context.Background(), Action{Type: "applescript.exec", Status: "success"})
	_ = r.Record(context.Background(), Action{Type: "dbus.call", Status: "success"})

	reg := newMockRegistry()
	RegisterListTool(reg, r)
	out, _ := reg.exec["actions.list"](context.Background(), `{"action_type":"dbus.call"}`)
	var rows []Action
	_ = json.Unmarshal([]byte(out), &rows)
	if len(rows) != 1 || rows[0].Type != "dbus.call" {
		t.Errorf("filter didn't apply, got %+v", rows)
	}
}

func TestListTool_EmptyInputAccepted(t *testing.T) {
	db := newMemDB(t)
	r := NewSQLiteRecorder(db)
	reg := newMockRegistry()
	RegisterListTool(reg, r)

	if _, err := reg.exec["actions.list"](context.Background(), ""); err != nil {
		t.Errorf("empty input should be accepted, got: %v", err)
	}
	if _, err := reg.exec["actions.list"](context.Background(), "{}"); err != nil {
		t.Errorf("'{}' input should be accepted, got: %v", err)
	}
}

func TestListTool_InvalidJSON(t *testing.T) {
	db := newMemDB(t)
	r := NewSQLiteRecorder(db)
	reg := newMockRegistry()
	RegisterListTool(reg, r)
	if _, err := reg.exec["actions.list"](context.Background(), `not json`); err == nil {
		t.Error("expected error for invalid JSON")
	}
}
