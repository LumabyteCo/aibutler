package missionruntime

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/LumabyteCo/aibutler/internal/agent"
	"github.com/LumabyteCo/aibutler/internal/agent/worker"
	"github.com/LumabyteCo/aibutler/internal/capability"
)

// LLMExecutorConfig wires an LLM-backed worker.TaskExecutor that runs
// each task through the existing agent loop with the configured model
// adapter, tool dispatcher, and capability set.
//
// The agent loop already enforces:
//
//   - Per-task tool-call cap (MaxToolCalls)
//   - Per-task wall-clock timeout (StepTimeout)
//   - Per-task USD budget (BudgetCapPerStep — translated to
//     agent.Config.BudgetCap)
//
// Mission-wide budget enforcement (e.g. mission.cost_so_far_usd >=
// mission.budget_usd) is intentionally out of scope here — the
// supervisor is the natural place for that since it walks all the
// step results. A follow-up commit can sum CostUSD across step results
// and call Manager.Cancel when the mission budget is exceeded; the
// per-step cap below already prevents any single step from blowing
// the budget by an order of magnitude.
type LLMExecutorConfig struct {
	// Model is the LLM adapter (Claude, OpenAI, Ollama, etc.). Required.
	Model agent.ModelAdapter
	// Tools is the dispatcher that workers use to invoke tools.
	// Typically *tool.Dispatcher from the main app. Required.
	Tools agent.ToolExecutor
	// Caps is the capability set granted to each spawned worker.
	// If nil, capability.MessagingDefaults() is used — workers can
	// invoke common channel / memory / data tools but not raw shell.
	Caps *capability.CapabilitySet
	// SessionPrefix is prepended to the agent SessionID so audit
	// rows can be filtered to mission workers (e.g. "mission-").
	// Default: "mission-".
	SessionPrefix string
	// MaxToolCalls caps tool invocations per worker step. Default 30.
	MaxToolCalls int
	// StepTimeout caps wall-clock time per worker step. Default 2 min.
	StepTimeout time.Duration
	// BudgetCapPerStep caps USD spend per worker step. Default $0.50.
	// Set to 0 to disable per-step budget enforcement.
	BudgetCapPerStep float64
}

// NewLLMExecutor returns a worker.TaskExecutor that runs each Task
// through the existing agent.Run loop. Each worker step gets a fresh
// agent.Agent with isolated state, MaxToolCalls + Timeout + BudgetCap
// enforced by the agent core.
//
// The returned closure captures cfg by value — safe to call across
// many concurrent workers without locking.
func NewLLMExecutor(cfg LLMExecutorConfig) (worker.TaskExecutor, error) {
	if cfg.Model == nil {
		return nil, errors.New("missionruntime: LLMExecutorConfig.Model is required")
	}
	if cfg.Tools == nil {
		return nil, errors.New("missionruntime: LLMExecutorConfig.Tools is required")
	}

	if cfg.SessionPrefix == "" {
		cfg.SessionPrefix = "mission-"
	}
	if cfg.MaxToolCalls <= 0 {
		cfg.MaxToolCalls = 30
	}
	if cfg.StepTimeout <= 0 {
		cfg.StepTimeout = 2 * time.Minute
	}
	if cfg.BudgetCapPerStep < 0 {
		cfg.BudgetCapPerStep = 0
	}
	if cfg.Caps == nil {
		cfg.Caps = capability.NewCapabilitySet(capability.MessagingDefaults())
	}

	return func(ctx context.Context, task worker.Task) (string, error) {
		agentCfg := agent.Config{
			ID:           "worker-" + task.StepID,
			SessionID:    cfg.SessionPrefix + task.MissionID,
			ParentID:     task.MissionID,
			Task:         task.Task,
			Type:         agent.TypeBackground,
			Model:        cfg.Model,
			Tools:        cfg.Tools,
			Caps:         cfg.Caps,
			Mode:         agent.ModeSingle,
			MaxToolCalls: cfg.MaxToolCalls,
			Timeout:      cfg.StepTimeout,
			BudgetCap:    cfg.BudgetCapPerStep,
		}

		ag := agent.New(agentCfg)
		result, err := ag.Run(ctx)
		if err != nil {
			return "", fmt.Errorf("agent run: %w", err)
		}

		// Map agent result statuses to worker outcomes. Cancelled,
		// Failed, and BudgetExceeded all surface as errors so the
		// supervisor records the step as failed.
		switch result.Status {
		case agent.StateCompleted:
			return result.Output, nil
		case agent.StateCancelled:
			return result.Output, fmt.Errorf("step cancelled: %s", result.Error)
		case agent.StateFailed:
			return result.Output, fmt.Errorf("step failed: %s", result.Error)
		default:
			// Unknown / in-progress — shouldn't happen post-Run.
			return result.Output, fmt.Errorf("step ended in unexpected state: %s", result.Status)
		}
	}, nil
}
