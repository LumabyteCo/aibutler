package cli_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/LumabyteCo/aibutler/internal/cli"
)

// TestDefaultDataDirHonorsEnv guards the v0.2.1 fix (B2): every deploy
// artifact (Dockerfile, all 3 compose files, systemd unit, Helm chart) sets
// AIBUTLER_DATA, but the binary never read it — main.go hardcoded
// ~/.aibutler, so all documented containerized data paths were fiction.
func TestDefaultDataDirHonorsEnv(t *testing.T) {
	t.Setenv("AIBUTLER_DATA", "/data")

	got := cli.DefaultDataDir()
	if got != "/data" {
		t.Errorf("DefaultDataDir() = %q, want /q — AIBUTLER_DATA must be honored", got)
	}
}

// TestDefaultDataDirFallsBackToHome verifies the default behavior is
// unchanged when the env var is absent.
func TestDefaultDataDirFallsBackToHome(t *testing.T) {
	os.Unsetenv("AIBUTLER_DATA")

	home, _ := os.UserHomeDir()
	want := filepath.Join(home, ".aibutler")

	if got := cli.DefaultDataDir(); got != want {
		t.Errorf("DefaultDataDir() = %q, want %q", got, want)
	}
}