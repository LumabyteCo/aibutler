package webchat_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/LumabyteCo/aibutler/internal/webchat"
	"github.com/LumabyteCo/aibutler/testutil"
)

// fakeHistory is a controllable HistoryStore for the route tests.
type fakeHistory struct {
	entries []webchat.HistoryEntry
	calls   int
}

func (f *fakeHistory) RecentForAccount(_ context.Context, _ string, limit int) ([]webchat.HistoryEntry, error) {
	f.calls++
	if len(f.entries) > limit {
		return f.entries[:limit], nil
	}
	return f.entries, nil
}

// TestHistoryEndpoint guards the B5 fix: the chat panel rehydrates after a
// reload by fetching /api/history, which returns the account's recent
// messages oldest-first. Before this endpoint the panel always started
// blank even though every turn was in SQLite.
func TestHistoryEndpoint(t *testing.T) {
	adapter := webchat.New(webchat.Config{})
	adapter.SetHistoryStore(&fakeHistory{
		entries: []webchat.HistoryEntry{
			{SessionID: "s1", Role: "user", Content: "hello", CreatedAt: "2026-10-05T09:00:00Z"},
			{SessionID: "s1", Role: "assistant", Content: "hi there", CreatedAt: "2026-10-05T09:00:05Z"},
		},
	})
	srv := httptest.NewServer(adapter.HistoryHandlerForTest())
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/history?account=127.0.0.1&limit=50")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	var out struct {
		Entries []webchat.HistoryEntry `json:"entries"`
		Count    int                    `json:"count"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(out.Entries) != 2 {
		t.Fatalf("entries = %d, want 2", len(out.Entries))
	}
	if out.Entries[0].Content != "hello" || out.Entries[1].Content != "hi there" {
		t.Errorf("order wrong: [%s, %s]", out.Entries[0].Content, out.Entries[1].Content)
	}
}

// TestHistoryEndpointNoStore verifies the 503 when no store is wired (older
// binaries / disabled feature) — the UI treats this as "no history".
func TestHistoryEndpointNoStore(t *testing.T) {
	adapter := webchat.New(webchat.Config{})
	srv := httptest.NewServer(adapter.HistoryHandlerForTest())
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/history")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", resp.StatusCode)
	}
}

// TestHistoryStoreQuery verifies the SQL-backed store against a real
// schema: seeded webchat sessions for an account come back oldest-first.
func TestHistoryStoreQuery(t *testing.T) {
	database := testutil.TestDB(t)
	conn := database.Conn()
	ctx := context.Background()

	// Seed two webchat sessions for the same account.
	for _, sid := range []string{"sess-a", "sess-b"} {
		if _, err := conn.ExecContext(ctx,
			`INSERT INTO sessions (id, channel, account_id, scope) VALUES (?, 'webchat', '1.2.3.4', 'default')`,
			sid); err != nil {
			t.Fatalf("seed session: %v", err)
		}
	}
	msgs := []struct{ sid, role, content, at string }{
		{"sess-a", "user", "first message", "2026-10-05T09:00:00Z"},
		{"sess-a", "assistant", "first reply", "2026-10-05T09:00:01Z"},
		{"sess-b", "user", "later message", "2026-10-05T09:05:00Z"},
		{"sess-b", "assistant", "later reply", "2026-10-05T09:05:02Z"},
	}
	for i, m := range msgs {
		if _, err := conn.ExecContext(ctx,
			`INSERT INTO messages (session_id, role, content, created_at) VALUES (?, ?, ?, ?)`,
			m.sid, m.role, m.content, m.at); err != nil {
			t.Fatalf("seed msg %d: %v", i, err)
		}
	}
	// A session for a DIFFERENT account — must not leak.
	if _, err := conn.ExecContext(ctx,
		`INSERT INTO sessions (id, channel, account_id, scope) VALUES ('sess-other', 'webchat', '9.9.9.9', 'default')`); err != nil {
		t.Fatalf("seed other session: %v", err)
	}
	if _, err := conn.ExecContext(ctx,
		`INSERT INTO messages (session_id, role, content, created_at) VALUES ('sess-other', 'user', 'secret for other', '2026-10-05T09:03:00Z')`); err != nil {
		t.Fatalf("seed other msg: %v", err)
	}

	store := webchat.NewHistoryStore(conn)
	entries, err := store.RecentForAccount(ctx, "1.2.3.4", 50)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(entries) != 4 {
		t.Fatalf("entries = %d, want 4", len(entries))
	}
	if entries[0].Content != "first message" || entries[3].Content != "later reply" {
		t.Errorf("chronological order broken: first=%q last=%q", entries[0].Content, entries[3].Content)
	}
	for _, e := range entries {
		if e.Content == "secret for other" {
			t.Error("history leaked another account's messages")
		}
	}
}