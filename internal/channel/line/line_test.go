package line_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/LumabyteCo/aibutler/internal/channel/line"
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

func TestSend_Success(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if !strings.Contains(r.URL.Path, "/message/push") {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		body, _ := io.ReadAll(r.Body)
		var payload map[string]interface{}
		json.Unmarshal(body, &payload)
		if payload["to"] != "U1234567890" {
			t.Errorf("expected to=U1234567890, got %v", payload["to"])
		}
		msgs, ok := payload["messages"].([]interface{})
		if !ok || len(msgs) == 0 {
			t.Fatal("expected non-empty messages array")
		}
		msg := msgs[0].(map[string]interface{})
		if msg["type"] != "text" {
			t.Errorf("expected type=text, got %v", msg["type"])
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{}`))
	}))
	defer ts.Close()

	client := line.NewClient("secret", "token")
	client.SetBaseURL(ts.URL)
	client.SetHTTPClient(ts.Client())

	err := client.Send(context.Background(), "U1234567890", "Hello LINE!")
	if err != nil {
		t.Fatalf("Send: unexpected error: %v", err)
	}
}

func TestSendFlexMessage_Success(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var payload map[string]interface{}
		json.Unmarshal(body, &payload)
		msgs := payload["messages"].([]interface{})
		msg := msgs[0].(map[string]interface{})
		if msg["type"] != "flex" {
			t.Errorf("expected type=flex, got %v", msg["type"])
		}
		if msg["altText"] != "Test flex" {
			t.Errorf("expected altText='Test flex', got %v", msg["altText"])
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{}`))
	}))
	defer ts.Close()

	client := line.NewClient("secret", "token")
	client.SetBaseURL(ts.URL)
	client.SetHTTPClient(ts.Client())

	contents := map[string]interface{}{
		"type": "bubble",
		"body": map[string]interface{}{
			"type": "box",
			"layout": "vertical",
		},
	}
	err := client.SendFlexMessage(context.Background(), "U1234567890", "Test flex", contents)
	if err != nil {
		t.Fatalf("SendFlexMessage: unexpected error: %v", err)
	}
}

func TestParseWebhook_Valid(t *testing.T) {
	body := `{
		"events": [{
			"type": "message",
			"replyToken": "reply123",
			"source": {"type": "user", "userId": "U1234567890"},
			"message": {"id": "msg001", "type": "text", "text": "Hello from LINE"}
		}]
	}`
	r := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(body))
	msg, err := line.ParseWebhook(r)
	if err != nil {
		t.Fatalf("ParseWebhook: unexpected error: %v", err)
	}
	if msg == nil {
		t.Fatal("ParseWebhook: expected non-nil message")
	}
	if msg.UserID != "U1234567890" {
		t.Errorf("expected UserID=U1234567890, got %s", msg.UserID)
	}
	if msg.Text != "Hello from LINE" {
		t.Errorf("expected Text='Hello from LINE', got %s", msg.Text)
	}
	if msg.ReplyToken != "reply123" {
		t.Errorf("expected ReplyToken=reply123, got %s", msg.ReplyToken)
	}
	if msg.MessageID != "msg001" {
		t.Errorf("expected MessageID=msg001, got %s", msg.MessageID)
	}
}

func TestRegisterLINETools(t *testing.T) {
	reg := newMockRegistry()
	client := line.NewClient("secret", "token")
	line.RegisterLINETools(reg, client)

	want := map[string]bool{
		"line.send":             false,
		"line.send_flex":        false,
		"line.send_quick_reply": false,
	}
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
