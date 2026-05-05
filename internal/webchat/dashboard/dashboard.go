package dashboard

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// AgentRecord is a local copy of the registry record to avoid import cycles.
type AgentRecord struct {
	Name         string   `json:"name"`
	URL          string   `json:"url"`
	Capabilities []string `json:"capabilities"`
	LastSeen     string   `json:"last_seen"`
	SuccessCount int      `json:"success_count"`
	FailureCount int      `json:"failure_count"`
}

// RegistryBrowser is a narrow interface for the agent registry.
type RegistryBrowser interface {
	DiscoverAll(ctx context.Context) ([]AgentRecord, error)
	Register(ctx context.Context, name, url string, capabilities []string, healthCheckURL string) error
	Deregister(ctx context.Context, name string) error
}

// SwarmStore is a narrow interface for reading swarm run data.
type SwarmStore interface {
	ListRuns(ctx context.Context, limit int) ([]SwarmRun, error)
	GetRun(ctx context.Context, runID string) (*SwarmRun, error)
	GetTraces(ctx context.Context, runID string) ([]TraceSpan, error)
}

// SwarmRun represents a swarm execution.
type SwarmRun struct {
	ID          string  `json:"id"`
	RunID       string  `json:"run_id"`
	Goal        string  `json:"goal"`
	PlanJSON    string  `json:"plan_json"`
	Status      string  `json:"status"`
	TotalCost   float64 `json:"total_cost_usd"`
	TraceID     string  `json:"trace_id"`
	StartedAt   string  `json:"started_at"`
	CompletedAt string  `json:"completed_at,omitempty"`
}

// TraceSpan represents a single span in a distributed trace.
type TraceSpan struct {
	ID           string  `json:"id"`
	TraceID      string  `json:"trace_id"`
	SpanID       string  `json:"span_id"`
	ParentSpanID string  `json:"parent_span_id,omitempty"`
	AgentID      string  `json:"agent_id"`
	PeerURL      string  `json:"peer_url,omitempty"`
	TaskSummary  string  `json:"task_summary,omitempty"`
	Status       string  `json:"status"`
	CostUSD      float64 `json:"cost_usd"`
	StartedAt    string  `json:"started_at"`
	CompletedAt  string  `json:"completed_at,omitempty"`
}

// AgentCardData is the agent card configuration stored as a singleton row.
type AgentCardData struct {
	Name         string   `json:"name"`
	Description  string   `json:"description"`
	URL          string   `json:"url"`
	Capabilities []string `json:"capabilities"`
	Skills       []string `json:"skills"`
	AuthSchemes  []string `json:"auth_schemes"`
	Streaming    bool     `json:"streaming"`
	UpdatedAt    string   `json:"updated_at"`
}

// Dashboard provides HTTP API endpoints for the web dashboard.
type Dashboard struct {
	db         *sql.DB
	registry   RegistryBrowser
	swarmStore SwarmStore
}

// New creates a Dashboard.
func New(db *sql.DB, registry RegistryBrowser, swarmStore SwarmStore) *Dashboard {
	return &Dashboard{
		db:         db,
		registry:   registry,
		swarmStore: swarmStore,
	}
}

// Handler returns an http.Handler with all dashboard routes.
func (d *Dashboard) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/dashboard/stats", d.handleStats)
	mux.HandleFunc("/api/dashboard/memory", d.handleMemory)
	mux.HandleFunc("/api/dashboard/agents", d.handleAgents)
	mux.HandleFunc("/api/dashboard/costs", d.handleCosts)
	mux.HandleFunc("/api/dashboard/swarm/runs", d.handleSwarmRuns)
	mux.HandleFunc("/api/dashboard/swarm/run/", d.handleSwarmRun)
	mux.HandleFunc("/api/dashboard/swarm/topology", d.handleSwarmTopology)
	mux.HandleFunc("/api/dashboard/registry", d.handleRegistry)
	mux.HandleFunc("/api/dashboard/registry/", d.handleRegistry)
	mux.HandleFunc("/api/dashboard/agent-card", d.handleAgentCard)
	mux.HandleFunc("/api/dashboard/agents/active", d.handleActiveAgents)
	mux.HandleFunc("/api/dashboard/agents/tree/", d.handleAgentTree)
	mux.HandleFunc("/api/dashboard/ai/usage", d.handleAIUsage)
	mux.HandleFunc("/api/dashboard/ai/providers", d.handleAIProviders)

	// Enhanced dashboard routes.
	d.RegisterEnhancedRoutes(mux)

	// Mission dashboard routes — read-only viewer for the mission engine.
	d.RegisterMissionRoutes(mux)

	return mux
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func (d *Dashboard) handleStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	ctx := r.Context()

	stats := map[string]interface{}{}

	var sessions int
	if err := d.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM sessions").Scan(&sessions); err == nil {
		stats["sessions"] = sessions
	}

	var thoughts int
	if err := d.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM captured_thoughts").Scan(&thoughts); err == nil {
		stats["thoughts"] = thoughts
	}

	var entities int
	if err := d.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM entities").Scan(&entities); err == nil {
		stats["entities"] = entities
	}

	var agents int
	if err := d.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM agent_registry").Scan(&agents); err == nil {
		stats["agents"] = agents
	}

	var swarmRuns int
	if err := d.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM swarm_runs").Scan(&swarmRuns); err == nil {
		stats["swarm_runs"] = swarmRuns
	}

	var keyFacts int
	if err := d.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM key_facts").Scan(&keyFacts); err == nil {
		stats["key_facts"] = keyFacts
	}

	writeJSON(w, http.StatusOK, stats)
}

