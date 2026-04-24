package channel

import (
	"context"
	"sync"
	"testing"
	"time"
)

// mockStreamChannel is a test channel that records sent messages.
type mockStreamChannel struct {
	mu       sync.Mutex
	name     string
	messages []OutgoingMessage
}

func (m *mockStreamChannel) Name() string                                         { return m.name }
func (m *mockStreamChannel) Start(ctx context.Context, handler MessageHandler) error { return nil }
func (m *mockStreamChannel) Stop(ctx context.Context) error                        { return nil }
func (m *mockStreamChannel) SendTyping(ctx context.Context, accountID string) error { return nil }

func (m *mockStreamChannel) Send(ctx context.Context, accountID string, msg OutgoingMessage) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.messages = append(m.messages, msg)
	return nil
}

func (m *mockStreamChannel) Messages() []OutgoingMessage {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]OutgoingMessage, len(m.messages))
	copy(result, m.messages)
	return result
}

func TestNewStreamDelivery(t *testing.T) {
	ch := &mockStreamChannel{name: "test"}
	sd := NewStreamDelivery(ch, "user1", 500*time.Millisecond)

	if sd == nil {
		t.Fatal("NewStreamDelivery returned nil")
	}
	if sd.channel != ch {
		t.Error("channel not set correctly")
	}
	if sd.accountID != "user1" {
		t.Error("accountID not set correctly")
	}
	if sd.interval != 500*time.Millisecond {
		t.Error("interval not set correctly")
	}
	if sd.Accumulated() != "" {
		t.Error("buffer should be empty initially")
	}
}

func TestStreamDeliveryImmediateMode(t *testing.T) {
	ch := &mockStreamChannel{name: "websocket"}
	sd := NewStreamDelivery(ch, "user1", 0) // 0 = immediate

	ctx := context.Background()

	// Deliver multiple tokens.
	if err := sd.DeliverToken(ctx, "Hello"); err != nil {
		t.Fatalf("DeliverToken: %v", err)
	}
	if err := sd.DeliverToken(ctx, " world"); err != nil {
		t.Fatalf("DeliverToken: %v", err)
	}

	msgs := ch.Messages()
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages in immediate mode, got %d", len(msgs))
	}

	// First message has accumulated "Hello", second has "Hello world".
	if msgs[0].Text != "Hello" {
		t.Errorf("first message text = %q, want %q", msgs[0].Text, "Hello")
	}
	if msgs[1].Text != "Hello world" {
		t.Errorf("second message text = %q, want %q", msgs[1].Text, "Hello world")
	}

	// Verify streaming flag.
	if !msgs[0].Streaming {
		t.Error("messages should have Streaming=true")
	}
}

func TestStreamDeliveryFlush(t *testing.T) {
	ch := &mockStreamChannel{name: "telegram"}
	sd := NewStreamDelivery(ch, "user1", 10*time.Second) // Very long interval

	ctx := context.Background()

	// Deliver tokens (won't send due to long interval).
	_ = sd.DeliverToken(ctx, "buffered")
	_ = sd.DeliverToken(ctx, " content")

	msgs := ch.Messages()
	// First token triggers send (lastSend is zero), but second won't.
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message before flush (first token always sends), got %d", len(msgs))
	}

	// Flush sends remaining.
	if err := sd.Flush(ctx); err != nil {
		t.Fatalf("Flush: %v", err)
	}

	msgs = ch.Messages()
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages after flush, got %d", len(msgs))
	}

	// Final message should have all accumulated text.
	if msgs[1].Text != "buffered content" {
		t.Errorf("flushed text = %q, want %q", msgs[1].Text, "buffered content")
	}

	// Accumulated should return full text.
	if sd.Accumulated() != "buffered content" {
		t.Errorf("Accumulated = %q, want %q", sd.Accumulated(), "buffered content")
	}
}
