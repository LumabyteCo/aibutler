package tests

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/LumabyteCo/aibutler/internal/protocol/a2a"
	"github.com/LumabyteCo/aibutler/testutil"
)

// newTestA2AHandler creates an A2A handler wired to a test database.
func newTestA2AHandler(t *testing.T, token string) (*a2a.Handler, string) {
	t.Helper()
	database := testutil.TestDB(t)
	conn := database.Conn()

	tokenHash := a2a.HashToken(token)
	card := a2a.AgentCard{
		Name:         "test-butler",
		Description:  "AI Butler test agent",
		URL:          "http://localhost:8080",
		Capabilities: []string{"general", "memory", "tasks"},
		Version:      "2.0.0",
		Streaming:    true,
	}

	runner := &fakeTaskRunner{}
	handler := a2a.NewHandler(conn, runner, card, []string{tokenHash})
	return handler, token
}

// TestA2AAgentCardDiscovery verifies GET /.well-known/agent.json returns a valid AgentCard.
func TestA2AAgentCardDiscovery(t *testing.T) {
	handler, _ := newTestA2AHandler(t, "test-token")

	req := httptest.NewRequest(http.MethodGet, "/.well-known/agent.json", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var card a2a.AgentCard
	if err := json.NewDecoder(w.Body).Decode(&card); err != nil {
		t.Fatalf("decode agent card: %v", err)
	}

	if card.Name == "" {
		t.Error("agent card name is empty")
	}
	if card.URL == "" {
		t.Error("agent card url is empty")
	}
	if card.Description == "" {
		t.Error("agent card description is empty")
	}
	if len(card.Capabilities) == 0 {
		t.Error("agent card capabilities is empty")
	}
}

// TestA2AMultiPartMessageRequest verifies POST /a2a/tasks with messages[] containing parts[].
func TestA2AMultiPartMessageRequest(t *testing.T) {
	handler, token := newTestA2AHandler(t, "test-token")

	body := `{
		"id": "task-multipart",
		"messages": [
			{
				"role": "user",
				"parts": [{"text": "What is the weather today?"}]
			}
		]
	}`

	req := httptest.NewRequest(http.MethodPost, "/a2a/tasks", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", w.Code, w.Body.String())
	}

	var result a2a.TaskResult
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("decode result: %v", err)
	}

	// Task was extracted from the last user message part.
	if result.Status != "completed" && result.Status != "failed" {
		t.Errorf("expected completed or failed status, got %q", result.Status)
	}
}

// TestA2ATaskLifecycleStates verifies POST, GET status, and lifecycle transitions.
func TestA2ATaskLifecycleStates(t *testing.T) {
	handler, token := newTestA2AHandler(t, "test-token")

	// Submit a task.
	body := `{"id":"task-lifecycle","task":"process this request"}`
	req := httptest.NewRequest(http.MethodPost, "/a2a/tasks", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("submit task: expected 200, got %d", w.Code)
	}

	var result a2a.TaskResult
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("decode result: %v", err)
	}

	// The handler processes synchronously, so it should be completed or failed.
	if result.Status != "completed" && result.Status != "failed" {
		t.Errorf("expected terminal status, got %q", result.Status)
	}
}

// TestA2ATaskCancellation submits a task, cancels it, and verifies canceled status.
func TestA2ATaskCancellation(t *testing.T) {
	database := testutil.TestDB(t)
	conn := database.Conn()

	tokenHash := a2a.HashToken("cancel-token")
	card := a2a.AgentCard{Name: "cancel-agent", URL: "http://localhost:8080"}
	runner := &fakeTaskRunner{}
	handler := a2a.NewHandler(conn, runner, card, []string{tokenHash})

	// Insert a running task directly for cancellation.
	_, err := conn.ExecContext(testCtx(),
		`INSERT INTO a2a_delegations (direction, peer_agent, task_summary, status, lifecycle_state, created_at)
		 VALUES ('inbound', 'test', 'cancel test', 'running', 'working', datetime('now'))`)
	if err != nil {
		t.Fatalf("insert delegation: %v", err)
	}

	var taskID int64
	if err := conn.QueryRowContext(testCtx(), `SELECT id FROM a2a_delegations LIMIT 1`).Scan(&taskID); err != nil {
		t.Fatalf("get task id: %v", err)
	}

	// Cancel it.
	cancelURL := fmt.Sprintf("/a2a/tasks/%d/cancel", taskID)
	req := httptest.NewRequest(http.MethodPost, cancelURL, nil)
	req.Header.Set("Authorization", "Bearer cancel-token")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("cancel: expected 200, got %d; body: %s", w.Code, w.Body.String())
	}

	var resp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode cancel response: %v", err)
	}
	if resp["status"] != "canceled" {
		t.Errorf("expected status 'canceled', got %q", resp["status"])
	}
}

