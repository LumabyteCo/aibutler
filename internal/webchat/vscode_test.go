package webchat_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/LumabyteCo/aibutler/internal/webchat"
)

// fakeRunner records the prompt and returns a canned output.
type fakeRunner struct {
	lastPrompt  string
	lastChannel string
	output      string
	err         error
}

func (f *fakeRunner) Run(_ context.Context, _, task, channel string) (string, error) {
	f.lastPrompt = task
	f.lastChannel = channel
	return f.output, f.err
}

func newVSCodeServer(t *testing.T, r *fakeRunner) *httptest.Server {
	t.Helper()
	h := &webchat.VSCodeHandler{}
	h.SetRunner(r)
	return httptest.NewServer(h)
}

// TestVSCodeRouteContract guards the editor-extension contract (D12):
// /api/vscode/{ask,explain,fix,tests} accept the extension's JSON payload
// shape and return {"output": "..."}. Before this handler existed, every
// extension command 404'd.
func TestVSCodeRouteContract(t *testing.T) {
	runner := &fakeRunner{output: "here is your answer"}
	srv := newVSCodeServer(t, runner)
	defer srv.Close()

	cases := []struct {
		action string
		body   string
	}{
		{"ask", `{"message":"what is a closure?"}`},
		{"explain", `{"code":"func main() {}","language":"go"}`},
		{"fix", `{"code":"print('hi'","language":"python"}`},
		{"tests", `{"code":"func Add(a,b int) int {return a+b}","language":"go","path":"add.go"}`},
	}

	for _, tc := range cases {
		t.Run(tc.action, func(t *testing.T) {
			resp, err := http.Post(srv.URL+"/api/vscode/"+tc.action,
				"application/json", strings.NewReader(tc.body))
			if err != nil {
				t.Fatalf("post: %v", err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("status = %d, want 200", resp.StatusCode)
			}
			var out struct{ Output string }
			if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if out.Output != "here is your answer" {
				t.Errorf("output = %q", out.Output)
			}
		})
	}

	if runner.lastChannel != "vscode" {
		t.Errorf("channel = %q, want vscode", runner.lastChannel)
	}
}

// TestVSCodePromptShape verifies the action-specific prompt building —
// explain/fix/tests must include the code and language context.
func TestVSCodePromptShape(t *testing.T) {
	runner := &fakeRunner{output: "ok"}
	srv := newVSCodeServer(t, runner)
	defer srv.Close()

	if _, err := http.Post(srv.URL+"/api/vscode/tests",
		"application/json", strings.NewReader(`{"code":"func Add(a,b int) int {return a+b}","language":"go","path":"add.go"}`)); err != nil {
		t.Fatalf("post: %v", err)
	}
	p := runner.lastPrompt
	if !strings.Contains(p, "func Add") {
		t.Errorf("prompt must include the code, got: %q", p)
	}
	if !strings.Contains(p, "unit tests") {
		t.Errorf("tests action prompt must ask for unit tests, got: %q", p)
	}
	if !strings.Contains(p, "add.go") {
		t.Errorf("tests action prompt should include the file path hint")
	}
}

// TestVSCodeRouteValidation covers: wrong method, unknown action, bad JSON,
// empty payloads, and no-runner (503 with clear message).
func TestVSCodeRouteValidation(t *testing.T) {
	// No runner → 503 with a helpful error.
	bare := httptest.NewServer(&webchat.VSCodeHandler{})
	defer bare.Close()
	resp, _ := http.Post(bare.URL+"/api/vscode/ask", "application/json",
		strings.NewReader(`{"message":"hi"}`))
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("no-runner status = %d, want 503", resp.StatusCode)
	}
	resp.Body.Close()

	// With runner: method and action validation.
	runner := &fakeRunner{output: "x"}
	srv := newVSCodeServer(t, runner)
	defer srv.Close()

	if resp, _ := http.Get(srv.URL + "/api/vscode/ask"); resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("GET status = %d, want 405", resp.StatusCode)
	} else {
		resp.Body.Close()
	}

	if resp, _ := http.Post(srv.URL+"/api/vscode/explode", "application/json",
		strings.NewReader(`{}`)); resp.StatusCode != http.StatusNotFound {
		t.Errorf("unknown action status = %d, want 404", resp.StatusCode)
	} else {
		resp.Body.Close()
	}

	if resp, _ := http.Post(srv.URL+"/api/vscode/ask", "application/json",
		strings.NewReader(`not json`)); resp.StatusCode != http.StatusBadRequest {
		t.Errorf("bad JSON status = %d, want 400", resp.StatusCode)
	} else {
		resp.Body.Close()
	}

	// ask with empty message → 404 (unusable payload).
	if resp, _ := http.Post(srv.URL+"/api/vscode/ask", "application/json",
		strings.NewReader(`{"message":""}`)); resp.StatusCode != http.StatusNotFound {
		t.Errorf("empty ask status = %d, want 404", resp.StatusCode)
	} else {
		resp.Body.Close()
	}
}