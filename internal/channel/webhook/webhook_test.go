package webhook_test

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/LumabyteCo/aibutler/internal/channel/webhook"
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
		auth := r.Header.Get("Authorization")
		if auth != "Bearer test-token" {
			t.Errorf("expected bearer auth, got %s", auth)
		}
		body, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(body), "user123") {
			t.Errorf("expected recipient in body, got %s", body)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	adapter := webhook.New(webhook.Config{
		OutboundURL: ts.URL,
		AuthType:    "bearer",
		AuthSecret:  "test-token",
	})
	adapter.SetHTTPClient(ts.Client())

	err := adapter.Send(context.Background(), "user123", "Hello Webhook!")
	if err != nil {
		t.Fatalf("Send: unexpected error: %v", err)
	}
}

func TestInboundHandler_ValidPost(t *testing.T) {
	adapter := webhook.New(webhook.Config{
		OutboundURL: "https://example.com/webhook",
		AuthType:    "none",
	})

	handler := adapter.InboundHandler()
	body := `{"text":"hello","sender":"user1","channel_id":"ch1"}`
	r := httptest.NewRequest(http.MethodPost, "/webhook/inbound", strings.NewReader(body))
	w := httptest.NewRecorder()
	handler(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}
}

func TestVerifyHMAC(t *testing.T) {
	secret := "my-secret-key"
	adapter := webhook.New(webhook.Config{
		OutboundURL: "https://example.com",
		AuthType:    "hmac",
		AuthSecret:  secret,
	})

	payload := []byte(`{"text":"test"}`)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	validSig := hex.EncodeToString(mac.Sum(nil))

	if !adapter.VerifyHMAC(payload, validSig) {
		t.Error("expected valid HMAC to pass verification")
	}
	if adapter.VerifyHMAC(payload, "invalid-signature") {
		t.Error("expected invalid HMAC to fail verification")
	}
}

func TestInboundHandler_BearerAuth(t *testing.T) {
	adapter := webhook.New(webhook.Config{
		OutboundURL: "https://example.com/webhook",
		AuthType:    "bearer",
		AuthSecret:  "secret123",
	})

	handler := adapter.InboundHandler()
	body := `{"text":"hello","sender":"user1","channel_id":"ch1"}`

	// Without auth header — should fail.
	r := httptest.NewRequest(http.MethodPost, "/webhook/inbound", strings.NewReader(body))
	w := httptest.NewRecorder()
	handler(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 without auth, got %d", w.Code)
	}

	// With correct auth header — should pass.
	r = httptest.NewRequest(http.MethodPost, "/webhook/inbound", strings.NewReader(body))
	r.Header.Set("Authorization", "Bearer secret123")
	w = httptest.NewRecorder()
	handler(w, r)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200 with auth, got %d", w.Code)
	}
}

func TestRegisterWebhookTools(t *testing.T) {
	reg := newMockRegistry()
	adapter := webhook.New(webhook.Config{
		OutboundURL: "https://example.com/webhook",
	})
	webhook.RegisterWebhookTools(reg, adapter)

	want := map[string]bool{"webhook.send": false}
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
