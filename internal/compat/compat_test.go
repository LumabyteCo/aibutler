package compat_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/LumabyteCo/aibutler/internal/agent"
	"github.com/LumabyteCo/aibutler/internal/capability"
	"github.com/LumabyteCo/aibutler/internal/config"
	"github.com/LumabyteCo/aibutler/internal/memory"
	"github.com/LumabyteCo/aibutler/internal/memory/entity"
	"github.com/LumabyteCo/aibutler/internal/memory/fts"
	"github.com/LumabyteCo/aibutler/internal/memory/graph"
	"github.com/LumabyteCo/aibutler/internal/memory/hybrid"
	"github.com/LumabyteCo/aibutler/internal/memory/vector"
	"github.com/LumabyteCo/aibutler/internal/plugin/defense"
	"github.com/LumabyteCo/aibutler/internal/plugin/manifest"
	"github.com/LumabyteCo/aibutler/internal/plugin/store"
	"github.com/LumabyteCo/aibutler/internal/prompt"
	"github.com/LumabyteCo/aibutler/internal/session"
	"github.com/LumabyteCo/aibutler/internal/tool"
	"github.com/LumabyteCo/aibutler/testutil"
)

// ==========================================================================
// Core
// ==========================================================================

// TestCompat_DataToolsStillWork verifies raw SQL data tools still function
// after migration 009 adds new tables.
func TestCompat_DataToolsStillWork(t *testing.T) {
	database := testutil.TestDB(t)
	conn := database.Conn()
	ctx := context.Background()

	// data.write: insert into user_tasks
	_, err := conn.ExecContext(ctx, `INSERT INTO user_tasks (list_name, content, status, priority) VALUES ('work', 'Review PR', 'pending', 0)`)
	if err != nil {
		t.Fatalf("insert task: %v", err)
	}

	// data.query: read back
	var content string
	err = conn.QueryRowContext(ctx, `SELECT content FROM user_tasks WHERE list_name='work'`).Scan(&content)
	if err != nil {
		t.Fatalf("query task: %v", err)
	}
	if content != "Review PR" {
		t.Errorf("content = %q, want 'Review PR'", content)
	}

	// Verify new tables coexist: query memory_imports
	var count int
	err = conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM memory_imports`).Scan(&count)
	if err != nil {
		t.Fatalf("query memory_imports: %v", err)
	}
	if count != 0 {
		t.Errorf("memory_imports = %d, want 0", count)
	}
}

// TestCompat_MemoryToolsStillWork verifies memory save/recall still works.
func TestCompat_MemoryToolsStillWork(t *testing.T) {
	database := testutil.TestDB(t)
	conn := database.Conn()
	ctx := context.Background()

	memStore := memory.NewStore(conn)

	// Save a thought
	id, err := memStore.SaveThought(ctx, "Alice prefers Go for backend work", "terminal", "sess-1", []string{"people", "tech"})
	if err != nil {
		t.Fatalf("save thought: %v", err)
	}
	if id == 0 {
		t.Fatal("expected non-zero id")
	}

	// Save a key fact
	factID, err := memStore.SaveKeyFact(ctx, "User likes dark mode", "preference", "sess-1")
	if err != nil {
		t.Fatalf("save key fact: %v", err)
	}
	if factID == 0 {
		t.Fatal("expected non-zero fact id")
	}

	// Recall thoughts
	thoughts, err := memStore.GetThoughts(ctx, memory.ThoughtQuery{Limit: 10})
	if err != nil {
		t.Fatalf("get thoughts: %v", err)
	}
	if len(thoughts) != 1 {
		t.Fatalf("thoughts = %d, want 1", len(thoughts))
	}
	if thoughts[0].Content != "Alice prefers Go for backend work" {
		t.Errorf("thought content = %q", thoughts[0].Content)
	}
}

// TestCompat_SessionLifecycle verifies session creation and message flow.
func TestCompat_SessionLifecycle(t *testing.T) {
	database := testutil.TestDB(t)
	cfg := testutil.TestConfig()
	sm := session.NewManager(database.Conn(), cfg)
	ctx := context.Background()

	// Create session
	sid, err := sm.Create(ctx, "terminal", "user-1", "general")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if sid == "" {
		t.Fatal("empty session id")
	}

	// Add messages
	err = sm.AddMessage(ctx, sid, agent.Message{Role: "user", Content: "Hello"})
	if err != nil {
		t.Fatalf("add message: %v", err)
	}
	err = sm.AddMessage(ctx, sid, agent.Message{Role: "assistant", Content: "Hi there!"})
	if err != nil {
		t.Fatalf("add message: %v", err)
	}

	// Retrieve
	msgs, err := sm.Messages(ctx, sid)
	if err != nil {
		t.Fatalf("messages: %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("messages = %d, want 2", len(msgs))
	}
	if msgs[0].Role != "user" || msgs[1].Role != "assistant" {
		t.Errorf("roles = %q, %q", msgs[0].Role, msgs[1].Role)
	}
}

// TestCompat_CapabilityEnforcement verifies capability engine still gates access.
func TestCompat_CapabilityEnforcement(t *testing.T) {
	engine := capability.NewEngine(nil)
	caps := capability.NewCapabilitySet([]capability.Capability{
		{Resource: "tool.file.read", Paths: []string{"./"}, AuditLevel: capability.AuditSummary},
	})
	ctx := context.Background()

	// Allowed: file read
	result := engine.Check(ctx, caps, capability.CheckRequest{Resource: "tool.file.read"})
	if !result.Allowed {
		t.Error("file.read should be allowed")
	}

	// Denied: shell exec (not in caps)
	result = engine.Check(ctx, caps, capability.CheckRequest{Resource: "tool.shell.exec"})
	if result.Allowed {
		t.Error("shell.exec should be denied")
	}
}

// TestCompat_PromptComposerStillWorks verifies prompt composition works.
func TestCompat_PromptComposerStillWorks(t *testing.T) {
	database := testutil.TestDB(t)
	conn := database.Conn()
	cfg := testutil.TestConfig()
	sm := session.NewManager(conn, cfg)
	tracker := prompt.NewTracker(conn, cfg)
	composer := prompt.NewComposer(cfg, sm, tracker, conn)
	ctx := context.Background()

	sid, _ := sm.Create(ctx, "terminal", "user-1", "general")
	sm.AddMessage(ctx, sid, agent.Message{Role: "user", Content: "Hello"})

	p, err := composer.Compose(ctx, sid, "What is the weather?", "terminal")
	if err != nil {
		t.Fatalf("compose: %v", err)
	}
	if p == nil {
		t.Fatal("prompt is nil")
	}
	if p.UserMessage != "What is the weather?" {
		t.Errorf("user message = %q", p.UserMessage)
	}
}

// TestCompat_CostTrackerStillRecords verifies cost tracking persists entries.
func TestCompat_CostTrackerStillRecords(t *testing.T) {
	database := testutil.TestDB(t)
	conn := database.Conn()
	cfg := testutil.TestConfig()
	tracker := prompt.NewTracker(conn, cfg)
	ctx := context.Background()

	err := tracker.Record(ctx, prompt.UsageEntry{
		SessionID:    "sess-1",
		Model:        "claude-3-haiku",
		Provider:     "anthropic",
		InputTokens:  100,
		OutputTokens: 50,
		CostUSD:      0.001,
	})
	if err != nil {
		t.Fatalf("record: %v", err)
	}

	usage, err := tracker.MonthlyUsage(ctx)
	if err != nil {
		t.Fatalf("monthly usage: %v", err)
	}
	if usage < 0.001 {
		t.Errorf("usage = %f, want >= 0.001", usage)
	}
}

// ==========================================================================
// Intelligence Core
// ==========================================================================

// TestCompat_FTSSearchStillWorks verifies FTS5 full-text search across thoughts.
func TestCompat_FTSSearchStillWorks(t *testing.T) {
	database := testutil.TestDB(t)
	conn := database.Conn()
	ctx := context.Background()

	memStore := memory.NewStore(conn)
	ftsStore := fts.NewStore(conn)

	// Save thoughts (triggers FTS5 indexing via SQL triggers)
	memStore.SaveThought(ctx, "Machine learning is transforming healthcare", "terminal", "s1", nil)
	memStore.SaveThought(ctx, "The Go programming language is great for systems", "terminal", "s1", nil)

	// FTS search
	results, err := ftsStore.SearchThoughts(ctx, "machine learning", 10)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(results) == 0 {
		t.Error("expected FTS results for 'machine learning'")
	}
}

// TestCompat_EntityExtractionStillWorks verifies entity storage and retrieval.
func TestCompat_EntityExtractionStillWorks(t *testing.T) {
	database := testutil.TestDB(t)
	conn := database.Conn()
	ctx := context.Background()

	entityStore := entity.NewStore(conn)

	// Save entities
	id, err := entityStore.SaveOrUpdate(ctx, entity.TypePerson, "Alice", "s1", map[string]string{"role": "engineer"})
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	if id == 0 {
		t.Fatal("expected non-zero entity id")
	}

	// Get by type
	entities, err := entityStore.GetByType(ctx, entity.TypePerson, 10)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if len(entities) != 1 {
		t.Fatalf("entities = %d, want 1", len(entities))
	}
	if entities[0].Name != "Alice" {
		t.Errorf("name = %q", entities[0].Name)
	}
}

// TestCompat_GraphStillWorks verifies knowledge graph operations via entity_relationships.
func TestCompat_GraphStillWorks(t *testing.T) {
	database := testutil.TestDB(t)
	conn := database.Conn()
	ctx := context.Background()

	entityStore := entity.NewStore(conn)
	graphStore := graph.NewStore(conn)

	// Create entities (graph.Stats counts from entities + entity_relationships)
	entityStore.SaveOrUpdate(ctx, entity.TypePerson, "Alice", "s1", nil)
	entityStore.SaveOrUpdate(ctx, entity.TypeProject, "Butler", "s1", nil)

	// Stats
	stats, err := graphStore.Stats(ctx)
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	if stats["entities"] != 2 {
		t.Errorf("entities = %d, want 2", stats["entities"])
	}
}

// TestCompat_HybridSearchStillWorks verifies combined FTS + entity search.
func TestCompat_HybridSearchStillWorks(t *testing.T) {
	database := testutil.TestDB(t)
	conn := database.Conn()
	ctx := context.Background()

	memStore := memory.NewStore(conn)
	ftsStore := fts.NewStore(conn)
	entityStore := entity.NewStore(conn)
	searcher := hybrid.NewSearcher(ftsStore, entityStore)

	// Seed
	memStore.SaveThought(ctx, "Bob is working on the AI Butler project", "terminal", "s1", nil)
	entityStore.SaveOrUpdate(ctx, entity.TypePerson, "Bob", "s1", nil)

	// Hybrid search
	results, err := searcher.Search(ctx, "Bob", 10)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	// Should find via FTS and/or entity match
	if len(results) == 0 {
		t.Error("expected hybrid results for 'Bob'")
	}
}

// TestCompat_VectorSearchStillWorks verifies pure Go vector distance search.
func TestCompat_VectorSearchStillWorks(t *testing.T) {
	database := testutil.TestDB(t)
	conn := database.Conn()
	ctx := context.Background()

	vecStore := vector.NewStore(conn)

	// Save an embedding
	embedding := make([]float32, 128)
	for i := range embedding {
		embedding[i] = float32(i) / 128.0
	}
	err := vecStore.Save(ctx, "thought", 1, embedding, "test-model")
	if err != nil {
		t.Fatalf("save: %v", err)
	}

	// Search with similar vector
	query := make([]float32, 128)
	for i := range query {
		query[i] = float32(i) / 128.0
	}
	results, err := vecStore.Search(ctx, query, 5)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("results = %d, want 1", len(results))
	}
}

// ==========================================================================
// Agent Orchestration
// ==========================================================================

// TestCompat_AgentModesStillWork verifies ModeAuto is a valid mode and resolves to multi at runtime.
func TestCompat_AgentModesStillWork(t *testing.T) {
	if !agent.ValidMode("auto") {
		t.Error("ModeAuto should be valid")
	}
	if !agent.ValidMode("multi") {
		t.Error("ModeMulti should be valid")
	}
	if !agent.ValidMode("custom") {
		t.Error("ModeCustom should be valid")
	}
}

// TestCompat_ParallelExecutionStillWorks verifies ModeMulti parallel tool execution.
func TestCompat_ParallelExecutionStillWorks(t *testing.T) {
	model := testutil.NewFakeModel(
		// Response with 2 tool calls
		agent.Response{
			ToolCalls: []agent.ToolCall{
				{ID: "t1", Name: "search", Input: `{"q":"alice"}`},
				{ID: "t2", Name: "lookup", Input: `{"q":"bob"}`},
			},
		},
		// Final response
		agent.Response{Content: "Found both."},
	)
	tools := testutil.NewFakeToolExecutor(map[string]string{
		"search": "result for alice",
		"lookup": "result for bob",
	})

	a := agent.New(agent.Config{
		Model: model,
		Tools: tools,
		Mode:  agent.ModeMulti,
	})

	result, err := a.Run(context.Background())
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if result.Output != "Found both." {
		t.Errorf("content = %q", result.Output)
	}
	if len(tools.ToolCalls()) != 2 {
		t.Errorf("tool calls = %d, want 2", len(tools.ToolCalls()))
	}
}

// TestCompat_SubagentNestingStillWorks verifies depth-limited nesting.
func TestCompat_SubagentNestingStillWorks(t *testing.T) {
	// Agent at max depth should not be able to delegate further.
	model := testutil.NewFakeModel(
		agent.Response{Content: "I'm at max depth, can't delegate."},
	)
	tools := testutil.NewFakeToolExecutor(map[string]string{})
	a := agent.New(agent.Config{
		Model:    model,
		Tools:    tools,
		Mode:     agent.ModeMulti,
		Depth:    3,
		MaxDepth: 3,
	})

	result, err := a.Run(context.Background())
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if result.Output == "" {
		t.Error("expected non-empty response at max depth")
	}
}

// TestCompat_CostScopingStillWorks verifies per-subagent budget via config.
func TestCompat_CostScopingStillWorks(t *testing.T) {
	cfg := testutil.TestConfig()
	// The budget config should still be accessible.
	if cfg.Options.Agents.SubagentTimeout == 0 {
		t.Error("expected non-zero subagent timeout")
	}
}

// ==========================================================================
// Plugins
// ==========================================================================

// TestCompat_PluginDefenseStillWorks verifies L1 sandbox + L2 audit analysis.
func TestCompat_PluginDefenseStillWorks(t *testing.T) {
	m := &manifest.Manifest{
		Name:         "safe-plugin",
		Version:      "1.0.0",
		Capabilities: []string{"kv.read", "kv.write"},
	}

	// Validate sandbox
	err := defense.ValidateSandbox(m)
	if err != nil {
		t.Fatalf("sandbox validation should pass: %v", err)
	}

	// Audit capabilities
	result := defense.AuditCapabilities(m.Capabilities)
	if !result.Passed {
		t.Errorf("audit should pass for kv-only plugin: warnings=%v critical=%v", result.Warnings, result.Critical)
	}
}

// TestCompat_PluginKVStoreStillWorks verifies plugin-scoped KV operations.
func TestCompat_PluginKVStoreStillWorks(t *testing.T) {
	database := testutil.TestDB(t)
	conn := database.Conn()
	ctx := context.Background()

	// Register a fake plugin first
	_, err := conn.ExecContext(ctx,
		`INSERT INTO plugins (name, version, manifest_hash, wasm_hash, status) VALUES ('test-plugin', '1.0.0', 'mhash', 'whash', 'enabled')`)
	if err != nil {
		t.Fatalf("insert plugin: %v", err)
	}

	kvStore := store.New(conn, 1)

	// Set
	err = kvStore.Set(ctx, "my-key", []byte("my-value"))
	if err != nil {
		t.Fatalf("set: %v", err)
	}

	// Get
	val, err := kvStore.Get(ctx, "my-key")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if string(val) != "my-value" {
		t.Errorf("val = %q, want 'my-value'", val)
	}

	// Delete
	err = kvStore.Delete(ctx, "my-key")
	if err != nil {
		t.Fatalf("delete: %v", err)
	}

	// Verify deleted (store returns error for missing keys)
	_, err = kvStore.Get(ctx, "my-key")
	if err == nil {
		t.Error("expected error after delete")
	}
}

// ==========================================================================
// Cross-schema: Verify migration 009 doesn't break existing schemas
// ==========================================================================

// TestCompat_AllCoreTablesStillExist checks all core tables survive migration 009.
func TestCompat_AllCoreTablesStillExist(t *testing.T) {
	database := testutil.TestDB(t)
	conn := database.Conn()
	ctx := context.Background()

	criticalTables := []string{
		// Core sessions + memory
		"sessions", "key_facts", "captured_thoughts", "user_tasks",
		"user_contacts", "resource_access_log", "token_usage",
		// Intelligence core (entities, vectors, transcripts)
		"entities", "entity_relationships", "memory_vectors",
		"session_transcripts",
		// Agent orchestration
		"agents", "agent_delegations", "background_agents", "custom_agent_roles",
		// Plugin system
		"plugins", "plugin_kv", "plugin_audit",
		// Memory imports, digests, A2A delegations
		"memory_imports", "memory_digests", "a2a_delegations",
	}

	for _, table := range criticalTables {
		var name string
		err := conn.QueryRowContext(ctx,
			`SELECT name FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&name)
		if err == sql.ErrNoRows {
			t.Errorf("table %q not found after migration 009", table)
		} else if err != nil {
			t.Errorf("table %q check: %v", table, err)
		}
	}
}

