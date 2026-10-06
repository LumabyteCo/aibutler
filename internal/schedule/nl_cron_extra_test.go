package schedule_test

import (
	"context"
	"github.com/LumabyteCo/aibutler/internal/tool"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/LumabyteCo/aibutler/internal/schedule"
)

// TestNaturalTodayAt — "today at 15:00" must produce a date-anchored one-off
// cron (not a recurring schedule). And when the time is already past today,
// the create tool must warn so the model can tell the user.
func TestNaturalTodayAt(t *testing.T) {
	cron, err := schedule.NLToCron("today at 15:00")
	if err != nil {
		t.Fatalf("NLToCron: %v", err)
	}
	// Cron must be anchored to today's date (one-off), not recurring.
	parts := strings.Fields(cron)
	if len(parts) != 5 {
		t.Fatalf("expected 5-field cron, got %q", cron)
	}
	if parts[2] != strconv.Itoa(time.Now().Day()) || parts[3] != strconv.Itoa(int(time.Now().Month())) {
		t.Errorf("today at cron not anchored to today: %q", cron)
	}
}

// TestNaturalTomorrowAt — "tomorrow at 9:00" anchors to tomorrow.
func TestNaturalTomorrowAt(t *testing.T) {
	cron, err := schedule.NLToCron("tomorrow at 9:00")
	if err != nil {
		t.Fatalf("NLToCron: %v", err)
	}
	tomorrow := time.Now().AddDate(0, 0, 1)
	parts := strings.Fields(cron)
	if parts[2] != strconv.Itoa(tomorrow.Day()) {
		t.Errorf("tomorrow at cron not anchored to tomorrow: %q", cron)
	}
}

// TestCreatePastDueTodayWarns — the critical UX fix (FINDING-02): a user
// asking to schedule for a time that's already passed must get a warning,
// not silence or a burnt-turn loop.
//
// Uses the real registered tool (registry + store + nil model for the
// rule-based path) so the assertion tests the actual warning logic.
func TestCreatePastDueTodayWarns(t *testing.T) {
	reg := tool.NewRegistry()
	schedule.RegisterScheduleTools(reg, nil, nil) // nil store — the warning path returns before touching the DB
	ct, ok := reg.Get("schedule.create")
	if !ok {
		t.Fatal("schedule.create tool not registered")
	}

	input := `{"name":"Yesterday Briefing","natural":"today at 00:00","task":"Daily briefing","channel":"webchat","account_id":"user-1"}`
	output, err := ct.Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if !strings.Contains(output, "already passed") {
		t.Errorf("past-due schedule should warn: %q", output)
	}
	if !strings.Contains(output, "tomorrow") {
		t.Errorf("warning should name tomorrow: %q", output)
	}
}