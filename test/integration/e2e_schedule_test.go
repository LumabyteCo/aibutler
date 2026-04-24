//go:build integration

package integration

import (
	"strings"
	"testing"
	"time"

	"github.com/LumabyteCo/aibutler/internal/agent"
)

// TestE2EScheduleCreate verifies that schedule.create persists a new
// schedule into the schedules table.
func TestE2EScheduleCreate(t *testing.T) {
	p := setupPipelineWithOpts(t, pipelineOpts{
		WithSchedule: true,
		Responses: []agent.Response{
			// Turn 1: model calls schedule.create
			toolCallResponse("Creating schedule.",
				tc("tc1", "schedule.create", `{"name":"daily-backup","cron":"0 2 * * *","task":"run backup","channel":"webchat","account_id":"user-1"}`),
			),
			// Turn 1 continued: final reply
			finalResponse("Done! I've scheduled a daily backup at 2:00 AM."),
		},
	})

	p.sendMsg(t, "Schedule a daily backup at 2am")

	// Verify final response.
	resp := p.lastResponse(t)
	if !strings.Contains(resp, "daily backup") {
		t.Errorf("response = %q, want mention of daily backup", resp)
	}

	// Verify schedule was persisted.
	count := p.countRows(t, "schedules")
	if count != 1 {
		t.Fatalf("schedules rows = %d, want 1", count)
	}

	name := p.querySingleString(t, "SELECT name FROM schedules LIMIT 1")
	if name != "daily-backup" {
		t.Errorf("schedule name = %q, want 'daily-backup'", name)
	}

	cronExpr := p.querySingleString(t, "SELECT cron_expr FROM schedules LIMIT 1")
	if cronExpr != "0 2 * * *" {
		t.Errorf("cron_expr = %q, want '0 2 * * *'", cronExpr)
	}

	task := p.querySingleString(t, "SELECT task FROM schedules LIMIT 1")
	if task != "run backup" {
		t.Errorf("task = %q, want 'run backup'", task)
	}
}

// TestE2EScheduleList creates 2 schedules, then lists them.
func TestE2EScheduleList(t *testing.T) {
	p := setupPipelineWithOpts(t, pipelineOpts{
		WithSchedule: true,
		Responses: []agent.Response{
			// Turn 1: create first schedule
			toolCallResponse("Creating schedule 1.",
				tc("tc1", "schedule.create", `{"name":"daily-backup","cron":"0 2 * * *","task":"run backup","channel":"webchat","account_id":"user-1"}`),
			),
			finalResponse("Daily backup scheduled."),

			// Turn 2: create second schedule
			toolCallResponse("Creating schedule 2.",
				tc("tc2", "schedule.create", `{"name":"weekly-report","cron":"0 9 * * 1","task":"send weekly report","channel":"webchat","account_id":"user-1"}`),
			),
			finalResponse("Weekly report scheduled."),

			// Turn 3: list schedules
			toolCallResponse("Listing schedules.",
				tc("tc3", "schedule.list", `{}`),
			),
			finalResponse("You have 2 schedules: daily-backup and weekly-report."),
		},
	})

	// Turn 1: create first schedule.
	p.sendMsg(t, "Schedule daily backup at 2am")
	if p.countRows(t, "schedules") != 1 {
		t.Fatal("expected 1 schedule after first create")
	}

	// Sleep to ensure the second schedule gets a different millisecond-based ID.
	time.Sleep(2 * time.Millisecond)

	// Turn 2: create second schedule.
	p.sendMsg(t, "Schedule weekly report on Mondays at 9am")
	if p.countRows(t, "schedules") != 2 {
		t.Fatal("expected 2 schedules after second create")
	}

	// Turn 3: list.
	p.sendMsg(t, "Show me my schedules")

	resp := p.lastResponse(t)
	if !strings.Contains(resp, "2 schedules") {
		t.Errorf("list response = %q, want mention of 2 schedules", resp)
	}

	if p.responseCount() != 3 {
		t.Errorf("response count = %d, want 3", p.responseCount())
	}
}

// TestE2EScheduleDelete creates a schedule, then deletes it.
func TestE2EScheduleDelete(t *testing.T) {
	p := setupPipelineWithOpts(t, pipelineOpts{
		WithSchedule: true,
		Responses: []agent.Response{
			// Turn 1: create schedule
			toolCallResponse("Creating schedule.",
				tc("tc1", "schedule.create", `{"name":"daily-backup","cron":"0 2 * * *","task":"run backup","channel":"webchat","account_id":"user-1"}`),
			),
			finalResponse("Schedule created."),

			// Turn 2: delete schedule — we need the ID from the DB.
			// The schedule ID is generated as "sched_<timestamp>", so we
			// query it dynamically after turn 1 completes. For the test,
			// we supply a placeholder that the tool will receive. Since
			// the FakeModel provides exact tool call input, we first
			// query the DB after turn 1 to get the actual ID.
		},
	})

	// Turn 1: create.
	p.sendMsg(t, "Schedule daily backup at 2am")
	if p.countRows(t, "schedules") != 1 {
		t.Fatal("expected 1 schedule after create")
	}

	// Get the actual schedule ID from the DB.
	schedID := p.querySingleString(t, "SELECT id FROM schedules LIMIT 1")

	// Now add responses for turn 2 (delete).
	p.Fake.AddResponses(
		toolCallResponse("Deleting schedule.",
			tc("tc2", "schedule.delete", `{"id":"`+schedID+`"}`),
		),
		finalResponse("Schedule deleted."),
	)

	// Turn 2: delete.
	p.sendMsg(t, "Delete the daily backup schedule")

	// Verify row is deleted.
	count := p.countRows(t, "schedules")
	if count != 0 {
		t.Errorf("schedules rows = %d, want 0 after deletion", count)
	}

	resp := p.lastResponse(t)
	if !strings.Contains(resp, "deleted") {
		t.Errorf("response = %q, want mention of deleted", resp)
	}
}
