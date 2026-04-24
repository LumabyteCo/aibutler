package sandbox

import (
	"fmt"
	"os"
	"runtime"
	"strings"
)

// Mode defines the sandbox restriction level.
type Mode string

const (
	// ModeOff disables sandboxing.
	ModeOff Mode = "off"
	// ModeWorkspaceOnly restricts writes to the workspace directory.
	ModeWorkspaceOnly Mode = "workspace-only"
	// ModeAllowList restricts writes to allowed paths only.
	ModeAllowList Mode = "allow-list"
)

// Sandbox wraps shell commands with OS-level isolation.
type Sandbox struct {
	mode       Mode
	workDir    string
	allowPaths []string
}

// New creates a sandbox with the given mode, workspace directory, and allowed paths.
func New(mode Mode, workDir string, allowPaths []string) *Sandbox {
	return &Sandbox{
		mode:       mode,
		workDir:    workDir,
		allowPaths: allowPaths,
	}
}

// WrapCommand returns the sandboxed command and args for the given command string.
// When sandboxing is off or inside a container, returns the original command unwrapped.
func (s *Sandbox) WrapCommand(command string) (string, []string, error) {
	if s.mode == ModeOff {
		return "sh", []string{"-lc", command}, nil
	}

	// Auto-skip if inside a container.
	if s.DetectContainer() {
		return "sh", []string{"-lc", command}, nil
	}

	switch runtime.GOOS {
	case "linux":
		return s.wrapLinux(command)
	case "darwin":
		return s.wrapDarwin(command)
	default:
		// Unsupported OS: run without sandbox.
		return "sh", []string{"-lc", command}, nil
	}
}

// wrapLinux uses unshare for Linux namespace isolation.
func (s *Sandbox) wrapLinux(command string) (string, []string, error) {
	return "unshare", []string{
		"--user", "--map-root-user",
		"--mount", "--ipc", "--pid", "--uts",
		"--fork",
		"sh", "-lc", command,
	}, nil
}

// wrapDarwin uses sandbox-exec for macOS sandboxing.
func (s *Sandbox) wrapDarwin(command string) (string, []string, error) {
	profile, err := s.buildDarwinProfile()
	if err != nil {
		return "", nil, err
	}
	return "sandbox-exec", []string{"-p", profile, "sh", "-lc", command}, nil
}

// buildDarwinProfile creates a macOS sandbox profile string.
func (s *Sandbox) buildDarwinProfile() (string, error) {
	var sb strings.Builder
	sb.WriteString("(version 1)\n")
	sb.WriteString("(allow default)\n")
	sb.WriteString("(deny file-write*)\n")

	switch s.mode {
	case ModeWorkspaceOnly:
		if s.workDir == "" {
			return "", fmt.Errorf("sandbox: workspace-only mode requires workDir")
		}
		sb.WriteString(fmt.Sprintf("(allow file-write* (subpath %q))\n", s.workDir))
	case ModeAllowList:
		for _, p := range s.allowPaths {
			sb.WriteString(fmt.Sprintf("(allow file-write* (subpath %q))\n", p))
		}
	}

	// Always allow writes to /tmp and /dev.
	sb.WriteString("(allow file-write* (subpath \"/tmp\"))\n")
	sb.WriteString("(allow file-write* (subpath \"/dev\"))\n")

	return sb.String(), nil
}

// DetectContainer returns true if running inside a container.
func (s *Sandbox) DetectContainer() bool {
	// Check common container indicators.
	for _, path := range []string{"/.dockerenv", "/run/.containerenv"} {
		if _, err := os.Stat(path); err == nil {
			return true
		}
	}

	// Check environment variables.
	for _, env := range []string{"KUBERNETES_SERVICE_HOST", "container", "DOCKER_CONTAINER"} {
		if os.Getenv(env) != "" {
			return true
		}
	}

	return false
}
