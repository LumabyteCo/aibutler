package a2a_test

import (
	"context"
	"crypto/sha256"
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

type mockRunner struct {
	output string
	err    error
}

func (m *mockRunner) RunTask(_ context.Context, _ string) (string, error) {
	return m.output, m.err
}

func hashToken(token string) string {
	h := sha256.Sum256([]byte(token))
	return fmt.Sprintf("%x", h)
}

func testCard() a2a.AgentCard {
	return a2a.AgentCard{
		Name:         "test-agent",
		Description:  "A test agent",
		URL:          "http://localhost:8081",
		Capabilities: []string{"memory.search", "data.query"},
		Version:      "0.1.0",
	}
}

func testHandler(db interface{ ExecContext(context.Context, string, ...interface{}) (interface{ LastInsertId() (int64, error) }, error) }) *a2a.Handler {
	return nil // placeholder, we need the real DB
}

func newTestHandler(t *testing.T) (*a2a.Handler, *httptest.Server) {
	t.Helper()
	database := testutil.TestDB(t)
	conn := database.Conn()
	runner := &mockRunner{output: "task completed successfully"}
	tokenHash := hashToken("secret-token")
	handler := a2a.NewHandler(conn, runner, testCard(), []string{tokenHash})
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return handler, srv
}

func TestAgentCardDiscovery(t *testing.T) {
	_, srv := newTestHandler(t)

	resp, err := http.Get(srv.URL + "/.well-known/agent.json")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Fatalf("status = %d", resp.StatusCode)
	}

	var card a2a.AgentCard
	json.NewDecoder(resp.Body).Decode(&card)
	if card.Name != "test-agent" {
		t.Errorf("name = %q", card.Name)
	}
	if len(card.Capabilities) != 2 {
		t.Errorf("capabilities = %v", card.Capabilities)
	}
}

func TestTaskDelegationSuccess(t *testing.T) {
	_, srv := newTestHandler(t)

	body := `{"id": "task-1", "task": "search memory for Alice"}`
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/a2a/tasks", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer secret-token")
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d: %s", resp.StatusCode, b)
	}

	var result a2a.TaskResult
	json.NewDecoder(resp.Body).Decode(&result)
	if result.Status != "completed" {
		t.Errorf("status = %q", result.Status)
	}
	if result.Output != "task completed successfully" {
		t.Errorf("output = %q", result.Output)
	}
}

func TestTaskDelegationRejectsNoAuth(t *testing.T) {
	_, srv := newTestHandler(t)

	body := `{"id": "task-1", "task": "do something"}`
	resp, err := http.Post(srv.URL+"/a2a/tasks", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", resp.StatusCode)
	}
}

func TestTaskDelegationRejectsWrongToken(t *testing.T) {
	_, srv := newTestHandler(t)

	body := `{"id": "task-1", "task": "do something"}`
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/a2a/tasks", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer wrong-token")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("status = %d, want 403", resp.StatusCode)
	}
}

func TestTaskDelegationRecordsInDB(t *testing.T) {
	database := testutil.TestDB(t)
	conn := database.Conn()
	runner := &mockRunner{output: "done"}
	handler := a2a.NewHandler(conn, runner, testCard(), []string{hashToken("tok")})
	srv := httptest.NewServer(handler)
	defer srv.Close()

	body := `{"id": "task-db", "task": "test task"}`
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/a2a/tasks", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer tok")
	http.DefaultClient.Do(req)

	var direction, status string
	ctx := context.Background()
	err := conn.QueryRowContext(ctx, "SELECT direction, status FROM a2a_delegations LIMIT 1").Scan(&direction, &status)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if direction != "inbound" {
		t.Errorf("direction = %q", direction)
	}
	if status != "completed" {
		t.Errorf("status = %q", status)
	}
}

