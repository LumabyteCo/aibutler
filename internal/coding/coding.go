package coding

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const (
	defaultTimeout   = 30 * time.Second
	defaultMaxOutput = 8192
)

// toolRegistry is the narrow interface for registering tools.
type toolRegistry interface {
	Register(name, description, schema, capability string, exec func(ctx context.Context, input string) (string, error))
}

// Runner executes code snippets, linters, and tests.
type Runner struct {
	workDir   string
	timeout   time.Duration
	maxOutput int
}

// NewRunner creates a coding runner.
func NewRunner(workDir string) *Runner {
	return &Runner{
		workDir:   workDir,
		timeout:   defaultTimeout,
		maxOutput: defaultMaxOutput,
	}
}

// SetTimeout configures the execution timeout.
func (r *Runner) SetTimeout(d time.Duration) {
	r.timeout = d
}

// Run executes a code snippet in the given language.
func (r *Runner) Run(ctx context.Context, language, code string) (string, error) {
	switch strings.ToLower(language) {
	case "go", "golang":
		return r.runGo(ctx, code)
	case "python", "python3", "py":
		return r.runCommand(ctx, "python3", "-c", code)
	case "javascript", "js", "node":
		return r.runCommand(ctx, "node", "-e", code)
	default:
		return "", fmt.Errorf("unsupported language: %s", language)
	}
}

// Lint runs a linter for the given language on the code.
func (r *Runner) Lint(ctx context.Context, language, code string) (string, error) {
	switch strings.ToLower(language) {
	case "go", "golang":
		return r.lintGo(ctx, code)
	case "python", "python3", "py":
		return r.runCommand(ctx, "python3", "-m", "py_compile", "-c", code)
	case "javascript", "js", "node":
		return r.lintNode(ctx, code)
	default:
		return "", fmt.Errorf("unsupported language: %s", language)
	}
}

// Test runs a test suite in the given language/directory.
func (r *Runner) Test(ctx context.Context, language, testDir string) (string, error) {
	switch strings.ToLower(language) {
	case "go", "golang":
		return r.runCommandInDir(ctx, testDir, "go", "test", "./...")
	case "python", "python3", "py":
		return r.runCommandInDir(ctx, testDir, "python3", "-m", "pytest")
	case "javascript", "js", "node":
		return r.runCommandInDir(ctx, testDir, "npm", "test")
	default:
		return "", fmt.Errorf("unsupported language: %s", language)
	}
}

func (r *Runner) runGo(ctx context.Context, code string) (string, error) {
	tmpDir, err := os.MkdirTemp("", "coding-go-*")
	if err != nil {
		return "", fmt.Errorf("create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	mainFile := filepath.Join(tmpDir, "main.go")
	if err := os.WriteFile(mainFile, []byte(code), 0600); err != nil {
		return "", fmt.Errorf("write temp file: %w", err)
	}

	return r.runCommandInDir(ctx, tmpDir, "go", "run", "main.go")
}

func (r *Runner) lintGo(ctx context.Context, code string) (string, error) {
	tmpDir, err := os.MkdirTemp("", "coding-lint-*")
	if err != nil {
		return "", fmt.Errorf("create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	mainFile := filepath.Join(tmpDir, "main.go")
	if err := os.WriteFile(mainFile, []byte(code), 0600); err != nil {
		return "", fmt.Errorf("write temp file: %w", err)
	}

	return r.runCommandInDir(ctx, tmpDir, "go", "vet", "./...")
}

func (r *Runner) lintNode(ctx context.Context, code string) (string, error) {
	tmpDir, err := os.MkdirTemp("", "coding-lint-*")
	if err != nil {
		return "", fmt.Errorf("create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	jsFile := filepath.Join(tmpDir, "check.js")
	if err := os.WriteFile(jsFile, []byte(code), 0600); err != nil {
		return "", fmt.Errorf("write temp file: %w", err)
	}

	return r.runCommand(ctx, "node", "--check", jsFile)
}

func (r *Runner) runCommand(ctx context.Context, name string, args ...string) (string, error) {
	return r.runCommandInDir(ctx, r.workDir, name, args...)
}

func (r *Runner) runCommandInDir(ctx context.Context, dir, name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()

	output := stdout.String()
	if stderr.Len() > 0 {
		if output != "" {
			output += "\n"
		}
		output += stderr.String()
	}

	// Truncate if needed.
	if len(output) > r.maxOutput {
		output = output[:r.maxOutput] + "\n... (truncated)"
	}

	if err != nil {
		return output, fmt.Errorf("execution failed: %w\n%s", err, output)
	}
	return output, nil
}

// RegisterCodingTools registers code.run, code.lint, and code.test tools.
func RegisterCodingTools(registry toolRegistry, runner *Runner) {
	registry.Register(
		"code.run",
		"Execute a code snippet in Go, Python, or JavaScript",
		`{"type":"object","properties":{"language":{"type":"string","enum":["go","python","javascript"]},"code":{"type":"string"}},"required":["language","code"]}`,
		"code.execute",
		func(ctx context.Context, input string) (string, error) {
			var params struct {
				Language string `json:"language"`
				Code     string `json:"code"`
			}
			if err := json.Unmarshal([]byte(input), &params); err != nil {
				return "", fmt.Errorf("invalid input: %w", err)
			}
			return runner.Run(ctx, params.Language, params.Code)
		},
	)

	registry.Register(
		"code.lint",
		"Run a linter on code in Go, Python, or JavaScript",
		`{"type":"object","properties":{"language":{"type":"string","enum":["go","python","javascript"]},"code":{"type":"string"}},"required":["language","code"]}`,
		"code.lint",
		func(ctx context.Context, input string) (string, error) {
			var params struct {
				Language string `json:"language"`
				Code     string `json:"code"`
			}
			if err := json.Unmarshal([]byte(input), &params); err != nil {
				return "", fmt.Errorf("invalid input: %w", err)
			}
			return runner.Lint(ctx, params.Language, params.Code)
		},
	)

	registry.Register(
		"code.test",
		"Run tests in a directory for Go, Python, or JavaScript",
		`{"type":"object","properties":{"language":{"type":"string","enum":["go","python","javascript"]},"test_dir":{"type":"string"}},"required":["language","test_dir"]}`,
		"code.test",
		func(ctx context.Context, input string) (string, error) {
			var params struct {
				Language string `json:"language"`
				TestDir  string `json:"test_dir"`
			}
			if err := json.Unmarshal([]byte(input), &params); err != nil {
				return "", fmt.Errorf("invalid input: %w", err)
			}
			return runner.Test(ctx, params.Language, params.TestDir)
		},
	)
}
