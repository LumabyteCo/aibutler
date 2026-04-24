package telemetry

import "sync/atomic"

// Collector collects anonymous usage metrics.
// When disabled (default), all operations are no-ops.
type Collector struct {
	enabled     bool
	toolCalls   atomic.Int64
	sessions    atomic.Int64
	errors      atomic.Int64
}

// NewCollector creates a telemetry collector.
func NewCollector(enabled bool) *Collector {
	return &Collector{enabled: enabled}
}

// RecordToolCall increments the tool call counter.
func (c *Collector) RecordToolCall() {
	if !c.enabled {
		return
	}
	c.toolCalls.Add(1)
}

// RecordSession increments the session counter.
func (c *Collector) RecordSession() {
	if !c.enabled {
		return
	}
	c.sessions.Add(1)
}

// RecordError increments the error counter.
func (c *Collector) RecordError() {
	if !c.enabled {
		return
	}
	c.errors.Add(1)
}

// Snapshot returns the current metrics.
func (c *Collector) Snapshot() Metrics {
	return Metrics{
		Enabled:   c.enabled,
		ToolCalls: c.toolCalls.Load(),
		Sessions:  c.sessions.Load(),
		Errors:    c.errors.Load(),
	}
}

// Metrics holds a snapshot of anonymized telemetry data.
type Metrics struct {
	Enabled   bool  `json:"enabled"`
	ToolCalls int64 `json:"tool_calls"`
	Sessions  int64 `json:"sessions"`
	Errors    int64 `json:"errors"`
}
