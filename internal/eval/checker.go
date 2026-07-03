package eval

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// TaskResult is the judged outcome of one task execution.
type TaskResult struct {
	TaskID     string   `json:"task_id"`
	Success    bool     `json:"success"`
	Failures   []string `json:"failures,omitempty"`
	ToolCalls  int      `json:"tool_calls"`
	ToolErrors int      `json:"tool_errors"`
	Retries    int      `json:"retries"`
	TokensIn   int      `json:"tokens_in"`
	TokensOut  int      `json:"tokens_out"`
	CostUSD    float64  `json:"cost_usd"`
	WallMS     int64    `json:"wall_ms"`
}

// Judge evaluates a trace against a task's checks and budget. Every check is
// deterministic; there is no model in the gate.
func Judge(t Task, tr Trace, workspace string) TaskResult {
	res := TaskResult{
		TaskID:     t.ID,
		ToolCalls:  len(tr.Calls),
		ToolErrors: tr.ToolErrors,
		Retries:    countRetries(tr),
		TokensIn:   tr.TokensIn,
		TokensOut:  tr.TokensOut,
		CostUSD:    tr.CostUSD,
		WallMS:     tr.WallMS,
	}

	if tr.RunError != "" {
		res.Failures = append(res.Failures, "run: "+tr.RunError)
	}
	if t.Budget.MaxToolCalls != nil && len(tr.Calls) > *t.Budget.MaxToolCalls {
		res.Failures = append(res.Failures,
			fmt.Sprintf("budget: %d tool calls > max %d", len(tr.Calls), *t.Budget.MaxToolCalls))
	}
	if t.Budget.MaxTokens > 0 && tr.TokensIn+tr.TokensOut > t.Budget.MaxTokens {
		res.Failures = append(res.Failures,
			fmt.Sprintf("budget: %d tokens > max %d", tr.TokensIn+tr.TokensOut, t.Budget.MaxTokens))
	}

	for _, c := range t.Checks {
		if msg := runCheck(c, tr, workspace); msg != "" {
			res.Failures = append(res.Failures, msg)
		}
	}
	res.Success = len(res.Failures) == 0
	return res
}

// countRetries counts error→same-tool-again correction cycles: the loop fed
// an error back and the model reattempted the same tool.
func countRetries(tr Trace) int {
	retries := 0
	for i := 1; i < len(tr.Calls); i++ {
		if tr.Calls[i-1].IsError && tr.Calls[i].Name == tr.Calls[i-1].Name {
			retries++
		}
	}
	return retries
}

func runCheck(c Check, tr Trace, workspace string) string {
	switch c.Kind {
	case "output_contains":
		if !strings.Contains(tr.Output, c.Value) {
			return fmt.Sprintf("output_contains: %q not in output", c.Value)
		}
	case "output_regex":
		re, err := regexp.Compile(c.Value)
		if err != nil {
			return fmt.Sprintf("output_regex: bad pattern %q: %v", c.Value, err)
		}
		if !re.MatchString(tr.Output) {
			return fmt.Sprintf("output_regex: %q did not match output", c.Value)
		}
	case "file_equals":
		data, err := os.ReadFile(filepath.Join(workspace, c.Target))
		if err != nil {
			return fmt.Sprintf("file_equals: %s: %v", c.Target, err)
		}
		if string(data) != c.Value {
			return fmt.Sprintf("file_equals: %s content mismatch (got %d bytes)", c.Target, len(data))
		}
	case "file_contains":
		data, err := os.ReadFile(filepath.Join(workspace, c.Target))
		if err != nil {
			return fmt.Sprintf("file_contains: %s: %v", c.Target, err)
		}
		if !strings.Contains(string(data), c.Value) {
			return fmt.Sprintf("file_contains: %q not in %s", c.Value, c.Target)
		}
	case "file_absent":
		if _, err := os.Stat(filepath.Join(workspace, c.Target)); !os.IsNotExist(err) {
			return fmt.Sprintf("file_absent: %s exists", c.Target)
		}
	case "tool_called":
		for _, call := range tr.Calls {
			if call.Name == c.Target {
				return ""
			}
		}
		return fmt.Sprintf("tool_called: %s never called", c.Target)
	case "tool_not_called":
		for _, call := range tr.Calls {
			if call.Name == c.Target {
				return fmt.Sprintf("tool_not_called: %s was called", c.Target)
			}
		}
	case "tool_order":
		want := strings.Split(c.Value, ",")
		i := 0
		for _, call := range tr.Calls {
			if i < len(want) && call.Name == strings.TrimSpace(want[i]) {
				i++
			}
		}
		if i != len(want) {
			return fmt.Sprintf("tool_order: sequence %q not satisfied (matched %d/%d)", c.Value, i, len(want))
		}
	case "max_tool_errors":
		max, err := strconv.Atoi(c.Value)
		if err != nil {
			return fmt.Sprintf("max_tool_errors: bad value %q", c.Value)
		}
		if tr.ToolErrors > max {
			return fmt.Sprintf("max_tool_errors: %d errors > max %d", tr.ToolErrors, max)
		}
	case "min_tool_errors":
		min, err := strconv.Atoi(c.Value)
		if err != nil {
			return fmt.Sprintf("min_tool_errors: bad value %q", c.Value)
		}
		if tr.ToolErrors < min {
			return fmt.Sprintf("min_tool_errors: %d errors < min %d — an expected refusal did not happen", tr.ToolErrors, min)
		}
	default:
		return fmt.Sprintf("unknown check kind %q", c.Kind)
	}
	return ""
}
