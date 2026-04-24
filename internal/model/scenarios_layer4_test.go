package model_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/LumabyteCo/aibutler/internal/agent"
	"github.com/LumabyteCo/aibutler/internal/capability"
	"github.com/LumabyteCo/aibutler/internal/mcp"
	mcpserver "github.com/LumabyteCo/aibutler/internal/mcp/server"
	"github.com/LumabyteCo/aibutler/internal/memory"
	"github.com/LumabyteCo/aibutler/internal/memory/digest"
	"github.com/LumabyteCo/aibutler/internal/memory/entity"
	"github.com/LumabyteCo/aibutler/internal/memory/fts"
	"github.com/LumabyteCo/aibutler/internal/memory/graph"
	"github.com/LumabyteCo/aibutler/internal/memory/hybrid"
	"github.com/LumabyteCo/aibutler/internal/memory/migration"
	"github.com/LumabyteCo/aibutler/internal/model"
	"github.com/LumabyteCo/aibutler/internal/prompt"
	"github.com/LumabyteCo/aibutler/internal/protocol/a2a"
	"github.com/LumabyteCo/aibutler/internal/session"
	"github.com/LumabyteCo/aibutler/internal/tool"
	"github.com/LumabyteCo/aibutler/testutil"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// mcpRequest sends a JSON-RPC request via the MCP server's stdio interface
// and returns the parsed response.
func mcpRequest(t *testing.T, srv *mcpserver.Server, method string, params interface{}) mcp.JSONRPCResponse {
	t.Helper()

	var p interface{}
	if params != nil {
		p = params
	}
	req := mcp.JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  method,
		Params:  p,
	}
	reqBytes, _ := json.Marshal(req)
	reqBytes = append(reqBytes, '\n')

	in := bytes.NewReader(reqBytes)
	var out bytes.Buffer

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- srv.Serve(ctx, in, &out)
	}()

	// Wait for response (Serve returns on EOF).
	<-done

	var resp mcp.JSONRPCResponse
	scanner := bufio.NewScanner(&out)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		json.Unmarshal([]byte(line), &resp)
		break
	}
	return resp
}

// newMCPServerWithMemory creates an MCP server backed by real memory tools.
func newMCPServerWithMemory(t *testing.T) (*mcpserver.Server, *memory.Store, *entity.Store, *fts.Store, *graph.Store) {
	t.Helper()
	database := testutil.TestDB(t)
	conn := database.Conn()

	registry := tool.NewRegistry()
	memStore := memory.NewStore(conn)
	memory.RegisterMemoryTools(registry, memStore, nil)

	ftsStore := fts.NewStore(conn)
	entityStore := entity.NewStore(conn)
	graphStore := graph.NewStore(conn)
	hybridSearcher := hybrid.NewSearcher(ftsStore, entityStore)
	memory.RegisterP2MemoryTools(registry, memory.P2Deps{
		FTS: ftsStore, Entity: entityStore, Graph: graphStore, Hybrid: hybridSearcher,
	})

	// Create a ToolProvider adapter from the registry.
	provider := &testToolProvider{registry: registry}
	lister := mcpserver.NewRegistryLister(provider, []string{"memory.read", "memory.write"})
	srv := mcpserver.New(mcp.ServerInfo{Name: "test-butler", Version: "0.1.0"}, lister)

	return srv, memStore, entityStore, ftsStore, graphStore
}

type testToolProvider struct {
	registry *tool.Registry
}

func (p *testToolProvider) All() []mcpserver.ToolEntry {
	tools := p.registry.All()
	entries := make([]mcpserver.ToolEntry, 0, len(tools))
	for _, t := range tools {
		entries = append(entries, mcpserver.ToolEntry{
			Name:        t.Name(),
			Description: t.Description(),
			Schema:      t.Schema(),
			Capability:  t.Capability(),
			Executor:    t,
		})
	}
	return entries
}

// =========================================================================
// GROUP A: MCP Server Tool Exposure (Scenarios 101-108)
// =========================================================================

func TestScenario101_MCPServerInitializeHandshake(t *testing.T) {
	srv, _, _, _, _ := newMCPServerWithMemory(t)
	resp := mcpRequest(t, srv, "initialize", map[string]interface{}{
		"protocolVersion": "2024-11-05",
		"clientInfo":      map[string]string{"name": "test-client"},
	})

	if resp.Error != nil {
		t.Fatalf("error: %s", resp.Error.Message)
	}

	var result struct {
		ProtocolVersion string `json:"protocolVersion"`
		ServerInfo      struct {
			Name    string `json:"name"`
			Version string `json:"version"`
		} `json:"serverInfo"`
	}
	json.Unmarshal(resp.Result, &result)

	if result.ProtocolVersion != "2024-11-05" {
		t.Errorf("protocol = %q", result.ProtocolVersion)
	}
	if result.ServerInfo.Name != "test-butler" {
		t.Errorf("name = %q", result.ServerInfo.Name)
	}
}

func TestScenario102_MCPServerToolsList(t *testing.T) {
	srv, _, _, _, _ := newMCPServerWithMemory(t)
	resp := mcpRequest(t, srv, "tools/list", nil)

	if resp.Error != nil {
		t.Fatalf("error: %s", resp.Error.Message)
	}

	var result mcp.ToolListResult
	json.Unmarshal(resp.Result, &result)

	if len(result.Tools) == 0 {
		t.Fatal("no tools returned")
	}

	// Should have memory tools (memory.search, memory.capture, memory.facts, etc.)
	names := make(map[string]bool)
	for _, tool := range result.Tools {
		names[tool.Name] = true
	}

	for _, expected := range []string{"memory.search", "memory.capture", "memory.facts"} {
		if !names[expected] {
			t.Errorf("missing tool %q", expected)
		}
	}
}

