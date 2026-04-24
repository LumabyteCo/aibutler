package internal

import (
	"os/exec"
	"testing"
)

func TestCrossCompileCheck(t *testing.T) {
	// Verify that the project compiles for linux/arm64.
	// This is a build-only check, not execution.
	cmd := exec.Command("go", "build", "-o", "/dev/null", ".")
	cmd.Env = append(cmd.Environ(), "CGO_ENABLED=0", "GOOS=linux", "GOARCH=arm64")
	cmd.Dir = projectRoot(t)

	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("cross-compile linux/arm64 failed: %v\n%s", err, output)
	}
}

func projectRoot(t *testing.T) string {
	t.Helper()
	// Walk up from internal/ to project root.
	cmd := exec.Command("go", "list", "-m", "-f", "{{.Dir}}")
	output, err := cmd.Output()
	if err != nil {
		t.Fatalf("could not determine project root: %v", err)
	}
	return string(output[:len(output)-1]) // trim newline
}
