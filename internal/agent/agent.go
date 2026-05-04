package agent

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/LumabyteCo/aibutler/internal/capability"
)

// Message is a single conversation turn.
type Message struct {
	Role      string     // "system", "user", "assistant", "tool"
	Content   string
	Images    []Image    // Optional multimodal images attached to a user message
	ToolID    string     // For tool results
	ToolCalls []ToolCall // For assistant messages that invoke tools
}

// ImageSource describes how an Image's payload should be interpreted.
type ImageSource string

const (
	// ImageSourceBase64 — Data is the base64-encoded body of the image.
	// MimeType identifies the content type (e.g. "image/png").
	ImageSourceBase64 ImageSource = "base64"
	// ImageSourceURL — Data is an absolute URL the model can fetch.
	ImageSourceURL ImageSource = "url"
)

// Image is one image attached to a Message. Adapters that support
// multimodal input (OpenAI-compatible — Ollama vision, OpenAI GPT-4o,
// LM Studio with vision LLMs, etc.) render Images alongside Content.
//
// Models without vision support silently ignore the Images field — the
// text Content still flows through normally.
type Image struct {
	Source   ImageSource // "base64" or "url"
	Data     string      // base64 body OR absolute URL, depending on Source
	MimeType string      // "image/png" | "image/jpeg" | "image/webp" | "image/gif"
}

// Response is what the model returns.
type Response struct {
	Content   string
	ToolCalls []ToolCall
	TokensIn  int
	TokensOut int
}

// ToolCall is a model request to invoke a tool.
type ToolCall struct {
	ID     string
	Name   string
	Input  string // JSON
}

// ToolDef describes an available tool.
type ToolDef struct {
	Name        string
	Description string
	Schema      string // JSON Schema
}

// ModelAdapter is implemented by LLM providers (real adapters in `internal/ai`;
// fake adapter in `testutil` for tests).
type ModelAdapter interface {
	Complete(ctx context.Context, messages []Message) (Response, error)
}

// ToolExecutor runs tool calls with capability checking.
type ToolExecutor interface {
	Execute(ctx context.Context, call ToolCall) (string, error)
	AvailableTools(ctx context.Context, mode Mode, caps *capability.CapabilitySet) []ToolDef
}

// BudgetChecker checks global budget status before/during agent execution.
type BudgetChecker interface {
	// ShouldPause returns true if the global monthly budget is exhausted and action is "pause".
	ShouldPause(ctx context.Context) bool
}

// Config holds everything needed to create an agent.
type Config struct {
	ID        string
	SessionID string
	ParentID  string
	Task      string
	Type      AgentType
	Model     ModelAdapter
	Tools     ToolExecutor
	Caps      *capability.CapabilitySet
	Mode         Mode
	DB           *sql.DB   // For state persistence
	InitMessages []Message // Pre-composed messages (system + history + user); if nil, uses Task

	MaxToolCalls  int            // Default: 50
	Timeout       time.Duration  // Default: 5m
	BudgetCap     float64        // Max USD spend (0 = unlimited)
	BudgetChecker BudgetChecker  // Global budget checker (nil = no global check)
	Depth         int            // Current nesting depth (0 = root agent)
	MaxDepth      int            // Max nesting depth (0 = unlimited, default 3)
	Autonomy      AutonomyConfig // L2 autonomy: auto/ask action lists

	// CostEstimator converts token counts to USD using per-provider pricing.
	// Injected so this package doesn't take a dependency on internal/model
	// (which would create an import cycle — model already imports agent).
	// When nil, agents fall back to a flat $0.01/1K token estimate, which
	// is wrong for most real providers — the CLI bootstrap always wires the
	// real per-model estimator from internal/model.
	CostEstimator func(provider string, tokensIn, tokensOut int) float64
	// Provider is the name used to look up pricing ("anthropic", "openai",
	// "gemini", "xai", "local"). When empty, falls back to the flat rate.
	Provider string
}

// Agent is a single execution instance of the AI agent.
type Agent struct {
	cfg      Config
	state    State
	mu       sync.Mutex
	messages []Message
	result   Result

	tokensIn  int
	tokensOut int
	toolCalls int
	costUSD   float64
	startTime time.Time
}

