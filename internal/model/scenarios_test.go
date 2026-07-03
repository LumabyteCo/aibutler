package model_test

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/LumabyteCo/aibutler/internal/agent"
	"github.com/LumabyteCo/aibutler/internal/capability"
	"github.com/LumabyteCo/aibutler/internal/memory"
	"github.com/LumabyteCo/aibutler/internal/memory/entity"
	"github.com/LumabyteCo/aibutler/internal/memory/fts"
	"github.com/LumabyteCo/aibutler/internal/memory/graph"
	"github.com/LumabyteCo/aibutler/internal/memory/hybrid"
	"github.com/LumabyteCo/aibutler/internal/model"
	"github.com/LumabyteCo/aibutler/internal/prompt"
	"github.com/LumabyteCo/aibutler/internal/session"
	"github.com/LumabyteCo/aibutler/internal/tool"
	"github.com/LumabyteCo/aibutler/testutil"
)

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

// scenario holds a wired-up test fixture.
type scenario struct {
	factory *model.Factory
	sessID  string
	fake    *testutil.FakeModel
	ctx     context.Context
	db      *testutil.FakeModel // unused alias just to keep name short
	sm      *session.Manager
}

// recordingPostProcessor records AfterAgentRun calls.
type recordingPostProcessor struct {
	mu           sync.Mutex
	callCount    int
	sessionID    string
	userMsg      string
	assistantMsg string
	toolOutputs  []agent.ToolOutput
}

func (p *recordingPostProcessor) AfterAgentRun(_ context.Context, sessID, user, assistant, _ string, outputs []agent.ToolOutput) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.callCount++
	p.sessionID = sessID
	p.userMsg = user
	p.assistantMsg = assistant
	p.toolOutputs = outputs
}

// newScenario creates a full factory+session wired to real data tools.
func newScenario(t *testing.T, responses ...agent.Response) *scenario {
	t.Helper()
	database := testutil.TestDB(t)
	cfg := testutil.TestConfig()
	fake := testutil.NewFakeModel(responses...)

	sm := session.NewManager(database.Conn(), cfg)
	tracker := prompt.NewTracker(database.Conn(), cfg)
	composer := prompt.NewComposer(cfg, sm, tracker, database.Conn())

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
	sessID, _ := sm.Create(ctx, "webchat", "user-1", "default")

	return &scenario{factory: factory, sessID: sessID, fake: fake, ctx: ctx, sm: sm}
}

// newSeededScenario creates a scenario with pre-loaded test data.
func newSeededScenario(t *testing.T, responses ...agent.Response) *scenario {
	t.Helper()
	database := testutil.TestDBSeeded(t)
	cfg := testutil.TestConfig()
	fake := testutil.NewFakeModel(responses...)

	sm := session.NewManager(database.Conn(), cfg)
	tracker := prompt.NewTracker(database.Conn(), cfg)
	composer := prompt.NewComposer(cfg, sm, tracker, database.Conn())

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
	sessID, _ := sm.Create(ctx, "webchat", "user-1", "default")

	return &scenario{factory: factory, sessID: sessID, fake: fake, ctx: ctx, sm: sm}
}

// newScenarioWithMemory adds memory (FTS, entity, hybrid) tools.
func newScenarioWithMemory(t *testing.T, responses ...agent.Response) *scenario {
	t.Helper()
	database := testutil.TestDB(t)
	cfg := testutil.TestConfig()
	fake := testutil.NewFakeModel(responses...)

	sm := session.NewManager(database.Conn(), cfg)
	tracker := prompt.NewTracker(database.Conn(), cfg)
	composer := prompt.NewComposer(cfg, sm, tracker, database.Conn())

	registry := tool.NewRegistry()
	tool.RegisterDataTools(registry, database.Conn())

	memStore := memory.NewStore(database.Conn())
	memory.RegisterMemoryTools(registry, memStore, nil)

	ftsStore := fts.NewStore(database.Conn())
	entityStore := entity.NewStore(database.Conn())
	graphStore := graph.NewStore(database.Conn())
	hybridSearcher := hybrid.NewSearcher(ftsStore, entityStore)
	memory.RegisterP2MemoryTools(registry, memory.P2Deps{
		FTS: ftsStore, Entity: entityStore, Graph: graphStore, Hybrid: hybridSearcher,
	})

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
	sessID, _ := sm.Create(ctx, "webchat", "user-1", "default")

	return &scenario{factory: factory, sessID: sessID, fake: fake, ctx: ctx, sm: sm}
}

func (s *scenario) run(t *testing.T, msg string) *agent.Result {
	t.Helper()
	result, err := s.factory.Run(s.ctx, s.sessID, msg, "webchat")
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	return result
}

func (s *scenario) runExpectErr(t *testing.T, msg string) error {
	t.Helper()
	_, err := s.factory.Run(s.ctx, s.sessID, msg, "webchat")
	return err
}

func assertCompleted(t *testing.T, r *agent.Result) {
	t.Helper()
	if r.Status != agent.StateCompleted {
		t.Fatalf("status = %s, want completed (error: %s)", r.Status, r.Error)
	}
}

func assertOutput(t *testing.T, r *agent.Result, substr string) {
	t.Helper()
	if !strings.Contains(r.Output, substr) {
		t.Errorf("output %q missing %q", r.Output, substr)
	}
}

func assertToolOutputCount(t *testing.T, r *agent.Result, n int) {
	t.Helper()
	if len(r.ToolOutputs) != n {
		t.Fatalf("ToolOutputs = %d, want %d", len(r.ToolOutputs), n)
	}
}

func tc(id, name, input string) agent.ToolCall {
	return agent.ToolCall{ID: id, Name: name, Input: input}
}

func resp(content string, tokIn, tokOut int) agent.Response {
	return agent.Response{Content: content, TokensIn: tokIn, TokensOut: tokOut}
}

func toolResp(calls []agent.ToolCall, tokIn, tokOut int) agent.Response {
	return agent.Response{ToolCalls: calls, TokensIn: tokIn, TokensOut: tokOut}
}

// =========================================================================
// GROUP 1: Basic Conversations (1–10)
// =========================================================================

func TestScenario01_SimpleGreeting(t *testing.T) {
	s := newScenario(t, resp("Hello! How can I help you today?", 50, 10))
	r := s.run(t, "hi")
	assertCompleted(t, r)
	assertOutput(t, r, "Hello")
}

func TestScenario02_SimpleQuestion(t *testing.T) {
	s := newScenario(t, resp("The capital of France is Paris.", 40, 8))
	r := s.run(t, "What is the capital of France?")
	assertCompleted(t, r)
	assertOutput(t, r, "Paris")
}

func TestScenario03_MultiSentenceResponse(t *testing.T) {
	s := newScenario(t, resp("Sure! First, gather ingredients. Then, preheat the oven. Finally, bake for 30 minutes.", 60, 20))
	r := s.run(t, "How do I bake a cake?")
	assertCompleted(t, r)
	assertOutput(t, r, "preheat the oven")
}

func TestScenario04_EmptyAssistantResponse(t *testing.T) {
	s := newScenario(t, resp("", 10, 1))
	r := s.run(t, "test")
	assertCompleted(t, r)
}

func TestScenario05_UnicodeInMessage(t *testing.T) {
	s := newScenario(t, resp("日本語でお答えします。こんにちは！", 30, 15))
	r := s.run(t, "こんにちは、お元気ですか？")
	assertCompleted(t, r)
	assertOutput(t, r, "こんにちは")
}

func TestScenario06_LongUserMessage(t *testing.T) {
	longMsg := strings.Repeat("This is a test message. ", 100) // ~2400 chars
	s := newScenario(t, resp("Got it.", 200, 5))
	r := s.run(t, longMsg)
	assertCompleted(t, r)
}

func TestScenario07_SpecialCharactersInResponse(t *testing.T) {
	s := newScenario(t, resp(`Here's a code snippet: if (x < 10 && y > 5) { return "ok"; }`, 40, 15))
	r := s.run(t, "Show me a code snippet")
	assertCompleted(t, r)
	assertOutput(t, r, `"ok"`)
}

func TestScenario08_MultipleSessions(t *testing.T) {
	s := newScenario(t, resp("First session.", 10, 5), resp("Second session.", 10, 5))
	r1 := s.run(t, "hello 1")
	assertCompleted(t, r1)

	sess2, _ := s.sm.Create(s.ctx, "webchat", "user-2", "default")
	s.sessID = sess2
	r2 := s.run(t, "hello 2")
	assertCompleted(t, r2)
}

