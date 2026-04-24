package streaming

import (
	"testing"
	"time"

	"github.com/LumabyteCo/aibutler/internal/agent"
)

func TestNewEventChannel_DefaultBufferSize(t *testing.T) {
	cfg := DefaultStreamConfig()
	ch := NewEventChannel(cfg)
	if cap(ch) != 64 {
		t.Errorf("buffer size = %d, want 64", cap(ch))
	}
}

func TestNewEventChannel_CustomBufferSize(t *testing.T) {
	cfg := StreamConfig{BufferSize: 128}
	ch := NewEventChannel(cfg)
	if cap(ch) != 128 {
		t.Errorf("buffer size = %d, want 128", cap(ch))
	}
}

func TestBackpressureRelay_DropsOnlyPings(t *testing.T) {
	src := make(chan agent.StreamEvent, 10)
	// dst has buffer of 1 to create backpressure.
	dst := make(chan agent.StreamEvent, 1)
	done := make(chan struct{})

	go BackpressureRelay(src, dst, done)

	// Fill dst so it's at capacity.
	src <- agent.StreamEvent{Type: "text_delta", Text: "hello"}
	// Give relay time to process.
	time.Sleep(20 * time.Millisecond)

	// Now send a ping — it should be dropped.
	src <- agent.StreamEvent{Type: "ping"}
	time.Sleep(20 * time.Millisecond)

	// Send a content event — it should block and eventually be delivered.
	go func() {
		src <- agent.StreamEvent{Type: "text_delta", Text: "world"}
	}()

	// Read first event.
	ev := <-dst
	if ev.Type != "text_delta" || ev.Text != "hello" {
		t.Errorf("first event = %v, want text_delta/hello", ev)
	}

	// Read second event — should be "world" (ping was dropped).
	select {
	case ev = <-dst:
		if ev.Type == "ping" {
			t.Error("ping should have been dropped under backpressure")
		}
		if ev.Text != "world" {
			t.Errorf("second event text = %q, want 'world'", ev.Text)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for second event")
	}

	close(src)
	// Wait for relay to finish.
	time.Sleep(50 * time.Millisecond)
}

func TestBackpressureRelay_NoPingDrop(t *testing.T) {
	// When consumer is fast, pings should pass through.
	src := make(chan agent.StreamEvent, 10)
	dst := make(chan agent.StreamEvent, 10)
	done := make(chan struct{})

	go BackpressureRelay(src, dst, done)

	src <- agent.StreamEvent{Type: "ping"}
	src <- agent.StreamEvent{Type: "text_delta", Text: "content"}
	close(src)

	// Collect events.
	time.Sleep(50 * time.Millisecond)
	var events []agent.StreamEvent
	for {
		select {
		case ev, ok := <-dst:
			if !ok {
				goto done
			}
			events = append(events, ev)
		default:
			goto done
		}
	}
done:
	if len(events) != 2 {
		t.Errorf("got %d events, want 2 (ping should pass when consumer is fast)", len(events))
	}
}
