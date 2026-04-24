package gchat_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/LumabyteCo/aibutler/internal/channel/gchat"
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
		if !strings.Contains(r.URL.Path, "/messages") {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		body, _ := io.ReadAll(r.Body)
		var payload map[string]interface{}
		json.Unmarshal(body, &payload)
		if payload["text"] != "Hello Chat!" {
			t.Errorf("expected text='Hello Chat!', got %v", payload["text"])
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"name":"spaces/AAA/messages/BBB"}`))
	}))
	defer ts.Close()

	client := gchat.NewClient([]byte(`{"type":"service_account"}`))
	client.SetBaseURL(ts.URL)
	client.SetHTTPClient(ts.Client())

	err := client.Send(context.Background(), "spaces/AAA", "Hello Chat!")
	if err != nil {
		t.Fatalf("Send: unexpected error: %v", err)
	}
}

func TestSendCard_Success(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var payload map[string]interface{}
		json.Unmarshal(body, &payload)
		cards, ok := payload["cardsV2"].([]interface{})
		if !ok || len(cards) == 0 {
			t.Error("expected cardsV2 in payload")
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"name":"spaces/AAA/messages/CCC"}`))
	}))
	defer ts.Close()

	client := gchat.NewClient([]byte(`{"type":"service_account"}`))
	client.SetBaseURL(ts.URL)
	client.SetHTTPClient(ts.Client())

	card := map[string]interface{}{
		"header": map[string]interface{}{
			"title": "Test Card",
		},
	}
	err := client.SendCard(context.Background(), "spaces/AAA", card)
	if err != nil {
		t.Fatalf("SendCard: unexpected error: %v", err)
	}
}

func TestParseWebhook_Valid(t *testing.T) {
	body := `{
		"type": "MESSAGE",
		"message": {
			"name": "spaces/AAA/messages/BBB",
			"sender": {"name": "users/123", "displayName": "Test User"},
			"text": "Hello from Google Chat"
		},
		"space": {"name": "spaces/AAA"}
	}`
	r := httptest.NewRequest(http.MethodPost, "/gchat/webhook", strings.NewReader(body))

	client := gchat.NewClient([]byte(`{"type":"service_account"}`))
	msg, err := client.ParseWebhook(r)
	if err != nil {
		t.Fatalf("ParseWebhook: unexpected error: %v", err)
	}
	if msg == nil {
		t.Fatal("ParseWebhook: expected non-nil message")
	}
	if msg.From != "users/123" {
		t.Errorf("expected From=users/123, got %s", msg.From)
	}
	if msg.Text != "Hello from Google Chat" {
		t.Errorf("expected Text='Hello from Google Chat', got %s", msg.Text)
	}
	if msg.SpaceName != "spaces/AAA" {
		t.Errorf("expected SpaceName=spaces/AAA, got %s", msg.SpaceName)
	}
}

func TestRegisterGChatTools(t *testing.T) {
	reg := newMockRegistry()
	client := gchat.NewClient([]byte(`{"type":"service_account"}`))
	gchat.RegisterGChatTools(reg, client)

	want := map[string]bool{"gchat.send": false, "gchat.send_card": false}
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
