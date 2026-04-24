package server_v2

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/LumabyteCo/aibutler/internal/mcp"
)

// mockMemory implements MemorySearcher for testing.
type mockMemory struct{}

func (m *mockMemory) Search(_ context.Context, query string) ([]SearchResult, error) {
	return []SearchResult{
		{Content: "result for: " + query, Score: 0.95},
	}, nil
}

// --- Mocks for the write-side interfaces (schedule.create, channel.send,
// swarm.run, agent.delegate). Each records the last call so tests can
// assert the dispatch wired through correctly. ---

type mockSchedule struct {
	createErr  error
	lastCreate *CreateScheduleRequest
}

func (m *mockSchedule) ListTasks(_ context.Context) ([]ScheduleEntry, error) {
	return nil, nil
}

func (m *mockSchedule) CreateTask(_ context.Context, req CreateScheduleRequest) (string, error) {
	m.lastCreate = &req
	if m.createErr != nil {
		return "", m.createErr
	}
	return "sched-test-1", nil
}

type mockChannels struct {
	sendErr  error
	lastSend *struct{ Channel, To, Message string }
}

func (m *mockChannels) ListChannels() []string { return []string{"webchat"} }

func (m *mockChannels) SendMessage(_ context.Context, channel, to, message string) error {
	m.lastSend = &struct{ Channel, To, Message string }{channel, to, message}
	return m.sendErr
}

type mockAgents struct {
	delegateErr    error
	lastDelegate   *DelegateRequest
	delegateResult string
}

func (m *mockAgents) ListAgents(_ context.Context) ([]AgentEntry, error) { return nil, nil }

func (m *mockAgents) DelegateTask(_ context.Context, req DelegateRequest) (string, error) {
	m.lastDelegate = &req
	if m.delegateErr != nil {
		return "", m.delegateErr
	}
	return m.delegateResult, nil
}

type mockSwarm struct {
	runErr    error
	lastGoal  string
	runResult string
}

func (m *mockSwarm) Run(_ context.Context, goal string) (string, error) {
	m.lastGoal = goal
	if m.runErr != nil {
		return "", m.runErr
	}
	return m.runResult, nil
}

// stdioRoundTrip sends a JSON-RPC request over stdio and returns the response.
func stdioRoundTrip(t *testing.T, srv *Server, req mcp.JSONRPCRequest) mcp.JSONRPCResponse {
	t.Helper()

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}

	pr, pw := io.Pipe()
	var output bytes.Buffer
	done := make(chan struct{})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	go func() {
		defer close(done)
		srv.HandleStdio(ctx, pr, &output)
	}()

	// Write request then close input to signal EOF.
	pw.Write(append(data, '\n'))
	pw.Close()

	// Wait for server to finish.
	select {
	case <-done:
	case <-ctx.Done():
		t.Fatal("stdio roundtrip timed out")
	}

	lines := strings.Split(strings.TrimSpace(output.String()), "\n")
	if len(lines) == 0 || lines[0] == "" {
		t.Fatal("no response received")
	}

	var resp mcp.JSONRPCResponse
	if err := json.Unmarshal([]byte(lines[0]), &resp); err != nil {
		t.Fatalf("unmarshal response: %v\nraw: %s", err, lines[0])
	}
	return resp
}

func TestToolsList(t *testing.T) {
	srv := New(&mockMemory{}, nil, nil, nil)

	resp := stdioRoundTrip(t, srv, mcp.JSONRPCRequest{JSONRPC: "2.0", ID: 1, Method: "tools/list"})

	if resp.Error != nil {
		t.Fatalf("unexpected error: %s", resp.Error.Message)
	}

	var result mcp.ToolListResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if len(result.Tools) != 5 {
		t.Errorf("tools count = %d, want 5", len(result.Tools))
	}

	names := make(map[string]bool)
	for _, tool := range result.Tools {
		names[tool.Name] = true
	}
	for _, expected := range []string{"butler.memory.search", "butler.schedule.create", "butler.channel.send", "butler.swarm.run", "butler.agent.delegate"} {
		if !names[expected] {
			t.Errorf("missing tool: %s", expected)
		}
	}
}

