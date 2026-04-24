package plugin

import (
	"context"
	"fmt"
	"sync"

	"github.com/LumabyteCo/aibutler/internal/plugin/host"
	"github.com/LumabyteCo/aibutler/internal/plugin/manifest"
)

// MockCall records a single function call to a plugin.
type MockCall struct {
	Plugin   string
	Function string
	Input    []byte
}

// MockRuntime is a test double for Runtime that records calls and returns configured responses.
type MockRuntime struct {
	mu      sync.Mutex
	plugins map[string]*manifest.Manifest
	calls   []MockCall
	results map[string][]byte  // key: "plugin.function"
	errors  map[string]error   // key: "plugin.function"
}

// NewMockRuntime creates a MockRuntime with empty state.
func NewMockRuntime() *MockRuntime {
	return &MockRuntime{
		plugins: make(map[string]*manifest.Manifest),
		results: make(map[string][]byte),
		errors:  make(map[string]error),
	}
}

// SetResult configures the response for a plugin.function call.
func (m *MockRuntime) SetResult(plugin, function string, result []byte) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.results[plugin+"."+function] = result
}

// SetError configures an error for a plugin.function call.
func (m *MockRuntime) SetError(plugin, function string, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.errors[plugin+"."+function] = err
}

// Calls returns a copy of all recorded calls.
func (m *MockRuntime) Calls() []MockCall {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]MockCall, len(m.calls))
	copy(out, m.calls)
	return out
}

// Load records that a plugin was loaded.
func (m *MockRuntime) Load(_ context.Context, _ string, man *manifest.Manifest, _ host.Deps) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.plugins[man.Name]; exists {
		return fmt.Errorf("plugin %q already loaded", man.Name)
	}
	m.plugins[man.Name] = man
	return nil
}

// Unload records that a plugin was unloaded.
func (m *MockRuntime) Unload(_ context.Context, name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, ok := m.plugins[name]; !ok {
		return fmt.Errorf("plugin %q not loaded", name)
	}
	delete(m.plugins, name)
	return nil
}

// Call records the call and returns the configured result.
func (m *MockRuntime) Call(_ context.Context, name, function string, input []byte) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, ok := m.plugins[name]; !ok {
		return nil, fmt.Errorf("plugin %q not loaded", name)
	}

	m.calls = append(m.calls, MockCall{Plugin: name, Function: function, Input: input})

	key := name + "." + function
	if err, ok := m.errors[key]; ok {
		return nil, err
	}
	if result, ok := m.results[key]; ok {
		return result, nil
	}
	return []byte(`{}`), nil
}

// IsLoaded returns true if a plugin is loaded in the mock.
func (m *MockRuntime) IsLoaded(name string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.plugins[name]
	return ok
}

// Loaded returns the names of all loaded plugins.
func (m *MockRuntime) Loaded() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	names := make([]string, 0, len(m.plugins))
	for name := range m.plugins {
		names = append(names, name)
	}
	return names
}