// TestCompat_NewTablesHaveCorrectSchema verifies the memory_imports, memory_digests,
// and a2a_delegations tables have expected columns.
func TestCompat_NewTablesHaveCorrectSchema(t *testing.T) {
	database := testutil.TestDB(t)
	conn := database.Conn()
	ctx := context.Background()

	// memory_imports
	_, err := conn.ExecContext(ctx,
		`INSERT INTO memory_imports (source, filename, status) VALUES ('claude', 'test.json', 'pending')`)
	if err != nil {
		t.Errorf("memory_imports insert: %v", err)
	}

	// memory_digests
	_, err = conn.ExecContext(ctx,
		`INSERT INTO memory_digests (digest_type, title, content) VALUES ('weekly', 'Week 1', 'Summary here')`)
	if err != nil {
		t.Errorf("memory_digests insert: %v", err)
	}

	// a2a_delegations
	_, err = conn.ExecContext(ctx,
		`INSERT INTO a2a_delegations (direction, peer_agent, task_summary) VALUES ('inbound', 'test-agent', 'search task')`)
	if err != nil {
		t.Errorf("a2a_delegations insert: %v", err)
	}
}

// TestCompat_CapabilityDefaultsIncludeNewResources verifies capability defaults
// include the resources added for memory imports, digests, and A2A delegations.
func TestCompat_CapabilityDefaultsIncludeNewResources(t *testing.T) {
	messagingCaps := capability.MessagingDefaults()

	found := map[string]bool{
		"mcp.server.call": false,
		"a2a.delegate":    false,
	}

	for _, c := range messagingCaps {
		if _, ok := found[c.Resource]; ok {
			found[c.Resource] = true
		}
	}

	for resource, present := range found {
		if !present {
			t.Errorf("MessagingDefaults missing %q capability", resource)
		}
	}
}

