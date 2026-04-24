package subprocess

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// Config describes a subprocess bridge configuration.
type Config struct {
	Command      string        `yaml:"command"`
	Args         []string      `yaml:"args"`
	Timeout      time.Duration `yaml:"timeout"`
	WorkDir      string        `yaml:"work_dir"`
	Capabilities []string      `yaml:"capabilities"`
}

// Adapter executes tasks by spawning external processes.
type Adapter struct {
	cfg Config
}

// New creates a subprocess adapter with the given config.
func New(cfg Config) *Adapter {
	if cfg.Timeout <= 0 {
		cfg.Timeout = 300 * time.Second
	}
	return &Adapter{cfg: cfg}
}

// Execute runs the configured command with the task substituted into args.
func (a *Adapter) Execute(ctx context.Context, task string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, a.cfg.Timeout)
	defer cancel()

	// Substitute {task} placeholder in args.
	args := make([]string, len(a.cfg.Args))
	for i, arg := range a.cfg.Args {
		args[i] = strings.ReplaceAll(arg, "{task}", task)
	}

	cmd := exec.CommandContext(ctx, a.cfg.Command, args...)
	if a.cfg.WorkDir != "" {
		cmd.Dir = a.cfg.WorkDir
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return "", fmt.Errorf("subprocess: timeout after %s", a.cfg.Timeout)
		}
		return "", fmt.Errorf("subprocess: %s: %w\nstderr: %s", a.cfg.Command, err, stderr.String())
	}

	return stdout.String(), nil
}

// Available checks whether the configured command exists on the system PATH.
func (a *Adapter) Available() bool {
	_, err := exec.LookPath(a.cfg.Command)
	return err == nil
}
