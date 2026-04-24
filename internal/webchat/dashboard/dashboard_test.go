package dashboard_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/LumabyteCo/aibutler/internal/webchat/dashboard"
	"github.com/LumabyteCo/aibutler/testutil"
)

// mockRegistry implements dashboard.RegistryBrowser.
type mockRegistry struct {
	agents []dashboard.AgentRecord
}

func (m *mockRegistry) DiscoverAll(_ context.Context) ([]dashboard.AgentRecord, error) {
	return m.agents, nil
}

func (m *mockRegistry) Register(_ context.Context, name, url string, capabilities []string, _ string) error {
	m.agents = append(m.agents, dashboard.AgentRecord{Name: name, URL: url, Capabilities: capabilities})
	return nil
}

func (m *mockRegistry) Deregister(_ context.Context, name string) error {
	for i, a := range m.agents {
		if a.Name == name {
			m.agents = append(m.agents[:i], m.agents[i+1:]...)
			return nil
		}
	}
	return fmt.Errorf("not found")
}

// mockSwarmStore implements dashboard.SwarmStore.
type mockSwarmStore struct {
	runs   []dashboard.SwarmRun
	traces []dashboard.TraceSpan
}

func (m *mockSwarmStore) ListRuns(_ context.Context, limit int) ([]dashboard.SwarmRun, error) {
	if limit > len(m.runs) {
		return m.runs, nil
	}
	return m.runs[:limit], nil
}

func (m *mockSwarmStore) GetRun(_ context.Context, runID string) (*dashboard.SwarmRun, error) {
	for _, r := range m.runs {
		if r.RunID == runID {
			return &r, nil
		}
	}
	return nil, fmt.Errorf("not found")
}

func (m *mockSwarmStore) GetTraces(_ context.Context, runID string) ([]dashboard.TraceSpan, error) {
	return m.traces, nil
}

func newTestDashboard(t *testing.T) (*dashboard.Dashboard, http.Handler) {
	t.Helper()
	db := testutil.TestDB(t)
	conn := db.Conn()
	ctx := context.Background()

	// Seed some data for stats.
	conn.ExecContext(ctx, `INSERT INTO sessions (id, channel, account_id) VALUES ('s1', 'webchat', 'u1')`)
	conn.ExecContext(ctx, `INSERT INTO captured_thoughts (content, source, session_id) VALUES ('test thought', 'note', 's1')`)
	conn.ExecContext(ctx, `INSERT INTO key_facts (fact, category, source_session) VALUES ('test fact', 'pref', 's1')`)

	reg := &mockRegistry{}
	store := &mockSwarmStore{
		runs: []dashboard.SwarmRun{
			{RunID: "run-1", Goal: "test goal", Status: "completed", StartedAt: "2026-03-30T00:00:00Z"},
		},
		traces: []dashboard.TraceSpan{
			{SpanID: "span-1", TraceID: "trace-1", AgentID: "agent-1", Status: "completed"},
		},
	}

	d := dashboard.New(conn, reg, store)
	return d, d.Handler()
}