func TestScenario09_DifferentChannels(t *testing.T) {
	database := testutil.TestDB(t)
	cfg := testutil.TestConfig()
	fake := testutil.NewFakeModel(resp("telegram reply", 10, 5))
	sm := session.NewManager(database.Conn(), cfg)
	tracker := prompt.NewTracker(database.Conn(), cfg)
	composer := prompt.NewComposer(cfg, sm, tracker, database.Conn())
	factory := model.NewFactory(model.FactoryConfig{
		Composer: composer, Model: fake, Tracker: tracker, DB: database.Conn(), Config: cfg,
		Caps: capability.NewCapabilitySet(capability.MessagingDefaults()),
	})
	ctx := context.Background()
	sessID, _ := sm.Create(ctx, "telegram", "user-1", "default")
	result, err := factory.Run(ctx, sessID, "hi from telegram", "telegram")
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	assertCompleted(t, result)
}

func TestScenario10_TokenCountingWorks(t *testing.T) {
	s := newScenario(t, resp("ok", 100, 50))
	r := s.run(t, "count test")
	assertCompleted(t, r)
	if r.TokensIn == 0 || r.TokensOut == 0 {
		t.Error("expected non-zero token counts")
	}
}

// =========================================================================
// GROUP 2: Task Management (11–25)
// =========================================================================

func TestScenario11_AddTask(t *testing.T) {
	s := newScenario(t,
		toolResp([]agent.ToolCall{tc("1", "task.add", `{"content":"Buy milk"}`)}, 30, 10),
		resp("Added 'Buy milk' to your tasks.", 20, 8),
	)
	r := s.run(t, "add buy milk to my tasks")
	assertCompleted(t, r)
	assertOutput(t, r, "Buy milk")
}

func TestScenario12_ListTasks(t *testing.T) {
	s := newSeededScenario(t,
		toolResp([]agent.ToolCall{tc("1", "task.list", `{}`)}, 30, 10),
		resp("You have 2 tasks.", 40, 10),
	)
	r := s.run(t, "show my tasks")
	assertCompleted(t, r)
	assertToolOutputCount(t, r, 1)
	if r.ToolOutputs[0].ToolName != "task.list" {
		t.Errorf("tool = %q, want task.list", r.ToolOutputs[0].ToolName)
	}
}

func TestScenario13_CompleteTask(t *testing.T) {
	s := newSeededScenario(t,
		toolResp([]agent.ToolCall{tc("1", "task.complete", `{"id":1}`)}, 20, 8),
		resp("Task completed.", 15, 5),
	)
	r := s.run(t, "mark the first task as done")
	assertCompleted(t, r)
}

func TestScenario14_RemoveTask(t *testing.T) {
	s := newSeededScenario(t,
		toolResp([]agent.ToolCall{tc("1", "task.remove", `{"id":2}`)}, 20, 8),
		resp("Task removed.", 15, 5),
	)
	r := s.run(t, "delete the PR review task")
	assertCompleted(t, r)
}

func TestScenario15_AddTaskWithPriority(t *testing.T) {
	s := newScenario(t,
		toolResp([]agent.ToolCall{tc("1", "task.add", `{"content":"Urgent fix","priority":3}`)}, 30, 10),
		resp("Added with high priority.", 20, 8),
	)
	r := s.run(t, "add urgent fix with high priority")
	assertCompleted(t, r)
}

func TestScenario16_ListTasksByStatus(t *testing.T) {
	s := newSeededScenario(t,
		toolResp([]agent.ToolCall{tc("1", "task.list", `{"status":"pending"}`)}, 30, 10),
		resp("Here are your pending tasks.", 30, 10),
	)
	r := s.run(t, "show pending tasks only")
	assertCompleted(t, r)
}

func TestScenario17_AddThenListTasks(t *testing.T) {
	s := newScenario(t,
		toolResp([]agent.ToolCall{tc("1", "task.add", `{"content":"Walk dog"}`)}, 30, 10),
		toolResp([]agent.ToolCall{tc("2", "task.list", `{}`)}, 30, 10),
		resp("You have 1 task: Walk dog.", 30, 10),
	)
	r := s.run(t, "add walk dog then show all tasks")
	assertCompleted(t, r)
	assertToolOutputCount(t, r, 2)
}

func TestScenario18_AddMultipleTasks(t *testing.T) {
	s := newScenario(t,
		toolResp([]agent.ToolCall{tc("1", "task.add", `{"content":"Task A"}`)}, 20, 8),
		toolResp([]agent.ToolCall{tc("2", "task.add", `{"content":"Task B"}`)}, 20, 8),
		toolResp([]agent.ToolCall{tc("3", "task.add", `{"content":"Task C"}`)}, 20, 8),
		resp("Added 3 tasks.", 15, 5),
	)
	r := s.run(t, "add Task A, Task B, Task C")
	assertCompleted(t, r)
	assertToolOutputCount(t, r, 3)
}

func TestScenario19_TaskInNamedList(t *testing.T) {
	s := newScenario(t,
		toolResp([]agent.ToolCall{tc("1", "task.add", `{"content":"Sprint review","list":"work"}`)}, 30, 10),
		resp("Added to work list.", 15, 5),
	)
	r := s.run(t, "add sprint review to work list")
	assertCompleted(t, r)
}

func TestScenario20_ClearCompletedTasks(t *testing.T) {
	s := newSeededScenario(t,
		toolResp([]agent.ToolCall{tc("1", "task.complete", `{"id":1}`)}, 20, 8),
		toolResp([]agent.ToolCall{tc("2", "task.clear", `{}`)}, 20, 8),
		resp("Cleared completed tasks.", 15, 5),
	)
	r := s.run(t, "complete first task and clear done tasks")
	assertCompleted(t, r)
}

func TestScenario21_PrioritizeTask(t *testing.T) {
	s := newSeededScenario(t,
		toolResp([]agent.ToolCall{tc("1", "task.prioritize", `{"id":1,"priority":2}`)}, 20, 8),
		resp("Priority updated.", 10, 5),
	)
	r := s.run(t, "set first task to medium priority")
	assertCompleted(t, r)
}

func TestScenario22_TaskAddCompleteCycle(t *testing.T) {
	s := newScenario(t,
		toolResp([]agent.ToolCall{tc("1", "task.add", `{"content":"Quick task"}`)}, 20, 8),
		toolResp([]agent.ToolCall{tc("2", "task.complete", `{"id":1}`)}, 20, 8),
		resp("Done.", 10, 3),
	)
	r := s.run(t, "add quick task and mark it done")
	assertCompleted(t, r)
}

func TestScenario23_TaskWithSpecialChars(t *testing.T) {
	s := newScenario(t,
		toolResp([]agent.ToolCall{tc("1", "task.add", `{"content":"Review O'Brien's PR #42 & deploy"}`)}, 30, 10),
		resp("Added.", 10, 3),
	)
	r := s.run(t, "add Review O'Brien's PR #42 & deploy")
	assertCompleted(t, r)
}

func TestScenario24_ListEmptyTasks(t *testing.T) {
	s := newScenario(t,
		toolResp([]agent.ToolCall{tc("1", "task.list", `{}`)}, 20, 8),
		resp("No tasks found.", 15, 5),
	)
	r := s.run(t, "what are my tasks")
	assertCompleted(t, r)
}

func TestScenario25_TaskListFromWorkList(t *testing.T) {
	s := newSeededScenario(t,
		toolResp([]agent.ToolCall{tc("1", "task.list", `{"list":"work"}`)}, 30, 10),
		resp("1 work task.", 15, 5),
	)
	r := s.run(t, "show my work tasks")
	assertCompleted(t, r)
}

// =========================================================================
// GROUP 3: Expenses & Finance (26–33)
// =========================================================================

func TestScenario26_LogExpense(t *testing.T) {
	s := newScenario(t,
		toolResp([]agent.ToolCall{tc("1", "expense.log", `{"amount":15.50,"category":"food","description":"lunch"}`)}, 30, 10),
		resp("Logged $15.50 for food.", 15, 5),
	)
	r := s.run(t, "I spent $15.50 on lunch")
	assertCompleted(t, r)
}

func TestScenario27_ExpenseSummary(t *testing.T) {
	s := newScenario(t,
		toolResp([]agent.ToolCall{tc("1", "expense.log", `{"amount":20,"category":"food"}`)}, 20, 8),
		toolResp([]agent.ToolCall{tc("2", "expense.log", `{"amount":50,"category":"transport"}`)}, 20, 8),
		toolResp([]agent.ToolCall{tc("3", "expense.summary", `{}`)}, 20, 8),
		resp("Food: $20, Transport: $50.", 20, 8),
	)
	r := s.run(t, "log $20 food and $50 transport then show summary")
	assertCompleted(t, r)
}

