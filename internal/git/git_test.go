package git

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func initTestRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	// Check if git is available.
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	cmds := [][]string{
		{"git", "init"},
		{"git", "config", "user.email", "test@test.com"},
		{"git", "config", "user.name", "Test"},
	}
	for _, args := range cmds {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("setup %v: %s: %v", args, out, err)
		}
	}

	// Create initial commit.
	fp := filepath.Join(dir, "README.md")
	if err := os.WriteFile(fp, []byte("# Test\n"), 0644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"git", "add", "."},
		{"git", "commit", "-m", "initial"},
	} {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("setup %v: %s: %v", args, out, err)
		}
	}

	return dir
}

func TestStatus(t *testing.T) {
	dir := initTestRepo(t)
	c := NewClient(dir)

	status, err := c.Status(context.Background())
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if !strings.Contains(status, "##") {
		t.Fatalf("expected branch info in status, got: %s", status)
	}
}

func TestDiff(t *testing.T) {
	dir := initTestRepo(t)

	// Modify a file to create a diff.
	fp := filepath.Join(dir, "README.md")
	if err := os.WriteFile(fp, []byte("# Modified\n"), 0644); err != nil {
		t.Fatal(err)
	}

	c := NewClient(dir)
	diff, err := c.Diff(context.Background())
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	if !strings.Contains(diff, "Modified") {
		t.Fatalf("expected diff to contain 'Modified', got: %s", diff)
	}
}

func TestCommit(t *testing.T) {
	dir := initTestRepo(t)

	// Create a new file.
	fp := filepath.Join(dir, "new.txt")
	if err := os.WriteFile(fp, []byte("new content\n"), 0644); err != nil {
		t.Fatal(err)
	}

	c := NewClient(dir)
	out, err := c.Commit(context.Background(), "add new file", []string{"new.txt"})
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}
	if !strings.Contains(out, "add new file") {
		t.Fatalf("expected commit message in output, got: %s", out)
	}
}

func TestLog(t *testing.T) {
	dir := initTestRepo(t)
	c := NewClient(dir)

	log, err := c.Log(context.Background(), 5)
	if err != nil {
		t.Fatalf("Log: %v", err)
	}
	if !strings.Contains(log, "initial") {
		t.Fatalf("expected 'initial' in log, got: %s", log)
	}
}

func TestToolRegistration(t *testing.T) {
	dir := initTestRepo(t)
	c := NewClient(dir)

	registered := make(map[string]bool)
	reg := &testToolRegistry{tools: registered}
	RegisterGitTools(reg, c)

	expected := []string{"git.status", "git.diff", "git.commit", "git.log", "git.branch", "git.pr_create"}
	for _, name := range expected {
		if !registered[name] {
			t.Errorf("expected tool %q to be registered", name)
		}
	}
}

// testToolRegistry implements toolRegistry for testing.
type testToolRegistry struct {
	tools map[string]bool
}

func (r *testToolRegistry) Register(name, desc, schema, cap string, exec func(ctx context.Context, input string) (string, error)) {
	r.tools[name] = true
}
