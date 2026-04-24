package model_test

import (
	"context"
	"testing"

	"github.com/LumabyteCo/aibutler/internal/agent"
	"github.com/LumabyteCo/aibutler/internal/capability"
	"github.com/LumabyteCo/aibutler/internal/model"
	"github.com/LumabyteCo/aibutler/internal/prompt"
	"github.com/LumabyteCo/aibutler/internal/session"
	"github.com/LumabyteCo/aibutler/internal/tool"
	"github.com/LumabyteCo/aibutler/testutil"
)

func TestFactorySimpleResponse(t *testing.T) {
	database := testutil.TestDB(t)
	cfg := testutil.TestConfig()

	fake := testutil.NewFakeModel(agent.Response{
		Content:  "Hello! I'm your AI assistant.",
		TokensIn: 50, TokensOut: 10,
	})

	sm := session.NewManager(database.Conn(), cfg)
	tracker := prompt.NewTracker(database.Conn(), cfg)
	composer := prompt.NewComposer(cfg, sm, tracker, database.Conn())

	factory := model.NewFactory(model.FactoryConfig{
		Composer: composer,
		Model:    fake,
		Tools:    nil,
		Caps:     capability.NewCapabilitySet(capability.MessagingDefaults()),
		Tracker:  tracker,
		DB:       database.Conn(),
		Config:   cfg,
	})

	ctx := context.Background()
	sessID, err := sm.Create(ctx, "webchat", "user-1", "default")
	if err != nil {
		t.Fatal(err)
	}

	result, err := factory.Run(ctx, sessID, "Hello!", "webchat")
	if err != nil {
		t.Fatalf("Factory.Run error: %v", err)
	}
	if result.Output != "Hello! I'm your AI assistant." {
		t.Errorf("output = %q, want 'Hello! I'm your AI assistant.'", result.Output)
	}
	if result.Status != agent.StateCompleted {
		t.Errorf("status = %v, want completed", result.Status)
	}
}

func TestFactoryWithToolCall(t *testing.T) {
	database := testutil.TestDB(t)
	cfg := testutil.TestConfig()

	// FakeModel: first call returns tool_call, second returns final answer.
	fake := testutil.NewFakeModel(
		agent.Response{
			Content: "I'll add that task for you.",
			ToolCalls: []agent.ToolCall{
				{ID: "call_1", Name: "task.add", Input: `{"content":"Buy groceries"}`},
			},
			TokensIn: 50, TokensOut: 20,
		},
		agent.Response{
			Content:  "Done! I've added 'Buy groceries' to your task list.",
			TokensIn: 80, TokensOut: 15,
		},
	)

	sm := session.NewManager(database.Conn(), cfg)
	tracker := prompt.NewTracker(database.Conn(), cfg)
	composer := prompt.NewComposer(cfg, sm, tracker, database.Conn())

	// Register data tools so task.add is available.
	registry := tool.NewRegistry()
	tool.RegisterDataTools(registry, database.Conn())
	dispatcher := tool.NewDispatcher(registry, capability.NewEngine(nil), nil)

	factory := model.NewFactory(model.FactoryConfig{
		Composer: composer,
		Model:    fake,
		Tools:    dispatcher,
		Caps:     capability.NewCapabilitySet(capability.MessagingDefaults()),
		Tracker:  tracker,
		DB:       database.Conn(),
		Config:   cfg,
	})

	ctx := context.Background()
	sessID, err := sm.Create(ctx, "webchat", "user-1", "default")
	if err != nil {
		t.Fatal(err)
	}

	result, err := factory.Run(ctx, sessID, "Add buy groceries to my list", "webchat")
	if err != nil {
		t.Fatalf("Factory.Run error: %v", err)
	}
	if result.Output != "Done! I've added 'Buy groceries' to your task list." {
		t.Errorf("output = %q", result.Output)
	}
	if fake.CallCount() != 2 {
		t.Errorf("model calls = %d, want 2", fake.CallCount())
	}

	// Verify task was actually added to DB.
	var count int
	err = database.Conn().QueryRow("SELECT COUNT(*) FROM user_tasks WHERE content = 'Buy groceries'").Scan(&count)
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Errorf("tasks in DB = %d, want 1", count)
	}
}

func TestFactoryPromptComposition(t *testing.T) {
	database := testutil.TestDB(t)
	cfg := testutil.TestConfig()
	cfg.Settings.PersonaName = "TestButler"

	fake := testutil.NewFakeModel(agent.Response{
		Content: "Hello!", TokensIn: 10, TokensOut: 5,
	})

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
	sessID, err := sm.Create(ctx, "webchat", "user-1", "default")
	if err != nil {
		t.Fatal(err)
	}

	_, err = factory.Run(ctx, sessID, "Hi there", "webchat")
	if err != nil {
		t.Fatal(err)
	}

	// Verify the model received the composed prompt.
	calls := fake.Calls()
	if len(calls) != 1 {
		t.Fatalf("calls = %d, want 1", len(calls))
	}

	messages := calls[0]
	// First message should be system with persona name.
	foundSystem := false
	for _, m := range messages {
		if m.Role == "system" {
			foundSystem = true
			if len(m.Content) == 0 {
				t.Error("system message is empty")
			}
		}
	}
	if !foundSystem {
		t.Error("no system message found in model call")
	}

	// Last message should be the user message.
	last := messages[len(messages)-1]
	if last.Role != "user" || last.Content != "Hi there" {
		t.Errorf("last message = {%s, %q}, want {user, 'Hi there'}", last.Role, last.Content)
	}
}

func TestFactoryTokenTracking(t *testing.T) {
	database := testutil.TestDB(t)
	cfg := testutil.TestConfig()

	fake := testutil.NewFakeModel(agent.Response{
		Content: "OK", TokensIn: 100, TokensOut: 50,
	})

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
	sessID, err := sm.Create(ctx, "webchat", "user-1", "default")
	if err != nil {
		t.Fatal(err)
	}

	_, err = factory.Run(ctx, sessID, "test", "webchat")
	if err != nil {
		t.Fatal(err)
	}

	// Verify token usage was recorded.
	var inputTokens, outputTokens int
	err = database.Conn().QueryRow(
		"SELECT input_tokens, output_tokens FROM token_usage ORDER BY rowid DESC LIMIT 1",
	).Scan(&inputTokens, &outputTokens)
	if err != nil {
		t.Fatalf("query token_usage: %v", err)
	}
	if inputTokens != 100 {
		t.Errorf("recorded input_tokens = %d, want 100", inputTokens)
	}
	if outputTokens != 50 {
		t.Errorf("recorded output_tokens = %d, want 50", outputTokens)
	}
}
