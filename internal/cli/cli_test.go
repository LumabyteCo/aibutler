package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LumabyteCo/aibutler/internal/config"
)

// testApp bootstraps an in-memory App for testing.
func testApp(t *testing.T) *App {
	t.Helper()
	dataDir := t.TempDir()
	app, err := Bootstrap(dataDir, ":memory:")
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	t.Cleanup(app.Shutdown)
	return app
}

func TestBootstrap(t *testing.T) {
	app := testApp(t)

	if app.Config == nil {
		t.Fatal("Config is nil")
	}
	if app.DB == nil {
		t.Fatal("DB is nil")
	}
	if app.Vault == nil {
		t.Fatal("Vault is nil")
	}
	if app.Engine == nil {
		t.Fatal("Engine is nil")
	}
	if app.Channels == nil {
		t.Fatal("Channels is nil")
	}
	if app.Tools == nil {
		t.Fatal("Tools is nil")
	}
	if app.Sessions == nil {
		t.Fatal("Sessions is nil")
	}
	if app.Composer == nil {
		t.Fatal("Composer is nil")
	}
	if app.Tracker == nil {
		t.Fatal("Tracker is nil")
	}
	// Scheduler may be nil if schedule is disabled; default has it enabled.
	if app.Scheduler == nil {
		t.Fatal("Scheduler is nil (expected non-nil with default config)")
	}
}

func TestBootstrapShutdown(t *testing.T) {
	dataDir := t.TempDir()
	app, err := Bootstrap(dataDir, ":memory:")
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	// Should not panic.
	app.Shutdown()
}

func TestVersion(t *testing.T) {
	var buf bytes.Buffer
	CmdVersion(&buf)
	out := buf.String()
	if !strings.Contains(out, "aibutler v"+Version) {
		t.Fatalf("expected 'aibutler v%s', got: %s", Version, out)
	}
}

