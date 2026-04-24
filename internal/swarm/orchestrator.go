package swarm

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/LumabyteCo/aibutler/internal/agent"
)

// Subtask is a unit of work within a swarm plan.
type Subtask struct {
	ID             string   `json:"id"`
	Task           string   `json:"task"`
	DependsOn      []string `json:"depends_on,omitempty"`
	CapabilityHint string   `json:"capability_hint,omitempty"`
}

// Plan is a decomposed set of subtasks for a goal.
type Plan struct {
	Goal     string    `json:"goal"`
	Subtasks []Subtask `json:"subtasks"`
}

// RegistryEntry is a peer agent returned from discovery.
type RegistryEntry struct {
	Name string
	URL  string
}

// RegistryLookup discovers peer agents by capability.
type RegistryLookup interface {
	Discover(ctx context.Context, capability string) ([]RegistryEntry, error)
}

// TaskRunner executes a task locally.
type TaskRunner interface {
	RunTask(ctx context.Context, task string) (string, error)
}

// Orchestrator decomposes a goal into subtasks, fans them out, and aggregates results.
type Orchestrator struct {
	db        *sql.DB
	model     agent.ModelAdapter
	registry  RegistryLookup
	runner    TaskRunner
	budgetUSD float64 // max budget; 0 = unlimited
	spentUSD  float64 // accumulated cost
	costMu    sync.Mutex
}

// SetBudget sets the maximum budget in USD for this orchestrator.
// If budget is exceeded during plan execution, remaining subtasks are aborted.
func (o *Orchestrator) SetBudget(usd float64) {
	o.costMu.Lock()
	defer o.costMu.Unlock()
	o.budgetUSD = usd
	o.spentUSD = 0
}

// trackCost records the cost of a subtask and returns true if budget is exceeded.
func (o *Orchestrator) trackCost(cost float64) bool {
	o.costMu.Lock()
	defer o.costMu.Unlock()
	o.spentUSD += cost
	if o.budgetUSD > 0 && o.spentUSD > o.budgetUSD {
		return true
	}
	return false
}

// New creates a swarm orchestrator.
// model and registry may be nil (falls back to single subtask / local runner).
func New(db *sql.DB, model agent.ModelAdapter, registry RegistryLookup, runner TaskRunner) *Orchestrator {
	return &Orchestrator{db: db, model: model, registry: registry, runner: runner}
}

// Decompose uses the LLM to split a goal into subtasks.
// Returns a single-subtask plan on failure or when model is nil.
func (o *Orchestrator) Decompose(ctx context.Context, goal string) (*Plan, error) {
	if o.model == nil {
		return &Plan{Goal: goal, Subtasks: []Subtask{{ID: "sub-1", Task: goal}}}, nil
	}
	prompt := fmt.Sprintf(
		"Decompose the following goal into 1-5 subtasks. "+
			"Respond ONLY with JSON: {\"subtasks\":[{\"id\":\"sub-1\",\"task\":\"...\",\"depends_on\":[],\"capability_hint\":\"\"}]}\n\nGoal: %s",
		goal)

	resp, err := o.model.Complete(ctx, []agent.Message{{Role: "user", Content: prompt}})
	if err != nil || resp.Content == "" {
		return &Plan{Goal: goal, Subtasks: []Subtask{{ID: "sub-1", Task: goal}}}, nil
	}

	content := resp.Content
	if start := strings.Index(content, "{"); start >= 0 {
		if end := strings.LastIndex(content, "}"); end > start {
			content = content[start : end+1]
		}
	}

	var parsed struct {
		Subtasks []Subtask `json:"subtasks"`
	}
	if err := json.Unmarshal([]byte(content), &parsed); err != nil || len(parsed.Subtasks) == 0 {
		return &Plan{Goal: goal, Subtasks: []Subtask{{ID: "sub-1", Task: goal}}}, nil
	}
	return &Plan{Goal: goal, Subtasks: parsed.Subtasks}, nil
}

