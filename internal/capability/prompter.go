package capability

import (
	"bufio"
	"fmt"
	"io"
	"strings"
)

// PermissionMode defines the security posture for tool execution.
type PermissionMode int

const (
	ModeReadOnly       PermissionMode = iota
	ModeWorkspaceWrite
	ModeFullAccess
)

// ParsePermissionMode converts a string to PermissionMode.
func ParsePermissionMode(s string) PermissionMode {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "read-only", "readonly":
		return ModeReadOnly
	case "workspace-write", "workspacewrite":
		return ModeWorkspaceWrite
	case "full-access", "fullaccess":
		return ModeFullAccess
	default:
		return ModeWorkspaceWrite // safe default
	}
}

// PermissionRule describes an allow/deny rule for a tool pattern.
type PermissionRule struct {
	ToolPattern string `yaml:"pattern"` // e.g., "bash(git:*)" — tool name with optional input prefix
	Action      string `yaml:"action"`  // "allow" or "deny"
}

// InteractivePrompter checks tool permissions against rules and prompts the user
// when no rule matches.
type InteractivePrompter struct {
	reader io.Reader
	writer io.Writer
	rules  []PermissionRule
	mode   PermissionMode
}

// NewInteractivePrompter creates a prompter with the given rules and mode.
func NewInteractivePrompter(r io.Reader, w io.Writer, rules []PermissionRule) *InteractivePrompter {
	return &InteractivePrompter{
		reader: r,
		writer: w,
		rules:  rules,
		mode:   ModeWorkspaceWrite,
	}
}

// SetMode sets the permission mode.
func (p *InteractivePrompter) SetMode(mode PermissionMode) {
	p.mode = mode
}

// ShouldAllow checks if a tool call should be allowed based on rules and mode.
// Returns true if allowed, false if denied.
func (p *InteractivePrompter) ShouldAllow(toolName, toolInput string) (bool, error) {
	// Mode-based filtering first.
	switch p.mode {
	case ModeReadOnly:
		if isWriteTool(toolName) {
			return false, nil
		}
	case ModeWorkspaceWrite:
		// Allow reads and workspace writes, deny system-level writes.
		if isSystemWriteTool(toolName) {
			return false, nil
		}
	case ModeFullAccess:
		// All tools allowed by mode (still subject to rules).
	}

	// Check explicit rules.
	for _, rule := range p.rules {
		if matchesRule(rule.ToolPattern, toolName, toolInput) {
			return rule.Action == "allow", nil
		}
	}

	// No rule matched — prompt the user interactively (only for potential write operations).
	if p.reader != nil && p.writer != nil && isWriteTool(toolName) {
		return p.promptUser(toolName, toolInput)
	}

	// Default: allow — mode-based filtering already blocked disallowed writes above.
	return true, nil
}

// promptUser asks the user for permission interactively.
func (p *InteractivePrompter) promptUser(toolName, toolInput string) (bool, error) {
	preview := toolInput
	if len(preview) > 100 {
		preview = preview[:100] + "..."
	}
	fmt.Fprintf(p.writer, "Allow tool %q with input: %s? [y/N] ", toolName, preview)

	scanner := bufio.NewScanner(p.reader)
	if scanner.Scan() {
		answer := strings.TrimSpace(strings.ToLower(scanner.Text()))
		return answer == "y" || answer == "yes", nil
	}
	return false, scanner.Err()
}

// matchesRule checks if a tool call matches a permission rule pattern.
// Pattern format: "toolName" or "toolName(inputPrefix:*)"
func matchesRule(pattern, toolName, toolInput string) bool {
	// Check for input-prefix pattern: "bash(git:*)"
	if idx := strings.Index(pattern, "("); idx >= 0 {
		toolPart := pattern[:idx]
		if toolPart != toolName {
			return false
		}
		inputPart := strings.TrimSuffix(strings.TrimPrefix(pattern[idx:], "("), ")")
		inputPart = strings.TrimSuffix(inputPart, "*")
		inputPart = strings.TrimSuffix(inputPart, ":")
		return strings.Contains(toolInput, inputPart)
	}

	// Simple exact match or wildcard.
	if pattern == "*" {
		return true
	}
	if pattern == toolName {
		return true
	}
	// Prefix wildcard: "shell.*"
	if strings.HasSuffix(pattern, "*") {
		prefix := strings.TrimSuffix(pattern, "*")
		return strings.HasPrefix(toolName, prefix)
	}
	return false
}

// isWriteTool returns true for tools that modify state.
func isWriteTool(name string) bool {
	writePrefixes := []string{
		"task.add", "task.complete", "task.remove", "task.clear", "task.prioritize",
		"expense.log",
		"contact.add", "contact.update",
		"journal.write",
		"health.log",
		"reminder.set", "reminder.cancel",
		"habit.create", "habit.log",
		"place.save", "place.update", "place.delete",
		"memory.capture", "memory.forget",
		"instruction.save", "instruction.update", "instruction.remove",
		"shell.exec",
		"git.commit", "git.branch", "git.pr_create",
		"file.write",
	}
	for _, p := range writePrefixes {
		if name == p || strings.HasPrefix(name, p) {
			return true
		}
	}
	return false
}

// isSystemWriteTool returns true for tools that can modify system-level state.
func isSystemWriteTool(name string) bool {
	systemTools := []string{
		"shell.exec",
	}
	for _, t := range systemTools {
		if name == t {
			return true
		}
	}
	return false
}