func (d *Dashboard) handleMemory(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	ctx := r.Context()

	limit := 50
	if l := r.URL.Query().Get("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil && n > 0 && n <= 200 {
			limit = n
		}
	}

	rows, err := d.db.QueryContext(ctx,
		`SELECT id, content, source, COALESCE(session_id,''), created_at
		 FROM captured_thoughts ORDER BY created_at DESC LIMIT ?`, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()

	type thought struct {
		ID        int    `json:"id"`
		Content   string `json:"content"`
		Source    string `json:"source"`
		SessionID string `json:"session_id"`
		CreatedAt string `json:"created_at"`
	}

	var thoughts []thought
	for rows.Next() {
		var t thought
		if err := rows.Scan(&t.ID, &t.Content, &t.Source, &t.SessionID, &t.CreatedAt); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		thoughts = append(thoughts, t)
	}
	if thoughts == nil {
		thoughts = []thought{}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"thoughts": thoughts})
}

func (d *Dashboard) handleAgents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	ctx := r.Context()

	rows, err := d.db.QueryContext(ctx,
		`SELECT id, direction, peer_agent, task_summary, status, COALESCE(result_summary,''), created_at
		 FROM a2a_delegations ORDER BY created_at DESC LIMIT 50`)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()

	type delegation struct {
		ID            int    `json:"id"`
		Direction     string `json:"direction"`
		PeerAgent     string `json:"peer_agent"`
		TaskSummary   string `json:"task_summary"`
		Status        string `json:"status"`
		ResultSummary string `json:"result_summary"`
		CreatedAt     string `json:"created_at"`
	}

	var delegations []delegation
	for rows.Next() {
		var dlg delegation
		if err := rows.Scan(&dlg.ID, &dlg.Direction, &dlg.PeerAgent, &dlg.TaskSummary, &dlg.Status, &dlg.ResultSummary, &dlg.CreatedAt); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		delegations = append(delegations, dlg)
	}
	if delegations == nil {
		delegations = []delegation{}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"delegations": delegations})
}

func (d *Dashboard) handleCosts(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	ctx := r.Context()

	rows, err := d.db.QueryContext(ctx,
		`SELECT model, SUM(input_tokens), SUM(output_tokens), SUM(cost_usd)
		 FROM token_usage GROUP BY model ORDER BY SUM(cost_usd) DESC`)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()

	type costEntry struct {
		Model        string  `json:"model"`
		InputTokens  int     `json:"input_tokens"`
		OutputTokens int     `json:"output_tokens"`
		CostUSD      float64 `json:"cost_usd"`
	}

	var entries []costEntry
	for rows.Next() {
		var e costEntry
		if err := rows.Scan(&e.Model, &e.InputTokens, &e.OutputTokens, &e.CostUSD); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		entries = append(entries, e)
	}
	if entries == nil {
		entries = []costEntry{}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"costs": entries})
}

func (d *Dashboard) handleSwarmRuns(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	if d.swarmStore == nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{"runs": []SwarmRun{}})
		return
	}

	runs, err := d.swarmStore.ListRuns(r.Context(), 50)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if runs == nil {
		runs = []SwarmRun{}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"runs": runs})
}

