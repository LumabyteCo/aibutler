package hook

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// helper to write a temporary script that returns the given exit code and stdout.
func writeScript(t *testing.T, dir, name string, exitCode int, stdout string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	content := "#!/bin/sh\n"
	if stdout != "" {
		content += "echo '" + stdout + "'\n"
	}
	content += "exit " + itoa(exitCode) + "\n"
	if err := os.WriteFile(path, []byte(content), 0755); err != nil {
		t.Fatal(err)
	}
	return path
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	s := ""
	neg := false
	if n < 0 {
		neg = true
		n = -n
	}
	for n > 0 {
		s = string(rune('0'+n%10)) + s
		n /= 10
	}
	if neg {
		s = "-" + s
	}
	return s
}

func TestPreHookAllow(t *testing.T) {
	dir := t.TempDir()
	script := writeScript(t, dir, "allow.sh", 0, "all good")

	e := New([]HookConfig{{Command: script}}, nil)
	result, err := e.RunPreToolUse(context.Background(), "bash", `{"command":"ls"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Denied {
		t.Fatal("expected allow, got deny")
	}
	if len(result.Messages) != 1 || result.Messages[0] != "all good" {
		t.Fatalf("expected message 'all good', got %v", result.Messages)
	}
}

func TestPreHookDenyExit2(t *testing.T) {
	dir := t.TempDir()
	script := writeScript(t, dir, "deny.sh", 2, "not allowed")

	e := New([]HookConfig{{Command: script}}, nil)
	result, err := e.RunPreToolUse(context.Background(), "bash", `{"command":"rm -rf /"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Denied {
		t.Fatal("expected deny, got allow")
	}
	if len(result.Messages) != 1 || result.Messages[0] != "not allowed" {
		t.Fatalf("expected message 'not allowed', got %v", result.Messages)
	}
}

func TestPostHookFeedback(t *testing.T) {
	dir := t.TempDir()
	script := writeScript(t, dir, "audit.sh", 0, "logged to audit")

	e := New(nil, []HookConfig{{Command: script}})
	result, err := e.RunPostToolUse(context.Background(), "bash", `{"command":"ls"}`, "file1.txt", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Denied {
		t.Fatal("post-hook should not deny")
	}
	if len(result.Messages) != 1 || result.Messages[0] != "logged to audit" {
		t.Fatalf("expected message 'logged to audit', got %v", result.Messages)
	}
}

func TestToolFilterMatch(t *testing.T) {
	dir := t.TempDir()
	script := writeScript(t, dir, "filter.sh", 0, "matched")

	e := New([]HookConfig{{Command: script, Tools: []string{"shell.*"}}}, nil)
	result, err := e.RunPreToolUse(context.Background(), "shell.exec", `{}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Messages) != 1 || result.Messages[0] != "matched" {
		t.Fatalf("expected match, got %v", result.Messages)
	}
}

func TestToolFilterMiss(t *testing.T) {
	dir := t.TempDir()
	script := writeScript(t, dir, "filter.sh", 0, "should not see this")

	e := New([]HookConfig{{Command: script, Tools: []string{"shell.*"}}}, nil)
	result, err := e.RunPreToolUse(context.Background(), "memory.search", `{}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Messages) != 0 {
		t.Fatalf("expected no messages (filter miss), got %v", result.Messages)
	}
}

func TestAgentLifecycleHook(t *testing.T) {
	dir := t.TempDir()
	script := writeScript(t, dir, "lifecycle.sh", 0, "agent spawned")

	e := New(nil, nil)
	e.SetAgentHooks(OnAgentSpawn, []HookConfig{{Command: script}})
	result, err := e.OnAgentSpawnHook(context.Background(), "test-agent")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Denied {
		t.Fatal("lifecycle hook should not deny")
	}
	if len(result.Messages) != 1 || result.Messages[0] != "agent spawned" {
		t.Fatalf("expected 'agent spawned', got %v", result.Messages)
	}
}
