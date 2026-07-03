package eval

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/LumabyteCo/aibutler/internal/agent"
	"github.com/LumabyteCo/aibutler/internal/capability"
	"github.com/LumabyteCo/aibutler/internal/file"
	"github.com/LumabyteCo/aibutler/internal/tool"
)

// Trace is the observable record of one task execution.
type Trace struct {
	Output     string
	Calls      []TraceCall
	ToolErrors int
	TokensIn   int
	TokensOut  int
	CostUSD    float64
	WallMS     int64
	// RunError carries a non-completed run's real cause (timeout, model
	// error) so failures name it instead of a misleading content-check miss.
	RunError string
}

// TraceCall is one executed tool call.
type TraceCall struct {
	Name    string
	Input   string
	Output  string
	IsError bool
}

// Runner executes one task inside an isolated workspace.
type Runner struct {
	// Model supplies completions. Nil selects unit mode: the task's script
	// drives the loop. Non-nil selects live mode with a real provider —
	// tools remain workspace-rooted either way, so eval runs can never
	// touch data outside their temp directory.
	Model agent.ModelAdapter
}

// RunTask materializes the task workspace, wires a workspace-rooted toolset
// through the real dispatcher, runs the real agent loop, and returns the
// trace. The workspace is a fresh temp directory owned by the caller.
//
// The literal {{WORKSPACE}} in the prompt and in scripted tool inputs is
// replaced with the absolute workspace path — file tools resolve relative
// paths against the process working directory, so tasks must name their
// files absolutely, and a live model needs the real path in its prompt for
// exactly the same reason.
func (r *Runner) RunTask(ctx context.Context, t Task, workspace string) (Trace, error) {
	t = expandWorkspace(t, workspace)
	for rel, content := range t.Files {
		p := filepath.Join(workspace, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return Trace{}, fmt.Errorf("eval: workspace: %w", err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			return Trace{}, fmt.Errorf("eval: workspace: %w", err)
		}
	}

	registry := tool.NewRegistry()
	file.RegisterFileTools(registry, []string{workspace})
	dispatcher := tool.NewDispatcher(registry, capability.NewEngine(nil), nil)
	recorder := &recordingExecutor{inner: dispatcher}

	model := r.Model
	if model == nil {
		if len(t.Script) == 0 {
			return Trace{}, fmt.Errorf("eval: task %s has no script; unit mode needs one (or run with a live model)", t.ID)
		}
		model = &scriptedModel{steps: t.Script}
	}

	// The loop gets headroom BEYOND the task budget: if the agent hard-capped
	// at the budget, an over-budget behavior would soft-stop invisibly and
	// look like a pass. The judge fails the task on the visible overage.
	maxCalls := 25
	if t.Budget.MaxToolCalls != nil {
		maxCalls = *t.Budget.MaxToolCalls + 5
	}

	start := time.Now()
	a := agent.New(agent.Config{
		ID:        "eval-" + t.ID,
		SessionID: "eval",
		Task:      t.Prompt,
		Model:     model,
		Tools:     recorder,
		// Sequential execution: a step with several tool calls must record
		// them in order, or tool_order checks become nondeterministic.
		Mode:         agent.ModeSingle,
		MaxToolCalls: maxCalls,
		Timeout:      5 * time.Minute,
	})
	result, err := a.Run(ctx)
	if err != nil {
		return Trace{}, fmt.Errorf("eval: task %s: %w", t.ID, err)
	}

	tr := recorder.trace()
	tr.Output = result.Output
	tr.TokensIn = result.TokensIn
	tr.TokensOut = result.TokensOut
	tr.CostUSD = result.CostUSD
	tr.WallMS = time.Since(start).Milliseconds()
	// The loop reports timeouts/model errors in Result, not as Go errors —
	// surface the true cause so a timeout doesn't masquerade as a content
	// mismatch.
	if result.Status != agent.StateCompleted || result.Error != "" {
		tr.RunError = fmt.Sprintf("status=%s error=%s", result.Status, result.Error)
	}
	return tr, nil
}

// expandWorkspace substitutes the {{WORKSPACE}} placeholder into the fields
// that reach tools and the model. Check targets stay workspace-relative —
// the judge joins them itself.
func expandWorkspace(t Task, workspace string) Task {
	replace := func(s string) string { return strings.ReplaceAll(s, "{{WORKSPACE}}", workspace) }
	t.Prompt = replace(t.Prompt)
	steps := make([]ScriptStep, len(t.Script))
	for i, step := range t.Script {
		steps[i] = ScriptStep{Text: step.Text}
		for _, tc := range step.Tools {
			steps[i].Tools = append(steps[i].Tools, ScriptTool{Name: tc.Name, Input: replace(tc.Input)})
		}
	}
	t.Script = steps
	return t
}

// recordingExecutor wraps the dispatcher to capture every call and outcome.
type recordingExecutor struct {
	inner *tool.Dispatcher
	mu    sync.Mutex
	calls []TraceCall
}

func (r *recordingExecutor) Execute(ctx context.Context, call agent.ToolCall) (string, error) {
	out, err := r.inner.Execute(ctx, call)
	rec := TraceCall{Name: call.Name, Input: call.Input, Output: out, IsError: err != nil}
	if err != nil {
		rec.Output = err.Error()
	}
	r.mu.Lock()
	r.calls = append(r.calls, rec)
	r.mu.Unlock()
	return out, err
}

func (r *recordingExecutor) AvailableTools(ctx context.Context, mode agent.Mode, caps *capability.CapabilitySet) []agent.ToolDef {
	return r.inner.AvailableTools(ctx, mode, caps)
}

func (r *recordingExecutor) trace() Trace {
	r.mu.Lock()
	defer r.mu.Unlock()
	t := Trace{Calls: append([]TraceCall(nil), r.calls...)}
	for _, c := range r.calls {
		if c.IsError {
			t.ToolErrors++
		}
	}
	return t
}

// scriptedModel replays a task's scripted turns through the real loop.
type scriptedModel struct {
	mu    sync.Mutex
	steps []ScriptStep
	next  int
}

func (m *scriptedModel) Complete(_ context.Context, _ []agent.Message) (agent.Response, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.next >= len(m.steps) {
		// Script exhausted: end the run with an empty final answer rather
		// than looping — the checks will judge what happened.
		return agent.Response{Content: ""}, nil
	}
	step := m.steps[m.next]
	m.next++
	resp := agent.Response{Content: step.Text}
	for i, tc := range step.Tools {
		resp.ToolCalls = append(resp.ToolCalls, agent.ToolCall{
			ID:    fmt.Sprintf("eval-%d-%d", m.next, i),
			Name:  tc.Name,
			Input: tc.Input,
		})
	}
	return resp, nil
}
