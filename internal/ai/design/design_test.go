package design_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/LumabyteCo/aibutler/internal/ai/design"
)

// mockRegistry captures tool registrations for verification.
type mockRegistry struct {
	tools map[string]bool
}

func (m *mockRegistry) Register(name, description, schema, capability string, exec func(ctx context.Context, input string) (string, error)) {
	m.tools[name] = true
}

func TestRegisterDesignToolsCanva(t *testing.T) {
	reg := &mockRegistry{tools: make(map[string]bool)}
	canva := design.NewCanva("test-key")
	figma := design.NewFigma("test-key")
	design.RegisterDesignTools(reg, canva, figma)

	if !reg.tools["design.generate.canva"] {
		t.Error("expected design.generate.canva to be registered")
	}
}

func TestRegisterDesignToolsFigma(t *testing.T) {
	reg := &mockRegistry{tools: make(map[string]bool)}
	canva := design.NewCanva("test-key")
	figma := design.NewFigma("test-key")
	design.RegisterDesignTools(reg, canva, figma)

	if !reg.tools["design.generate.figma"] {
		t.Error("expected design.generate.figma to be registered")
	}
}

func TestCanvaGenerateNoKey(t *testing.T) {
	canva := design.NewCanva("")
	_, err := canva.Generate(context.Background(), "test prompt", "")
	if err == nil || !strings.Contains(err.Error(), "not configured") {
		t.Errorf("expected not-configured error, got: %v", err)
	}
}

// rewriteTransport rewrites request URLs to point at the test server.
type rewriteTransport struct {
	inner http.RoundTripper
	tsURL string
}

func (rt *rewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req.URL.Scheme = "http"
	req.URL.Host = strings.TrimPrefix(rt.tsURL, "http://")
	return rt.inner.RoundTrip(req)
}

func redirectClient(ts *httptest.Server) *http.Client {
	return &http.Client{Transport: &rewriteTransport{inner: ts.Client().Transport, tsURL: ts.URL}}
}

func TestCanvaGenerateWithKey(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if !strings.Contains(r.Header.Get("Authorization"), "Bearer test-canva-key") {
			t.Error("expected Bearer token in Authorization header")
		}
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"design_id": "d123",
			"url":       "https://canva.com/design/d123",
		})
	}))
	defer ts.Close()

	canva := design.NewCanva("test-canva-key")
	canva.SetHTTPClient(redirectClient(ts))

	result, err := canva.Generate(context.Background(), "logo design", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "canva") {
		t.Error("result should contain provider name")
	}
}

func TestFigmaGenerateWithKey(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"file_key": "f456",
			"url":      "https://figma.com/file/f456",
		})
	}))
	defer ts.Close()

	figma := design.NewFigma("test-figma-key")
	figma.SetHTTPClient(redirectClient(ts))

	result, err := figma.GenerateMockup(context.Background(), "dashboard mockup")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "figma") {
		t.Error("result should contain provider name")
	}
}
