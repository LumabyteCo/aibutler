package testutil

import (
	"sync"
	"time"
)

// Clock provides deterministic time for tests.
type Clock struct {
	mu  sync.Mutex
	now time.Time
}

// NewClock creates a clock starting at the given time.
func NewClock(start time.Time) *Clock {
	return &Clock{now: start}
}

// Now returns the current time.
func (c *Clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

// Advance moves the clock forward by the given duration.
func (c *Clock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}
