package cli

import (
	"context"
	"testing"

	"github.com/LumabyteCo/aibutler/internal/plugin/registry"
	"github.com/LumabyteCo/aibutler/internal/tool"
)

// fakeToolLike satisfies registry.ToolLike for testing.
type fakeToolLike struct {
	name, desc, schema, cap string
}

func (f *fakeToolLike) Name() string        { return f.name }
func (f *fakeToolLike) Description() string  { return f.desc }
func (f *fakeToolLike) Schema() string       { return f.schema }
func (f *fakeToolLike) Capability() string   { return f.cap }
func (f *fakeToolLike) Execute(_ context.Context, input string) (string, error) {
	return "executed:" + input, nil
}

func TestToolRegAdapterRegister(t *testing.T) {
	reg := tool.NewRegistry()
	adapter := &toolRegAdapter{reg: reg}

	tl := &fakeToolLike{
		name:   "plugin.test.analyze",
		desc:   "Analyze data",
		schema: `{"type":"object"}`,
		cap:    "plugin.test.call",
	}

	adapter.Register(tl)

	// Tool should now be in the registry.
	got, ok := reg.Get("plugin.test.analyze")
	if !ok {
		t.Fatal("tool not found in registry after Register")
	}
	if got.Name() != "plugin.test.analyze" {
		t.Errorf("name = %q", got.Name())
	}
	if got.Description() != "Analyze data" {
		t.Errorf("desc = %q", got.Description())
	}
	if got.Schema() != `{"type":"object"}` {
		t.Errorf("schema = %q", got.Schema())
	}
	if got.Capability() != "plugin.test.call" {
		t.Errorf("capability = %q", got.Capability())
	}

	// Execute should delegate to the original ToolLike.
	out, err := got.Execute(context.Background(), "hello")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if out != "executed:hello" {
		t.Errorf("output = %q", out)
	}
}

func TestToolRegAdapterUnregisterPrefix(t *testing.T) {
	reg := tool.NewRegistry()
	adapter := &toolRegAdapter{reg: reg}

	// Register two tools with same prefix.
	adapter.Register(&fakeToolLike{name: "plugin.x.a"})
	adapter.Register(&fakeToolLike{name: "plugin.x.b"})
	adapter.Register(&fakeToolLike{name: "plugin.y.c"})

	adapter.UnregisterPrefix("plugin.x.")

	if _, ok := reg.Get("plugin.x.a"); ok {
		t.Error("plugin.x.a should have been removed")
	}
	if _, ok := reg.Get("plugin.x.b"); ok {
		t.Error("plugin.x.b should have been removed")
	}
	if _, ok := reg.Get("plugin.y.c"); !ok {
		t.Error("plugin.y.c should still exist")
	}
}

// Ensure fakeToolLike satisfies registry.ToolLike at compile time.
var _ registry.ToolLike = (*fakeToolLike)(nil)
