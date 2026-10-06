package schedule

import (
	"strconv"
	"context"
	"strings"
	"encoding/json"
	"fmt"
	"time"

	"github.com/LumabyteCo/aibutler/internal/tool"
)

// RegisterScheduleTools registers schedule management tools.
// model is optional — if provided, enables LLM fallback for NL-to-cron conversion.
func RegisterScheduleTools(registry *tool.Registry, store *Store, model CronConverter) {
	registry.Register(&createTool{store: store, model: model})
	registry.Register(&listTool{store: store})
	registry.Register(&deleteTool{store: store})
}

// createTool creates a new schedule.
type createTool struct {
	store *Store
	model CronConverter
}

type createInput struct {
	Name         string   `json:"name"`
	Cron         string   `json:"cron"`
	Natural      string   `json:"natural"` // Natural language schedule (e.g. "every day at 9:00")
	Task         string   `json:"task"`
	Channel      string   `json:"channel"`
	AccountID    string   `json:"account_id"`
	Capabilities []string `json:"capabilities"`
}

func (t *createTool) Name() string        { return "schedule.create" }
func (t *createTool) Description() string {
	return "Create a scheduled task from natural language (\"every weekday at 8am\", \"every Monday at 9am\", " +
		"\"daily at midnight\") or a cron expression. USE THIS DIRECTLY when the user asks to schedule anything — " +
		"do NOT compute dates with shell commands. If the user asks to schedule something for a time that's already " +
		"passed (e.g. \"8am today\" at 9am), the next occurrence will be tomorrow — create it and tell the user " +
		"it'll fire next time, not \"yesterday\". If the request names a one-off past time, clarify: \"that time has " +
		"already passed today; would you like it for tomorrow instead?\""
}
func (t *createTool) Capability() string  { return "schedule.manage" }

func (t *createTool) Schema() string {
	return `{
		"type": "object",
		"properties": {
			"name":       {"type": "string", "description": "Schedule name"},
			"cron":       {"type": "string", "description": "Cron expression (5-field: min hour dom mon dow)"},
			"natural":    {"type": "string", "description": "Natural language schedule (e.g. 'every day at 9:00', 'every weekday')"},
			"task":       {"type": "string", "description": "Task description for the agent"},
			"channel":    {"type": "string", "description": "Output channel (e.g. telegram, webchat)"},
			"account_id": {"type": "string", "description": "Target account for delivery"},
			"capabilities": {"type": "array", "items": {"type": "string"}, "description": "Capability resources the job runs with (e.g. memory.read). Empty = default set. Declaring a minimal list is recommended for background jobs."}
		},
		"required": ["name", "task", "channel", "account_id"]
	}`
}

func (t *createTool) Execute(ctx context.Context, input string) (string, error) {
	var in createInput
	if err := json.Unmarshal([]byte(input), &in); err != nil {
		return "", fmt.Errorf("schedule.create: %w", err)
	}

	// Convert natural language to cron if cron not provided.
	if in.Cron == "" && in.Natural != "" {
		cron, err := NLToCronWithModel(ctx, in.Natural, t.model)
		if err != nil {
			return "", fmt.Errorf("schedule.create: %w", err)
		}
		in.Cron = cron
	}

	if in.Cron == "" {
		return "", fmt.Errorf("schedule.create: either cron or natural is required")
	}

	// Validate cron expression
	if _, err := ParseCron(in.Cron); err != nil {
		return "", fmt.Errorf("schedule.create: invalid cron: %w", err)
	}

	// One-off past-due check (when the user said "today at H:M" and that
	// time has already passed local-time): warn instead of silently booking
	// for tomorrow. Recurring jobs ("every day", "every weekday") don't
	// warn — the next occurrence is tomorrow by design.
	if strings.HasPrefix(strings.ToLower(in.Natural), "today at ") {
		parts := strings.Fields(in.Cron)
		if len(parts) == 5 {
			now := time.Now()
			hour, _ := strconv.Atoi(parts[1])
			min, _ := strconv.Atoi(parts[0])
			fire := time.Date(now.Year(), now.Month(), now.Day(), hour, min, 0, 0, now.Location())
			if fire.Before(now) {
				// Warn the caller so the model can say "that's past today;
				// it'll fire tomorrow" instead of leaving a dead-end.
				return fmt.Sprintf("Schedule %q saved (cron: %s). Note: the requested time %q has already passed today — it will fire tomorrow at %02d:%02d.",
					in.Name, in.Cron, in.Natural, hour, min), nil
			}
		}
	}

	// Builtin task keys are reserved for code-registered maintenance —
	// a model must not be able to alias an agent task onto them.
	if hasBuiltinPrefix(in.Task) {
		return "", fmt.Errorf("schedule.create: task names starting with %q are reserved", BuiltinPrefix)
	}

	sched := &Schedule{
		ID:           fmt.Sprintf("sched_%d", timeNowUnixMilli()),
		Name:         in.Name,
		CronExpr:     in.Cron,
		Task:         in.Task,
		Channel:      in.Channel,
		AccountID:    in.AccountID,
		Capabilities: in.Capabilities,
		Enabled:      true,
	}

	if err := t.store.Create(ctx, sched); err != nil {
		return "", fmt.Errorf("schedule.create: %w", err)
	}

	return fmt.Sprintf("Schedule %q created (cron: %s). ID: %s", sched.Name, sched.CronExpr, sched.ID), nil
}

// listTool lists all schedules.
type listTool struct{ store *Store }

func (t *listTool) Name() string        { return "schedule.list" }
func (t *listTool) Description() string { return "List all scheduled tasks." }
func (t *listTool) Capability() string  { return "schedule.manage" }

func (t *listTool) Schema() string {
	return `{"type": "object", "properties": {}}`
}

func (t *listTool) Execute(ctx context.Context, _ string) (string, error) {
	schedules, err := t.store.List(ctx)
	if err != nil {
		return "", fmt.Errorf("schedule.list: %w", err)
	}
	if len(schedules) == 0 {
		return "No schedules configured.", nil
	}
	data, _ := json.MarshalIndent(schedules, "", "  ")
	return string(data), nil
}

// deleteTool deletes a schedule.
type deleteTool struct{ store *Store }

type deleteInput struct {
	ID string `json:"id"`
}

func (t *deleteTool) Name() string        { return "schedule.delete" }
func (t *deleteTool) Description() string { return "Delete a scheduled task." }
func (t *deleteTool) Capability() string  { return "schedule.manage" }

func (t *deleteTool) Schema() string {
	return `{
		"type": "object",
		"properties": {
			"id": {"type": "string", "description": "Schedule ID to delete"}
		},
		"required": ["id"]
	}`
}

func (t *deleteTool) Execute(ctx context.Context, input string) (string, error) {
	var in deleteInput
	if err := json.Unmarshal([]byte(input), &in); err != nil {
		return "", fmt.Errorf("schedule.delete: %w", err)
	}
	if err := t.store.Delete(ctx, in.ID); err != nil {
		return "", fmt.Errorf("schedule.delete: %w", err)
	}
	return fmt.Sprintf("Schedule %s deleted.", in.ID), nil
}

// timeNowUnixMilli returns current time in milliseconds (mockable in tests via variable).
var timeNowUnixMilli = func() int64 { return time.Now().UnixMilli() }