func TestScenario103_MCPServerMemorySearch(t *testing.T) {
	srv, memStore, _, _, _ := newMCPServerWithMemory(t)
	ctx := context.Background()

	// Seed thoughts.
	memStore.SaveThought(ctx, "Alice is working on project Alpha", "terminal", "s1", []string{"project"})
	memStore.SaveThought(ctx, "Budget meeting scheduled for Friday", "terminal", "s1", []string{"meeting"})

	resp := mcpRequest(t, srv, "tools/call", map[string]interface{}{
		"name":      "memory.facts",
		"arguments": map[string]interface{}{},
	})

	if resp.Error != nil {
		t.Fatalf("error: %s", resp.Error.Message)
	}

	var result mcp.ToolCallResult
	json.Unmarshal(resp.Result, &result)
	if len(result.Content) == 0 {
		t.Fatal("no content returned")
	}
}

func TestScenario104_MCPServerFTSSearch(t *testing.T) {
	srv, memStore, _, _, _ := newMCPServerWithMemory(t)
	ctx := context.Background()

	memStore.SaveThought(ctx, "Machine learning transforms data analysis", "terminal", "s1", nil)

	resp := mcpRequest(t, srv, "tools/call", map[string]interface{}{
		"name":      "memory.fts_search",
		"arguments": map[string]interface{}{"query": "machine learning", "limit": 5},
	})

	if resp.Error != nil {
		t.Fatalf("error: %s", resp.Error.Message)
	}

	var result mcp.ToolCallResult
	json.Unmarshal(resp.Result, &result)
	if len(result.Content) == 0 {
		t.Fatal("no FTS results")
	}
}

func TestScenario105_MCPServerEntityQuery(t *testing.T) {
	srv, _, entityStore, _, _ := newMCPServerWithMemory(t)
	ctx := context.Background()

	entityStore.SaveOrUpdate(ctx, entity.TypePerson, "Bob", "s1", map[string]string{"role": "CTO"})

	resp := mcpRequest(t, srv, "tools/call", map[string]interface{}{
		"name":      "memory.people",
		"arguments": map[string]interface{}{"limit": 10},
	})

	if resp.Error != nil {
		t.Fatalf("error: %s", resp.Error.Message)
	}

	var result mcp.ToolCallResult
	json.Unmarshal(resp.Result, &result)
	if len(result.Content) == 0 {
		t.Fatal("no entity results")
	}
	if !strings.Contains(result.Content[0].Text, "Bob") {
		t.Errorf("output missing 'Bob': %s", result.Content[0].Text)
	}
}

func TestScenario106_MCPServerGraphQuery(t *testing.T) {
	srv, _, entityStore, _, _ := newMCPServerWithMemory(t)
	ctx := context.Background()

	entityStore.SaveOrUpdate(ctx, entity.TypePerson, "Alice", "s1", nil)
	entityStore.SaveOrUpdate(ctx, entity.TypeProject, "Butler", "s1", nil)

	resp := mcpRequest(t, srv, "tools/call", map[string]interface{}{
		"name":      "memory.graph",
		"arguments": map[string]interface{}{"entity": "Alice"},
	})

	if resp.Error != nil {
		t.Fatalf("error: %s", resp.Error.Message)
	}

	var result mcp.ToolCallResult
	json.Unmarshal(resp.Result, &result)
	if len(result.Content) == 0 {
		t.Fatal("no graph results")
	}
}

func TestScenario107_MCPServerStatsQuery(t *testing.T) {
	srv, memStore, _, _, _ := newMCPServerWithMemory(t)
	ctx := context.Background()

	memStore.SaveThought(ctx, "Stats test thought", "terminal", "s1", nil)

	resp := mcpRequest(t, srv, "tools/call", map[string]interface{}{
		"name":      "memory.stats",
		"arguments": map[string]interface{}{},
	})

	if resp.Error != nil {
		t.Fatalf("error: %s", resp.Error.Message)
	}

	var result mcp.ToolCallResult
	json.Unmarshal(resp.Result, &result)
	if len(result.Content) == 0 {
		t.Fatal("no stats returned")
	}
}

func TestScenario108_MCPServerRejectsUnauthorizedTool(t *testing.T) {
	// Create a provider with a shell tool not in allowed capabilities.
	database := testutil.TestDB(t)
	conn := database.Conn()

	registry := tool.NewRegistry()
	registry.Register(&tool.FuncTool{
		ToolName: "shell.exec", ToolDesc: "Execute shell", ToolSchema: "{}",
		ToolCap: "tool.shell.exec",
		Exec:    func(_ context.Context, input string) (string, error) { return "executed", nil },
	})
	memory.RegisterMemoryTools(registry, memory.NewStore(conn), nil)

	provider := &testToolProvider{registry: registry}
	// Only allow memory.read — shell.exec should be filtered out.
	lister := mcpserver.NewRegistryLister(provider, []string{"memory.read", "memory.write"})
	srv := mcpserver.New(mcp.ServerInfo{Name: "test", Version: "0.1"}, lister)

	// Verify shell.exec not in tools list.
	resp := mcpRequest(t, srv, "tools/list", nil)
	var result mcp.ToolListResult
	json.Unmarshal(resp.Result, &result)

	for _, t2 := range result.Tools {
		if t2.Name == "shell.exec" {
			t.Error("shell.exec should not be in MCP tools list")
		}
	}

	// Attempt to call it.
	callResp := mcpRequest(t, srv, "tools/call", map[string]interface{}{
		"name":      "shell.exec",
		"arguments": map[string]interface{}{"cmd": "ls"},
	})
	var callResult mcp.ToolCallResult
	json.Unmarshal(callResp.Result, &callResult)
	if len(callResult.Content) > 0 && callResult.Content[0].Text == "executed" {
		t.Error("shell.exec should not have been executed")
	}
}

