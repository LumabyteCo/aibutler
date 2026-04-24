package slack

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/LumabyteCo/aibutler/internal/channel"
	"nhooyr.io/websocket"
)

// ---------------------------------------------------------------------------
// Mock API client
// ---------------------------------------------------------------------------

type postCall struct {
	channel, text, threadTS string
}

type updateCall struct {
	channel, ts, text string
}

type mockAPI struct {
	wsURL       string
	postCalls   []postCall
	updateCalls []updateCall
	mu          sync.Mutex
}

func (m *mockAPI) PostMessage(_ context.Context, ch, text, threadTS string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.postCalls = append(m.postCalls, postCall{channel: ch, text: text, threadTS: threadTS})
	return "1234567890.123456", nil
}

func (m *mockAPI) UpdateMessage(_ context.Context, ch, ts, text string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.updateCalls = append(m.updateCalls, updateCall{channel: ch, ts: ts, text: text})
	return nil
}

func (m *mockAPI) GetWSURL(_ context.Context) (string, error) {
	return m.wsURL, nil
}

// ---------------------------------------------------------------------------
// Test WebSocket server helpers
// ---------------------------------------------------------------------------

// socketModeServer creates an httptest server that speaks Socket Mode.
// It accepts a WebSocket connection and sends events via the returned channel.
// It also collects any messages written back by the client (e.g. acks).
func socketModeServer(t *testing.T) (srv *httptest.Server, sendEvent func(data []byte), clientMsgs func() []json.RawMessage, done func()) {
	t.Helper()

	var (
		mu       sync.Mutex
		received []json.RawMessage
		conn     *websocket.Conn
		connReady = make(chan struct{})
	)

	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, &websocket.AcceptOptions{
			InsecureSkipVerify: true,
		})
		if err != nil {
			t.Logf("ws accept error: %v", err)
			return
		}
		mu.Lock()
		conn = c
		mu.Unlock()
		close(connReady)

		// Read loop: collect client messages (acks, etc.)
		for {
			_, data, err := c.Read(r.Context())
			if err != nil {
				return
			}
			mu.Lock()
			received = append(received, json.RawMessage(data))
			mu.Unlock()
		}
	}))

	sendEvent = func(data []byte) {
		<-connReady
		mu.Lock()
		c := conn
		mu.Unlock()
		if c != nil {
			c.Write(context.Background(), websocket.MessageText, data)
		}
	}

	clientMsgs = func() []json.RawMessage {
		mu.Lock()
		defer mu.Unlock()
		out := make([]json.RawMessage, len(received))
		copy(out, received)
		return out
	}

	done = func() {
		mu.Lock()
		c := conn
		mu.Unlock()
		if c != nil {
			c.Close(websocket.StatusNormalClosure, "test done")
		}
		srv.Close()
	}

	return srv, sendEvent, clientMsgs, done
}

// wsURL converts an httptest server URL to a ws:// URL.
func wsURL(srv *httptest.Server) string {
	return "ws" + strings.TrimPrefix(srv.URL, "http")
}

