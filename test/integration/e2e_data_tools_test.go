//go:build integration

package integration

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

// ============================================================================
// Task Tools (11 tests)
// ============================================================================

func TestE2ETaskAdd(t *testing.T) {
	p := setupE2E(t,
		toolCallResponse("Adding task.", tc("tc1", "task.add", `{"content":"Buy groceries"}`)),
		finalResponse("Task added!"),
	)

	p.sendMsg(t, "Add task: buy groceries")

	resp := p.lastResponse(t)
	if resp != "Task added!" {
		t.Errorf("response = %q, want %q", resp, "Task added!")
	}

	count := p.countRows(t, "user_tasks")
	if count != 1 {
		t.Fatalf("user_tasks rows = %d, want 1", count)
	}

	content := p.querySingleString(t, "SELECT content FROM user_tasks WHERE id = 1")
	if content != "Buy groceries" {
		t.Errorf("content = %q, want %q", content, "Buy groceries")
	}

	listName := p.querySingleString(t, "SELECT list_name FROM user_tasks WHERE id = 1")
	if listName != "default" {
		t.Errorf("list_name = %q, want %q", listName, "default")
	}
}

func TestE2ETaskAddWithPriority(t *testing.T) {
	p := setupE2E(t,
		toolCallResponse("Adding priority task.", tc("tc1", "task.add", `{"content":"Fix bug","priority":3}`)),
		finalResponse("Urgent task added."),
	)

	p.sendMsg(t, "Add urgent task: fix bug")

	resp := p.lastResponse(t)
	if resp != "Urgent task added." {
		t.Errorf("response = %q", resp)
	}

	priority := p.querySingleInt(t, "SELECT priority FROM user_tasks WHERE id = 1")
	if priority != 3 {
		t.Errorf("priority = %d, want 3", priority)
	}
}

func TestE2ETaskAddToNamedList(t *testing.T) {
	p := setupE2E(t,
		toolCallResponse("Adding to work list.", tc("tc1", "task.add", `{"content":"Review PR","list":"work"}`)),
		finalResponse("Added to work list."),
	)

	p.sendMsg(t, "Add to work list: review PR")

	listName := p.querySingleString(t, "SELECT list_name FROM user_tasks WHERE id = 1")
	if listName != "work" {
		t.Errorf("list_name = %q, want %q", listName, "work")
	}
}

func TestE2ETaskList(t *testing.T) {
	p := setupE2E(t,
		// Turn 1: add task 1
		toolCallResponse("Adding.", tc("tc1", "task.add", `{"content":"Task A"}`)),
		finalResponse("Added A."),
		// Turn 2: add task 2
		toolCallResponse("Adding.", tc("tc2", "task.add", `{"content":"Task B"}`)),
		finalResponse("Added B."),
		// Turn 3: add task 3
		toolCallResponse("Adding.", tc("tc3", "task.add", `{"content":"Task C"}`)),
		finalResponse("Added C."),
		// Turn 4: list tasks
		toolCallResponse("Listing.", tc("tc4", "task.list", `{}`)),
		finalResponse("Here are your tasks: A, B, C."),
	)

	p.sendMsg(t, "Add task A")
	p.sendMsg(t, "Add task B")
	p.sendMsg(t, "Add task C")
	p.sendMsg(t, "List my tasks")

	count := p.countRows(t, "user_tasks")
	if count != 3 {
		t.Errorf("user_tasks rows = %d, want 3", count)
	}

	if p.responseCount() != 4 {
		t.Errorf("response count = %d, want 4", p.responseCount())
	}

	resp := p.lastResponse(t)
	if resp != "Here are your tasks: A, B, C." {
		t.Errorf("response = %q", resp)
	}
}

func TestE2ETaskListByStatus(t *testing.T) {
	p := setupE2E(t,
		// Turn 1: add task 1
		toolCallResponse("Adding.", tc("tc1", "task.add", `{"content":"Pending task"}`)),
		finalResponse("Added."),
		// Turn 2: add task 2
		toolCallResponse("Adding.", tc("tc2", "task.add", `{"content":"Done task"}`)),
		finalResponse("Added."),
		// Turn 3: complete task 2
		toolCallResponse("Completing.", tc("tc3", "task.complete", `{"id":2}`)),
		finalResponse("Completed."),
		// Turn 4: list pending only
		toolCallResponse("Listing.", tc("tc4", "task.list", `{"status":"pending"}`)),
		finalResponse("1 pending task."),
	)

	p.sendMsg(t, "Add pending task")
	p.sendMsg(t, "Add done task")
	p.sendMsg(t, "Complete task 2")
	p.sendMsg(t, "List pending tasks")

	// Verify DB: task 2 is completed.
	status := p.querySingleString(t, "SELECT status FROM user_tasks WHERE id = 2")
	if status != "completed" {
		t.Errorf("status = %q, want %q", status, "completed")
	}

	resp := p.lastResponse(t)
	if resp != "1 pending task." {
		t.Errorf("response = %q", resp)
	}
}

