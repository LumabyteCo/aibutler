package discord_test

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/LumabyteCo/aibutler/internal/channel"
	"github.com/LumabyteCo/aibutler/internal/discord"
	"nhooyr.io/websocket"
)

// --- Mock API Client ---

type sendCall struct {
	channelID string
	content   string
}

type editCall struct {
	channelID string
	messageID string
	content   string
}

type mockAPI struct {
	gatewayURL  string
	sendCalls   []sendCall
	editCalls   []editCall
	typingCalls []string
	mu          sync.Mutex
}

func (m *mockAPI) SendMessage(_ context.Context, channelID, content string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sendCalls = append(m.sendCalls, sendCall{channelID: channelID, content: content})
	return "msg-" + channelID, nil
}

func (m *mockAPI) EditMessage(_ context.Context, channelID, messageID, content string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.editCalls = append(m.editCalls, editCall{channelID: channelID, messageID: messageID, content: content})
	return nil
}

func (m *mockAPI) TriggerTyping(_ context.Context, channelID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.typingCalls = append(m.typingCalls, channelID)
	return nil
}

func (m *mockAPI) GetGatewayURL(_ context.Context) (string, error) {
	return m.gatewayURL, nil
}

// --- Test Gateway Server ---

// gatewayEvent mirrors the wire format.
type gatewayEvent struct {
	Op int              `json:"op"`
	D  json.RawMessage  `json:"d,omitempty"`
	S  *int64           `json:"s,omitempty"`
	T  string           `json:"t,omitempty"`
}

// testGateway is a fake Discord Gateway WebSocket server.
type testGateway struct {
	server       *httptest.Server
	heartbeatMs  int
	mu           sync.Mutex
	conn         *websocket.Conn
	identifyRecv chan json.RawMessage
	heartbeats   []json.RawMessage
}

func newTestGateway(t *testing.T, heartbeatMs int) *testGateway {
	t.Helper()
	gw := &testGateway{
		heartbeatMs:  heartbeatMs,
		identifyRecv: make(chan json.RawMessage, 1),
	}
	gw.server = httptest.NewServer(http.HandlerFunc(gw.handler))
	return gw
}

func (gw *testGateway) handler(w http.ResponseWriter, r *http.Request) {
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		InsecureSkipVerify: true,
	})
	if err != nil {
		return
	}
	gw.mu.Lock()
	gw.conn = conn
	gw.mu.Unlock()

	conn.SetReadLimit(1 << 20)

	// Send Hello (op 10).
	hello := gatewayEvent{Op: 10}
	helloData, _ := json.Marshal(map[string]int{"heartbeat_interval": gw.heartbeatMs})
	hello.D = helloData
	helloBytes, _ := json.Marshal(hello)
	_ = conn.Write(r.Context(), websocket.MessageText, helloBytes)

	// Read messages until close.
	for {
		_, data, err := conn.Read(r.Context())
		if err != nil {
			return
		}

		var event gatewayEvent
		if err := json.Unmarshal(data, &event); err != nil {
			continue
		}

		switch event.Op {
		case 2: // Identify
			select {
			case gw.identifyRecv <- event.D:
			default:
			}
		case 1: // Heartbeat
			gw.mu.Lock()
			gw.heartbeats = append(gw.heartbeats, event.D)
			gw.mu.Unlock()
			// Send Heartbeat ACK (op 11).
			ack := gatewayEvent{Op: 11}
			ackBytes, _ := json.Marshal(ack)
			_ = conn.Write(r.Context(), websocket.MessageText, ackBytes)
		}
	}
}

func (gw *testGateway) sendMessageCreate(t *testing.T, id, channelID, content, authorID string, bot bool) {
	t.Helper()
	gw.mu.Lock()
	c := gw.conn
	gw.mu.Unlock()
	if c == nil {
		t.Fatal("no gateway connection")
	}

	seq := int64(1)
	msgData, _ := json.Marshal(map[string]interface{}{
		"id":         id,
		"channel_id": channelID,
		"content":    content,
		"author": map[string]interface{}{
			"id":       authorID,
			"username": "testuser",
			"bot":      bot,
		},
	})
	event := gatewayEvent{
		Op: 0,
		D:  msgData,
		S:  &seq,
		T:  "MESSAGE_CREATE",
	}
	eventBytes, _ := json.Marshal(event)
	if err := c.Write(context.Background(), websocket.MessageText, eventBytes); err != nil {
		t.Fatalf("failed to send MESSAGE_CREATE: %v", err)
	}
}

func (gw *testGateway) url() string {
	// Convert http:// to ws://.
	return strings.Replace(gw.server.URL, "http://", "ws://", 1)
}

func (gw *testGateway) close() {
	gw.server.Close()
}

// --- Tests ---

func TestDiscordName(t *testing.T) {
	a := discord.New(&mockAPI{})
	if a.Name() != "discord" {
		t.Errorf("name = %q, want discord", a.Name())
	}
}

