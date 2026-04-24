//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/LumabyteCo/aibutler/internal/agent"
	"github.com/LumabyteCo/aibutler/internal/capability"
	"github.com/LumabyteCo/aibutler/internal/channel"
	"github.com/LumabyteCo/aibutler/internal/i18n"
	"github.com/LumabyteCo/aibutler/internal/model"
	"github.com/LumabyteCo/aibutler/internal/prompt"
	"github.com/LumabyteCo/aibutler/internal/session"
	"github.com/LumabyteCo/aibutler/internal/stopphrase"
	"github.com/LumabyteCo/aibutler/internal/webchat"
	"github.com/LumabyteCo/aibutler/testutil"
	"nhooyr.io/websocket"
)

// wsFrame matches the WebChat JSON protocol.
type wsFrame struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

// setupWebChat starts a full webchat adapter with FakeModel and returns
// the WebSocket URL and a cleanup function.
func setupWebChat(t *testing.T, responses ...agent.Response) (string, *testutil.FakeModel) {
	t.Helper()

	database := testutil.TestDB(t)
	cfg := testutil.TestConfig()
	fake := testutil.NewFakeModel(responses...)

	sm := session.NewManager(database.Conn(), cfg)
	tracker := prompt.NewTracker(database.Conn(), cfg)
	composer := prompt.NewComposer(cfg, sm, tracker, database.Conn())

	factory := model.NewFactory(model.FactoryConfig{
		Composer: composer,
		Model:    fake,
		Caps:     capability.NewCapabilitySet(capability.MessagingDefaults()),
		Tracker:  tracker,
		DB:       database.Conn(),
		Config:   cfg,
	})

	bundle := i18n.New("en")
	stop := stopphrase.NewMatcher(bundle)

	// Use a random high port for the webchat server.
	port := 19000 + int(time.Now().UnixNano()%1000)
	webCfg := webchat.Config{
		Port:          port,
		BindAddress:   "127.0.0.1",
		MaxUploadSize: 1024 * 1024,
	}
	adapter := webchat.New(webCfg)

	reg := channel.NewRegistry()
	reg.Register(adapter)

	router := channel.NewRouter(channel.RouterConfig{
		Sessions: sm,
		Stop:     stop,
		Channels: reg,
		Config:   cfg,
		I18n:     bundle,
		DB:       database.Conn(),
		Agent:    factory,
	})

	// Start webchat with the router as handler.
	ctx := context.Background()
	if err := adapter.Start(ctx, router.HandleMessage); err != nil {
		t.Fatalf("webchat start: %v", err)
	}
	t.Cleanup(func() {
		adapter.Stop(context.Background())
	})

	// Wait for server to be ready.
	time.Sleep(50 * time.Millisecond)

	wsURL := fmt.Sprintf("ws://127.0.0.1:%d/ws", port)
	return wsURL, fake
}

// readFrame reads a single JSON frame from the WebSocket.
func readFrame(ctx context.Context, conn *websocket.Conn) (wsFrame, error) {
	_, data, err := conn.Read(ctx)
	if err != nil {
		return wsFrame{}, err
	}
	var frame wsFrame
	if err := json.Unmarshal(data, &frame); err != nil {
		return wsFrame{}, err
	}
	return frame, nil
}

// sendMessage sends a text message frame over WebSocket.
func sendMessage(ctx context.Context, conn *websocket.Conn, text string) error {
	frame := wsFrame{Type: "message", Text: text}
	data, _ := json.Marshal(frame)
	return conn.Write(ctx, websocket.MessageText, data)
}

// TestWebChatE2E verifies: WebSocket → message → FakeModel → response back via WebSocket.
func TestWebChatE2E(t *testing.T) {
	wsURL, fake := setupWebChat(t, agent.Response{
		Content: "Hi from AI Butler!", TokensIn: 15, TokensOut: 8,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("ws dial: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	// Send a message.
	if err := sendMessage(ctx, conn, "Hello!"); err != nil {
		t.Fatalf("send: %v", err)
	}

	// Read response — may get a typing frame first.
	var gotResponse bool
	for i := 0; i < 5; i++ {
		frame, err := readFrame(ctx, conn)
		if err != nil {
			t.Fatalf("read frame: %v", err)
		}
		if frame.Type == "message" {
			if frame.Text != "Hi from AI Butler!" {
				t.Errorf("response text = %q, want 'Hi from AI Butler!'", frame.Text)
			}
			gotResponse = true
			break
		}
		// Skip typing frames.
	}
	if !gotResponse {
		t.Error("never received a message frame")
	}
	if fake.CallCount() != 1 {
		t.Errorf("model calls = %d, want 1", fake.CallCount())
	}
}

// TestWebChatMultiTurn verifies 2 messages over WebSocket share the same session.
func TestWebChatMultiTurn(t *testing.T) {
	wsURL, fake := setupWebChat(t,
		agent.Response{Content: "First.", TokensIn: 10, TokensOut: 5},
		agent.Response{Content: "Second.", TokensIn: 20, TokensOut: 10},
	)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("ws dial: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	// First message.
	if err := sendMessage(ctx, conn, "Turn one"); err != nil {
		t.Fatal(err)
	}
	// Read until we get a message response.
	for {
		frame, err := readFrame(ctx, conn)
		if err != nil {
			t.Fatal(err)
		}
		if frame.Type == "message" {
			if frame.Text != "First." {
				t.Errorf("first response = %q", frame.Text)
			}
			break
		}
	}

	// Second message.
	if err := sendMessage(ctx, conn, "Turn two"); err != nil {
		t.Fatal(err)
	}
	for {
		frame, err := readFrame(ctx, conn)
		if err != nil {
			t.Fatal(err)
		}
		if frame.Type == "message" {
			if frame.Text != "Second." {
				t.Errorf("second response = %q", frame.Text)
			}
			break
		}
	}

	if fake.CallCount() != 2 {
		t.Errorf("model calls = %d, want 2", fake.CallCount())
	}

	// Second model call should have more messages (history from first turn).
	calls := fake.Calls()
	if len(calls) == 2 && len(calls[1]) <= len(calls[0]) {
		t.Error("second call should include history from first turn")
	}
}
