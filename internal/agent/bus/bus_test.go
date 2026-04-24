package bus

import (
	"testing"
	"time"
)

func TestPublishSubscribe(t *testing.T) {
	b := New()
	ch := b.Subscribe("events")

	b.Publish("events", "agent-1", "hello world")

	select {
	case msg := <-ch:
		if msg.Topic != "events" {
			t.Errorf("topic = %q, want 'events'", msg.Topic)
		}
		if msg.From != "agent-1" {
			t.Errorf("from = %q, want 'agent-1'", msg.From)
		}
		if msg.Payload != "hello world" {
			t.Errorf("payload = %q, want 'hello world'", msg.Payload)
		}
		if msg.Time.IsZero() {
			t.Error("time should not be zero")
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for message")
	}
}

func TestMultipleSubscribers(t *testing.T) {
	b := New()
	ch1 := b.Subscribe("updates")
	ch2 := b.Subscribe("updates")

	b.Publish("updates", "agent-2", "broadcast")

	for i, ch := range []<-chan Message{ch1, ch2} {
		select {
		case msg := <-ch:
			if msg.Payload != "broadcast" {
				t.Errorf("subscriber %d: payload = %q, want 'broadcast'", i, msg.Payload)
			}
		case <-time.After(time.Second):
			t.Fatalf("subscriber %d: timed out", i)
		}
	}
}

func TestUnsubscribe(t *testing.T) {
	b := New()
	ch := b.Subscribe("topic-a")

	b.Unsubscribe("topic-a", ch)

	// After unsubscribe, channel should be closed.
	_, ok := <-ch
	if ok {
		t.Error("expected channel to be closed after unsubscribe")
	}

	// Publishing after unsubscribe should not panic.
	b.Publish("topic-a", "agent-3", "orphan")
}

func TestTopicIsolation(t *testing.T) {
	b := New()
	chA := b.Subscribe("topic-a")
	chB := b.Subscribe("topic-b")

	b.Publish("topic-a", "agent-1", "only for A")

	select {
	case msg := <-chA:
		if msg.Payload != "only for A" {
			t.Errorf("topic-a payload = %q, want 'only for A'", msg.Payload)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for topic-a message")
	}

	// topic-b should have no messages.
	select {
	case msg := <-chB:
		t.Errorf("topic-b should have no messages, got %+v", msg)
	case <-time.After(50 * time.Millisecond):
		// Expected: no message.
	}
}