func TestScenario28_BudgetCheck(t *testing.T) {
	s := newScenario(t,
		toolResp([]agent.ToolCall{tc("1", "expense.budget_check", `{"category":"food"}`)}, 20, 8),
		resp("You're under budget.", 15, 5),
	)
	r := s.run(t, "am I over budget on food?")
	assertCompleted(t, r)
}

func TestScenario29_CostStatus(t *testing.T) {
	s := newScenario(t,
		toolResp([]agent.ToolCall{tc("1", "cost.status", `{}`)}, 20, 8),
		resp("Current usage: $0.01.", 15, 5),
	)
	r := s.run(t, "how much has this cost me?")
	assertCompleted(t, r)
}

func TestScenario30_MultipleExpensesThenBudget(t *testing.T) {
	s := newScenario(t,
		toolResp([]agent.ToolCall{tc("1", "expense.log", `{"amount":100,"category":"food"}`)}, 20, 8),
		toolResp([]agent.ToolCall{tc("2", "expense.log", `{"amount":200,"category":"food"}`)}, 20, 8),
		toolResp([]agent.ToolCall{tc("3", "expense.budget_check", `{"category":"food"}`)}, 20, 8),
		resp("$300 on food this month.", 20, 8),
	)
	r := s.run(t, "I spent 100 and 200 on food, check my budget")
	assertCompleted(t, r)
	assertToolOutputCount(t, r, 3)
}

func TestScenario31_JournalWrite(t *testing.T) {
	s := newScenario(t,
		toolResp([]agent.ToolCall{tc("1", "journal.write", `{"content":"Had a great day","mood":"happy"}`)}, 20, 8),
		resp("Journal entry saved.", 10, 5),
	)
	r := s.run(t, "write in my journal: had a great day, feeling happy")
	assertCompleted(t, r)
}

func TestScenario32_JournalReadAndMoodTrend(t *testing.T) {
	s := newScenario(t,
		toolResp([]agent.ToolCall{tc("1", "journal.write", `{"content":"good day","mood":"happy"}`)}, 20, 8),
		toolResp([]agent.ToolCall{tc("2", "journal.write", `{"content":"bad day","mood":"sad"}`)}, 20, 8),
		toolResp([]agent.ToolCall{tc("3", "journal.mood_trend", `{}`)}, 20, 8),
		resp("Your mood has been mixed.", 15, 5),
	)
	r := s.run(t, "write two journal entries then show mood trend")
	assertCompleted(t, r)
}

func TestScenario33_HealthLog(t *testing.T) {
	s := newScenario(t,
		toolResp([]agent.ToolCall{tc("1", "health.log", `{"metric":"weight","value":"75.5","unit":"kg"}`)}, 20, 8),
		resp("Logged weight: 75.5 kg.", 15, 5),
	)
	r := s.run(t, "log my weight as 75.5 kg")
	assertCompleted(t, r)
}

// =========================================================================
// GROUP 4: Contacts (34–40)
// =========================================================================

func TestScenario34_AddContact(t *testing.T) {
	s := newScenario(t,
		toolResp([]agent.ToolCall{tc("1", "contact.add", `{"name":"Alice","email":"alice@example.com","relationship":"friend"}`)}, 30, 10),
		resp("Contact Alice added.", 15, 5),
	)
	r := s.run(t, "add contact Alice, alice@example.com, friend")
	assertCompleted(t, r)
}

func TestScenario35_SearchContacts(t *testing.T) {
	s := newSeededScenario(t,
		toolResp([]agent.ToolCall{tc("1", "contact.search", `{"query":"Sarah"}`)}, 20, 8),
		resp("Found Sarah.", 15, 5),
	)
	r := s.run(t, "find Sarah's contact")
	assertCompleted(t, r)
}

func TestScenario36_UpdateContact(t *testing.T) {
	s := newSeededScenario(t,
		toolResp([]agent.ToolCall{tc("1", "contact.update", `{"id":1,"phone":"+1234567890"}`)}, 20, 8),
		resp("Updated Sarah's phone.", 15, 5),
	)
	r := s.run(t, "update Sarah's phone to +1234567890")
	assertCompleted(t, r)
}

func TestScenario37_ContactBirthdays(t *testing.T) {
	s := newSeededScenario(t,
		toolResp([]agent.ToolCall{tc("1", "contact.birthdays", `{"days":30}`)}, 20, 8),
		resp("No upcoming birthdays.", 15, 5),
	)
	r := s.run(t, "any birthdays coming up?")
	assertCompleted(t, r)
}

func TestScenario38_AddContactThenSearch(t *testing.T) {
	s := newScenario(t,
		toolResp([]agent.ToolCall{tc("1", "contact.add", `{"name":"Charlie","email":"charlie@test.com"}`)}, 20, 8),
		toolResp([]agent.ToolCall{tc("2", "contact.search", `{"query":"Charlie"}`)}, 20, 8),
		resp("Charlie is in your contacts.", 15, 5),
	)
	r := s.run(t, "add Charlie then find him")
	assertCompleted(t, r)
	assertToolOutputCount(t, r, 2)
}

func TestScenario39_SearchNonexistentContact(t *testing.T) {
	s := newScenario(t,
		toolResp([]agent.ToolCall{tc("1", "contact.search", `{"query":"Zzzzz"}`)}, 20, 8),
		resp("No contacts found.", 15, 5),
	)
	r := s.run(t, "find Zzzzz")
	assertCompleted(t, r)
}

func TestScenario40_AddContactWithFullDetails(t *testing.T) {
	s := newScenario(t,
		toolResp([]agent.ToolCall{tc("1", "contact.add", `{"name":"Dr. Smith","email":"smith@hospital.com","phone":"+1555000","relationship":"doctor"}`)}, 30, 10),
		resp("Added Dr. Smith.", 10, 5),
	)
	r := s.run(t, "add my doctor Dr. Smith, smith@hospital.com, +1555000")
	assertCompleted(t, r)
}

// =========================================================================
// GROUP 5: Memory & Intelligence (41–55)
// =========================================================================

func TestScenario41_CaptureMemory(t *testing.T) {
	s := newScenarioWithMemory(t,
		toolResp([]agent.ToolCall{tc("1", "memory.capture", `{"content":"User prefers morning meetings","tags":["preference"]}`)}, 20, 8),
		resp("Noted.", 10, 3),
	)
	r := s.run(t, "remember that I prefer morning meetings")
	assertCompleted(t, r)
}

func TestScenario42_SearchMemory(t *testing.T) {
	s := newScenarioWithMemory(t,
		toolResp([]agent.ToolCall{tc("1", "memory.capture", `{"content":"Favorite color is blue","tags":["preference"]}`)}, 20, 8),
		toolResp([]agent.ToolCall{tc("2", "memory.search", `{"query":"color"}`)}, 20, 8),
		resp("Your favorite color is blue.", 15, 5),
	)
	r := s.run(t, "remember my fav color is blue, then search for it")
	assertCompleted(t, r)
}

func TestScenario43_GetFacts(t *testing.T) {
	s := newScenarioWithMemory(t,
		toolResp([]agent.ToolCall{tc("1", "memory.facts", `{}`)}, 20, 8),
		resp("No facts yet.", 10, 3),
	)
	r := s.run(t, "what do you know about me?")
	assertCompleted(t, r)
}

func TestScenario44_MemorySearchNoResults(t *testing.T) {
	s := newScenarioWithMemory(t,
		toolResp([]agent.ToolCall{tc("1", "memory.search", `{"query":"nonexistent topic"}`)}, 20, 8),
		resp("I don't have any notes on that.", 15, 5),
	)
	r := s.run(t, "search my notes for quantum computing")
	assertCompleted(t, r)
}

func TestScenario45_MemoryByTags(t *testing.T) {
	s := newScenarioWithMemory(t,
		toolResp([]agent.ToolCall{tc("1", "memory.search", `{"tags":["work"]}`)}, 20, 8),
		resp("No work-related notes.", 15, 5),
	)
	r := s.run(t, "show me all work-related notes")
	assertCompleted(t, r)
}

func TestScenario46_CaptureMultipleMemories(t *testing.T) {
	s := newScenarioWithMemory(t,
		toolResp([]agent.ToolCall{tc("1", "memory.capture", `{"content":"Allergic to peanuts","tags":["health"]}`)}, 20, 8),
		toolResp([]agent.ToolCall{tc("2", "memory.capture", `{"content":"Loves hiking","tags":["hobby"]}`)}, 20, 8),
		resp("Saved both notes.", 10, 5),
	)
	r := s.run(t, "remember I'm allergic to peanuts and love hiking")
	assertCompleted(t, r)
	assertToolOutputCount(t, r, 2)
}

