package cli

import (
	"context"
	"testing"

	"github.com/LumabyteCo/aibutler/internal/agent"
	"github.com/LumabyteCo/aibutler/internal/capability"
	"github.com/LumabyteCo/aibutler/internal/model"
	"github.com/LumabyteCo/aibutler/testutil"
)

func TestFactoryRunnerRunTask(t *testing.T) {
	app := testApp(t)
	fake := testutil.NewFakeModel(agent.Response{Content: "task result from factory"})
	factory := model.NewFactory(model.FactoryConfig{
		Composer: app.Composer,
		Model:    fake,
		Tools:    app.Dispatcher,
		Caps:     capability.NewCapabilitySet(capability.MessagingDefaults()),
		Tracker:  app.Tracker,
		DB:       app.DB.Conn(),
		Config:   app.Config,
	})

	runner := &factoryRunner{factory: factory}
	result, err := runner.RunTask(context.Background(), "summarize my week")
	if err != nil {
		t.Fatalf("RunTask: %v", err)
	}
	if result != "task result from factory" {
		t.Errorf("result = %q, want 'task result from factory'", result)
	}
	if fake.CallCount() != 1 {
		t.Errorf("model calls = %d, want 1", fake.CallCount())
	}
}

func TestFactoryRunnerTaskAppearsInMessages(t *testing.T) {
	app := testApp(t)
	fake := testutil.NewFakeModel(agent.Response{Content: "ok"})
	factory := model.NewFactory(model.FactoryConfig{
		Composer: app.Composer,
		Model:    fake,
		Tools:    app.Dispatcher,
		Caps:     capability.NewCapabilitySet(capability.MessagingDefaults()),
		Tracker:  app.Tracker,
		DB:       app.DB.Conn(),
		Config:   app.Config,
	})

	runner := &factoryRunner{factory: factory}
	runner.RunTask(context.Background(), "what is the weather?")

	calls := fake.Calls()
	if len(calls) == 0 {
		t.Fatal("expected at least one model call")
	}
	found := false
	for _, msg := range calls[0] {
		if msg.Content == "what is the weather?" {
			found = true
		}
	}
	if !found {
		t.Error("expected task to appear in model messages")
	}
}