// =========================================================================
// GROUP B: Memory Import Workflows (Scenarios 109-116)
// =========================================================================

func TestScenario109_ImportClaudeConversation(t *testing.T) {
	database := testutil.TestDB(t)
	conn := database.Conn()
	ctx := context.Background()

	memStore := memory.NewStore(conn)
	entityStore := entity.NewStore(conn)
	orch := migration.NewOrchestrator(conn, memStore, entityStore)

	claudeJSON := `[{"uuid":"c1","name":"AI Chat","chat_messages":[
		{"sender":"human","text":"What is Go?"},
		{"sender":"assistant","text":"Go is a programming language created by Google."}
	]}]`

	imp := &migration.ClaudeImporter{}
	result, err := orch.Run(ctx, imp, strings.NewReader(claudeJSON), migration.ImportOpts{Filename: "claude.json"})
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if result.ThoughtsImported == 0 {
		t.Error("expected thoughts imported > 0")
	}

	// Verify thoughts exist in DB.
	thoughts, _ := memStore.GetThoughts(ctx, memory.ThoughtQuery{Limit: 100})
	if len(thoughts) == 0 {
		t.Error("no thoughts found after import")
	}
}

func TestScenario110_ImportChatGPTConversation(t *testing.T) {
	database := testutil.TestDB(t)
	conn := database.Conn()
	ctx := context.Background()

	memStore := memory.NewStore(conn)
	entityStore := entity.NewStore(conn)
	orch := migration.NewOrchestrator(conn, memStore, entityStore)

	chatgptJSON := `[{"title":"ML Discussion","mapping":{
		"a":{"message":{"author":{"role":"user"},"content":{"parts":["Tell me about neural networks"]}},"children":["b"]},
		"b":{"message":{"author":{"role":"assistant"},"content":{"parts":["Neural networks are computing systems inspired by biological brains."]}},"children":[]}
	}}]`

	imp := &migration.ChatGPTImporter{}
	result, err := orch.Run(ctx, imp, strings.NewReader(chatgptJSON), migration.ImportOpts{Filename: "chatgpt.json"})
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if result.ThoughtsImported == 0 {
		t.Error("expected thoughts imported > 0")
	}
}

func TestScenario111_ImportPlaintext(t *testing.T) {
	database := testutil.TestDB(t)
	conn := database.Conn()
	ctx := context.Background()

	memStore := memory.NewStore(conn)
	entityStore := entity.NewStore(conn)
	orch := migration.NewOrchestrator(conn, memStore, entityStore)

	text := "First paragraph about Go.\n\nSecond paragraph about Rust.\n\nThird paragraph about Python."

	imp := &migration.PlaintextImporter{Filename: "notes.txt"}
	result, err := orch.Run(ctx, imp, strings.NewReader(text), migration.ImportOpts{Filename: "notes.txt"})
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if result.ThoughtsImported != 3 {
		t.Errorf("thoughts = %d, want 3", result.ThoughtsImported)
	}
}

func TestScenario112_ImportDryRun(t *testing.T) {
	database := testutil.TestDB(t)
	conn := database.Conn()
	ctx := context.Background()

	memStore := memory.NewStore(conn)
	entityStore := entity.NewStore(conn)
	orch := migration.NewOrchestrator(conn, memStore, entityStore)

	text := "First paragraph.\n\nSecond paragraph."
	imp := &migration.PlaintextImporter{Filename: "notes.txt"}
	result, err := orch.Run(ctx, imp, strings.NewReader(text), migration.ImportOpts{Filename: "notes.txt", DryRun: true})
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if result.ThoughtsImported != 2 {
		t.Errorf("dry run count = %d, want 2", result.ThoughtsImported)
	}

	// Verify nothing persisted.
	thoughts, _ := memStore.GetThoughts(ctx, memory.ThoughtQuery{Limit: 100})
	if len(thoughts) != 0 {
		t.Errorf("thoughts = %d after dry run, want 0", len(thoughts))
	}
}

func TestScenario113_ImportThenSearchMemory(t *testing.T) {
	database := testutil.TestDB(t)
	conn := database.Conn()
	ctx := context.Background()

	memStore := memory.NewStore(conn)
	entityStore := entity.NewStore(conn)
	ftsStore := fts.NewStore(conn)
	orch := migration.NewOrchestrator(conn, memStore, entityStore)

	claudeJSON := `[{"uuid":"c1","name":"Tech Chat","chat_messages":[
		{"sender":"human","text":"What is Kubernetes?"},
		{"sender":"assistant","text":"Kubernetes is a container orchestration platform for automating deployment."}
	]}]`

	imp := &migration.ClaudeImporter{}
	orch.Run(ctx, imp, strings.NewReader(claudeJSON), migration.ImportOpts{Filename: "claude.json"})

	// Search via FTS for imported content.
	results, err := ftsStore.SearchThoughts(ctx, "Kubernetes", 10)
	if err != nil {
		t.Fatalf("fts: %v", err)
	}
	if len(results) == 0 {
		t.Error("expected FTS results for imported 'Kubernetes' content")
	}
}

