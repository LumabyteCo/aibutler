package tool

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/LumabyteCo/aibutler/internal/agent"
	"github.com/LumabyteCo/aibutler/internal/capability"
)

// Tool is the interface every tool must implement.
type Tool interface {
	Name() string
	Description() string
	Schema() string // JSON Schema for input
	Execute(ctx context.Context, input string) (string, error)
	Capability() string // Required capability resource (empty = always available)
}

// Registry holds all registered tools.
type Registry struct {
	mu    sync.RWMutex
	tools map[string]Tool
}

// NewRegistry creates an empty tool registry.
func NewRegistry() *Registry {
	return &Registry{tools: make(map[string]Tool)}
}

// Register adds a tool to the registry.
func (r *Registry) Register(t Tool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.tools[t.Name()] = t
}

// Unregister removes a tool by name.
func (r *Registry) Unregister(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.tools, name)
}

// UnregisterPrefix removes all tools whose names start with prefix.
func (r *Registry) UnregisterPrefix(prefix string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for name := range r.tools {
		if strings.HasPrefix(name, prefix) {
			delete(r.tools, name)
		}
	}
}

// Get returns a tool by name.
func (r *Registry) Get(name string) (Tool, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.tools[name]
	return t, ok
}

// All returns all registered tools.
func (r *Registry) All() []Tool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	tools := make([]Tool, 0, len(r.tools))
	for _, t := range r.tools {
		tools = append(tools, t)
	}
	return tools
}

// Available returns tool definitions filtered by mode and capabilities.
func (r *Registry) Available(mode agent.Mode, caps *capability.CapabilitySet, engine *capability.Engine) []agent.ToolDef {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var defs []agent.ToolDef
	for _, t := range r.tools {
		// In single mode, hide delegation tools.
		if mode == agent.ModeSingle {
			if t.Name() == "agent.delegate" || t.Name() == "agent.spawn" {
				continue
			}
		}

		// Check capability if required. This is a filter check (Probe=true)
		// so it doesn't consume the rate-limit budget — we're just deciding
		// whether to advertise the tool, not actually using it.
		cap := t.Capability()
		if cap != "" && caps != nil && engine != nil {
			result := engine.Check(context.Background(), caps, capability.CheckRequest{Resource: cap, Probe: true})
			if !result.Allowed {
				continue
			}
		}

		defs = append(defs, agent.ToolDef{
			Name:        t.Name(),
			Description: t.Description(),
			Schema:      t.Schema(),
		})
	}
	return defs
}

// Dispatcher implements agent.ToolExecutor via capability-gated dispatch.
// TelemetryRecorder records tool call metrics.
type TelemetryRecorder interface {
	RecordToolCall()
	RecordError()
}

// HookRunner is a narrow interface for the hook engine (avoids import cycles).
type HookRunner interface {
	RunPreToolUse(ctx context.Context, toolName, toolInput string) (denied bool, messages []string, err error)
	RunPostToolUse(ctx context.Context, toolName, toolInput, toolOutput string, isError bool) (denied bool, messages []string, err error)
}

// ComplianceLogger is a narrow interface for compliance audit logging.
type ComplianceLogger interface {
	Log(ctx context.Context, userID, action, resource, details, ip, outcome string) error
}

// CacheProvider is a narrow interface for response caching.
type CacheProvider interface {
	Get(ctx context.Context, key string) (string, bool, error)
	Set(ctx context.Context, key, value string, ttl time.Duration) error
	TTLForTool(toolName string) time.Duration
}

// PathMutator is implemented by tools that modify files. MutatesPaths
// inspects a call's input and returns the file paths the call would change;
// the dispatcher checkpoints each one before execution so the mutation can
// be undone. Empty result = nothing to checkpoint for this call.
type PathMutator interface {
	MutatesPaths(input string) []string
}