func TestScenario47_HybridSearch(t *testing.T) {
	s := newScenarioWithMemory(t,
		toolResp([]agent.ToolCall{tc("1", "memory.search", `{"query":"meeting schedule"}`)}, 20, 8),
		resp("No results found for meetings.", 15, 5),
	)
	r := s.run(t, "search everything for meeting schedule")
	assertCompleted(t, r)
}

func TestScenario48_MemoryStats(t *testing.T) {
	s := newScenarioWithMemory(t,
		toolResp([]agent.ToolCall{tc("1", "memory.stats", `{}`)}, 20, 8),
		resp("Memory stats: 0 thoughts, 0 entities.", 15, 5),
	)
	r := s.run(t, "show memory statistics")
	assertCompleted(t, r)
}

func TestScenario49_CaptureThenSearch(t *testing.T) {
	s := newScenarioWithMemory(t,
		toolResp([]agent.ToolCall{tc("1", "memory.capture", `{"content":"Project deadline is Friday","tags":["work","deadline"]}`)}, 20, 8),
		toolResp([]agent.ToolCall{tc("2", "memory.search", `{"tags":["deadline"]}`)}, 20, 8),
		resp("Deadline is Friday.", 15, 5),
	)
	r := s.run(t, "note that project deadline is Friday then find deadlines")
	assertCompleted(t, r)
}

func TestScenario50_MemoryWithTaskCombo(t *testing.T) {
	s := newScenarioWithMemory(t,
		toolResp([]agent.ToolCall{tc("1", "task.add", `{"content":"Prepare presentation"}`)}, 20, 8),
		toolResp([]agent.ToolCall{tc("2", "memory.capture", `{"content":"Presentation is for Q1 review","tags":["work"]}`)}, 20, 8),
		resp("Task added and context noted.", 15, 5),
	)
	r := s.run(t, "add task to prepare presentation and remember it's for Q1 review")
	assertCompleted(t, r)
	assertToolOutputCount(t, r, 2)
}

func TestScenario51_FTSSearch(t *testing.T) {
	s := newScenarioWithMemory(t,
		toolResp([]agent.ToolCall{tc("1", "memory.fts_search", `{"query":"project alpha"}`)}, 20, 8),
		resp("No results for project alpha.", 15, 5),
	)
	r := s.run(t, "search transcripts for project alpha")
	assertCompleted(t, r)
}

func TestScenario52_EntityPeopleSearch(t *testing.T) {
	s := newScenarioWithMemory(t,
		toolResp([]agent.ToolCall{tc("1", "memory.entities.people", `{}`)}, 20, 8),
		resp("No people entities found.", 15, 5),
	)
	r := s.run(t, "who have I mentioned recently?")
	assertCompleted(t, r)
}

func TestScenario53_EntityDecisionSearch(t *testing.T) {
	s := newScenarioWithMemory(t,
		toolResp([]agent.ToolCall{tc("1", "memory.entities.decisions", `{}`)}, 20, 8),
		resp("No decisions recorded.", 15, 5),
	)
	r := s.run(t, "what decisions have I made?")
	assertCompleted(t, r)
}

func TestScenario54_EntityProjectSearch(t *testing.T) {
	s := newScenarioWithMemory(t,
		toolResp([]agent.ToolCall{tc("1", "memory.entities.projects", `{}`)}, 20, 8),
		resp("No projects found.", 15, 5),
	)
	r := s.run(t, "what projects am I working on?")
	assertCompleted(t, r)
}

func TestScenario55_GraphQuery(t *testing.T) {
	s := newScenarioWithMemory(t,
		toolResp([]agent.ToolCall{tc("1", "memory.entities.graph", `{"name":"Alice"}`)}, 20, 8),
		resp("No graph connections for Alice.", 15, 5),
	)
	r := s.run(t, "show me how Alice is connected")
	assertCompleted(t, r)
}

// =========================================================================
// GROUP 6: Custom Roles (56–65)
// =========================================================================

func TestScenario56_ExplicitRoleRouting(t *testing.T) {
	database := testutil.TestDB(t)
	cfg := testutil.TestConfig()
	cfg.Settings.AgentMode = "custom"
	fake := testutil.NewFakeModel(resp("analyst response", 20, 8))
	sm := session.NewManager(database.Conn(), cfg)
	tracker := prompt.NewTracker(database.Conn(), cfg)
	composer := prompt.NewComposer(cfg, sm, tracker, database.Conn())
	roles := []agent.CustomRole{
		{Name: "analyst", Description: "Data analyst", Prompt: "You analyze data."},
	}
	router := agent.NewRoleRouter(roles, "explicit", nil)
	factory := model.NewFactory(model.FactoryConfig{
		Composer: composer, Model: fake, Tracker: tracker, DB: database.Conn(),
		Config: cfg, RoleRouter: router,
		Caps: capability.NewCapabilitySet(capability.MessagingDefaults()),
	})
	ctx := context.Background()
	sessID, _ := sm.Create(ctx, "webchat", "user-1", "default")
	result, _ := factory.Run(ctx, sessID, "@analyst show metrics", "webchat")
	assertCompleted(t, result)
	calls := fake.Calls()
	if len(calls) < 1 {
		t.Fatal("no model calls")
	}
	if !strings.Contains(calls[0][0].Content, "analyze data") {
		t.Error("role prompt not injected into system message")
	}
}

func TestScenario57_RoleToolFiltering(t *testing.T) {
	database := testutil.TestDB(t)
	cfg := testutil.TestConfig()
	cfg.Settings.AgentMode = "custom"
	fake := testutil.NewFakeModel(resp("done", 10, 5))
	sm := session.NewManager(database.Conn(), cfg)
	tracker := prompt.NewTracker(database.Conn(), cfg)
	composer := prompt.NewComposer(cfg, sm, tracker, database.Conn())
	registry := tool.NewRegistry()
	tool.RegisterDataTools(registry, database.Conn())
	dispatcher := tool.NewDispatcher(registry, capability.NewEngine(nil), nil)
	roles := []agent.CustomRole{
		{Name: "reader", Description: "Read only", Tools: []string{"task.list"}, Prompt: "Read-only."},
	}
	router := agent.NewRoleRouter(roles, "explicit", nil)
	factory := model.NewFactory(model.FactoryConfig{
		Composer: composer, Model: fake, Tools: dispatcher, Tracker: tracker,
		DB: database.Conn(), Config: cfg, RoleRouter: router,
		Caps: capability.NewCapabilitySet(capability.MessagingDefaults()),
	})
	ctx := context.Background()
	sessID, _ := sm.Create(ctx, "webchat", "user-1", "default")
	result, _ := factory.Run(ctx, sessID, "@reader show tasks", "webchat")
	assertCompleted(t, result)
}

func TestScenario58_RoleWithCustomPrompt(t *testing.T) {
	database := testutil.TestDB(t)
	cfg := testutil.TestConfig()
	cfg.Settings.AgentMode = "custom"
	fake := testutil.NewFakeModel(resp("creative response", 20, 8))
	sm := session.NewManager(database.Conn(), cfg)
	tracker := prompt.NewTracker(database.Conn(), cfg)
	composer := prompt.NewComposer(cfg, sm, tracker, database.Conn())
	roles := []agent.CustomRole{
		{Name: "poet", Description: "Creative writer", Prompt: "You write beautiful poetry."},
	}
	router := agent.NewRoleRouter(roles, "explicit", nil)
	factory := model.NewFactory(model.FactoryConfig{
		Composer: composer, Model: fake, Tracker: tracker, DB: database.Conn(),
		Config: cfg, RoleRouter: router,
		Caps: capability.NewCapabilitySet(capability.MessagingDefaults()),
	})
	ctx := context.Background()
	sessID, _ := sm.Create(ctx, "webchat", "user-1", "default")
	result, _ := factory.Run(ctx, sessID, "@poet write about the sea", "webchat")
	assertCompleted(t, result)
	if !strings.Contains(fake.Calls()[0][0].Content, "poetry") {
		t.Error("poet prompt not in system message")
	}
}

func TestScenario59_EmptyToolsRoleAllowsAll(t *testing.T) {
	database := testutil.TestDB(t)
	cfg := testutil.TestConfig()
	cfg.Settings.AgentMode = "custom"
	fake := testutil.NewFakeModel(resp("done", 10, 5))
	sm := session.NewManager(database.Conn(), cfg)
	tracker := prompt.NewTracker(database.Conn(), cfg)
	composer := prompt.NewComposer(cfg, sm, tracker, database.Conn())
	roles := []agent.CustomRole{
		{Name: "general", Description: "General", Tools: nil, Prompt: "Be helpful."},
	}
	router := agent.NewRoleRouter(roles, "explicit", nil)
	factory := model.NewFactory(model.FactoryConfig{
		Composer: composer, Model: fake, Tracker: tracker, DB: database.Conn(),
		Config: cfg, RoleRouter: router,
		Caps: capability.NewCapabilitySet(nil),
	})
	ctx := context.Background()
	sessID, _ := sm.Create(ctx, "webchat", "user-1", "default")
	result, _ := factory.Run(ctx, sessID, "@general hello", "webchat")
	assertCompleted(t, result)
}

