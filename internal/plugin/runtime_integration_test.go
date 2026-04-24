//go:build integration

package plugin

import (
	"context"
	"encoding/json"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/LumabyteCo/aibutler/internal/plugin/host"
	"github.com/LumabyteCo/aibutler/internal/plugin/manifest"
)

func testdataDir() string {
	_, filename, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(filename), "testdata")
}

func TestExtismRuntimeLoadAndCall(t *testing.T) {
	ctx := context.Background()
	rt := NewExtismRuntime()

	dir := testdataDir()
	m, err := manifest.ParseFile(filepath.Join(dir, "count_vowels.toml"))
	if err != nil {
		t.Fatalf("parse manifest: %v", err)
	}

	deps := host.Deps{
		Caps:       m.Capabilities,
		PluginName: m.Name,
	}

	if err := rt.Load(ctx, dir, m, deps); err != nil {
		t.Fatalf("load: %v", err)
	}
	defer rt.Unload(ctx, m.Name)

	if !rt.IsLoaded("count-vowels") {
		t.Error("plugin should be loaded")
	}

	out, err := rt.Call(ctx, "count-vowels", "count_vowels", []byte(`hello world`))
	if err != nil {
		t.Fatalf("call: %v", err)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(out, &result); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}

	count, ok := result["count"]
	if !ok {
		t.Fatalf("no count in result: %s", out)
	}
	// "hello world" has 3 vowels: e, o, o
	if count.(float64) != 3 {
		t.Errorf("count = %v, want 3", count)
	}
}

func TestExtismRuntimeFunctionNotFound(t *testing.T) {
	ctx := context.Background()
	rt := NewExtismRuntime()

	dir := testdataDir()
	m, err := manifest.ParseFile(filepath.Join(dir, "count_vowels.toml"))
	if err != nil {
		t.Fatalf("parse manifest: %v", err)
	}

	deps := host.Deps{Caps: m.Capabilities, PluginName: m.Name}
	if err := rt.Load(ctx, dir, m, deps); err != nil {
		t.Fatalf("load: %v", err)
	}
	defer rt.Unload(ctx, m.Name)

	_, err = rt.Call(ctx, "count-vowels", "nonexistent_function", nil)
	if err == nil {
		t.Error("expected error for nonexistent function")
	}
}