func TestE2ETaskListByList(t *testing.T) {
	p := setupE2E(t,
		// Turn 1: add to work
		toolCallResponse("Adding.", tc("tc1", "task.add", `{"content":"Code review","list":"work"}`)),
		finalResponse("Added to work."),
		// Turn 2: add to personal
		toolCallResponse("Adding.", tc("tc2", "task.add", `{"content":"Grocery run","list":"personal"}`)),
		finalResponse("Added to personal."),
		// Turn 3: list work only
		toolCallResponse("Listing.", tc("tc3", "task.list", `{"list":"work"}`)),
		finalResponse("Work tasks: Code review."),
	)

	p.sendMsg(t, "Add code review to work")
	p.sendMsg(t, "Add grocery run to personal")
	p.sendMsg(t, "List work tasks")

	resp := p.lastResponse(t)
	if resp != "Work tasks: Code review." {
		t.Errorf("response = %q", resp)
	}
}

func TestE2ETaskComplete(t *testing.T) {
	p := setupE2E(t,
		// Turn 1: add task
		toolCallResponse("Adding.", tc("tc1", "task.add", `{"content":"Ship feature"}`)),
		finalResponse("Added."),
		// Turn 2: complete task
		toolCallResponse("Completing.", tc("tc2", "task.complete", `{"id":1}`)),
		finalResponse("Task completed!"),
	)

	p.sendMsg(t, "Add task: ship feature")
	p.sendMsg(t, "Complete task 1")

	status := p.querySingleString(t, "SELECT status FROM user_tasks WHERE id = 1")
	if status != "completed" {
		t.Errorf("status = %q, want %q", status, "completed")
	}

	resp := p.lastResponse(t)
	if resp != "Task completed!" {
		t.Errorf("response = %q", resp)
	}
}

func TestE2ETaskCompleteNotFound(t *testing.T) {
	p := setupE2E(t,
		// Turn 1: try to complete non-existent task
		toolCallResponse("Trying.", tc("tc1", "task.complete", `{"id":999}`)),
		finalResponse("Task not found."),
	)

	p.sendMsg(t, "Complete task 999")

	// The tool itself returns "Task not found" as tool result; the final model response says the same.
	resp := p.lastResponse(t)
	if resp != "Task not found." {
		t.Errorf("response = %q", resp)
	}

	count := p.countRows(t, "user_tasks")
	if count != 0 {
		t.Errorf("user_tasks rows = %d, want 0", count)
	}
}

func TestE2ETaskRemove(t *testing.T) {
	p := setupE2E(t,
		// Turn 1: add task
		toolCallResponse("Adding.", tc("tc1", "task.add", `{"content":"Temp task"}`)),
		finalResponse("Added."),
		// Turn 2: remove task
		toolCallResponse("Removing.", tc("tc2", "task.remove", `{"id":1}`)),
		finalResponse("Task removed."),
	)

	p.sendMsg(t, "Add temp task")

	count := p.countRows(t, "user_tasks")
	if count != 1 {
		t.Fatalf("after add: user_tasks rows = %d, want 1", count)
	}

	p.sendMsg(t, "Remove task 1")

	count = p.countRows(t, "user_tasks")
	if count != 0 {
		t.Errorf("after remove: user_tasks rows = %d, want 0", count)
	}
}

