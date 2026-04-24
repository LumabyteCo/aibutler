package model_test

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/LumabyteCo/aibutler/internal/agent"
	"github.com/LumabyteCo/aibutler/internal/capability"
	"github.com/LumabyteCo/aibutler/internal/model"
	"github.com/LumabyteCo/aibutler/internal/prompt"
	"github.com/LumabyteCo/aibutler/internal/session"
	"github.com/LumabyteCo/aibutler/internal/tool"
	"github.com/LumabyteCo/aibutler/testutil"
)

// fakePostProcessor records AfterAgentRun calls.
type fakePostProcessor struct {
	mu          sync.Mutex
	sessionID   string
	userMsg     string
	assistantMsg string
	toolOutputs []agent.ToolOutput
	callCount   int
}

func (p *fakePostProcessor) AfterAgentRun(_ context.Context, sessID, user, assistant string, outputs []agent.ToolOutput) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.callCount++
	p.sessionID = sessID
	p.userMsg = user
	p.assistantMsg = assistant
	p.toolOutputs = outputs
}

func TestFactoryPostProcessorCalled(t *testing.T) {
	database := testutil.TestDB(t)
	cfg := testutil.TestConfig()

	fake := testutil.NewFakeModel(
		agent.Response{
			ToolCalls: []agent.ToolCall{
				{ID: "c1", Name: "task.list", Input: `{}`},
			},
			TokensIn: 20, TokensOut: 10,
		},
		agent.Response{
			Content:  "Here are your tasks.",
			TokensIn: 30, TokensOut: 15,
		},
	)

	sm := session.NewManager(database.Conn(), cfg)
	tracker := prompt.NewTracker(database.Conn(), cfg)
	composer := prompt.NewComposer(cfg, sm, tracker, database.Conn())

	registry := tool.NewRegistry()
	tool.RegisterDataTools(registry, database.Conn())
	dispatcher := tool.NewDispatcher(registry, capability.NewEngine(nil), nil)

	proc := &fakePostProcessor{}

	factory := model.NewFactory(model.FactoryConfig{
		Composer:      composer,
		Model:         fake,
		Tools:         dispatcher,
		Caps:          capability.NewCapabilitySet(capability.MessagingDefaults()),
		Tracker:       tracker,
		DB:            database.Conn(),
		Config:        cfg,
		PostProcessor: proc,
	})

	ctx := context.Background()
	sessID, _ := sm.Create(ctx, "webchat", "user-1", "default")

	result, err := factory.Run(ctx, sessID, "show my tasks", "webchat")
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if result.Status != agent.StateCompleted {
		t.Fatalf("status = %s", result.Status)
	}

	// PostProcessor should have been called.
	proc.mu.Lock()
	defer proc.mu.Unlock()
	if proc.callCount != 1 {
		t.Fatalf("postProcessor calls = %d, want 1", proc.callCount)
	}
	if proc.sessionID != sessID {
		t.Errorf("sessionID = %q, want %q", proc.sessionID, sessID)
	}
	if proc.userMsg != "show my tasks" {
		t.Errorf("userMsg = %q", proc.userMsg)
	}
	if proc.assistantMsg != "Here are your tasks." {
		t.Errorf("assistantMsg = %q", proc.assistantMsg)
	}
	if len(proc.toolOutputs) != 1 {
		t.Fatalf("toolOutputs = %d, want 1", len(proc.toolOutputs))
	}
	if proc.toolOutputs[0].ToolName != "task.list" {
		t.Errorf("toolName = %q, want task.list", proc.toolOutputs[0].ToolName)
	}
}

func TestFactoryPostProcessorNilIsOK(t *testing.T) {
	database := testutil.TestDB(t)
	cfg := testutil.TestConfig()

	fake := testutil.NewFakeModel(agent.Response{
		Content: "hi", TokensIn: 5, TokensOut: 3,
	})

	sm := session.NewManager(database.Conn(), cfg)
	tracker := prompt.NewTracker(database.Conn(), cfg)
	composer := prompt.NewComposer(cfg, sm, tracker, database.Conn())

	// No PostProcessor — should not panic.
	factory := model.NewFactory(model.FactoryConfig{
		Composer: composer,
		Model:    fake,
		Tracker:  tracker,
		DB:       database.Conn(),
		Config:   cfg,
	})

	ctx := context.Background()
	sessID, _ := sm.Create(ctx, "webchat", "user-1", "default")

	_, err := factory.Run(ctx, sessID, "hello", "webchat")
	if err != nil {
		t.Fatalf("run: %v", err)
	}
}

