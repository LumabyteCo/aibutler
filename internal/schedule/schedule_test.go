package schedule_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/LumabyteCo/aibutler/internal/agent"
	"github.com/LumabyteCo/aibutler/internal/schedule"
	"github.com/LumabyteCo/aibutler/internal/tool"
	"github.com/LumabyteCo/aibutler/testutil"
)

// ---------------------------------------------------------------------------
// Cron parsing tests
// ---------------------------------------------------------------------------

func TestParseCronBasic(t *testing.T) {
	c, err := schedule.ParseCron("0 8 * * *")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	next := c.Next(time.Date(2024, 1, 1, 7, 0, 0, 0, time.UTC))
	if next.Hour() != 8 || next.Minute() != 0 {
		t.Errorf("expected 08:00, got %s", next.Format("15:04"))
	}

	// Verify the expression round-trips through String()
	s := c.String()
	if !strings.HasPrefix(s, "0 8 ") {
		t.Errorf("expected String() to start with '0 8 ', got %q", s)
	}
}

func TestParseCronSteps(t *testing.T) {
	c, err := schedule.ParseCron("*/15 * * * *")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// With */15, the minutes should be 0, 15, 30, 45
	s := c.String()
	fields := strings.Fields(s)
	minuteField := fields[0]
	expected := "0,15,30,45"
	if minuteField != expected {
		t.Errorf("minute field = %q, want %q", minuteField, expected)
	}
}

func TestParseCronRanges(t *testing.T) {
	c, err := schedule.ParseCron("0 9-17 * * 1-5")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	s := c.String()
	fields := strings.Fields(s)

	// hour field: 9,10,11,...,17
	hours := fields[1]
	expectedHours := "9,10,11,12,13,14,15,16,17"
	if hours != expectedHours {
		t.Errorf("hour field = %q, want %q", hours, expectedHours)
	}

	// dow field: 1,2,3,4,5
	dow := fields[4]
	expectedDow := "1,2,3,4,5"
	if dow != expectedDow {
		t.Errorf("dow field = %q, want %q", dow, expectedDow)
	}
}

func TestParseCronLists(t *testing.T) {
	c, err := schedule.ParseCron("0,30 * * * *")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	s := c.String()
	fields := strings.Fields(s)
	minuteField := fields[0]
	expected := "0,30"
	if minuteField != expected {
		t.Errorf("minute field = %q, want %q", minuteField, expected)
	}
}

func TestParseCronSpecialDaily(t *testing.T) {
	c, err := schedule.ParseCron("@daily")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	s := c.String()
	fields := strings.Fields(s)
	if fields[0] != "0" || fields[1] != "0" {
		t.Errorf("@daily: minute=%s hour=%s, want minute=0 hour=0", fields[0], fields[1])
	}
}

func TestParseCronSpecialHourly(t *testing.T) {
	c, err := schedule.ParseCron("@hourly")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	s := c.String()
	fields := strings.Fields(s)
	if fields[0] != "0" {
		t.Errorf("@hourly: minute=%s, want 0", fields[0])
	}
	// hour should be wildcard (all hours)
	hourParts := strings.Split(fields[1], ",")
	if len(hourParts) != 24 {
		t.Errorf("@hourly: expected 24 hour values, got %d", len(hourParts))
	}
}

func TestParseCronInvalid(t *testing.T) {
	_, err := schedule.ParseCron("bad")
	if err == nil {
		t.Fatal("expected error for invalid cron expression")
	}
}

func TestCronNext(t *testing.T) {
	c, err := schedule.ParseCron("30 8 * * *")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	from := time.Date(2024, 1, 1, 8, 0, 0, 0, time.UTC)
	next := c.Next(from)
	expected := time.Date(2024, 1, 1, 8, 30, 0, 0, time.UTC)
	if !next.Equal(expected) {
		t.Errorf("Next(%v) = %v, want %v", from, next, expected)
	}
}

func TestCronNextWraparound(t *testing.T) {
	c, err := schedule.ParseCron("0 0 * * *")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	from := time.Date(2024, 1, 1, 23, 59, 0, 0, time.UTC)
	next := c.Next(from)
	expected := time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC)
	if !next.Equal(expected) {
		t.Errorf("Next(%v) = %v, want %v", from, next, expected)
	}
}

// ---------------------------------------------------------------------------
// Store tests
// ---------------------------------------------------------------------------

// testEnv bundles a Store with the underlying *sql.DB for seeding FK data.
type testEnv struct {
	store *schedule.Store
	conn  *sql.DB
}

