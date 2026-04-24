package whatsapp_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/LumabyteCo/aibutler/internal/channel/whatsapp"
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
		if payload["type"] != "text" {
			t.Errorf("expected type=text, got %v", payload["type"])
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"messages":[{"id":"wamid.test"}]}`))
	}))
	defer ts.Close()

	client := whatsapp.NewClient("12345", "test-token")
	client.SetBaseURL(ts.URL)
	client.SetHTTPClient(ts.Client())

	err := client.Send(context.Background(), "15551234567", "Hello!")
	if err != nil {
		t.Fatalf("Send: unexpected error: %v", err)
	}
}

func TestParseWebhook_Valid(t *testing.T) {
	body := `{
		"object": "whatsapp_business_account",
		"entry": [{
			"changes": [{
				"value": {
					"messages": [{
						"id": "wamid.test123",
						"from": "15551234567",
						"type": "text",
						"text": {"body": "Hello from WhatsApp"}
					}]
				}
			}]
		}]
	}`
	r := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(body))
	msg, err := whatsapp.ParseWebhook(r)
	if err != nil {
		t.Fatalf("ParseWebhook: unexpected error: %v", err)
	}
	if msg == nil {
		t.Fatal("ParseWebhook: expected non-nil message")
	}
	if msg.From != "15551234567" {
		t.Errorf("expected From=15551234567, got %s", msg.From)
	}
	if msg.Body != "Hello from WhatsApp" {
		t.Errorf("expected Body='Hello from WhatsApp', got %s", msg.Body)
	}
	if msg.MessageID != "wamid.test123" {
		t.Errorf("expected MessageID=wamid.test123, got %s", msg.MessageID)
	}
}

func TestParseWebhook_Invalid(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader("not-json"))
	_, err := whatsapp.ParseWebhook(r)
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestRegisterWhatsAppTools(t *testing.T) {
	reg := newMockRegistry()
	client := whatsapp.NewClient("12345", "test-token")
	whatsapp.RegisterWhatsAppTools(reg, client)

	want := map[string]bool{"whatsapp.send": false, "whatsapp.send_template": false}
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

func TestSendTemplate_Success(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var payload map[string]interface{}
		json.Unmarshal(body, &payload)
		if payload["type"] != "template" {
			t.Errorf("expected type=template, got %v", payload["type"])
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"messages":[{"id":"wamid.tmpl"}]}`))
	}))
	defer ts.Close()

	client := whatsapp.NewClient("12345", "test-token")
	client.SetBaseURL(ts.URL)
	client.SetHTTPClient(ts.Client())

	err := client.SendTemplate(context.Background(), "15551234567", "hello_world", nil)
	if err != nil {
		t.Fatalf("SendTemplate: unexpected error: %v", err)
	}
}