func TestE2ETaskClear(t *testing.T) {
	p := setupE2E(t,
		// Turn 1: add task 1
		toolCallResponse("Adding.", tc("tc1", "task.add", `{"content":"Task X"}`)),
		finalResponse("Added."),
		// Turn 2: add task 2
		toolCallResponse("Adding.", tc("tc2", "task.add", `{"content":"Task Y"}`)),
		finalResponse("Added."),
		// Turn 3: complete task 1
		toolCallResponse("Completing.", tc("tc3", "task.complete", `{"id":1}`)),
		finalResponse("Done."),
		// Turn 4: complete task 2
		toolCallResponse("Completing.", tc("tc4", "task.complete", `{"id":2}`)),
		finalResponse("Done."),
		// Turn 5: clear completed
		toolCallResponse("Clearing.", tc("tc5", "task.clear", `{}`)),
		finalResponse("Cleared all completed tasks."),
	)

	p.sendMsg(t, "Add task X")
	p.sendMsg(t, "Add task Y")
	p.sendMsg(t, "Complete task 1")
	p.sendMsg(t, "Complete task 2")
	p.sendMsg(t, "Clear completed tasks")

	count := p.countRows(t, "user_tasks")
	if count != 0 {
		t.Errorf("user_tasks rows = %d, want 0 after clear", count)
	}
}

func TestE2ETaskPrioritize(t *testing.T) {
	p := setupE2E(t,
		// Turn 1: add task
		toolCallResponse("Adding.", tc("tc1", "task.add", `{"content":"Important thing"}`)),
		finalResponse("Added."),
		// Turn 2: prioritize task
		toolCallResponse("Prioritizing.", tc("tc2", "task.prioritize", `{"id":1,"priority":2}`)),
		finalResponse("Priority updated."),
	)

	p.sendMsg(t, "Add task: important thing")

	priority := p.querySingleInt(t, "SELECT priority FROM user_tasks WHERE id = 1")
	if priority != 0 {
		t.Errorf("initial priority = %d, want 0", priority)
	}

	p.sendMsg(t, "Set task 1 priority to high")

	priority = p.querySingleInt(t, "SELECT priority FROM user_tasks WHERE id = 1")
	if priority != 2 {
		t.Errorf("updated priority = %d, want 2", priority)
	}
}

// ============================================================================
// Journal Tools (5 tests)
// ============================================================================

func TestE2EJournalWrite(t *testing.T) {
	p := setupE2E(t,
		toolCallResponse("Writing.", tc("tc1", "journal.write", `{"content":"Great day"}`)),
		finalResponse("Journal entry saved."),
	)

	p.sendMsg(t, "Write in my journal: great day")

	count := p.countRows(t, "user_journal")
	if count != 1 {
		t.Fatalf("user_journal rows = %d, want 1", count)
	}

	content := p.querySingleString(t, "SELECT content FROM user_journal WHERE id = 1")
	if content != "Great day" {
		t.Errorf("content = %q, want %q", content, "Great day")
	}
}

func TestE2EJournalWriteWithMood(t *testing.T) {
	p := setupE2E(t,
		toolCallResponse("Writing.", tc("tc1", "journal.write", `{"content":"Good vibes","mood":"happy"}`)),
		finalResponse("Saved with mood."),
	)

	p.sendMsg(t, "Journal: good vibes, feeling happy")

	mood := p.querySingleString(t, "SELECT mood FROM user_journal WHERE id = 1")
	if mood != "happy" {
		t.Errorf("mood = %q, want %q", mood, "happy")
	}
}

func TestE2EJournalRead(t *testing.T) {
	p := setupE2E(t,
		// Turn 1: write entry 1
		toolCallResponse("Writing.", tc("tc1", "journal.write", `{"content":"Entry one"}`)),
		finalResponse("Saved."),
		// Turn 2: write entry 2
		toolCallResponse("Writing.", tc("tc2", "journal.write", `{"content":"Entry two"}`)),
		finalResponse("Saved."),
		// Turn 3: read
		toolCallResponse("Reading.", tc("tc3", "journal.read", `{}`)),
		finalResponse("Here are your entries."),
	)

	p.sendMsg(t, "Journal: entry one")
	p.sendMsg(t, "Journal: entry two")
	p.sendMsg(t, "Read my journal")

	count := p.countRows(t, "user_journal")
	if count != 2 {
		t.Errorf("user_journal rows = %d, want 2", count)
	}

	if p.responseCount() != 3 {
		t.Errorf("response count = %d, want 3", p.responseCount())
	}
}