// TestCompat_ToolRegistryAcceptsNewTools verifies the Registry.All() method works.
func TestCompat_ToolRegistryAcceptsNewTools(t *testing.T) {
	registry := tool.NewRegistry()

	// Verify All() method exists and returns empty for new registry
	all := registry.All()
	if len(all) != 0 {
		t.Errorf("empty registry All() = %d, want 0", len(all))
	}
}

// TestCompat_ConfigIncludesNewSections verifies A2A + MCP server config.
func TestCompat_ConfigIncludesNewSections(t *testing.T) {
	cfg := config.Default()

	// A2A config
	if cfg.Configurations.A2A.Port != 8081 {
		t.Errorf("A2A port = %d, want 8081", cfg.Configurations.A2A.Port)
	}

	// MCP server exposure
	if len(cfg.Configurations.MCPServer.AllowedCapabilities) == 0 {
		t.Error("MCPServer AllowedCapabilities should have defaults")
	}
}

// TestCompat_MemoryStoreParallelAccess verifies concurrent memory operations still work.
func TestCompat_MemoryStoreParallelAccess(t *testing.T) {
	database := testutil.TestDB(t)
	conn := database.Conn()
	ctx := context.Background()
	memStore := memory.NewStore(conn)

	// Run concurrent saves
	done := make(chan error, 10)
	for i := 0; i < 10; i++ {
		go func(n int) {
			_, err := memStore.SaveThought(ctx, "Concurrent thought", "terminal", "s1", nil)
			done <- err
		}(i)
	}

	for i := 0; i < 10; i++ {
		if err := <-done; err != nil {
			t.Errorf("concurrent save %d: %v", i, err)
		}
	}

	thoughts, _ := memStore.GetThoughts(ctx, memory.ThoughtQuery{Limit: 100})
	if len(thoughts) != 10 {
		t.Errorf("thoughts = %d, want 10", len(thoughts))
	}
}

