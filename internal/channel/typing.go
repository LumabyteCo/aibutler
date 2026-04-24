package channel

import (
	"context"
	"sync"
	"time"
)

// TypingManager sends periodic typing indicators on a channel.
type TypingManager struct {
	interval time.Duration
	timeout  time.Duration

	mu     sync.Mutex
	active map[string]context.CancelFunc
}

// NewTypingManager creates a typing manager with the given heartbeat interval and timeout.
func NewTypingManager(interval, timeout time.Duration) *TypingManager {
	return &TypingManager{
		interval: interval,
		timeout:  timeout,
		active:   make(map[string]context.CancelFunc),
	}
}

// Start begins sending typing indicators for the given account on the channel.
// If typing is already active for the account, it is restarted.
func (tm *TypingManager) Start(ctx context.Context, ch Channel, accountID string) {
	tm.Stop(accountID)

	ctx, cancel := context.WithTimeout(ctx, tm.timeout)

	tm.mu.Lock()
	tm.active[accountID] = cancel
	tm.mu.Unlock()

	go tm.heartbeat(ctx, ch, accountID)
}

// Stop cancels the typing indicator for the given account.
func (tm *TypingManager) Stop(accountID string) {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	if cancel, ok := tm.active[accountID]; ok {
		cancel()
		delete(tm.active, accountID)
	}
}

// StopAll cancels all active typing indicators.
func (tm *TypingManager) StopAll() {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	for id, cancel := range tm.active {
		cancel()
		delete(tm.active, id)
	}
}

func (tm *TypingManager) heartbeat(ctx context.Context, ch Channel, accountID string) {
	defer tm.Stop(accountID)

	// Send immediately once.
	_ = ch.SendTyping(ctx, accountID)

	ticker := time.NewTicker(tm.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_ = ch.SendTyping(ctx, accountID)
		}
	}
}
