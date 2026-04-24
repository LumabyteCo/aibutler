//go:build integration

package integration

import (
	"strings"
	"testing"
)

// TestE2EMultiTurnTaskWorkflow verifies a 3-turn task workflow:
// Turn 1: task.add("Buy milk") -> Turn 2: task.add("Walk dog") -> Turn 3: task.list -> 2 tasks.
func TestE2EMultiTurnTaskWorkflow(t *testing.T) {
	p := setupE2E(t,
		// Turn 1: tool call + final
		toolCallResponse("Adding task.",
			tc("t1", "task.add", `{"content":"Buy milk"}`),
		),
		finalResponse("Added Buy milk."),
		// Turn 2: tool call + final
		toolCallResponse("Adding another task.",
			tc("t2", "task.add", `{"content":"Walk dog"}`),
		),
		finalResponse("Added Walk dog."),
		// Turn 3: tool call + final
		toolCallResponse("Listing tasks.",
			tc("t3", "task.list", `{}`),
		),
		finalResponse("You have 2 tasks."),
	)

	p.sendMsg(t, "Add task: Buy milk")
	p.sendMsg(t, "Add task: Walk dog")
	p.sendMsg(t, "List my tasks")

	// Verify 2 tasks in DB.
	count := p.countRows(t, "user_tasks")
	if count != 2 {
		t.Errorf("user_tasks rows = %d, want 2", count)
	}

	// Verify 3 responses sent.
	if p.responseCount() != 3 {
		t.Errorf("responses = %d, want 3", p.responseCount())
	}

	// Verify 6 model calls (2 per turn).
	if p.Fake.CallCount() != 6 {
		t.Errorf("model calls = %d, want 6", p.Fake.CallCount())
	}
}

// TestE2EMultiTurnJournalWorkflow verifies a 2-turn journal workflow:
// Turn 1: journal.write -> Turn 2: journal.read -> entry present.
func TestE2EMultiTurnJournalWorkflow(t *testing.T) {
	p := setupE2E(t,
		// Turn 1: write
		toolCallResponse("Writing journal entry.",
			tc("j1", "journal.write", `{"content":"Today was great","mood":"happy"}`),
		),
		finalResponse("Journal entry saved."),
		// Turn 2: read
		toolCallResponse("Reading journal.",
			tc("j2", "journal.read", `{}`),
		),
		finalResponse("Here is your journal entry."),
	)

	p.sendMsg(t, "Write journal: Today was great, mood happy")
	p.sendMsg(t, "Read my journal")

	// Verify 1 journal entry in DB.
	count := p.countRows(t, "user_journal")
	if count != 1 {
		t.Errorf("user_journal rows = %d, want 1", count)
	}

	// Verify the second model call (turn 2, call index 2) tool result contains "Today was great".
	calls := p.Fake.Calls()
	if len(calls) < 4 {
		t.Fatalf("model calls = %d, want >= 4", len(calls))
	}
	// The 4th model call (index 3) is the final response after journal.read.
	// It should have a tool result message containing the journal content.
	found := false
	for _, msg := range calls[3] {
		if msg.Role == "tool" && strings.Contains(msg.Content, "Today was great") {
			found = true
			break
		}
	}
	if !found {
		t.Error("journal.read tool result should contain 'Today was great'")
	}
}

