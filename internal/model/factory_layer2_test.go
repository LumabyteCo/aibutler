package model_test

import (
	"context"
	"strings"
	"testing"

	"github.com/LumabyteCo/aibutler/internal/agent"
	"github.com/LumabyteCo/aibutler/internal/capability"
	"github.com/LumabyteCo/aibutler/internal/model"
	"github.com/LumabyteCo/aibutler/internal/prompt"
	"github.com/LumabyteCo/aibutler/internal/session"
	"github.com/LumabyteCo/aibutler/testutil"
)

func TestFactoryWithCustomRoleRouting(t *testing.T) {
	database := testutil.TestDB(t)
	cfg := testutil.TestConfig()
	cfg.Settings.AgentMode = "custom"

	fake := testutil.NewFakeModel(agent.Response{
		Content: "analysis complete", TokensIn: 10, TokensOut: 5,
	})

	sm := session.NewManager(database.Conn(), cfg)
	tracker := prompt.NewTracker(database.Conn(), cfg)
	composer := prompt.NewComposer(cfg, sm, tracker, database.Conn())

	// Create a role router with an explicit routing strategy.
	roles := []agent.CustomRole{
		{Name: "analyst", Description: "Data analysis", Prompt: "You are a data analyst specializing in statistics."},
		{Name: "coder", Description: "Code tasks", Prompt: "You are an expert programmer."},
	}
	router := agent.NewRoleRouter(roles, "explicit", nil)

	factory := model.NewFactory(model.FactoryConfig{
		Composer:   composer,
		Model:      fake,
		Caps:       capability.NewCapabilitySet(nil),
		Tracker:    tracker,
		DB:         database.Conn(),
		Config:     cfg,
		RoleRouter: router,
	})

	ctx := context.Background()
	sessID, _ := sm.Create(ctx, "webchat", "user-1", "default")

	// "@analyst analyze this data" should route to analyst role.
	result, err := factory.Run(ctx, sessID, "@analyst analyze this data", "webchat")
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if result.Status != agent.StateCompleted {
		t.Fatalf("status = %s", result.Status)
	}

	// Verify the system prompt includes the analyst role's prompt.
	calls := fake.Calls()
	if len(calls) != 1 {
		t.Fatalf("model calls = %d, want 1", len(calls))
	}
	systemMsg := calls[0][0]
	if systemMsg.Role != "system" {
		t.Fatalf("first message role = %s, want system", systemMsg.Role)
	}
	if !strings.Contains(systemMsg.Content, "data analyst") {
		t.Errorf("system prompt should contain role prompt 'data analyst', got: %s", systemMsg.Content[:min(200, len(systemMsg.Content))])
	}
}