func TestToolsCallMemorySearch(t *testing.T) {
	srv := New(&mockMemory{}, nil, nil, nil)

	params := mcp.ToolCallParams{
		Name:      "butler.memory.search",
		Arguments: json.RawMessage(`{"query":"test query"}`),
	}

	resp := stdioRoundTrip(t, srv, mcp.JSONRPCRequest{JSONRPC: "2.0", ID: 2, Method: "tools/call", Params: params})

	if resp.Error != nil {
		t.Fatalf("unexpected error: %s", resp.Error.Message)
	}

	var result mcp.ToolCallResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}

	if result.IsError {
		t.Error("result indicates error")
	}
	if len(result.Content) == 0 {
		t.Fatal("no content returned")
	}
	if !strings.Contains(result.Content[0].Text, "result for: test query") {
		t.Errorf("content = %q, want to contain 'result for: test query'", result.Content[0].Text)
	}
}

// TestScheduleCreateDispatches proves butler.schedule.create routes to
// the wired ScheduleOps.CreateTask. Regression guard against the old
// "return hardcoded sched-stub" behavior.
func TestScheduleCreateDispatches(t *testing.T) {
	sched := &mockSchedule{}
	srv := New(&mockMemory{}, sched, &mockChannels{}, &mockAgents{})

	params := mcp.ToolCallParams{
		Name: "butler.schedule.create",
		Arguments: json.RawMessage(`{"description":"daily standup","cron":"0 9 * * 1-5","task":"summarize yesterday","channel":"webchat","account_id":"user-1"}`),
	}
	resp := stdioRoundTrip(t, srv, mcp.JSONRPCRequest{JSONRPC: "2.0", ID: 1, Method: "tools/call", Params: params})

	if resp.Error != nil {
		t.Fatalf("unexpected JSON-RPC error: %s", resp.Error.Message)
	}
	var result mcp.ToolCallResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected success, got error: %v", result.Content)
	}
	if sched.lastCreate == nil {
		t.Fatal("ScheduleOps.CreateTask was never called")
	}
	if sched.lastCreate.Description != "daily standup" {
		t.Errorf("description = %q", sched.lastCreate.Description)
	}
	if sched.lastCreate.CronExpr != "0 9 * * 1-5" {
		t.Errorf("cron = %q", sched.lastCreate.CronExpr)
	}
	if sched.lastCreate.Task != "summarize yesterday" {
		t.Errorf("task = %q", sched.lastCreate.Task)
	}
	// Response body should echo the generated id.
	if len(result.Content) == 0 || !strings.Contains(result.Content[0].Text, "sched-test-1") {
		t.Errorf("response should contain generated id, got %v", result.Content)
	}
}

// TestScheduleCreateValidation rejects missing required fields.
func TestScheduleCreateValidation(t *testing.T) {
	srv := New(&mockMemory{}, &mockSchedule{}, &mockChannels{}, &mockAgents{})

	for _, body := range []string{
		`{}`,
		`{"description":"x"}`, // missing cron
		`{"cron":"* * * * *"}`, // missing description
	} {
		params := mcp.ToolCallParams{Name: "butler.schedule.create", Arguments: json.RawMessage(body)}
		resp := stdioRoundTrip(t, srv, mcp.JSONRPCRequest{JSONRPC: "2.0", ID: 1, Method: "tools/call", Params: params})

		var result mcp.ToolCallResult
		_ = json.Unmarshal(resp.Result, &result)
		if !result.IsError {
			t.Errorf("body %s: expected IsError=true for missing required field", body)
		}
	}
}

// TestChannelSendDispatches proves butler.channel.send routes to the
// wired ChannelOps.SendMessage.
func TestChannelSendDispatches(t *testing.T) {
	chans := &mockChannels{}
	srv := New(&mockMemory{}, &mockSchedule{}, chans, &mockAgents{})

	params := mcp.ToolCallParams{
		Name:      "butler.channel.send",
		Arguments: json.RawMessage(`{"channel":"webchat","to":"user-1","message":"hello"}`),
	}
	resp := stdioRoundTrip(t, srv, mcp.JSONRPCRequest{JSONRPC: "2.0", ID: 2, Method: "tools/call", Params: params})

	if resp.Error != nil {
		t.Fatalf("JSON-RPC error: %s", resp.Error.Message)
	}
	var result mcp.ToolCallResult
	_ = json.Unmarshal(resp.Result, &result)
	if result.IsError {
		t.Fatalf("expected success, got: %v", result.Content)
	}
	if chans.lastSend == nil {
		t.Fatal("ChannelOps.SendMessage was never called")
	}
	if chans.lastSend.Channel != "webchat" || chans.lastSend.To != "user-1" || chans.lastSend.Message != "hello" {
		t.Errorf("dispatched args wrong: %+v", chans.lastSend)
	}
}

