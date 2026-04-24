package channel_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/LumabyteCo/aibutler/internal/channel"
)

// --- fake channel for testing ---

type fakeChannel struct {
	name    string
	mu      sync.Mutex
	sent    []channel.OutgoingMessage
	typing  int
	started bool
	stopped bool
}

func newFake(name string) *fakeChannel {
	return &fakeChannel{name: name}
}

func (f *fakeChannel) Name() string { return f.name }

func (f *fakeChannel) Start(_ context.Context, _ channel.MessageHandler) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.started = true
	return nil
}

func (f *fakeChannel) Stop(_ context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.stopped = true
	return nil
}

func (f *fakeChannel) Send(_ context.Context, _ string, msg channel.OutgoingMessage) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = append(f.sent, msg)
	return nil
}

func (f *fakeChannel) SendTyping(_ context.Context, _ string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.typing++
	return nil
}

func (f *fakeChannel) Sent() []channel.OutgoingMessage {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]channel.OutgoingMessage, len(f.sent))
	copy(out, f.sent)
	return out
}

func (f *fakeChannel) TypingCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.typing
}

// --- tests ---

func TestEnvelopeCreation(t *testing.T) {
	env := channel.Envelope{
		ID:        "msg-1",
		Channel:   "test",
		AccountID: "user-1",
		Type:      channel.TypeText,
		Text:      "hello",
		Timestamp: time.Now(),
	}
	if env.Channel != "test" {
		t.Errorf("channel = %q, want test", env.Channel)
	}
	if env.Type != channel.TypeText {
		t.Errorf("type = %q, want text", env.Type)
	}
}

func TestRegistryRegisterAndGet(t *testing.T) {
	reg := channel.NewRegistry()
	fake := newFake("test")
	reg.Register(fake)

	ch, ok := reg.Get("test")
	if !ok {
		t.Fatal("expected to find channel")
	}
	if ch.Name() != "test" {
		t.Errorf("name = %q, want test", ch.Name())
	}

	_, ok = reg.Get("nonexistent")
	if ok {
		t.Error("expected not found")
	}
}

func TestRegistryAll(t *testing.T) {
	reg := channel.NewRegistry()
	reg.Register(newFake("a"))
	reg.Register(newFake("b"))

	all := reg.All()
	if len(all) != 2 {
		t.Errorf("len = %d, want 2", len(all))
	}
}

func TestStubAdapter(t *testing.T) {
	stub := channel.NewStubAdapter("slack")
	if stub.Name() != "slack" {
		t.Errorf("name = %q, want slack", stub.Name())
	}

	ctx := context.Background()
	if err := stub.Start(ctx, nil); err != channel.ErrNotImplemented {
		t.Errorf("Start err = %v, want ErrNotImplemented", err)
	}
	if err := stub.Stop(ctx); err != channel.ErrNotImplemented {
		t.Errorf("Stop err = %v, want ErrNotImplemented", err)
	}
	if err := stub.Send(ctx, "", channel.OutgoingMessage{}); err != channel.ErrNotImplemented {
		t.Errorf("Send err = %v, want ErrNotImplemented", err)
	}
	if err := stub.SendTyping(ctx, ""); err != channel.ErrNotImplemented {
		t.Errorf("SendTyping err = %v, want ErrNotImplemented", err)
	}
}

func TestTypingManagerStartStop(t *testing.T) {
	tm := channel.NewTypingManager(50*time.Millisecond, 5*time.Second)
	fake := newFake("test")
	ctx := context.Background()

	tm.Start(ctx, fake, "user-1")
	time.Sleep(180 * time.Millisecond)
	tm.Stop("user-1")

	// Should have fired at least 2 times (immediate + 2-3 ticks).
	count := fake.TypingCount()
	if count < 2 {
		t.Errorf("typing count = %d, want >= 2", count)
	}
}

func TestTypingManagerTimeout(t *testing.T) {
	tm := channel.NewTypingManager(20*time.Millisecond, 100*time.Millisecond)
	fake := newFake("test")
	ctx := context.Background()

	tm.Start(ctx, fake, "user-1")
	time.Sleep(200 * time.Millisecond)

	// After timeout, typing should have stopped naturally.
	countBefore := fake.TypingCount()
	time.Sleep(100 * time.Millisecond)
	countAfter := fake.TypingCount()

	if countAfter != countBefore {
		t.Errorf("typing continued after timeout: before=%d, after=%d", countBefore, countAfter)
	}
}

func TestTypingManagerStopAll(t *testing.T) {
	tm := channel.NewTypingManager(50*time.Millisecond, 5*time.Second)
	fake := newFake("test")
	ctx := context.Background()

	tm.Start(ctx, fake, "user-1")
	tm.Start(ctx, fake, "user-2")
	time.Sleep(80 * time.Millisecond)
	tm.StopAll()

	countBefore := fake.TypingCount()
	time.Sleep(100 * time.Millisecond)
	countAfter := fake.TypingCount()

	if countAfter != countBefore {
		t.Errorf("typing continued after StopAll: before=%d, after=%d", countBefore, countAfter)
	}
}

func TestTypingManagerRestart(t *testing.T) {
	tm := channel.NewTypingManager(50*time.Millisecond, 5*time.Second)
	fake := newFake("test")
	ctx := context.Background()

	tm.Start(ctx, fake, "user-1")
	time.Sleep(80 * time.Millisecond)
	tm.Start(ctx, fake, "user-1") // restart
	time.Sleep(80 * time.Millisecond)
	tm.Stop("user-1")

	// Just verify no panic and typing count > 0.
	if fake.TypingCount() == 0 {
		t.Error("expected typing count > 0")
	}
}
