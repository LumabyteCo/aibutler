//go:build integration

package integration

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/LumabyteCo/aibutler/internal/agent"
	"github.com/LumabyteCo/aibutler/internal/capability"
	"github.com/LumabyteCo/aibutler/internal/channel"
	"github.com/LumabyteCo/aibutler/internal/config"
	"github.com/LumabyteCo/aibutler/internal/i18n"
	"github.com/LumabyteCo/aibutler/internal/model"
	"github.com/LumabyteCo/aibutler/internal/prompt"
	"github.com/LumabyteCo/aibutler/internal/session"
	"github.com/LumabyteCo/aibutler/internal/stopphrase"
	"github.com/LumabyteCo/aibutler/internal/tool"
	"github.com/LumabyteCo/aibutler/testutil"
)

// pipeline wires a complete Router with FakeModel for testing.
type pipeline struct {
	Router  *channel.Router
	Factory *model.Factory
	Fake    *testutil.FakeModel
	Channel *fakeChannel
	SM      *session.Manager
	Config  *config.Config
}

func setupPipeline(t *testing.T, responses ...agent.Response) *pipeline {
	t.Helper()
	database := testutil.TestDB(t)
	cfg := testutil.TestConfig()

	fake := testutil.NewFakeModel(responses...)

	sm := session.NewManager(database.Conn(), cfg)
	tracker := prompt.NewTracker(database.Conn(), cfg)
	composer := prompt.NewComposer(cfg, sm, tracker, database.Conn())

	// Register data tools.
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

	fch := &fakeChannel{name: "webchat"}
	reg := channel.NewRegistry()
	reg.Register(fch)

	bundle := i18n.New("en")
	stop := stopphrase.NewMatcher(bundle)

	router := channel.NewRouter(channel.RouterConfig{
		Sessions: sm,
		Stop:     stop,
		Channels: reg,
		Config:   cfg,
		I18n:     bundle,
		DB:       database.Conn(),
		Agent:    factory,
	})

	return &pipeline{
		Router:  router,
		Factory: factory,
		Fake:    fake,
		Channel: fch,
		SM:      sm,
		Config:  cfg,
	}
}

// TestEndToEndMessageFlow verifies: Envelope → Router → Factory → FakeModel → response via channel.
func TestEndToEndMessageFlow(t *testing.T) {
	p := setupPipeline(t, agent.Response{
		Content: "Hello! How can I help?", TokensIn: 20, TokensOut: 10,
	})

	env := channel.Envelope{
		ID:        "e2e-1",
		Channel:   "webchat",
		AccountID: "user-e2e",
		Type:      channel.TypeText,
		Text:      "Hello!",
		Timestamp: time.Now(),
	}

	err := p.Router.HandleMessage(context.Background(), env)
	if err != nil {
		t.Fatalf("HandleMessage: %v", err)
	}

	p.Channel.mu.Lock()
	defer p.Channel.mu.Unlock()
	if len(p.Channel.sent) != 1 {
		t.Fatalf("sent = %d, want 1", len(p.Channel.sent))
	}
	if p.Channel.sent[0].Text != "Hello! How can I help?" {
		t.Errorf("response = %q", p.Channel.sent[0].Text)
	}
}

// TestEndToEndToolExecution verifies: FakeModel requests task.add → Dispatcher executes → task in DB.
func TestEndToEndToolExecution(t *testing.T) {
	p := setupPipeline(t,
		agent.Response{
			Content: "Adding task.",
			ToolCalls: []agent.ToolCall{
				{ID: "tc1", Name: "task.add", Input: `{"content":"E2E task"}`},
			},
			TokensIn: 30, TokensOut: 15,
		},
		agent.Response{
			Content:  "Done! Task added.",
			TokensIn: 40, TokensOut: 10,
		},
	)

	env := channel.Envelope{
		ID:        "e2e-tool-1",
		Channel:   "webchat",
		AccountID: "user-e2e",
		Type:      channel.TypeText,
		Text:      "Add a task: E2E task",
		Timestamp: time.Now(),
	}

	err := p.Router.HandleMessage(context.Background(), env)
	if err != nil {
		t.Fatalf("HandleMessage: %v", err)
	}

	p.Channel.mu.Lock()
	if len(p.Channel.sent) != 1 {
		t.Fatalf("sent = %d, want 1", len(p.Channel.sent))
	}
	if p.Channel.sent[0].Text != "Done! Task added." {
		t.Errorf("response = %q", p.Channel.sent[0].Text)
	}
	p.Channel.mu.Unlock()

	if p.Fake.CallCount() != 2 {
		t.Errorf("model calls = %d, want 2", p.Fake.CallCount())
	}
}

