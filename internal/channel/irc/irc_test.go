package irc_test

import (
	"context"
	"testing"

	"github.com/LumabyteCo/aibutler/internal/channel/irc"
)

// mockRegistry records registered tools.
type mockRegistry struct {
	tools []string
	exec  map[string]func(ctx context.Context, input string) (string, error)
}

func newMockRegistry() *mockRegistry {
	return &mockRegistry{exec: make(map[string]func(ctx context.Context, input string) (string, error))}
}

func (m *mockRegistry) Register(name, description, schema, capability string, exec func(ctx context.Context, input string) (string, error)) {
	m.tools = append(m.tools, name)
	m.exec[name] = exec
}

func TestSend_WithMockFn(t *testing.T) {
	client := irc.NewClient("irc.example.com:6667", "butler")

	var sentTarget, sentMessage string
	client.SetSendFunc(func(_ context.Context, target, message string) error {
		sentTarget = target
		sentMessage = message
		return nil
	})

	err := client.Send(context.Background(), "#general", "Hello IRC!")
	if err != nil {
		t.Fatalf("Send: unexpected error: %v", err)
	}
	if sentTarget != "#general" {
		t.Errorf("expected target=#general, got %s", sentTarget)
	}
	if sentMessage != "Hello IRC!" {
		t.Errorf("expected message='Hello IRC!', got %s", sentMessage)
	}
}

func TestParseMessage_PRIVMSG(t *testing.T) {
	raw := ":nick!user@host PRIVMSG #channel :Hello, world!"
	msg, err := irc.ParseMessage(raw)
	if err != nil {
		t.Fatalf("ParseMessage: unexpected error: %v", err)
	}
	if msg.From != "nick" {
		t.Errorf("expected From=nick, got %s", msg.From)
	}
	if msg.Target != "#channel" {
		t.Errorf("expected Target=#channel, got %s", msg.Target)
	}
	if msg.Text != "Hello, world!" {
		t.Errorf("expected Text='Hello, world!', got %s", msg.Text)
	}
	if msg.Command != "PRIVMSG" {
		t.Errorf("expected Command=PRIVMSG, got %s", msg.Command)
	}
}

func TestRegisterIRCTools(t *testing.T) {
	reg := newMockRegistry()
	client := irc.NewClient("irc.example.com:6667", "butler")
	irc.RegisterIRCTools(reg, client)

	if len(reg.tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(reg.tools))
	}
	if reg.tools[0] != "irc.send" {
		t.Errorf("expected tool irc.send, got %s", reg.tools[0])
	}
}
