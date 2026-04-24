package config

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeTestConfig(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestWatcherDetectsChange(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")

	// Write initial config.
	initial := `settings:
  language: en
  timezone: UTC
  notifications: true
  morning_briefing: "8:00 AM"
  active_channels: ["webchat"]
  model: claude-sonnet-4-6
  persona_name: Butler
  skills: ["coding"]
  agents_enabled: true
  agent_mode: auto
  cost:
    strategy: balanced
    monthly_budget: 10.0
configurations:
  hooks:
    pre_tool_use: []
`
	writeTestConfig(t, cfgPath, initial)

	cfg := Default()
	changed := make(chan bool, 1)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	stop := cfg.StartWatcher(ctx, cfgPath, 50*time.Millisecond, func(newCfg *Config) {
		select {
		case changed <- true:
		default:
		}
	})
	defer stop()

	// Wait for initial tick.
	time.Sleep(100 * time.Millisecond)

	// Modify config (change hooks).
	updated := initial + `    post_tool_use:
      - command: "echo audit"
`
	// Ensure modification time is newer.
	time.Sleep(100 * time.Millisecond)
	writeTestConfig(t, cfgPath, updated)

	// Wait for watcher to detect.
	select {
	case <-changed:
		// success
	case <-time.After(2 * time.Second):
		t.Fatal("watcher did not detect config change within 2 seconds")
	}
}

func TestWatcherIgnoresUnchanged(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")

	initial := `settings:
  language: en
  timezone: UTC
  notifications: true
  morning_briefing: "8:00 AM"
  active_channels: ["webchat"]
  model: claude-sonnet-4-6
  persona_name: Butler
  skills: ["coding"]
  agents_enabled: true
  agent_mode: auto
  cost:
    strategy: balanced
    monthly_budget: 10.0
`
	writeTestConfig(t, cfgPath, initial)

	cfg := Default()
	callCount := 0

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	stop := cfg.StartWatcher(ctx, cfgPath, 50*time.Millisecond, func(newCfg *Config) {
		callCount++
	})
	defer stop()

	// Wait several ticks.
	time.Sleep(300 * time.Millisecond)

	if callCount != 0 {
		t.Errorf("expected 0 onChange calls for unchanged file, got %d", callCount)
	}
}