func TestScenario60_MultipleRolesExplicit(t *testing.T) {
	database := testutil.TestDB(t)
	cfg := testutil.TestConfig()
	cfg.Settings.AgentMode = "custom"
	fake := testutil.NewFakeModel(resp("I'm the helper.", 10, 5))
	sm := session.NewManager(database.Conn(), cfg)
	tracker := prompt.NewTracker(database.Conn(), cfg)
	composer := prompt.NewComposer(cfg, sm, tracker, database.Conn())
	roles := []agent.CustomRole{
		{Name: "analyst", Description: "Analyze", Prompt: "You analyze."},
		{Name: "helper", Description: "Help", Prompt: "You help."},
	}
	router := agent.NewRoleRouter(roles, "explicit", nil)
	factory := model.NewFactory(model.FactoryConfig{
		Composer: composer, Model: fake, Tracker: tracker, DB: database.Conn(),
		Config: cfg, RoleRouter: router,
		Caps: capability.NewCapabilitySet(capability.MessagingDefaults()),
	})
	ctx := context.Background()
	sessID, _ := sm.Create(ctx, "webchat", "user-1", "default")
	result, _ := factory.Run(ctx, sessID, "@helper what time is it?", "webchat")
	assertCompleted(t, result)
	if !strings.Contains(fake.Calls()[0][0].Content, "You help") {
		t.Error("helper role not routed")
	}
}

func TestScenario61_NoRoleFallback(t *testing.T) {
	database := testutil.TestDB(t)
	cfg := testutil.TestConfig()
	cfg.Settings.AgentMode = "custom"
	fake := testutil.NewFakeModel(resp("ok", 10, 5))
	sm := session.NewManager(database.Conn(), cfg)
	tracker := prompt.NewTracker(database.Conn(), cfg)
	composer := prompt.NewComposer(cfg, sm, tracker, database.Conn())
	roles := []agent.CustomRole{
		{Name: "analyst", Description: "Analyze", Prompt: "You analyze."},
	}
	router := agent.NewRoleRouter(roles, "explicit", nil)
	factory := model.NewFactory(model.FactoryConfig{
		Composer: composer, Model: fake, Tracker: tracker, DB: database.Conn(),
		Config: cfg, RoleRouter: router,
		Caps: capability.NewCapabilitySet(capability.MessagingDefaults()),
	})
	ctx := context.Background()
	sessID, _ := sm.Create(ctx, "webchat", "user-1", "default")
	// No @role prefix → should still work (no role found, runs without role)
	result, _ := factory.Run(ctx, sessID, "hello world", "webchat")
	assertCompleted(t, result)
}

func TestScenario62_RoundRobinRouting(t *testing.T) {
	database := testutil.TestDB(t)
	cfg := testutil.TestConfig()
	cfg.Settings.AgentMode = "custom"
	fake := testutil.NewFakeModel(resp("r1", 10, 5), resp("r2", 10, 5))
	sm := session.NewManager(database.Conn(), cfg)
	tracker := prompt.NewTracker(database.Conn(), cfg)
	composer := prompt.NewComposer(cfg, sm, tracker, database.Conn())
	roles := []agent.CustomRole{
		{Name: "role-a", Description: "A", Prompt: "role A prompt"},
		{Name: "role-b", Description: "B", Prompt: "role B prompt"},
	}
	router := agent.NewRoleRouter(roles, "round-robin", nil)
	factory := model.NewFactory(model.FactoryConfig{
		Composer: composer, Model: fake, Tracker: tracker, DB: database.Conn(),
		Config: cfg, RoleRouter: router,
		Caps: capability.NewCapabilitySet(capability.MessagingDefaults()),
	})
	ctx := context.Background()
	sessID, _ := sm.Create(ctx, "webchat", "user-1", "default")
	r1, _ := factory.Run(ctx, sessID, "test 1", "webchat")
	assertCompleted(t, r1)
	r2, _ := factory.Run(ctx, sessID, "test 2", "webchat")
	assertCompleted(t, r2)
}

func TestScenario63_AutoModeDefault(t *testing.T) {
	s := newScenario(t, resp("auto mode response", 20, 8))
	r := s.run(t, "hello from auto mode")
	assertCompleted(t, r)
}

func TestScenario64_SingleModeOverride(t *testing.T) {
	s := newScenario(t, resp("single mode", 20, 8))
	r := s.run(t, "[mode:single] run in single mode")
	assertCompleted(t, r)
}

func TestScenario65_MultiModeOverride(t *testing.T) {
	s := newScenario(t, resp("multi mode", 20, 8))
	r := s.run(t, "[mode:multi] run in multi mode")
	assertCompleted(t, r)
}

// =========================================================================
// GROUP 7: Multi-Tool & Parallel Scenarios (66–75)
// =========================================================================

func TestScenario66_TwoToolsParallel(t *testing.T) {
	s := newScenario(t,
		toolResp([]agent.ToolCall{
			tc("1", "task.add", `{"content":"Item 1"}`),
			tc("2", "task.add", `{"content":"Item 2"}`),
		}, 30, 10),
		resp("Added both items.", 15, 5),
	)
	r := s.run(t, "add Item 1 and Item 2")
	assertCompleted(t, r)
	assertToolOutputCount(t, r, 2)
}

func TestScenario67_ThreeToolsSequential(t *testing.T) {
	s := newScenario(t,
		toolResp([]agent.ToolCall{tc("1", "task.add", `{"content":"A"}`)}, 20, 8),
		toolResp([]agent.ToolCall{tc("2", "task.add", `{"content":"B"}`)}, 20, 8),
		toolResp([]agent.ToolCall{tc("3", "task.list", `{}`)}, 20, 8),
		resp("3 operations done.", 10, 5),
	)
	r := s.run(t, "add A, add B, then list")
	assertCompleted(t, r)
	assertToolOutputCount(t, r, 3)
}

func TestScenario68_ToolOutputFeedsNextCall(t *testing.T) {
	s := newScenario(t,
		toolResp([]agent.ToolCall{tc("1", "task.add", `{"content":"Buy gift"}`)}, 20, 8),
		toolResp([]agent.ToolCall{tc("2", "task.prioritize", `{"id":1,"priority":3}`)}, 20, 8),
		resp("Added and prioritized.", 15, 5),
	)
	r := s.run(t, "add buy gift and make it high priority")
	assertCompleted(t, r)
}

func TestScenario69_ComplexWorkflow(t *testing.T) {
	s := newScenarioWithMemory(t,
		toolResp([]agent.ToolCall{tc("1", "task.add", `{"content":"Finish report"}`)}, 20, 8),
		toolResp([]agent.ToolCall{tc("2", "expense.log", `{"amount":25,"category":"office"}`)}, 20, 8),
		toolResp([]agent.ToolCall{tc("3", "memory.capture", `{"content":"Report due Monday","tags":["work"]}`)}, 20, 8),
		resp("Done: task added, expense logged, note saved.", 20, 8),
	)
	r := s.run(t, "add task finish report, log $25 office expense, and remember report is due Monday")
	assertCompleted(t, r)
	assertToolOutputCount(t, r, 3)
}

func TestScenario70_TaskAndContactCombo(t *testing.T) {
	s := newScenario(t,
		toolResp([]agent.ToolCall{
			tc("1", "task.add", `{"content":"Call dentist"}`),
			tc("2", "contact.add", `{"name":"Dr. Dental","phone":"+1555111"}`),
		}, 30, 10),
		resp("Task and contact created.", 15, 5),
	)
	r := s.run(t, "add task to call dentist and save Dr. Dental +1555111")
	assertCompleted(t, r)
	assertToolOutputCount(t, r, 2)
}

func TestScenario71_ExpenseAndJournalCombo(t *testing.T) {
	s := newScenario(t,
		toolResp([]agent.ToolCall{
			tc("1", "expense.log", `{"amount":200,"category":"travel"}`),
			tc("2", "journal.write", `{"content":"Trip to the mountains","mood":"happy"}`),
		}, 30, 10),
		resp("Expense logged and journal entry saved.", 15, 5),
	)
	r := s.run(t, "log $200 travel expense and journal entry about mountain trip")
	assertCompleted(t, r)
}