// Checkpointer records a file's pre-mutation state. Snapshot errors abort
// the mutation (fail closed): a rollback guarantee that silently skips is
// not a guarantee.
type Checkpointer interface {
	Snapshot(ctx context.Context, tool, path string) error
}

type Dispatcher struct {
	registry     *Registry
	engine       *capability.Engine
	auditor      capability.Auditor
	telemetry    TelemetryRecorder
	hooks        HookRunner
	compliance   ComplianceLogger
	cache        CacheProvider
	checkpointer Checkpointer

	// Repeat-call circuit breaker: identical (tool, input) calls repeated
	// within the window get an advisory instead of execution — runaway
	// loops burn budget and, worse, retry destructive actions.
	repeatLimit  int
	repeatWindow time.Duration
	repeatMu     sync.Mutex
	repeatSeen   map[string]*repeatEntry
}

type repeatEntry struct {
	count int
	first time.Time
}

// NewDispatcher creates a dispatcher.
func NewDispatcher(registry *Registry, engine *capability.Engine, auditor capability.Auditor) *Dispatcher {
	return &Dispatcher{
		registry: registry,
		engine:   engine,
		auditor:  auditor,
	}
}

// SetTelemetry attaches a telemetry recorder to the dispatcher.
func (d *Dispatcher) SetTelemetry(t TelemetryRecorder) {
	d.telemetry = t
}

// SetHookEngine attaches a hook runner to the dispatcher.
func (d *Dispatcher) SetHookEngine(h HookRunner) {
	d.hooks = h
}

// SetComplianceLogger attaches a compliance logger to the dispatcher.
func (d *Dispatcher) SetComplianceLogger(cl ComplianceLogger) {
	d.compliance = cl
}

// SetCache attaches a response cache to the dispatcher.
func (d *Dispatcher) SetCache(c CacheProvider) {
	d.cache = c
}

// SetCheckpointer attaches the pre-mutation checkpoint store. Tools that
// implement PathMutator get their target files snapshotted before execution.
func (d *Dispatcher) SetCheckpointer(c Checkpointer) {
	d.checkpointer = c
}

// SetRepeatCallLimit enables the repeat-call circuit breaker: the Nth
// identical call within a 10-minute window returns an advisory instead of
// executing. 0 disables.
func (d *Dispatcher) SetRepeatCallLimit(n int) {
	d.repeatLimit = n
	d.repeatWindow = 10 * time.Minute
	if d.repeatSeen == nil {
		d.repeatSeen = make(map[string]*repeatEntry)
	}
}

// checkRepeat returns an advisory message when the identical call has hit
// the repeat limit inside the window. It also prunes expired entries
// opportunistically, keeping the map bounded by the window.
func (d *Dispatcher) checkRepeat(name, input string) (string, bool) {
	if d.repeatLimit <= 0 {
		return "", false
	}
	sum := sha256.Sum256([]byte(name + "\x00" + input))
	key := string(sum[:])
	now := time.Now()

	d.repeatMu.Lock()
	defer d.repeatMu.Unlock()
	for k, e := range d.repeatSeen {
		if now.Sub(e.first) > d.repeatWindow {
			delete(d.repeatSeen, k)
		}
	}
	e, ok := d.repeatSeen[key]
	if !ok || now.Sub(e.first) > d.repeatWindow {
		d.repeatSeen[key] = &repeatEntry{count: 1, first: now}
		return "", false
	}
	e.count++
	if e.count >= d.repeatLimit {
		return fmt.Sprintf(
			"blocked: this exact %s call has now been attempted %d times with identical input in the last few minutes. If a different outcome is needed, something must change first — the input, the approach, or the state it depends on. Adjust and retry, or ask the user how to proceed.",
			name, e.count), true
	}
	return "", false
}

// Execute dispatches a tool call through capability gates.
func (d *Dispatcher) Execute(ctx context.Context, call agent.ToolCall) (string, error) {
	return d.ExecuteWithCaps(ctx, call, nil)
}

