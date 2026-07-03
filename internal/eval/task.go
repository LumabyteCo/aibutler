// Package eval is the internal benchmark harness: representative task
// definitions with deterministic checkers, so changes to skills, prompt
// heuristics, or tool-selection logic are judged by measured deltas —
// success rate, retries, tool errors, tokens, wall time — not impression.
//
// Two execution modes share the same task definitions and the same isolated
// workspace-rooted toolset:
//
//   - unit: a scripted model drives the REAL agent loop, dispatcher, and
//     tools deterministically. Measures loop/dispatch/tool mechanics in CI.
//   - live: a real model provider replaces the script. Measures end-to-end
//     behavior; results are only comparable across runs with equal suite
//     hashes.
//
// Integrity: the default suite is compiled into the binary via go:embed, and
// every recorded run carries the sha256 of the exact suite content it ran
// against — a result measured against a modified suite is visibly
// non-comparable. Checkers are pure Go; no model judges the gate.
package eval

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

//go:embed suites/*.yaml
var embeddedSuites embed.FS

// Task is one benchmark scenario.
type Task struct {
	ID          string            `yaml:"id"`
	Description string            `yaml:"description"`
	Prompt      string            `yaml:"prompt"`
	Files       map[string]string `yaml:"files"`  // workspace pre-state (relative path → content)
	Script      []ScriptStep      `yaml:"script"` // unit-mode model turns; ignored in live mode
	Budget      Budget            `yaml:"budget"`
	Checks      []Check           `yaml:"checks"`
	Repeat      int               `yaml:"repeat"` // consistency runs (default 1); a task passes only if ALL repeats pass
}

// ScriptStep is one scripted model turn for unit mode. A step with tool
// calls returns them; a step with only text ends the run.
type ScriptStep struct {
	Text  string       `yaml:"text"`
	Tools []ScriptTool `yaml:"tools"`
}

// ScriptTool is one scripted tool invocation.
type ScriptTool struct {
	Name  string `yaml:"name"`
	Input string `yaml:"input"` // JSON
}

// Budget bounds a task; exceeding it fails the task regardless of checks.
// MaxToolCalls is a pointer so an explicit 0 means "no tool calls allowed"
// while absence means the default ceiling — a sentinel 0 would silently turn
// the strictest budget into no budget at all.
type Budget struct {
	MaxToolCalls *int `yaml:"max_tool_calls"` // nil = default 25; 0 = hard zero
	MaxTokens    int  `yaml:"max_tokens"`     // 0 = unlimited (unit mode emits zero tokens)
}

// Check is one deterministic assertion about a run.
//
// Kinds:
//
//	output_contains  — final output contains Value
//	output_regex     — final output matches Value (RE2)
//	file_equals      — workspace file Target has exactly Value as content
//	file_contains    — workspace file Target contains Value
//	file_absent      — workspace file Target does not exist
//	tool_called      — some call used tool Target
//	tool_not_called  — no call used tool Target
//	tool_order       — comma-separated Value appears as a subsequence of calls
//	max_tool_errors  — at most Value (integer) calls returned errors
//	min_tool_errors  — at least Value (integer) calls returned errors; use it
//	                   when a refusal IS the expected behavior, so a safety
//	                   regression that lets the call succeed fails the task
type Check struct {
	Kind   string `yaml:"kind"`
	Target string `yaml:"target"`
	Value  string `yaml:"value"`
}

// Suite is a loaded set of tasks plus the content hash that pins it.
type Suite struct {
	Tasks []Task
	Hash  string // sha256 over sorted (name, content) pairs
	Name  string
}

// DefaultSuite loads the suite compiled into the binary.
func DefaultSuite() (Suite, error) {
	return loadSuiteFS(embeddedSuites, "suites", "builtin")
}

// LoadSuiteDir loads a user-provided suite from a directory of YAML files.
func LoadSuiteDir(dir string) (Suite, error) {
	return loadSuiteFS(os.DirFS(dir), ".", filepath.Base(dir))
}

func loadSuiteFS(fsys fs.FS, root, name string) (Suite, error) {
	entries, err := fs.ReadDir(fsys, root)
	if err != nil {
		return Suite{}, fmt.Errorf("eval: read suite: %w", err)
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && (strings.HasSuffix(e.Name(), ".yaml") || strings.HasSuffix(e.Name(), ".yml")) {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	if len(names) == 0 {
		return Suite{}, fmt.Errorf("eval: no task files in suite")
	}

	h := sha256.New()
	var tasks []Task
	seen := map[string]bool{}
	for _, n := range names {
		content, err := fs.ReadFile(fsys, filepath.ToSlash(filepath.Join(root, n)))
		if err != nil {
			return Suite{}, fmt.Errorf("eval: read %s: %w", n, err)
		}
		h.Write([]byte(n))
		h.Write([]byte{0})
		h.Write(content)
		h.Write([]byte{0})

		var fileTasks []Task
		if err := yaml.Unmarshal(content, &fileTasks); err != nil {
			// A file may also hold a single task document.
			var one Task
			if err2 := yaml.Unmarshal(content, &one); err2 != nil || one.ID == "" {
				return Suite{}, fmt.Errorf("eval: parse %s: %w", n, err)
			}
			fileTasks = []Task{one}
		}
		for _, t := range fileTasks {
			if err := ValidateTask(t); err != nil {
				return Suite{}, fmt.Errorf("eval: %s: %w", n, err)
			}
			if seen[t.ID] {
				return Suite{}, fmt.Errorf("eval: duplicate task id %q", t.ID)
			}
			seen[t.ID] = true
			tasks = append(tasks, t)
		}
	}
	return Suite{Tasks: tasks, Hash: hex.EncodeToString(h.Sum(nil)), Name: name}, nil
}

var validCheckKinds = map[string]bool{
	"output_contains": true, "output_regex": true,
	"file_equals": true, "file_contains": true, "file_absent": true,
	"tool_called": true, "tool_not_called": true, "tool_order": true,
	"max_tool_errors": true, "min_tool_errors": true,
}

// fileCheckKinds have workspace-relative Target paths.
var fileCheckKinds = map[string]bool{
	"file_equals": true, "file_contains": true, "file_absent": true,
}

// ValidateTask enforces the anti-vacuous-pass rules. It runs on every load
// path AND at the top of RunSuite, so programmatic callers (the paths future
// self-improvement code uses) get the same guarantees as YAML suites.
func ValidateTask(t Task) error {
	if t.ID == "" {
		return fmt.Errorf("task missing id")
	}
	if t.Prompt == "" {
		return fmt.Errorf("task %s: missing prompt", t.ID)
	}
	if len(t.Checks) == 0 {
		return fmt.Errorf("task %s: no checks — a task that can't fail measures nothing", t.ID)
	}
	for _, c := range t.Checks {
		if !validCheckKinds[c.Kind] {
			return fmt.Errorf("task %s: unknown check kind %q", t.ID, c.Kind)
		}
		// File-check targets must stay inside the workspace, same rule as
		// Files keys — a target that escapes checks the wrong location and
		// turns the check vacuous.
		if fileCheckKinds[c.Kind] {
			if c.Target == "" || filepath.IsAbs(c.Target) || strings.Contains(c.Target, "..") {
				return fmt.Errorf("task %s: check target %q must be workspace-relative", t.ID, c.Target)
			}
		}
	}
	for rel := range t.Files {
		if filepath.IsAbs(rel) || strings.Contains(rel, "..") {
			return fmt.Errorf("task %s: file path %q must be workspace-relative", t.ID, rel)
		}
	}
	return nil
}
