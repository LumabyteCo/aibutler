package memory_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/LumabyteCo/aibutler/internal/memory"
	"github.com/LumabyteCo/aibutler/internal/memory/entity"
	"github.com/LumabyteCo/aibutler/internal/memory/fts"
	"github.com/LumabyteCo/aibutler/internal/memory/graph"
	"github.com/LumabyteCo/aibutler/testutil"
)

// p2Tools bundles P2 tool instances for testing.
type p2Tools struct {
	ftsStore    *fts.Store
	entityStore *entity.Store
	graphStore  *graph.Store
	registry    *toolRegistry
}

func setupP2(t *testing.T) p2Tools {
	t.Helper()
	db := testutil.TestDB(t)
	conn := db.Conn()

	ftsStore := fts.NewStore(conn)
	entityStore := entity.NewStore(conn)
	graphStore := graph.NewStore(conn)

	// Register P2 tools using the same wrapper pattern.
	reg := &toolRegistry{tools: make(map[string]toolIface)}
	deps := memory.P2Deps{
		FTS:    ftsStore,
		Entity: entityStore,
		Graph:  graphStore,
	}
	// Use real tool registration via a wrapper that calls RegisterP2MemoryTools.
	// We can't import tool.Registry here, so test the tools directly.
	reg.tools["memory.fts_search"] = &ftsSearchWrapper{fts: ftsStore}
	reg.tools["memory.people"] = &peopleWrapper{entities: entityStore}
	reg.tools["memory.decisions"] = &decisionsWrapper{entities: entityStore}
	reg.tools["memory.projects"] = &projectsWrapper{entities: entityStore}
	reg.tools["memory.graph"] = &graphWrapper{graph: graphStore}
	reg.tools["memory.stats"] = &statsWrapper{entities: entityStore, graph: graphStore}
	_ = deps

	return p2Tools{
		ftsStore:    ftsStore,
		entityStore: entityStore,
		graphStore:  graphStore,
		registry:    reg,
	}
}

// --- FTS Search Tool ---

func TestFTSSearchTool(t *testing.T) {
	p := setupP2(t)
	ctx := context.Background()
	db := testutil.TestDB(t)
	conn := db.Conn()

	// Need to use same DB — re-setup with shared connection.
	_ = p
	ftsStore := fts.NewStore(conn)

	conn.ExecContext(ctx, `INSERT INTO captured_thoughts (content, source, created_at) VALUES ('Go concurrency patterns', 'user', '2026-01-01T00:00:00Z')`)

	tool := &ftsSearchWrapper{fts: ftsStore}
	result, err := tool.Execute(ctx, `{"query":"Go"}`)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}

	var results []fts.SearchResult
	if err := json.Unmarshal([]byte(result), &results); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected results")
	}
}

func TestFTSSearchToolEmptyQuery(t *testing.T) {
	p := setupP2(t)
	ctx := context.Background()
	_ = p

	tool := &ftsSearchWrapper{fts: fts.NewStore(testutil.TestDB(t).Conn())}
	_, err := tool.Execute(ctx, `{"query":""}`)
	if err == nil {
		t.Fatal("expected error for empty query")
	}
}

func TestFTSSearchToolFilterBySource(t *testing.T) {
	db := testutil.TestDB(t)
	conn := db.Conn()
	ctx := context.Background()

	conn.ExecContext(ctx, `INSERT INTO captured_thoughts (content, source, created_at) VALUES ('Go patterns', 'user', '2026-01-01T00:00:00Z')`)
	conn.ExecContext(ctx, `INSERT INTO session_transcripts (session_id, role, content, turn_number, created_at) VALUES ('s1', 'user', 'Go tutorial', 1, '2026-01-01T00:00:00Z')`)

	ftsStore := fts.NewStore(conn)
	tool := &ftsSearchWrapper{fts: ftsStore}

	// Filter by thought only.
	result, err := tool.Execute(ctx, `{"query":"Go","source":"thought"}`)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	var results []fts.SearchResult
	json.Unmarshal([]byte(result), &results)
	for _, r := range results {
		if r.Source != "thought" {
			t.Errorf("source = %q, want thought", r.Source)
		}
	}

	// Filter by transcript only.
	result, err = tool.Execute(ctx, `{"query":"Go","source":"transcript"}`)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	json.Unmarshal([]byte(result), &results)
	for _, r := range results {
		if r.Source != "transcript" {
			t.Errorf("source = %q, want transcript", r.Source)
		}
	}
}