func TestE2EJournalReadByDateRange(t *testing.T) {
	today := time.Now().UTC().Format("2006-01-02")
	p := setupE2E(t,
		// Turn 1: write entry
		toolCallResponse("Writing.", tc("tc1", "journal.write", `{"content":"Today note"}`)),
		finalResponse("Saved."),
		// Turn 2: read with date range
		toolCallResponse("Reading.", tc("tc2", "journal.read",
			fmt.Sprintf(`{"from":"%s","to":"%s"}`, today, today))),
		finalResponse("Entries for today."),
	)

	p.sendMsg(t, "Journal: today note")
	p.sendMsg(t, "Read journal for today")

	resp := p.lastResponse(t)
	if resp != "Entries for today." {
		t.Errorf("response = %q", resp)
	}
}

func TestE2EJournalMoodTrend(t *testing.T) {
	p := setupE2E(t,
		// Turn 1: write happy
		toolCallResponse("Writing.", tc("tc1", "journal.write", `{"content":"Day 1","mood":"happy"}`)),
		finalResponse("Saved."),
		// Turn 2: write happy
		toolCallResponse("Writing.", tc("tc2", "journal.write", `{"content":"Day 2","mood":"happy"}`)),
		finalResponse("Saved."),
		// Turn 3: write sad
		toolCallResponse("Writing.", tc("tc3", "journal.write", `{"content":"Day 3","mood":"sad"}`)),
		finalResponse("Saved."),
		// Turn 4: mood trend
		toolCallResponse("Analyzing.", tc("tc4", "journal.mood_trend", `{}`)),
		finalResponse("Mostly happy: 2 happy, 1 sad."),
	)

	p.sendMsg(t, "Journal: day 1, happy")
	p.sendMsg(t, "Journal: day 2, happy")
	p.sendMsg(t, "Journal: day 3, sad")
	p.sendMsg(t, "What are my mood trends?")

	count := p.countRows(t, "user_journal")
	if count != 3 {
		t.Errorf("user_journal rows = %d, want 3", count)
	}

	resp := p.lastResponse(t)
	if resp != "Mostly happy: 2 happy, 1 sad." {
		t.Errorf("response = %q", resp)
	}
}

// ============================================================================
// Contact Tools (4 tests)
// ============================================================================

func TestE2EContactAdd(t *testing.T) {
	p := setupE2E(t,
		toolCallResponse("Adding contact.", tc("tc1", "contact.add",
			`{"name":"Alice Smith","email":"alice@example.com","phone":"+15551234567"}`)),
		finalResponse("Contact added."),
	)

	p.sendMsg(t, "Add contact Alice Smith")

	count := p.countRows(t, "user_contacts")
	if count != 1 {
		t.Fatalf("user_contacts rows = %d, want 1", count)
	}

	name := p.querySingleString(t, "SELECT name FROM user_contacts WHERE id = 1")
	if name != "Alice Smith" {
		t.Errorf("name = %q, want %q", name, "Alice Smith")
	}
}

func TestE2EContactSearch(t *testing.T) {
	p := setupE2E(t,
		// Turn 1: add Alice
		toolCallResponse("Adding.", tc("tc1", "contact.add", `{"name":"Alice Smith"}`)),
		finalResponse("Added."),
		// Turn 2: add Bob
		toolCallResponse("Adding.", tc("tc2", "contact.add", `{"name":"Bob Jones"}`)),
		finalResponse("Added."),
		// Turn 3: search for Alice
		toolCallResponse("Searching.", tc("tc3", "contact.search", `{"query":"Alice"}`)),
		finalResponse("Found Alice Smith."),
	)

	p.sendMsg(t, "Add contact Alice Smith")
	p.sendMsg(t, "Add contact Bob Jones")
	p.sendMsg(t, "Search contacts for Alice")

	resp := p.lastResponse(t)
	if resp != "Found Alice Smith." {
		t.Errorf("response = %q", resp)
	}

	count := p.countRows(t, "user_contacts")
	if count != 2 {
		t.Errorf("user_contacts rows = %d, want 2", count)
	}
}

func TestE2EContactUpdate(t *testing.T) {
	p := setupE2E(t,
		// Turn 1: add contact
		toolCallResponse("Adding.", tc("tc1", "contact.add", `{"name":"Charlie","phone":"+1000"}`)),
		finalResponse("Added."),
		// Turn 2: update phone
		toolCallResponse("Updating.", tc("tc2", "contact.update", `{"id":1,"phone":"+19995551234"}`)),
		finalResponse("Contact updated."),
	)

	p.sendMsg(t, "Add contact Charlie")
	p.sendMsg(t, "Update Charlie's phone")

	phone := p.querySingleString(t, "SELECT phone FROM user_contacts WHERE id = 1")
	if phone != "+19995551234" {
		t.Errorf("phone = %q, want %q", phone, "+19995551234")
	}
}