// makeSocketModeEvent builds a Socket Mode envelope wrapping an events_api message event.
func makeSocketModeEvent(envelopeID, user, ch, text, threadTS, ts string) []byte {
	inner := map[string]interface{}{
		"event": map[string]interface{}{
			"type":      "message",
			"text":      text,
			"user":      user,
			"channel":   ch,
			"thread_ts": threadTS,
			"ts":        ts,
		},
	}
	payload, _ := json.Marshal(inner)
	envelope := map[string]interface{}{
		"envelope_id": envelopeID,
		"type":        "events_api",
		"payload":     json.RawMessage(payload),
	}
	data, _ := json.Marshal(envelope)
	return data
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestSlackName(t *testing.T) {
	a := New(&mockAPI{})
	if a.Name() != "slack" {
		t.Errorf("Name() = %q, want %q", a.Name(), "slack")
	}
}

func TestNormalizeTextEvent(t *testing.T) {
	srv, sendEvent, _, done := socketModeServer(t)
	defer done()

	api := &mockAPI{wsURL: wsURL(srv)}

	received := make(chan channel.Envelope, 1)
	handler := func(_ context.Context, env channel.Envelope) error {
		received <- env
		return nil
	}

	a := New(api)
	ctx := context.Background()
	if err := a.Start(ctx, handler); err != nil {
		t.Fatal(err)
	}
	defer a.Stop(ctx)

	// Send a text message event.
	event := makeSocketModeEvent("env-1", "U123", "C456", "hello world", "", "1700000001.000001")
	sendEvent(event)

	select {
	case env := <-received:
		if env.ID != "1700000001.000001" {
			t.Errorf("ID = %q, want %q", env.ID, "1700000001.000001")
		}
		if env.Channel != "slack" {
			t.Errorf("Channel = %q, want %q", env.Channel, "slack")
		}
		if env.AccountID != "U123" {
			t.Errorf("AccountID = %q, want %q", env.AccountID, "U123")
		}
		if env.Text != "hello world" {
			t.Errorf("Text = %q, want %q", env.Text, "hello world")
		}
		if env.Type != channel.TypeText {
			t.Errorf("Type = %q, want %q", env.Type, channel.TypeText)
		}
		// When no thread_ts, ThreadID should fall back to ts.
		if env.ThreadID != "1700000001.000001" {
			t.Errorf("ThreadID = %q, want %q", env.ThreadID, "1700000001.000001")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for envelope")
	}
}

func TestNormalizeThreadEvent(t *testing.T) {
	srv, sendEvent, _, done := socketModeServer(t)
	defer done()

	api := &mockAPI{wsURL: wsURL(srv)}

	received := make(chan channel.Envelope, 1)
	handler := func(_ context.Context, env channel.Envelope) error {
		received <- env
		return nil
	}

	a := New(api)
	ctx := context.Background()
	if err := a.Start(ctx, handler); err != nil {
		t.Fatal(err)
	}
	defer a.Stop(ctx)

	// Send a threaded message event.
	event := makeSocketModeEvent("env-2", "U789", "C456", "thread reply",
		"1700000000.000000", "1700000002.000002")
	sendEvent(event)

	select {
	case env := <-received:
		if env.ThreadID != "1700000000.000000" {
			t.Errorf("ThreadID = %q, want %q", env.ThreadID, "1700000000.000000")
		}
		if env.ID != "1700000002.000002" {
			t.Errorf("ID = %q, want %q", env.ID, "1700000002.000002")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for threaded envelope")
	}
}

func TestSendMessage(t *testing.T) {
	api := &mockAPI{}
	a := New(api)

	// Seed the session map so Send can resolve the channel.
	a.mu.Lock()
	a.sessions["U123"] = "C456"
	a.mu.Unlock()

	ctx := context.Background()
	err := a.Send(ctx, "U123", channel.OutgoingMessage{Text: "hi there"})
	if err != nil {
		t.Fatal(err)
	}

	api.mu.Lock()
	defer api.mu.Unlock()
	if len(api.postCalls) != 1 {
		t.Fatalf("postCalls = %d, want 1", len(api.postCalls))
	}
	call := api.postCalls[0]
	if call.channel != "C456" {
		t.Errorf("channel = %q, want %q", call.channel, "C456")
	}
	if call.text != "hi there" {
		t.Errorf("text = %q, want %q", call.text, "hi there")
	}
	if call.threadTS != "" {
		t.Errorf("threadTS = %q, want empty", call.threadTS)
	}
}

func TestSendMessageInThread(t *testing.T) {
	api := &mockAPI{}
	a := New(api)

	a.mu.Lock()
	a.sessions["U123"] = "C456"
	a.mu.Unlock()

	ctx := context.Background()
	err := a.Send(ctx, "U123", channel.OutgoingMessage{
		Text:    "thread reply",
		ReplyTo: "1700000000.000000",
	})
	if err != nil {
		t.Fatal(err)
	}

	api.mu.Lock()
	defer api.mu.Unlock()
	if len(api.postCalls) != 1 {
		t.Fatalf("postCalls = %d, want 1", len(api.postCalls))
	}
	call := api.postCalls[0]
	if call.threadTS != "1700000000.000000" {
		t.Errorf("threadTS = %q, want %q", call.threadTS, "1700000000.000000")
	}
}

func TestUpdateMessage(t *testing.T) {
	api := &mockAPI{}
	a := New(api)

	a.mu.Lock()
	a.sessions["U123"] = "C456"
	a.mu.Unlock()

	ctx := context.Background()
	err := a.Send(ctx, "U123", channel.OutgoingMessage{
		Text:   "edited text",
		EditID: "1700000001.000001",
	})
	if err != nil {
		t.Fatal(err)
	}

	api.mu.Lock()
	defer api.mu.Unlock()
	if len(api.updateCalls) != 1 {
		t.Fatalf("updateCalls = %d, want 1", len(api.updateCalls))
	}
	call := api.updateCalls[0]
	if call.channel != "C456" {
		t.Errorf("channel = %q, want %q", call.channel, "C456")
	}
	if call.ts != "1700000001.000001" {
		t.Errorf("ts = %q, want %q", call.ts, "1700000001.000001")
	}
	if call.text != "edited text" {
		t.Errorf("text = %q, want %q", call.text, "edited text")
	}
	// PostMessage should NOT have been called.
	if len(api.postCalls) != 0 {
		t.Errorf("postCalls = %d, want 0", len(api.postCalls))
	}
}

func TestSendTyping(t *testing.T) {
	a := New(&mockAPI{})
	err := a.SendTyping(context.Background(), "U123")
	if err != nil {
		t.Errorf("SendTyping returned error: %v", err)
	}
}

func TestSocketModeAcknowledge(t *testing.T) {
	srv, sendEvent, clientMsgs, done := socketModeServer(t)
	defer done()

	api := &mockAPI{wsURL: wsURL(srv)}

	handler := func(_ context.Context, _ channel.Envelope) error { return nil }
	a := New(api)
	ctx := context.Background()
	if err := a.Start(ctx, handler); err != nil {
		t.Fatal(err)
	}
	defer a.Stop(ctx)

	// Send an event with an envelope_id.
	event := makeSocketModeEvent("ack-test-id", "U111", "C222", "ack me", "", "1700000003.000003")
	sendEvent(event)

	// Wait for the ack to arrive.
	deadline := time.After(3 * time.Second)
	for {
		msgs := clientMsgs()
		for _, raw := range msgs {
			var ack struct {
				EnvelopeID string `json:"envelope_id"`
			}
			if err := json.Unmarshal(raw, &ack); err == nil && ack.EnvelopeID == "ack-test-id" {
				return // success
			}
		}

		select {
		case <-deadline:
			t.Fatal("timeout waiting for ack with envelope_id")
		case <-time.After(50 * time.Millisecond):
			// poll again
		}
	}
}

func TestGracefulDisconnect(t *testing.T) {
	srv, _, _, done := socketModeServer(t)
	defer done()

	api := &mockAPI{wsURL: wsURL(srv)}

	handler := func(_ context.Context, _ channel.Envelope) error { return nil }
	a := New(api)
	ctx := context.Background()
	if err := a.Start(ctx, handler); err != nil {
		t.Fatal(err)
	}

	// Stop should complete without error.
	if err := a.Stop(ctx); err != nil {
		t.Errorf("Stop returned error: %v", err)
	}
}

func TestSendToUnknownChannel(t *testing.T) {
	a := New(&mockAPI{})
	err := a.Send(context.Background(), "UNKNOWN_USER", channel.OutgoingMessage{Text: "hi"})
	if err == nil {
		t.Fatal("expected error for unknown account, got nil")
	}
	want := `no channel for account "UNKNOWN_USER"`
	if !strings.Contains(err.Error(), want) {
		t.Errorf("error = %q, want to contain %q", err.Error(), want)
	}
}

func TestBotMessageIgnored(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, &websocket.AcceptOptions{
			InsecureSkipVerify: true,
		})
		if err != nil {
			return
		}
		defer c.Close(websocket.StatusNormalClosure, "")

		// Send a bot message (has bot_id set).
		inner := map[string]interface{}{
			"event": map[string]interface{}{
				"type":    "message",
				"text":    "bot says hi",
				"user":    "U999",
				"channel": "C999",
				"ts":      "1700000005.000005",
				"bot_id":  "B123",
			},
		}
		payload, _ := json.Marshal(inner)
		envelope := map[string]interface{}{
			"envelope_id": "env-bot",
			"type":        "events_api",
			"payload":     json.RawMessage(payload),
		}
		data, _ := json.Marshal(envelope)
		c.Write(r.Context(), websocket.MessageText, data)

		// Keep connection alive for a bit.
		<-r.Context().Done()
	}))
	defer srv.Close()

	api := &mockAPI{wsURL: wsURL(srv)}

	called := make(chan struct{}, 1)
	handler := func(_ context.Context, _ channel.Envelope) error {
		called <- struct{}{}
		return nil
	}

	a := New(api)
	ctx := context.Background()
	if err := a.Start(ctx, handler); err != nil {
		t.Fatal(err)
	}
	defer a.Stop(ctx)

	select {
	case <-called:
		t.Error("handler should NOT be called for bot messages")
	case <-time.After(500 * time.Millisecond):
		// Expected: handler was not called.
	}
}

