package prompt_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LumabyteCo/aibutler/internal/agent"
	"github.com/LumabyteCo/aibutler/internal/prompt"
	"github.com/LumabyteCo/aibutler/internal/session"
	"github.com/LumabyteCo/aibutler/testutil"
)

// --- Skill Tests ---

func TestLoadDefaultSkills(t *testing.T) {
	skills, err := prompt.LoadDefaultSkills()
	if err != nil {
		t.Fatalf("load defaults: %v", err)
	}
	if len(skills) < 2 {
		t.Fatalf("expected >= 2 default skills, got %d", len(skills))
	}

	// Verify coding skill.
	var coding *prompt.Skill
	for _, s := range skills {
		if s.Name == "coding" {
			coding = s
			break
		}
	}
	if coding == nil {
		t.Fatal("coding skill not found")
	}
	if !coding.Enabled {
		t.Error("coding should be enabled")
	}
	if coding.Summary == "" {
		t.Error("coding summary should not be empty")
	}
	if len(coding.Triggers) == 0 {
		t.Error("coding triggers should not be empty")
	}
	if coding.Body == "" {
		t.Error("coding body should not be empty")
	}
}

func TestLoadSkillFromFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "custom.md")

	content := `---
name: custom
description: "Custom skill"
summary: "custom things"
enabled: true
triggers: [custom, special]
---

# Custom Skill

Do custom things.
`
	os.WriteFile(path, []byte(content), 0644)

	skill, err := prompt.LoadSkill(path)
	if err != nil {
		t.Fatalf("load skill: %v", err)
	}
	if skill.Name != "custom" {
		t.Errorf("name = %q, want custom", skill.Name)
	}
	if !skill.Enabled {
		t.Error("expected enabled")
	}
	if len(skill.Triggers) != 2 {
		t.Errorf("triggers = %d, want 2", len(skill.Triggers))
	}
	if !strings.Contains(skill.Body, "Custom Skill") {
		t.Errorf("body missing 'Custom Skill': %q", skill.Body)
	}
}

func TestLoadSkillsDir(t *testing.T) {
	dir := t.TempDir()

	// Write two skill files.
	for _, name := range []string{"a.md", "b.md"} {
		content := "---\nname: " + strings.TrimSuffix(name, ".md") + "\nenabled: true\n---\n\nBody."
		os.WriteFile(filepath.Join(dir, name), []byte(content), 0644)
	}
	// Write a non-skill file (should be ignored).
	os.WriteFile(filepath.Join(dir, "readme.txt"), []byte("not a skill"), 0644)

	skills, err := prompt.LoadSkillsDir(dir)
	if err != nil {
		t.Fatalf("load dir: %v", err)
	}
	if len(skills) != 2 {
		t.Errorf("skills = %d, want 2", len(skills))
	}
}

func TestLoadSkillsDirMissing(t *testing.T) {
	skills, err := prompt.LoadSkillsDir("/nonexistent/path")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if skills != nil {
		t.Errorf("expected nil skills for missing dir, got %d", len(skills))
	}
}

func TestMatchSkillsCoding(t *testing.T) {
	skills := []*prompt.Skill{
		{Name: "coding", Enabled: true, Triggers: []string{"code", "file", "git", "debug"}},
		{Name: "research", Enabled: true, Triggers: []string{"search", "find", "what is"}},
	}

	matched := prompt.MatchSkills(skills, "help me write code for this file", 3)
	if len(matched) != 1 {
		t.Fatalf("matched = %d, want 1", len(matched))
	}
	if matched[0].Name != "coding" {
		t.Errorf("matched[0] = %q, want coding", matched[0].Name)
	}
}

func TestMatchSkillsResearch(t *testing.T) {
	skills := []*prompt.Skill{
		{Name: "coding", Enabled: true, Triggers: []string{"code", "file", "git"}},
		{Name: "research", Enabled: true, Triggers: []string{"search", "find", "what is"}},
	}

	matched := prompt.MatchSkills(skills, "what is the capital of France?", 3)
	if len(matched) != 1 {
		t.Fatalf("matched = %d, want 1", len(matched))
	}
	if matched[0].Name != "research" {
		t.Errorf("matched[0] = %q, want research", matched[0].Name)
	}
}

