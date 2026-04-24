package git

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

const (
	defaultTimeout   = 30 * time.Second
	defaultMaxOutput = 8192 // 8KB
)

// Client provides git operations for a working directory.
type Client struct {
	workDir   string
	timeout   time.Duration
	maxOutput int
}

// NewClient creates a git client for the given working directory.
func NewClient(workDir string) *Client {
	return &Client{
		workDir:   workDir,
		timeout:   defaultTimeout,
		maxOutput: defaultMaxOutput,
	}
}

// Status returns `git status --short --branch` output.
func (c *Client) Status(ctx context.Context) (string, error) {
	return c.run(ctx, "git", "status", "--short", "--branch")
}

// Diff returns combined staged + unstaged diff output.
func (c *Client) Diff(ctx context.Context) (string, error) {
	staged, err := c.run(ctx, "git", "diff", "--cached")
	if err != nil {
		return "", err
	}
	unstaged, err := c.run(ctx, "git", "diff")
	if err != nil {
		return "", err
	}
	if staged == "" {
		return unstaged, nil
	}
	if unstaged == "" {
		return staged, nil
	}
	return staged + "\n" + unstaged, nil
}

// Commit stages files and commits with the given message.
func (c *Client) Commit(ctx context.Context, message string, files []string) (string, error) {
	if len(files) == 0 {
		return "", fmt.Errorf("git: no files specified for commit")
	}

	// Stage files.
	args := append([]string{"add", "--"}, files...)
	if _, err := c.run(ctx, "git", args...); err != nil {
		return "", fmt.Errorf("git add: %w", err)
	}

	// Commit.
	return c.run(ctx, "git", "commit", "-m", message)
}

// Log returns `git log --oneline -N` output.
func (c *Client) Log(ctx context.Context, count int) (string, error) {
	if count <= 0 {
		count = 10
	}
	return c.run(ctx, "git", "log", "--oneline", fmt.Sprintf("-%d", count))
}

// Branch handles list/create/switch actions.
func (c *Client) Branch(ctx context.Context, action, name string) (string, error) {
	switch action {
	case "list", "":
		return c.run(ctx, "git", "branch", "--list")
	case "create":
		if name == "" {
			return "", fmt.Errorf("git: branch name required for create")
		}
		return c.run(ctx, "git", "branch", name)
	case "switch":
		if name == "" {
			return "", fmt.Errorf("git: branch name required for switch")
		}
		return c.run(ctx, "git", "checkout", name)
	default:
		return "", fmt.Errorf("git: unknown branch action %q", action)
	}
}

// PRCreate creates a pull request using `gh pr create`.
func (c *Client) PRCreate(ctx context.Context, title, body, base string) (string, error) {
	args := []string{"pr", "create", "--title", title, "--body", body}
	if base != "" {
		args = append(args, "--base", base)
	}
	return c.run(ctx, "gh", args...)
}

// GitContext returns a compact string of git status + diff stat for system prompt injection.
func (c *Client) GitContext(ctx context.Context) string {
	status, err := c.run(ctx, "git", "status", "--short", "--branch")
	if err != nil {
		return ""
	}
	diffStat, _ := c.run(ctx, "git", "diff", "--stat")
	if diffStat == "" {
		return status
	}
	return status + "\n" + diffStat
}

// run executes a command in the working directory with timeout and output truncation.
func (c *Client) run(ctx context.Context, name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = c.workDir

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		errMsg := stderr.String()
		if errMsg == "" {
			errMsg = err.Error()
		}
		return "", fmt.Errorf("%s %s: %s", name, strings.Join(args, " "), strings.TrimSpace(errMsg))
	}

	out := stdout.String()
	if len(out) > c.maxOutput {
		out = out[:c.maxOutput] + "\n... (truncated)"
	}
	return strings.TrimSpace(out), nil
}

// toolRegistry is a narrow interface for tool registration (avoids import cycles).
type toolRegistry interface {
	Register(name, desc, schema, cap string, exec func(ctx context.Context, input string) (string, error))
}

// RegisterGitTools registers all git tools in the registry.
func RegisterGitTools(reg toolRegistry, client *Client) {
	reg.Register("git.status", "Show git repository status (branch and changes)",
		`{"type":"object","properties":{}}`,
		"tool.git.read",
		func(ctx context.Context, input string) (string, error) {
			return client.Status(ctx)
		})

	reg.Register("git.diff", "Show git diff (staged and unstaged changes)",
		`{"type":"object","properties":{}}`,
		"tool.git.read",
		func(ctx context.Context, input string) (string, error) {
			return client.Diff(ctx)
		})

	reg.Register("git.commit", "Stage files and create a git commit",
		`{"type":"object","properties":{"message":{"type":"string","description":"Commit message"},"files":{"type":"array","items":{"type":"string"},"description":"Files to stage"}},"required":["message","files"]}`,
		"tool.git.write",
		func(ctx context.Context, input string) (string, error) {
			var params struct {
				Message string   `json:"message"`
				Files   []string `json:"files"`
			}
			if err := json.Unmarshal([]byte(input), &params); err != nil {
				return "", fmt.Errorf("git.commit: %w", err)
			}
			return client.Commit(ctx, params.Message, params.Files)
		})

	reg.Register("git.log", "Show recent git commit history",
		`{"type":"object","properties":{"count":{"type":"integer","description":"Number of commits to show (default: 10)"}}}`,
		"tool.git.read",
		func(ctx context.Context, input string) (string, error) {
			var params struct {
				Count int `json:"count"`
			}
			_ = json.Unmarshal([]byte(input), &params)
			return client.Log(ctx, params.Count)
		})

	reg.Register("git.branch", "List, create, or switch git branches",
		`{"type":"object","properties":{"action":{"type":"string","enum":["list","create","switch"]},"name":{"type":"string","description":"Branch name (for create/switch)"}},"required":["action"]}`,
		"tool.git.read",
		func(ctx context.Context, input string) (string, error) {
			var params struct {
				Action string `json:"action"`
				Name   string `json:"name"`
			}
			if err := json.Unmarshal([]byte(input), &params); err != nil {
				return "", fmt.Errorf("git.branch: %w", err)
			}
			return client.Branch(ctx, params.Action, params.Name)
		})

	reg.Register("git.pr_create", "Create a pull request using GitHub CLI",
		`{"type":"object","properties":{"title":{"type":"string"},"body":{"type":"string"},"base":{"type":"string","description":"Base branch (optional)"}},"required":["title","body"]}`,
		"tool.git.write",
		func(ctx context.Context, input string) (string, error) {
			var params struct {
				Title string `json:"title"`
				Body  string `json:"body"`
				Base  string `json:"base"`
			}
			if err := json.Unmarshal([]byte(input), &params); err != nil {
				return "", fmt.Errorf("git.pr_create: %w", err)
			}
			return client.PRCreate(ctx, params.Title, params.Body, params.Base)
		})
}