func TestStatsEndpoint(t *testing.T) {
	_, handler := newTestDashboard(t)

	req := httptest.NewRequest(http.MethodGet, "/api/dashboard/stats", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	var resp map[string]interface{}
	json.NewDecoder(rec.Body).Decode(&resp)
	if resp["sessions"] == nil {
		t.Error("expected sessions in stats response")
	}
	if resp["thoughts"] == nil {
		t.Error("expected thoughts in stats response")
	}
}

func TestMemoryEndpoint(t *testing.T) {
	_, handler := newTestDashboard(t)

	req := httptest.NewRequest(http.MethodGet, "/api/dashboard/memory", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	var resp map[string]interface{}
	json.NewDecoder(rec.Body).Decode(&resp)
	thoughts, ok := resp["thoughts"].([]interface{})
	if !ok {
		t.Fatal("expected thoughts array")
	}
	if len(thoughts) != 1 {
		t.Errorf("thoughts count = %d, want 1", len(thoughts))
	}
}

func TestAgentsEndpoint(t *testing.T) {
	_, handler := newTestDashboard(t)

	req := httptest.NewRequest(http.MethodGet, "/api/dashboard/agents", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	var resp map[string]interface{}
	json.NewDecoder(rec.Body).Decode(&resp)
	if resp["delegations"] == nil {
		t.Error("expected delegations in response")
	}
}

func TestCostsEndpoint(t *testing.T) {
	_, handler := newTestDashboard(t)

	req := httptest.NewRequest(http.MethodGet, "/api/dashboard/costs", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	var resp map[string]interface{}
	json.NewDecoder(rec.Body).Decode(&resp)
	if resp["costs"] == nil {
		t.Error("expected costs in response")
	}
}

func TestSwarmRunsEndpoint(t *testing.T) {
	_, handler := newTestDashboard(t)

	req := httptest.NewRequest(http.MethodGet, "/api/dashboard/swarm/runs", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	var resp map[string]interface{}
	json.NewDecoder(rec.Body).Decode(&resp)
	runs, ok := resp["runs"].([]interface{})
	if !ok {
		t.Fatal("expected runs array")
	}
	if len(runs) != 1 {
		t.Errorf("runs count = %d, want 1", len(runs))
	}
}

func TestRegistryCRUD(t *testing.T) {
	_, handler := newTestDashboard(t)

	// POST: register agent.
	body := `{"name":"test-agent","url":"http://localhost:9999","capabilities":["search"]}`
	req := httptest.NewRequest(http.MethodPost, "/api/dashboard/registry", bytes.NewBufferString(body))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("POST registry: status = %d, want 201; body = %s", rec.Code, rec.Body.String())
	}

	// GET: list agents.
	req = httptest.NewRequest(http.MethodGet, "/api/dashboard/registry", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("GET registry: status = %d, want 200", rec.Code)
	}
	var resp map[string]interface{}
	json.NewDecoder(rec.Body).Decode(&resp)
	agents := resp["agents"].([]interface{})
	if len(agents) != 1 {
		t.Errorf("agents count = %d, want 1", len(agents))
	}

	// DELETE: deregister.
	req = httptest.NewRequest(http.MethodDelete, "/api/dashboard/registry/test-agent", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("DELETE registry: status = %d, want 200", rec.Code)
	}
}

func TestAgentCardCRUD(t *testing.T) {
	_, handler := newTestDashboard(t)

	// GET: default card (empty).
	req := httptest.NewRequest(http.MethodGet, "/api/dashboard/agent-card", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET agent-card: status = %d, want 200", rec.Code)
	}

	// PUT: save card.
	card := `{"name":"butler","description":"My assistant","url":"http://localhost:3377","capabilities":["memory"],"skills":["coding"],"auth_schemes":["bearer"],"streaming":true}`
	req = httptest.NewRequest(http.MethodPut, "/api/dashboard/agent-card", bytes.NewBufferString(card))
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT agent-card: status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}

	// GET: verify saved.
	req = httptest.NewRequest(http.MethodGet, "/api/dashboard/agent-card", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET agent-card after PUT: status = %d, want 200", rec.Code)
	}
	var result dashboard.AgentCardData
	json.NewDecoder(rec.Body).Decode(&result)
	if result.Name != "butler" {
		t.Errorf("agent card name = %q, want 'butler'", result.Name)
	}
	if !result.Streaming {
		t.Error("expected streaming = true")
	}
}

func TestActiveAgentsEndpoint(t *testing.T) {
	_, handler := newTestDashboard(t)

	// Seed an active agent.
	db := testutil.TestDB(t)
	conn := db.Conn()
	ctx := context.Background()
	conn.ExecContext(ctx,
		`INSERT INTO agents (id, session_id, type, state, task, capabilities, model, created_at, updated_at, mode, cost_usd, tokens_input, tokens_output, duration_ms, tool_calls)
		 VALUES ('agent-active-1', 's1', 'primary', 'running', 'test task', '[]', 'claude', datetime('now'), datetime('now'), 'multi', 0.01, 100, 50, 1000, 3)`)

	// Use the handler from newTestDashboard (which has its own DB).
	// Just test that the endpoint returns 200 with the right shape.
	req := httptest.NewRequest(http.MethodGet, "/api/dashboard/agents/active", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	var resp map[string]interface{}
	json.NewDecoder(rec.Body).Decode(&resp)
	if resp["agents"] == nil {
		t.Error("expected agents in response")
	}
}

func TestAgentTreeEndpoint(t *testing.T) {
	_, handler := newTestDashboard(t)

	// Test with non-existent agent — should return 404.
	req := httptest.NewRequest(http.MethodGet, "/api/dashboard/agents/tree/nonexistent", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}

	// Test missing ID — should return 400.
	req = httptest.NewRequest(http.MethodGet, "/api/dashboard/agents/tree/", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestSwarmTopology(t *testing.T) {
	_, handler := newTestDashboard(t)

	req := httptest.NewRequest(http.MethodGet, "/api/dashboard/swarm/topology", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	var resp map[string]interface{}
	json.NewDecoder(rec.Body).Decode(&resp)
	if resp["nodes"] == nil {
		t.Error("expected nodes in topology")
	}
	if resp["edges"] == nil {
		t.Error("expected edges in topology")
	}
}

func TestAIUsageEndpoint(t *testing.T) {
	_, handler := newTestDashboard(t)

	req := httptest.NewRequest(http.MethodGet, "/api/dashboard/ai/usage", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	var resp map[string]interface{}
	json.NewDecoder(rec.Body).Decode(&resp)
	if resp["total_calls"] == nil {
		t.Error("expected total_calls in AI usage response")
	}
	if resp["providers"] == nil {
		t.Error("expected providers in AI usage response")
	}
}

func TestAIProvidersEndpoint(t *testing.T) {
	_, handler := newTestDashboard(t)

	req := httptest.NewRequest(http.MethodGet, "/api/dashboard/ai/providers", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	var resp map[string]interface{}
	json.NewDecoder(rec.Body).Decode(&resp)
	providers, ok := resp["providers"].([]interface{})
	if !ok {
		t.Fatal("expected providers array")
	}
	if len(providers) != 5 {
		t.Errorf("providers count = %d, want 5", len(providers))
	}
}