func TestMatchSkillsNoMatch(t *testing.T) {
	skills := []*prompt.Skill{
		{Name: "coding", Enabled: true, Triggers: []string{"code", "file", "git"}},
	}

	matched := prompt.MatchSkills(skills, "what's the weather like?", 3)
	if len(matched) != 0 {
		t.Errorf("matched = %d, want 0", len(matched))
	}
}

func TestMatchSkillsDisabledIgnored(t *testing.T) {
	skills := []*prompt.Skill{
		{Name: "coding", Enabled: false, Triggers: []string{"code"}},
	}

	matched := prompt.MatchSkills(skills, "help me write code", 3)
	if len(matched) != 0 {
		t.Errorf("matched = %d, want 0 (disabled)", len(matched))
	}
}

func TestMatchSkillsMaxLimit(t *testing.T) {
	skills := []*prompt.Skill{
		{Name: "a", Enabled: true, Triggers: []string{"hello"}},
		{Name: "b", Enabled: true, Triggers: []string{"hello"}},
		{Name: "c", Enabled: true, Triggers: []string{"hello"}},
		{Name: "d", Enabled: true, Triggers: []string{"hello"}},
		{Name: "e", Enabled: true, Triggers: []string{"hello"}},
	}

	matched := prompt.MatchSkills(skills, "hello world", 2)
	if len(matched) != 2 {
		t.Errorf("matched = %d, want 2 (max limit)", len(matched))
	}
}

// --- Cost Tracker Tests ---

func TestCostRecordAndMonthlyUsage(t *testing.T) {
	database := testutil.TestDB(t)
	cfg := testutil.TestConfig()
	tracker := prompt.NewTracker(database.Conn(), cfg)
	ctx := context.Background()

	// Record some usage.
	tracker.Record(ctx, prompt.UsageEntry{
		SessionID: "s1", Model: "claude-sonnet-4-6", Provider: "anthropic",
		InputTokens: 1000, OutputTokens: 500, CostUSD: 0.015,
	})
	tracker.Record(ctx, prompt.UsageEntry{
		SessionID: "s1", Model: "claude-sonnet-4-6", Provider: "anthropic",
		InputTokens: 2000, OutputTokens: 1000, CostUSD: 0.030,
	})

	total, err := tracker.MonthlyUsage(ctx)
	if err != nil {
		t.Fatalf("monthly usage: %v", err)
	}
	// Should be 0.015 + 0.030 = 0.045
	if total < 0.044 || total > 0.046 {
		t.Errorf("total = %f, want ~0.045", total)
	}
}

func TestCostMonthlyBreakdown(t *testing.T) {
	database := testutil.TestDB(t)
	cfg := testutil.TestConfig()
	tracker := prompt.NewTracker(database.Conn(), cfg)
	ctx := context.Background()

	tracker.Record(ctx, prompt.UsageEntry{
		SessionID: "s1", Model: "claude-sonnet-4-6", Provider: "anthropic",
		InputTokens: 1000, OutputTokens: 500, CostUSD: 0.01,
	})
	tracker.Record(ctx, prompt.UsageEntry{
		SessionID: "s1", Model: "gpt-4o", Provider: "openai",
		InputTokens: 500, OutputTokens: 200, CostUSD: 0.005,
	})

	breakdown, err := tracker.MonthlyBreakdown(ctx)
	if err != nil {
		t.Fatalf("breakdown: %v", err)
	}
	if len(breakdown) != 2 {
		t.Errorf("breakdown models = %d, want 2", len(breakdown))
	}
}

func TestBudgetAlertUnderBudget(t *testing.T) {
	database := testutil.TestDB(t)
	cfg := testutil.TestConfig()
	cfg.Settings.Cost.MonthlyBudget = 10.00
	tracker := prompt.NewTracker(database.Conn(), cfg)
	ctx := context.Background()

	// Record small amount.
	tracker.Record(ctx, prompt.UsageEntry{
		SessionID: "s1", Model: "claude-sonnet-4-6", Provider: "anthropic",
		InputTokens: 100, OutputTokens: 50, CostUSD: 0.001,
	})

	alert, err := tracker.CheckBudget(ctx)
	if err != nil {
		t.Fatalf("check budget: %v", err)
	}
	if alert != nil {
		t.Errorf("expected nil alert (under budget), got %v", alert)
	}
}

