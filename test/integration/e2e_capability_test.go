//go:build integration

package integration

import (
	"testing"

	"github.com/LumabyteCo/aibutler/internal/agent"
	"github.com/LumabyteCo/aibutler/internal/capability"
)

// TestE2ECapabilityAllowed uses the default MessagingDefaults capabilities which
// include data.tasks.write. The model calls task.add and it should succeed,
// leaving a task row in the database.
func TestE2ECapabilityAllowed(t *testing.T) {
	p := setupPipelineWithOpts(t, pipelineOpts{
		Responses: []agent.Response{
			toolCallResponse("Adding a task.",
				tc("tc1", "task.add", `{"content":"E2E capability test task"}`),
			),
			finalResponse("Task has been added."),
		},
	})

	p.sendMsg(t, "Add a task: E2E capability test task")

	// Verify the task was persisted.
	taskCount := p.countRows(t, "user_tasks")
	if taskCount != 1 {
		t.Fatalf("user_tasks = %d, want 1", taskCount)
	}

	content := p.querySingleString(t, "SELECT content FROM user_tasks LIMIT 1")
	if content != "E2E capability test task" {
		t.Errorf("task content = %q, want 'E2E capability test task'", content)
	}

	// Verify model was called twice (tool call + final).
	if p.Fake.CallCount() != 2 {
		t.Errorf("model calls = %d, want 2", p.Fake.CallCount())
	}

	resp := p.lastResponse(t)
	if resp != "Task has been added." {
		t.Errorf("response = %q", resp)
	}
}

// TestE2ECapabilityDenied verifies that with an empty CapabilitySet,
// AvailableTools returns no data tools (they are filtered out). This is the
// enforcement mechanism: the model never sees denied tools.
func TestE2ECapabilityDenied(t *testing.T) {
	emptyCaps := capability.NewCapabilitySet(nil)

	p := setupPipelineWithOpts(t, pipelineOpts{
		Responses: []agent.Response{
			finalResponse("No tools available."),
		},
		CapOverride: emptyCaps,
	})

	// Verify AvailableTools returns no data tools with empty capabilities.
	engine := capability.NewEngine(nil)
	defs := p.Registry.Available(agent.ModeAuto, emptyCaps, engine)

	// With empty caps, no tools requiring capabilities should be returned.
	for _, d := range defs {
		if d.Name == "task.add" || d.Name == "task.list" || d.Name == "expense.log" {
			t.Errorf("tool %q should be filtered out with empty capabilities", d.Name)
		}
	}

	// A message still works — the model just has no tools.
	p.sendMsg(t, "Hello")
	resp := p.lastResponse(t)
	if resp != "No tools available." {
		t.Errorf("response = %q", resp)
	}
}

// TestE2ECapabilityAuditLogged uses WithAuditor=true and the default capabilities.
// The model calls task.add (which succeeds). We verify the FakeAuditor has at
// least 1 entry with Action="task.add".
func TestE2ECapabilityAuditLogged(t *testing.T) {
	p := setupPipelineWithOpts(t, pipelineOpts{
		WithAuditor: true,
		Responses: []agent.Response{
			toolCallResponse("Adding task.",
				tc("tc1", "task.add", `{"content":"Audited task"}`),
			),
			finalResponse("Done."),
		},
	})

	p.sendMsg(t, "Add a task: Audited task")

	// Verify audit entries were recorded.
	entries := p.Auditor.Entries()
	if len(entries) == 0 {
		t.Fatal("expected at least 1 audit entry, got 0")
	}

	foundTaskAdd := false
	for _, entry := range entries {
		if entry.Action == "task.add" {
			foundTaskAdd = true
			break
		}
	}
	if !foundTaskAdd {
		t.Errorf("no audit entry with Action='task.add'; entries: %v", entries)
	}
}

// TestE2ECapabilityReadVsWrite verifies that granting only data.tasks.read
// includes task.list in AvailableTools but excludes task.add (data.tasks.write).
func TestE2ECapabilityReadVsWrite(t *testing.T) {
	readOnlyCaps := capability.NewCapabilitySet([]capability.Capability{
		{Resource: "data.tasks.read"},
		// Intentionally no data.tasks.write.
	})

	p := setupPipelineWithOpts(t, pipelineOpts{
		Responses: []agent.Response{
			// Model calls task.list (read — allowed by AvailableTools).
			toolCallResponse("Listing tasks.",
				tc("tc1", "task.list", `{}`),
			),
			finalResponse("No tasks found."),
		},
		CapOverride: readOnlyCaps,
	})

	// Verify AvailableTools includes task.list but NOT task.add.
	engine := capability.NewEngine(nil)
	defs := p.Registry.Available(agent.ModeAuto, readOnlyCaps, engine)

	foundList := false
	foundAdd := false
	for _, d := range defs {
		if d.Name == "task.list" {
			foundList = true
		}
		if d.Name == "task.add" {
			foundAdd = true
		}
	}
	if !foundList {
		t.Error("task.list should be available with data.tasks.read capability")
	}
	if foundAdd {
		t.Error("task.add should NOT be available without data.tasks.write capability")
	}

	// Verify the read path works end-to-end.
	p.sendMsg(t, "List my tasks")
	if p.Fake.CallCount() != 2 {
		t.Errorf("model calls = %d, want 2", p.Fake.CallCount())
	}
	resp := p.lastResponse(t)
	if resp != "No tasks found." {
		t.Errorf("response = %q", resp)
	}
}

// TestE2ECapabilityToolFiltering verifies comprehensive tool filtering:
// with restricted capabilities, only the tools matching granted caps appear.
func TestE2ECapabilityToolFiltering(t *testing.T) {
	// Grant only data.tasks.read and data.contacts.read.
	restrictedCaps := capability.NewCapabilitySet([]capability.Capability{
		{Resource: "data.tasks.read"},
		{Resource: "data.contacts.read"},
	})

	p := setupPipelineWithOpts(t, pipelineOpts{
		Responses: []agent.Response{
			finalResponse("Filtered tools verified."),
		},
		CapOverride: restrictedCaps,
	})

	engine := capability.NewEngine(nil)
	defs := p.Registry.Available(agent.ModeAuto, restrictedCaps, engine)

	// Build a set of available tool names.
	available := make(map[string]bool)
	for _, d := range defs {
		available[d.Name] = true
	}

	// Read tools should be present.
	if !available["task.list"] {
		t.Error("task.list should be available (data.tasks.read granted)")
	}
	if !available["contact.search"] {
		t.Error("contact.search should be available (data.contacts.read granted)")
	}

	// Write tools should be absent.
	if available["task.add"] {
		t.Error("task.add should NOT be available (data.tasks.write not granted)")
	}
	if available["contact.add"] {
		t.Error("contact.add should NOT be available (data.contacts.write not granted)")
	}
	if available["expense.log"] {
		t.Error("expense.log should NOT be available (data.finance.write not granted)")
	}
	if available["journal.write"] {
		t.Error("journal.write should NOT be available (data.journal.write not granted)")
	}

	// Verify the pipeline still works for a simple message.
	p.sendMsg(t, "Hello")
	resp := p.lastResponse(t)
	if resp != "Filtered tools verified." {
		t.Errorf("response = %q", resp)
	}
}
