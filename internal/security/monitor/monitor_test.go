package monitor_test

import (
	"context"
	"testing"
	"time"

	"github.com/LumabyteCo/aibutler/internal/security/monitor"
	"github.com/LumabyteCo/aibutler/testutil"
)

func TestRecordEvent(t *testing.T) {
	db := testutil.TestDB(t)
	m := monitor.New(db.Conn(), monitor.DefaultThresholds())
	ctx := context.Background()

	err := m.Record(ctx, monitor.EventFailedLogin, "warning", `{"user":"admin"}`, "192.168.1.1", "Mozilla/5.0")
	if err != nil {
		t.Fatalf("record event: %v", err)
	}

	// Record another event of different type.
	err = m.Record(ctx, monitor.EventCapabilityDenied, "info", `{"resource":"tool.shell"}`, "10.0.0.1", "")
	if err != nil {
		t.Fatalf("record second event: %v", err)
	}

	// Verify they were stored.
	events, err := m.RecentEvents(ctx, 10)
	if err != nil {
		t.Fatalf("recent events: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("events count = %d, want 2", len(events))
	}
	// Most recent first.
	if events[0].Type != monitor.EventCapabilityDenied {
		t.Errorf("first event type = %q, want %q", events[0].Type, monitor.EventCapabilityDenied)
	}
	if events[1].Severity != "warning" {
		t.Errorf("second event severity = %q, want 'warning'", events[1].Severity)
	}
}

func TestRecentEvents(t *testing.T) {
	db := testutil.TestDB(t)
	m := monitor.New(db.Conn(), monitor.DefaultThresholds())
	ctx := context.Background()

	// Insert 5 events.
	for i := 0; i < 5; i++ {
		if err := m.Record(ctx, monitor.EventFailedLogin, "info", "{}", "", ""); err != nil {
			t.Fatalf("record %d: %v", i, err)
		}
	}

	// Get only 3.
	events, err := m.RecentEvents(ctx, 3)
	if err != nil {
		t.Fatalf("recent events: %v", err)
	}
	if len(events) != 3 {
		t.Errorf("events count = %d, want 3", len(events))
	}
}

func TestEventsByType(t *testing.T) {
	db := testutil.TestDB(t)
	m := monitor.New(db.Conn(), monitor.DefaultThresholds())
	ctx := context.Background()

	// Record mixed events.
	_ = m.Record(ctx, monitor.EventFailedLogin, "warning", "{}", "", "")
	_ = m.Record(ctx, monitor.EventCapabilityDenied, "info", "{}", "", "")
	_ = m.Record(ctx, monitor.EventFailedLogin, "critical", "{}", "", "")
	_ = m.Record(ctx, monitor.EventCostSpike, "warning", "{}", "", "")

	// Filter by type.
	events, err := m.EventsByType(ctx, monitor.EventFailedLogin, time.Now().Add(-1*time.Hour))
	if err != nil {
		t.Fatalf("events by type: %v", err)
	}
	if len(events) != 2 {
		t.Errorf("failed login events = %d, want 2", len(events))
	}
	for _, e := range events {
		if e.Type != monitor.EventFailedLogin {
			t.Errorf("event type = %q, want %q", e.Type, monitor.EventFailedLogin)
		}
	}
}

func TestCheckThresholds(t *testing.T) {
	db := testutil.TestDB(t)
	// Set low thresholds for testing.
	thresholds := monitor.AlertThresholds{
		FailedLoginsPerMinute: 3,
		CostSpikeMultiplier:   2.0,
		UnusualToolCallCount:  2,
	}
	m := monitor.New(db.Conn(), thresholds)
	ctx := context.Background()

	// No alerts initially.
	alerts, err := m.CheckThresholds(ctx)
	if err != nil {
		t.Fatalf("check thresholds: %v", err)
	}
	if len(alerts) != 0 {
		t.Errorf("initial alerts = %d, want 0", len(alerts))
	}

	// Record enough failed logins to trigger alert.
	for i := 0; i < 4; i++ {
		_ = m.Record(ctx, monitor.EventFailedLogin, "warning", "{}", "10.0.0.1", "")
	}

	// Record enough unusual tool calls to trigger alert.
	for i := 0; i < 3; i++ {
		_ = m.Record(ctx, monitor.EventUnusualToolCall, "info", "{}", "", "")
	}

	alerts, err = m.CheckThresholds(ctx)
	if err != nil {
		t.Fatalf("check thresholds after events: %v", err)
	}
	if len(alerts) != 2 {
		t.Fatalf("alerts count = %d, want 2", len(alerts))
	}

	// Verify alert types.
	types := map[monitor.EventType]bool{}
	for _, a := range alerts {
		types[a.Type] = true
		if a.Count == 0 {
			t.Errorf("alert %q has zero count", a.Type)
		}
		if a.Message == "" {
			t.Errorf("alert %q has empty message", a.Type)
		}
	}
	if !types[monitor.EventFailedLogin] {
		t.Error("missing failed_login alert")
	}
	if !types[monitor.EventUnusualToolCall] {
		t.Error("missing unusual_tool_call alert")
	}
}