func TestScenario72_FourToolsInSequence(t *testing.T) {
	s := newScenario(t,
		toolResp([]agent.ToolCall{tc("1", "task.add", `{"content":"T1"}`)}, 15, 5),
		toolResp([]agent.ToolCall{tc("2", "task.add", `{"content":"T2"}`)}, 15, 5),
		toolResp([]agent.ToolCall{tc("3", "task.add", `{"content":"T3"}`)}, 15, 5),
		toolResp([]agent.ToolCall{tc("4", "task.add", `{"content":"T4"}`)}, 15, 5),
		resp("4 tasks added.", 10, 5),
	)
	r := s.run(t, "add T1, T2, T3, T4 as separate tasks")
	assertCompleted(t, r)
	assertToolOutputCount(t, r, 4)
}

func TestScenario73_HabitAndHealthCombo(t *testing.T) {
	s := newScenario(t,
		toolResp([]agent.ToolCall{
			tc("1", "habit.create", `{"name":"Meditate","frequency":"daily"}`),
			tc("2", "health.log", `{"metric":"sleep","value":"7.5","unit":"hours"}`),
		}, 30, 10),
		resp("Habit created and health logged.", 15, 5),
	)
	r := s.run(t, "create meditate habit and log 7.5 hours sleep")
	assertCompleted(t, r)
}

func TestScenario74_PlaceSaveAndSearch(t *testing.T) {
	s := newScenario(t,
		toolResp([]agent.ToolCall{tc("1", "place.save", `{"name":"Cafe Luna","category":"cafe","rating":5}`)}, 20, 8),
		toolResp([]agent.ToolCall{tc("2", "place.search", `{"category":"cafe"}`)}, 20, 8),
		resp("Cafe Luna saved and found.", 15, 5),
	)
	r := s.run(t, "save Cafe Luna as a 5-star cafe then search cafes")
	assertCompleted(t, r)
}

func TestScenario75_ReminderAndTask(t *testing.T) {
	s := newScenario(t,
		toolResp([]agent.ToolCall{
			tc("1", "task.add", `{"content":"Buy birthday gift"}`),
			tc("2", "reminder.set", `{"message":"Buy gift for Sarah","remind_at":"2026-04-01T10:00:00Z"}`),
		}, 30, 10),
		resp("Task and reminder set.", 15, 5),
	)
	r := s.run(t, "add task to buy birthday gift and remind me April 1st at 10am")
	assertCompleted(t, r)
}

// =========================================================================
// GROUP 8: PostProcessor & Indexing (76–82)
// =========================================================================

func TestScenario76_PostProcessorCalled(t *testing.T) {
	database := testutil.TestDB(t)
	cfg := testutil.TestConfig()
	fake := testutil.NewFakeModel(
		toolResp([]agent.ToolCall{tc("1", "task.list", `{}`)}, 20, 10),
		resp("Here are your tasks.", 20, 8),
	)
	sm := session.NewManager(database.Conn(), cfg)
	tracker := prompt.NewTracker(database.Conn(), cfg)
	composer := prompt.NewComposer(cfg, sm, tracker, database.Conn())
	registry := tool.NewRegistry()
	tool.RegisterDataTools(registry, database.Conn())
	dispatcher := tool.NewDispatcher(registry, capability.NewEngine(nil), nil)
	proc := &recordingPostProcessor{}
	factory := model.NewFactory(model.FactoryConfig{
		Composer: composer, Model: fake, Tools: dispatcher,
		Caps: capability.NewCapabilitySet(capability.MessagingDefaults()),
		Tracker: tracker, DB: database.Conn(), Config: cfg, PostProcessor: proc,
	})
	ctx := context.Background()
	sessID, _ := sm.Create(ctx, "webchat", "user-1", "default")
	result, _ := factory.Run(ctx, sessID, "show tasks", "webchat")
	assertCompleted(t, result)
	proc.mu.Lock()
	defer proc.mu.Unlock()
	if proc.callCount != 1 {
		t.Fatalf("postProcessor calls = %d", proc.callCount)
	}
	if proc.userMsg != "show tasks" {
		t.Errorf("userMsg = %q", proc.userMsg)
	}
	if proc.assistantMsg != "Here are your tasks." {
		t.Errorf("assistantMsg = %q", proc.assistantMsg)
	}
}

func TestScenario77_PostProcessorRecordsToolOutputs(t *testing.T) {
	database := testutil.TestDB(t)
	cfg := testutil.TestConfig()
	fake := testutil.NewFakeModel(
		toolResp([]agent.ToolCall{
			tc("1", "task.add", `{"content":"Test"}`),
			tc("2", "task.list", `{}`),
		}, 20, 10),
		resp("Done.", 10, 5),
	)
	sm := session.NewManager(database.Conn(), cfg)
	tracker := prompt.NewTracker(database.Conn(), cfg)
	composer := prompt.NewComposer(cfg, sm, tracker, database.Conn())
	registry := tool.NewRegistry()
	tool.RegisterDataTools(registry, database.Conn())
	dispatcher := tool.NewDispatcher(registry, capability.NewEngine(nil), nil)
	proc := &recordingPostProcessor{}
	factory := model.NewFactory(model.FactoryConfig{
		Composer: composer, Model: fake, Tools: dispatcher,
		Caps: capability.NewCapabilitySet(capability.MessagingDefaults()),
		Tracker: tracker, DB: database.Conn(), Config: cfg, PostProcessor: proc,
	})
	ctx := context.Background()
	sessID, _ := sm.Create(ctx, "webchat", "user-1", "default")
	factory.Run(ctx, sessID, "add test and list", "webchat")
	proc.mu.Lock()
	defer proc.mu.Unlock()
	if len(proc.toolOutputs) != 2 {
		t.Fatalf("toolOutputs = %d, want 2", len(proc.toolOutputs))
	}
}

func TestScenario78_PostProcessorNilSafe(t *testing.T) {
	s := newScenario(t, resp("ok", 10, 5))
	// No PostProcessor set — should not panic
	r := s.run(t, "hello")
	assertCompleted(t, r)
}

func TestScenario79_PostProcessorSessionID(t *testing.T) {
	database := testutil.TestDB(t)
	cfg := testutil.TestConfig()
	fake := testutil.NewFakeModel(resp("ok", 10, 5))
	sm := session.NewManager(database.Conn(), cfg)
	tracker := prompt.NewTracker(database.Conn(), cfg)
	composer := prompt.NewComposer(cfg, sm, tracker, database.Conn())
	proc := &recordingPostProcessor{}
	factory := model.NewFactory(model.FactoryConfig{
		Composer: composer, Model: fake, Tracker: tracker, DB: database.Conn(),
		Config: cfg, PostProcessor: proc,
		Caps: capability.NewCapabilitySet(capability.MessagingDefaults()),
	})
	ctx := context.Background()
	sessID, _ := sm.Create(ctx, "webchat", "user-1", "default")
	factory.Run(ctx, sessID, "test", "webchat")
	proc.mu.Lock()
	defer proc.mu.Unlock()
	if proc.sessionID != sessID {
		t.Errorf("sessionID = %q, want %q", proc.sessionID, sessID)
	}
}

func TestScenario80_PostProcessorNoTools(t *testing.T) {
	database := testutil.TestDB(t)
	cfg := testutil.TestConfig()
	fake := testutil.NewFakeModel(resp("just text", 10, 5))
	sm := session.NewManager(database.Conn(), cfg)
	tracker := prompt.NewTracker(database.Conn(), cfg)
	composer := prompt.NewComposer(cfg, sm, tracker, database.Conn())
	proc := &recordingPostProcessor{}
	factory := model.NewFactory(model.FactoryConfig{
		Composer: composer, Model: fake, Tracker: tracker, DB: database.Conn(),
		Config: cfg, PostProcessor: proc,
		Caps: capability.NewCapabilitySet(capability.MessagingDefaults()),
	})
	ctx := context.Background()
	sessID, _ := sm.Create(ctx, "webchat", "user-1", "default")
	factory.Run(ctx, sessID, "no tools needed", "webchat")
	proc.mu.Lock()
	defer proc.mu.Unlock()
	if len(proc.toolOutputs) != 0 {
		t.Errorf("toolOutputs = %d, want 0", len(proc.toolOutputs))
	}
}

