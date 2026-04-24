package model

import (
	"context"
	"sync"
	"time"
)

// BatchConfig controls how concurrent requests are collected and executed.
type BatchConfig struct {
	WindowDuration time.Duration // collect calls for this long (default 50ms)
	MaxBatchSize   int           // max concurrent in a batch (default 5)
}

// DefaultBatchConfig returns sensible defaults for batch execution.
func DefaultBatchConfig() BatchConfig {
	return BatchConfig{
		WindowDuration: 50 * time.Millisecond,
		MaxBatchSize:   5,
	}
}

type batchItem struct {
	fn     func() (string, error)
	result chan batchResult
}

type batchResult struct {
	value string
	err   error
}

// BatchExecutor collects concurrent calls within a time window and executes
// them in parallel, up to MaxBatchSize at a time.
type BatchExecutor struct {
	cfg     BatchConfig
	pending chan batchItem
	done    chan struct{}
	once    sync.Once
}

// NewBatchExecutor creates a BatchExecutor and starts its background loop.
func NewBatchExecutor(cfg BatchConfig) *BatchExecutor {
	if cfg.WindowDuration <= 0 {
		cfg.WindowDuration = 50 * time.Millisecond
	}
	if cfg.MaxBatchSize <= 0 {
		cfg.MaxBatchSize = 5
	}

	b := &BatchExecutor{
		cfg:     cfg,
		pending: make(chan batchItem, cfg.MaxBatchSize*2),
		done:    make(chan struct{}),
	}
	go b.loop()
	return b
}

// Execute submits a function to the batch executor and blocks until it completes.
func (b *BatchExecutor) Execute(ctx context.Context, fn func() (string, error)) (string, error) {
	item := batchItem{
		fn:     fn,
		result: make(chan batchResult, 1),
	}

	select {
	case b.pending <- item:
	case <-ctx.Done():
		return "", ctx.Err()
	}

	select {
	case r := <-item.result:
		return r.value, r.err
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

// Stop shuts down the batch executor.
func (b *BatchExecutor) Stop() {
	b.once.Do(func() {
		close(b.done)
	})
}

func (b *BatchExecutor) loop() {
	for {
		// Wait for first item.
		var batch []batchItem
		select {
		case item := <-b.pending:
			batch = append(batch, item)
		case <-b.done:
			return
		}

		// Collect more items within the window, up to MaxBatchSize.
		timer := time.NewTimer(b.cfg.WindowDuration)
	collect:
		for len(batch) < b.cfg.MaxBatchSize {
			select {
			case item := <-b.pending:
				batch = append(batch, item)
			case <-timer.C:
				break collect
			case <-b.done:
				timer.Stop()
				// Drain remaining items and cancel them.
				for _, item := range batch {
					item.result <- batchResult{err: context.Canceled}
				}
				return
			}
		}
		timer.Stop()

		// Execute all collected items in parallel.
		var wg sync.WaitGroup
		wg.Add(len(batch))
		for _, item := range batch {
			go func(it batchItem) {
				defer wg.Done()
				val, err := it.fn()
				it.result <- batchResult{value: val, err: err}
			}(item)
		}
		wg.Wait()
	}
}