// TestE2EMultiTurnContactWorkflow verifies a 3-turn contact workflow:
// Turn 1: contact.add(Sarah) -> Turn 2: contact.update(id:1, phone) -> Turn 3: contact.search -> updated phone.
func TestE2EMultiTurnContactWorkflow(t *testing.T) {
	p := setupE2E(t,
		// Turn 1: add
		toolCallResponse("Adding contact.",
			tc("c1", "contact.add", `{"name":"Sarah","email":"sarah@example.com"}`),
		),
		finalResponse("Contact Sarah added."),
		// Turn 2: update
		toolCallResponse("Updating contact.",
			tc("c2", "contact.update", `{"id":1,"phone":"+1234567890"}`),
		),
		finalResponse("Contact updated."),
		// Turn 3: search
		toolCallResponse("Searching contacts.",
			tc("c3", "contact.search", `{"query":"Sarah"}`),
		),
		finalResponse("Found Sarah."),
	)

	p.sendMsg(t, "Add contact Sarah, email sarah@example.com")
	p.sendMsg(t, "Update Sarah's phone to +1234567890")
	p.sendMsg(t, "Search for Sarah")

	// Verify 1 contact in DB.
	count := p.countRows(t, "user_contacts")
	if count != 1 {
		t.Errorf("user_contacts rows = %d, want 1", count)
	}

	// Verify the phone was updated.
	phone := p.querySingleString(t, "SELECT phone FROM user_contacts WHERE name = 'Sarah'")
	if phone != "+1234567890" {
		t.Errorf("phone = %q, want +1234567890", phone)
	}

	// Verify the search result in model call includes the phone number.
	calls := p.Fake.Calls()
	if len(calls) < 6 {
		t.Fatalf("model calls = %d, want 6", len(calls))
	}
	// The 6th call (index 5) is the final after contact.search; check tool result in it.
	found := false
	for _, msg := range calls[5] {
		if msg.Role == "tool" && strings.Contains(msg.Content, "+1234567890") {
			found = true
			break
		}
	}
	if !found {
		t.Error("contact.search tool result should contain phone '+1234567890'")
	}
}

// TestE2EMultiTurnExpenseWorkflow verifies a 3-turn expense workflow:
// Turn 1: expense.log(food,$25) -> Turn 2: expense.log(transport,$10) -> Turn 3: expense.summary -> 2 categories.
func TestE2EMultiTurnExpenseWorkflow(t *testing.T) {
	p := setupE2E(t,
		// Turn 1: log food
		toolCallResponse("Logging expense.",
			tc("e1", "expense.log", `{"amount":25,"category":"food","description":"lunch"}`),
		),
		finalResponse("Expense logged: food $25."),
		// Turn 2: log transport
		toolCallResponse("Logging another expense.",
			tc("e2", "expense.log", `{"amount":10,"category":"transport","description":"bus"}`),
		),
		finalResponse("Expense logged: transport $10."),
		// Turn 3: summary
		toolCallResponse("Getting summary.",
			tc("e3", "expense.summary", `{}`),
		),
		finalResponse("Here is your expense summary."),
	)

	p.sendMsg(t, "Log $25 for food")
	p.sendMsg(t, "Log $10 for transport")
	p.sendMsg(t, "Show expense summary")

	// Verify 2 expenses in DB.
	count := p.countRows(t, "user_expenses")
	if count != 2 {
		t.Errorf("user_expenses rows = %d, want 2", count)
	}

	// Verify the summary tool result contains both categories.
	calls := p.Fake.Calls()
	if len(calls) < 6 {
		t.Fatalf("model calls = %d, want 6", len(calls))
	}
	found := 0
	for _, msg := range calls[5] {
		if msg.Role == "tool" {
			if strings.Contains(msg.Content, "food") {
				found++
			}
			if strings.Contains(msg.Content, "transport") {
				found++
			}
		}
	}
	if found < 2 {
		t.Error("expense.summary tool result should contain both 'food' and 'transport' categories")
	}
}