func TestScenario114_ImportThenEntityGraph(t *testing.T) {
	database := testutil.TestDB(t)
	conn := database.Conn()
	ctx := context.Background()

	memStore := memory.NewStore(conn)
	entityStore := entity.NewStore(conn)
	orch := migration.NewOrchestrator(conn, memStore, entityStore)

	// Import data mentioning a person.
	text := "Alice Smith is leading the refactoring effort.\n\nBob Jones handles database optimization."
	imp := &migration.PlaintextImporter{Filename: "team.txt"}
	result, err := orch.Run(ctx, imp, strings.NewReader(text), migration.ImportOpts{Filename: "team.txt"})
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if result.ThoughtsImported < 2 {
		t.Errorf("thoughts = %d, want >= 2", result.ThoughtsImported)
	}

	// Verify thoughts were stored.
	thoughts, _ := memStore.GetThoughts(ctx, memory.ThoughtQuery{Limit: 100})
	if len(thoughts) < 2 {
		t.Errorf("persisted thoughts = %d", len(thoughts))
	}
}

func TestScenario115_ImportTrackingRecord(t *testing.T) {
	database := testutil.TestDB(t)
	conn := database.Conn()
	ctx := context.Background()

	memStore := memory.NewStore(conn)
	entityStore := entity.NewStore(conn)
	orch := migration.NewOrchestrator(conn, memStore, entityStore)

	text := "Paragraph one.\n\nParagraph two."
	imp := &migration.PlaintextImporter{Filename: "notes.txt"}
	orch.Run(ctx, imp, strings.NewReader(text), migration.ImportOpts{Filename: "notes.txt"})

	// Verify memory_imports record.
	var source, status string
	var count int
	err := conn.QueryRowContext(ctx, `SELECT source, status, thoughts_imported FROM memory_imports LIMIT 1`).Scan(&source, &status, &count)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if source != "plaintext" {
		t.Errorf("source = %q", source)
	}
	if status != "completed" {
		t.Errorf("status = %q", status)
	}
	if count < 2 {
		t.Errorf("count = %d", count)
	}
}

func TestScenario116_ImportMalformedJSON(t *testing.T) {
	database := testutil.TestDB(t)
	conn := database.Conn()
	ctx := context.Background()

	memStore := memory.NewStore(conn)
	entityStore := entity.NewStore(conn)
	orch := migration.NewOrchestrator(conn, memStore, entityStore)

	imp := &migration.ClaudeImporter{}
	_, err := orch.Run(ctx, imp, strings.NewReader("{invalid json"), migration.ImportOpts{Filename: "bad.json"})
	if err == nil {
		t.Error("expected error for malformed JSON")
	}

	// Verify no partial data.
	thoughts, _ := memStore.GetThoughts(ctx, memory.ThoughtQuery{Limit: 100})
	if len(thoughts) != 0 {
		t.Errorf("thoughts = %d after failed import, want 0", len(thoughts))
	}
}

// =========================================================================
// GROUP C: Memory Digest Generation (Scenarios 117-122)
// =========================================================================

func TestScenario117_WeeklyDigest(t *testing.T) {
	database := testutil.TestDB(t)
	conn := database.Conn()
	ctx := context.Background()

	memStore := memory.NewStore(conn)
	entityStore := entity.NewStore(conn)
	graphStore := graph.NewStore(conn)

	// Seed thoughts.
	memStore.SaveThought(ctx, "Discussed project roadmap with team", "terminal", "s1", []string{"work"})
	memStore.SaveThought(ctx, "Started learning about Kubernetes", "terminal", "s1", []string{"learning"})
	memStore.SaveThought(ctx, "Fixed critical bug in authentication", "terminal", "s1", []string{"bugfix"})

	gen := digest.NewGenerator(conn, memStore, entityStore, graphStore)
	d, err := gen.GenerateWeekly(ctx)
	if err != nil {
		t.Fatalf("weekly: %v", err)
	}
	if d.Title == "" {
		t.Error("empty digest title")
	}
	if d.Content == "" {
		t.Error("empty digest content")
	}
	if d.SourceThoughtCount < 3 {
		t.Errorf("source count = %d, want >= 3", d.SourceThoughtCount)
	}
}

func TestScenario118_TopicDigest(t *testing.T) {
	database := testutil.TestDB(t)
	conn := database.Conn()
	ctx := context.Background()

	memStore := memory.NewStore(conn)
	entityStore := entity.NewStore(conn)
	graphStore := graph.NewStore(conn)

	memStore.SaveThought(ctx, "Machine learning model achieved 95% accuracy", "terminal", "s1", nil)
	memStore.SaveThought(ctx, "Deep learning requires large datasets", "terminal", "s1", nil)

	gen := digest.NewGenerator(conn, memStore, entityStore, graphStore)
	d, err := gen.GenerateTopicDigest(ctx, "learning")
	if err != nil {
		t.Fatalf("topic: %v", err)
	}
	if d.Title == "" {
		t.Error("empty digest title")
	}
	if d.SourceThoughtCount == 0 {
		t.Error("expected source thoughts > 0")
	}
}

func TestScenario119_EntityDigest(t *testing.T) {
	database := testutil.TestDB(t)
	conn := database.Conn()
	ctx := context.Background()

	memStore := memory.NewStore(conn)
	entityStore := entity.NewStore(conn)
	graphStore := graph.NewStore(conn)

	memStore.SaveThought(ctx, "Alice presented the quarterly results", "terminal", "s1", nil)
	memStore.SaveThought(ctx, "Meeting with Alice about API design", "terminal", "s1", nil)
	entityStore.SaveOrUpdate(ctx, entity.TypePerson, "Alice", "s1", map[string]string{"role": "engineer"})

	gen := digest.NewGenerator(conn, memStore, entityStore, graphStore)
	d, err := gen.GenerateEntityDigest(ctx, "Alice")
	if err != nil {
		t.Fatalf("entity digest: %v", err)
	}
	if d.SourceThoughtCount == 0 {
		t.Error("expected source thoughts > 0")
	}
}

