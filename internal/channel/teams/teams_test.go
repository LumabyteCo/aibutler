package teams_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/LumabyteCo/aibutler/internal/channel/teams"
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
		if !strings.Contains(r.URL.Path, "/v3/conversations/") {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		body, _ := io.ReadAll(r.Body)
		var payload map[string]interface{}
		json.Unmarshal(body, &payload)
		if payload["type"] != "message" {
			t.Errorf("expected type=message, got %v", payload["type"])
		}
		if payload["text"] != "Hello Teams!" {
			t.Errorf("expected text='Hello Teams!', got %v", payload["text"])
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"id":"msg123"}`))
	}))
	defer ts.Close()

	client := teams.NewClient("app-id", "app-password")
	client.SetBaseURL(ts.URL)
	client.SetHTTPClient(ts.Client())

	err := client.Send(context.Background(), "conv123", "Hello Teams!")
	if err != nil {
		t.Fatalf("Send: unexpected error: %v", err)
	}
}

func TestSendCard_Success(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var payload map[string]interface{}
		json.Unmarshal(body, &payload)
		attachments, ok := payload["attachments"].([]interface{})
		if !ok || len(attachments) == 0 {
			t.Error("expected attachments in payload")
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"id":"msg456"}`))
	}))
	defer ts.Close()

	client := teams.NewClient("app-id", "app-password")
	client.SetBaseURL(ts.URL)
	client.SetHTTPClient(ts.Client())

	card := map[string]interface{}{
		"type":    "AdaptiveCard",
		"version": "1.4",
		"body": []map[string]interface{}{
			{"type": "TextBlock", "text": "Hello from card"},
		},
	}
	err := client.SendCard(context.Background(), "conv123", card)
	if err != nil {
		t.Fatalf("SendCard: unexpected error: %v", err)
	}
}

func TestParseWebhook_Valid(t *testing.T) {
	body := `{
		"type": "message",
		"id": "msg789",
		"text": "Hello from Teams",
		"conversation": {"id": "conv456"},
		"from": {"id": "user123", "name": "Test User"}
	}`
	r := httptest.NewRequest(http.MethodPost, "/api/messages", strings.NewReader(body))

	client := teams.NewClient("app-id", "app-password")
	msg, err := client.ParseWebhook(r)
	if err != nil {
		t.Fatalf("ParseWebhook: unexpected error: %v", err)
	}
	if msg == nil {
		t.Fatal("ParseWebhook: expected non-nil message")
	}
	if msg.From != "user123" {
		t.Errorf("expected From=user123, got %s", msg.From)
	}
	if msg.Text != "Hello from Teams" {
		t.Errorf("expected Text='Hello from Teams', got %s", msg.Text)
	}
	if msg.ConversationID != "conv456" {
		t.Errorf("expected ConversationID=conv456, got %s", msg.ConversationID)
	}
	if msg.MessageID != "msg789" {
		t.Errorf("expected MessageID=msg789, got %s", msg.MessageID)
	}
}

func TestRegisterTeamsTools(t *testing.T) {
	reg := newMockRegistry()
	client := teams.NewClient("app-id", "app-password")
	teams.RegisterTeamsTools(reg, client)

	want := map[string]bool{"teams.send": false, "teams.send_card": false}
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
