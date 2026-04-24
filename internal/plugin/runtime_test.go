package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/LumabyteCo/aibutler/internal/agent"
	"github.com/LumabyteCo/aibutler/internal/plugin/host"
	"github.com/LumabyteCo/aibutler/internal/plugin/manifest"
)

// --- MockRuntime tests ---

func TestMockRuntimeLoadAndUnload(t *testing.T) {
	rt := NewMockRuntime()
	ctx := context.Background()
	m := &manifest.Manifest{Name: "test-plugin", Version: "1.0.0"}

	if err := rt.Load(ctx, "/tmp", m, host.Deps{}); err != nil {
		t.Fatalf("load: %v", err)
	}
	if !rt.IsLoaded("test-plugin") {
		t.Error("plugin should be loaded")
	}

	loaded := rt.Loaded()
	if len(loaded) != 1 || loaded[0] != "test-plugin" {
		t.Errorf("loaded = %v, want [test-plugin]", loaded)
	}

	if err := rt.Unload(ctx, "test-plugin"); err != nil {
		t.Fatalf("unload: %v", err)
	}
	if rt.IsLoaded("test-plugin") {
		t.Error("plugin should not be loaded after unload")
	}
}

func TestMockRuntimeLoadDuplicate(t *testing.T) {
	rt := NewMockRuntime()
	ctx := context.Background()
	m := &manifest.Manifest{Name: "dup", Version: "1.0.0"}

	_ = rt.Load(ctx, "/tmp", m, host.Deps{})
	err := rt.Load(ctx, "/tmp", m, host.Deps{})
	if err == nil {
		t.Error("expected error for duplicate load")
	}
}

func TestMockRuntimeUnloadNotLoaded(t *testing.T) {
	rt := NewMockRuntime()
	err := rt.Unload(context.Background(), "missing")
	if err == nil {
		t.Error("expected error for unloading missing plugin")
	}
}

func TestMockRuntimeCallRecording(t *testing.T) {
	rt := NewMockRuntime()
	ctx := context.Background()
	m := &manifest.Manifest{Name: "p1", Version: "1.0.0"}
	_ = rt.Load(ctx, "/tmp", m, host.Deps{})

	rt.SetResult("p1", "fn1", []byte(`{"count":5}`))

	out, err := rt.Call(ctx, "p1", "fn1", []byte(`{"input":"hello"}`))
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if string(out) != `{"count":5}` {
		t.Errorf("output = %s", out)
	}

	calls := rt.Calls()
	if len(calls) != 1 {
		t.Fatalf("calls = %d, want 1", len(calls))
	}
	if calls[0].Plugin != "p1" || calls[0].Function != "fn1" {
		t.Errorf("call = %+v", calls[0])
	}
}

func TestMockRuntimeCallNotLoaded(t *testing.T) {
	rt := NewMockRuntime()
	_, err := rt.Call(context.Background(), "missing", "fn", nil)
	if err == nil {
		t.Error("expected error for call to unloaded plugin")
	}
}

func TestMockRuntimeCallError(t *testing.T) {
	rt := NewMockRuntime()
	ctx := context.Background()
	m := &manifest.Manifest{Name: "p", Version: "1.0.0"}
	_ = rt.Load(ctx, "/tmp", m, host.Deps{})

	rt.SetError("p", "bad", errors.New("wasm trap"))

	_, err := rt.Call(ctx, "p", "bad", nil)
	if err == nil || err.Error() != "wasm trap" {
		t.Errorf("err = %v, want wasm trap", err)
	}
}

func TestMockRuntimeCallDefaultResult(t *testing.T) {
	rt := NewMockRuntime()
	ctx := context.Background()
	m := &manifest.Manifest{Name: "p", Version: "1.0.0"}
	_ = rt.Load(ctx, "/tmp", m, host.Deps{})

	out, err := rt.Call(ctx, "p", "any_fn", nil)
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if string(out) != `{}` {
		t.Errorf("default result = %s, want {}", out)
	}
}

// --- PluginTool tests ---

func TestPluginToolName(t *testing.T) {
	rt := NewMockRuntime()
	def := manifest.ToolDef{Name: "analyze", Description: "Analyze data", Schema: `{"type":"object"}`, Function: "run_analyze"}
	pt := NewPluginTool(rt, "my-plugin", def)

	if pt.Name() != "plugin.my-plugin.analyze" {
		t.Errorf("name = %q", pt.Name())
	}
	if pt.Description() != "Analyze data" {
		t.Errorf("description = %q", pt.Description())
	}
	if pt.Schema() != `{"type":"object"}` {
		t.Errorf("schema = %q", pt.Schema())
	}
	if pt.Capability() != "plugin.my-plugin.call" {
		t.Errorf("capability = %q", pt.Capability())
	}
}

