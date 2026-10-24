package webchat_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/LumabyteCo/aibutler/internal/channel"
	"github.com/LumabyteCo/aibutler/internal/webchat"
	"nhooyr.io/websocket"
)

// TestBroadcastReachesConnectedClients is the delivery half of the
// scheduled-safety policy: a run's summary must reach an open webchat tab,
// even though webchat account IDs change on every reconnect.
func TestBroadcastReachesConnectedClients(t *testing.T) {
	cfg := webchat.DefaultConfig()
	cfg.Port = 18099
	a := webchat.New(cfg)

	handler := func(context.Context, channel.Envelope) error { return nil }
	if err := a.Start(context.Background(), handler); err != nil {
		t.Fatal(err)
	}
	defer a.Stop(context.Background())
	time.Sleep(100 * time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, _, err := websocket.Dial(ctx, "ws://localhost:18099/ws", nil)
	if err != nil {
		t.Skipf("could not connect to websocket: %v", err)
		return
	}
	defer conn.Close(websocket.StatusNormalClosure, "")
	time.Sleep(100 * time.Millisecond) // let the server register the connection

	want := "Scheduled briefing: all clear."
	if err := a.Broadcast(ctx, channel.OutgoingMessage{Text: want}); err != nil {
		t.Fatalf("Broadcast: %v", err)
	}

	_, data, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("read broadcast: %v", err)
	}
	var frame struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(data, &frame); err != nil {
		t.Fatalf("unmarshal frame: %v", err)
	}
	if frame.Text != want {
		t.Errorf("broadcast text = %q, want %q", frame.Text, want)
	}
}

// TestBroadcastWithoutClients reports the offline case instead of
// pretending delivery succeeded.
func TestBroadcastWithoutClients(t *testing.T) {
	a := webchat.New(webchat.DefaultConfig())
	err := a.Broadcast(context.Background(), channel.OutgoingMessage{Text: "x"})
	if err == nil {
		t.Fatal("expected an error when no clients are connected")
	}
}