func TestTaskDelegationWithRunnerError(t *testing.T) {
	database := testutil.TestDB(t)
	conn := database.Conn()
	runner := &mockRunner{err: fmt.Errorf("task failed: out of memory")}
	handler := a2a.NewHandler(conn, runner, testCard(), []string{hashToken("tok")})
	srv := httptest.NewServer(handler)
	defer srv.Close()

	body := `{"id": "task-err", "task": "bad task"}`
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/a2a/tasks", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer tok")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()

	var result a2a.TaskResult
	json.NewDecoder(resp.Body).Decode(&result)
	if result.Status != "failed" {
		t.Errorf("status = %q, want failed", result.Status)
	}
	if !strings.Contains(result.Error, "out of memory") {
		t.Errorf("error = %q", result.Error)
	}
}

func TestHandlerUnknownRoute(t *testing.T) {
	_, srv := newTestHandler(t)

	resp, err := http.Get(srv.URL + "/unknown")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 404 {
		t.Errorf("status = %d, want 404", resp.StatusCode)
	}
}

func TestClientDiscover(t *testing.T) {
	database := testutil.TestDB(t)
	conn := database.Conn()
	handler := a2a.NewHandler(conn, &mockRunner{}, testCard(), nil)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	client := a2a.NewClient(nil, conn)
	card, err := client.Discover(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	if card.Name != "test-agent" {
		t.Errorf("name = %q", card.Name)
	}
}

func TestClientDelegate(t *testing.T) {
	database := testutil.TestDB(t)
	conn := database.Conn()
	runner := &mockRunner{output: "delegation result"}
	handler := a2a.NewHandler(conn, runner, testCard(), []string{hashToken("client-tok")})
	srv := httptest.NewServer(handler)
	defer srv.Close()

	client := a2a.NewClient(nil, conn)
	result, err := client.Delegate(context.Background(), srv.URL, "client-tok", "search for Alice")
	if err != nil {
		t.Fatalf("delegate: %v", err)
	}
	if result.Status != "completed" {
		t.Errorf("status = %q", result.Status)
	}
	if result.Output != "delegation result" {
		t.Errorf("output = %q", result.Output)
	}
}

func TestClientDelegateRecordsInDB(t *testing.T) {
	database := testutil.TestDB(t)
	conn := database.Conn()
	runner := &mockRunner{output: "ok"}
	handler := a2a.NewHandler(conn, runner, testCard(), []string{hashToken("tok2")})
	srv := httptest.NewServer(handler)
	defer srv.Close()

	client := a2a.NewClient(nil, conn)
	client.Delegate(context.Background(), srv.URL, "tok2", "outbound task")

	ctx := context.Background()
	var count int
	conn.QueryRowContext(ctx, "SELECT COUNT(*) FROM a2a_delegations WHERE direction = 'outbound'").Scan(&count)
	if count < 1 {
		t.Error("expected outbound delegation record")
	}
}

func TestClientDelegateAuthHeader(t *testing.T) {
	// Verify the Authorization header is sent correctly.
	var gotAuth string
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		json.NewEncoder(w).Encode(a2a.TaskResult{Status: "completed"})
	}))
	defer mock.Close()

	client := a2a.NewClient(nil, nil)
	client.Delegate(context.Background(), mock.URL, "my-secret", "test")
	if gotAuth != "Bearer my-secret" {
		t.Errorf("auth = %q, want 'Bearer my-secret'", gotAuth)
	}
}

func TestSetRunner(t *testing.T) {
	database := testutil.TestDB(t)
	conn := database.Conn()
	initial := &mockRunner{output: "from initial runner"}
	handler := a2a.NewHandler(conn, initial, testCard(), []string{hashToken("tok")})
	srv := httptest.NewServer(handler)
	defer srv.Close()

	// Replace runner after handler creation.
	handler.SetRunner(&mockRunner{output: "from new runner"})

	body := `{"id": "t1", "task": "test task"}`
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/a2a/tasks", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer tok")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()

	var result a2a.TaskResult
	json.NewDecoder(resp.Body).Decode(&result)
	if result.Output != "from new runner" {
		t.Errorf("output = %q, want 'from new runner'", result.Output)
	}
}

func TestHandlerEmptyBody(t *testing.T) {
	_, srv := newTestHandler(t)

	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/a2a/tasks", strings.NewReader(""))
	req.Header.Set("Authorization", "Bearer secret-token")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", resp.StatusCode)
	}
}