func TestE2EContactBirthdays(t *testing.T) {
	// Use a birthday 10 days from now to ensure it falls within the lookup window.
	upcoming := time.Now().UTC().AddDate(0, 0, 10)
	birthdayStr := fmt.Sprintf("1990-%02d-%02d", upcoming.Month(), upcoming.Day())

	p := setupE2E(t,
		// Turn 1: add contact with birthday
		toolCallResponse("Adding.", tc("tc1", "contact.add",
			fmt.Sprintf(`{"name":"Dana","birthday":"%s"}`, birthdayStr))),
		finalResponse("Added."),
		// Turn 2: update to set the birthday field (contact.add doesn't support birthday directly)
		toolCallResponse("Updating.", tc("tc2", "contact.update",
			fmt.Sprintf(`{"id":1,"birthday":"%s"}`, birthdayStr))),
		finalResponse("Updated."),
		// Turn 3: query birthdays
		toolCallResponse("Checking.", tc("tc3", "contact.birthdays", `{"days":30}`)),
		finalResponse("Dana's birthday is coming up!"),
	)

	p.sendMsg(t, "Add contact Dana with birthday")
	p.sendMsg(t, "Set Dana's birthday")
	p.sendMsg(t, "Any upcoming birthdays?")

	resp := p.lastResponse(t)
	if resp != "Dana's birthday is coming up!" {
		t.Errorf("response = %q", resp)
	}

	birthday := p.querySingleString(t, "SELECT birthday FROM user_contacts WHERE id = 1")
	if birthday != birthdayStr {
		t.Errorf("birthday = %q, want %q", birthday, birthdayStr)
	}
}

// ============================================================================
// Expense Tools (3 tests)
// ============================================================================

func TestE2EExpenseLog(t *testing.T) {
	p := setupE2E(t,
		toolCallResponse("Logging.", tc("tc1", "expense.log",
			`{"amount":42.50,"category":"food","description":"Lunch"}`)),
		finalResponse("Expense logged."),
	)

	p.sendMsg(t, "Log expense: $42.50 for lunch")

	count := p.countRows(t, "user_expenses")
	if count != 1 {
		t.Fatalf("user_expenses rows = %d, want 1", count)
	}

	category := p.querySingleString(t, "SELECT category FROM user_expenses WHERE id = 1")
	if category != "food" {
		t.Errorf("category = %q, want %q", category, "food")
	}
}

func TestE2EExpenseSummary(t *testing.T) {
	p := setupE2E(t,
		// Turn 1: log expense 1
		toolCallResponse("Logging.", tc("tc1", "expense.log", `{"amount":25.00,"category":"food"}`)),
		finalResponse("Logged."),
		// Turn 2: log expense 2
		toolCallResponse("Logging.", tc("tc2", "expense.log", `{"amount":75.00,"category":"food"}`)),
		finalResponse("Logged."),
		// Turn 3: log expense 3
		toolCallResponse("Logging.", tc("tc3", "expense.log", `{"amount":50.00,"category":"transport"}`)),
		finalResponse("Logged."),
		// Turn 4: summary
		toolCallResponse("Summarizing.", tc("tc4", "expense.summary", `{}`)),
		finalResponse("Food: $100, Transport: $50."),
	)

	p.sendMsg(t, "Log $25 food")
	p.sendMsg(t, "Log $75 food")
	p.sendMsg(t, "Log $50 transport")
	p.sendMsg(t, "Expense summary")

	count := p.countRows(t, "user_expenses")
	if count != 3 {
		t.Errorf("user_expenses rows = %d, want 3", count)
	}

	resp := p.lastResponse(t)
	if resp != "Food: $100, Transport: $50." {
		t.Errorf("response = %q", resp)
	}
}

