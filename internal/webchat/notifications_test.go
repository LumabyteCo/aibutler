package webchat_test

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/LumabyteCo/aibutler/internal/channel"
	"github.com/LumabyteCo/aibutler/internal/notification"
	"github.com/LumabyteCo/aibutler/internal/webchat"
	"nhooyr.io/websocket"
)

type fakeNotifyStore struct {
	mu        sync.Mutex
	pending   []notification.Notification
	delivered []int64
	ch        chan int64
}

func (f *fakeNotifyStore) Pending(context.Context, string, int) ([]notification.Notification, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]notification.Notification(nil), f.pending...), nil
}

func (f *fakeNotifyStore) MarkDelivered(_ context.Context, id int64) error {
	f.mu.Lock()
	f.delivered = append(f.delivered, id)
	f.mu.Unlock()
	if f.ch != nil {
		select {
		case f.ch <- id:
		default:
		}
	}
	return nil
}

// TestQueuedNotificationDeliveredOnConnect guards durable webchat delivery:
// a scheduled result queued while no tab was open must arrive when one
// connects, then be marked delivered.
func TestQueuedNotificationDeliveredOnConnect(t *testing.T) {
	cfg := webchat.DefaultConfig()
	cfg.Port = 18101
	a := webchat.New(cfg)

	store := &fakeNotifyStore{
		pending: []notification.Notification{{
			ID: 7, Channel: "webchat", Text: "Scheduled: goodnight needs your PIN",
		}},
		ch: make(chan int64, 1),
	}
	a.SetNotificationStore(store)

	handler := func(context.Context, channel.Envelope) error { return nil }
	if err := a.Start(context.Background(), handler); err != nil {
		t.Fatal(err)
	}
	defer a.Stop(context.Background())
	time.Sleep(100 * time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, "ws://localhost:18101/ws", nil)
	if err != nil {
		t.Skipf("could not connect to websocket: %v", err)
		return
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	_, data, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("read queued notification: %v", err)
	}
	var frame struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(data, &frame); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if frame.Text != "Scheduled: goodnight needs your PIN" {
		t.Errorf("text = %q", frame.Text)
	}

	select {
	case id := <-store.ch:
		if id != 7 {
			t.Errorf("marked delivered id = %d, want 7", id)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("notification was not marked delivered")
	}
}
