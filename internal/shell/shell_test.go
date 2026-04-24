package shell_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/LumabyteCo/aibutler/internal/config"
	"github.com/LumabyteCo/aibutler/internal/shell"
	"github.com/LumabyteCo/aibutler/testutil"
)

// --- Parser Tests ---

func TestParseSimpleCommand(t *testing.T) {
	prog, err := shell.Parse("echo hello")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if prog == nil {
		t.Fatal("expected non-nil program")
	}
}

func TestParseInvalidSyntax(t *testing.T) {
	_, err := shell.Parse("echo '")
	if err == nil {
		t.Fatal("expected parse error for invalid syntax")
	}
}

// --- Validation Tests ---

func TestValidateSimpleCommand(t *testing.T) {
	prog, _ := shell.Parse("echo hello")
	if err := shell.Validate(prog); err != nil {
		t.Fatalf("validate: %v", err)
	}
}

func TestValidateRejectSemicolon(t *testing.T) {
	prog, _ := shell.Parse("echo a; echo b")
	if err := shell.Validate(prog); err == nil {
		t.Fatal("expected validation error for semicolon")
	}
}

func TestValidateRejectAnd(t *testing.T) {
	prog, _ := shell.Parse("cmd1 && cmd2")
	if err := shell.Validate(prog); err == nil {
		t.Fatal("expected validation error for &&")
	}
}

func TestValidateRejectOr(t *testing.T) {
	prog, _ := shell.Parse("cmd1 || cmd2")
	if err := shell.Validate(prog); err == nil {
		t.Fatal("expected validation error for ||")
	}
}

func TestValidateRejectPipe(t *testing.T) {
	prog, _ := shell.Parse("cmd1 | cmd2")
	if err := shell.Validate(prog); err == nil {
		t.Fatal("expected validation error for pipe")
	}
}

func TestValidateRejectCmdSubst(t *testing.T) {
	prog, _ := shell.Parse("echo $(whoami)")
	if err := shell.Validate(prog); err == nil {
		t.Fatal("expected validation error for command substitution")
	}
}

func TestValidateRejectBacktick(t *testing.T) {
	prog, _ := shell.Parse("echo `whoami`")
	if err := shell.Validate(prog); err == nil {
		t.Fatal("expected validation error for backtick substitution")
	}
}

func TestValidateRejectGlobStar(t *testing.T) {
	prog, _ := shell.Parse("ls *.go")
	if err := shell.Validate(prog); err == nil {
		t.Fatal("expected validation error for glob *")
	}
}

func TestValidateRejectGlobQuestion(t *testing.T) {
	prog, _ := shell.Parse("ls file?.txt")
	if err := shell.Validate(prog); err == nil {
		t.Fatal("expected validation error for glob ?")
	}
}

func TestValidateRejectEnvVar(t *testing.T) {
	prog, _ := shell.Parse("echo $HOME")
	if err := shell.Validate(prog); err == nil {
		t.Fatal("expected validation error for env var")
	}
}

func TestValidateRejectBackground(t *testing.T) {
	prog, _ := shell.Parse("cmd &")
	if err := shell.Validate(prog); err == nil {
		t.Fatal("expected validation error for background &")
	}
}

// --- Allowlist Tests ---

func TestAllowlistMatch(t *testing.T) {
	if !shell.MatchAllowlist([]string{"git", "npm"}, "git") {
		t.Error("expected git to be allowed")
	}
}

func TestAllowlistNoMatch(t *testing.T) {
	if shell.MatchAllowlist([]string{"git", "npm"}, "rm") {
		t.Error("expected rm to be denied")
	}
}

func TestAllowlistWildcard(t *testing.T) {
	if !shell.MatchAllowlist([]string{"go*"}, "go") {
		t.Error("expected go to match go*")
	}
	if !shell.MatchAllowlist([]string{"go*"}, "gofmt") {
		t.Error("expected gofmt to match go*")
	}
	if shell.MatchAllowlist([]string{"go*"}, "npm") {
		t.Error("expected npm to NOT match go*")
	}
}

func TestAllowlistEmpty(t *testing.T) {
	if shell.MatchAllowlist(nil, "echo") {
		t.Error("expected empty allowlist to deny everything")
	}
}

// --- ExtractCommandName Tests ---

func TestExtractCommandName(t *testing.T) {
	prog, _ := shell.Parse("git status")
	name := shell.ExtractCommandName(prog)
	if name != "git" {
		t.Errorf("name = %q, want git", name)
	}
}

func TestExtractCommandNameQuoted(t *testing.T) {
	prog, _ := shell.Parse("'echo' hello")
	name := shell.ExtractCommandName(prog)
	if name != "echo" {
		t.Errorf("name = %q, want echo", name)
	}
}

// --- Executor Tests ---

