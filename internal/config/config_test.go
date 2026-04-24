package config_test

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/LumabyteCo/aibutler/internal/config"
)

func TestDefault(t *testing.T) {
	cfg := config.Default()

	if cfg.Settings.Language != "en" {
		t.Errorf("language = %q, want en", cfg.Settings.Language)
	}
	if cfg.Settings.Model != "claude-sonnet-4-6" {
		t.Errorf("model = %q, want claude-sonnet-4-6", cfg.Settings.Model)
	}
	if cfg.Settings.Cost.Strategy != "balanced" {
		t.Errorf("strategy = %q, want balanced", cfg.Settings.Cost.Strategy)
	}
	if cfg.Settings.Cost.MonthlyBudget != 10.00 {
		t.Errorf("budget = %f, want 10.00", cfg.Settings.Cost.MonthlyBudget)
	}
	if cfg.Configurations.Agents.MaxConcurrent != 5 {
		t.Errorf("max_concurrent = %d, want 5", cfg.Configurations.Agents.MaxConcurrent)
	}
	if cfg.Options.Prompts.MaxTier1Tokens != 700 {
		t.Errorf("max_tier1_tokens = %d, want 700", cfg.Options.Prompts.MaxTier1Tokens)
	}
	if cfg.Options.Agents.MaxToolCalls != 50 {
		t.Errorf("max_tool_calls = %d, want 50", cfg.Options.Agents.MaxToolCalls)
	}
}

func TestLoadYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	yamlContent := `
settings:
  language: ar
  model: gpt-4o
  cost:
    strategy: frugal
    monthly_budget: 5.00
  agent_mode: single
configurations:
  agents:
    max_concurrent: 3
options:
  prompts:
    max_tier1_tokens: 500
`
	if err := os.WriteFile(path, []byte(yamlContent), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	if cfg.Settings.Language != "ar" {
		t.Errorf("language = %q, want ar", cfg.Settings.Language)
	}
	if cfg.Settings.Model != "gpt-4o" {
		t.Errorf("model = %q, want gpt-4o", cfg.Settings.Model)
	}
	if cfg.Settings.Cost.Strategy != "frugal" {
		t.Errorf("strategy = %q, want frugal", cfg.Settings.Cost.Strategy)
	}
	if cfg.Settings.Cost.MonthlyBudget != 5.00 {
		t.Errorf("budget = %f, want 5.00", cfg.Settings.Cost.MonthlyBudget)
	}
	if cfg.Configurations.Agents.MaxConcurrent != 3 {
		t.Errorf("max_concurrent = %d, want 3", cfg.Configurations.Agents.MaxConcurrent)
	}
	if cfg.Options.Prompts.MaxTier1Tokens != 500 {
		t.Errorf("max_tier1_tokens = %d, want 500", cfg.Options.Prompts.MaxTier1Tokens)
	}
	// Resolution: settings.model overrides configurations.models.primary
	if cfg.Configurations.Models.Primary != "gpt-4o" {
		t.Errorf("resolved primary = %q, want gpt-4o", cfg.Configurations.Models.Primary)
	}
}

func TestLoadPartialYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	// Only override a few fields; rest should be defaults.
	yamlContent := `
settings:
  persona_name: Jarvis
`
	if err := os.WriteFile(path, []byte(yamlContent), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	if cfg.Settings.PersonaName != "Jarvis" {
		t.Errorf("persona = %q, want Jarvis", cfg.Settings.PersonaName)
	}
	// Defaults preserved.
	if cfg.Settings.Language != "en" {
		t.Errorf("language = %q, want en (default)", cfg.Settings.Language)
	}
	if cfg.Options.Agents.MaxToolCalls != 50 {
		t.Errorf("max_tool_calls = %d, want 50 (default)", cfg.Options.Agents.MaxToolCalls)
	}
}

func TestLoadEmptyYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	if err := os.WriteFile(path, []byte(""), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	// All defaults should be preserved.
	if cfg.Settings.Model != "claude-sonnet-4-6" {
		t.Errorf("model = %q, want claude-sonnet-4-6", cfg.Settings.Model)
	}
}

func TestValidateInvalidStrategy(t *testing.T) {
	cfg := config.Default()
	cfg.Settings.Cost.Strategy = "invalid"
	if err := cfg.Validate(); err == nil {
		t.Error("expected error for invalid cost strategy")
	}
}

func TestValidateNegativeBudget(t *testing.T) {
	cfg := config.Default()
	cfg.Settings.Cost.MonthlyBudget = -5.0
	if err := cfg.Validate(); err == nil {
		t.Error("expected error for negative budget")
	}
}

func TestValidateInvalidAgentMode(t *testing.T) {
	cfg := config.Default()
	cfg.Settings.AgentMode = "invalid_mode"
	if err := cfg.Validate(); err == nil {
		t.Error("expected error for invalid agent mode")
	}
}