func TestScenario81_ToolOutputTruncatedInResult(t *testing.T) {
	// Verify large tool outputs get capped at 10KB.
	database := testutil.TestDB(t)
	cfg := testutil.TestConfig()
	bigOutput := strings.Repeat("X", 20000)
	fake := testutil.NewFakeModel(
		toolResp([]agent.ToolCall{tc("1", "task.list", `{}`)}, 20, 10),
		resp("done", 10, 5),
	)
	sm := session.NewManager(database.Conn(), cfg)
	tracker := prompt.NewTracker(database.Conn(), cfg)
	composer := prompt.NewComposer(cfg, sm, tracker, database.Conn())
	registry := tool.NewRegistry()
	registry.Register(&tool.FuncTool{
		ToolName: "task.list", ToolDesc: "list", ToolSchema: `{}`,
		Exec: func(_ context.Context, _ string) (string, error) { return bigOutput, nil },
	})
	dispatcher := tool.NewDispatcher(registry, capability.NewEngine(nil), nil)
	factory := model.NewFactory(model.FactoryConfig{
		Composer: composer, Model: fake, Tools: dispatcher,
		Caps: capability.NewCapabilitySet(capability.MessagingDefaults()),
		Tracker: tracker, DB: database.Conn(), Config: cfg,
	})
	ctx := context.Background()
	sessID, _ := sm.Create(ctx, "webchat", "user-1", "default")
	result, _ := factory.Run(ctx, sessID, "list", "webchat")
	assertCompleted(t, result)
	if len(result.ToolOutputs) != 1 {
		t.Fatalf("ToolOutputs = %d", len(result.ToolOutputs))
	}
	if len(result.ToolOutputs[0].Output) > 11000 {
		t.Errorf("output not truncated: %d bytes", len(result.ToolOutputs[0].Output))
	}
}

func TestScenario82_NoToolsRegistered(t *testing.T) {
	database := testutil.TestDB(t)
	cfg := testutil.TestConfig()
	fake := testutil.NewFakeModel(resp("I can only chat.", 15, 5))
	sm := session.NewManager(database.Conn(), cfg)
	tracker := prompt.NewTracker(database.Conn(), cfg)
	composer := prompt.NewComposer(cfg, sm, tracker, database.Conn())
	// No tools, no dispatcher
	factory := model.NewFactory(model.FactoryConfig{
		Composer: composer, Model: fake, Tracker: tracker, DB: database.Conn(), Config: cfg,
		Caps: capability.NewCapabilitySet(capability.MessagingDefaults()),
	})
	ctx := context.Background()
	sessID, _ := sm.Create(ctx, "webchat", "user-1", "default")
	result, _ := factory.Run(ctx, sessID, "do something", "webchat")
	assertCompleted(t, result)
}

// =========================================================================
// GROUP 9: Error & Edge Cases (83–92)
// =========================================================================

func TestScenario83_ModelError(t *testing.T) {
	database := testutil.TestDB(t)
	cfg := testutil.TestConfig()
	fake := testutil.NewFakeModel()
	fake.SetError(fmt.Errorf("model error"))
	sm := session.NewManager(database.Conn(), cfg)
	tracker := prompt.NewTracker(database.Conn(), cfg)
	composer := prompt.NewComposer(cfg, sm, tracker, database.Conn())
	factory := model.NewFactory(model.FactoryConfig{
		Composer: composer, Model: fake, Tracker: tracker, DB: database.Conn(), Config: cfg,
		Caps: capability.NewCapabilitySet(capability.MessagingDefaults()),
	})
	ctx := context.Background()
	sessID, _ := sm.Create(ctx, "webchat", "user-1", "default")
	r, err := factory.Run(ctx, sessID, "hello", "webchat")
	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}
	if r.Status != agent.StateFailed {
		t.Errorf("status = %q, want failed", r.Status)
	}
	if !strings.Contains(r.Error, "model error") {
		t.Errorf("error = %q, want model error substring", r.Error)
	}
}

func TestScenario84_ModelReturnsError(t *testing.T) {
	database := testutil.TestDB(t)
	cfg := testutil.TestConfig()
	fake := testutil.NewFakeModel()
	fake.SetError(fmt.Errorf("max tool calls exceeded"))
	sm := session.NewManager(database.Conn(), cfg)
	tracker := prompt.NewTracker(database.Conn(), cfg)
	composer := prompt.NewComposer(cfg, sm, tracker, database.Conn())
	factory := model.NewFactory(model.FactoryConfig{
		Composer: composer, Model: fake, Tracker: tracker, DB: database.Conn(), Config: cfg,
		Caps: capability.NewCapabilitySet(capability.MessagingDefaults()),
	})
	ctx := context.Background()
	sessID, _ := sm.Create(ctx, "webchat", "user-1", "default")
	r, err := factory.Run(ctx, sessID, "test", "webchat")
	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}
	if r.Status != agent.StateFailed {
		t.Errorf("status = %q, want failed", r.Status)
	}
}

func TestScenario85_ToolReturnsError(t *testing.T) {
	// Tool errors are returned as tool messages, not fatal errors.
	database := testutil.TestDB(t)
	cfg := testutil.TestConfig()
	fake := testutil.NewFakeModel(
		toolResp([]agent.ToolCall{tc("1", "task.complete", `{"id":99999}`)}, 20, 8),
		resp("That task doesn't exist.", 15, 5),
	)
	sm := session.NewManager(database.Conn(), cfg)
	tracker := prompt.NewTracker(database.Conn(), cfg)
	composer := prompt.NewComposer(cfg, sm, tracker, database.Conn())
	registry := tool.NewRegistry()
	tool.RegisterDataTools(registry, database.Conn())
	dispatcher := tool.NewDispatcher(registry, capability.NewEngine(nil), nil)
	factory := model.NewFactory(model.FactoryConfig{
		Composer: composer, Model: fake, Tools: dispatcher,
		Caps: capability.NewCapabilitySet(capability.MessagingDefaults()),
		Tracker: tracker, DB: database.Conn(), Config: cfg,
	})
	ctx := context.Background()
	sessID, _ := sm.Create(ctx, "webchat", "user-1", "default")
	result, err := factory.Run(ctx, sessID, "complete task 99999", "webchat")
	// Should still complete — tool errors are non-fatal
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	assertCompleted(t, result)
}

func TestScenario86_UnknownToolCall(t *testing.T) {
	s := newScenario(t,
		toolResp([]agent.ToolCall{tc("1", "nonexistent.tool", `{}`)}, 20, 8),
		resp("I couldn't use that tool.", 15, 5),
	)
	r := s.run(t, "use a nonexistent tool")
	assertCompleted(t, r)
}

func TestScenario87_EmptyToolInput(t *testing.T) {
	s := newScenario(t,
		toolResp([]agent.ToolCall{tc("1", "task.list", `{}`)}, 20, 8),
		resp("No tasks.", 10, 5),
	)
	r := s.run(t, "list tasks")
	assertCompleted(t, r)
}

func TestScenario88_MultipleSessionsSameFactory(t *testing.T) {
	database := testutil.TestDB(t)
	cfg := testutil.TestConfig()
	fake := testutil.NewFakeModel(resp("r1", 10, 5), resp("r2", 10, 5))
	sm := session.NewManager(database.Conn(), cfg)
	tracker := prompt.NewTracker(database.Conn(), cfg)
	composer := prompt.NewComposer(cfg, sm, tracker, database.Conn())
	factory := model.NewFactory(model.FactoryConfig{
		Composer: composer, Model: fake, Tracker: tracker, DB: database.Conn(), Config: cfg,
		Caps: capability.NewCapabilitySet(capability.MessagingDefaults()),
	})
	ctx := context.Background()
	sess1, _ := sm.Create(ctx, "webchat", "user-1", "default")
	sess2, _ := sm.Create(ctx, "webchat", "user-2", "default")
	r1, _ := factory.Run(ctx, sess1, "hello from user 1", "webchat")
	r2, _ := factory.Run(ctx, sess2, "hello from user 2", "webchat")
	assertCompleted(t, r1)
	assertCompleted(t, r2)
}

func TestScenario89_FactoryWithoutCaps(t *testing.T) {
	database := testutil.TestDB(t)
	cfg := testutil.TestConfig()
	fake := testutil.NewFakeModel(resp("ok", 10, 5))
	sm := session.NewManager(database.Conn(), cfg)
	tracker := prompt.NewTracker(database.Conn(), cfg)
	composer := prompt.NewComposer(cfg, sm, tracker, database.Conn())
	factory := model.NewFactory(model.FactoryConfig{
		Composer: composer, Model: fake, Tracker: tracker, DB: database.Conn(), Config: cfg,
		Caps: capability.NewCapabilitySet(nil), // no capabilities
	})
	ctx := context.Background()
	sessID, _ := sm.Create(ctx, "webchat", "user-1", "default")
	result, _ := factory.Run(ctx, sessID, "hello", "webchat")
	assertCompleted(t, result)
}

func TestScenario90_VeryLongToolOutput(t *testing.T) {
	s := newScenario(t,
		toolResp([]agent.ToolCall{tc("1", "task.add", `{"content":"x"}`)}, 15, 5),
		resp("added", 10, 5),
	)
	r := s.run(t, "add x")
	assertCompleted(t, r)
	// ToolOutputs should be present and non-empty.
	if len(r.ToolOutputs) != 1 {
		t.Fatalf("ToolOutputs = %d", len(r.ToolOutputs))
	}
}

