package sandbox

import (
	"runtime"
	"strings"
	"testing"
)

func TestModeOff(t *testing.T) {
	s := New(ModeOff, "/workspace", nil)
	cmd, args, err := s.WrapCommand("ls -la")
	if err != nil {
		t.Fatalf("WrapCommand: unexpected error: %v", err)
	}
	if cmd != "sh" {
		t.Errorf("cmd = %q, want %q", cmd, "sh")
	}
	if len(args) != 2 || args[0] != "-lc" || args[1] != "ls -la" {
		t.Errorf("args = %v, want [-lc, ls -la]", args)
	}
}

func TestWorkspaceOnly(t *testing.T) {
	s := New(ModeWorkspaceOnly, "/workspace", nil)

	// Skip if we detect container (the sandbox auto-skips).
	if s.DetectContainer() {
		t.Skip("inside container, sandbox auto-skips")
	}

	cmd, args, err := s.WrapCommand("echo test")
	if err != nil {
		t.Fatalf("WrapCommand: unexpected error: %v", err)
	}

	switch runtime.GOOS {
	case "darwin":
		if cmd != "sandbox-exec" {
			t.Errorf("cmd = %q, want %q", cmd, "sandbox-exec")
		}
		// Check that profile contains workspace path.
		profile := args[1]
		if !strings.Contains(profile, "/workspace") {
			t.Error("profile should contain workspace path")
		}
		if !strings.Contains(profile, "deny file-write*") {
			t.Error("profile should deny writes by default")
		}
	case "linux":
		if cmd != "unshare" {
			t.Errorf("cmd = %q, want %q", cmd, "unshare")
		}
	default:
		if cmd != "sh" {
			t.Errorf("cmd = %q, want %q for unsupported OS", cmd, "sh")
		}
	}
}

func TestContainerDetection(t *testing.T) {
	s := New(ModeOff, "", nil)
	// This is a best-effort test. Just verify it returns without panicking.
	_ = s.DetectContainer()
}

func TestAllowList(t *testing.T) {
	s := New(ModeAllowList, "", []string{"/data", "/output"})

	if s.DetectContainer() {
		t.Skip("inside container, sandbox auto-skips")
	}

	cmd, args, err := s.WrapCommand("touch /data/file.txt")
	if err != nil {
		t.Fatalf("WrapCommand: unexpected error: %v", err)
	}

	switch runtime.GOOS {
	case "darwin":
		if cmd != "sandbox-exec" {
			t.Errorf("cmd = %q, want %q", cmd, "sandbox-exec")
		}
		profile := args[1]
		if !strings.Contains(profile, "/data") {
			t.Error("profile should contain /data allow path")
		}
		if !strings.Contains(profile, "/output") {
			t.Error("profile should contain /output allow path")
		}
	case "linux":
		if cmd != "unshare" {
			t.Errorf("cmd = %q, want %q", cmd, "unshare")
		}
	}
}