// Verify interface compliance at test time.
var _ channel.Channel = (*Adapter)(nil)

// Verify mock satisfies APIClient.
var _ APIClient = (*mockAPI)(nil)

func TestVerifySlackSignature(t *testing.T) {
	// Empty signing secret = always passes (backward compatible).
	if !VerifySlackSignature([]byte("body"), "123", "sig", "") {
		t.Error("empty signing secret should always pass")
	}

	// Compute a valid signature.
	body := []byte(`{"event":"test"}`)
	timestamp := "1531420618"
	signingSecret := "test-signing-secret"
	baseString := "v0:" + timestamp + ":" + string(body)
	mac := hmac.New(sha256.New, []byte(signingSecret))
	mac.Write([]byte(baseString))
	expected := "v0=" + hex.EncodeToString(mac.Sum(nil))

	if !VerifySlackSignature(body, timestamp, expected, signingSecret) {
		t.Error("expected valid signature to pass")
	}

	// Wrong signature.
	if VerifySlackSignature(body, timestamp, "v0=bad", signingSecret) {
		t.Error("expected invalid signature to fail")
	}

	// Wrong body.
	if VerifySlackSignature([]byte("wrong body"), timestamp, expected, signingSecret) {
		t.Error("expected mismatched body to fail")
	}
}

func TestInterfaceCompliance(t *testing.T) {
	// This test exists to confirm compile-time interface checks above.
	var c channel.Channel = New(&mockAPI{})
	if c.Name() != "slack" {
		t.Errorf("interface check: Name() = %q, want slack", c.Name())
	}
}