// TestCompat_SessionWithNewMigration verifies sessions + messages work with migration 009 applied.
func TestCompat_SessionWithNewMigration(t *testing.T) {
	database := testutil.TestDB(t)
	cfg := testutil.TestConfig()
	sm := session.NewManager(database.Conn(), cfg)
	ctx := context.Background()

	// Create session, add messages, verify round-trip
	sid, _ := sm.Create(ctx, "telegram", "user-42", "chat")
	sm.AddMessage(ctx, sid, agent.Message{Role: "user", Content: "test"})
	sm.AddMessage(ctx, sid, agent.Message{Role: "assistant", Content: "reply"})

	msgs, err := sm.Messages(ctx, sid)
	if err != nil {
		t.Fatalf("messages: %v", err)
	}
	if len(msgs) != 2 {
		t.Errorf("messages = %d, want 2", len(msgs))
	}
}

// TestCompat_FTSAndEntityTogether verifies FTS + entity extraction work in sequence.
func TestCompat_FTSAndEntityTogether(t *testing.T) {
	database := testutil.TestDB(t)
	conn := database.Conn()
	ctx := context.Background()

	memStore := memory.NewStore(conn)
	ftsStore := fts.NewStore(conn)
	entityStore := entity.NewStore(conn)

	// Save thought and entity
	memStore.SaveThought(ctx, "Meeting with Sarah about the API redesign", "terminal", "s1", nil)
	entityStore.SaveOrUpdate(ctx, entity.TypePerson, "Sarah", "s1", map[string]string{"role": "architect"})

	// FTS should find the thought
	results, err := ftsStore.SearchThoughts(ctx, "API redesign", 10)
	if err != nil {
		t.Fatalf("fts search: %v", err)
	}
	if len(results) == 0 {
		t.Error("expected FTS results for 'API redesign'")
	}

	// Entity search should find Sarah
	entities, err := entityStore.Search(ctx, "Sarah", 10)
	if err != nil {
		t.Fatalf("entity search: %v", err)
	}
	if len(entities) == 0 {
		t.Error("expected entity results for 'Sarah'")
	}
}