func (d *Dashboard) handleSwarmRun(w http.ResponseWriter, r *http.Request) {
	// Route: /api/dashboard/swarm/run/{id} or /api/dashboard/swarm/run/{id}/traces
	path := strings.TrimPrefix(r.URL.Path, "/api/dashboard/swarm/run/")
	parts := strings.SplitN(path, "/", 2)
	runID := parts[0]
	if runID == "" {
		writeError(w, http.StatusBadRequest, "missing run id")
		return
	}

	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	if d.swarmStore == nil {
		writeError(w, http.StatusNotFound, "swarm not configured")
		return
	}

	// /api/dashboard/swarm/run/{id}/traces
	if len(parts) == 2 && parts[1] == "traces" {
		traces, err := d.swarmStore.GetTraces(r.Context(), runID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if traces == nil {
			traces = []TraceSpan{}
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{"traces": traces})
		return
	}

	// /api/dashboard/swarm/run/{id}
	run, err := d.swarmStore.GetRun(r.Context(), runID)
	if err != nil {
		writeError(w, http.StatusNotFound, "run not found")
		return
	}
	writeJSON(w, http.StatusOK, run)
}

func (d *Dashboard) handleSwarmTopology(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	ctx := r.Context()

	type node struct {
		ID     string `json:"id"`
		Name   string `json:"name"`
		Status string `json:"status"`
	}
	type edge struct {
		From string `json:"from"`
		To   string `json:"to"`
		Task string `json:"task"`
	}

	// Build topology from active swarm runs + their trace spans.
	rows, err := d.db.QueryContext(ctx,
		`SELECT st.agent_id, st.peer_url, st.task_summary, st.status, st.parent_span_id, st.span_id
		 FROM swarm_trace st
		 JOIN swarm_runs sr ON st.trace_id = sr.trace_id
		 WHERE sr.status = 'running'
		 ORDER BY st.started_at`)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{"nodes": []node{}, "edges": []edge{}})
		return
	}
	defer rows.Close()

	nodeMap := make(map[string]node)
	var edges []edge
	spanAgent := make(map[string]string) // span_id -> agent_id

	for rows.Next() {
		var agentID, peerURL, task, status, parentSpan, spanID string
		if err := rows.Scan(&agentID, &peerURL, &task, &status, &parentSpan, &spanID); err != nil {
			continue
		}
		name := agentID
		if peerURL != "" {
			name = peerURL
		}
		nodeMap[agentID] = node{ID: agentID, Name: name, Status: status}
		spanAgent[spanID] = agentID

		if parentSpan != "" {
			if parentAgent, ok := spanAgent[parentSpan]; ok {
				edges = append(edges, edge{From: parentAgent, To: agentID, Task: task})
			}
		}
	}

	var nodes []node
	for _, n := range nodeMap {
		nodes = append(nodes, n)
	}
	if nodes == nil {
		nodes = []node{}
	}
	if edges == nil {
		edges = []edge{}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"nodes": nodes, "edges": edges})
}