func TestBudgetAlertAt75Percent(t *testing.T) {
	database := testutil.TestDB(t)
	cfg := testutil.TestConfig()
	cfg.Settings.Cost.MonthlyBudget = 10.00
	tracker := prompt.NewTracker(database.Conn(), cfg)
	ctx := context.Background()

	// Record 7.50 (75% of $10).
	tracker.Record(ctx, prompt.UsageEntry{
		SessionID: "s1", Model: "claude-sonnet-4-6", Provider: "anthropic",
		InputTokens: 10000, OutputTokens: 5000, CostUSD: 7.50,
	})

	alert, err := tracker.CheckBudget(ctx)
	if err != nil {
		t.Fatalf("check budget: %v", err)
	}
	if alert == nil {
		t.Fatal("expected alert at 75%")
	}
	if alert.Percentage != 75 {
		t.Errorf("percentage = %f, want 75", alert.Percentage)
	}
	if alert.Action != "warn" {
		t.Errorf("action = %q, want warn", alert.Action)
	}
}

func TestBudgetAlertAt100Percent(t *testing.T) {
	database := testutil.TestDB(t)
	cfg := testutil.TestConfig()
	cfg.Settings.Cost.MonthlyBudget = 10.00
	tracker := prompt.NewTracker(database.Conn(), cfg)
	ctx := context.Background()

	// Record 10.50 (105% of $10).
	tracker.Record(ctx, prompt.UsageEntry{
		SessionID: "s1", Model: "claude-sonnet-4-6", Provider: "anthropic",
		InputTokens: 10000, OutputTokens: 5000, CostUSD: 10.50,
	})

	alert, err := tracker.CheckBudget(ctx)
	if err != nil {
		t.Fatalf("check budget: %v", err)
	}
	if alert == nil {
		t.Fatal("expected alert at 100%")
	}
	if alert.Percentage != 100 {
		t.Errorf("percentage = %f, want 100", alert.Percentage)
	}
}

func TestBudgetNoBudgetSet(t *testing.T) {
	database := testutil.TestDB(t)
	cfg := testutil.TestConfig()
	cfg.Settings.Cost.MonthlyBudget = 0 // No budget
	tracker := prompt.NewTracker(database.Conn(), cfg)

	alert, err := tracker.CheckBudget(context.Background())
	if err != nil {
		t.Fatalf("check budget: %v", err)
	}
	if alert != nil {
		t.Error("expected nil alert when no budget set")
	}
}

// --- Composer Tests ---

func TestComposerLoadSkills(t *testing.T) {
	database := testutil.TestDB(t)
	cfg := testutil.TestConfig()
	sm := session.NewManager(database.Conn(), cfg)
	tracker := prompt.NewTracker(database.Conn(), cfg)

	composer := prompt.NewComposer(cfg, sm, tracker, database.Conn())
	if err := composer.LoadSkills(); err != nil {
		t.Fatalf("load skills: %v", err)
	}

	skills := composer.Skills()
	if len(skills) < 2 {
		t.Errorf("expected >= 2 skills, got %d", len(skills))
	}
}

func TestComposeTier1Under700Tokens(t *testing.T) {
	database := testutil.TestDB(t)
	cfg := testutil.TestConfig()
	sm := session.NewManager(database.Conn(), cfg)
	tracker := prompt.NewTracker(database.Conn(), cfg)

	composer := prompt.NewComposer(cfg, sm, tracker, database.Conn())
	composer.LoadSkills()

	p, err := composer.Compose(context.Background(), "", "hello", "")
	if err != nil {
		t.Fatalf("compose: %v", err)
	}

	if p.SystemMessage == "" {
		t.Error("system message should not be empty")
	}

	// Check contains persona name.
	if !strings.Contains(p.SystemMessage, cfg.Settings.PersonaName) {
		t.Errorf("system message missing persona name %q", cfg.Settings.PersonaName)
	}

	// Check contains mode.
	if !strings.Contains(p.SystemMessage, "Mode:") {
		t.Error("system message missing mode")
	}
}