func TestConfigShow(t *testing.T) {
	app := testApp(t)
	var buf bytes.Buffer
	if err := CmdConfig(app, []string{"show"}, &buf); err != nil {
		t.Fatalf("CmdConfig show: %v", err)
	}
	out := buf.String()
	for _, want := range []string{"Settings", "Configurations", "Options"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

func TestSkillList(t *testing.T) {
	app := testApp(t)
	var buf bytes.Buffer
	if err := CmdSkill(app, []string{"list"}, &buf); err != nil {
		t.Fatalf("CmdSkill list: %v", err)
	}
	out := buf.String()
	for _, want := range []string{"coding", "research"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

func TestCostStatus(t *testing.T) {
	app := testApp(t)
	var buf bytes.Buffer
	if err := CmdCost(app, []string{"status"}, &buf); err != nil {
		t.Fatalf("CmdCost status: %v", err)
	}
	out := buf.String()
	for _, want := range []string{"Cost Status", "Budget", "Remaining"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

func TestCostBreakdown(t *testing.T) {
	app := testApp(t)
	var buf bytes.Buffer
	if err := CmdCost(app, []string{"breakdown"}, &buf); err != nil {
		t.Fatalf("CmdCost breakdown: %v", err)
	}
	out := buf.String()
	// Fresh DB has no usage, so expect "No usage this month." or the MODEL header.
	if !strings.Contains(out, "No usage") && !strings.Contains(out, "MODEL") {
		t.Fatalf("unexpected breakdown output:\n%s", out)
	}
}

func TestCostStrategy(t *testing.T) {
	app := testApp(t)
	var buf bytes.Buffer
	if err := CmdCost(app, []string{"strategy", "frugal"}, &buf); err != nil {
		t.Fatalf("CmdCost strategy: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "frugal") {
		t.Fatalf("expected 'frugal' in output, got: %s", out)
	}
	// Verify the config was updated.
	if app.Config.Settings.Cost.Strategy != "frugal" {
		t.Fatalf("config not updated: strategy = %s", app.Config.Settings.Cost.Strategy)
	}
}

func TestCostBudget(t *testing.T) {
	app := testApp(t)
	var buf bytes.Buffer
	if err := CmdCost(app, []string{"budget", "25.00"}, &buf); err != nil {
		t.Fatalf("CmdCost budget: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "$25.00") {
		t.Fatalf("expected '$25.00' in output, got: %s", out)
	}
	if app.Config.Settings.Cost.MonthlyBudget != 25.00 {
		t.Fatalf("config not updated: budget = %.2f", app.Config.Settings.Cost.MonthlyBudget)
	}
}

func TestAgentList(t *testing.T) {
	app := testApp(t)
	var buf bytes.Buffer
	if err := CmdAgent(app, []string{"list"}, &buf); err != nil {
		t.Fatalf("CmdAgent list: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "No active agents.") {
		t.Fatalf("expected 'No active agents.', got: %s", out)
	}
}

func TestAgentHistory(t *testing.T) {
	app := testApp(t)
	var buf bytes.Buffer
	if err := CmdAgent(app, []string{"history"}, &buf); err != nil {
		t.Fatalf("CmdAgent history: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "No agent history.") {
		t.Fatalf("expected 'No agent history.', got: %s", out)
	}
}

func TestModeShow(t *testing.T) {
	app := testApp(t)
	var buf bytes.Buffer
	if err := CmdMode(app, []string{}, &buf); err != nil {
		t.Fatalf("CmdMode show: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "auto") {
		t.Errorf("output missing 'auto':\n%s", out)
	}
	// auto resolves to multi (parallel tool execution) — the note must
	// describe real behavior, not the retired "behaves as single" text.
	if !strings.Contains(out, "resolves to multi") {
		t.Errorf("output missing auto→multi note:\n%s", out)
	}
}

func TestModeSwitch(t *testing.T) {
	app := testApp(t)

	// Switch to single.
	var buf bytes.Buffer
	if err := CmdMode(app, []string{"single"}, &buf); err != nil {
		t.Fatalf("CmdMode switch to single: %v", err)
	}

	// Verify by showing mode.
	buf.Reset()
	if err := CmdMode(app, []string{}, &buf); err != nil {
		t.Fatalf("CmdMode show after switch: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "single") {
		t.Fatalf("expected 'single' in output, got: %s", out)
	}
}

func TestModeMultiPersists(t *testing.T) {
	app := testApp(t)

	var buf bytes.Buffer
	if err := CmdMode(app, []string{"multi"}, &buf); err != nil {
		t.Fatalf("CmdMode multi: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "switched to: multi") {
		t.Errorf("expected multi switch message, got: %s", out)
	}
	if app.Config.Settings.AgentMode != "multi" {
		t.Fatalf("expected mode 'multi', got: %s", app.Config.Settings.AgentMode)
	}

	// The switch must be persisted: re-reading the config from disk has to
	// show multi, not the previous value. (The old implementation only
	// mutated the in-memory struct, so `aibutler mode single` printed
	// success and then forgot it on exit.)
	reloaded, err := config.Load(app.configPath)
	if err != nil {
		t.Fatalf("reload config: %v", err)
	}
	if reloaded.Settings.AgentMode != "multi" {
		t.Fatalf("persisted mode = %q, want multi", reloaded.Settings.AgentMode)
	}
}

func TestModeSwarmPersists(t *testing.T) {
	app := testApp(t)

	var buf bytes.Buffer
	if err := CmdMode(app, []string{"swarm"}, &buf); err != nil {
		t.Fatalf("CmdMode swarm: %v", err)
	}
	if !strings.Contains(buf.String(), "switched to: swarm") {
		t.Errorf("expected swarm switch message, got: %s", buf.String())
	}
	if app.Config.Settings.AgentMode != "swarm" {
		t.Fatalf("mode = %q, want swarm", app.Config.Settings.AgentMode)
	}
	reloaded, err := config.Load(app.configPath)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if reloaded.Settings.AgentMode != "swarm" {
		t.Fatalf("persisted mode = %q, want swarm", reloaded.Settings.AgentMode)
	}
}

func TestModeCustomPersists(t *testing.T) {
	app := testApp(t)

	var buf bytes.Buffer
	if err := CmdMode(app, []string{"custom"}, &buf); err != nil {
		t.Fatalf("CmdMode custom: %v", err)
	}
	if !strings.Contains(buf.String(), "switched to: custom") {
		t.Errorf("expected custom switch message, got: %s", buf.String())
	}
	if app.Config.Settings.AgentMode != "custom" {
		t.Fatalf("mode = %q, want custom", app.Config.Settings.AgentMode)
	}
	reloaded, err := config.Load(app.configPath)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if reloaded.Settings.AgentMode != "custom" {
		t.Fatalf("persisted mode = %q, want custom", reloaded.Settings.AgentMode)
	}
}

func TestAuthStatus(t *testing.T) {
	app := testApp(t)
	var buf bytes.Buffer
	if err := CmdAuth(app, []string{"status"}, &buf); err != nil {
		t.Fatalf("CmdAuth status: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "healthy") {
		t.Fatalf("expected 'healthy' in output, got: %s", out)
	}
}

func TestAuthList(t *testing.T) {
	app := testApp(t)
	var buf bytes.Buffer
	if err := CmdAuth(app, []string{"list"}, &buf); err != nil {
		t.Fatalf("CmdAuth list: %v", err)
	}
	// No error is sufficient; fresh vault has no credentials.
}

func TestVoiceStatus(t *testing.T) {
	app := testApp(t)
	var buf bytes.Buffer
	if err := CmdVoice(app, []string{"status"}, &buf); err != nil {
		t.Fatalf("CmdVoice status: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "whisper") {
		t.Errorf("output missing 'whisper':\n%s", out)
	}
	if !strings.Contains(out, "stub") {
		t.Errorf("output missing 'stub':\n%s", out)
	}
}

func TestVoiceProviders(t *testing.T) {
	app := testApp(t)
	var buf bytes.Buffer
	if err := CmdVoice(app, []string{"providers"}, &buf); err != nil {
		t.Fatalf("CmdVoice providers: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "whisper") {
		t.Fatalf("expected 'whisper' in providers, got: %s", out)
	}
}

func TestBackupNow(t *testing.T) {
	// Backup requires an on-disk database (SQLite cannot backup :memory:).
	dataDir := t.TempDir()
	dbPath := filepath.Join(dataDir, "aibutler.db")
	app, err := Bootstrap(dataDir, dbPath)
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	t.Cleanup(app.Shutdown)

	var buf bytes.Buffer
	if err := CmdBackup(app, []string{"now"}, &buf); err != nil {
		t.Fatalf("CmdBackup now: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "Backup created") {
		t.Fatalf("expected 'Backup created' in output, got: %s", out)
	}

	// Verify backup file was actually created in the backups directory.
	backups := filepath.Join(dataDir, "backups")
	entries, err := os.ReadDir(backups)
	if err != nil {
		t.Fatalf("read backup dir: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("no backup files found")
	}
}

func TestIntegrity(t *testing.T) {
	app := testApp(t)
	var buf bytes.Buffer
	if err := CmdIntegrity(app, nil, &buf); err != nil {
		t.Fatalf("CmdIntegrity: %v", err)
	}
	out := buf.String()
	// Output format: "Database integrity... OK" and "Vault health...      OK"
	if !strings.Contains(out, "Database integrity") || !strings.Contains(out, "OK") {
		t.Errorf("expected database OK in output:\n%s", out)
	}
	if !strings.Contains(out, "Vault health") || !strings.Contains(out, "OK") {
		t.Errorf("expected vault OK in output:\n%s", out)
	}
}

func TestSetup(t *testing.T) {
	app := testApp(t)
	var buf bytes.Buffer
	if err := CmdSetup(app, nil, &buf); err != nil {
		t.Fatalf("CmdSetup: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "AI Butler Setup") {
		t.Fatalf("expected 'AI Butler Setup' in output, got: %s", out)
	}
}