func (d *Dashboard) handleRegistry(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	switch r.Method {
	case http.MethodGet:
		if d.registry == nil {
			writeJSON(w, http.StatusOK, map[string]interface{}{"agents": []AgentRecord{}})
			return
		}
		agents, err := d.registry.DiscoverAll(ctx)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if agents == nil {
			agents = []AgentRecord{}
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{"agents": agents})

	case http.MethodPost:
		if d.registry == nil {
			writeError(w, http.StatusInternalServerError, "registry not configured")
			return
		}
		// Limit request body to 1MB to prevent memory exhaustion from oversized payloads.
		r.Body = http.MaxBytesReader(w, r.Body, 1024*1024)
		var body struct {
			Name         string   `json:"name"`
			URL          string   `json:"url"`
			Capabilities []string `json:"capabilities"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON")
			return
		}
		if body.Name == "" || body.URL == "" {
			writeError(w, http.StatusBadRequest, "name and url required")
			return
		}
		if err := d.registry.Register(ctx, body.Name, body.URL, body.Capabilities, ""); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, map[string]string{"status": "registered"})

	case http.MethodDelete:
		if d.registry == nil {
			writeError(w, http.StatusInternalServerError, "registry not configured")
			return
		}
		// Extract name from path: /api/dashboard/registry/{name}
		name := strings.TrimPrefix(r.URL.Path, "/api/dashboard/registry/")
		name = strings.TrimPrefix(name, "/api/dashboard/registry")
		name = strings.TrimPrefix(name, "/")
		if name == "" {
			writeError(w, http.StatusBadRequest, "agent name required")
			return
		}
		if err := d.registry.Deregister(ctx, name); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "deregistered"})

	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (d *Dashboard) handleAgentCard(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	switch r.Method {
	case http.MethodGet:
		card, err := d.getAgentCard(ctx)
		if err != nil {
			writeJSON(w, http.StatusOK, &AgentCardData{
				Capabilities: []string{},
				Skills:       []string{},
				AuthSchemes:  []string{},
			})
			return
		}
		writeJSON(w, http.StatusOK, card)

	case http.MethodPut:
		// Limit request body to 1MB to prevent memory exhaustion from oversized payloads.
		r.Body = http.MaxBytesReader(w, r.Body, 1024*1024)
		var card AgentCardData
		if err := json.NewDecoder(r.Body).Decode(&card); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON")
			return
		}
		if err := d.saveAgentCard(ctx, &card); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "updated"})

	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (d *Dashboard) getAgentCard(ctx context.Context) (*AgentCardData, error) {
	var card AgentCardData
	var capsJSON, skillsJSON, authJSON string
	var streaming int
	err := d.db.QueryRowContext(ctx,
		`SELECT name, description, url, capabilities, skills, auth_schemes, streaming, updated_at
		 FROM agent_card_config WHERE id = 1`).Scan(
		&card.Name, &card.Description, &card.URL,
		&capsJSON, &skillsJSON, &authJSON,
		&streaming, &card.UpdatedAt)
	if err != nil {
		return nil, err
	}
	json.Unmarshal([]byte(capsJSON), &card.Capabilities)
	json.Unmarshal([]byte(skillsJSON), &card.Skills)
	json.Unmarshal([]byte(authJSON), &card.AuthSchemes)
	card.Streaming = streaming != 0
	if card.Capabilities == nil {
		card.Capabilities = []string{}
	}
	if card.Skills == nil {
		card.Skills = []string{}
	}
	if card.AuthSchemes == nil {
		card.AuthSchemes = []string{}
	}
	return &card, nil
}

func (d *Dashboard) handleActiveAgents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	ctx := r.Context()

	type activeAgent struct {
		ID        string  `json:"id"`
		Task      string  `json:"task"`
		State     string  `json:"state"`
		Type      string  `json:"type"`
		Mode      string  `json:"mode"`
		ParentID  string  `json:"parent_id,omitempty"`
		CostUSD   float64 `json:"cost_usd"`
		Duration  int64   `json:"duration_ms"`
		CreatedAt string  `json:"created_at"`
	}

	rows, err := d.db.QueryContext(ctx,
		`SELECT id, COALESCE(task,''), state, COALESCE(type,''), COALESCE(mode,''),
		        COALESCE(parent_id,''), COALESCE(cost_usd,0), COALESCE(duration_ms,0),
		        COALESCE(created_at,'')
		 FROM agents WHERE state IN ('spawned', 'running', 'waiting')
		 ORDER BY created_at DESC LIMIT 50`)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{"agents": []activeAgent{}})
		return
	}
	defer rows.Close()

	var agents []activeAgent
	for rows.Next() {
		var a activeAgent
		if err := rows.Scan(&a.ID, &a.Task, &a.State, &a.Type, &a.Mode,
			&a.ParentID, &a.CostUSD, &a.Duration, &a.CreatedAt); err != nil {
			continue
		}
		agents = append(agents, a)
	}
	if agents == nil {
		agents = []activeAgent{}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"agents": agents})
}

func (d *Dashboard) handleAgentTree(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	// Extract agent ID from path: /api/dashboard/agents/tree/{id}
	agentID := strings.TrimPrefix(r.URL.Path, "/api/dashboard/agents/tree/")
	if agentID == "" {
		writeError(w, http.StatusBadRequest, "missing agent id")
		return
	}
	ctx := r.Context()

	type treeNode struct {
		ID       string     `json:"id"`
		Task     string     `json:"task"`
		State    string     `json:"state"`
		Type     string     `json:"type"`
		CostUSD  float64    `json:"cost_usd"`
		Children []treeNode `json:"children,omitempty"`
	}

	// Build the tree by querying the root + all descendants via parent_id.
	nodeMap := make(map[string]*treeNode)
	childMap := make(map[string][]string) // parent_id -> child_ids

	rows, err := d.db.QueryContext(ctx,
		`WITH RECURSIVE tree AS (
		   SELECT id, task, state, type, COALESCE(parent_id,'') as parent_id, COALESCE(cost_usd,0) as cost_usd
		   FROM agents WHERE id = ?
		   UNION ALL
		   SELECT a.id, a.task, a.state, a.type, COALESCE(a.parent_id,''), COALESCE(a.cost_usd,0)
		   FROM agents a JOIN tree t ON a.parent_id = t.id
		 ) SELECT id, COALESCE(task,''), state, COALESCE(type,''), parent_id, cost_usd FROM tree`, agentID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()

	for rows.Next() {
		var id, task, state, typ, parentID string
		var costUSD float64
		if err := rows.Scan(&id, &task, &state, &typ, &parentID, &costUSD); err != nil {
			continue
		}
		nodeMap[id] = &treeNode{ID: id, Task: task, State: state, Type: typ, CostUSD: costUSD}
		if parentID != "" {
			childMap[parentID] = append(childMap[parentID], id)
		}
	}

	// Build tree structure.
	var buildTree func(id string) treeNode
	buildTree = func(id string) treeNode {
		node := *nodeMap[id]
		for _, childID := range childMap[id] {
			if _, ok := nodeMap[childID]; ok {
				node.Children = append(node.Children, buildTree(childID))
			}
		}
		return node
	}

	root, ok := nodeMap[agentID]
	if !ok {
		writeError(w, http.StatusNotFound, "agent not found")
		return
	}

	tree := buildTree(root.ID)
	writeJSON(w, http.StatusOK, tree)
}

func (d *Dashboard) saveAgentCard(ctx context.Context, card *AgentCardData) error {
	if card.Capabilities == nil {
		card.Capabilities = []string{}
	}
	if card.Skills == nil {
		card.Skills = []string{}
	}
	if card.AuthSchemes == nil {
		card.AuthSchemes = []string{}
	}
	capsJSON, _ := json.Marshal(card.Capabilities)
	skillsJSON, _ := json.Marshal(card.Skills)
	authJSON, _ := json.Marshal(card.AuthSchemes)
	streaming := 0
	if card.Streaming {
		streaming = 1
	}
	now := time.Now().UTC().Format(time.RFC3339)

	_, err := d.db.ExecContext(ctx,
		`INSERT INTO agent_card_config (id, name, description, url, capabilities, skills, auth_schemes, streaming, updated_at)
		 VALUES (1, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET
		     name = excluded.name,
		     description = excluded.description,
		     url = excluded.url,
		     capabilities = excluded.capabilities,
		     skills = excluded.skills,
		     auth_schemes = excluded.auth_schemes,
		     streaming = excluded.streaming,
		     updated_at = excluded.updated_at`,
		card.Name, card.Description, card.URL,
		string(capsJSON), string(skillsJSON), string(authJSON),
		streaming, now)
	if err != nil {
		return fmt.Errorf("dashboard: save agent card: %w", err)
	}
	return nil
}

// handleAIUsage returns AI service usage statistics.
func (d *Dashboard) handleAIUsage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	// Return usage stats from transaction_audit table (AI tool calls logged there).
	ctx := r.Context()
	usage := map[string]interface{}{
		"providers": map[string]interface{}{},
		"total_calls": 0,
	}

	// Count AI-related tool calls from resource_access_log if available.
	var totalCalls int
	err := d.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM resource_access_log WHERE action LIKE 'design.%' OR action LIKE '3d.%' OR action LIKE 'ai.%'`).Scan(&totalCalls)
	if err == nil {
		usage["total_calls"] = totalCalls
	}

	// Per-provider breakdown.
	providers := map[string]int{}
	rows, err := d.db.QueryContext(ctx,
		`SELECT action, COUNT(*) as cnt FROM resource_access_log
		 WHERE action LIKE 'design.%' OR action LIKE '3d.%' OR action LIKE 'ai.%'
		 GROUP BY action`)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var action string
			var cnt int
			if rows.Scan(&action, &cnt) == nil {
				providers[action] = cnt
			}
		}
	}
	usage["providers"] = providers

	writeJSON(w, http.StatusOK, usage)
}

// handleAIProviders returns configured AI providers and their status.
func (d *Dashboard) handleAIProviders(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	// Return static list of known AI providers with stub status.
	// In production, each provider would check its API key in the vault.
	providers := []map[string]interface{}{
		{"name": "canva", "category": "design", "status": "unconfigured"},
		{"name": "figma", "category": "design", "status": "unconfigured"},
		{"name": "meshy", "category": "3d", "status": "unconfigured"},
		{"name": "tripo", "category": "3d", "status": "unconfigured"},
		{"name": "luma", "category": "3d", "status": "unconfigured"},
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"providers": providers})
}
