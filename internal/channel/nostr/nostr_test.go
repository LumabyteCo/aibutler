package nostr_test

import (
	"context"
	"testing"

	"github.com/LumabyteCo/aibutler/internal/channel/nostr"
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

func TestSend_WithMockFunc(t *testing.T) {
	var gotPubKey, gotText string
	client := nostr.NewClient("wss://relay.example.com", "deadbeef")
	client.SetSendFunc(func(ctx context.Context, pubkey, text string) error {
		gotPubKey = pubkey
		gotText = text
		return nil
	})

	err := client.Send(context.Background(), "abc123pubkey", "Hello Nostr!")
	if err != nil {
		t.Fatalf("Send: unexpected error: %v", err)
	}
	if gotPubKey != "abc123pubkey" {
		t.Errorf("expected pubkey=abc123pubkey, got %s", gotPubKey)
	}
	if gotText != "Hello Nostr!" {
		t.Errorf("expected text='Hello Nostr!', got %s", gotText)
	}
}

func TestParseEvent_Valid(t *testing.T) {
	data := []byte(`{
		"id": "event123",
		"pubkey": "abc123",
		"content": "Hello from Nostr",
		"kind": 4,
		"created_at": 1711900000
	}`)

	client := nostr.NewClient("wss://relay.example.com", "deadbeef")
	event, err := client.ParseEvent(data)
	if err != nil {
		t.Fatalf("ParseEvent: unexpected error: %v", err)
	}
	if event.ID != "event123" {
		t.Errorf("expected ID=event123, got %s", event.ID)
	}
	if event.PubKey != "abc123" {
		t.Errorf("expected PubKey=abc123, got %s", event.PubKey)
	}
	if event.Content != "Hello from Nostr" {
		t.Errorf("expected Content='Hello from Nostr', got %s", event.Content)
	}
	if event.Kind != 4 {
		t.Errorf("expected Kind=4, got %d", event.Kind)
	}
}

func TestRegisterNostrTools(t *testing.T) {
	reg := newMockRegistry()
	client := nostr.NewClient("wss://relay.example.com", "deadbeef")
	nostr.RegisterNostrTools(reg, client)

	want := map[string]bool{"nostr.send": false}
	for _, name := range reg.tools {
		if _, ok := want[name]; ok {
			want[name] = true
		}
	}
	for name, found := range want {
		if !found {
			t.Errorf("tool %q was not registered", name)
		}
	}
}
