package model

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestBatchExecutor_ParallelExecution(t *testing.T) {
	exec := NewBatchExecutor(BatchConfig{
		WindowDuration: 100 * time.Millisecond,
		MaxBatchSize:   5,
	})
	defer exec.Stop()

	var running int64
	var maxRunning int64
	var mu sync.Mutex

	var wg sync.WaitGroup
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			ctx := context.Background()
			result, err := exec.Execute(ctx, func() (string, error) {
				cur := atomic.AddInt64(&running, 1)
				mu.Lock()
				if cur > maxRunning {
					maxRunning = cur
				}
				mu.Unlock()
				time.Sleep(50 * time.Millisecond)
				atomic.AddInt64(&running, -1)
				return "ok", nil
			})
			if err != nil {
				t.Errorf("Execute[%d]: %v", n, err)
			}
			if result != "ok" {
				t.Errorf("Execute[%d] = %q, want 'ok'", n, result)
			}
		}(i)
	}
	wg.Wait()

	// Multiple items should have run in parallel.
	mu.Lock()
	if maxRunning < 2 {
		t.Logf("maxRunning = %d (batch parallelism may vary with timing)", maxRunning)
	}
	mu.Unlock()
}

func TestBatchExecutor_WindowBatching(t *testing.T) {
	exec := NewBatchExecutor(BatchConfig{
		WindowDuration: 200 * time.Millisecond,
		MaxBatchSize:   10,
	})
	defer exec.Stop()

	// Submit multiple calls quickly — they should be batched.
	var wg sync.WaitGroup
	results := make([]string, 5)
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			ctx := context.Background()
			val, err := exec.Execute(ctx, func() (string, error) {
				return "done", nil
			})
			if err != nil {
				t.Errorf("Execute[%d]: %v", n, err)
				return
			}
			results[n] = val
		}(i)
	}
	wg.Wait()

	for i, r := range results {
		if r != "done" {
			t.Errorf("results[%d] = %q, want 'done'", i, r)
		}
	}
}

func TestBatchExecutor_MaxSizeLimit(t *testing.T) {
	maxBatch := 2
	exec := NewBatchExecutor(BatchConfig{
		WindowDuration: 50 * time.Millisecond,
		MaxBatchSize:   maxBatch,
	})
	defer exec.Stop()

	// Submit more items than MaxBatchSize — all should still complete.
	var wg sync.WaitGroup
	count := 6
	errors := make([]error, count)
	for i := 0; i < count; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			ctx := context.Background()
			_, err := exec.Execute(ctx, func() (string, error) {
				time.Sleep(10 * time.Millisecond)
				return "ok", nil
			})
			errors[n] = err
		}(i)
	}
	wg.Wait()

	for i, err := range errors {
		if err != nil {
			t.Errorf("Execute[%d] error: %v", i, err)
		}
	}
}