// TestE2EMultiTurnHabitWorkflow verifies a 3-turn habit workflow:
// Turn 1: habit.create("Exercise") -> Turn 2: habit.log("Exercise") -> Turn 3: habit.streak -> streak >= 1.
func TestE2EMultiTurnHabitWorkflow(t *testing.T) {
	p := setupE2E(t,
		// Turn 1: create
		toolCallResponse("Creating habit.",
			tc("h1", "habit.create", `{"name":"Exercise","frequency":"daily"}`),
		),
		finalResponse("Habit Exercise created."),
		// Turn 2: log
		toolCallResponse("Logging habit.",
			tc("h2", "habit.log", `{"name":"Exercise"}`),
		),
		finalResponse("Logged Exercise for today."),
		// Turn 3: streak
		toolCallResponse("Checking streak.",
			tc("h3", "habit.streak", `{"name":"Exercise"}`),
		),
		finalResponse("Your streak is 1 day."),
	)

	p.sendMsg(t, "Create habit: Exercise")
	p.sendMsg(t, "Log Exercise for today")
	p.sendMsg(t, "What is my Exercise streak?")

	// Verify habit exists.
	count := p.countRows(t, "user_habits")
	if count != 1 {
		t.Errorf("user_habits rows = %d, want 1", count)
	}

	// Verify habit log exists.
	logCount := p.countRows(t, "user_habit_logs")
	if logCount != 1 {
		t.Errorf("user_habit_logs rows = %d, want 1", logCount)
	}

	// Verify the streak tool result contains streak info.
	calls := p.Fake.Calls()
	if len(calls) < 6 {
		t.Fatalf("model calls = %d, want 6", len(calls))
	}
	found := false
	for _, msg := range calls[5] {
		if msg.Role == "tool" && strings.Contains(msg.Content, "streak") {
			found = true
			break
		}
	}
	if !found {
		t.Error("habit.streak tool result should contain 'streak'")
	}
}

// TestE2EMultiTurnReminderWorkflow verifies a 3-turn reminder workflow:
// Turn 1: reminder.set -> Turn 2: reminder.list -> Turn 3: reminder.cancel(id:1) -> cancelled.
func TestE2EMultiTurnReminderWorkflow(t *testing.T) {
	p := setupE2E(t,
		// Turn 1: set
		toolCallResponse("Setting reminder.",
			tc("r1", "reminder.set", `{"message":"Call dentist","remind_at":"2026-04-01T10:00:00Z"}`),
		),
		finalResponse("Reminder set."),
		// Turn 2: list
		toolCallResponse("Listing reminders.",
			tc("r2", "reminder.list", `{}`),
		),
		finalResponse("You have 1 reminder."),
		// Turn 3: cancel
		toolCallResponse("Cancelling reminder.",
			tc("r3", "reminder.cancel", `{"id":1}`),
		),
		finalResponse("Reminder cancelled."),
	)

	p.sendMsg(t, "Remind me to call dentist on April 1st")
	p.sendMsg(t, "List my reminders")
	p.sendMsg(t, "Cancel reminder 1")

	// Verify the reminder status is now cancelled.
	status := p.querySingleString(t, "SELECT status FROM user_reminders WHERE id = 1")
	if status != "cancelled" {
		t.Errorf("reminder status = %q, want 'cancelled'", status)
	}

	// Verify 6 model calls.
	if p.Fake.CallCount() != 6 {
		t.Errorf("model calls = %d, want 6", p.Fake.CallCount())
	}
}

// TestE2EMultiTurnPlaceWorkflow verifies a 3-turn place workflow:
// Turn 1: place.save("Cafe Roma") -> Turn 2: place.update(id:1, rating:5) -> Turn 3: place.search("Roma") -> verify.
func TestE2EMultiTurnPlaceWorkflow(t *testing.T) {
	p := setupE2E(t,
		// Turn 1: save
		toolCallResponse("Saving place.",
			tc("p1", "place.save", `{"name":"Cafe Roma","address":"123 Main St","category":"cafe"}`),
		),
		finalResponse("Place saved."),
		// Turn 2: update
		toolCallResponse("Updating place.",
			tc("p2", "place.update", `{"id":1,"rating":5}`),
		),
		finalResponse("Place updated with rating 5."),
		// Turn 3: search
		toolCallResponse("Searching places.",
			tc("p3", "place.search", `{"query":"Roma"}`),
		),
		finalResponse("Found Cafe Roma."),
	)

	p.sendMsg(t, "Save place Cafe Roma at 123 Main St")
	p.sendMsg(t, "Rate Cafe Roma 5 stars")
	p.sendMsg(t, "Search for Roma")

	// Verify place exists with updated rating.
	rating := p.querySingleInt(t, "SELECT rating FROM user_places WHERE id = 1")
	if rating != 5 {
		t.Errorf("rating = %d, want 5", rating)
	}

	// Verify the search result includes the place with rating.
	calls := p.Fake.Calls()
	if len(calls) < 6 {
		t.Fatalf("model calls = %d, want 6", len(calls))
	}
	found := false
	for _, msg := range calls[5] {
		if msg.Role == "tool" && strings.Contains(msg.Content, "Cafe Roma") {
			found = true
			break
		}
	}
	if !found {
		t.Error("place.search tool result should contain 'Cafe Roma'")
	}
}