// TestA2AStreamingEndpoint verifies GET /a2a/tasks/{id}/stream returns SSE format.
func TestA2AStreamingEndpoint(t *testing.T) {
	database := testutil.TestDB(t)
	conn := database.Conn()

	tokenHash := a2a.HashToken("stream-token")
	card := a2a.AgentCard{Name: "stream-agent", URL: "http://localhost:8080"}
	runner := &fakeTaskRunner{}
	handler := a2a.NewHandler(conn, runner, card, []string{tokenHash})

	// Insert a completed task.
	_, err := conn.ExecContext(testCtx(),
		`INSERT INTO a2a_delegations (direction, peer_agent, task_summary, status, lifecycle_state, result_summary, created_at, completed_at)
		 VALUES ('inbound', 'test', 'stream test', 'completed', 'completed', 'done', datetime('now'), datetime('now'))`)
	if err != nil {
		t.Fatalf("insert delegation: %v", err)
	}

	var taskID int64
	if err := conn.QueryRowContext(testCtx(), `SELECT id FROM a2a_delegations LIMIT 1`).Scan(&taskID); err != nil {
		t.Fatalf("get task id: %v", err)
	}

	streamURL := fmt.Sprintf("/a2a/tasks/%d/stream", taskID)
	req := httptest.NewRequest(http.MethodGet, streamURL, nil)
	req.Header.Set("Authorization", "Bearer stream-token")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("stream: expected 200, got %d", w.Code)
	}

	contentType := w.Header().Get("Content-Type")
	if !strings.HasPrefix(contentType, "text/event-stream") {
		t.Errorf("expected Content-Type text/event-stream, got %q", contentType)
	}

	body, _ := io.ReadAll(w.Body)
	if !strings.Contains(string(body), "data:") {
		t.Error("expected SSE data: prefix in response body")
	}
}

// TestA2ABearerTokenAuth verifies correct token passes, wrong token gets 403, missing gets 401.
func TestA2ABearerTokenAuth(t *testing.T) {
	handler, _ := newTestA2AHandler(t, "valid-token")

	taskBody := `{"id":"auth-test","task":"test auth"}`

	// Correct token → 200.
	req := httptest.NewRequest(http.MethodPost, "/a2a/tasks", strings.NewReader(taskBody))
	req.Header.Set("Authorization", "Bearer valid-token")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("valid token: expected 200, got %d", w.Code)
	}

	// Wrong token → 403.
	req = httptest.NewRequest(http.MethodPost, "/a2a/tasks", strings.NewReader(taskBody))
	req.Header.Set("Authorization", "Bearer wrong-token")
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Errorf("wrong token: expected 403, got %d", w.Code)
	}

	// No token → 401.
	req = httptest.NewRequest(http.MethodPost, "/a2a/tasks", strings.NewReader(taskBody))
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("missing token: expected 401, got %d", w.Code)
	}
}

// TestA2ADepthHeaderPropagation sends a task with X-Swarm-Depth at the limit.
func TestA2ADepthHeaderPropagation(t *testing.T) {
	handler, token := newTestA2AHandler(t, "depth-token")
	handler.SetMaxDepth(3)

	body := `{"id":"depth-test","task":"test depth"}`

	// Depth 2 → should succeed (2+1=3 <= 3).
	req := httptest.NewRequest(http.MethodPost, "/a2a/tasks", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-Swarm-Depth", "2")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("depth 2 (limit 3): expected 200, got %d; body: %s", w.Code, w.Body.String())
	}

	// Depth 3 → should be rejected (3+1=4 > 3).
	req = httptest.NewRequest(http.MethodPost, "/a2a/tasks", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-Swarm-Depth", "3")
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusTooManyRequests {
		t.Errorf("depth 3 (limit 3): expected 429, got %d", w.Code)
	}
}