// New creates an agent in SPAWNED state.
func New(cfg Config) *Agent {
	if cfg.MaxToolCalls == 0 {
		cfg.MaxToolCalls = 50
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = 5 * time.Minute
	}
	if cfg.Mode == "" {
		cfg.Mode = ModeAuto
	}

	return &Agent{
		cfg:   cfg,
		state: StateSpawned,
		result: Result{
			ID:        cfg.ID,
			ParentID:  cfg.ParentID,
			SessionID: cfg.SessionID,
		},
	}
}

// State returns the current state.
func (a *Agent) State() State {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.state
}

// Run executes the agent loop. Returns the final result.
func (a *Agent) Run(ctx context.Context) (Result, error) {
	a.startTime = time.Now()

	// Apply timeout.
	ctx, cancel := context.WithTimeout(ctx, a.cfg.Timeout)
	defer cancel()

	// Transition: SPAWNED → RUNNING
	if err := a.transition(StateRunning); err != nil {
		return a.failWith(err.Error())
	}
	a.persistState(ctx)

	// Prepare initial messages.
	if len(a.cfg.InitMessages) > 0 {
		a.messages = make([]Message, len(a.cfg.InitMessages))
		copy(a.messages, a.cfg.InitMessages)
	} else {
		a.messages = []Message{
			{Role: "user", Content: a.cfg.Task},
		}
	}

	mode := effectiveMode(a.cfg.Mode)

	// Get available tools.
	var tools []ToolDef
	if a.cfg.Tools != nil {
		tools = a.cfg.Tools.AvailableTools(ctx, mode, a.cfg.Caps)
	}
	_ = tools // Provided to model via adapter

	// Core loop.
	for {
		// Check context cancellation.
		if err := ctx.Err(); err != nil {
			_ = a.transition(StateCancelled)
			a.result.Status = StateCancelled
			a.result.Error = "timeout"
			a.finalize()
			a.persistState(ctx)
			return a.result, nil
		}

		// Check per-agent budget.
		if a.cfg.BudgetCap > 0 && a.costUSD >= a.cfg.BudgetCap {
			_ = a.transition(StateCancelled)
			a.result.Status = StateCancelled
			a.result.Error = "budget_exceeded"
			a.finalize()
			a.persistState(ctx)
			return a.result, nil
		}

		// Check global monthly budget.
		if a.cfg.BudgetChecker != nil && a.cfg.BudgetChecker.ShouldPause(ctx) {
			_ = a.transition(StateCancelled)
			a.result.Status = StateCancelled
			a.result.Error = "global_budget_paused"
			a.finalize()
			a.persistState(ctx)
			return a.result, nil
		}

		// Check tool call limit.
		if a.toolCalls >= a.cfg.MaxToolCalls {
			_ = a.transition(StateCompleted)
			a.result.Status = StateCompleted
			a.result.Output = "complexity limit reached"
			a.finalize()
			a.persistState(ctx)
			return a.result, nil
		}

		// Call model.
		resp, err := a.cfg.Model.Complete(ctx, a.messages)
		if err != nil {
			return a.failWith(fmt.Sprintf("model error: %v", err))
		}

		a.tokensIn += resp.TokensIn
		a.tokensOut += resp.TokensOut
		// Per-provider pricing when the CLI wires in a real estimator;
		// fall back to a flat rate only in tests / bare agents.
		if a.cfg.CostEstimator != nil && a.cfg.Provider != "" {
			a.costUSD += a.cfg.CostEstimator(a.cfg.Provider, resp.TokensIn, resp.TokensOut)
		} else {
			a.costUSD += float64(resp.TokensIn+resp.TokensOut) / 1000.0 * 0.01
		}

		// No tool calls → final answer.
		if len(resp.ToolCalls) == 0 {
			_ = a.transition(StateCompleted)
			a.persistState(ctx)
			a.result.Status = StateCompleted
			a.result.Output = resp.Content
			a.finalize()
			return a.result, nil
		}

		// Has tool calls → execute them.
		_ = a.transition(StateWaiting)

		a.messages = append(a.messages, Message{
			Role:      "assistant",
			Content:   resp.Content,
			ToolCalls: resp.ToolCalls,
		})

		// In multi/custom mode with multiple tool calls, execute in parallel.
		if (mode == ModeMulti || mode == ModeCustom) && len(resp.ToolCalls) > 1 {
			a.executeToolsParallel(ctx, resp.ToolCalls)
		} else {
			a.executeToolsSequential(ctx, resp.ToolCalls)
		}

		// Back to RUNNING for next iteration.
		_ = a.transition(StateRunning)
		a.persistState(ctx)
	}
}