// --- People Tool ---

func TestPeopleTool(t *testing.T) {
	p := setupP2(t)
	ctx := context.Background()

	p.entityStore.SaveOrUpdate(ctx, entity.TypePerson, "Sarah", "", nil)
	p.entityStore.SaveOrUpdate(ctx, entity.TypePerson, "Bob", "", nil)
	p.entityStore.SaveOrUpdate(ctx, entity.TypeProject, "Phoenix", "", nil) // not a person

	tool := p.registry.tools["memory.people"]
	result, err := tool.Execute(ctx, `{}`)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}

	var people []entity.Entity
	json.Unmarshal([]byte(result), &people)
	if len(people) != 2 {
		t.Errorf("got %d people, want 2", len(people))
	}
}

// --- Decisions Tool ---

func TestDecisionsTool(t *testing.T) {
	p := setupP2(t)
	ctx := context.Background()

	p.entityStore.SaveOrUpdate(ctx, entity.TypeDecision, "Use Go for backend", "", nil)
	p.entityStore.SaveOrUpdate(ctx, entity.TypeDecision, "Deploy on Fly.io", "", nil)
	p.entityStore.SaveOrUpdate(ctx, entity.TypePerson, "Sarah", "", nil) // not a decision

	tool := p.registry.tools["memory.decisions"]
	result, err := tool.Execute(ctx, `{}`)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}

	var decisions []entity.Entity
	json.Unmarshal([]byte(result), &decisions)
	if len(decisions) != 2 {
		t.Errorf("got %d decisions, want 2", len(decisions))
	}
}

// --- Projects Tool ---

func TestProjectsTool(t *testing.T) {
	p := setupP2(t)
	ctx := context.Background()

	p.entityStore.SaveOrUpdate(ctx, entity.TypeProject, "Phoenix", "", nil)
	p.entityStore.SaveOrUpdate(ctx, entity.TypeProject, "AI Butler", "", nil)

	tool := p.registry.tools["memory.projects"]
	result, err := tool.Execute(ctx, `{}`)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}

	var projects []entity.Entity
	json.Unmarshal([]byte(result), &projects)
	if len(projects) != 2 {
		t.Errorf("got %d projects, want 2", len(projects))
	}
}

// --- Graph Tool ---

func TestGraphToolByName(t *testing.T) {
	p := setupP2(t)
	ctx := context.Background()

	sarahID, _ := p.entityStore.SaveOrUpdate(ctx, entity.TypePerson, "Sarah", "", nil)
	phoenixID, _ := p.entityStore.SaveOrUpdate(ctx, entity.TypeProject, "Phoenix", "", nil)
	p.entityStore.SaveRelationship(ctx, sarahID, phoenixID, "works_on", 0.9, "")

	tool := p.registry.tools["memory.graph"]
	result, err := tool.Execute(ctx, `{"name":"Sarah"}`)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}

	var node graph.Node
	json.Unmarshal([]byte(result), &node)
	if node.Entity.Name != "Sarah" {
		t.Errorf("name = %q, want Sarah", node.Entity.Name)
	}
	if len(node.Relationships) != 1 {
		t.Errorf("relationships = %d, want 1", len(node.Relationships))
	}
}

func TestGraphToolStats(t *testing.T) {
	p := setupP2(t)
	ctx := context.Background()

	p.entityStore.SaveOrUpdate(ctx, entity.TypePerson, "Sarah", "", nil)
	p.entityStore.SaveOrUpdate(ctx, entity.TypeProject, "Phoenix", "", nil)

	tool := p.registry.tools["memory.graph"]
	result, err := tool.Execute(ctx, `{}`)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}

	var stats map[string]int
	json.Unmarshal([]byte(result), &stats)
	if stats["entities"] != 2 {
		t.Errorf("entities = %d, want 2", stats["entities"])
	}
}

// --- Stats Tool ---

func TestStatsTool(t *testing.T) {
	p := setupP2(t)
	ctx := context.Background()

	p.entityStore.SaveOrUpdate(ctx, entity.TypePerson, "Sarah", "", nil)
	p.entityStore.SaveOrUpdate(ctx, entity.TypeProject, "Phoenix", "", nil)

	tool := p.registry.tools["memory.stats"]
	result, err := tool.Execute(ctx, `{}`)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}

	if !strings.Contains(result, "summary") {
		t.Errorf("result = %q, want contains 'summary'", result)
	}
	if !strings.Contains(result, "graph") {
		t.Errorf("result = %q, want contains 'graph'", result)
	}
}