func TestComposeTier2ActivatesSkills(t *testing.T) {
	database := testutil.TestDB(t)
	cfg := testutil.TestConfig()
	sm := session.NewManager(database.Conn(), cfg)
	tracker := prompt.NewTracker(database.Conn(), cfg)

	composer := prompt.NewComposer(cfg, sm, tracker, database.Conn())
	composer.LoadSkills()

	p, err := composer.Compose(context.Background(), "", "help me debug this code", "")
	if err != nil {
		t.Fatalf("compose: %v", err)
	}

	if len(p.SkillsLoaded) == 0 {
		t.Error("expected skills to be loaded for coding question")
	}
	if p.SkillContext == "" {
		t.Error("expected skill context for coding question")
	}

	// Check coding skill was activated.
	found := false
	for _, name := range p.SkillsLoaded {
		if name == "coding" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected coding skill in loaded skills: %v", p.SkillsLoaded)
	}
}

func TestComposeTier2NoSkillsForUnrelated(t *testing.T) {
	database := testutil.TestDB(t)
	cfg := testutil.TestConfig()
	sm := session.NewManager(database.Conn(), cfg)
	tracker := prompt.NewTracker(database.Conn(), cfg)

	composer := prompt.NewComposer(cfg, sm, tracker, database.Conn())
	composer.LoadSkills()

	p, err := composer.Compose(context.Background(), "", "what's the weather like?", "")
	if err != nil {
		t.Fatalf("compose: %v", err)
	}

	if len(p.SkillsLoaded) != 0 {
		t.Errorf("expected no skills loaded for weather question, got %v", p.SkillsLoaded)
	}
}

func TestComposeTier3SlidingWindow(t *testing.T) {
	database := testutil.TestDB(t)
	cfg := testutil.TestConfig()
	cfg.Settings.Cost.Strategy = "frugal" // 30 message window
	sm := session.NewManager(database.Conn(), cfg)
	tracker := prompt.NewTracker(database.Conn(), cfg)
	ctx := context.Background()

	// Create session with messages.
	sessionID, _ := sm.Create(ctx, "terminal", "user-1", "default")
	for i := 0; i < 50; i++ {
		sm.AddMessage(ctx, sessionID, agent.Message{Role: "user", Content: "msg"})
	}

	composer := prompt.NewComposer(cfg, sm, tracker, database.Conn())
	composer.LoadSkills()

	p, err := composer.Compose(ctx, sessionID, "new message", "")
	if err != nil {
		t.Fatalf("compose: %v", err)
	}

	if len(p.History) != 30 {
		t.Errorf("history = %d, want 30 (frugal window)", len(p.History))
	}
}

func TestComposeWithKeyFacts(t *testing.T) {
	database := testutil.TestDBSeeded(t) // Has 2 key facts
	cfg := testutil.TestConfig()
	sm := session.NewManager(database.Conn(), cfg)
	tracker := prompt.NewTracker(database.Conn(), cfg)

	composer := prompt.NewComposer(cfg, sm, tracker, database.Conn())
	composer.LoadSkills()

	p, err := composer.Compose(context.Background(), "", "hello", "")
	if err != nil {
		t.Fatalf("compose: %v", err)
	}

	// System message should contain key facts.
	if !strings.Contains(p.SystemMessage, "Key facts:") {
		t.Error("system message missing key facts")
	}
}

func TestComposeEstimatesTokens(t *testing.T) {
	database := testutil.TestDB(t)
	cfg := testutil.TestConfig()
	sm := session.NewManager(database.Conn(), cfg)
	tracker := prompt.NewTracker(database.Conn(), cfg)

	composer := prompt.NewComposer(cfg, sm, tracker, database.Conn())
	composer.LoadSkills()

	p, err := composer.Compose(context.Background(), "", "hello there", "")
	if err != nil {
		t.Fatalf("compose: %v", err)
	}

	if p.EstTokens <= 0 {
		t.Errorf("estimated tokens = %d, want > 0", p.EstTokens)
	}
}