func TestScenario120_DigestPersistence(t *testing.T) {
	database := testutil.TestDB(t)
	conn := database.Conn()
	ctx := context.Background()

	memStore := memory.NewStore(conn)
	entityStore := entity.NewStore(conn)
	graphStore := graph.NewStore(conn)

	memStore.SaveThought(ctx, "Test thought for persistence", "terminal", "s1", nil)

	gen := digest.NewGenerator(conn, memStore, entityStore, graphStore)
	d, _ := gen.GenerateWeekly(ctx)
	gen.Save(ctx, d)

	// List and verify.
	digests, err := gen.List(ctx, digest.DigestWeekly, 10)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(digests) != 1 {
		t.Fatalf("digests = %d, want 1", len(digests))
	}
	if digests[0].Title != d.Title {
		t.Errorf("title = %q, want %q", digests[0].Title, d.Title)
	}
}

func TestScenario121_DigestWithNoData(t *testing.T) {
	database := testutil.TestDB(t)
	conn := database.Conn()
	ctx := context.Background()

	gen := digest.NewGenerator(conn, memory.NewStore(conn), entity.NewStore(conn), graph.NewStore(conn))
	d, err := gen.GenerateWeekly(ctx)
	if err != nil {
		t.Fatalf("weekly: %v", err)
	}
	if d.SourceThoughtCount != 0 {
		t.Errorf("source count = %d, want 0 for empty DB", d.SourceThoughtCount)
	}
}

func TestScenario122_DigestAfterImport(t *testing.T) {
	database := testutil.TestDB(t)
	conn := database.Conn()
	ctx := context.Background()

	memStore := memory.NewStore(conn)
	entityStore := entity.NewStore(conn)
	graphStore := graph.NewStore(conn)
	orch := migration.NewOrchestrator(conn, memStore, entityStore)

	// Import ChatGPT data.
	chatgptJSON := `[{"title":"AI Ethics","mapping":{
		"a":{"message":{"author":{"role":"user"},"content":{"parts":["What are the ethical concerns?"]}},"children":["b"]},
		"b":{"message":{"author":{"role":"assistant"},"content":{"parts":["Key ethical concerns include bias, privacy, and accountability."]}},"children":[]}
	}}]`

	imp := &migration.ChatGPTImporter{}
	orch.Run(ctx, imp, strings.NewReader(chatgptJSON), migration.ImportOpts{Filename: "chatgpt.json"})

	// Generate digest — should include imported data.
	gen := digest.NewGenerator(conn, memStore, entityStore, graphStore)
	d, err := gen.GenerateWeekly(ctx)
	if err != nil {
		t.Fatalf("weekly: %v", err)
	}
	if d.SourceThoughtCount == 0 {
		t.Error("digest should reflect imported data")
	}
}

// =========================================================================
// GROUP D: A2A Protocol (Scenarios 123-128)
// =========================================================================

func newA2ATestServer(t *testing.T) (*a2a.Handler, *httptest.Server) {
	t.Helper()
	database := testutil.TestDB(t)
	conn := database.Conn()
	runner := &a2aRunner{output: "task completed"}
	tokenHash := a2a.HashToken("test-token")
	card := a2a.AgentCard{
		Name: "test-butler", Description: "Test agent",
		URL: "http://localhost", Capabilities: []string{"memory.search"}, Version: "0.1",
	}
	handler := a2a.NewHandler(conn, runner, card, []string{tokenHash})
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return handler, srv
}

type a2aRunner struct {
	output string
	err    error
}

func (r *a2aRunner) RunTask(_ context.Context, _ string) (string, error) {
	return r.output, r.err
}

func TestScenario123_A2AAgentCardDiscovery(t *testing.T) {
	_, srv := newA2ATestServer(t)

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
	if card.Name != "test-butler" {
		t.Errorf("name = %q", card.Name)
	}
	if len(card.Capabilities) == 0 {
		t.Error("empty capabilities")
	}
}

func TestScenario124_A2ATaskDelegation(t *testing.T) {
	_, srv := newA2ATestServer(t)

	body := `{"id":"t1","task":"search memory for Alice"}`
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/a2a/tasks", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer test-token")
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Fatalf("status = %d", resp.StatusCode)
	}

	var result a2a.TaskResult
	json.NewDecoder(resp.Body).Decode(&result)
	if result.Status != "completed" {
		t.Errorf("status = %q", result.Status)
	}
	if result.Output != "task completed" {
		t.Errorf("output = %q", result.Output)
	}
}

func TestScenario125_A2ARejectsNoAuth(t *testing.T) {
	_, srv := newA2ATestServer(t)

	body := `{"id":"t1","task":"do something"}`
	resp, err := http.Post(srv.URL+"/a2a/tasks", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", resp.StatusCode)
	}
}