func TestE2EExpenseBudgetCheck(t *testing.T) {
	p := setupE2E(t,
		// Turn 1: log expense
		toolCallResponse("Logging.", tc("tc1", "expense.log", `{"amount":150.00,"category":"food"}`)),
		finalResponse("Logged."),
		// Turn 2: budget check
		toolCallResponse("Checking budget.", tc("tc2", "expense.budget_check", `{"category":"food"}`)),
		finalResponse("Food budget: $150 of $500 spent (30%)."),
	)

	// Pre-seed budget.
	ctx := context.Background()
	_, err := p.DB.ExecContext(ctx, "INSERT INTO user_budgets (category, amount, period) VALUES ('food', 500, 'monthly')")
	if err != nil {
		t.Fatalf("seed budget: %v", err)
	}

	p.sendMsg(t, "Log $150 food expense")
	p.sendMsg(t, "Check food budget")

	resp := p.lastResponse(t)
	if resp != "Food budget: $150 of $500 spent (30%)." {
		t.Errorf("response = %q", resp)
	}

	// Verify budget row is still there.
	budgetCount := p.countRows(t, "user_budgets")
	if budgetCount != 1 {
		t.Errorf("user_budgets rows = %d, want 1", budgetCount)
	}
}

// ============================================================================
// Health Tools (2 tests)
// ============================================================================

func TestE2EHealthLog(t *testing.T) {
	p := setupE2E(t,
		toolCallResponse("Logging.", tc("tc1", "health.log",
			`{"metric":"weight","value":"75.5","unit":"kg"}`)),
		finalResponse("Health metric logged."),
	)

	p.sendMsg(t, "Log weight: 75.5 kg")

	count := p.countRows(t, "user_health")
	if count != 1 {
		t.Fatalf("user_health rows = %d, want 1", count)
	}

	metric := p.querySingleString(t, "SELECT metric FROM user_health WHERE id = 1")
	if metric != "weight" {
		t.Errorf("metric = %q, want %q", metric, "weight")
	}

	unit := p.querySingleString(t, "SELECT unit FROM user_health WHERE id = 1")
	if unit != "kg" {
		t.Errorf("unit = %q, want %q", unit, "kg")
	}
}

func TestE2EHealthRead(t *testing.T) {
	p := setupE2E(t,
		// Turn 1: log weight
		toolCallResponse("Logging.", tc("tc1", "health.log",
			`{"metric":"weight","value":"75","unit":"kg"}`)),
		finalResponse("Logged."),
		// Turn 2: log blood pressure
		toolCallResponse("Logging.", tc("tc2", "health.log",
			`{"metric":"blood_pressure","value":"120/80","unit":"mmHg"}`)),
		finalResponse("Logged."),
		// Turn 3: read weight only
		toolCallResponse("Reading.", tc("tc3", "health.read", `{"metric":"weight"}`)),
		finalResponse("Weight: 75 kg."),
	)

	p.sendMsg(t, "Log weight 75 kg")
	p.sendMsg(t, "Log blood pressure 120/80")
	p.sendMsg(t, "Show my weight readings")

	count := p.countRows(t, "user_health")
	if count != 2 {
		t.Errorf("user_health rows = %d, want 2", count)
	}

	resp := p.lastResponse(t)
	if resp != "Weight: 75 kg." {
		t.Errorf("response = %q", resp)
	}
}

// ============================================================================
// Reminder Tools (3 tests)
// ============================================================================

func TestE2EReminderSet(t *testing.T) {
	future := time.Now().Add(2 * time.Hour).UTC().Format(time.RFC3339)
	p := setupE2E(t,
		toolCallResponse("Setting.", tc("tc1", "reminder.set",
			fmt.Sprintf(`{"message":"Call dentist","remind_at":"%s"}`, future))),
		finalResponse("Reminder set."),
	)

	p.sendMsg(t, "Remind me to call the dentist")

	count := p.countRows(t, "user_reminders")
	if count != 1 {
		t.Fatalf("user_reminders rows = %d, want 1", count)
	}

	message := p.querySingleString(t, "SELECT message FROM user_reminders WHERE id = 1")
	if message != "Call dentist" {
		t.Errorf("message = %q, want %q", message, "Call dentist")
	}

	status := p.querySingleString(t, "SELECT status FROM user_reminders WHERE id = 1")
	if status != "active" {
		t.Errorf("status = %q, want %q", status, "active")
	}
}

