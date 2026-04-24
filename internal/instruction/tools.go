package instruction

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/LumabyteCo/aibutler/internal/tool"
)

// RegisterInstructionTools registers all instruction tools in the tool registry.
func RegisterInstructionTools(registry *tool.Registry, store *Store) {
	registry.Register(&saveTool{store: store})
	registry.Register(&listTool{store: store})
	registry.Register(&updateTool{store: store})
	registry.Register(&removeTool{store: store})
}

// --- instruction.save ---

type saveTool struct{ store *Store }

func (t *saveTool) Name() string        { return "instruction.save" }
func (t *saveTool) Description() string { return "Save a learned instruction or rule that persists across sessions" }
func (t *saveTool) Capability() string  { return "instruction.write" }
func (t *saveTool) Schema() string {
	return `{"type":"object","properties":{"content":{"type":"string","description":"The instruction or rule to save"},"category":{"type":"string","enum":["style","behavior","rule","knowledge","preference"],"description":"Category of instruction"},"priority":{"type":"integer","minimum":0,"maximum":100,"description":"Priority (0-100, higher = more important, default 50)"},"scope":{"type":"string","enum":["global","channel","session"],"description":"Scope (default: global)"},"scope_value":{"type":"string","description":"Channel name or session ID (required for channel/session scope)"}},"required":["content"]}`
}

func (t *saveTool) Execute(ctx context.Context, input string) (string, error) {
	var args struct {
		Content    string `json:"content"`
		Category   string `json:"category"`
		Priority   int    `json:"priority"`
		Scope      string `json:"scope"`
		ScopeValue string `json:"scope_value"`
	}
	if err := json.Unmarshal([]byte(input), &args); err != nil {
		return "", fmt.Errorf("instruction.save: invalid input: %w", err)
	}
	if args.Content == "" {
		return "", fmt.Errorf("instruction.save: content is required")
	}
	if args.Priority == 0 {
		args.Priority = 50
	}

	// Check for conflicts.
	conflicts, _ := t.store.CheckConflicts(ctx, args.Content)

	id, err := t.store.Save(ctx, args.Content, args.Category, args.Priority,
		args.Scope, args.ScopeValue, SourceExplicit, "")
	if err != nil {
		return "", err
	}

	count, _ := t.store.Count(ctx)
	result := fmt.Sprintf("Instruction saved (id=%d, category: %s, priority: %d). I now have %d active instruction(s).",
		id, args.Category, args.Priority, count)

	if len(conflicts) > 0 {
		var warnings []string
		for _, c := range conflicts {
			warnings = append(warnings, fmt.Sprintf("instruction #%d (%q): %s", c.ExistingID, c.ExistingContent, c.Reason))
		}
		result += "\n\nWarning — potential conflicts with: " + strings.Join(warnings, "; ")
	}

	return result, nil
}

// --- instruction.list ---

type listTool struct{ store *Store }

func (t *listTool) Name() string        { return "instruction.list" }
func (t *listTool) Description() string { return "List learned instructions (optionally filtered)" }
func (t *listTool) Capability() string  { return "instruction.read" }
func (t *listTool) Schema() string {
	return `{"type":"object","properties":{"category":{"type":"string","description":"Filter by category"},"scope":{"type":"string","description":"Filter by scope"},"active_only":{"type":"boolean","description":"Only show active instructions (default true)"},"format":{"type":"string","enum":["json","markdown"],"description":"Output format (default json)"}}}`
}

func (t *listTool) Execute(ctx context.Context, input string) (string, error) {
	var args struct {
		Category   string `json:"category"`
		Scope      string `json:"scope"`
		ActiveOnly *bool  `json:"active_only"`
		Format     string `json:"format"`
	}
	if err := json.Unmarshal([]byte(input), &args); err != nil {
		return "", fmt.Errorf("instruction.list: invalid input: %w", err)
	}

	activeOnly := true
	if args.ActiveOnly != nil {
		activeOnly = *args.ActiveOnly
	}

	if args.Format == "markdown" {
		md, err := t.store.RenderMarkdown(ctx)
		if err != nil {
			return "", err
		}
		return md, nil
	}

	instructions, err := t.store.List(ctx, ListQuery{
		Category:   args.Category,
		Scope:      args.Scope,
		ActiveOnly: activeOnly,
	})
	if err != nil {
		return "", err
	}

	data, _ := json.Marshal(instructions)
	return string(data), nil
}

// --- instruction.update ---

type updateTool struct{ store *Store }

func (t *updateTool) Name() string        { return "instruction.update" }
func (t *updateTool) Description() string { return "Update a learned instruction's content, priority, or status" }
func (t *updateTool) Capability() string  { return "instruction.write" }
func (t *updateTool) Schema() string {
	return `{"type":"object","properties":{"id":{"type":"integer","description":"Instruction ID to update"},"content":{"type":"string","description":"New instruction text"},"priority":{"type":"integer","minimum":0,"maximum":100},"category":{"type":"string","enum":["style","behavior","rule","knowledge","preference"]},"active":{"type":"boolean","description":"Enable or disable the instruction"}},"required":["id"]}`
}

func (t *updateTool) Execute(ctx context.Context, input string) (string, error) {
	var args struct {
		ID       int64  `json:"id"`
		Content  string `json:"content"`
		Priority int    `json:"priority"`
		Category string `json:"category"`
		Active   *bool  `json:"active"`
	}
	if err := json.Unmarshal([]byte(input), &args); err != nil {
		return "", fmt.Errorf("instruction.update: invalid input: %w", err)
	}
	if args.ID == 0 {
		return "", fmt.Errorf("instruction.update: id is required")
	}

	if err := t.store.Update(ctx, args.ID, args.Content, args.Priority, args.Category, args.Active); err != nil {
		return "", err
	}

	return fmt.Sprintf("Instruction %d updated.", args.ID), nil
}

// --- instruction.remove ---

type removeTool struct{ store *Store }

func (t *removeTool) Name() string        { return "instruction.remove" }
func (t *removeTool) Description() string { return "Remove a learned instruction permanently" }
func (t *removeTool) Capability() string  { return "instruction.write" }
func (t *removeTool) Schema() string {
	return `{"type":"object","properties":{"id":{"type":"integer","description":"Instruction ID to remove"}},"required":["id"]}`
}

func (t *removeTool) Execute(ctx context.Context, input string) (string, error) {
	var args struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal([]byte(input), &args); err != nil {
		return "", fmt.Errorf("instruction.remove: invalid input: %w", err)
	}
	if args.ID == 0 {
		return "", fmt.Errorf("instruction.remove: id is required")
	}

	if err := t.store.Remove(ctx, args.ID); err != nil {
		return "", err
	}

	return fmt.Sprintf("Instruction %d removed.", args.ID), nil
}