func TestScenario126_A2ARejectsWrongToken(t *testing.T) {
	_, srv := newA2ATestServer(t)

	body := `{"id":"t1","task":"do something"}`
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

func TestScenario127_A2AOutboundDelegation(t *testing.T) {
	// Set up a mock remote agent.
	database := testutil.TestDB(t)
	conn := database.Conn()
	runner := &a2aRunner{output: "remote result"}
	tokenHash := a2a.HashToken("remote-tok")
	card := a2a.AgentCard{Name: "remote-agent", URL: "http://remote"}
	handler := a2a.NewHandler(conn, runner, card, []string{tokenHash})
	remote := httptest.NewServer(handler)
	defer remote.Close()

	// Use client to delegate.
	client := a2a.NewClient(nil, conn)
	result, err := client.Delegate(context.Background(), remote.URL, "remote-tok", "search for Bob")
	if err != nil {
		t.Fatalf("delegate: %v", err)
	}
	if result.Status != "completed" {
		t.Errorf("status = %q", result.Status)
	}
	if result.Output != "remote result" {
		t.Errorf("output = %q", result.Output)
	}
}

func TestScenario128_A2ADelegationLogged(t *testing.T) {
	database := testutil.TestDB(t)
	conn := database.Conn()
	runner := &a2aRunner{output: "ok"}
	tokenHash := a2a.HashToken("tok")
	handler := a2a.NewHandler(conn, runner, a2a.AgentCard{Name: "agent"}, []string{tokenHash})
	srv := httptest.NewServer(handler)
	defer srv.Close()

	body := `{"id":"t1","task":"test task"}`
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/a2a/tasks", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer tok")
	http.DefaultClient.Do(req)

	ctx := context.Background()
	var direction, status string
	err := conn.QueryRowContext(ctx, `SELECT direction, status FROM a2a_delegations LIMIT 1`).Scan(&direction, &status)
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

// =========================================================================
// GROUP E: Cross-Layer Integration (Scenarios 129-138)
// =========================================================================

func TestScenario129_AgentMemorySearchAfterImport(t *testing.T) {
	// Import data, then run agent that searches memory.
	database := testutil.TestDB(t)
	conn := database.Conn()
	ctx := context.Background()

	memStore := memory.NewStore(conn)
	entityStore := entity.NewStore(conn)
	ftsStore := fts.NewStore(conn)
	orch := migration.NewOrchestrator(conn, memStore, entityStore)

	// Import.
	text := "Alice is the lead architect for project Phoenix."
	imp := &migration.PlaintextImporter{Filename: "notes.txt"}
	orch.Run(ctx, imp, strings.NewReader(text), migration.ImportOpts{Filename: "notes.txt"})

	// Verify searchable via FTS.
	results, err := ftsStore.SearchThoughts(ctx, "Phoenix", 10)
	if err != nil {
		t.Fatalf("fts: %v", err)
	}
	if len(results) == 0 {
		t.Error("imported content not found via FTS")
	}

	// Run agent that issues memory.search tool call.
	cfg := testutil.TestConfig()
	fake := testutil.NewFakeModel(
		toolResp([]agent.ToolCall{tc("t1", "memory.fts_search", `{"query":"Phoenix","limit":5}`)}, 50, 10),
		resp("I found info about project Phoenix.", 50, 20),
	)

	sm := session.NewManager(conn, cfg)
	tracker := prompt.NewTracker(conn, cfg)
	composer := prompt.NewComposer(cfg, sm, tracker, conn)

	registry := tool.NewRegistry()
	tool.RegisterDataTools(registry, conn)
	memory.RegisterMemoryTools(registry, memStore, nil)
	hybridSearcher := hybrid.NewSearcher(ftsStore, entityStore)
	graphStore := graph.NewStore(conn)
	memory.RegisterP2MemoryTools(registry, memory.P2Deps{
		FTS: ftsStore, Entity: entityStore, Graph: graphStore, Hybrid: hybridSearcher,
	})
	dispatcher := tool.NewDispatcher(registry, capability.NewEngine(nil), nil)

	factory := model.NewFactory(model.FactoryConfig{
		Composer: composer, Model: fake, Tools: dispatcher,
		Caps: capability.NewCapabilitySet(capability.MessagingDefaults()),
		Tracker: tracker, DB: conn, Config: cfg,
	})

	sessID, _ := sm.Create(ctx, "webchat", "user-1", "default")
	result, err := factory.Run(ctx, sessID, "What do you know about project Phoenix?", "webchat")
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	assertCompleted(t, result)
	assertOutput(t, result, "Phoenix")
}

func TestScenario130_MultiAgentWithMemoryTools(t *testing.T) {
	// ModeMulti: agent issues parallel memory tool calls.
	s := newScenarioWithMemory(t,
		toolResp([]agent.ToolCall{
			tc("t1", "memory.stats", `{}`),
			tc("t2", "memory.people", `{"limit":5}`),
		}, 60, 15),
		resp("Memory has 0 thoughts and no people tracked.", 60, 20),
	)
	r := s.run(t, "Show me memory stats and people")
	assertCompleted(t, r)
	assertToolOutputCount(t, r, 2)
}

func TestScenario131_ImportThenDigestThenSearch(t *testing.T) {
	// Multi-step: import → digest → verify digest reflects imported content.
	database := testutil.TestDB(t)
	conn := database.Conn()
	ctx := context.Background()

	memStore := memory.NewStore(conn)
	entityStore := entity.NewStore(conn)
	graphStore := graph.NewStore(conn)
	orch := migration.NewOrchestrator(conn, memStore, entityStore)

	// Import.
	text := "Weekly standup discussed deployment pipeline improvements.\n\nReview of Q1 metrics shows growth."
	imp := &migration.PlaintextImporter{Filename: "standup.txt"}
	orch.Run(ctx, imp, strings.NewReader(text), migration.ImportOpts{Filename: "standup.txt"})

	// Generate digest.
	gen := digest.NewGenerator(conn, memStore, entityStore, graphStore)
	d, err := gen.GenerateWeekly(ctx)
	if err != nil {
		t.Fatalf("digest: %v", err)
	}
	if d.SourceThoughtCount < 2 {
		t.Errorf("source count = %d", d.SourceThoughtCount)
	}

	// Save digest.
	gen.Save(ctx, d)

	// Verify digest in DB.
	digests, _ := gen.List(ctx, digest.DigestWeekly, 10)
	if len(digests) != 1 {
		t.Fatalf("digests = %d, want 1", len(digests))
	}
}

func TestScenario132_DigestThenAgentReferences(t *testing.T) {
	// Generate digest, then agent searches memory for digest-related topics.
	database := testutil.TestDB(t)
	conn := database.Conn()
	ctx := context.Background()

	memStore := memory.NewStore(conn)
	entityStore := entity.NewStore(conn)
	graphStore := graph.NewStore(conn)

	memStore.SaveThought(ctx, "This week focused on API optimization", "terminal", "s1", nil)
	memStore.SaveThought(ctx, "Performance testing showed 40% improvement", "terminal", "s1", nil)

	gen := digest.NewGenerator(conn, memStore, entityStore, graphStore)
	d, _ := gen.GenerateWeekly(ctx)
	gen.Save(ctx, d)

	// Now search memory for "optimization".
	ftsStore := fts.NewStore(conn)
	results, err := ftsStore.SearchThoughts(ctx, "optimization", 10)
	if err != nil {
		t.Fatalf("fts: %v", err)
	}
	if len(results) == 0 {
		t.Error("expected FTS results for 'optimization'")
	}
}

func TestScenario133_MCPServerExposesAllMemoryTools(t *testing.T) {
	srv, _, _, _, _ := newMCPServerWithMemory(t)
	tools := srv.Tools()

	// Verify we have a decent set of memory tools.
	if len(tools) < 5 {
		t.Errorf("tools = %d, want >= 5 memory tools", len(tools))
	}

	names := make(map[string]bool)
	for _, tool := range tools {
		names[tool.Name] = true
	}

	expected := []string{"memory.search", "memory.capture", "memory.facts", "memory.fts_search", "memory.people"}
	for _, name := range expected {
		if !names[name] {
			t.Errorf("missing MCP tool %q", name)
		}
	}
}

func TestScenario134_A2AAndMCPCoexist(t *testing.T) {
	// Both A2A and MCP can be initialized from the same DB without conflict.
	database := testutil.TestDB(t)
	conn := database.Conn()
	ctx := context.Background()

	// MCP: seed + query.
	memStore := memory.NewStore(conn)
	memStore.SaveThought(ctx, "Shared memory test", "terminal", "s1", nil)

	registry := tool.NewRegistry()
	memory.RegisterMemoryTools(registry, memStore, nil)
	provider := &testToolProvider{registry: registry}
	lister := mcpserver.NewRegistryLister(provider, []string{"memory.read", "memory.write"})
	mcpSrv := mcpserver.New(mcp.ServerInfo{Name: "butler"}, lister)

	tools := mcpSrv.Tools()
	if len(tools) == 0 {
		t.Error("MCP server has no tools")
	}

	// A2A: handler works alongside MCP.
	runner := &a2aRunner{output: "a2a result"}
	handler := a2a.NewHandler(conn, runner, a2a.AgentCard{Name: "butler"}, nil)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/.well-known/agent.json")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Errorf("A2A status = %d", resp.StatusCode)
	}
}