// ExecuteWithCaps dispatches a tool call with specific capabilities.
// If caps is nil, falls back to caps from context (defense-in-depth).
func (d *Dispatcher) ExecuteWithCaps(ctx context.Context, call agent.ToolCall, caps *capability.CapabilitySet) (string, error) {
	// 1. Look up tool.
	t, ok := d.registry.Get(call.Name)
	if !ok {
		return "", fmt.Errorf("unknown tool: %s", call.Name)
	}

	// 2. Capability check. Fall back to context caps if explicit caps not provided.
	if caps == nil {
		caps = capability.CapsFromContext(ctx)
	}
	cap := t.Capability()
	if cap != "" && caps != nil {
		result := d.engine.Check(ctx, caps, capability.CheckRequest{Resource: cap})
		if !result.Allowed {
			return "", fmt.Errorf("capability denied: %s (reason: %s)", cap, result.Reason)
		}
		// Allowed-but-confirmation-required is a structured signal, not
		// a hard denial. The mission engine catches this via errors.As
		// and auto-pauses the mission to waiting_user with a payload
		// the UI can render. Outside a mission context the error
		// surfaces as an ordinary tool failure — the agent loop logs
		// it and the user retries after granting explicit confirmation.
		if result.RequiresConfirmation {
			return "", &capability.ConfirmationRequiredError{
				Capability: cap,
				Reason:     result.Reason,
			}
		}
	}

	// 3. Run pre-tool hooks — if denied, return denial message (skip execution).
	if d.hooks != nil {
		denied, msgs, hookErr := d.hooks.RunPreToolUse(ctx, call.Name, call.Input)
		if hookErr != nil {
			return "", fmt.Errorf("hook pre-tool error: %w", hookErr)
		}
		if denied {
			reason := "tool call denied by hook"
			if len(msgs) > 0 {
				reason = strings.Join(msgs, "; ")
			}
			return "", fmt.Errorf("denied: %s", reason)
		}
		// Pre-hook feedback will be merged after execution.
		_ = msgs
	}

	// 4. Inject capabilities into context for tools that need them (e.g. proxy).
	if caps != nil {
		ctx = capability.WithCaps(ctx, caps)
	}

	// E4: Check cache before executing. Tools that mutate state must never
	// be served a cached "success": a mutating call replayed after a restore
	// would report done without doing anything, skipping post-hooks and audit
	// with it. The PathMutator check makes this structural — a mutating tool
	// cannot silently fall out of the name denylist.
	_, mutates := t.(PathMutator)
	cacheable := d.cache != nil && !mutates && isCacheable(call.Name)
	if cacheable {
		ttl := d.cache.TTLForTool(call.Name)
		if ttl > 0 {
			cacheKey := call.Name + "\x00" + call.Input
			if cached, found, cErr := d.cache.Get(ctx, cacheKey); cErr == nil && found {
				return cached, nil
			}
		}
	}

	// 4b. Repeat-call circuit breaker: identical calls looping within the
	// window get an advisory result instead of executing again. Runs after
	// the cache (a cache hit is free — no budget burn, nothing destructive)
	// and exempts read-capability tools, whose repetition is a normal
	// read-verify pattern rather than a stuck loop. Returned as tool output
	// (not an error) so the model reads and reacts to it; the audit trail
	// records the block.
	if !strings.HasSuffix(cap, ".read") {
		if advisory, blocked := d.checkRepeat(call.Name, call.Input); blocked {
			if d.auditor != nil {
				_ = d.auditor.LogAccess(ctx, capability.AuditEntry{
					Action:         call.Name,
					CapabilityUsed: cap,
					Status:         "blocked_repeat",
				})
			}
			return advisory, nil
		}
	}

	// 4c. Pre-mutation checkpoints: tools that declare the paths they will
	// modify get each one snapshotted first, so the change is undoable.
	// Snapshot failure aborts the mutation — fail closed. The snapshot layer
	// validates each path against the allowed roots before reading it.
	if d.checkpointer != nil {
		if pm, ok := t.(PathMutator); ok {
			for _, p := range pm.MutatesPaths(call.Input) {
				if err := d.checkpointer.Snapshot(ctx, call.Name, p); err != nil {
					return "", fmt.Errorf("checkpoint before %s failed (mutation aborted): %w", call.Name, err)
				}
			}
		}
	}

	// 5. Execute.
	output, err := t.Execute(ctx, call.Input)

	// 5b. Telemetry.
	if d.telemetry != nil {
		d.telemetry.RecordToolCall()
		if err != nil {
			d.telemetry.RecordError()
		}
	}

	// 6. Run post-tool hooks — merge feedback into output.
	if d.hooks != nil {
		isErr := err != nil
		outStr := output
		if isErr {
			outStr = err.Error()
		}
		_, postMsgs, hookErr := d.hooks.RunPostToolUse(ctx, call.Name, call.Input, outStr, isErr)
		if hookErr == nil && len(postMsgs) > 0 {
			feedback := SanitizeHookFeedback(strings.Join(postMsgs, "\n"))
			if output != "" {
				output = output + "\n\n<hook_feedback untrusted=\"true\">\n" + feedback + "\n</hook_feedback>"
			} else {
				output = "<hook_feedback untrusted=\"true\">\n" + feedback + "\n</hook_feedback>"
			}
		}
	}

	// E4: Store result in cache on success (mutating tools excluded above).
	if cacheable && err == nil {
		ttl := d.cache.TTLForTool(call.Name)
		if ttl > 0 {
			cacheKey := call.Name + "\x00" + call.Input
			_ = d.cache.Set(ctx, cacheKey, output, ttl)
		}
	}

	// E3: Compliance audit logging for tool executions.
	if d.compliance != nil {
		outcome := "success"
		if err != nil {
			outcome = "error"
		}
		_ = d.compliance.Log(ctx, "", call.Name, cap, call.Input, "", outcome)
	}

	// 7. Audit.
	if d.auditor != nil {
		status := "success"
		errMsg := ""
		if err != nil {
			status = "error"
			errMsg = err.Error()
		}
		_ = d.auditor.LogAccess(ctx, capability.AuditEntry{
			Action:         call.Name,
			CapabilityUsed: cap,
			Status:         status,
			Error:          errMsg,
		})
	}

	return output, err
}

