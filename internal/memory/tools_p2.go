package memory

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/LumabyteCo/aibutler/internal/memory/entity"
	"github.com/LumabyteCo/aibutler/internal/memory/fts"
	"github.com/LumabyteCo/aibutler/internal/memory/graph"
	"github.com/LumabyteCo/aibutler/internal/memory/hybrid"
	"github.com/LumabyteCo/aibutler/internal/memory/vector"
	"github.com/LumabyteCo/aibutler/internal/tool"
)

// P2Deps holds dependencies for advanced memory tools.
type P2Deps struct {
	FTS    *fts.Store
	Entity *entity.Store
	Graph  *graph.Store
	Hybrid *hybrid.Searcher
	Vector *vector.Store
}

// RegisterP2MemoryTools registers advanced memory tools (FTS search, entities, graph).
func RegisterP2MemoryTools(registry *tool.Registry, deps P2Deps) {
	if deps.Hybrid != nil {
		registry.Register(&hybridSearchTool{hybrid: deps.Hybrid})
	}
	if deps.FTS != nil {
		registry.Register(&ftsSearchTool{fts: deps.FTS})
	}
	if deps.Entity != nil {
		registry.Register(&peopleTool{entities: deps.Entity})
		registry.Register(&decisionsTool{entities: deps.Entity})
		registry.Register(&projectsTool{entities: deps.Entity})
	}
	if deps.Graph != nil {
		registry.Register(&graphTool{graph: deps.Graph})
	}
	if deps.Entity != nil {
		registry.Register(&memoryStatsTool{entities: deps.Entity, graph: deps.Graph, vectors: deps.Vector})
	}
}

// --- memory.search (hybrid) ---

type hybridSearchTool struct {
	hybrid *hybrid.Searcher
}

func (t *hybridSearchTool) Name() string        { return "memory.search" }
func (t *hybridSearchTool) Description() string { return "Search Living Memory across thoughts, transcripts, and entities" }
func (t *hybridSearchTool) Capability() string  { return "memory.read" }
func (t *hybridSearchTool) Schema() string {
	return `{"type":"object","properties":{"query":{"type":"string","description":"Search query (keywords, names, topics)"},"limit":{"type":"integer","description":"Max results (default 20)"}},"required":["query"]}`
}

func (t *hybridSearchTool) Execute(ctx context.Context, input string) (string, error) {
	var args struct {
		Query string `json:"query"`
		Limit int    `json:"limit"`
	}
	if err := json.Unmarshal([]byte(input), &args); err != nil {
		return "", fmt.Errorf("memory.search: invalid input: %w", err)
	}
	if args.Query == "" {
		return "", fmt.Errorf("memory.search: query is required")
	}

	results, err := t.hybrid.Search(ctx, args.Query, args.Limit)
	if err != nil {
		return "", err
	}
	data, _ := json.Marshal(results)
	return string(data), nil
}

// --- memory.fts_search ---

type ftsSearchTool struct {
	fts *fts.Store
}

func (t *ftsSearchTool) Name() string        { return "memory.fts_search" }
func (t *ftsSearchTool) Description() string { return "Full-text BM25 search across thoughts and transcripts" }
func (t *ftsSearchTool) Capability() string  { return "memory.read" }
func (t *ftsSearchTool) Schema() string {
	return `{"type":"object","properties":{"query":{"type":"string","description":"Search query (keywords)"},"limit":{"type":"integer","description":"Max results (default 20)"},"source":{"type":"string","description":"Filter by source: thought, transcript, or all (default all)"}},"required":["query"]}`
}

func (t *ftsSearchTool) Execute(ctx context.Context, input string) (string, error) {
	var args struct {
		Query  string `json:"query"`
		Limit  int    `json:"limit"`
		Source string `json:"source"`
	}
	if err := json.Unmarshal([]byte(input), &args); err != nil {
		return "", fmt.Errorf("memory.fts_search: invalid input: %w", err)
	}
	if args.Query == "" {
		return "", fmt.Errorf("memory.fts_search: query is required")
	}

	var results []fts.SearchResult
	var err error

	switch args.Source {
	case "thought":
		results, err = t.fts.SearchThoughts(ctx, args.Query, args.Limit)
	case "transcript":
		results, err = t.fts.SearchTranscripts(ctx, args.Query, args.Limit)
	default:
		results, err = t.fts.SearchAll(ctx, args.Query, args.Limit)
	}
	if err != nil {
		return "", err
	}

	data, _ := json.Marshal(results)
	return string(data), nil
}

// --- memory.people ---

type peopleTool struct {
	entities *entity.Store
}

func (t *peopleTool) Name() string        { return "memory.people" }
func (t *peopleTool) Description() string { return "List known people from conversations" }
func (t *peopleTool) Capability() string  { return "memory.read" }
func (t *peopleTool) Schema() string {
	return `{"type":"object","properties":{"limit":{"type":"integer","description":"Max results (default 50)"}}}`
}