func TestPluginToolExecute(t *testing.T) {
	rt := NewMockRuntime()
	ctx := context.Background()
	m := &manifest.Manifest{Name: "p", Version: "1.0.0"}
	_ = rt.Load(ctx, "/tmp", m, host.Deps{})

	rt.SetResult("p", "run_analyze", []byte(`{"result":"done"}`))

	def := manifest.ToolDef{Name: "analyze", Function: "run_analyze"}
	pt := NewPluginTool(rt, "p", def)

	out, err := pt.Execute(ctx, `{"data":"test"}`)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if out != `{"result":"done"}` {
		t.Errorf("output = %s", out)
	}
}

func TestPluginToolDefaultsFunction(t *testing.T) {
	rt := NewMockRuntime()
	ctx := context.Background()
	m := &manifest.Manifest{Name: "p", Version: "1.0.0"}
	_ = rt.Load(ctx, "/tmp", m, host.Deps{})

	rt.SetResult("p", "my_tool", []byte(`ok`))

	def := manifest.ToolDef{Name: "my_tool"} // Function defaults to Name
	pt := NewPluginTool(rt, "p", def)

	_, err := pt.Execute(ctx, `{}`)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}

	calls := rt.Calls()
	if len(calls) != 1 || calls[0].Function != "my_tool" {
		t.Errorf("calls = %+v", calls)
	}
}

func TestPluginToolExecuteError(t *testing.T) {
	rt := NewMockRuntime()
	ctx := context.Background()
	m := &manifest.Manifest{Name: "p", Version: "1.0.0"}
	_ = rt.Load(ctx, "/tmp", m, host.Deps{})
	rt.SetError("p", "fail", errors.New("plugin crashed"))

	def := manifest.ToolDef{Name: "fail", Function: "fail"}
	pt := NewPluginTool(rt, "p", def)

	_, err := pt.Execute(ctx, `{}`)
	if err == nil {
		t.Error("expected error from plugin crash")
	}
}

// --- PluginModelAdapter tests ---

func TestPluginModelAdapterComplete(t *testing.T) {
	rt := NewMockRuntime()
	ctx := context.Background()
	m := &manifest.Manifest{Name: "llm", Version: "1.0.0"}
	_ = rt.Load(ctx, "/tmp", m, host.Deps{})

	expectedResp := agent.Response{Content: "Hello!", TokensIn: 10, TokensOut: 5}
	respBytes, _ := json.Marshal(expectedResp)
	rt.SetResult("llm", "complete", respBytes)

	adapter := NewPluginModelAdapter(rt, "llm", "", []string{"custom-v1"})

	resp, err := adapter.Complete(ctx, []agent.Message{{Role: "user", Content: "Hi"}})
	if err != nil {
		t.Fatalf("complete: %v", err)
	}
	if resp.Content != "Hello!" {
		t.Errorf("content = %q", resp.Content)
	}
	if resp.TokensIn != 10 || resp.TokensOut != 5 {
		t.Errorf("tokens = %d/%d", resp.TokensIn, resp.TokensOut)
	}
}

func TestPluginModelAdapterModels(t *testing.T) {
	rt := NewMockRuntime()
	adapter := NewPluginModelAdapter(rt, "llm", "model_complete", []string{"a", "b"})

	models := adapter.Models()
	if len(models) != 2 || models[0] != "a" || models[1] != "b" {
		t.Errorf("models = %v", models)
	}
}

func TestPluginModelAdapterDefaultFunction(t *testing.T) {
	rt := NewMockRuntime()
	ctx := context.Background()
	m := &manifest.Manifest{Name: "llm", Version: "1.0.0"}
	_ = rt.Load(ctx, "/tmp", m, host.Deps{})

	respBytes, _ := json.Marshal(agent.Response{Content: "ok"})
	rt.SetResult("llm", "complete", respBytes)

	adapter := NewPluginModelAdapter(rt, "llm", "", nil) // defaults to "complete"

	_, err := adapter.Complete(ctx, []agent.Message{{Role: "user", Content: "test"}})
	if err != nil {
		t.Fatalf("complete: %v", err)
	}

	calls := rt.Calls()
	if len(calls) != 1 || calls[0].Function != "complete" {
		t.Errorf("calls = %+v", calls)
	}
}

func TestPluginModelAdapterError(t *testing.T) {
	rt := NewMockRuntime()
	ctx := context.Background()
	m := &manifest.Manifest{Name: "llm", Version: "1.0.0"}
	_ = rt.Load(ctx, "/tmp", m, host.Deps{})
	rt.SetError("llm", "complete", errors.New("model error"))

	adapter := NewPluginModelAdapter(rt, "llm", "", nil)

	_, err := adapter.Complete(ctx, []agent.Message{{Role: "user", Content: "test"}})
	if err == nil {
		t.Error("expected error from model adapter")
	}
}

// --- ExtismRuntime nil manifest guard ---

func TestExtismRuntimeLoadNilManifest(t *testing.T) {
	rt := NewExtismRuntime()
	err := rt.Load(context.Background(), "/tmp", nil, host.Deps{})
	if err == nil {
		t.Fatal("expected error for nil manifest")
	}
	if !containsStr(err.Error(), "nil") {
		t.Errorf("error should mention nil, got: %v", err)
	}
}

func containsStr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