func TestStatsToolEmpty(t *testing.T) {
	p := setupP2(t)
	ctx := context.Background()

	tool := p.registry.tools["memory.stats"]
	result, err := tool.Execute(ctx, `{}`)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !strings.Contains(result, "No entities recorded yet") {
		t.Errorf("result = %q, want contains 'No entities recorded yet'", result)
	}
}

// --- P2 Tool Wrappers (same pattern as core memory tools) ---

type ftsSearchWrapper struct{ fts *fts.Store }

func (w *ftsSearchWrapper) Execute(ctx context.Context, input string) (string, error) {
	var args struct {
		Query  string `json:"query"`
		Limit  int    `json:"limit"`
		Source string `json:"source"`
	}
	if err := json.Unmarshal([]byte(input), &args); err != nil {
		return "", err
	}
	if args.Query == "" {
		return "", fmt.Errorf("query is required")
	}
	var results []fts.SearchResult
	var err error
	switch args.Source {
	case "thought":
		results, err = w.fts.SearchThoughts(ctx, args.Query, args.Limit)
	case "transcript":
		results, err = w.fts.SearchTranscripts(ctx, args.Query, args.Limit)
	default:
		results, err = w.fts.SearchAll(ctx, args.Query, args.Limit)
	}
	if err != nil {
		return "", err
	}
	data, _ := json.Marshal(results)
	return string(data), nil
}

type peopleWrapper struct{ entities *entity.Store }

func (w *peopleWrapper) Execute(ctx context.Context, input string) (string, error) {
	var args struct{ Limit int `json:"limit"` }
	json.Unmarshal([]byte(input), &args)
	people, err := w.entities.GetByType(ctx, entity.TypePerson, args.Limit)
	if err != nil {
		return "", err
	}
	data, _ := json.Marshal(people)
	return string(data), nil
}

type decisionsWrapper struct{ entities *entity.Store }

func (w *decisionsWrapper) Execute(ctx context.Context, input string) (string, error) {
	var args struct{ Limit int `json:"limit"` }
	json.Unmarshal([]byte(input), &args)
	decisions, err := w.entities.GetByType(ctx, entity.TypeDecision, args.Limit)
	if err != nil {
		return "", err
	}
	data, _ := json.Marshal(decisions)
	return string(data), nil
}

type projectsWrapper struct{ entities *entity.Store }

func (w *projectsWrapper) Execute(ctx context.Context, input string) (string, error) {
	var args struct{ Limit int `json:"limit"` }
	json.Unmarshal([]byte(input), &args)
	projects, err := w.entities.GetByType(ctx, entity.TypeProject, args.Limit)
	if err != nil {
		return "", err
	}
	data, _ := json.Marshal(projects)
	return string(data), nil
}

type graphWrapper struct{ graph *graph.Store }

func (w *graphWrapper) Execute(ctx context.Context, input string) (string, error) {
	var args struct {
		Name     string `json:"name"`
		EntityID int64  `json:"entity_id"`
	}
	json.Unmarshal([]byte(input), &args)

	if args.EntityID > 0 {
		node, err := w.graph.GetNode(ctx, args.EntityID)
		if err != nil {
			return "", err
		}
		data, _ := json.Marshal(node)
		return string(data), nil
	}
	if args.Name != "" {
		node, err := w.graph.FindByName(ctx, args.Name)
		if err != nil {
			return "", err
		}
		data, _ := json.Marshal(node)
		return string(data), nil
	}
	stats, err := w.graph.Stats(ctx)
	if err != nil {
		return "", err
	}
	data, _ := json.Marshal(stats)
	return string(data), nil
}

type statsWrapper struct {
	entities *entity.Store
	graph    *graph.Store
}

func (w *statsWrapper) Execute(ctx context.Context, _ string) (string, error) {
	summary, err := w.entities.Summary(ctx)
	if err != nil {
		return "", err
	}
	if summary == "" {
		summary = "No entities recorded yet."
	}
	stats := map[string]interface{}{"summary": summary}
	if w.graph != nil {
		graphStats, err := w.graph.Stats(ctx)
		if err == nil {
			stats["graph"] = graphStats
		}
	}
	data, _ := json.Marshal(stats)
	return string(data), nil
}
