package plugin

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/LumabyteCo/aibutler/internal/plugin/host"
	"github.com/LumabyteCo/aibutler/internal/plugin/manifest"
)

func TestMockRuntimeConcurrentCalls(t *testing.T) {
	rt := NewMockRuntime()
	ctx := context.Background()
	m := &manifest.Manifest{Name: "concurrent-plugin", Version: "1.0.0"}
	if err := rt.Load(ctx, "/tmp", m, host.Deps{}); err != nil {
		t.Fatalf("load: %v", err)
	}

	// Set up 10 different functions.
	for i := 0; i < 10; i++ {
		fn := fmt.Sprintf("fn_%d", i)
		rt.SetResult("concurrent-plugin", fn, []byte(fmt.Sprintf(`{"idx":%d}`, i)))
	}

	// Call all 10 functions concurrently.
	var wg sync.WaitGroup
	errs := make([]error, 10)
	results := make([]string, 10)

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			fn := fmt.Sprintf("fn_%d", idx)
			out, err := rt.Call(ctx, "concurrent-plugin", fn, nil)
			errs[idx] = err
			results[idx] = string(out)
		}(i)
	}

	wg.Wait()

	for i := 0; i < 10; i++ {
		if errs[i] != nil {
			t.Errorf("fn_%d error: %v", i, errs[i])
		}
		expected := fmt.Sprintf(`{"idx":%d}`, i)
		if results[i] != expected {
			t.Errorf("fn_%d result = %q, want %q", i, results[i], expected)
		}
	}

	// All calls should have been recorded.
	calls := rt.Calls()
	if len(calls) != 10 {
		t.Errorf("total calls = %d, want 10", len(calls))
	}
}

func TestMockRuntimeConcurrentLoadUnload(t *testing.T) {
	rt := NewMockRuntime()
	ctx := context.Background()

	var wg sync.WaitGroup
	const n = 20

	// Load 20 different plugins concurrently.
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			name := fmt.Sprintf("plugin-%d", idx)
			m := &manifest.Manifest{Name: name, Version: "1.0.0"}
			_ = rt.Load(ctx, "/tmp", m, host.Deps{})
		}(i)
	}
	wg.Wait()

	loaded := rt.Loaded()
	if len(loaded) != n {
		t.Errorf("loaded = %d, want %d", len(loaded), n)
	}

	// Unload all concurrently.
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			_ = rt.Unload(ctx, fmt.Sprintf("plugin-%d", idx))
		}(i)
	}
	wg.Wait()

	if len(rt.Loaded()) != 0 {
		t.Errorf("loaded after unload = %d, want 0", len(rt.Loaded()))
	}
}