func newTestEnv(t *testing.T) *testEnv {
	t.Helper()
	database := testutil.TestDB(t)
	conn := database.Conn()
	return &testEnv{
		store: schedule.NewStore(conn),
		conn:  conn,
	}
}

// seedAgent inserts a session and agent row so that FK constraints on
// schedule_runs.agent_id are satisfied.
func (e *testEnv) seedAgent(t *testing.T, agentID string) {
	t.Helper()
	ctx := context.Background()
	now := time.Now().UTC().Format(time.RFC3339)

	// Session (agents FK → sessions)
	_, err := e.conn.ExecContext(ctx,
		`INSERT OR IGNORE INTO sessions (id, channel, account_id) VALUES ('test-sess', 'terminal', 'test-user')`)
	if err != nil {
		t.Fatalf("seed session: %v", err)
	}

	// Agent
	_, err = e.conn.ExecContext(ctx,
		`INSERT OR IGNORE INTO agents (id, session_id, type, state, task, capabilities, model, created_at, updated_at)
		 VALUES (?, 'test-sess', 'scheduled', 'completed', 'test', '[]', 'test-model', ?, ?)`,
		agentID, now, now)
	if err != nil {
		t.Fatalf("seed agent %s: %v", agentID, err)
	}
}

func TestStoreCreateAndGet(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()

	sched := &schedule.Schedule{
		ID:        "sched-1",
		Name:      "Morning Report",
		CronExpr:  "0 8 * * *",
		Task:      "Generate daily report",
		Channel:   "telegram",
		AccountID: "user-1",
		Skills:    []string{"research"},
		Enabled:   true,
	}

	if err := env.store.Create(ctx, sched); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := env.store.Get(ctx, "sched-1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	if got.ID != sched.ID {
		t.Errorf("ID = %q, want %q", got.ID, sched.ID)
	}
	if got.Name != sched.Name {
		t.Errorf("Name = %q, want %q", got.Name, sched.Name)
	}
	if got.CronExpr != sched.CronExpr {
		t.Errorf("CronExpr = %q, want %q", got.CronExpr, sched.CronExpr)
	}
	if got.Task != sched.Task {
		t.Errorf("Task = %q, want %q", got.Task, sched.Task)
	}
	if got.Channel != sched.Channel {
		t.Errorf("Channel = %q, want %q", got.Channel, sched.Channel)
	}
	if got.AccountID != sched.AccountID {
		t.Errorf("AccountID = %q, want %q", got.AccountID, sched.AccountID)
	}
	if !got.Enabled {
		t.Error("Enabled = false, want true")
	}
	if len(got.Skills) != 1 || got.Skills[0] != "research" {
		t.Errorf("Skills = %v, want [research]", got.Skills)
	}
}

func TestStoreList(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()

	// Create two schedules (names sorted: Alpha < Beta)
	for _, s := range []*schedule.Schedule{
		{ID: "s-2", Name: "Beta", CronExpr: "0 9 * * *", Task: "t2", Channel: "webchat", AccountID: "u1", Enabled: true},
		{ID: "s-1", Name: "Alpha", CronExpr: "0 8 * * *", Task: "t1", Channel: "telegram", AccountID: "u1", Enabled: true},
	} {
		if err := env.store.Create(ctx, s); err != nil {
			t.Fatalf("Create %s: %v", s.Name, err)
		}
	}

	list, err := env.store.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("List length = %d, want 2", len(list))
	}
	// Should be sorted by name
	if list[0].Name != "Alpha" {
		t.Errorf("list[0].Name = %q, want Alpha", list[0].Name)
	}
	if list[1].Name != "Beta" {
		t.Errorf("list[1].Name = %q, want Beta", list[1].Name)
	}
}

func TestStoreDelete(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()

	sched := &schedule.Schedule{
		ID: "s-del", Name: "ToDelete", CronExpr: "0 0 * * *",
		Task: "t", Channel: "c", AccountID: "u", Enabled: true,
	}
	if err := env.store.Create(ctx, sched); err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := env.store.Delete(ctx, "s-del"); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	_, err := env.store.Get(ctx, "s-del")
	if err == nil {
		t.Fatal("expected error after delete, got nil")
	}
}

