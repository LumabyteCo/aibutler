package tool_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/LumabyteCo/aibutler/internal/agent"
	"github.com/LumabyteCo/aibutler/internal/capability"
	"github.com/LumabyteCo/aibutler/internal/tool"
)

// mutatingTool implements tool.PathMutator.
type mutatingTool struct {
	executed int
}

func (t *mutatingTool) Name() string        { return "test.mutate" }
func (t *mutatingTool) Description() string { return "test mutator" }
func (t *mutatingTool) Capability() string  { return "" }
func (t *mutatingTool) Schema() string      { return `{}` }
func (t *mutatingTool) Execute(_ context.Context, _ string) (string, error) {
	t.executed++
	return "mutated", nil
}
func (t *mutatingTool) MutatesPaths(input string) []string {
	if input == `{"path":""}` {
		return nil
	}
	return []string{"/tmp/target"}
}

type fakeCheckpointer struct {
	calls []string
	fail  bool
}

func (f *fakeCheckpointer) Snapshot(_ context.Context, toolName, path string) error {
	f.calls = append(f.calls, toolName+":"+path)
	if f.fail {
		return errors.New("disk full")
	}
	return nil
}

func newGuardDispatcher(t *testing.T, mt tool.Tool) (*tool.Dispatcher, *tool.Registry) {
	t.Helper()
	reg := tool.NewRegistry()
	reg.Register(mt)
	return tool.NewDispatcher(reg, capability.NewEngine(nil), nil), reg
}

func TestDispatcherSnapshotsBeforeMutation(t *testing.T) {
	mt := &mutatingTool{}
	d, _ := newGuardDispatcher(t, mt)
	cp := &fakeCheckpointer{}
	d.SetCheckpointer(cp)

	out, err := d.Execute(context.Background(), agent.ToolCall{Name: "test.mutate", Input: `{"path":"/tmp/target"}`})
	if err != nil || out != "mutated" {
		t.Fatalf("execute: %q, %v", out, err)
	}
	if len(cp.calls) != 1 || cp.calls[0] != "test.mutate:/tmp/target" {
		t.Fatalf("checkpointer calls = %v, want one snapshot of the target", cp.calls)
	}
	if mt.executed != 1 {
		t.Fatalf("tool executed %d times, want 1", mt.executed)
	}
}

func TestDispatcherFailsClosedWhenSnapshotFails(t *testing.T) {
	mt := &mutatingTool{}
	d, _ := newGuardDispatcher(t, mt)
	d.SetCheckpointer(&fakeCheckpointer{fail: true})

	_, err := d.Execute(context.Background(), agent.ToolCall{Name: "test.mutate", Input: `{"path":"/tmp/target"}`})
	if err == nil || !strings.Contains(err.Error(), "mutation aborted") {
		t.Fatalf("expected mutation-aborted error, got %v", err)
	}
	if mt.executed != 0 {
		t.Fatal("tool must not execute when its pre-image cannot be preserved")
	}
}

func TestDispatcherSkipsSnapshotWhenNoPaths(t *testing.T) {
	mt := &mutatingTool{}
	d, _ := newGuardDispatcher(t, mt)
	cp := &fakeCheckpointer{}
	d.SetCheckpointer(cp)

	if _, err := d.Execute(context.Background(), agent.ToolCall{Name: "test.mutate", Input: `{"path":""}`}); err != nil {
		t.Fatal(err)
	}
	if len(cp.calls) != 0 {
		t.Fatalf("no paths declared, but %d snapshots taken", len(cp.calls))
	}
}

func TestRepeatCallCircuitBreaker(t *testing.T) {
	mt := &mutatingTool{}
	d, _ := newGuardDispatcher(t, mt)
	d.SetRepeatCallLimit(3)

	call := agent.ToolCall{Name: "test.mutate", Input: `{"path":"/tmp/target"}`}
	ctx := context.Background()

	// First two identical calls execute.
	for i := 0; i < 2; i++ {
		out, err := d.Execute(ctx, call)
		if err != nil || out != "mutated" {
			t.Fatalf("call %d: %q, %v", i+1, out, err)
		}
	}
	// Third identical call gets the advisory, not execution.
	out, err := d.Execute(ctx, call)
	if err != nil {
		t.Fatalf("advisory should not be an error: %v", err)
	}
	if !strings.Contains(out, "blocked") || !strings.Contains(out, "identical") {
		t.Fatalf("expected repeat advisory, got %q", out)
	}
	if mt.executed != 2 {
		t.Fatalf("tool executed %d times, want 2", mt.executed)
	}

	// A different input resets the pattern.
	out, err = d.Execute(ctx, agent.ToolCall{Name: "test.mutate", Input: `{"path":"/tmp/other"}`})
	if err != nil || out != "mutated" {
		t.Fatalf("different input should execute: %q, %v", out, err)
	}
}

func TestRepeatCallBreakerDisabledByDefault(t *testing.T) {
	mt := &mutatingTool{}
	d, _ := newGuardDispatcher(t, mt)

	call := agent.ToolCall{Name: "test.mutate", Input: `{"path":"/tmp/target"}`}
	for i := 0; i < 5; i++ {
		if out, err := d.Execute(context.Background(), call); err != nil || out != "mutated" {
			t.Fatalf("call %d blocked with breaker disabled: %q, %v", i+1, out, err)
		}
	}
	if mt.executed != 5 {
		t.Fatalf("executed %d, want 5", mt.executed)
	}
}