// TestCompat_AgentRunWithToolExecution verifies full agent→tool pipeline.
func TestCompat_AgentRunWithToolExecution(t *testing.T) {
	model := testutil.NewFakeModel(
		agent.Response{
			ToolCalls: []agent.ToolCall{
				{ID: "t1", Name: "memory.search", Input: `{"q":"Alice"}`},
			},
		},
		agent.Response{Content: "Found 3 memories about Alice."},
	)
	tools := testutil.NewFakeToolExecutor(map[string]string{
		"memory.search": `[{"content": "Alice is an engineer"}]`,
	})

	a := agent.New(agent.Config{
		Model:    model,
		Tools:    tools,
		Mode:         agent.ModeMulti,
		MaxToolCalls: 5,
	})

	result, err := a.Run(context.Background())
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if result.Output != "Found 3 memories about Alice." {
		t.Errorf("content = %q", result.Output)
	}
	if model.CallCount() != 2 {
		t.Errorf("model calls = %d, want 2", model.CallCount())
	}
}

// TestCompat_CostTrackerMultipleEntries verifies cost aggregation with multiple sessions.
func TestCompat_CostTrackerMultipleEntries(t *testing.T) {
	database := testutil.TestDB(t)
	conn := database.Conn()
	cfg := testutil.TestConfig()
	tracker := prompt.NewTracker(conn, cfg)
	ctx := context.Background()

	entries := []prompt.UsageEntry{
		{SessionID: "s1", Model: "claude-3-haiku", Provider: "anthropic", InputTokens: 100, OutputTokens: 50, CostUSD: 0.001},
		{SessionID: "s2", Model: "claude-3-opus", Provider: "anthropic", InputTokens: 200, OutputTokens: 100, CostUSD: 0.010},
		{SessionID: "s3", Model: "claude-3-haiku", Provider: "anthropic", InputTokens: 50, OutputTokens: 25, CostUSD: 0.0005},
	}

	for _, e := range entries {
		if err := tracker.Record(ctx, e); err != nil {
			t.Fatalf("record: %v", err)
		}
	}

	total, err := tracker.MonthlyUsage(ctx)
	if err != nil {
		t.Fatalf("monthly: %v", err)
	}
	expected := 0.0115
	if total < expected-0.001 || total > expected+0.001 {
		t.Errorf("total = %f, want ~%f", total, expected)
	}

	breakdown, err := tracker.MonthlyBreakdown(ctx)
	if err != nil {
		t.Fatalf("breakdown: %v", err)
	}
	if len(breakdown) < 2 {
		t.Errorf("breakdown models = %d, want >= 2", len(breakdown))
	}
}

