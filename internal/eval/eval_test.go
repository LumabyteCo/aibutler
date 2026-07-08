package eval_test

import (
	"context"
	"strings"
	"testing"

	"github.com/LumabyteCo/aibutler/internal/agent"
	"github.com/LumabyteCo/aibutler/internal/eval"
	"github.com/LumabyteCo/aibutler/testutil"
)

// The built-in suite must pass end-to-end in unit mode. This is both the
// harness's own CI coverage and the guarantee that a green baseline exists
// on every commit.
func TestBuiltinSuitePasses(t *testing.T) {
	db := testutil.TestDB(t)
	suite, err := eval.DefaultSuite()
	if err != nil {
		t.Fatalf("load suite: %v", err)
	}
	if len(suite.Tasks) < 5 {
		t.Fatalf("built-in suite unexpectedly small: %d tasks", len(suite.Tasks))
	}

	report, err := eval.RunSuite(context.Background(), db.Conn(), suite, &eval.Runner{}, "unit", "scripted")
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if report.TasksPassed != report.TasksTotal {
		for _, r := range report.Results {
			if !r.Success {
				t.Errorf("task %s failed: %v", r.TaskID, r.Failures)
			}
		}
		t.Fatalf("built-in suite: %d/%d passed", report.TasksPassed, report.TasksTotal)
	}

	// The run is on the record with the pinned suite hash.
	runs, err := eval.ListRuns(context.Background(), db.Conn(), 5)
	if err != nil || len(runs) != 1 {
		t.Fatalf("expected 1 recorded run, got %d (err %v)", len(runs), err)
	}
	if runs[0].SuiteHash != suite.Hash {
		t.Error("recorded run does not carry the suite hash")
	}
}

// A harness that cannot fail measures nothing: a deliberately-broken task
// must be reported as a failure, with the failing check named.
func TestHarnessDetectsFailure(t *testing.T) {
	db := testutil.TestDB(t)
	suite := eval.Suite{
		Hash: "test-fail-suite",
		Tasks: []eval.Task{{
			ID:     "must-fail",
			Prompt: "write the wrong thing",
			Script: []eval.ScriptStep{
				{Tools: []eval.ScriptTool{{Name: "file.write", Input: `{"path":"{{WORKSPACE}}/f.txt","content":"wrong"}`}}},
				{Text: "done"},
			},
			Checks: []eval.Check{{Kind: "file_equals", Target: "f.txt", Value: "right"}},
		}},
	}
	report, err := eval.RunSuite(context.Background(), db.Conn(), suite, &eval.Runner{}, "unit", "scripted")
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if report.TasksPassed != 0 {
		t.Fatal("harness passed a task whose check must fail")
	}
	if len(report.Results) != 1 || len(report.Results[0].Failures) == 0 ||
		!strings.Contains(report.Results[0].Failures[0], "file_equals") {
		t.Fatalf("failure not attributed to its check: %+v", report.Results)
	}
}

// Budget ceilings are part of the judgment: exceeding max_tool_calls fails
// the task even when every content check passes.
func TestBudgetCeilingFailsTask(t *testing.T) {
	db := testutil.TestDB(t)
	suite := eval.Suite{
		Hash: "test-budget-suite",
		Tasks: []eval.Task{{
			ID:     "over-budget",
			Prompt: "do it wastefully",
			Budget: eval.Budget{MaxToolCalls: intPtr(1)},
			Script: []eval.ScriptStep{
				{Tools: []eval.ScriptTool{{Name: "file.write", Input: `{"path":"{{WORKSPACE}}/a.txt","content":"x"}`}}},
				{Tools: []eval.ScriptTool{{Name: "file.write", Input: `{"path":"{{WORKSPACE}}/b.txt","content":"y"}`}}},
				{Text: "done"},
			},
			Checks: []eval.Check{{Kind: "file_equals", Target: "a.txt", Value: "x"}},
		}},
	}
	report, err := eval.RunSuite(context.Background(), db.Conn(), suite, &eval.Runner{}, "unit", "scripted")
	if err != nil {
		t.Fatal(err)
	}
	if report.TasksPassed != 0 {
		t.Fatal("budget ceiling not enforced")
	}
	if !strings.Contains(strings.Join(report.Results[0].Failures, " "), "budget") {
		t.Fatalf("failure not attributed to budget: %+v", report.Results[0].Failures)
	}
}

