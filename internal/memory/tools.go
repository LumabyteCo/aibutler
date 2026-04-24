package memory

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/LumabyteCo/aibutler/internal/tool"
)

// InstructionDetector detects instruction-like patterns in text and saves them.
// Defined here (not in instruction package) to avoid circular imports.
type InstructionDetector interface {
	DetectAndSave(ctx context.Context, text string) (int, error)
}

// RegisterMemoryTools registers all memory tools in the tool registry.
// The detector parameter is optional — pass nil to disable auto-detection.
func RegisterMemoryTools(registry *tool.Registry, store *Store, detector InstructionDetector) {
	registry.Register(&captureTool{store: store, detector: detector})
	registry.Register(&searchTool{store: store})
	registry.Register(&factsTool{store: store})
}

// --- memory.capture ---

type captureTool struct {
	store    *Store
	detector InstructionDetector
}

func (t *captureTool) Name() string        { return "memory.capture" }
func (t *captureTool) Description() string { return "Capture a thought or note to living memory" }
func (t *captureTool) Capability() string  { return "memory.write" }
func (t *captureTool) Schema() string {
	return `{"type":"object","properties":{"content":{"type":"string","description":"The thought or note to capture"},"tags":{"type":"array","items":{"type":"string"},"description":"Optional tags for categorization"},"source":{"type":"string","description":"Source channel (terminal, telegram, etc.)"}},"required":["content"]}`
}

func (t *captureTool) Execute(ctx context.Context, input string) (string, error) {
	var args struct {
		Content string   `json:"content"`
		Tags    []string `json:"tags"`
		Source  string   `json:"source"`
	}
	if err := json.Unmarshal([]byte(input), &args); err != nil {
		return "", fmt.Errorf("memory.capture: invalid input: %w", err)
	}
	if args.Content == "" {
		return "", fmt.Errorf("memory.capture: content is required")
	}

	id, err := t.store.SaveThought(ctx, args.Content, args.Source, "", args.Tags)
	if err != nil {
		return "", err
	}

	// Auto-extract key facts.
	extracted := ExtractKeyFacts(args.Content)
	var factMessages []string
	for _, e := range extracted {
		if _, err := t.store.SaveKeyFact(ctx, e.Fact, e.Category, ""); err == nil {
			factMessages = append(factMessages, fmt.Sprintf("%s (%s)", e.Fact, e.Category))
		}
	}

	result := fmt.Sprintf("Thought captured (id=%d).", id)
	if len(factMessages) > 0 {
		result += fmt.Sprintf(" Extracted %d key fact(s): %s", len(factMessages), fmt.Sprintf("%v", factMessages))
	}

	// Auto-detect instruction patterns.
	if t.detector != nil {
		count, _ := t.detector.DetectAndSave(ctx, args.Content)
		if count > 0 {
			result += fmt.Sprintf(" Auto-detected %d instruction(s).", count)
		}
	}

	return result, nil
}

// --- memory.thoughts ---
//
// NOTE: this tool used to register under the name "memory.search", but that
// name now belongs to the hybrid search tool in tools_p2.go. Registering under
// the same name was a silent collision (map last-wins), so this tag/contains
// filter was completely shadowed. Renaming it to "memory.thoughts" exposes
// the basic captured-thoughts listing as a distinct tool without clobbering
// hybrid search.

type searchTool struct {
	store *Store
}

func (t *searchTool) Name() string        { return "memory.thoughts" }
func (t *searchTool) Description() string { return "List captured thoughts filtered by tags or by substring. For rich hybrid search across the whole memory, use memory.search instead." }
func (t *searchTool) Capability() string  { return "memory.read" }
func (t *searchTool) Schema() string {
	return `{"type":"object","properties":{"tags":{"type":"array","items":{"type":"string"},"description":"Filter by tags (OR logic)"},"query":{"type":"string","description":"Substring to match in thought content"},"limit":{"type":"integer","description":"Max results (default 50)"}}}`
}

func (t *searchTool) Execute(ctx context.Context, input string) (string, error) {
	var args struct {
		Tags  []string `json:"tags"`
		Query string   `json:"query"`
		Limit int      `json:"limit"`
	}
	if err := json.Unmarshal([]byte(input), &args); err != nil {
		return "", fmt.Errorf("memory.thoughts: invalid input: %w", err)
	}

	thoughts, err := t.store.GetThoughts(ctx, ThoughtQuery{
		Tags:     args.Tags,
		Contains: args.Query,
		Limit:    args.Limit,
	})
	if err != nil {
		return "", err
	}

	data, _ := json.Marshal(thoughts)
	return string(data), nil
}

// --- memory.facts ---

type factsTool struct {
	store *Store
}

func (t *factsTool) Name() string        { return "memory.facts" }
func (t *factsTool) Description() string { return "List extracted key facts about the user" }
func (t *factsTool) Capability() string  { return "memory.read" }
func (t *factsTool) Schema() string {
	return `{"type":"object","properties":{"category":{"type":"string","description":"Filter by category: preference, identity, project, decision"},"limit":{"type":"integer","description":"Max results (default 10)"}}}`
}

func (t *factsTool) Execute(ctx context.Context, input string) (string, error) {
	var args struct {
		Category string `json:"category"`
		Limit    int    `json:"limit"`
	}
	if err := json.Unmarshal([]byte(input), &args); err != nil {
		return "", fmt.Errorf("memory.facts: invalid input: %w", err)
	}

	facts, err := t.store.GetKeyFacts(ctx, args.Category, args.Limit)
	if err != nil {
		return "", err
	}

	data, _ := json.Marshal(facts)
	return string(data), nil
}
