package memory

import (
	"context"
	"encoding/json"
	"fmt"

	bankpkg "github.com/LumabyteCo/aibutler/internal/memory/bank"
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
	registry.Register(&forgetTool{store: store})
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

	// Auto-extract key facts, each carrying provenance back to the thought
	// it came from (so forgetting the thought forgets the facts) and the
	// user-stated confidence prior (the user said it in their own words).
	extracted := ExtractKeyFacts(args.Content)
	var factMessages []string
	for _, e := range extracted {
		if _, err := t.store.SaveFact(ctx, FactInput{
			Fact:       e.Fact,
			Category:   e.Category,
			FactKey:    e.Key,
			SourceType: "thought",
			SourceID:   id,
			Confidence: ConfidenceUserStated,
		}); err == nil {
			factMessages = append(factMessages, fmt.Sprintf("%s (%s)", e.Fact, e.Category))
		}
	}

	result := fmt.Sprintf("Thought captured (id=%d).", id)
	if len(factMessages) > 0 {
		result += fmt.Sprintf(" Extracted %d key fact(s): %s", len(factMessages), fmt.Sprintf("%v", factMessages))
	}

	// Auto-detect instruction patterns — default bank only. Learned
	// instructions are global and injected into the primary prompt every
	// turn; a background worker processing untrusted input must not be able
	// to install one.
	if t.detector != nil && bankpkg.FromContext(ctx) == bankpkg.Default {
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

func (t *searchTool) Name() string { return "memory.thoughts" }
func (t *searchTool) Description() string {
	return "List captured thoughts filtered by tags or by substring. For rich hybrid search across the whole memory, use memory.search instead."
}
func (t *searchTool) Capability() string { return "memory.read" }
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

	// Retrieval is a promotion signal: facts the model actually pulls into
	// turns earn frequency weight in core-memory selection. Best-effort.
	if len(facts) > 0 {
		ids := make([]int64, 0, len(facts))
		for _, f := range facts {
			ids = append(ids, f.ID)
		}
		_ = t.store.TouchFactAccess(ctx, ids)
	}

	data, _ := json.Marshal(facts)
	return string(data), nil
}

// --- memory.forget ---
//
// True deletion with provenance cascade: forgetting a thought or transcript
// also removes every fact extracted from it, its embedding, and its full-text
// index entry in one transaction. Forgetting a fact removes just that fact
// (its conflict-ledger rows cascade). Registered under its own capability so
// deployments can gate deletion more strictly than ordinary memory writes.

type forgetTool struct {
	store *Store
}

func (t *forgetTool) Name() string { return "memory.forget" }
func (t *forgetTool) Description() string {
	return "Permanently delete a memory item. Deleting a thought or transcript also deletes everything derived from it (extracted facts, embeddings, search index entries). This cannot be undone — confirm with the user before calling."
}
func (t *forgetTool) Capability() string { return "memory.forget" }
func (t *forgetTool) Schema() string {
	return `{"type":"object","properties":{"fact_id":{"type":"integer","description":"ID of a key fact to delete"},"thought_id":{"type":"integer","description":"ID of a captured thought to delete with everything derived from it"},"transcript_id":{"type":"integer","description":"ID of a transcript row to delete with everything derived from it"}}}`
}

func (t *forgetTool) Execute(ctx context.Context, input string) (string, error) {
	var args struct {
		FactID       int64 `json:"fact_id"`
		ThoughtID    int64 `json:"thought_id"`
		TranscriptID int64 `json:"transcript_id"`
	}
	if err := json.Unmarshal([]byte(input), &args); err != nil {
		return "", fmt.Errorf("memory.forget: invalid input: %w", err)
	}

	set := 0
	for _, id := range []int64{args.FactID, args.ThoughtID, args.TranscriptID} {
		if id != 0 {
			set++
		}
	}
	if set != 1 {
		return "", fmt.Errorf("memory.forget: provide exactly one of fact_id, thought_id, transcript_id")
	}

	switch {
	case args.FactID != 0:
		if err := t.store.ForgetFact(ctx, args.FactID); err != nil {
			return "", err
		}
		return fmt.Sprintf("Fact %d permanently deleted.", args.FactID), nil
	case args.ThoughtID != 0:
		res, err := t.store.ForgetThought(ctx, args.ThoughtID)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("Thought %d permanently deleted, along with %d derived fact(s) and %d embedding(s).",
			args.ThoughtID, res.Facts, res.Vectors), nil
	default:
		res, err := t.store.ForgetTranscript(ctx, args.TranscriptID)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("Transcript row %d permanently deleted, along with %d derived fact(s) and %d embedding(s).",
			args.TranscriptID, res.Facts, res.Vectors), nil
	}
}
