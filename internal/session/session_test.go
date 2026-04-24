package session_test

import (
	"context"
	"testing"
	"time"

	"github.com/LumabyteCo/aibutler/internal/agent"
	"github.com/LumabyteCo/aibutler/internal/session"
	"github.com/LumabyteCo/aibutler/testutil"
)

func TestCreateAndGetSession(t *testing.T) {
	database := testutil.TestDB(t)
	cfg := testutil.TestConfig()
	mgr := session.NewManager(database.Conn(), cfg)
	ctx := context.Background()

	id, err := mgr.Create(ctx, "terminal", "user-1", "default")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if id == "" {
		t.Fatal("expected non-empty session ID")
	}

	sess, err := mgr.Get(ctx, id)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if sess.Channel != "terminal" {
		t.Errorf("channel = %q, want terminal", sess.Channel)
	}
	if sess.AccountID != "user-1" {
		t.Errorf("account_id = %q, want user-1", sess.AccountID)
	}
}

func TestGetNonexistentSession(t *testing.T) {
	database := testutil.TestDB(t)
	cfg := testutil.TestConfig()
	mgr := session.NewManager(database.Conn(), cfg)

	_, err := mgr.Get(context.Background(), "nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent session")
	}
}

func TestCloseSession(t *testing.T) {
	database := testutil.TestDB(t)
	cfg := testutil.TestConfig()
	mgr := session.NewManager(database.Conn(), cfg)
	ctx := context.Background()

	id, _ := mgr.Create(ctx, "telegram", "user-1", "default")

	if err := mgr.Close(ctx, id); err != nil {
		t.Fatalf("close: %v", err)
	}

	// Should still be retrievable.
	sess, err := mgr.Get(ctx, id)
	if err != nil {
		t.Fatalf("get after close: %v", err)
	}
	if sess.ID != id {
		t.Errorf("id = %q, want %q", sess.ID, id)
	}
}