// TestE2EContextCarryover verifies that conversation history carries over between turns.
// 2 simple turns (no tools): Turn 1: "My name is Alex" -> Turn 2: "What's my name?"
// The second model call should include the history from turn 1.
func TestE2EContextCarryover(t *testing.T) {
	p := setupE2E(t,
		finalResponse("Nice to meet you, Alex!"),
		finalResponse("Your name is Alex."),
	)

	p.sendMsg(t, "My name is Alex")
	p.sendMsg(t, "What's my name?")

	if p.Fake.CallCount() != 2 {
		t.Fatalf("model calls = %d, want 2", p.Fake.CallCount())
	}

	calls := p.Fake.Calls()

	// The second call should include history from the first turn.
	// It should contain the user's first message and the assistant's first response.
	foundUserMsg := false
	foundAssistantMsg := false
	for _, msg := range calls[1] {
		if msg.Role == "user" && strings.Contains(msg.Content, "My name is Alex") {
			foundUserMsg = true
		}
		if msg.Role == "assistant" && strings.Contains(msg.Content, "Nice to meet you, Alex!") {
			foundAssistantMsg = true
		}
	}
	if !foundUserMsg {
		t.Error("second call should include user message 'My name is Alex' from turn 1")
	}
	if !foundAssistantMsg {
		t.Error("second call should include assistant response from turn 1")
	}
}

// TestE2EThreeTurnConversation verifies that message count grows across 3 simple turns.
func TestE2EThreeTurnConversation(t *testing.T) {
	p := setupE2E(t,
		finalResponse("Reply one."),
		finalResponse("Reply two."),
		finalResponse("Reply three."),
	)

	p.sendMsg(t, "Message one")
	p.sendMsg(t, "Message two")
	p.sendMsg(t, "Message three")

	calls := p.Fake.Calls()
	if len(calls) != 3 {
		t.Fatalf("model calls = %d, want 3", len(calls))
	}

	// Each subsequent call should have more messages than the previous.
	if len(calls[1]) <= len(calls[0]) {
		t.Errorf("call[1] messages (%d) should be > call[0] messages (%d)", len(calls[1]), len(calls[0]))
	}
	if len(calls[2]) <= len(calls[1]) {
		t.Errorf("call[2] messages (%d) should be > call[1] messages (%d)", len(calls[2]), len(calls[1]))
	}
}

// TestE2EMultiToolSingleTurn verifies that the model can request 2 tool calls in a single turn.
func TestE2EMultiToolSingleTurn(t *testing.T) {
	p := setupE2E(t,
		// Single response with 2 tool calls.
		toolCallResponse("Adding two tasks at once.",
			tc("mt1", "task.add", `{"content":"Task A"}`),
			tc("mt2", "task.add", `{"content":"Task B"}`),
		),
		finalResponse("Both tasks added."),
	)

	p.sendMsg(t, "Add Task A and Task B")

	// Verify both tasks in DB.
	count := p.countRows(t, "user_tasks")
	if count != 2 {
		t.Errorf("user_tasks rows = %d, want 2", count)
	}

	// Verify 2 model calls (tool call response + final).
	if p.Fake.CallCount() != 2 {
		t.Errorf("model calls = %d, want 2", p.Fake.CallCount())
	}

	// Verify the final response was sent.
	resp := p.lastResponse(t)
	if resp != "Both tasks added." {
		t.Errorf("response = %q", resp)
	}
}