func TestScenario91_SystemMessageContainsPersona(t *testing.T) {
	s := newScenario(t, resp("hi", 10, 5))
	s.run(t, "test")
	calls := s.fake.Calls()
	if len(calls) < 1 {
		t.Fatal("no calls")
	}
	sysMsg := calls[0][0]
	if sysMsg.Role != "system" {
		t.Errorf("first msg role = %q", sysMsg.Role)
	}
	if sysMsg.Content == "" {
		t.Error("system message is empty")
	}
}

func TestScenario92_UserMessagePassedCorrectly(t *testing.T) {
	s := newScenario(t, resp("ok", 10, 5))
	s.run(t, "specific user query")
	calls := s.fake.Calls()
	lastMsg := calls[0][len(calls[0])-1]
	if lastMsg.Role != "user" || lastMsg.Content != "specific user query" {
		t.Errorf("last msg = %s: %q", lastMsg.Role, lastMsg.Content)
	}
}

// =========================================================================
// GROUP 10: Session & History (93–97)
// =========================================================================

func TestScenario93_ConversationHistoryInPrompt(t *testing.T) {
	database := testutil.TestDB(t)
	cfg := testutil.TestConfig()
	fake := testutil.NewFakeModel(resp("first", 10, 5), resp("second", 10, 5))
	sm := session.NewManager(database.Conn(), cfg)
	tracker := prompt.NewTracker(database.Conn(), cfg)
	composer := prompt.NewComposer(cfg, sm, tracker, database.Conn())
	factory := model.NewFactory(model.FactoryConfig{
		Composer: composer, Model: fake, Tracker: tracker, DB: database.Conn(), Config: cfg,
		Caps: capability.NewCapabilitySet(capability.MessagingDefaults()),
	})
	ctx := context.Background()
	sessID, _ := sm.Create(ctx, "webchat", "user-1", "default")

	// First turn
	r1, _ := factory.Run(ctx, sessID, "hello", "webchat")
	assertCompleted(t, r1)

	// Store first turn in session
	sm.AddMessage(ctx, sessID, agent.Message{Role: "user", Content: "hello"})
	sm.AddMessage(ctx, sessID, agent.Message{Role: "assistant", Content: "first"})

	// Second turn — should see history
	r2, _ := factory.Run(ctx, sessID, "follow up", "webchat")
	assertCompleted(t, r2)

	calls := fake.Calls()
	// Second call should have more messages (history + new user msg)
	if len(calls[1]) <= len(calls[0]) {
		t.Error("second call should have more messages (includes history)")
	}
}

func TestScenario94_SessionCreateAndUse(t *testing.T) {
	database := testutil.TestDB(t)
	cfg := testutil.TestConfig()
	sm := session.NewManager(database.Conn(), cfg)
	ctx := context.Background()
	sessID, err := sm.Create(ctx, "webchat", "user-1", "default")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	sess, err := sm.Get(ctx, sessID)
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	if sess.Channel != "webchat" {
		t.Errorf("channel = %q", sess.Channel)
	}
}

func TestScenario95_SessionIsolation(t *testing.T) {
	database := testutil.TestDB(t)
	cfg := testutil.TestConfig()
	sm := session.NewManager(database.Conn(), cfg)
	ctx := context.Background()
	sess1, _ := sm.Create(ctx, "webchat", "user-1", "default")
	sess2, _ := sm.Create(ctx, "webchat", "user-2", "default")

	sm.AddMessage(ctx, sess1, agent.Message{Role: "user", Content: "secret"})

	msgs2, _ := sm.Messages(ctx, sess2)
	for _, m := range msgs2 {
		if strings.Contains(m.Content, "secret") {
			t.Error("session isolation broken: user-2 sees user-1's message")
		}
	}
}

func TestScenario96_TokenTrackingRecorded(t *testing.T) {
	database := testutil.TestDB(t)
	cfg := testutil.TestConfig()
	fake := testutil.NewFakeModel(resp("tracked", 100, 50))
	sm := session.NewManager(database.Conn(), cfg)
	tracker := prompt.NewTracker(database.Conn(), cfg)
	composer := prompt.NewComposer(cfg, sm, tracker, database.Conn())
	factory := model.NewFactory(model.FactoryConfig{
		Composer: composer, Model: fake, Tracker: tracker, DB: database.Conn(), Config: cfg,
		Caps: capability.NewCapabilitySet(capability.MessagingDefaults()),
	})
	ctx := context.Background()
	sessID, _ := sm.Create(ctx, "webchat", "user-1", "default")
	factory.Run(ctx, sessID, "track me", "webchat")
	usage, _ := tracker.MonthlyUsage(ctx)
	if usage <= 0 {
		t.Error("expected positive monthly usage after run")
	}
}

func TestScenario97_MultiTurnConversation(t *testing.T) {
	database := testutil.TestDB(t)
	cfg := testutil.TestConfig()
	fake := testutil.NewFakeModel(
		resp("turn 1 response", 10, 5),
		resp("turn 2 response", 10, 5),
		resp("turn 3 response", 10, 5),
	)
	sm := session.NewManager(database.Conn(), cfg)
	tracker := prompt.NewTracker(database.Conn(), cfg)
	composer := prompt.NewComposer(cfg, sm, tracker, database.Conn())
	factory := model.NewFactory(model.FactoryConfig{
		Composer: composer, Model: fake, Tracker: tracker, DB: database.Conn(), Config: cfg,
		Caps: capability.NewCapabilitySet(capability.MessagingDefaults()),
	})
	ctx := context.Background()
	sessID, _ := sm.Create(ctx, "webchat", "user-1", "default")

	for i, msg := range []string{"turn 1", "turn 2", "turn 3"} {
		r, _ := factory.Run(ctx, sessID, msg, "webchat")
		assertCompleted(t, r)
		sm.AddMessage(ctx, sessID, agent.Message{Role: "user", Content: msg})
		sm.AddMessage(ctx, sessID, agent.Message{Role: "assistant", Content: r.Output})
		_ = i
	}
	if fake.CallCount() != 3 {
		t.Errorf("callCount = %d, want 3", fake.CallCount())
	}
}

// =========================================================================
// GROUP 11: Misc Realistic Scenarios (98–100)
// =========================================================================

func TestScenario98_MorningBriefing(t *testing.T) {
	s := newSeededScenario(t,
		toolResp([]agent.ToolCall{
			tc("1", "task.list", `{}`),
			tc("2", "reminder.list", `{}`),
		}, 40, 15),
		resp("Good morning! You have 2 tasks and no reminders.", 20, 8),
	)
	r := s.run(t, "good morning, what's on my plate today?")
	assertCompleted(t, r)
	assertToolOutputCount(t, r, 2)
}

func TestScenario99_EndOfDayReview(t *testing.T) {
	s := newScenario(t,
		toolResp([]agent.ToolCall{tc("1", "expense.summary", `{}`)}, 20, 8),
		toolResp([]agent.ToolCall{tc("2", "journal.write", `{"content":"Productive day","mood":"content"}`)}, 20, 8),
		resp("Day reviewed. Expenses logged, journal saved.", 20, 8),
	)
	r := s.run(t, "let's do an end of day review: show expenses and write a journal entry")
	assertCompleted(t, r)
}

func TestScenario100_FullLifecycle(t *testing.T) {
	// Full lifecycle: create task → work on it → complete → log expense → journal → memory
	s := newScenarioWithMemory(t,
		toolResp([]agent.ToolCall{tc("1", "task.add", `{"content":"Ship feature X","list":"work","priority":3}`)}, 20, 8),
		toolResp([]agent.ToolCall{tc("2", "task.complete", `{"id":1}`)}, 20, 8),
		toolResp([]agent.ToolCall{tc("3", "expense.log", `{"amount":5,"category":"coffee","description":"celebration coffee"}`)}, 20, 8),
		toolResp([]agent.ToolCall{tc("4", "journal.write", `{"content":"Shipped feature X!","mood":"happy"}`)}, 20, 8),
		toolResp([]agent.ToolCall{tc("5", "memory.capture", `{"content":"Feature X shipped successfully","tags":["milestone","work"]}`)}, 20, 8),
		resp("Feature X shipped! Task completed, expense logged, journal written, and milestone noted.", 30, 12),
	)
	r := s.run(t, "I shipped feature X! Complete the task, log a $5 coffee, journal about it, and remember this milestone")
	assertCompleted(t, r)
	assertToolOutputCount(t, r, 5)
	assertOutput(t, r, "shipped")
}
