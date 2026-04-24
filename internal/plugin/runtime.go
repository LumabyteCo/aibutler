package plugin

import (
	"context"
	"fmt"
	"log"
	"path/filepath"
	"sync"

	extism "github.com/extism/go-sdk"

	"github.com/LumabyteCo/aibutler/internal/plugin/host"
	"github.com/LumabyteCo/aibutler/internal/plugin/manifest"
)

// Runtime manages WASM plugin lifecycle.
type Runtime interface {
	Load(ctx context.Context, pluginDir string, m *manifest.Manifest, deps host.Deps) error
	Unload(ctx context.Context, name string) error
	Call(ctx context.Context, name, function string, input []byte) ([]byte, error)
	IsLoaded(name string) bool
	Loaded() []string
}

// loadedPlugin holds a loaded WASM plugin and its metadata.
type loadedPlugin struct {
	name     string
	manifest *manifest.Manifest
	plugin   *extism.Plugin
	mu       sync.Mutex // per-plugin mutex (Extism not thread-safe)
}

// ExtismRuntime implements Runtime using the Extism Go SDK.
type ExtismRuntime struct {
	mu      sync.RWMutex
	plugins map[string]*loadedPlugin
}

// NewExtismRuntime creates a new Extism-based plugin runtime.
func NewExtismRuntime() *ExtismRuntime {
	return &ExtismRuntime{
		plugins: make(map[string]*loadedPlugin),
	}
}

// Load compiles and instantiates a WASM plugin.
func (r *ExtismRuntime) Load(ctx context.Context, pluginDir string, m *manifest.Manifest, deps host.Deps) error {
	if m == nil {
		return fmt.Errorf("plugin manifest is nil")
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.plugins[m.Name]; exists {
		return fmt.Errorf("plugin %q already loaded", m.Name)
	}

	wasmPath := m.WASMPath
	if !filepath.IsAbs(wasmPath) {
		wasmPath = filepath.Join(pluginDir, wasmPath)
	}

	extManifest := extism.Manifest{
		Wasm: []extism.Wasm{
			extism.WasmFile{Path: wasmPath},
		},
	}

	hostFns := newHostFunctions(deps)

	plugin, err := extism.NewPlugin(ctx, extManifest, extism.PluginConfig{
		EnableWasi: true,
	}, hostFns)
	if err != nil {
		return fmt.Errorf("load plugin %q: %w", m.Name, err)
	}

	r.plugins[m.Name] = &loadedPlugin{
		name:     m.Name,
		manifest: m,
		plugin:   plugin,
	}
	return nil
}

// Unload closes and removes a loaded plugin.
func (r *ExtismRuntime) Unload(ctx context.Context, name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	lp, ok := r.plugins[name]
	if !ok {
		return fmt.Errorf("plugin %q not loaded", name)
	}

	lp.mu.Lock()
	defer lp.mu.Unlock()

	if err := lp.plugin.Close(ctx); err != nil {
		// Don't delete from map if close fails — resource might still be alive.
		return fmt.Errorf("close plugin %q: %w", name, err)
	}
	delete(r.plugins, name)
	return nil
}

// Call invokes a function exported by a loaded plugin.
func (r *ExtismRuntime) Call(ctx context.Context, name, function string, input []byte) ([]byte, error) {
	// Hold RLock during lookup so Unload (which needs write lock) can't remove
	// the plugin between lookup and acquiring the per-plugin mutex.
	r.mu.RLock()
	lp, ok := r.plugins[name]
	if !ok {
		r.mu.RUnlock()
		return nil, fmt.Errorf("plugin %q not loaded", name)
	}

	// Per-plugin mutex: Extism plugins are not thread-safe.
	// Acquire under RLock to prevent Unload race, then release RLock.
	lp.mu.Lock()
	r.mu.RUnlock()
	defer lp.mu.Unlock()

	if !lp.plugin.FunctionExists(function) {
		return nil, fmt.Errorf("plugin %q has no export %q", name, function)
	}

	_, output, err := lp.plugin.Call(function, input)
	if err != nil {
		return nil, fmt.Errorf("call %s.%s: %w", name, function, err)
	}
	return output, nil
}

// IsLoaded returns true if a plugin is currently loaded.
func (r *ExtismRuntime) IsLoaded(name string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, ok := r.plugins[name]
	return ok
}

// Loaded returns the names of all loaded plugins.
func (r *ExtismRuntime) Loaded() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.plugins))
	for name := range r.plugins {
		names = append(names, name)
	}
	return names
}

// newHostFunctions creates Extism host functions from Deps.
// Each host function reads JSON input from plugin memory, delegates to the
// corresponding host.Execute* function, then writes JSON output back.
func newHostFunctions(deps host.Deps) []extism.HostFunction {
	makeHostFn := func(name string, handler func(ctx context.Context, d *host.Deps, input []byte) ([]byte, error)) extism.HostFunction {
		return extism.NewHostFunctionWithStack(
			name,
			func(ctx context.Context, p *extism.CurrentPlugin, stack []uint64) {
				input, err := p.ReadBytes(stack[0])
				if err != nil {
					out, _ := p.WriteBytes([]byte(`{"error":"read input failed"}`))
					stack[0] = out
					return
				}

				output, err := handler(ctx, &deps, input)
				if err != nil {
					out, _ := p.WriteBytes([]byte(fmt.Sprintf(`{"error":%q}`, err.Error())))
					stack[0] = out
					return
				}

				out, werr := p.WriteBytes(output)
				if werr != nil {
					var ferr error
					out, ferr = p.WriteBytes([]byte(`{"error":"write output failed"}`))
					if ferr != nil {
						log.Printf("plugin host fn %q: write output failed: %v; fallback write also failed: %v", name, werr, ferr)
					}
				}
				stack[0] = out
			},
			[]extism.ValueType{extism.ValueTypePTR},
			[]extism.ValueType{extism.ValueTypePTR},
		)
	}

	return []extism.HostFunction{
		makeHostFn("aibutler_tool_call", host.ExecuteToolCall),
		makeHostFn("aibutler_log", host.ExecuteLog),
		makeHostFn("aibutler_config_get", host.ExecuteConfigGet),
		makeHostFn("aibutler_credential_get", host.ExecuteCredentialGet),
	}
}