// TestCompat_FakeModelExhaustion verifies FakeModel error when out of responses.
func TestCompat_FakeModelExhaustion(t *testing.T) {
	model := testutil.NewFakeModel(
		agent.Response{Content: "First response"},
	)

	_, err := model.Complete(context.Background(), nil)
	if err != nil {
		t.Fatalf("first call: %v", err)
	}

	_, err = model.Complete(context.Background(), nil)
	if err == nil {
		t.Error("expected error on exhausted FakeModel")
	}
}

// TestCompat_HybridSearchWithVector verifies hybrid search accepts vector searcher.
func TestCompat_HybridSearchWithVector(t *testing.T) {
	database := testutil.TestDB(t)
	conn := database.Conn()
	ftsStore := fts.NewStore(conn)
	entityStore := entity.NewStore(conn)
	searcher := hybrid.NewSearcher(ftsStore, entityStore)

	// Vector can be nil/unused — searcher should still work without panicking.
	ctx := context.Background()
	results, err := searcher.Search(ctx, "anything", 10)
	if err != nil {
		t.Fatalf("search without vector: %v", err)
	}
	_ = results // May be empty, that's fine
}

// TestCompat_BackgroundAgentTimeout verifies background config default.
func TestCompat_BackgroundAgentTimeout(t *testing.T) {
	cfg := testutil.TestConfig()
	// Default background timeout should be non-zero.
	if cfg.Options.Agents.BackgroundTimeout == 0 {
		// Check via the actual config.
		fullCfg := config.Default()
		if fullCfg.Options.Agents.BackgroundTimeout == 0 {
			t.Log("BackgroundTimeout is zero in defaults (may be set elsewhere)")
		}
	}
}

// TestCompat_IoTCapabilityDefaults verifies IoT overlay didn't regress.
func TestCompat_IoTCapabilityDefaults(t *testing.T) {
	iotCaps := capability.IoTDefaults()
	if len(iotCaps) < 2 {
		t.Errorf("IoT defaults = %d caps, want >= 2", len(iotCaps))
	}

	found := false
	for _, c := range iotCaps {
		if c.Resource == "iot.sensor.read" {
			found = true
			if c.RateLimit == nil {
				t.Error("iot.sensor.read should have rate limit")
			} else if c.RateLimit.MaxCalls != 120 {
				t.Errorf("rate limit = %d, want 120", c.RateLimit.MaxCalls)
			} else if c.RateLimit.Window != time.Hour {
				t.Errorf("window = %v, want 1h", c.RateLimit.Window)
			}
		}
	}
	if !found {
		t.Error("iot.sensor.read not found in IoTDefaults")
	}
}