func TestExecAllowed(t *testing.T) {
	executor := shell.NewExecutor(config.ShellConfig{
		Mode:    "allowlist",
		Allowed: []string{"echo"},
	}, nil)

	result, err := executor.Exec(context.Background(), "echo hello")
	if err != nil {
		t.Fatalf("exec: %v", err)
	}
	if !strings.Contains(result.Stdout, "hello") {
		t.Errorf("stdout = %q, want contains 'hello'", result.Stdout)
	}
	if result.ExitCode != 0 {
		t.Errorf("exit code = %d, want 0", result.ExitCode)
	}
}

func TestExecDenied(t *testing.T) {
	executor := shell.NewExecutor(config.ShellConfig{
		Mode:    "allowlist",
		Allowed: []string{"echo"},
	}, nil)

	_, err := executor.Exec(context.Background(), "rm -rf /")
	if err == nil {
		t.Fatal("expected error for denied command")
	}
	if !strings.Contains(err.Error(), "not in allowlist") {
		t.Errorf("error = %q, want contains 'not in allowlist'", err.Error())
	}
}

func TestExecTimeout(t *testing.T) {
	executor := shell.NewExecutor(config.ShellConfig{
		Mode:    "allowlist",
		Allowed: []string{"sleep"},
	}, nil)
	executor.SetDefaultTimeout(100 * time.Millisecond)

	_, err := executor.Exec(context.Background(), "sleep 10")
	if err == nil {
		t.Fatal("expected timeout error")
	}
	if !strings.Contains(err.Error(), "timeout") {
		t.Errorf("error = %q, want contains 'timeout'", err.Error())
	}
}

func TestExecOutputLimit(t *testing.T) {
	executor := shell.NewExecutor(config.ShellConfig{
		Mode:    "allowlist",
		Allowed: []string{"printf"},
	}, nil)
	executor.SetMaxOutputBytes(10)

	result, err := executor.Exec(context.Background(), "printf 'aaaaaaaaaaaaaaaaaaaaaaaaa'")
	if err != nil {
		t.Fatalf("exec: %v", err)
	}
	if len(result.Stdout) > 10 {
		t.Errorf("stdout length = %d, want <= 10", len(result.Stdout))
	}
}

func TestApprovalHandlerInvoked(t *testing.T) {
	invoked := false
	executor := shell.NewExecutor(config.ShellConfig{
		Mode:    "allowlist",
		Allowed: []string{}, // Empty — nothing allowed.
	}, nil)
	executor.SetApprovalHandler(shell.ApprovalFunc(func(_ context.Context, _, _ string) (bool, error) {
		invoked = true
		return false, nil
	}))

	executor.Exec(context.Background(), "echo hello")
	if !invoked {
		t.Error("approval handler was not invoked")
	}
}

func TestApprovalApproved(t *testing.T) {
	executor := shell.NewExecutor(config.ShellConfig{
		Mode:    "allowlist",
		Allowed: []string{},
	}, nil)
	executor.SetApprovalHandler(shell.AlwaysApprove())

	result, err := executor.Exec(context.Background(), "echo approved")
	if err != nil {
		t.Fatalf("exec: %v", err)
	}
	if !strings.Contains(result.Stdout, "approved") {
		t.Errorf("stdout = %q", result.Stdout)
	}
}

func TestApprovalDenied(t *testing.T) {
	executor := shell.NewExecutor(config.ShellConfig{
		Mode:    "allowlist",
		Allowed: []string{},
	}, nil)
	executor.SetApprovalHandler(shell.AlwaysDeny())

	_, err := executor.Exec(context.Background(), "echo hello")
	if err == nil {
		t.Fatal("expected error for denied approval")
	}
}

func TestExecAuditTrail(t *testing.T) {
	auditor := testutil.NewFakeAuditor()
	executor := shell.NewExecutor(config.ShellConfig{
		Mode:    "allowlist",
		Allowed: []string{"echo"},
	}, auditor)

	executor.Exec(context.Background(), "echo audit")

	entries := auditor.Entries()
	found := false
	for _, e := range entries {
		if e.Status == "success" && e.Target == "echo audit" {
			found = true
		}
	}
	if !found {
		t.Error("expected success audit entry for echo")
	}
}

func TestExecDeniedAuditTrail(t *testing.T) {
	auditor := testutil.NewFakeAuditor()
	executor := shell.NewExecutor(config.ShellConfig{
		Mode:    "allowlist",
		Allowed: []string{"echo"},
	}, auditor)

	executor.Exec(context.Background(), "rm -rf /")

	entries := auditor.Entries()
	found := false
	for _, e := range entries {
		if e.Status == "denied" {
			found = true
		}
	}
	if !found {
		t.Error("expected denied audit entry")
	}
}

func TestExecFalseCommand(t *testing.T) {
	executor := shell.NewExecutor(config.ShellConfig{
		Mode:    "allowlist",
		Allowed: []string{"false"},
	}, nil)

	result, err := executor.Exec(context.Background(), "false")
	if err != nil {
		t.Fatalf("exec: %v", err)
	}
	if result.ExitCode != 1 {
		t.Errorf("exit code = %d, want 1", result.ExitCode)
	}
}
