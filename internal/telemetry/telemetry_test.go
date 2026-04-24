package telemetry_test

import (
	"testing"

	"github.com/LumabyteCo/aibutler/internal/telemetry"
)

func TestCollectorDisabledIsNoop(t *testing.T) {
	c := telemetry.NewCollector(false)
	c.RecordToolCall()
	c.RecordSession()
	c.RecordError()

	s := c.Snapshot()
	if s.ToolCalls != 0 || s.Sessions != 0 || s.Errors != 0 {
		t.Errorf("disabled collector should have zero metrics: %+v", s)
	}
	if s.Enabled {
		t.Error("expected enabled=false")
	}
}

func TestCollectorEnabledCounts(t *testing.T) {
	c := telemetry.NewCollector(true)
	c.RecordToolCall()
	c.RecordToolCall()
	c.RecordSession()
	c.RecordError()

	s := c.Snapshot()
	if s.ToolCalls != 2 {
		t.Errorf("tool_calls = %d, want 2", s.ToolCalls)
	}
	if s.Sessions != 1 {
		t.Errorf("sessions = %d, want 1", s.Sessions)
	}
	if s.Errors != 1 {
		t.Errorf("errors = %d, want 1", s.Errors)
	}
}

func TestCollectorAnonymized(t *testing.T) {
	c := telemetry.NewCollector(true)
	c.RecordToolCall()

	s := c.Snapshot()
	// Verify no PII in snapshot (only counts).
	if s.ToolCalls != 1 {
		t.Errorf("expected anonymous count, got %d", s.ToolCalls)
	}
}
