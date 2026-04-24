package browser_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/LumabyteCo/aibutler/internal/browser"
)

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

func TestNavigate(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(`<html><head><title>Test Page</title></head><body><p>Hello world</p></body></html>`))
	}))
	defer ts.Close()

	c := browser.NewClient()
	c.SetHTTPClient(ts.Client())

	title, text, err := c.Navigate(context.Background(), ts.URL)
	if err != nil {
		t.Fatalf("Navigate: unexpected error: %v", err)
	}
	if title != "Test Page" {
		t.Errorf("expected title='Test Page', got %q", title)
	}
	if !strings.Contains(text, "Hello world") {
		t.Errorf("expected text to contain 'Hello world', got %q", text)
	}
}

func TestExtractLinks(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(`<html><body>
			<a href="https://example.com/page1">Link 1</a>
			<a href="/relative">Relative</a>
			<a href="#anchor">Anchor</a>
		</body></html>`))
	}))
	defer ts.Close()

	c := browser.NewClient()
	c.SetHTTPClient(ts.Client())

	links, err := c.ExtractLinks(context.Background(), ts.URL)
	if err != nil {
		t.Fatalf("ExtractLinks: unexpected error: %v", err)
	}

	// Should have example.com/page1 and relative (not anchor).
	if len(links) < 2 {
		t.Errorf("expected at least 2 links, got %d: %v", len(links), links)
	}

	found := false
	for _, l := range links {
		if l == "https://example.com/page1" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected https://example.com/page1 in links, got %v", links)
	}
}

func TestScreenshot_ReturnsStub(t *testing.T) {
	c := browser.NewClient()
	msg, err := c.Screenshot(context.Background(), "https://example.com")
	if err != nil {
		t.Fatalf("Screenshot: unexpected error: %v", err)
	}
	if !strings.Contains(msg, "headless") {
		t.Errorf("expected headless message, got %q", msg)
	}
}

func TestRegisterBrowserTools(t *testing.T) {
	reg := newMockRegistry()
	c := browser.NewClient()
	browser.RegisterBrowserTools(reg, c)

	want := map[string]bool{
		"browser.navigate":      false,
		"browser.extract_links": false,
		"browser.screenshot":    false,
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

func TestNavigateTool_Execute(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(`<html><head><title>Tool Test</title></head><body>Tool content</body></html>`))
	}))
	defer ts.Close()

	reg := newMockRegistry()
	c := browser.NewClient()
	c.SetHTTPClient(ts.Client())
	browser.RegisterBrowserTools(reg, c)

	navExec := reg.exec["browser.navigate"]
	if navExec == nil {
		t.Fatal("browser.navigate not registered")
	}

	input, _ := json.Marshal(map[string]string{"url": ts.URL})
	out, err := navExec(context.Background(), string(input))
	if err != nil {
		t.Fatalf("browser.navigate exec: %v", err)
	}
	if !strings.Contains(out, "Tool Test") {
		t.Errorf("expected title in output, got %q", out)
	}
}