func (t *peopleTool) Execute(ctx context.Context, input string) (string, error) {
	var args struct {
		Limit int `json:"limit"`
	}
	_ = json.Unmarshal([]byte(input), &args)

	people, err := t.entities.GetByType(ctx, entity.TypePerson, args.Limit)
	if err != nil {
		return "", err
	}
	data, _ := json.Marshal(people)
	return string(data), nil
}

// --- memory.decisions ---

type decisionsTool struct {
	entities *entity.Store
}

func (t *decisionsTool) Name() string        { return "memory.decisions" }
func (t *decisionsTool) Description() string { return "List recorded decisions" }
func (t *decisionsTool) Capability() string  { return "memory.read" }
func (t *decisionsTool) Schema() string {
	return `{"type":"object","properties":{"limit":{"type":"integer","description":"Max results (default 50)"}}}`
}

func (t *decisionsTool) Execute(ctx context.Context, input string) (string, error) {
	var args struct {
		Limit int `json:"limit"`
	}
	_ = json.Unmarshal([]byte(input), &args)

	decisions, err := t.entities.GetByType(ctx, entity.TypeDecision, args.Limit)
	if err != nil {
		return "", err
	}
	data, _ := json.Marshal(decisions)
	return string(data), nil
}

// --- memory.projects ---

type projectsTool struct {
	entities *entity.Store
}

func (t *projectsTool) Name() string        { return "memory.projects" }
func (t *projectsTool) Description() string { return "List known projects from conversations" }
func (t *projectsTool) Capability() string  { return "memory.read" }
func (t *projectsTool) Schema() string {
	return `{"type":"object","properties":{"limit":{"type":"integer","description":"Max results (default 50)"}}}`
}

func (t *projectsTool) Execute(ctx context.Context, input string) (string, error) {
	var args struct {
		Limit int `json:"limit"`
	}
	_ = json.Unmarshal([]byte(input), &args)

	projects, err := t.entities.GetByType(ctx, entity.TypeProject, args.Limit)
	if err != nil {
		return "", err
	}
	data, _ := json.Marshal(projects)
	return string(data), nil
}

// --- memory.graph ---

type graphTool struct {
	graph *graph.Store
}

func (t *graphTool) Name() string        { return "memory.graph" }
func (t *graphTool) Description() string { return "Query the knowledge graph for entity relationships" }
func (t *graphTool) Capability() string  { return "memory.read" }
func (t *graphTool) Schema() string {
	return `{"type":"object","properties":{"name":{"type":"string","description":"Entity name to look up"},"entity_id":{"type":"integer","description":"Entity ID to look up (alternative to name)"}}}`
}

func (t *graphTool) Execute(ctx context.Context, input string) (string, error) {
	var args struct {
		Name     string `json:"name"`
		EntityID int64  `json:"entity_id"`
	}
	if err := json.Unmarshal([]byte(input), &args); err != nil {
		return "", fmt.Errorf("memory.graph: invalid input: %w", err)
	}

	var node *graph.Node
	var err error

	if args.EntityID > 0 {
		node, err = t.graph.GetNode(ctx, args.EntityID)
	} else if args.Name != "" {
		node, err = t.graph.FindByName(ctx, args.Name)
	} else {
		// Return stats if no specific entity requested.
		stats, err := t.graph.Stats(ctx)
		if err != nil {
			return "", err
		}
		data, _ := json.Marshal(stats)
		return string(data), nil
	}

	if err != nil {
		return "", fmt.Errorf("memory.graph: %w", err)
	}

	data, _ := json.Marshal(node)
	return string(data), nil
}

// --- memory.stats ---

type memoryStatsTool struct {
	entities *entity.Store
	graph    *graph.Store
	vectors  *vector.Store
}

func (t *memoryStatsTool) Name() string        { return "memory.stats" }
func (t *memoryStatsTool) Description() string { return "Get Living Memory statistics" }
func (t *memoryStatsTool) Capability() string  { return "memory.read" }
func (t *memoryStatsTool) Schema() string {
	return `{"type":"object","properties":{}}`
}

func (t *memoryStatsTool) Execute(ctx context.Context, _ string) (string, error) {
	summary, err := t.entities.Summary(ctx)
	if err != nil {
		return "", err
	}
	if summary == "" {
		summary = "No entities recorded yet."
	}

	stats := map[string]interface{}{"summary": summary}

	if t.graph != nil {
		graphStats, err := t.graph.Stats(ctx)
		if err == nil {
			stats["graph"] = graphStats
		}
	}

	if t.vectors != nil {
		vecCount, err := t.vectors.Count(ctx)
		if err == nil {
			stats["vector_embeddings"] = vecCount
		}
	}

	data, _ := json.Marshal(stats)
	return string(data), nil
}