func TestScenario135_SessionContinuityWithImportedMemory(t *testing.T) {
	// Import data, create session, verify thoughts queryable in session context.
	database := testutil.TestDB(t)
	conn := database.Conn()
	ctx := context.Background()
	cfg := testutil.TestConfig()

	memStore := memory.NewStore(conn)
	entityStore := entity.NewStore(conn)
	orch := migration.NewOrchestrator(conn, memStore, entityStore)

	text := "Important: Always use Go modules for dependency management."
	imp := &migration.PlaintextImporter{Filename: "rules.txt"}
	orch.Run(ctx, imp, strings.NewReader(text), migration.ImportOpts{Filename: "rules.txt"})

	// Create session and add a message.
	sm := session.NewManager(conn, cfg)
	sid, _ := sm.Create(ctx, "terminal", "user-1", "dev")
	sm.AddMessage(ctx, sid, agent.Message{Role: "user", Content: "What are the project rules?"})

	// Memory should still be queryable.
	thoughts, _ := memStore.GetThoughts(ctx, memory.ThoughtQuery{Limit: 10})
	if len(thoughts) == 0 {
		t.Error("no thoughts found in session context")
	}
}

func TestScenario136_CostTrackingWithMemoryTools(t *testing.T) {
	// Agent uses memory tools → cost tracker records usage.
	database := testutil.TestDB(t)
	conn := database.Conn()
	ctx := context.Background()
	cfg := testutil.TestConfig()

	memStore := memory.NewStore(conn)
	memStore.SaveThought(ctx, "Test thought for cost tracking", "terminal", "s1", nil)

	fake := testutil.NewFakeModel(
		toolResp([]agent.ToolCall{tc("t1", "memory.search", `{"query":"test","limit":5}`)}, 100, 30),
		resp("Found one thought about testing.", 100, 20),
	)

	sm := session.NewManager(conn, cfg)
	tracker := prompt.NewTracker(conn, cfg)
	composer := prompt.NewComposer(cfg, sm, tracker, conn)

	registry := tool.NewRegistry()
	tool.RegisterDataTools(registry, conn)
	memory.RegisterMemoryTools(registry, memStore, nil)
	ftsStore := fts.NewStore(conn)
	entityStore := entity.NewStore(conn)
	graphStore := graph.NewStore(conn)
	hybridSearcher := hybrid.NewSearcher(ftsStore, entityStore)
	memory.RegisterP2MemoryTools(registry, memory.P2Deps{
		FTS: ftsStore, Entity: entityStore, Graph: graphStore, Hybrid: hybridSearcher,
	})
	dispatcher := tool.NewDispatcher(registry, capability.NewEngine(nil), nil)

	factory := model.NewFactory(model.FactoryConfig{
		Composer: composer, Model: fake, Tools: dispatcher,
		Caps: capability.NewCapabilitySet(capability.MessagingDefaults()),
		Tracker: tracker, DB: conn, Config: cfg,
	})

	sessID, _ := sm.Create(ctx, "webchat", "user-1", "default")
	result, err := factory.Run(ctx, sessID, "Search my memory for test", "webchat")
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	assertCompleted(t, result)

	// Verify cost was tracked.
	usage, _ := tracker.MonthlyUsage(ctx)
	if usage <= 0 {
		t.Error("expected usage > 0 after agent run")
	}
}