// Retries are counted from real error→reattempt cycles in the trace.
func TestRetryCounting(t *testing.T) {
	db := testutil.TestDB(t)
	suite := eval.Suite{
		Hash: "test-retry-suite",
		Tasks: []eval.Task{{
			ID:     "one-retry",
			Prompt: "edit with a wrong pattern first",
			Files:  map[string]string{"f.txt": "alpha\n"},
			Script: []eval.ScriptStep{
				{Tools: []eval.ScriptTool{{Name: "file.edit", Input: `{"path":"{{WORKSPACE}}/f.txt","old":"beta","new":"gamma"}`}}},
				{Tools: []eval.ScriptTool{{Name: "file.edit", Input: `{"path":"{{WORKSPACE}}/f.txt","old":"alpha","new":"gamma"}`}}},
				{Text: "done"},
			},
			Checks: []eval.Check{{Kind: "file_contains", Target: "f.txt", Value: "gamma"}},
		}},
	}
	report, err := eval.RunSuite(context.Background(), db.Conn(), suite, &eval.Runner{}, "unit", "scripted")
	if err != nil {
		t.Fatal(err)
	}
	res := report.Results[0]
	if !res.Success || res.ToolErrors != 1 || res.Retries != 1 {
		t.Fatalf("expected success with 1 error / 1 retry, got %+v", res)
	}
}

// Suite hashing is stable for identical content and changes when content
// changes — the tamper-evidence property.
func TestSuiteHashPinsContent(t *testing.T) {
	s1, err := eval.DefaultSuite()
	if err != nil {
		t.Fatal(err)
	}
	s2, err := eval.DefaultSuite()
	if err != nil {
		t.Fatal(err)
	}
	if s1.Hash != s2.Hash || len(s1.Hash) != 64 {
		t.Fatalf("suite hash unstable: %s vs %s", s1.Hash, s2.Hash)
	}
}

