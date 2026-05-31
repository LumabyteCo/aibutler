package missionruntime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/LumabyteCo/aibutler/internal/agent"
	"github.com/LumabyteCo/aibutler/internal/agent/supervisor"
	"github.com/LumabyteCo/aibutler/internal/mission"
)

// LLMReplannerConfig wires an LLM-backed supervisor.Replanner that
// drives the existing model adapter directly (no tools, no agent
// loop) to produce a recovery sequence after a step failure.
//
// The replanner is invoked from the supervisor at most MaxReplans
// times per mission (set on Supervisor.MaxReplans, default 3). Each
// call sends a single prompt to Model and parses the response as a
// JSON list of steps. Failures to produce valid JSON within
// MaxRetries attempts are surfaced as ordinary errors — the supervisor
// then falls through to its fail-fast path with the failure recorded
// in mission.failed.
type LLMReplannerConfig struct {
	// Model is the LLM adapter used for the replan prompt. Required.
	// The replanner uses Complete directly — no tool dispatching, no
	// nested agent loop. That keeps cost predictable and the parsed
	// output schema strict.
	Model agent.ModelAdapter
	// MaxRetries caps how many times one replan call may re-prompt
	// after a malformed JSON response from the model. Default 1
	// (initial call + one retry).
	MaxRetries int
	// Timeout caps wall-clock per replan call (across all retries).
	// Default 30s. Pass 0 to inherit from the caller's ctx.
	Timeout time.Duration
}

// NewLLMReplanner returns a supervisor.Replanner backed by an LLM. The
// returned closure captures cfg by value so it's safe to share across
// concurrent supervisors.
func NewLLMReplanner(cfg LLMReplannerConfig) (supervisor.Replanner, error) {
	if cfg.Model == nil {
		return nil, errors.New("missionruntime: LLMReplannerConfig.Model is required")
	}
	if cfg.MaxRetries < 0 {
		cfg.MaxRetries = 0
	}
	if cfg.MaxRetries == 0 {
		cfg.MaxRetries = 1
	}
	if cfg.Timeout < 0 {
		cfg.Timeout = 0
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = 30 * time.Second
	}

	return &llmReplanner{cfg: cfg}, nil
}

type llmReplanner struct {
	cfg LLMReplannerConfig
}

// replanResponse is the strict JSON shape the model is asked to produce.
type replanResponse struct {
	// Steps is the replacement sequence from the failure point onward.
	// An empty list means "no recovery possible" — the replanner
	// translates that to supervisor.ErrReplanRejected.
	Steps []replanStep `json:"steps"`
	// Reason is a short human-readable note about the recovery
	// strategy. Stored in the mission.replanned event payload via
	// the supervisor's reason argument.
	Reason string `json:"reason,omitempty"`
}

type replanStep struct {
	Task      string   `json:"task"`
	DependsOn []string `json:"depends_on,omitempty"`
}

func (r *llmReplanner) Replan(ctx context.Context, req supervisor.ReplanRequest) ([]mission.Step, error) {
	callCtx := ctx
	if r.cfg.Timeout > 0 {
		var cancel context.CancelFunc
		callCtx, cancel = context.WithTimeout(ctx, r.cfg.Timeout)
		defer cancel()
	}

	prompt := buildReplanPrompt(req)
	messages := []agent.Message{
		{Role: "system", Content: replanSystemPrompt},
		{Role: "user", Content: prompt},
	}

	var lastErr error
	for attempt := 0; attempt < r.cfg.MaxRetries+1; attempt++ {
		if attempt > 0 {
			// Append a corrective prompt so the model can see its
			// prior malformed output and try again. Keeps the system
			// + initial-user turn intact for context.
			messages = append(messages, agent.Message{
				Role:    "user",
				Content: "Your previous response was not valid JSON in the required shape. Output ONLY a JSON object with `steps` (array) and `reason` (string) fields. No prose, no markdown.",
			})
		}

		resp, err := r.cfg.Model.Complete(callCtx, messages)
		if err != nil {
			lastErr = fmt.Errorf("replan model call: %w", err)
			if callCtx.Err() != nil {
				return nil, lastErr
			}
			continue
		}

		// Record the assistant's response so the next iteration sees
		// it as conversation history.
		messages = append(messages, agent.Message{
			Role:    "assistant",
			Content: resp.Content,
		})

		parsed, perr := parseReplanResponse(resp.Content)
		if perr != nil {
			lastErr = fmt.Errorf("parse replan response: %w", perr)
			continue
		}

		if len(parsed.Steps) == 0 {
			// Model explicitly signalled "no recovery" — propagate as
			// a rejection rather than an implementation error so the
			// supervisor records mission.failed with the underlying
			// step error as the cause.
			return nil, supervisor.ErrReplanRejected
		}

		out := make([]mission.Step, 0, len(parsed.Steps))
		for _, s := range parsed.Steps {
			task := strings.TrimSpace(s.Task)
			if task == "" {
				continue
			}
			out = append(out, mission.Step{
				Task:      task,
				DependsOn: s.DependsOn,
			})
		}
		if len(out) == 0 {
			lastErr = errors.New("replan response had no non-empty steps")
			continue
		}
		return out, nil
	}

	if lastErr == nil {
		lastErr = errors.New("replanner exhausted retries with no parseable response")
	}
	return nil, lastErr
}