func TestAddAndRetrieveMessages(t *testing.T) {
	database := testutil.TestDB(t)
	cfg := testutil.TestConfig()
	mgr := session.NewManager(database.Conn(), cfg)
	ctx := context.Background()

	id, _ := mgr.Create(ctx, "terminal", "user-1", "default")

	msgs := []agent.Message{
		{Role: "user", Content: "Hello"},
		{Role: "assistant", Content: "Hi there!"},
		{Role: "user", Content: "What's the weather?"},
	}

	for _, msg := range msgs {
		if err := mgr.AddMessage(ctx, id, msg); err != nil {
			t.Fatalf("add message: %v", err)
		}
	}

	got, err := mgr.Messages(ctx, id)
	if err != nil {
		t.Fatalf("messages: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("messages count = %d, want 3", len(got))
	}
	if got[0].Role != "user" || got[0].Content != "Hello" {
		t.Errorf("msg[0] = %v, want {user, Hello}", got[0])
	}
	if got[1].Role != "assistant" {
		t.Errorf("msg[1].role = %q, want assistant", got[1].Role)
	}
}

func TestToolMessage(t *testing.T) {
	database := testutil.TestDB(t)
	cfg := testutil.TestConfig()
	mgr := session.NewManager(database.Conn(), cfg)
	ctx := context.Background()

	id, _ := mgr.Create(ctx, "terminal", "user-1", "default")

	// Tool result message has a ToolID.
	mgr.AddMessage(ctx, id, agent.Message{Role: "tool", Content: "result data", ToolID: "tc-1"})

	got, _ := mgr.Messages(ctx, id)
	if len(got) != 1 {
		t.Fatalf("messages count = %d, want 1", len(got))
	}
	if got[0].ToolID != "tc-1" {
		t.Errorf("tool_id = %q, want tc-1", got[0].ToolID)
	}
}

func TestRecentMessages(t *testing.T) {
	database := testutil.TestDB(t)
	cfg := testutil.TestConfig()
	mgr := session.NewManager(database.Conn(), cfg)
	ctx := context.Background()

	id, _ := mgr.Create(ctx, "terminal", "user-1", "default")

	// Add 10 messages.
	for i := 0; i < 10; i++ {
		mgr.AddMessage(ctx, id, agent.Message{
			Role:    "user",
			Content: "message " + string(rune('A'+i)),
		})
	}

	// Get last 3.
	got, err := mgr.RecentMessages(ctx, id, 3)
	if err != nil {
		t.Fatalf("recent: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("recent count = %d, want 3", len(got))
	}
	// Should be the last 3 in chronological order.
	if got[0].Content != "message H" {
		t.Errorf("first recent = %q, want 'message H'", got[0].Content)
	}
	if got[2].Content != "message J" {
		t.Errorf("last recent = %q, want 'message J'", got[2].Content)
	}
}

func TestSlidingWindowFrugal(t *testing.T) {
	database := testutil.TestDB(t)
	cfg := testutil.TestConfig()
	cfg.Settings.Cost.Strategy = "frugal" // 30 messages
	mgr := session.NewManager(database.Conn(), cfg)
	ctx := context.Background()

	id, _ := mgr.Create(ctx, "terminal", "user-1", "default")

	// Add 50 messages.
	for i := 0; i < 50; i++ {
		mgr.AddMessage(ctx, id, agent.Message{Role: "user", Content: "msg"})
	}

	got, err := mgr.SlidingWindow(ctx, id)
	if err != nil {
		t.Fatalf("sliding window: %v", err)
	}
	if len(got) != 30 {
		t.Errorf("sliding window count = %d, want 30 (frugal)", len(got))
	}
}

func TestSlidingWindowBalanced(t *testing.T) {
	database := testutil.TestDB(t)
	cfg := testutil.TestConfig()
	cfg.Settings.Cost.Strategy = "balanced" // 100 messages
	mgr := session.NewManager(database.Conn(), cfg)
	ctx := context.Background()

	id, _ := mgr.Create(ctx, "terminal", "user-1", "default")

	// Add 50 messages (less than window).
	for i := 0; i < 50; i++ {
		mgr.AddMessage(ctx, id, agent.Message{Role: "user", Content: "msg"})
	}

	got, err := mgr.SlidingWindow(ctx, id)
	if err != nil {
		t.Fatalf("sliding window: %v", err)
	}
	// Should return all 50 since window is 100.
	if len(got) != 50 {
		t.Errorf("sliding window count = %d, want 50 (all fit in balanced window)", len(got))
	}
}

func TestSlidingWindowQuality(t *testing.T) {
	database := testutil.TestDB(t)
	cfg := testutil.TestConfig()
	cfg.Settings.Cost.Strategy = "quality" // 200 messages
	mgr := session.NewManager(database.Conn(), cfg)
	ctx := context.Background()

	id, _ := mgr.Create(ctx, "terminal", "user-1", "default")

	// Add 250 messages.
	for i := 0; i < 250; i++ {
		mgr.AddMessage(ctx, id, agent.Message{Role: "user", Content: "msg"})
	}

	got, err := mgr.SlidingWindow(ctx, id)
	if err != nil {
		t.Fatalf("sliding window: %v", err)
	}
	if len(got) != 200 {
		t.Errorf("sliding window count = %d, want 200 (quality)", len(got))
	}
}

func TestDeleteSession(t *testing.T) {
	database := testutil.TestDB(t)
	cfg := testutil.TestConfig()
	mgr := session.NewManager(database.Conn(), cfg)
	ctx := context.Background()

	id, _ := mgr.Create(ctx, "terminal", "user-1", "default")
	mgr.AddMessage(ctx, id, agent.Message{Role: "user", Content: "hello"})

	if err := mgr.Delete(ctx, id); err != nil {
		t.Fatalf("delete: %v", err)
	}

	// Session should be gone.
	_, err := mgr.Get(ctx, id)
	if err == nil {
		t.Error("expected error after deleting session")
	}

	// Messages should be gone too.
	msgs, err := mgr.Messages(ctx, id)
	if err != nil {
		t.Fatalf("messages: %v", err)
	}
	if len(msgs) != 0 {
		t.Errorf("messages count = %d, want 0 after delete", len(msgs))
	}
}

func TestCleanupExpired(t *testing.T) {
	database := testutil.TestDB(t)
	cfg := testutil.TestConfig()
	mgr := session.NewManager(database.Conn(), cfg)
	ctx := context.Background()

	// Create a session.
	id, _ := mgr.Create(ctx, "terminal", "user-1", "default")

	// Backdate the session to make it "expired".
	_, err := database.Conn().ExecContext(ctx,
		`UPDATE sessions SET updated_at = '2020-01-01T00:00:00Z' WHERE id = ?`, id)
	if err != nil {
		t.Fatalf("backdate: %v", err)
	}

	// Create a fresh session (should NOT be cleaned up).
	freshID, _ := mgr.Create(ctx, "terminal", "user-2", "default")

	// Cleanup sessions older than 1 day.
	count, err := mgr.CleanupExpired(ctx, 24*time.Hour)
	if err != nil {
		t.Fatalf("cleanup: %v", err)
	}
	if count != 1 {
		t.Errorf("cleaned = %d, want 1", count)
	}

	// Old session gone.
	_, err = mgr.Get(ctx, id)
	if err == nil {
		t.Error("expected old session to be deleted")
	}

	// Fresh session still exists.
	_, err = mgr.Get(ctx, freshID)
	if err != nil {
		t.Errorf("fresh session should still exist: %v", err)
	}
}

func TestCount(t *testing.T) {
	database := testutil.TestDB(t)
	cfg := testutil.TestConfig()
	mgr := session.NewManager(database.Conn(), cfg)
	ctx := context.Background()

	c, err := mgr.Count(ctx)
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if c != 0 {
		t.Errorf("count = %d, want 0", c)
	}

	mgr.Create(ctx, "terminal", "user-1", "default")
	mgr.Create(ctx, "terminal", "user-2", "default")
	mgr.Create(ctx, "telegram", "user-3", "default")

	c, err = mgr.Count(ctx)
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if c != 3 {
		t.Errorf("count = %d, want 3", c)
	}
}

func TestCleanupExpiredNoSessions(t *testing.T) {
	database := testutil.TestDB(t)
	cfg := testutil.TestConfig()
	mgr := session.NewManager(database.Conn(), cfg)

	count, err := mgr.CleanupExpired(context.Background(), 24*time.Hour)
	if err != nil {
		t.Fatalf("cleanup: %v", err)
	}
	if count != 0 {
		t.Errorf("cleaned = %d, want 0", count)
	}
}