func TestE2EReminderList(t *testing.T) {
	future1 := time.Now().Add(1 * time.Hour).UTC().Format(time.RFC3339)
	future2 := time.Now().Add(3 * time.Hour).UTC().Format(time.RFC3339)
	p := setupE2E(t,
		// Turn 1: set reminder 1
		toolCallResponse("Setting.", tc("tc1", "reminder.set",
			fmt.Sprintf(`{"message":"Meeting","remind_at":"%s"}`, future1))),
		finalResponse("Set."),
		// Turn 2: set reminder 2
		toolCallResponse("Setting.", tc("tc2", "reminder.set",
			fmt.Sprintf(`{"message":"Pickup","remind_at":"%s"}`, future2))),
		finalResponse("Set."),
		// Turn 3: list
		toolCallResponse("Listing.", tc("tc3", "reminder.list", `{}`)),
		finalResponse("You have 2 reminders."),
	)

	p.sendMsg(t, "Remind me about the meeting")
	p.sendMsg(t, "Remind me about pickup")
	p.sendMsg(t, "List my reminders")

	count := p.countRows(t, "user_reminders")
	if count != 2 {
		t.Errorf("user_reminders rows = %d, want 2", count)
	}

	resp := p.lastResponse(t)
	if resp != "You have 2 reminders." {
		t.Errorf("response = %q", resp)
	}
}

func TestE2EReminderCancel(t *testing.T) {
	future := time.Now().Add(2 * time.Hour).UTC().Format(time.RFC3339)
	p := setupE2E(t,
		// Turn 1: set reminder
		toolCallResponse("Setting.", tc("tc1", "reminder.set",
			fmt.Sprintf(`{"message":"Cancel me","remind_at":"%s"}`, future))),
		finalResponse("Set."),
		// Turn 2: cancel reminder
		toolCallResponse("Cancelling.", tc("tc2", "reminder.cancel", `{"id":1}`)),
		finalResponse("Reminder cancelled."),
	)

	p.sendMsg(t, "Set a reminder")
	p.sendMsg(t, "Cancel reminder 1")

	status := p.querySingleString(t, "SELECT status FROM user_reminders WHERE id = 1")
	if status != "cancelled" {
		t.Errorf("status = %q, want %q", status, "cancelled")
	}
}

// ============================================================================
// Habit Tools (3 tests)
// ============================================================================

func TestE2EHabitCreate(t *testing.T) {
	p := setupE2E(t,
		toolCallResponse("Creating.", tc("tc1", "habit.create",
			`{"name":"Meditate","frequency":"daily"}`)),
		finalResponse("Habit created."),
	)

	p.sendMsg(t, "Create habit: meditate daily")

	count := p.countRows(t, "user_habits")
	if count != 1 {
		t.Fatalf("user_habits rows = %d, want 1", count)
	}

	name := p.querySingleString(t, "SELECT name FROM user_habits WHERE id = 1")
	if name != "Meditate" {
		t.Errorf("name = %q, want %q", name, "Meditate")
	}

	freq := p.querySingleString(t, "SELECT frequency FROM user_habits WHERE id = 1")
	if freq != "daily" {
		t.Errorf("frequency = %q, want %q", freq, "daily")
	}
}

func TestE2EHabitLog(t *testing.T) {
	today := time.Now().UTC().Format("2006-01-02")
	p := setupE2E(t,
		// Turn 1: create habit
		toolCallResponse("Creating.", tc("tc1", "habit.create", `{"name":"Exercise"}`)),
		finalResponse("Created."),
		// Turn 2: log habit
		toolCallResponse("Logging.", tc("tc2", "habit.log",
			fmt.Sprintf(`{"name":"Exercise","date":"%s"}`, today))),
		finalResponse("Logged exercise for today."),
	)

	p.sendMsg(t, "Create habit: exercise")
	p.sendMsg(t, "Log exercise for today")

	count := p.countRows(t, "user_habit_logs")
	if count != 1 {
		t.Fatalf("user_habit_logs rows = %d, want 1", count)
	}

	logDate := p.querySingleString(t, "SELECT date FROM user_habit_logs WHERE id = 1")
	if logDate != today {
		t.Errorf("date = %q, want %q", logDate, today)
	}
}