func TestScenario137_ImportMultipleFormats(t *testing.T) {
	// Import from multiple sources into the same DB.
	database := testutil.TestDB(t)
	conn := database.Conn()
	ctx := context.Background()

	memStore := memory.NewStore(conn)
	entityStore := entity.NewStore(conn)
	orch := migration.NewOrchestrator(conn, memStore, entityStore)

	// Import plaintext.
	text := "First source paragraph."
	imp1 := &migration.PlaintextImporter{Filename: "notes.txt"}
	r1, _ := orch.Run(ctx, imp1, strings.NewReader(text), migration.ImportOpts{Filename: "notes.txt"})

	// Import Claude.
	claudeJSON := `[{"uuid":"c1","name":"Chat","chat_messages":[
		{"sender":"human","text":"Hi"},
		{"sender":"assistant","text":"Hello!"}
	]}]`
	imp2 := &migration.ClaudeImporter{}
	r2, _ := orch.Run(ctx, imp2, strings.NewReader(claudeJSON), migration.ImportOpts{Filename: "claude.json"})

	total := r1.ThoughtsImported + r2.ThoughtsImported
	if total < 2 {
		t.Errorf("total imported = %d, want >= 2", total)
	}

	// All should be queryable.
	thoughts, _ := memStore.GetThoughts(ctx, memory.ThoughtQuery{Limit: 100})
	if len(thoughts) < 2 {
		t.Errorf("thoughts = %d, want >= 2", len(thoughts))
	}

	// Import tracking should have 2 records.
	var importCount int
	conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM memory_imports`).Scan(&importCount)
	if importCount != 2 {
		t.Errorf("import records = %d, want 2", importCount)
	}
}

func TestScenario138_FullPipelineStressTest(t *testing.T) {
	// Multi-step: import → search → create thought → search again → digest → A2A.
	database := testutil.TestDB(t)
	conn := database.Conn()
	ctx := context.Background()

	memStore := memory.NewStore(conn)
	entityStore := entity.NewStore(conn)
	graphStore := graph.NewStore(conn)
	ftsStore := fts.NewStore(conn)
	orch := migration.NewOrchestrator(conn, memStore, entityStore)

	// Step 1: Import.
	text := "The system architecture uses event sourcing.\n\nMicroservices communicate via gRPC."
	imp := &migration.PlaintextImporter{Filename: "arch.txt"}
	result, err := orch.Run(ctx, imp, strings.NewReader(text), migration.ImportOpts{Filename: "arch.txt"})
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if result.ThoughtsImported < 2 {
		t.Fatalf("import count = %d", result.ThoughtsImported)
	}

	// Step 2: FTS search.
	ftsResults, err := ftsStore.SearchThoughts(ctx, "event sourcing", 10)
	if err != nil {
		t.Fatalf("fts: %v", err)
	}
	if len(ftsResults) == 0 {
		t.Fatal("FTS: no results for 'event sourcing'")
	}

	// Step 3: Create new thought via memory store.
	memStore.SaveThought(ctx, "Decision: migrate to Kubernetes in Q2", "terminal", "s1", []string{"decision"})

	// Step 4: Search again.
	results2, _ := ftsStore.SearchThoughts(ctx, "Kubernetes", 10)
	if len(results2) == 0 {
		t.Fatal("FTS: no results for new thought")
	}

	// Step 5: Generate digest.
	gen := digest.NewGenerator(conn, memStore, entityStore, graphStore)
	d, err := gen.GenerateWeekly(ctx)
	if err != nil {
		t.Fatalf("digest: %v", err)
	}
	if d.SourceThoughtCount < 3 {
		t.Errorf("digest sources = %d, want >= 3", d.SourceThoughtCount)
	}
	gen.Save(ctx, d)

	// Step 6: A2A delegation.
	runner := &a2aRunner{output: "delegation ok"}
	tokenHash := a2a.HashToken("stress-tok")
	handler := a2a.NewHandler(conn, runner, a2a.AgentCard{Name: "butler"}, []string{tokenHash})
	srv := httptest.NewServer(handler)
	defer srv.Close()

	client := a2a.NewClient(nil, conn)
	a2aResult, err := client.Delegate(ctx, srv.URL, "stress-tok", "search architecture")
	if err != nil {
		t.Fatalf("a2a: %v", err)
	}
	if a2aResult.Status != "completed" {
		t.Errorf("a2a status = %q", a2aResult.Status)
	}

	// Final verification: memory_imports + memory_digests + a2a_delegations all have records.
	var imports, digests, delegations int
	conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM memory_imports`).Scan(&imports)
	conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM memory_digests`).Scan(&digests)
	conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM a2a_delegations`).Scan(&delegations)

	if imports < 1 {
		t.Error("no import records")
	}
	if digests < 1 {
		t.Error("no digest records")
	}
	if delegations < 1 {
		t.Error("no delegation records")
	}

	fmt.Printf("Full pipeline: %d imports, %d digests, %d delegations, %d thoughts\n",
		imports, digests, delegations, d.SourceThoughtCount)
}
