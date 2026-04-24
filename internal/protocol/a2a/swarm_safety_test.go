package a2a_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/LumabyteCo/aibutler/internal/protocol/a2a"
	"github.com/LumabyteCo/aibutler/testutil"
)

func newSafetyHandler(t *testing.T) *a2a.Handler {
	t.Helper()
	database := testutil.TestDB(t)
	conn := database.Conn()
	runner := &mockRunner{output: "ok"}
	token := "safety-test-token"
	card := a2a.AgentCard{
		Name:         "test-agent",
		Description:  "safety test agent",
		Capabilities: []string{"test"},
		Version:      "0.1.0",
	}
	return a2a.NewHandler(conn, runner, card, []string{hashToken(token)})
}

func TestDepthLimit429(t *testing.T) {
	h := newSafetyHandler(t)
	h.SetMaxDepth(2)

	body := `{"id":"t1","task":"hello"}`
	req := httptest.NewRequest(http.MethodPost, "/a2a/tasks", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer safety-test-token")
	req.Header.Set("X-Swarm-Depth", "2") // Already at depth 2, will become 3 > max 2.

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusTooManyRequests {
		t.Errorf("got status %d, want 429", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "swarm depth limit exceeded") {
		t.Errorf("unexpected body: %s", rec.Body.String())
	}
}

func TestLoopDetection409(t *testing.T) {
	h := newSafetyHandler(t)

	body := `{"id":"t2","task":"hello"}`
	req := httptest.NewRequest(http.MethodPost, "/a2a/tasks", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer safety-test-token")
	req.Header.Set("X-Swarm-Agent-Chain", "agent-a,test-agent,agent-b")

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusConflict {
		t.Errorf("got status %d, want 409", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "agent loop detected") {
		t.Errorf("unexpected body: %s", rec.Body.String())
	}
}

func TestDepthAndChainContext(t *testing.T) {
	database := testutil.TestDB(t)
	conn := database.Conn()

	// Use a runner that captures context values.
	var capturedDepth int
	var capturedChain string
	runner := &contextCapturingRunner{
		onRun: func(ctx context.Context) {
			capturedDepth = a2a.SwarmDepthFromContext(ctx)
			capturedChain = a2a.SwarmChainFromContext(ctx)
		},
	}

	token := "ctx-test-token"
	card := a2a.AgentCard{Name: "my-agent", Version: "1.0"}
	h := a2a.NewHandler(conn, runner, card, []string{hashToken(token)})

	body := `{"id":"t3","task":"check context"}`
	req := httptest.NewRequest(http.MethodPost, "/a2a/tasks", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer ctx-test-token")
	req.Header.Set("X-Swarm-Depth", "1")
	req.Header.Set("X-Swarm-Agent-Chain", "agent-a")

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("got status %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	if capturedDepth != 2 {
		t.Errorf("depth = %d, want 2 (1 + 1)", capturedDepth)
	}
	if capturedChain != "agent-a,my-agent" {
		t.Errorf("chain = %q, want %q", capturedChain, "agent-a,my-agent")
	}
}

// contextCapturingRunner captures context values for testing.
type contextCapturingRunner struct {
	onRun func(ctx context.Context)
}

func (r *contextCapturingRunner) RunTask(ctx context.Context, task string) (string, error) {
	if r.onRun != nil {
		r.onRun(ctx)
	}
	return "ok", nil
}