func TestE2EHabitStreak(t *testing.T) {
	today := time.Now().UTC().Format("2006-01-02")
	p := setupE2E(t,
		// Turn 1: create habit
		toolCallResponse("Creating.", tc("tc1", "habit.create", `{"name":"Read"}`)),
		finalResponse("Created."),
		// Turn 2: log today
		toolCallResponse("Logging.", tc("tc2", "habit.log",
			fmt.Sprintf(`{"name":"Read","date":"%s"}`, today))),
		finalResponse("Logged."),
		// Turn 3: streak check
		toolCallResponse("Checking.", tc("tc3", "habit.streak", `{"name":"Read"}`)),
		finalResponse("Your reading streak is 1 day!"),
	)

	p.sendMsg(t, "Create habit: read")
	p.sendMsg(t, "Log reading for today")
	p.sendMsg(t, "What's my reading streak?")

	resp := p.lastResponse(t)
	if !strings.Contains(resp, "streak") {
		t.Errorf("response = %q, expected it to mention streak", resp)
	}
}

// ============================================================================
// Place Tools (4 tests)
// ============================================================================

func TestE2EPlaceSave(t *testing.T) {
	p := setupE2E(t,
		toolCallResponse("Saving.", tc("tc1", "place.save",
			`{"name":"Best Pizza","address":"456 Oak Ave","category":"restaurant","rating":5}`)),
		finalResponse("Place saved."),
	)

	p.sendMsg(t, "Save place: Best Pizza")

	count := p.countRows(t, "user_places")
	if count != 1 {
		t.Fatalf("user_places rows = %d, want 1", count)
	}

	name := p.querySingleString(t, "SELECT name FROM user_places WHERE id = 1")
	if name != "Best Pizza" {
		t.Errorf("name = %q, want %q", name, "Best Pizza")
	}

	category := p.querySingleString(t, "SELECT category FROM user_places WHERE id = 1")
	if category != "restaurant" {
		t.Errorf("category = %q, want %q", category, "restaurant")
	}
}

func TestE2EPlaceSearch(t *testing.T) {
	p := setupE2E(t,
		// Turn 1: save place 1
		toolCallResponse("Saving.", tc("tc1", "place.save",
			`{"name":"Cafe Luna","category":"cafe"}`)),
		finalResponse("Saved."),
		// Turn 2: save place 2
		toolCallResponse("Saving.", tc("tc2", "place.save",
			`{"name":"Gym Pro","category":"fitness"}`)),
		finalResponse("Saved."),
		// Turn 3: search for cafe
		toolCallResponse("Searching.", tc("tc3", "place.search", `{"query":"Luna"}`)),
		finalResponse("Found Cafe Luna."),
	)

	p.sendMsg(t, "Save Cafe Luna")
	p.sendMsg(t, "Save Gym Pro")
	p.sendMsg(t, "Search for Luna")

	resp := p.lastResponse(t)
	if resp != "Found Cafe Luna." {
		t.Errorf("response = %q", resp)
	}

	count := p.countRows(t, "user_places")
	if count != 2 {
		t.Errorf("user_places rows = %d, want 2", count)
	}
}

func TestE2EPlaceUpdate(t *testing.T) {
	p := setupE2E(t,
		// Turn 1: save place
		toolCallResponse("Saving.", tc("tc1", "place.save",
			`{"name":"Sushi Spot","rating":3}`)),
		finalResponse("Saved."),
		// Turn 2: update rating
		toolCallResponse("Updating.", tc("tc2", "place.update", `{"id":1,"rating":5}`)),
		finalResponse("Rating updated."),
	)

	p.sendMsg(t, "Save Sushi Spot with rating 3")
	p.sendMsg(t, "Update Sushi Spot rating to 5")

	rating := p.querySingleInt(t, "SELECT rating FROM user_places WHERE id = 1")
	if rating != 5 {
		t.Errorf("rating = %d, want 5", rating)
	}
}

func TestE2EPlaceDelete(t *testing.T) {
	p := setupE2E(t,
		// Turn 1: save place
		toolCallResponse("Saving.", tc("tc1", "place.save", `{"name":"Old Spot"}`)),
		finalResponse("Saved."),
		// Turn 2: delete place
		toolCallResponse("Deleting.", tc("tc2", "place.delete", `{"id":1}`)),
		finalResponse("Place deleted."),
	)

	p.sendMsg(t, "Save Old Spot")

	count := p.countRows(t, "user_places")
	if count != 1 {
		t.Fatalf("after save: user_places rows = %d, want 1", count)
	}

	p.sendMsg(t, "Delete Old Spot")

	count = p.countRows(t, "user_places")
	if count != 0 {
		t.Errorf("after delete: user_places rows = %d, want 0", count)
	}
}
