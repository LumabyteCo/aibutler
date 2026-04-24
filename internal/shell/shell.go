package shell

import (
	"bytes"
	"context"
	"fmt"
	"time"

	"github.com/LumabyteCo/aibutler/internal/capability"
	"github.com/LumabyteCo/aibutler/internal/config"
	"mvdan.cc/sh/v3/interp"
)

// ExecResult holds the output of a shell command execution.
type ExecResult struct {
	ExitCode int
	Stdout   string
	Stderr   string
}

// Executor runs shell commands in a sandboxed POSIX emulator.
type Executor struct {
	cfg             config.ShellConfig
	auditor         capability.Auditor
	approvalHandler ApprovalHandler
	maxOutputBytes  int
	defaultTimeout  time.Duration
}

// NewExecutor creates a shell executor.
func NewExecutor(cfg config.ShellConfig, auditor capability.Auditor) *Executor {
	return &Executor{
		cfg:            cfg,
		auditor:        auditor,
		maxOutputBytes: 512 * 1024,   // 512 KB
		defaultTimeout: 30 * time.Second,
	}
}

// SetApprovalHandler sets the interactive approval callback.
func (e *Executor) SetApprovalHandler(h ApprovalHandler) {
	e.approvalHandler = h
}

// SetMaxOutputBytes configures the output size limit.
func (e *Executor) SetMaxOutputBytes(n int) {
	e.maxOutputBytes = n
}

// SetDefaultTimeout configures the default command timeout.
func (e *Executor) SetDefaultTimeout(d time.Duration) {
	e.defaultTimeout = d
}

// Exec parses, validates, and executes a command string.
func (e *Executor) Exec(ctx context.Context, command string) (*ExecResult, error) {
	// 1. Parse.
	prog, err := Parse(command)
	if err != nil {
		return nil, fmt.Errorf("shell.exec: %w", err)
	}

	// 2. Validate AST.
	if err := Validate(prog); err != nil {
		e.audit(ctx, command, "denied", err.Error())
		return nil, fmt.Errorf("shell.exec: validation: %w", err)
	}

	// 3. Extract command name.
	cmdName := ExtractCommandName(prog)

	// 4. Allowlist check.
	allowed := MatchAllowlist(e.cfg.Allowed, cmdName)
	if !allowed {
		if e.approvalHandler != nil {
			approved, approvalErr := e.approvalHandler.RequestApproval(ctx, command, cmdName)
			if approvalErr != nil || !approved {
				e.audit(ctx, command, "denied", "not in allowlist, approval denied")
				return nil, fmt.Errorf("shell.exec: command %q not allowed", cmdName)
			}
		} else {
			e.audit(ctx, command, "denied", "not in allowlist")
			return nil, fmt.Errorf("shell.exec: command %q not in allowlist", cmdName)
		}
	}

	// 5. Execute via mvdan.cc/sh interpreter.
	execCtx, cancel := context.WithTimeout(ctx, e.defaultTimeout)
	defer cancel()

	var stdout, stderr bytes.Buffer
	limitedStdout := &limitWriter{w: &stdout, remaining: e.maxOutputBytes}
	limitedStderr := &limitWriter{w: &stderr, remaining: e.maxOutputBytes}

	runner, err := interp.New(
		interp.StdIO(nil, limitedStdout, limitedStderr),
	)
	if err != nil {
		return nil, fmt.Errorf("shell.exec: create runner: %w", err)
	}

	err = runner.Run(execCtx, prog)
	exitCode := 0
	if err != nil {
		if status, ok := interp.IsExitStatus(err); ok {
			exitCode = int(status)
		} else if execCtx.Err() != nil {
			e.audit(ctx, command, "timeout", "")
			return nil, fmt.Errorf("shell.exec: timeout after %s", e.defaultTimeout)
		} else {
			exitCode = 1
		}
	}

	result := &ExecResult{
		ExitCode: exitCode,
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
	}

	e.audit(ctx, command, "success", "")
	return result, nil
}

func (e *Executor) audit(ctx context.Context, command, status, errMsg string) {
	if e.auditor == nil {
		return
	}
	_ = e.auditor.LogAccess(ctx, capability.AuditEntry{
		Timestamp:      time.Now(),
		ResourceType:   "shell",
		Action:         "exec",
		Target:         command,
		CapabilityUsed: "tool.shell.exec",
		Status:         status,
		Error:          errMsg,
	})
}

// limitWriter enforces an output size limit.
type limitWriter struct {
	w         *bytes.Buffer
	remaining int
}

func (lw *limitWriter) Write(p []byte) (int, error) {
	if lw.remaining <= 0 {
		return len(p), nil // Silently discard excess.
	}
	if len(p) > lw.remaining {
		p = p[:lw.remaining]
	}
	n, err := lw.w.Write(p)
	lw.remaining -= n
	return n, err
}