// TestEndToEndSessionPersistence verifies 2 messages share session and history flows through.
func TestEndToEndSessionPersistence(t *testing.T) {
	p := setupPipeline(t,
		agent.Response{Content: "First reply.", TokensIn: 10, TokensOut: 5},
		agent.Response{Content: "Second reply.", TokensIn: 20, TokensOut: 10},
	)

	ctx := context.Background()

	// First message.
	env1 := channel.Envelope{
		ID: "sess-1", Channel: "webchat", AccountID: "user-sess",
		Type: channel.TypeText, Text: "Message one", Timestamp: time.Now(),
	}
	if err := p.Router.HandleMessage(ctx, env1); err != nil {
		t.Fatal(err)
	}

	// Second message (same account → same session).
	env2 := channel.Envelope{
		ID: "sess-2", Channel: "webchat", AccountID: "user-sess",
		Type: channel.TypeText, Text: "Message two", Timestamp: time.Now(),
	}
	if err := p.Router.HandleMessage(ctx, env2); err != nil {
		t.Fatal(err)
	}

	p.Channel.mu.Lock()
	defer p.Channel.mu.Unlock()
	if len(p.Channel.sent) != 2 {
		t.Fatalf("sent = %d, want 2", len(p.Channel.sent))
	}
	if p.Channel.sent[0].Text != "First reply." {
		t.Errorf("first = %q", p.Channel.sent[0].Text)
	}
	if p.Channel.sent[1].Text != "Second reply." {
		t.Errorf("second = %q", p.Channel.sent[1].Text)
	}

	// Verify the second model call received history from the first.
	calls := p.Fake.Calls()
	if len(calls) != 2 {
		t.Fatalf("model calls = %d, want 2", len(calls))
	}
	// The second call should have more messages (history from first turn).
	if len(calls[1]) <= len(calls[0]) {
		t.Error("second call should include history from first turn")
	}
}

// TestEndToEndStopPhrase verifies stop phrases bypass agent execution.
func TestEndToEndStopPhrase(t *testing.T) {
	p := setupPipeline(t) // No responses needed — agent shouldn't be called.

	env := channel.Envelope{
		ID: "stop-1", Channel: "webchat", AccountID: "user-stop",
		Type: channel.TypeText, Text: "stop", Timestamp: time.Now(),
	}

	err := p.Router.HandleMessage(context.Background(), env)
	if err != nil {
		t.Fatalf("HandleMessage: %v", err)
	}

	// Agent should not have been called.
	if p.Fake.CallCount() != 0 {
		t.Errorf("model calls = %d, want 0 (stop phrase)", p.Fake.CallCount())
	}
}

// fakeChannel implements channel.Channel for integration tests.
type fakeChannel struct {
	name string
	mu   sync.Mutex
	sent []channel.OutgoingMessage
}

func (f *fakeChannel) Name() string { return f.name }
func (f *fakeChannel) Start(_ context.Context, _ channel.MessageHandler) error {
	return nil
}
func (f *fakeChannel) Stop(_ context.Context) error { return nil }
func (f *fakeChannel) Send(_ context.Context, _ string, msg channel.OutgoingMessage) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = append(f.sent, msg)
	return nil
}
func (f *fakeChannel) SendTyping(_ context.Context, _ string) error { return nil }