// AvailableTools returns tool definitions filtered by mode and capabilities.
func (d *Dispatcher) AvailableTools(ctx context.Context, mode agent.Mode, caps *capability.CapabilitySet) []agent.ToolDef {
	return d.registry.Available(mode, caps, d.engine)
}

// RegisterDataTools registers all built-in SQLite data tools.
func RegisterDataTools(registry *Registry, db *sql.DB) {
	// Tasks.
	registry.Register(&taskAddTool{db: db})
	registry.Register(&taskListTool{db: db})
	registry.Register(&taskCompleteTool{db: db})
	registry.Register(&taskRemoveTool{db: db})
	registry.Register(&taskClearTool{db: db})
	registry.Register(&taskPrioritizeTool{db: db})

	// Expenses & budgets.
	registry.Register(&expenseLogTool{db: db})
	registry.Register(&expenseSummaryTool{db: db})
	registry.Register(&expenseBudgetCheckTool{db: db})

	// Contacts.
	registry.Register(&contactAddTool{db: db})
	registry.Register(&contactSearchTool{db: db})
	registry.Register(&contactUpdateTool{db: db})
	registry.Register(&contactBirthdaysTool{db: db})

	// Journal.
	registry.Register(&journalWriteTool{db: db})
	registry.Register(&journalReadTool{db: db})
	registry.Register(&journalMoodTrendTool{db: db})

	// Health.
	registry.Register(&healthLogTool{db: db})
	registry.Register(&healthReadTool{db: db})

	// Reminders.
	registry.Register(&reminderSetTool{db: db})
	registry.Register(&reminderListTool{db: db})
	registry.Register(&reminderCancelTool{db: db})

	// Habits.
	registry.Register(&habitCreateTool{db: db})
	registry.Register(&habitLogTool{db: db})
	registry.Register(&habitStreakTool{db: db})

	// Places.
	registry.Register(&placeSaveTool{db: db})
	registry.Register(&placeSearchTool{db: db})
	registry.Register(&placeUpdateTool{db: db})
	registry.Register(&placeDeleteTool{db: db})

	// Cost.
	registry.Register(&costStatusTool{db: db})
}