const replanSystemPrompt = `You are the replanner for a long-running mission system.

A mission's plan step has just failed. Your job is to propose a
replacement sequence of steps that recovers from the failure and
gets the mission back on track to its stated goal.

Output rules:

  - Respond with ONLY a JSON object — no prose, no markdown fences,
    no commentary before or after.
  - The JSON object has exactly two fields:
      "steps":  an array of step objects, each with "task" (string,
                required) and optionally "depends_on" (array of step
                IDs the new step depends on; usually empty).
      "reason": a short string describing the recovery strategy.
  - If the failure is genuinely unrecoverable (the goal can't be
    reached given what failed and the prior context), respond with
    {"steps": [], "reason": "..."} — an empty steps array signals
    rejection and the mission will be failed with the original error.
  - The replacement steps execute sequentially in the order you
    list them, starting AFTER the failed step. The original
    un-started steps that came after the failed step are
    automatically superseded by your replacement sequence — you do
    NOT need to repeat them in your output.
  - Be concrete. Tasks should be actionable descriptions a worker
    agent could carry out, similar in granularity to the original
    plan's steps.`

func buildReplanPrompt(req supervisor.ReplanRequest) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Mission goal:\n%s\n\n", req.Goal)

	if len(req.CompletedSteps) > 0 {
		b.WriteString("Steps that completed successfully (and their outputs):\n")
		for _, s := range req.CompletedSteps {
			fmt.Fprintf(&b, "- [%s] task: %s\n", s.ID, s.Task)
			if s.Output != "" {
				fmt.Fprintf(&b, "    output: %s\n", truncate(s.Output, 500))
			}
		}
		b.WriteString("\n")
	}

	fmt.Fprintf(&b, "Step that FAILED:\n- [%s] task: %s\n    error: %s\n\n",
		req.FailedStep.ID, req.FailedStep.Task, req.FailureReason)

	if len(req.RemainingSteps) > 0 {
		b.WriteString("Steps that were planned to run AFTER the failed step (these will be superseded by your replan — listed for context only):\n")
		for _, s := range req.RemainingSteps {
			fmt.Fprintf(&b, "- task: %s\n", s.Task)
		}
		b.WriteString("\n")
	}

	if req.PriorReplans > 0 {
		fmt.Fprintf(&b, "Note: this mission has already been replanned %d time(s). Bias toward a conservative, smaller recovery sequence.\n\n", req.PriorReplans)
	}

	b.WriteString("Propose the replacement step sequence as JSON now.")
	return b.String()
}

// parseReplanResponse strips common LLM noise (markdown fences,
// leading/trailing prose) and unmarshals the inner JSON.
func parseReplanResponse(raw string) (*replanResponse, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, errors.New("empty response")
	}

	// Strip markdown code fences if the model wrapped its output
	// despite the instruction not to.
	if strings.HasPrefix(trimmed, "```") {
		// Drop the opening fence line (and optional language tag).
		nl := strings.IndexByte(trimmed, '\n')
		if nl < 0 {
			return nil, errors.New("response is just an opening code fence")
		}
		trimmed = trimmed[nl+1:]
		// Drop the closing fence if present.
		if idx := strings.LastIndex(trimmed, "```"); idx >= 0 {
			trimmed = trimmed[:idx]
		}
		trimmed = strings.TrimSpace(trimmed)
	}

	// If the response still has leading/trailing prose, try to extract
	// the outermost JSON object by brace-matching from the first `{`.
	if start := strings.IndexByte(trimmed, '{'); start > 0 {
		trimmed = trimmed[start:]
	}
	if end := strings.LastIndexByte(trimmed, '}'); end >= 0 && end < len(trimmed)-1 {
		trimmed = trimmed[:end+1]
	}

	var parsed replanResponse
	if err := json.Unmarshal([]byte(trimmed), &parsed); err != nil {
		return nil, err
	}
	return &parsed, nil
}

func truncate(s string, max int) string {
	if max <= 0 || len(s) <= max {
		return s
	}
	return s[:max] + "..."
}