func TestValidateSwarmModeValid(t *testing.T) {
	cfg := config.Default()
	cfg.Settings.AgentMode = "swarm"
	if err := cfg.Validate(); err != nil {
		t.Errorf("swarm mode should be valid: %v", err)
	}
}

func TestValidateInvalidOptions(t *testing.T) {
	cfg := config.Default()
	cfg.Options.Prompts.MaxTier1Tokens = 0
	if err := cfg.Validate(); err == nil {
		t.Error("expected error for zero max_tier1_tokens")
	}
}

func TestValidateValid(t *testing.T) {
	cfg := config.Default()
	if err := cfg.Validate(); err != nil {
		t.Errorf("unexpected validation error: %v", err)
	}
}

func TestResolveModelOverride(t *testing.T) {
	cfg := config.Default()
	cfg.Settings.Model = "gpt-4o"
	cfg.Resolve()

	if cfg.Configurations.Models.Primary != "gpt-4o" {
		t.Errorf("primary = %q, want gpt-4o (overridden by settings)", cfg.Configurations.Models.Primary)
	}
}

func TestSlidingWindowSize(t *testing.T) {
	tests := []struct {
		strategy string
		want     int
	}{
		{"frugal", 30},
		{"balanced", 100},
		{"quality", 200},
	}

	for _, tt := range tests {
		cfg := config.Default()
		cfg.Settings.Cost.Strategy = tt.strategy
		got := cfg.SlidingWindowSize()
		if got != tt.want {
			t.Errorf("SlidingWindowSize(%q) = %d, want %d", tt.strategy, got, tt.want)
		}
	}
}

// Config validation tests.

func TestValidateMultiMode(t *testing.T) {
	cfg := config.Default()
	cfg.Settings.AgentMode = "multi"
	if err := cfg.Validate(); err != nil {
		t.Errorf("multi mode should be valid: %v", err)
	}
}

func TestValidateCustomMode(t *testing.T) {
	cfg := config.Default()
	cfg.Settings.AgentMode = "custom"
	if err := cfg.Validate(); err != nil {
		t.Errorf("custom mode should be valid: %v", err)
	}
}

func TestValidateCustomRoutingStrategies(t *testing.T) {
	for _, routing := range []string{"classify", "explicit", "round-robin", ""} {
		cfg := config.Default()
		cfg.Settings.AgentMode = "custom"
		cfg.Configurations.Agents.Routing = routing
		if err := cfg.Validate(); err != nil {
			t.Errorf("routing %q should be valid: %v", routing, err)
		}
	}
}

func TestValidateInvalidRouting(t *testing.T) {
	cfg := config.Default()
	cfg.Settings.AgentMode = "custom"
	cfg.Configurations.Agents.Routing = "random"
	if err := cfg.Validate(); err == nil {
		t.Error("expected error for invalid routing strategy")
	}
}

func TestValidateTooManyCustomRoles(t *testing.T) {
	cfg := config.Default()
	// 11 roles exceeds the 10-role limit.
	roles := make([]config.CustomRoleSpec, 11)
	for i := range roles {
		roles[i] = config.CustomRoleSpec{Name: fmt.Sprintf("role-%d", i)}
	}
	cfg.Configurations.Agents.CustomRoles = roles
	if err := cfg.Validate(); err == nil {
		t.Error("expected error for >10 custom roles")
	}
}

func TestValidateAutonomyLevels(t *testing.T) {
	for _, level := range []string{"l1", "l2", "l3", ""} {
		cfg := config.Default()
		cfg.Options.Agents.AutonomyLevel = level
		if err := cfg.Validate(); err != nil {
			t.Errorf("autonomy %q should be valid: %v", level, err)
		}
	}
}

func TestValidateInvalidAutonomy(t *testing.T) {
	cfg := config.Default()
	cfg.Options.Agents.AutonomyLevel = "l4"
	if err := cfg.Validate(); err == nil {
		t.Error("expected error for invalid autonomy level")
	}
}

func TestValidateL3AutonomyValid(t *testing.T) {
	cfg := config.Default()
	cfg.Options.Agents.AutonomyLevel = "l3"
	if err := cfg.Validate(); err != nil {
		t.Errorf("l3 autonomy should be valid: %v", err)
	}
}

func TestDefaultLayer2Fields(t *testing.T) {
	cfg := config.Default()
	if cfg.Options.Agents.AutonomyLevel != "l1" {
		t.Errorf("default autonomy = %q, want l1", cfg.Options.Agents.AutonomyLevel)
	}
	if cfg.Options.Agents.ParallelToolLimit != 5 {
		t.Errorf("default parallel_tool_limit = %d, want 5", cfg.Options.Agents.ParallelToolLimit)
	}
}

func TestLoadOrDefaultMissingFile(t *testing.T) {
	cfg, err := config.LoadOrDefault()
	if err != nil {
		t.Fatalf("load or default: %v", err)
	}
	if cfg.Settings.Language != "en" {
		t.Errorf("language = %q, want en (default)", cfg.Settings.Language)
	}
}
