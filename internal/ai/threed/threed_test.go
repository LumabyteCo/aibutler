package threed_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/LumabyteCo/aibutler/internal/ai/threed"
)

type mockRegistry struct {
	tools map[string]bool
}

func (m *mockRegistry) Register(name, description, schema, capability string, exec func(ctx context.Context, input string) (string, error)) {
	m.tools[name] = true
}

func TestRegisterThreeDToolsMeshy(t *testing.T) {
	reg := &mockRegistry{tools: make(map[string]bool)}
	meshy := threed.NewMeshy("test-key")
	tripo := threed.NewTripo("test-key")
	luma := threed.NewLuma("test-key")
	threed.RegisterThreeDTools(reg, meshy, tripo, luma)

	if !reg.tools["3d.generate.meshy"] {
		t.Error("expected 3d.generate.meshy to be registered")
	}
}

func TestRegisterThreeDToolsCount(t *testing.T) {
	reg := &mockRegistry{tools: make(map[string]bool)}
	meshy := threed.NewMeshy("test-key")
	tripo := threed.NewTripo("test-key")
	luma := threed.NewLuma("test-key")
	threed.RegisterThreeDTools(reg, meshy, tripo, luma)

	if len(reg.tools) != 3 {
		t.Errorf("tool count = %d, want 3", len(reg.tools))
	}
}

func TestMeshyGenerateNoKey(t *testing.T) {
	meshy := threed.NewMeshy("")
	_, err := meshy.Generate(context.Background(), "test prompt")
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

func TestMeshyGenerateWithKey(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if !strings.Contains(r.Header.Get("Authorization"), "Bearer test-meshy-key") {
			t.Error("expected Bearer token in Authorization header")
		}
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"result": "model-id-123",
		})
	}))
	defer ts.Close()

	meshy := threed.NewMeshy("test-meshy-key")
	meshy.SetHTTPClient(redirectClient(ts))

	result, err := meshy.Generate(context.Background(), "a red car")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "meshy") {
		t.Error("result should contain provider name")
	}
}

func TestTripoGenerateWithKey(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"task_id": "t456",
		})
	}))
	defer ts.Close()

	tripo := threed.NewTripo("test-tripo-key")
	tripo.SetHTTPClient(redirectClient(ts))

	result, err := tripo.Generate(context.Background(), "a blue chair")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "tripo") {
		t.Error("result should contain provider name")
	}
}

func TestLumaGenerateWithKey(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"generation_id": "g789",
		})
	}))
	defer ts.Close()

	luma := threed.NewLuma("test-luma-key")
	luma.SetHTTPClient(redirectClient(ts))

	result, err := luma.Generate(context.Background(), "a green tree")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "luma") {
		t.Error("result should contain provider name")
	}
}