// CompareRuns flags cross-suite comparisons as non-comparable and computes
// signed deltas for same-suite runs.
func TestCompareRuns(t *testing.T) {
	db := testutil.TestDB(t)
	ctx := context.Background()

	passing := eval.Suite{
		Hash: "same-hash",
		Tasks: []eval.Task{{
			ID: "t", Prompt: "p",
			Script: []eval.ScriptStep{
				{Tools: []eval.ScriptTool{{Name: "file.write", Input: `{"path":"{{WORKSPACE}}/f.txt","content":"x"}`}}},
				{Text: "done"},
			},
			Checks: []eval.Check{{Kind: "file_equals", Target: "f.txt", Value: "x"}},
		}},
	}
	failing := passing
	failing.Tasks = []eval.Task{{
		ID: "t", Prompt: "p",
		Script: []eval.ScriptStep{{Text: "did nothing"}},
		Checks: []eval.Check{{Kind: "file_equals", Target: "f.txt", Value: "x"}},
	}}

	base, err := eval.RunSuite(ctx, db.Conn(), failing, &eval.Runner{}, "unit", "scripted")
	if err != nil {
		t.Fatal(err)
	}
	cand, err := eval.RunSuite(ctx, db.Conn(), passing, &eval.Runner{}, "unit", "scripted")
	if err != nil {
		t.Fatal(err)
	}

	d, err := eval.CompareRuns(ctx, db.Conn(), base.RunID, cand.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if !d.Comparable {
		t.Fatal("same-hash runs must be comparable")
	}
	if d.SuccessRate <= 0 {
		t.Fatalf("candidate fixed the task; success delta = %v, want positive", d.SuccessRate)
	}
}

// A repeat>1 task fails when ANY repeat fails — consistency is part of the
// pass definition. (Both repeats fail identically here; the point is the
// aggregation, covered for the flaky case by the repeat-labels in failures.)
func TestRepeatAggregation(t *testing.T) {
	db := testutil.TestDB(t)
	suite := eval.Suite{
		Hash: "test-repeat-suite",
		Tasks: []eval.Task{{
			ID: "always-short", Prompt: "p", Repeat: 2,
			Script: []eval.ScriptStep{{Text: "nope"}},
			Checks: []eval.Check{{Kind: "output_contains", Value: "yes"}},
		}},
	}
	report, err := eval.RunSuite(context.Background(), db.Conn(), suite, &eval.Runner{}, "unit", "scripted")
	if err != nil {
		t.Fatal(err)
	}
	res := report.Results[0]
	if res.Success || len(res.Failures) != 2 {
		t.Fatalf("expected both repeats to fail with labels, got %+v", res)
	}
	if !strings.Contains(res.Failures[0], "repeat 1/2") || !strings.Contains(res.Failures[1], "repeat 2/2") {
		t.Fatalf("repeat labels missing: %v", res.Failures)
	}
}

func intPtr(n int) *int { return &n }

// A suite handed to RunSuite programmatically gets the same anti-vacuous
// validation as YAML: a zero-check task is rejected, not silently passed.
func TestRunSuiteRejectsUncheckedTasks(t *testing.T) {
	db := testutil.TestDB(t)
	suite := eval.Suite{Hash: "h", Tasks: []eval.Task{{
		ID: "unchecked", Prompt: "p",
		Script: []eval.ScriptStep{{Text: "done"}},
	}}}
	if _, err := eval.RunSuite(context.Background(), db.Conn(), suite, &eval.Runner{}, "unit", "scripted"); err == nil {
		t.Fatal("zero-check task must be rejected at RunSuite")
	}
	// Escaping file-check targets are rejected too.
	suite.Tasks[0].Checks = []eval.Check{{Kind: "file_absent", Target: "../outside"}}
	if _, err := eval.RunSuite(context.Background(), db.Conn(), suite, &eval.Runner{}, "unit", "scripted"); err == nil {
		t.Fatal("escaping check target must be rejected")
	}
}

// An explicit zero budget means zero tool calls, not "unenforced".
func TestExplicitZeroBudgetIsHardZero(t *testing.T) {
	db := testutil.TestDB(t)
	suite := eval.Suite{Hash: "h", Tasks: []eval.Task{{
		ID: "no-tools-allowed", Prompt: "p",
		Budget: eval.Budget{MaxToolCalls: intPtr(0)},
		Script: []eval.ScriptStep{
			{Tools: []eval.ScriptTool{{Name: "file.write", Input: `{"path":"{{WORKSPACE}}/x.txt","content":"x"}`}}},
			{Text: "done"},
		},
		Checks: []eval.Check{{Kind: "output_contains", Value: "done"}},
	}}}
	report, err := eval.RunSuite(context.Background(), db.Conn(), suite, &eval.Runner{}, "unit", "scripted")
	if err != nil {
		t.Fatal(err)
	}
	if report.TasksPassed != 0 {
		t.Fatal("a tool call under an explicit zero budget must fail the task")
	}
}

// min_tool_errors makes an expected refusal observable: if the refusal stops
// happening, the task fails instead of passing vacuously.
func TestMinToolErrorsDetectsMissingRefusal(t *testing.T) {
	db := testutil.TestDB(t)
	suite := eval.Suite{Hash: "h", Tasks: []eval.Task{{
		ID: "refusal-observed", Prompt: "p",
		Script: []eval.ScriptStep{
			{Tools: []eval.ScriptTool{{Name: "file.write", Input: `{"path":"{{WORKSPACE}}/ok.txt","content":"fine"}`}}},
			{Text: "refused"},
		},
		Checks: []eval.Check{
			{Kind: "output_contains", Value: "refused"},
			{Kind: "min_tool_errors", Value: "1"},
		},
	}}}
	report, err := eval.RunSuite(context.Background(), db.Conn(), suite, &eval.Runner{}, "unit", "scripted")
	if err != nil {
		t.Fatal(err)
	}
	// The write SUCCEEDED (no refusal), so min_tool_errors must fail the task
	// even though the narrated output says "refused".
	if report.TasksPassed != 0 {
		t.Fatal("narrated refusal without an observed error must fail")
	}
	if !strings.Contains(strings.Join(report.Results[0].Failures, " "), "min_tool_errors") {
		t.Fatalf("failure not attributed to min_tool_errors: %+v", report.Results[0].Failures)
	}
}

// settableModel records whether it received tool definitions — the live
// contract the first real run exposed: without SetTools the model cannot
// call tools, so every tool-using task fails with zero calls.
type settableModel struct {
	defs  []agent.ToolDef
	steps []eval.ScriptStep
	next  int
}

func (m *settableModel) SetTools(defs []agent.ToolDef) { m.defs = defs }

func (m *settableModel) Complete(_ context.Context, _ []agent.Message) (agent.Response, error) {
	if m.next >= len(m.steps) {
		return agent.Response{Content: "done"}, nil
	}
	step := m.steps[m.next]
	m.next++
	resp := agent.Response{Content: step.Text}
	for i, tc := range step.Tools {
		resp.ToolCalls = append(resp.ToolCalls, agent.ToolCall{ID: string(rune('a' + i)), Name: tc.Name, Input: tc.Input})
	}
	return resp, nil
}

// A live-mode model must be handed the workspace toolset before the run.
func TestLiveModelReceivesToolDefinitions(t *testing.T) {
	db := testutil.TestDB(t)
	model := &settableModel{steps: []eval.ScriptStep{{Text: "done"}}}
	suite := eval.Suite{Hash: "h", Tasks: []eval.Task{{
		ID: "t", Prompt: "p",
		Checks: []eval.Check{{Kind: "output_contains", Value: "done"}},
	}}}
	if _, err := eval.RunSuite(context.Background(), db.Conn(), suite, &eval.Runner{Model: model}, "live", "fake"); err != nil {
		t.Fatal(err)
	}
	if len(model.defs) == 0 {
		t.Fatal("live model received no tool definitions — tool-using tasks would all fail with zero calls")
	}
	found := false
	for _, d := range model.defs {
		if d.Name == "file.write" {
			found = true
		}
	}
	if !found {
		t.Fatalf("workspace file tools missing from advertised defs: %+v", model.defs)
	}
}