func TestNormalizeMessageCreate(t *testing.T) {
	gw := newTestGateway(t, 30000) // 30s heartbeat (won't fire during test).
	defer gw.close()

	api := &mockAPI{gatewayURL: gw.url()}
	a := discord.New(api)

	received := make(chan channel.Envelope, 1)
	handler := func(_ context.Context, env channel.Envelope) error {
		received <- env
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := a.Start(ctx, handler); err != nil {
		t.Fatal(err)
	}
	defer a.Stop(ctx)

	// Wait for identify to complete.
	select {
	case <-gw.identifyRecv:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for identify")
	}

	// Send a MESSAGE_CREATE event.
	gw.sendMessageCreate(t, "msg-123", "chan-456", "hello discord", "user-789", false)

	select {
	case env := <-received:
		if env.ID != "msg-123" {
			t.Errorf("ID = %q, want msg-123", env.ID)
		}
		if env.Channel != "discord" {
			t.Errorf("Channel = %q, want discord", env.Channel)
		}
		if env.AccountID != "chan-456" {
			t.Errorf("AccountID = %q, want chan-456", env.AccountID)
		}
		if env.ThreadID != "chan-456" {
			t.Errorf("ThreadID = %q, want chan-456", env.ThreadID)
		}
		if env.Type != channel.TypeText {
			t.Errorf("Type = %q, want text", env.Type)
		}
		if env.Text != "hello discord" {
			t.Errorf("Text = %q, want 'hello discord'", env.Text)
		}
	case <-time.After(3 * time.Second):
		t.Error("timeout waiting for message")
	}
}

func TestNormalizeSkipBot(t *testing.T) {
	gw := newTestGateway(t, 30000)
	defer gw.close()

	api := &mockAPI{gatewayURL: gw.url()}
	a := discord.New(api)

	called := make(chan struct{}, 1)
	handler := func(_ context.Context, _ channel.Envelope) error {
		called <- struct{}{}
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := a.Start(ctx, handler); err != nil {
		t.Fatal(err)
	}
	defer a.Stop(ctx)

	select {
	case <-gw.identifyRecv:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for identify")
	}

	// Send a bot message -- should be skipped.
	gw.sendMessageCreate(t, "msg-bot", "chan-1", "bot message", "bot-user", true)

	select {
	case <-called:
		t.Error("handler should NOT be called for bot messages")
	case <-time.After(500 * time.Millisecond):
		// Good -- handler was not called.
	}
}

func TestSendMessage(t *testing.T) {
	api := &mockAPI{}
	a := discord.New(api)

	ctx := context.Background()
	err := a.Send(ctx, "chan-100", channel.OutgoingMessage{Text: "hello!"})
	if err != nil {
		t.Fatal(err)
	}

	api.mu.Lock()
	defer api.mu.Unlock()
	if len(api.sendCalls) != 1 {
		t.Fatalf("sendCalls = %d, want 1", len(api.sendCalls))
	}
	if api.sendCalls[0].channelID != "chan-100" {
		t.Errorf("channelID = %q, want chan-100", api.sendCalls[0].channelID)
	}
	if api.sendCalls[0].content != "hello!" {
		t.Errorf("content = %q, want 'hello!'", api.sendCalls[0].content)
	}
}

func TestEditMessage(t *testing.T) {
	api := &mockAPI{}
	a := discord.New(api)

	ctx := context.Background()
	err := a.Send(ctx, "chan-200", channel.OutgoingMessage{
		Text:   "updated text",
		EditID: "msg-42",
	})
	if err != nil {
		t.Fatal(err)
	}

	api.mu.Lock()
	defer api.mu.Unlock()
	if len(api.editCalls) != 1 {
		t.Fatalf("editCalls = %d, want 1", len(api.editCalls))
	}
	if api.editCalls[0].channelID != "chan-200" {
		t.Errorf("channelID = %q, want chan-200", api.editCalls[0].channelID)
	}
	if api.editCalls[0].messageID != "msg-42" {
		t.Errorf("messageID = %q, want msg-42", api.editCalls[0].messageID)
	}
	if api.editCalls[0].content != "updated text" {
		t.Errorf("content = %q, want 'updated text'", api.editCalls[0].content)
	}
	// SendMessage should NOT have been called.
	if len(api.sendCalls) != 0 {
		t.Errorf("sendCalls = %d, want 0 (edit should not send)", len(api.sendCalls))
	}
}

func TestTriggerTyping(t *testing.T) {
	api := &mockAPI{}
	a := discord.New(api)

	ctx := context.Background()
	if err := a.SendTyping(ctx, "chan-300"); err != nil {
		t.Fatal(err)
	}

	api.mu.Lock()
	defer api.mu.Unlock()
	if len(api.typingCalls) != 1 {
		t.Fatalf("typingCalls = %d, want 1", len(api.typingCalls))
	}
	if api.typingCalls[0] != "chan-300" {
		t.Errorf("channelID = %q, want chan-300", api.typingCalls[0])
	}
}

func TestSendTyping(t *testing.T) {
	api := &mockAPI{}
	a := discord.New(api)

	ctx := context.Background()
	if err := a.SendTyping(ctx, "chan-500"); err != nil {
		t.Fatal(err)
	}

	api.mu.Lock()
	defer api.mu.Unlock()
	if len(api.typingCalls) != 1 {
		t.Fatalf("typingCalls = %d, want 1", len(api.typingCalls))
	}
	if api.typingCalls[0] != "chan-500" {
		t.Errorf("channelID = %q, want chan-500", api.typingCalls[0])
	}
}

func TestHeartbeat(t *testing.T) {
	// Use a very short heartbeat interval so we can observe it.
	gw := newTestGateway(t, 100) // 100ms
	defer gw.close()

	api := &mockAPI{gatewayURL: gw.url()}
	a := discord.New(api)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := a.Start(ctx, func(_ context.Context, _ channel.Envelope) error { return nil }); err != nil {
		t.Fatal(err)
	}
	defer a.Stop(ctx)

	// Wait for at least 2 heartbeats.
	deadline := time.After(2 * time.Second)
	for {
		gw.mu.Lock()
		n := len(gw.heartbeats)
		gw.mu.Unlock()
		if n >= 2 {
			break
		}
		select {
		case <-deadline:
			t.Fatalf("only received %d heartbeats, want >= 2", n)
		case <-time.After(50 * time.Millisecond):
		}
	}
}

func TestGatewayIdentify(t *testing.T) {
	gw := newTestGateway(t, 30000)
	defer gw.close()

	api := &mockAPI{gatewayURL: gw.url()}
	a := discord.New(api)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := a.Start(ctx, func(_ context.Context, _ channel.Envelope) error { return nil }); err != nil {
		t.Fatal(err)
	}
	defer a.Stop(ctx)

	var identData json.RawMessage
	select {
	case identData = <-gw.identifyRecv:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for identify")
	}

	var payload struct {
		Token   string `json:"token"`
		Intents int    `json:"intents"`
		Props   struct {
			OS      string `json:"os"`
			Browser string `json:"browser"`
			Device  string `json:"device"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(identData, &payload); err != nil {
		t.Fatalf("decode identify: %v", err)
	}

	// Intents: guild messages (1<<9) | message content (1<<15) | DM messages (1<<12)
	expectedIntents := (1 << 9) | (1 << 15) | (1 << 12)
	if payload.Intents != expectedIntents {
		t.Errorf("intents = %d, want %d", payload.Intents, expectedIntents)
	}
	if payload.Props.Browser != "aibutler" {
		t.Errorf("browser = %q, want aibutler", payload.Props.Browser)
	}
	if payload.Props.Device != "aibutler" {
		t.Errorf("device = %q, want aibutler", payload.Props.Device)
	}
}

func TestVerifyDiscordSignature(t *testing.T) {
	// Empty public key = always passes (backward compatible).
	if !discord.VerifyDiscordSignature([]byte("body"), "sig", "ts", "") {
		t.Error("empty public key should always pass")
	}

	// Generate a key pair and sign a message.
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	pubHex := hex.EncodeToString(pub)

	body := []byte(`{"type":1}`)
	timestamp := "1234567890"
	message := []byte(timestamp + string(body))
	sig := ed25519.Sign(priv, message)
	sigHex := hex.EncodeToString(sig)

	// Valid signature.
	if !discord.VerifyDiscordSignature(body, sigHex, timestamp, pubHex) {
		t.Error("expected valid signature to pass")
	}

	// Wrong body.
	if discord.VerifyDiscordSignature([]byte("wrong"), sigHex, timestamp, pubHex) {
		t.Error("expected wrong body to fail")
	}

	// Invalid hex signature.
	if discord.VerifyDiscordSignature(body, "not-hex", timestamp, pubHex) {
		t.Error("expected invalid hex signature to fail")
	}

	// Invalid hex public key.
	if discord.VerifyDiscordSignature(body, sigHex, timestamp, "not-hex") {
		t.Error("expected invalid hex public key to fail")
	}
}

func TestGracefulDisconnect(t *testing.T) {
	gw := newTestGateway(t, 30000)
	defer gw.close()

	api := &mockAPI{gatewayURL: gw.url()}
	a := discord.New(api)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := a.Start(ctx, func(_ context.Context, _ channel.Envelope) error { return nil }); err != nil {
		t.Fatal(err)
	}

	// Wait for identify so the connection is fully established.
	select {
	case <-gw.identifyRecv:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for identify")
	}

	if err := a.Stop(ctx); err != nil {
		t.Errorf("Stop() error: %v", err)
	}
}