func TestFactoryCustomRoleToolFiltering(t *testing.T) {
	database := testutil.TestDB(t)
	cfg := testutil.TestConfig()
	cfg.Settings.AgentMode = "custom"

	fake := testutil.NewFakeModel(agent.Response{
		Content: "done", TokensIn: 10, TokensOut: 5,
	})

	sm := session.NewManager(database.Conn(), cfg)
	tracker := prompt.NewTracker(database.Conn(), cfg)
	composer := prompt.NewComposer(cfg, sm, tracker, database.Conn())

	// Register multiple tools.
	registry := tool.NewRegistry()
	registry.Register(&tool.FuncTool{
		ToolName: "task.add", ToolDesc: "Add task", ToolSchema: `{}`, ToolCap: "data.tasks.write",
		Exec: func(_ context.Context, _ string) (string, error) { return "ok", nil },
	})
	registry.Register(&tool.FuncTool{
		ToolName: "task.list", ToolDesc: "List tasks", ToolSchema: `{}`, ToolCap: "data.tasks.read",
		Exec: func(_ context.Context, _ string) (string, error) { return "ok", nil },
	})
	registry.Register(&tool.FuncTool{
		ToolName: "shell.exec", ToolDesc: "Execute shell", ToolSchema: `{}`, ToolCap: "tool.shell.exec",
		Exec: func(_ context.Context, _ string) (string, error) { return "ok", nil },
	})
	dispatcher := tool.NewDispatcher(registry, capability.NewEngine(nil), nil)

	// Create role with Tools allowlist: only task.list allowed.
	roles := []agent.CustomRole{
		{
			Name:        "reader",
			Description: "Read-only tasks",
			Tools:       []string{"task.list"},
			Prompt:      "You are a read-only assistant.",
		},
	}
	router := agent.NewRoleRouter(roles, "explicit", nil)

	factory := model.NewFactory(model.FactoryConfig{
		Composer:   composer,
		Model:      fake,
		Tools:      dispatcher,
		Caps:       capability.NewCapabilitySet(capability.MessagingDefaults()),
		Tracker:    tracker,
		DB:         database.Conn(),
		Config:     cfg,
		RoleRouter: router,
	})

	ctx := context.Background()
	sessID, _ := sm.Create(ctx, "webchat", "user-1", "default")

	_, err := factory.Run(ctx, sessID, "@reader show my tasks", "webchat")
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	// Verify model received the role prompt and filtered tools.
	calls := fake.Calls()
	if len(calls) < 1 {
		t.Fatal("expected at least 1 model call")
	}

	// Check system message contains role prompt.
	systemMsg := calls[0][0]
	if systemMsg.Role != "system" {
		t.Fatalf("first message role = %s, want system", systemMsg.Role)
	}
	if !strings.Contains(systemMsg.Content, "read-only assistant") {
		t.Errorf("system prompt should contain role prompt")
	}
}

func TestFactoryNoRoleFilteringWithEmptyTools(t *testing.T) {
	database := testutil.TestDB(t)
	cfg := testutil.TestConfig()
	cfg.Settings.AgentMode = "custom"

	fake := testutil.NewFakeModel(agent.Response{
		Content: "done", TokensIn: 10, TokensOut: 5,
	})

	sm := session.NewManager(database.Conn(), cfg)
	tracker := prompt.NewTracker(database.Conn(), cfg)
	composer := prompt.NewComposer(cfg, sm, tracker, database.Conn())

	// Role with empty Tools (should allow all tools).
	roles := []agent.CustomRole{
		{Name: "general", Description: "General", Tools: nil, Prompt: "You are helpful."},
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

	// Should work without errors when Tools is nil/empty.
	_, err := factory.Run(ctx, sessID, "@general hello", "webchat")
	if err != nil {
		t.Fatalf("run: %v", err)
	}
}