func TestFactoryAutonomyL2PassedToAgent(t *testing.T) {
	database := testutil.TestDB(t)
	cfg := testutil.TestConfig()
	cfg.Options.Agents.AutonomyLevel = "l2"
	cfg.Options.Agents.L2AskActions = []string{"shell.exec"}

	// Model returns a tool call then a final answer.
	fake := testutil.NewFakeModel(
		agent.Response{
			ToolCalls: []agent.ToolCall{{ID: "1", Name: "shell.exec", Input: `{"cmd":"ls"}`}},
		},
		agent.Response{Content: "blocked by policy"},
	)

	sm := session.NewManager(database.Conn(), cfg)
	tracker := prompt.NewTracker(database.Conn(), cfg)
	composer := prompt.NewComposer(cfg, sm, tracker, database.Conn())

	factory := model.NewFactory(model.FactoryConfig{
		Composer: composer,
		Model:    fake,
		Tracker:  tracker,
		DB:       database.Conn(),
		Config:   cfg,
	})

	ctx := context.Background()
	sessID, _ := sm.Create(ctx, "webchat", "user-1", "default")

	result, err := factory.Run(ctx, sessID, "list files", "webchat")
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if result.Status != agent.StateCompleted {
		t.Fatalf("status = %s", result.Status)
	}

	// The model should have received a "blocked" tool result on the second call.
	calls := fake.Calls()
	if len(calls) < 2 {
		t.Fatalf("expected at least 2 model calls, got %d", len(calls))
	}
	secondCall := calls[1]
	found := false
	for _, msg := range secondCall {
		if msg.Role == "tool" && strings.Contains(msg.Content, "blocked by autonomy") {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected autonomy block message in tool result")
	}
}

func TestFactoryPerTurnModeOverride(t *testing.T) {
	database := testutil.TestDB(t)
	cfg := testutil.TestConfig()
	cfg.Settings.AgentMode = "multi" // Default mode is multi.

	// Model returns two tool calls; downgrade to single should still strip prefix.
	fake := testutil.NewFakeModel(
		agent.Response{
			ToolCalls: []agent.ToolCall{
				{ID: "1", Name: "test.tool1", Input: `{}`},
				{ID: "2", Name: "test.tool2", Input: `{}`},
			},
		},
		agent.Response{Content: "done"},
	)

	sm := session.NewManager(database.Conn(), cfg)
	tracker := prompt.NewTracker(database.Conn(), cfg)
	composer := prompt.NewComposer(cfg, sm, tracker, database.Conn())

	factory := model.NewFactory(model.FactoryConfig{
		Composer: composer,
		Model:    fake,
		Tracker:  tracker,
		DB:       database.Conn(),
		Config:   cfg,
	})

	ctx := context.Background()
	sessID, _ := sm.Create(ctx, "webchat", "user-1", "default")

	// "[mode:single] do something" should override multi to single for this turn (downgrade allowed).
	result, err := factory.Run(ctx, sessID, "[mode:single] do something", "webchat")
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if result.Status != agent.StateCompleted {
		t.Fatalf("status = %s", result.Status)
	}

	// The user message sent to the model should NOT contain the "[mode:single]" prefix.
	calls := fake.Calls()
	if len(calls) < 1 {
		t.Fatal("expected at least 1 model call")
	}
	lastMsg := calls[0][len(calls[0])-1]
	if lastMsg.Role != "user" {
		t.Fatalf("last message role = %s, want user", lastMsg.Role)
	}
	if strings.Contains(lastMsg.Content, "[mode:single]") {
		t.Errorf("user message should not contain mode override prefix, got: %s", lastMsg.Content)
	}
	if !strings.Contains(lastMsg.Content, "do something") {
		t.Errorf("user message should contain cleaned task, got: %s", lastMsg.Content)
	}
}

func TestFactoryPerTurnModeOverrideBlocksEscalation(t *testing.T) {
	database := testutil.TestDB(t)
	cfg := testutil.TestConfig()
	cfg.Settings.AgentMode = "single" // Default mode is single.

	fake := testutil.NewFakeModel(
		agent.Response{Content: "done"},
	)

	sm := session.NewManager(database.Conn(), cfg)
	tracker := prompt.NewTracker(database.Conn(), cfg)
	composer := prompt.NewComposer(cfg, sm, tracker, database.Conn())

	factory := model.NewFactory(model.FactoryConfig{
		Composer: composer,
		Model:    fake,
		Tracker:  tracker,
		DB:       database.Conn(),
		Config:   cfg,
	})

	ctx := context.Background()
	sessID, _ := sm.Create(ctx, "webchat", "user-1", "default")

	// "[mode:multi] escalate" should be blocked (single->multi is escalation).
	// The prefix is NOT stripped because the override is rejected.
	result, err := factory.Run(ctx, sessID, "[mode:multi] escalate", "webchat")
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if result.Status != agent.StateCompleted {
		t.Fatalf("status = %s", result.Status)
	}

	// The user message should still contain the prefix since override was blocked.
	calls := fake.Calls()
	if len(calls) < 1 {
		t.Fatal("expected at least 1 model call")
	}
	lastMsg := calls[0][len(calls[0])-1]
	if lastMsg.Role != "user" {
		t.Fatalf("last message role = %s, want user", lastMsg.Role)
	}
	if !strings.Contains(lastMsg.Content, "[mode:multi]") {
		t.Errorf("blocked escalation should preserve original task including prefix, got: %s", lastMsg.Content)
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