// executeToolsSequential executes tool calls one at a time (ModeSingle).
func (a *Agent) executeToolsSequential(ctx context.Context, calls []ToolCall) {
	for _, tc := range calls {
		a.toolCalls++

		// Inject the agent's capability set into the dispatch context so
		// tools that look up caps via capability.CapsFromContext(ctx)
		// (e.g. iot.*, proxy.*) see the correct permissions. Without this,
		// those tools receive a nil caps and fail with
		// "no capabilities in context".
		toolCtx := ctx
		if a.cfg.Caps != nil {
			toolCtx = capability.WithCaps(ctx, a.cfg.Caps)
		}

		var toolResult string
		if !a.cfg.Autonomy.ShouldAutoApprove(tc.Name) {
			toolResult = fmt.Sprintf("blocked by autonomy policy: %s requires confirmation", tc.Name)
		} else if a.cfg.Tools != nil {
			result, err := a.cfg.Tools.Execute(toolCtx, tc)
			if err != nil {
				toolResult = fmt.Sprintf("error: %v", err)
			} else {
				toolResult = result
			}
		} else {
			toolResult = "no tool executor configured"
		}

		a.messages = append(a.messages, Message{
			Role:    "tool",
			Content: toolResult,
			ToolID:  tc.ID,
		})
		a.result.appendToolOutput(tc.Name, toolResult)
	}
}

// executeToolsParallel executes tool calls concurrently (ModeMulti/ModeCustom).
// Results are collected in order matching the original tool call sequence.
func (a *Agent) executeToolsParallel(ctx context.Context, calls []ToolCall) {
	type toolResult struct {
		index  int
		result string
		toolID string
	}

	results := make([]toolResult, len(calls))
	var wg sync.WaitGroup

	// Inject capabilities into the parallel dispatch context — same
	// reasoning as executeToolsSerial above.
	toolCtx := ctx
	if a.cfg.Caps != nil {
		toolCtx = capability.WithCaps(ctx, a.cfg.Caps)
	}

	for i, tc := range calls {
		a.toolCalls++
		wg.Add(1)
		go func(idx int, call ToolCall) {
			defer wg.Done()
			var output string
			if !a.cfg.Autonomy.ShouldAutoApprove(call.Name) {
				output = fmt.Sprintf("blocked by autonomy policy: %s requires confirmation", call.Name)
			} else if a.cfg.Tools != nil {
				result, err := a.cfg.Tools.Execute(toolCtx, call)
				if err != nil {
					output = fmt.Sprintf("error: %v", err)
				} else {
					output = result
				}
			} else {
				output = "no tool executor configured"
			}
			results[idx] = toolResult{index: idx, result: output, toolID: call.ID}
		}(i, tc)
	}

	wg.Wait()

	// Append results in order.
	for i, r := range results {
		a.messages = append(a.messages, Message{
			Role:    "tool",
			Content: r.result,
			ToolID:  r.toolID,
		})
		a.result.appendToolOutput(calls[i].Name, r.result)
	}
}

func (a *Agent) transition(to State) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if !CanTransition(a.state, to) {
		return ErrInvalidTransition{From: a.state, To: to}
	}
	a.state = to
	return nil
}

func (a *Agent) failWith(msg string) (Result, error) {
	_ = a.transition(StateFailed)
	a.result.Status = StateFailed
	a.result.Error = msg
	a.result.Duration = time.Since(a.startTime)
	a.finalize()
	return a.result, nil
}

