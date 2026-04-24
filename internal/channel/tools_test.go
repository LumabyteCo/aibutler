package channel_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/LumabyteCo/aibutler/internal/agent"
	"github.com/LumabyteCo/aibutler/internal/capability"
	"github.com/LumabyteCo/aibutler/internal/channel"
	"github.com/LumabyteCo/aibutler/internal/contact"
	"github.com/LumabyteCo/aibutler/internal/tool"
	"github.com/LumabyteCo/aibutler/testutil"
)

func TestChannelReadReturnsMessages(t *testing.T) {
	database := testutil.TestDB(t)
	conn := database.Conn()
	ctx := context.Background()

	// Create a session and add messages.
	conn.ExecContext(ctx, `INSERT INTO sessions (id, channel, account_id, scope) VALUES ('s1', 'webchat', 'user1', 'default')`)
	conn.ExecContext(ctx, `INSERT INTO messages (session_id, role, content) VALUES ('s1', 'user', 'Hello')`)
	conn.ExecContext(ctx, `INSERT INTO messages (session_id, role, content) VALUES ('s1', 'assistant', 'Hi there!')`)

	reg := tool.NewRegistry()
	channels := channel.NewRegistry()
	resolver := contact.NewResolver(conn)
	channel.RegisterChannelToolsWithDeps(reg, channels, conn, resolver)

	disp := tool.NewDispatcher(reg, capability.NewEngine(nil), nil)
	result, err := disp.Execute(ctx, agent.ToolCall{
		Name:  "channel.read",
		Input: `{"session_id":"s1","limit":10}`,
	})
	if err != nil {
		t.Fatalf("channel.read: %v", err)
	}

	var msgs []struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}
	json.Unmarshal([]byte(result), &msgs)
	if len(msgs) != 2 {
		t.Fatalf("messages = %d, want 2", len(msgs))
	}
	if msgs[0].Content != "Hello" || msgs[1].Content != "Hi there!" {
		t.Errorf("messages = %v", msgs)
	}
}

func TestChannelRelayResolvesContact(t *testing.T) {
	database := testutil.TestDB(t)
	conn := database.Conn()
	ctx := context.Background()

	// Insert contact with channel_ids.
	conn.ExecContext(ctx,
		`INSERT INTO user_contacts (name, email, preferred_channel, channel_ids) VALUES (?, ?, ?, ?)`,
		"Alice", "alice@test.com", "testchan", `{"testchan":"alice123"}`)

	// Set up fake channel.
	channels := channel.NewRegistry()
	fake := &toolTestChannel{name: "testchan"}
	channels.Register(fake)

	resolver := contact.NewResolver(conn)
	reg := tool.NewRegistry()
	channel.RegisterChannelToolsWithDeps(reg, channels, conn, resolver)

	disp := tool.NewDispatcher(reg, capability.NewEngine(nil), nil)
	result, err := disp.Execute(ctx, agent.ToolCall{
		Name:  "channel.relay",
		Input: `{"contact":"Alice","text":"Hello Alice!"}`,
	})
	if err != nil {
		t.Fatalf("channel.relay: %v", err)
	}
	if !strings.Contains(result, "relayed to Alice") {
		t.Errorf("result = %q", result)
	}

	// Verify the fake channel received the send.
	if fake.lastAccountID != "alice123" {
		t.Errorf("accountID = %q, want alice123", fake.lastAccountID)
	}
	if fake.lastMsg.Text != "Hello Alice!" {
		t.Errorf("msg = %q, want 'Hello Alice!'", fake.lastMsg.Text)
	}
}

func TestChannelRelayExplicitChannel(t *testing.T) {
	database := testutil.TestDB(t)
	conn := database.Conn()
	ctx := context.Background()

	conn.ExecContext(ctx,
		`INSERT INTO user_contacts (name, phone, preferred_channel) VALUES ('Bob', '+1234', 'telegram')`)

	channels := channel.NewRegistry()
	fake := &toolTestChannel{name: "telegram"}
	channels.Register(fake)

	resolver := contact.NewResolver(conn)
	reg := tool.NewRegistry()
	channel.RegisterChannelToolsWithDeps(reg, channels, conn, resolver)

	disp := tool.NewDispatcher(reg, capability.NewEngine(nil), nil)
	result, err := disp.Execute(ctx, agent.ToolCall{
		Name:  "channel.relay",
		Input: `{"contact":"Bob","text":"Hi Bob","channel":"telegram"}`,
	})
	if err != nil {
		t.Fatalf("channel.relay: %v", err)
	}
	if !strings.Contains(result, "via telegram") {
		t.Errorf("result = %q", result)
	}
	if fake.lastAccountID != "+1234" {
		t.Errorf("accountID = %q, want +1234 (phone fallback for telegram)", fake.lastAccountID)
	}
}

func TestChannelRelayContactNotFound(t *testing.T) {
	database := testutil.TestDB(t)
	conn := database.Conn()

	channels := channel.NewRegistry()
	resolver := contact.NewResolver(conn)
	reg := tool.NewRegistry()
	channel.RegisterChannelToolsWithDeps(reg, channels, conn, resolver)

	disp := tool.NewDispatcher(reg, capability.NewEngine(nil), nil)
	_, err := disp.Execute(context.Background(), agent.ToolCall{
		Name:  "channel.relay",
		Input: `{"contact":"Ghost","text":"Hello"}`,
	})
	if err == nil {
		t.Fatal("expected error for unknown contact")
	}
	if !strings.Contains(err.Error(), "no contact found") {
		t.Errorf("error = %q", err.Error())
	}
}

// toolTestChannel is a test double that captures the last Send call.
type toolTestChannel struct {
	name          string
	lastAccountID string
	lastMsg       channel.OutgoingMessage
}

func (f *toolTestChannel) Name() string { return f.name }
func (f *toolTestChannel) Start(_ context.Context, _ channel.MessageHandler) error {
	return nil
}
func (f *toolTestChannel) Stop(_ context.Context) error { return nil }
func (f *toolTestChannel) Send(_ context.Context, accountID string, msg channel.OutgoingMessage) error {
	f.lastAccountID = accountID
	f.lastMsg = msg
	return nil
}
func (f *toolTestChannel) SendTyping(_ context.Context, _ string) error { return nil }
