package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LumabyteCo/aibutler/internal/config"
)

// TestUpdateAgentModePreservesFile guards the mode command's persistence:
// changing one setting must not rewrite the whole file. Users keep their
// comments, their ordering, and any keys this binary doesn't know about.
func TestUpdateAgentModePreservesFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	original := `# my hand-written config
settings:
    # I like auto
    agent_mode: auto  # inline note
    language: en
    model: claude-sonnet-4-6

# legacy key from an older build — must survive
custom_key: keep-me
`
	if err := os.WriteFile(path, []byte(original), 0600); err != nil {
		t.Fatal(err)
	}

	if err := config.UpdateAgentMode(path, "multi"); err != nil {
		t.Fatalf("UpdateAgentMode: %v", err)
	}

	out, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	got := string(out)
	for _, want := range []string{
		"# my hand-written config",
		"# I like auto",
		"custom_key: keep-me",
		"agent_mode: multi",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("updated file lost %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "agent_mode: auto") {
		t.Errorf("old mode still present:\n%s", got)
	}

	// And the result must load back as a valid config with the new mode.
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Settings.AgentMode != "multi" {
		t.Errorf("loaded mode = %q, want multi", cfg.Settings.AgentMode)
	}
}

// TestUpdateAgentModeMissingFile creates a minimal config when none exists.
func TestUpdateAgentModeMissingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := config.UpdateAgentMode(path, "single"); err != nil {
		t.Fatalf("UpdateAgentMode: %v", err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Settings.AgentMode != "single" {
		t.Errorf("mode = %q, want single", cfg.Settings.AgentMode)
	}
}

// TestUpdateAgentModeAddsSettings covers a config that has no settings
// block at all (very old or hand-minimal files).
func TestUpdateAgentModeAddsSettings(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte("custom_key: value\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := config.UpdateAgentMode(path, "mission"); err != nil {
		t.Fatalf("UpdateAgentMode: %v", err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Settings.AgentMode != "mission" {
		t.Errorf("mode = %q, want mission", cfg.Settings.AgentMode)
	}
}

// TestUpdateAgentModeRejectsBrokenYAML refuses to clobber a file it can't
// parse — silently replacing it would destroy the user's config.
func TestUpdateAgentModeRejectsBrokenYAML(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("settings: [unclosed\n"), 0600); err != nil {
		t.Fatal(err)
	}
	err := config.UpdateAgentMode(path, "multi")
	if err == nil {
		t.Fatal("expected parse error, got nil")
	}
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), "unclosed") {
		t.Errorf("broken file was modified:\n%s", data)
	}
}
