package webchat

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestStartInternet_RefuseWithoutTLS(t *testing.T) {
	adapter := New(DefaultConfig())
	err := adapter.StartInternet(context.Background(), nil, InternetConfig{
		Enabled: true,
		// No cert/key provided.
	})
	if err == nil {
		t.Fatal("expected error when TLS cert/key not provided")
	}
	if got := err.Error(); got != "webchat: internet mode requires TLS cert and key files" {
		t.Fatalf("unexpected error: %s", got)
	}
}

func TestStartInternet_RefuseWhenDisabled(t *testing.T) {
	adapter := New(DefaultConfig())
	err := adapter.StartInternet(context.Background(), nil, InternetConfig{
		Enabled: false,
	})
	if err == nil {
		t.Fatal("expected error when internet mode is disabled")
	}
}

func TestIPAllowlist(t *testing.T) {
	// Create allowlist middleware that only allows 10.0.0.0/8.
	mw, err := newIPAllowlistMiddleware([]string{"10.0.0.0/8"})
	if err != nil {
		t.Fatalf("create middleware: %v", err)
	}

	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := mw(inner)

	// Allowed IP.
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "10.1.2.3:1234"
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for allowed IP, got %d", w.Code)
	}

	// Denied IP.
	req2 := httptest.NewRequest("GET", "/", nil)
	req2.RemoteAddr = "192.168.1.1:1234"
	w2 := httptest.NewRecorder()
	handler.ServeHTTP(w2, req2)
	if w2.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for denied IP, got %d", w2.Code)
	}
}

func TestIPAllowlist_SingleIP(t *testing.T) {
	mw, err := newIPAllowlistMiddleware([]string{"192.168.1.100"})
	if err != nil {
		t.Fatalf("create middleware: %v", err)
	}

	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := mw(inner)

	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "192.168.1.100:5000"
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for exact IP match, got %d", w.Code)
	}
}

func TestExtractClientIP_XForwardedFor(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("X-Forwarded-For", "203.0.113.50, 70.41.3.18")
	req.RemoteAddr = "127.0.0.1:1234"

	ip := extractClientIP(req)
	if ip != "203.0.113.50" {
		t.Fatalf("expected 203.0.113.50, got %s", ip)
	}
}

func TestReverseProxyMiddleware_CORS(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := reverseProxyMiddleware(inner)

	// Request with Origin header.
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Origin", "https://example.com")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Header().Get("Access-Control-Allow-Origin") != "https://example.com" {
		t.Fatal("expected CORS origin header")
	}
}

func TestReverseProxyMiddleware_Preflight(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("inner handler should not be called for preflight")
	})

	handler := reverseProxyMiddleware(inner)

	req := httptest.NewRequest("OPTIONS", "/", nil)
	req.Header.Set("Origin", "https://example.com")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204 for preflight, got %d", w.Code)
	}
}

// Ensure net import is used.
var _ = net.IPv4