func TestStoreEnableDisable(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()

	sched := &schedule.Schedule{
		ID: "s-toggle", Name: "Toggle", CronExpr: "0 0 * * *",
		Task: "t", Channel: "c", AccountID: "u", Enabled: true,
	}
	if err := env.store.Create(ctx, sched); err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Disable
	if err := env.store.SetEnabled(ctx, "s-toggle", false); err != nil {
		t.Fatalf("SetEnabled(false): %v", err)
	}
	got, err := env.store.Get(ctx, "s-toggle")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Enabled {
		t.Error("expected Enabled=false after disable")
	}

	// Re-enable
	if err := env.store.SetEnabled(ctx, "s-toggle", true); err != nil {
		t.Fatalf("SetEnabled(true): %v", err)
	}
	got, err = env.store.Get(ctx, "s-toggle")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !got.Enabled {
		t.Error("expected Enabled=true after re-enable")
	}
}

func TestStoreRecordRun(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()

	// Create a schedule first (FK constraint)
	sched := &schedule.Schedule{
		ID: "s-run", Name: "RunTest", CronExpr: "0 0 * * *",
		Task: "t", Channel: "c", AccountID: "u", Enabled: true,
	}
	if err := env.store.Create(ctx, sched); err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Seed an agent for the FK on agent_id
	env.seedAgent(t, "agent-123")

	now := time.Now().UTC().Truncate(time.Second)
	run := &schedule.Run{
		ScheduleID: "s-run",
		Status:     "running",
		StartedAt:  now,
	}
	if err := env.store.RecordRun(ctx, run); err != nil {
		t.Fatalf("RecordRun(insert): %v", err)
	}
	if run.ID == 0 {
		t.Fatal("expected non-zero run ID after insert")
	}

	// Complete the run
	completedAt := now.Add(5 * time.Second)
	run.CompletedAt = &completedAt
	run.Status = "completed"
	run.AgentID = "agent-123"
	if err := env.store.RecordRun(ctx, run); err != nil {
		t.Fatalf("RecordRun(update): %v", err)
	}

	// Verify via LastRun
	last, err := env.store.LastRun(ctx, "s-run")
	if err != nil {
		t.Fatalf("LastRun: %v", err)
	}
	if last == nil {
		t.Fatal("expected non-nil last run")
	}
	if last.Status != "completed" {
		t.Errorf("Status = %q, want completed", last.Status)
	}
	if last.AgentID != "agent-123" {
		t.Errorf("AgentID = %q, want agent-123", last.AgentID)
	}
}

// ---------------------------------------------------------------------------
// Scheduler tests
// ---------------------------------------------------------------------------

type fakeRunner struct {
	mu      sync.Mutex
	called  bool
	result  *agent.Result
	taskGot string
}

func (f *fakeRunner) Run(_ context.Context, sessionID, task, channel string) (*agent.Result, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.called = true
	f.taskGot = task
	return f.result, nil
}

func (f *fakeRunner) wasCalled() (bool, string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.called, f.taskGot
}

func TestSchedulerTick(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()

	// Seed the agent row that the scheduler will reference after completion
	env.seedAgent(t, "agent-tick")

	// Create a schedule that should be due (cron = every 5 minutes, minimum allowed)
	sched := &schedule.Schedule{
		ID: "s-tick", Name: "TickTest", CronExpr: "*/5 * * * *",
		Task: "do something", Channel: "telegram", AccountID: "u1", Enabled: true,
	}
	if err := env.store.Create(ctx, sched); err != nil {
		t.Fatalf("Create: %v", err)
	}
	// Backdate created_at so the schedule is always due regardless of current minute.
	_, _ = env.conn.ExecContext(ctx,
		`UPDATE schedules SET created_at = datetime('now', '-10 minutes') WHERE id = ?`, sched.ID)

	runner := &fakeRunner{
		result: &agent.Result{
			ID:     "agent-tick",
			Status: agent.StateCompleted,
		},
	}

	scheduler := schedule.NewScheduler(env.store, runner, time.Second)

	if err := scheduler.Tick(ctx); err != nil {
		t.Fatalf("Tick: %v", err)
	}

	// Give the goroutine a moment to complete
	time.Sleep(200 * time.Millisecond)

	called, taskGot := runner.wasCalled()
	if !called {
		t.Error("expected runner to be called")
	}
	if taskGot != "do something" {
		t.Errorf("task = %q, want %q", taskGot, "do something")
	}

	// Verify run was recorded
	last, err := env.store.LastRun(ctx, "s-tick")
	if err != nil {
		t.Fatalf("LastRun: %v", err)
	}
	if last == nil {
		t.Fatal("expected a recorded run")
	}
	if last.Status != "completed" {
		t.Errorf("run status = %q, want completed", last.Status)
	}
}