// Run decomposes the goal, executes subtasks, and returns the synthesized answer.
func (o *Orchestrator) Run(ctx context.Context, runID, goal string) (string, error) {
	if runID == "" {
		runID = fmt.Sprintf("swarm-%d", time.Now().UnixNano())
	}

	plan, err := o.Decompose(ctx, goal)
	if err != nil {
		return "", fmt.Errorf("swarm: decompose: %w", err)
	}

	planJSON, err := json.Marshal(plan)
	if err != nil {
		return "", fmt.Errorf("swarm: marshal plan: %w", err)
	}
	traceID := traceIDFromContext(ctx)
	if traceID == "" {
		traceID = fmt.Sprintf("trace-%d", time.Now().UnixNano())
	}

	if _, dbErr := o.db.ExecContext(ctx,
		`INSERT INTO swarm_runs (run_id, goal, plan_json, status, trace_id, started_at)
		 VALUES (?, ?, ?, 'running', ?, ?)`,
		runID, goal, string(planJSON), traceID, time.Now().UTC().Format(time.RFC3339)); dbErr != nil {
		log.Printf("swarm: insert run %s: %v", runID, dbErr)
	}

	results, execErr := o.executePlan(ctx, plan)

	status := "completed"
	if execErr != nil {
		status = "failed"
	}
	if _, dbErr := o.db.ExecContext(ctx,
		`UPDATE swarm_runs SET status = ?, completed_at = ? WHERE run_id = ?`,
		status, time.Now().UTC().Format(time.RFC3339), runID); dbErr != nil {
		log.Printf("swarm: update run %s status: %v", runID, dbErr)
	}

	if execErr != nil {
		return "", execErr
	}
	return o.aggregate(ctx, goal, results), nil
}

func (o *Orchestrator) executePlan(ctx context.Context, plan *Plan) (map[string]string, error) {
	completed := make(map[string]string)
	var mu sync.Mutex

	remaining := make([]Subtask, len(plan.Subtasks))
	copy(remaining, plan.Subtasks)

	// costPerCall is a rough estimate for budget tracking.
	const costPerCall = 0.01

	for len(remaining) > 0 {
		// Check budget before starting a new wave.
		o.costMu.Lock()
		budgetExceeded := o.budgetUSD > 0 && o.spentUSD >= o.budgetUSD
		o.costMu.Unlock()
		if budgetExceeded {
			log.Printf("swarm: budget exceeded (spent $%.4f / $%.4f), aborting %d remaining subtasks",
				o.spentUSD, o.budgetUSD, len(remaining))
			for _, sub := range remaining {
				mu.Lock()
				completed[sub.ID] = "[aborted: budget exceeded]"
				mu.Unlock()
			}
			break
		}

		var wave, deferred []Subtask
		for _, sub := range remaining {
			ready := true
			for _, dep := range sub.DependsOn {
				mu.Lock()
				_, done := completed[dep]
				mu.Unlock()
				if !done {
					ready = false
					break
				}
			}
			if ready {
				wave = append(wave, sub)
			} else {
				deferred = append(deferred, sub)
			}
		}
		if len(wave) == 0 {
			wave = remaining
			deferred = nil
		}

		var wg sync.WaitGroup
		for _, sub := range wave {
			wg.Add(1)
			go func(s Subtask) {
				defer wg.Done()
				result, err := o.runner.RunTask(ctx, s.Task)
				// Track cost for budget enforcement.
				o.trackCost(costPerCall)
				mu.Lock()
				defer mu.Unlock()
				if err != nil {
					completed[s.ID] = fmt.Sprintf("[error: %v]", err)
				} else {
					completed[s.ID] = result
				}
			}(sub)
		}
		wg.Wait()
		remaining = deferred
	}
	return completed, nil
}

func (o *Orchestrator) aggregate(ctx context.Context, goal string, results map[string]string) string {
	if len(results) == 1 {
		for _, v := range results {
			return v
		}
	}
	if o.model == nil {
		var parts []string
		for id, r := range results {
			parts = append(parts, fmt.Sprintf("[%s]: %s", id, r))
		}
		return strings.Join(parts, "\n")
	}
	var resultParts []string
	for id, r := range results {
		resultParts = append(resultParts, fmt.Sprintf("Subtask %s: %s", id, r))
	}
	prompt := fmt.Sprintf(
		"Synthesize these subtask results into a cohesive answer for goal: %q\n\n%s",
		goal, strings.Join(resultParts, "\n\n"))
	resp, err := o.model.Complete(ctx, []agent.Message{{Role: "user", Content: prompt}})
	if err != nil || resp.Content == "" {
		var parts []string
		for _, v := range results {
			parts = append(parts, v)
		}
		return strings.Join(parts, "\n")
	}
	return resp.Content
}

type traceKey struct{}

// WithTraceID attaches a trace ID to the context for swarm runs.
func WithTraceID(ctx context.Context, traceID string) context.Context {
	return context.WithValue(ctx, traceKey{}, traceID)
}

func traceIDFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(traceKey{}).(string); ok {
		return v
	}
	return ""
}