func (a *Agent) finalize() {
	a.result.TokensIn = a.tokensIn
	a.result.TokensOut = a.tokensOut
	a.result.CostUSD = a.costUSD
	a.result.ToolCalls = a.toolCalls
	a.result.Duration = time.Since(a.startTime)
}

// persistState saves the current agent state to the database.
func (a *Agent) persistState(ctx context.Context) {
	if a.cfg.DB == nil {
		return
	}
	a.mu.Lock()
	state := a.state
	a.mu.Unlock()

	now := time.Now().UTC().Format(time.RFC3339)
	var parentID interface{}
	if a.cfg.ParentID != "" {
		parentID = a.cfg.ParentID
	}
	if _, err := a.cfg.DB.ExecContext(ctx,
		`INSERT INTO agents (id, session_id, type, state, task, capabilities, model, created_at, updated_at, parent_id,
		  mode, cost_usd, tokens_input, tokens_output, duration_ms, error, tool_calls)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET
		  state = excluded.state, updated_at = excluded.updated_at,
		  cost_usd = excluded.cost_usd, tokens_input = excluded.tokens_input,
		  tokens_output = excluded.tokens_output, duration_ms = excluded.duration_ms,
		  error = excluded.error, tool_calls = excluded.tool_calls`,
		a.cfg.ID, a.cfg.SessionID, string(a.cfg.Type), string(state),
		a.cfg.Task, "[]", "default", now, now, parentID,
		string(a.cfg.Mode), a.costUSD, a.tokensIn, a.tokensOut,
		time.Since(a.startTime).Milliseconds(), a.result.Error, a.toolCalls); err != nil {
		log.Printf("agent %s: state update failed: %v", a.cfg.ID, err)
	}
}

// Semaphore limits concurrent agent execution.
type Semaphore struct {
	ch chan struct{}
}

// NewSemaphore creates a semaphore with the given capacity.
func NewSemaphore(max int) *Semaphore {
	if max <= 0 {
		max = 5
	}
	return &Semaphore{ch: make(chan struct{}, max)}
}

// Acquire blocks until a slot is available or ctx is cancelled.
func (s *Semaphore) Acquire(ctx context.Context) error {
	select {
	case s.ch <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Release frees a semaphore slot.
func (s *Semaphore) Release() {
	<-s.ch
}

// UserSemaphore enforces per-user concurrency limits on top of the global semaphore.
// Each user gets at most perUser slots, and the global limit still applies.
type UserSemaphore struct {
	global  *Semaphore
	perUser int
	mu      sync.Mutex
	users   map[string]int
}

// NewUserSemaphore creates a per-user concurrency limiter backed by a global semaphore.
func NewUserSemaphore(global *Semaphore, perUser int) *UserSemaphore {
	if perUser <= 0 {
		perUser = 3
	}
	return &UserSemaphore{
		global:  global,
		perUser: perUser,
		users:   make(map[string]int),
	}
}

// Acquire checks the per-user limit, then acquires the global semaphore.
func (us *UserSemaphore) Acquire(ctx context.Context, userID string) error {
	us.mu.Lock()
	if us.users[userID] >= us.perUser {
		us.mu.Unlock()
		return fmt.Errorf("per-user concurrency limit (%d) reached for %s", us.perUser, userID)
	}
	us.users[userID]++
	us.mu.Unlock()

	if err := us.global.Acquire(ctx); err != nil {
		us.mu.Lock()
		us.users[userID]--
		us.mu.Unlock()
		return err
	}
	return nil
}

// Release frees a per-user slot and the global semaphore slot.
func (us *UserSemaphore) Release(userID string) {
	us.global.Release()
	us.mu.Lock()
	if us.users[userID] > 0 {
		us.users[userID]--
	}
	us.mu.Unlock()
}

// UserCount returns the current number of active agents for a user.
func (us *UserSemaphore) UserCount(userID string) int {
	us.mu.Lock()
	defer us.mu.Unlock()
	return us.users[userID]
}