func TestSchedulerTickNotDue(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()

	// Create a schedule with a far-future cron (Feb 29, only on leap years)
	sched := &schedule.Schedule{
		ID: "s-notdue", Name: "NotDue", CronExpr: "0 0 29 2 *",
		Task: "rare task", Channel: "c", AccountID: "u", Enabled: true,
	}
	if err := env.store.Create(ctx, sched); err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Insert a recent run so it won't be considered "never run"
	now := time.Now().UTC()
	run := &schedule.Run{
		ScheduleID: "s-notdue",
		Status:     "completed",
		StartedAt:  now,
	}
	if err := env.store.RecordRun(ctx, run); err != nil {
		t.Fatalf("RecordRun: %v", err)
	}

	runner := &fakeRunner{
		result: &agent.Result{ID: "x", Status: agent.StateCompleted},
	}

	scheduler := schedule.NewScheduler(env.store, runner, time.Second)
	if err := scheduler.Tick(ctx); err != nil {
		t.Fatalf("Tick: %v", err)
	}

	time.Sleep(50 * time.Millisecond)

	if called, _ := runner.wasCalled(); called {
		t.Error("expected runner NOT to be called for non-due schedule")
	}
}

func TestSchedulerTickDisabled(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()

	sched := &schedule.Schedule{
		ID: "s-disabled", Name: "Disabled", CronExpr: "*/5 * * * *",
		Task: "should not run", Channel: "c", AccountID: "u", Enabled: false,
	}
	if err := env.store.Create(ctx, sched); err != nil {
		t.Fatalf("Create: %v", err)
	}

	runner := &fakeRunner{
		result: &agent.Result{ID: "x", Status: agent.StateCompleted},
	}

	scheduler := schedule.NewScheduler(env.store, runner, time.Second)
	if err := scheduler.Tick(ctx); err != nil {
		t.Fatalf("Tick: %v", err)
	}

	time.Sleep(50 * time.Millisecond)

	if called, _ := runner.wasCalled(); called {
		t.Error("expected runner NOT to be called for disabled schedule")
	}
}

// ---------------------------------------------------------------------------
// Tool tests
// ---------------------------------------------------------------------------

func TestScheduleCreateTool(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()

	// Override timeNowUnixMilli for deterministic IDs
	origFn := schedule.ExportTimeNowUnixMilli()
	schedule.SetTimeNowUnixMilli(func() int64 { return 1700000000000 })
	defer schedule.SetTimeNowUnixMilli(origFn)

	registry := tool.NewRegistry()
	schedule.RegisterScheduleTools(registry, env.store, nil)

	ct, ok := registry.Get("schedule.create")
	if !ok {
		t.Fatal("schedule.create tool not registered")
	}

	input := `{"name":"Daily Report","cron":"0 8 * * *","task":"Generate report","channel":"telegram","account_id":"user-1"}`
	output, err := ct.Execute(ctx, input)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if !strings.Contains(output, "Daily Report") {
		t.Errorf("output missing schedule name: %s", output)
	}
	if !strings.Contains(output, "sched_1700000000000") {
		t.Errorf("output missing expected ID: %s", output)
	}

	// Verify it was actually stored
	got, err := env.store.Get(ctx, "sched_1700000000000")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Name != "Daily Report" {
		t.Errorf("Name = %q, want Daily Report", got.Name)
	}
}

func TestScheduleListTool(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()

	// Create some schedules
	for i, name := range []string{"Alpha", "Beta"} {
		sched := &schedule.Schedule{
			ID: fmt.Sprintf("s-%d", i), Name: name, CronExpr: "0 0 * * *",
			Task: "t", Channel: "c", AccountID: "u", Enabled: true,
		}
		if err := env.store.Create(ctx, sched); err != nil {
			t.Fatalf("Create: %v", err)
		}
	}

	registry := tool.NewRegistry()
	schedule.RegisterScheduleTools(registry, env.store, nil)

	lt, ok := registry.Get("schedule.list")
	if !ok {
		t.Fatal("schedule.list tool not registered")
	}

	output, err := lt.Execute(ctx, "{}")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	// Output should be valid JSON array
	var schedules []json.RawMessage
	if err := json.Unmarshal([]byte(output), &schedules); err != nil {
		t.Fatalf("output is not valid JSON: %v\noutput: %s", err, output)
	}
	if len(schedules) != 2 {
		t.Errorf("expected 2 schedules in list, got %d", len(schedules))
	}
}