// TestSwarmRunDispatches proves butler.swarm.run routes to the wired
// SwarmRunner.Run.
func TestSwarmRunDispatches(t *testing.T) {
	sw := &mockSwarm{runResult: "synthesis: done"}
	srv := New(&mockMemory{}, &mockSchedule{}, &mockChannels{}, &mockAgents{})
	srv.SetSwarmRunner(sw)

	params := mcp.ToolCallParams{
		Name:      "butler.swarm.run",
		Arguments: json.RawMessage(`{"task":"research the memory story"}`),
	}
	resp := stdioRoundTrip(t, srv, mcp.JSONRPCRequest{JSONRPC: "2.0", ID: 3, Method: "tools/call", Params: params})

	var result mcp.ToolCallResult
	_ = json.Unmarshal(resp.Result, &result)
	if result.IsError {
		t.Fatalf("expected success, got: %v", result.Content)
	}
	if sw.lastGoal != "research the memory story" {
		t.Errorf("goal dispatched as %q, want %q", sw.lastGoal, "research the memory story")
	}
	if len(result.Content) == 0 || !strings.Contains(result.Content[0].Text, "synthesis: done") {
		t.Errorf("response should include swarm result, got %v", result.Content)
	}
}

// TestSwarmRunNotConfigured proves we still get an honest error (not a
// fake success) when the swarm runner isn't wired — the "configured out"
// path. This prevents regression to the old stub behavior.
func TestSwarmRunNotConfigured(t *testing.T) {
	srv := New(&mockMemory{}, &mockSchedule{}, &mockChannels{}, &mockAgents{})
	// Intentionally NOT calling SetSwarmRunner.

	params := mcp.ToolCallParams{
		Name:      "butler.swarm.run",
		Arguments: json.RawMessage(`{"task":"x"}`),
	}
	resp := stdioRoundTrip(t, srv, mcp.JSONRPCRequest{JSONRPC: "2.0", ID: 4, Method: "tools/call", Params: params})

	var result mcp.ToolCallResult
	_ = json.Unmarshal(resp.Result, &result)
	if !result.IsError {
		t.Fatal("expected IsError=true when swarm runner not configured")
	}
	if len(result.Content) == 0 || !strings.Contains(result.Content[0].Text, "not configured") {
		t.Errorf("expected 'not configured' error, got %v", result.Content)
	}
}

// TestAgentDelegateDispatches proves butler.agent.delegate routes to
// AgentOps.DelegateTask and returns the runner's output.
func TestAgentDelegateDispatches(t *testing.T) {
	ag := &mockAgents{delegateResult: "task completed: 42"}
	srv := New(&mockMemory{}, &mockSchedule{}, &mockChannels{}, ag)

	params := mcp.ToolCallParams{
		Name:      "butler.agent.delegate",
		Arguments: json.RawMessage(`{"task":"compute the meaning of life"}`),
	}
	resp := stdioRoundTrip(t, srv, mcp.JSONRPCRequest{JSONRPC: "2.0", ID: 5, Method: "tools/call", Params: params})

	var result mcp.ToolCallResult
	_ = json.Unmarshal(resp.Result, &result)
	if result.IsError {
		t.Fatalf("expected success, got: %v", result.Content)
	}
	if ag.lastDelegate == nil || ag.lastDelegate.Task != "compute the meaning of life" {
		t.Errorf("delegate dispatched wrong: %+v", ag.lastDelegate)
	}
	if len(result.Content) == 0 || !strings.Contains(result.Content[0].Text, "task completed: 42") {
		t.Errorf("response should include runner output, got %v", result.Content)
	}
}

func TestResourcesList(t *testing.T) {
	srv := New(nil, nil, nil, nil)

	resp := stdioRoundTrip(t, srv, mcp.JSONRPCRequest{JSONRPC: "2.0", ID: 3, Method: "resources/list"})

	if resp.Error != nil {
		t.Fatalf("unexpected error: %s", resp.Error.Message)
	}

	var result ResourceListResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if len(result.Resources) != 5 {
		t.Errorf("resources count = %d, want 5", len(result.Resources))
	}
}

func TestHTTPHandler(t *testing.T) {
	srv := New(&mockMemory{}, nil, nil, nil)
	handler := srv.HandleHTTP()

	req := mcp.JSONRPCRequest{JSONRPC: "2.0", ID: 4, Method: "tools/list"}
	data, _ := json.Marshal(req)

	httpReq := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader(data))
	httpReq.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, httpReq)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}

	var resp mcp.JSONRPCResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if resp.Error != nil {
		t.Fatalf("unexpected error: %s", resp.Error.Message)
	}

	var result mcp.ToolListResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if len(result.Tools) != 5 {
		t.Errorf("tools count = %d, want 5", len(result.Tools))
	}
}