// FuncTool wraps a function as a Tool implementation.
// Useful for packages that can't import the tool package directly (avoiding circular imports).
type FuncTool struct {
	ToolName   string
	ToolDesc   string
	ToolSchema string
	ToolCap    string
	Exec       func(ctx context.Context, input string) (string, error)
}

func (f *FuncTool) Name() string        { return f.ToolName }
func (f *FuncTool) Description() string { return f.ToolDesc }
func (f *FuncTool) Schema() string      { return f.ToolSchema }
func (f *FuncTool) Capability() string  { return f.ToolCap }
func (f *FuncTool) Execute(ctx context.Context, input string) (string, error) {
	return f.Exec(ctx, input)
}

// SanitizeHookFeedback truncates and cleans hook output to mitigate prompt injection.
func SanitizeHookFeedback(feedback string) string {
	// Truncate to prevent excessively long hook output.
	const maxFeedback = 500
	if len(feedback) > maxFeedback {
		feedback = feedback[:maxFeedback] + "... [truncated]"
	}
	// Strip lines that look like prompt injection attempts.
	var clean []string
	for _, line := range strings.Split(feedback, "\n") {
		trimmed := strings.TrimSpace(strings.ToLower(line))
		if strings.HasPrefix(trimmed, "ignore") ||
			strings.HasPrefix(trimmed, "system:") ||
			strings.HasPrefix(trimmed, "you are") ||
			strings.HasPrefix(trimmed, "assistant:") ||
			strings.HasPrefix(trimmed, "forget") {
			continue
		}
		clean = append(clean, line)
	}
	return strings.Join(clean, "\n")
}

// isCacheable returns false for tools that should never be cached (mutating tools).
func isCacheable(toolName string) bool {
	// Skip caching for shell, file writes, and other mutating tools.
	// (Belt: the dispatcher also structurally excludes any PathMutator.)
	uncacheable := []string{
		"shell.", "file.write", "file.edit", "file.delete", "task.add", "task.complete",
		"task.remove", "task.clear", "expense.log", "contact.add", "contact.update",
		"journal.write", "health.log", "reminder.set", "reminder.cancel",
		"habit.create", "habit.log", "place.save", "place.update", "place.delete",
		"agent.delegate", "agent.spawn", "channel.send", "channel.relay",
		"memory.capture", "memory.forget", "instruction.save", "instruction.update", "instruction.remove",
		"transaction.", "voice.", "plugin.marketplace.install", "checkpoint.restore",
		"code.",
	}
	for _, prefix := range uncacheable {
		if strings.HasPrefix(toolName, prefix) {
			return false
		}
	}
	return true
}

// WireHealthEncryptor sets the encryptor on health.log and health.read tools.
func WireHealthEncryptor(registry *Registry, enc HealthEncryptor) {
	registry.mu.RLock()
	defer registry.mu.RUnlock()
	if t, ok := registry.tools["health.log"]; ok {
		if ht, ok := t.(*healthLogTool); ok {
			ht.enc = enc
		}
	}
	if t, ok := registry.tools["health.read"]; ok {
		if ht, ok := t.(*healthReadTool); ok {
			ht.enc = enc
		}
	}
}
