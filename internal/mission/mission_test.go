package mission

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	_ "github.com/ncruces/go-sqlite3/driver"
)

// --- State machine tests ---

func TestState_IsTerminal(t *testing.T) {
	cases := map[State]bool{
		StateCreated:     false,
		StatePlanned:     false,
		StateRunning:     false,
		StateWaitingUser: false,
		StateCompleted:   true,
		StateFailed:      true,
		StateCancelled:   true,
	}
	for state, want := range cases {
		if got := state.IsTerminal(); got != want {
			t.Errorf("%s.IsTerminal() = %v, want %v", state, got, want)
		}
	}
}

func TestCanTransition(t *testing.T) {
	cases := []struct {
		from, to State
		want     bool
	}{
		// Forward path.
		{StateCreated, StatePlanned, true},
		{StatePlanned, StateRunning, true},
		{StateRunning, StateCompleted, true},
		{StateRunning, StateWaitingUser, true},
		{StateWaitingUser, StateRunning, true},
		// Same-state idempotent.
		{StateRunning, StateRunning, true},
		// Cancellation from anywhere non-terminal.
		{StateCreated, StateCancelled, true},
		{StatePlanned, StateCancelled, true},
		{StateRunning, StateCancelled, true},
		// Backwards transitions are NOT allowed.
		{StateRunning, StateCreated, false},
		{StatePlanned, StateCreated, false},
		// Skip-ahead transitions are NOT allowed.
		{StateCreated, StateRunning, false},
		{StateCreated, StateCompleted, false},
		// Terminal states are sinks.
		{StateCompleted, StateRunning, false},
		{StateFailed, StateCancelled, false},
		{StateCancelled, StateCompleted, false},
	}
	for _, c := range cases {
		got := CanTransition(c.from, c.to)
		if got != c.want {
			t.Errorf("CanTransition(%s, %s) = %v, want %v", c.from, c.to, got, c.want)
		}
	}
}

// --- SQLiteStore tests ---

const schemaSQL = `
CREATE TABLE missions (
    id                    TEXT PRIMARY KEY,
    goal                  TEXT NOT NULL,
    state                 TEXT NOT NULL DEFAULT 'created',
    plan_json             TEXT,
    budget_usd            REAL NOT NULL DEFAULT 0,
    cost_so_far_usd       REAL NOT NULL DEFAULT 0,
    supervisor_agent_id   TEXT,
    created_at            DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    started_at            DATETIME,
    completed_at          DATETIME
);
CREATE TABLE mission_steps (
    id                  TEXT PRIMARY KEY,
    mission_id          TEXT NOT NULL,
    task                TEXT NOT NULL,
    depends_on_json     TEXT,
    assigned_worker_id  TEXT,
    state               TEXT NOT NULL DEFAULT 'created',
    output              TEXT,
    error               TEXT,
    created_at          DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    started_at          DATETIME,
    completed_at        DATETIME
);
CREATE TABLE mission_events (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    mission_id   TEXT NOT NULL,
    timestamp    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    event_type   TEXT NOT NULL,
    payload_json TEXT
);`

func newMemDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec(schemaSQL); err != nil {
		t.Fatal(err)
	}
	return db
}

func TestStore_MissionCRUD(t *testing.T) {
	store := NewSQLiteStore(newMemDB(t))
	ctx := context.Background()

	m := Mission{ID: "mis_a", Goal: "test", State: StateCreated}
	if err := store.CreateMission(ctx, m); err != nil {
		t.Fatalf("create: %v", err)
	}

	got, err := store.GetMission(ctx, "mis_a")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Goal != "test" || got.State != StateCreated {
		t.Errorf("got: %+v", got)
	}

	got.State = StatePlanned
	got.PlanJSON = `{"steps":[]}`
	if err := store.UpdateMission(ctx, got); err != nil {
		t.Fatalf("update: %v", err)
	}
	got2, _ := store.GetMission(ctx, "mis_a")
	if got2.State != StatePlanned || got2.PlanJSON != `{"steps":[]}` {
		t.Errorf("update didn't persist: %+v", got2)
	}
}

func TestStore_GetMission_NotFound(t *testing.T) {
	store := NewSQLiteStore(newMemDB(t))
	_, err := store.GetMission(context.Background(), "nope")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestStore_UpdateMission_NotFound(t *testing.T) {
	store := NewSQLiteStore(newMemDB(t))
	err := store.UpdateMission(context.Background(), Mission{ID: "ghost", Goal: "x", State: StateCreated})
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestStore_ListMissions_Filters(t *testing.T) {
	store := NewSQLiteStore(newMemDB(t))
	ctx := context.Background()

	for i, ms := range []Mission{
		{ID: "mis_1", Goal: "running 1", State: StateRunning, SupervisorAgentID: "sup_a"},
		{ID: "mis_2", Goal: "done 1", State: StateCompleted, SupervisorAgentID: "sup_a"},
		{ID: "mis_3", Goal: "running 2", State: StateRunning, SupervisorAgentID: "sup_b"},
		{ID: "mis_4", Goal: "cancelled 1", State: StateCancelled, SupervisorAgentID: "sup_a"},
	} {
		ms.CreatedAt = time.Now().Add(time.Duration(i) * time.Second)
		if err := store.CreateMission(ctx, ms); err != nil {
			t.Fatal(err)
		}
	}

	t.Run("default excludes terminal", func(t *testing.T) {
		got, _ := store.ListMissions(ctx, ListFilter{})
		if len(got) != 2 {
			t.Errorf("default should exclude completed/cancelled, got %d", len(got))
		}
	})
	t.Run("include_done returns all", func(t *testing.T) {
		got, _ := store.ListMissions(ctx, ListFilter{IncludeDone: true})
		if len(got) != 4 {
			t.Errorf("expected 4 with include_done, got %d", len(got))
		}
	})
	t.Run("filter by state", func(t *testing.T) {
		got, _ := store.ListMissions(ctx, ListFilter{State: StateRunning})
		if len(got) != 2 {
			t.Errorf("expected 2 running, got %d", len(got))
		}
	})
	t.Run("filter by supervisor", func(t *testing.T) {
		got, _ := store.ListMissions(ctx, ListFilter{Supervisor: "sup_b", IncludeDone: true})
		if len(got) != 1 || got[0].ID != "mis_3" {
			t.Errorf("expected 1 row mis_3, got %+v", got)
		}
	})
}

func TestStore_StepCRUDAndDependencies(t *testing.T) {
	store := NewSQLiteStore(newMemDB(t))
	ctx := context.Background()

	_ = store.CreateMission(ctx, Mission{ID: "mis_x", Goal: "x", State: StatePlanned})
	step := Step{
		ID:        "step_1",
		MissionID: "mis_x",
		Task:      "do thing",
		DependsOn: []string{"step_0"},
		State:     StateCreated,
	}
	if err := store.AddStep(ctx, step); err != nil {
		t.Fatalf("add step: %v", err)
	}

	steps, err := store.GetSteps(ctx, "mis_x")
	if err != nil || len(steps) != 1 {
		t.Fatalf("GetSteps: err=%v rows=%d", err, len(steps))
	}
	if len(steps[0].DependsOn) != 1 || steps[0].DependsOn[0] != "step_0" {
		t.Errorf("DependsOn round-trip failed: %+v", steps[0].DependsOn)
	}

	steps[0].State = StateCompleted
	steps[0].Output = "done"
	if err := store.UpdateStep(ctx, steps[0]); err != nil {
		t.Fatalf("update step: %v", err)
	}
	got, _ := store.GetSteps(ctx, "mis_x")
	if got[0].State != StateCompleted || got[0].Output != "done" {
		t.Errorf("update didn't persist: %+v", got[0])
	}
}

func TestStore_EventLog_AppendAndQuery(t *testing.T) {
	store := NewSQLiteStore(newMemDB(t))
	ctx := context.Background()
	_ = store.CreateMission(ctx, Mission{ID: "mis_e", Goal: "x", State: StateCreated})

	for _, ev := range []Event{
		{MissionID: "mis_e", Type: "mission.created", PayloadJSON: `{"detail":"x"}`},
		{MissionID: "mis_e", Type: "worker.started"},
		{MissionID: "mis_e", Type: "worker.completed"},
	} {
		if err := store.AppendEvent(ctx, ev); err != nil {
			t.Fatalf("append: %v", err)
		}
	}

	events, err := store.GetEvents(ctx, "mis_e", 10)
	if err != nil || len(events) != 3 {
		t.Fatalf("GetEvents: err=%v rows=%d", err, len(events))
	}
	if events[0].Type != "mission.created" || events[2].Type != "worker.completed" {
		t.Errorf("ordering wrong: %+v", events)
	}
}

// --- Manager tests ---

func newTestManager(t *testing.T) (*Manager, Store) {
	t.Helper()
	store := NewSQLiteStore(newMemDB(t))
	mgr := NewManager(store)
	return mgr, store
}

func TestManager_Create_LogsEvent(t *testing.T) {
	mgr, store := newTestManager(t)
	ctx := context.Background()
	m, err := mgr.Create(ctx, "build a thing", "sup_x", 2.50)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if !strings.HasPrefix(m.ID, "mis_") {
		t.Errorf("expected ID prefix mis_, got %q", m.ID)
	}
	if m.State != StateCreated {
		t.Errorf("state = %s, want created", m.State)
	}

	events, _ := store.GetEvents(ctx, m.ID, 10)
	if len(events) != 1 || events[0].Type != "mission.created" {
		t.Errorf("expected one mission.created event, got %+v", events)
	}
}

func TestManager_Create_EmptyGoal(t *testing.T) {
	mgr, _ := newTestManager(t)
	if _, err := mgr.Create(context.Background(), "  ", "", 0); !errors.Is(err, ErrEmptyGoal) {
		t.Errorf("expected ErrEmptyGoal, got %v", err)
	}
}

func TestManager_Lifecycle_HappyPath(t *testing.T) {
	mgr, store := newTestManager(t)
	ctx := context.Background()

	m, err := mgr.Create(ctx, "happy path", "", 0)
	if err != nil {
		t.Fatal(err)
	}

	if err := mgr.SetPlan(ctx, m.ID, []Step{{Task: "do A"}, {Task: "do B"}}); err != nil {
		t.Fatalf("plan: %v", err)
	}
	steps, _ := store.GetSteps(ctx, m.ID)
	if len(steps) != 2 {
		t.Errorf("expected 2 steps, got %d", len(steps))
	}
	for _, s := range steps {
		if s.ID == "" {
			t.Error("step should get a server-allocated ID")
		}
	}

	if err := mgr.Start(ctx, m.ID); err != nil {
		t.Fatalf("start: %v", err)
	}
	got, _ := store.GetMission(ctx, m.ID)
	if got.StartedAt == nil {
		t.Error("StartedAt should be set")
	}

	if err := mgr.Complete(ctx, m.ID, "done"); err != nil {
		t.Fatalf("complete: %v", err)
	}
	got2, _ := store.GetMission(ctx, m.ID)
	if got2.State != StateCompleted || got2.CompletedAt == nil {
		t.Errorf("expected completed with timestamp, got %+v", got2)
	}

	events, _ := store.GetEvents(ctx, m.ID, 10)
	wantTypes := []string{"mission.created", "mission.planned", "mission.started", "mission.completed"}
	if len(events) != len(wantTypes) {
		t.Fatalf("expected %d events, got %d", len(wantTypes), len(events))
	}
	for i, w := range wantTypes {
		if events[i].Type != w {
			t.Errorf("event[%d] = %q, want %q", i, events[i].Type, w)
		}
	}
}

func TestManager_InvalidTransition(t *testing.T) {
	mgr, _ := newTestManager(t)
	ctx := context.Background()
	m, _ := mgr.Create(ctx, "x", "", 0)

	// Skip-ahead: created → running is not allowed (must go through planned).
	err := mgr.Start(ctx, m.ID)
	if !errors.Is(err, ErrInvalidTransition) {
		t.Errorf("expected ErrInvalidTransition for created→running skip, got %v", err)
	}
}

func TestManager_TerminalIsSink(t *testing.T) {
	mgr, _ := newTestManager(t)
	ctx := context.Background()
	m, _ := mgr.Create(ctx, "x", "", 0)
	_ = mgr.SetPlan(ctx, m.ID, []Step{{Task: "a"}})
	_ = mgr.Start(ctx, m.ID)
	_ = mgr.Complete(ctx, m.ID, "done")

	// Now try to resume the completed mission.
	if err := mgr.Resume(ctx, m.ID); !errors.Is(err, ErrTerminal) {
		t.Errorf("expected ErrTerminal once completed, got %v", err)
	}
}

func TestManager_PauseResume(t *testing.T) {
	mgr, store := newTestManager(t)
	ctx := context.Background()
	m, _ := mgr.Create(ctx, "x", "", 0)
	_ = mgr.SetPlan(ctx, m.ID, []Step{{Task: "a"}})
	_ = mgr.Start(ctx, m.ID)

	if err := mgr.Pause(ctx, m.ID, "user away"); err != nil {
		t.Fatalf("pause: %v", err)
	}
	got, _ := store.GetMission(ctx, m.ID)
	if got.State != StateWaitingUser {
		t.Errorf("expected waiting_user, got %s", got.State)
	}

	if err := mgr.Resume(ctx, m.ID); err != nil {
		t.Fatalf("resume: %v", err)
	}
	got, _ = store.GetMission(ctx, m.ID)
	if got.State != StateRunning {
		t.Errorf("expected running after resume, got %s", got.State)
	}
}

func TestManager_AppendEvent_FreeForm(t *testing.T) {
	mgr, store := newTestManager(t)
	ctx := context.Background()
	m, _ := mgr.Create(ctx, "x", "", 0)

	if err := mgr.AppendEvent(ctx, m.ID, "worker.progress", map[string]interface{}{
		"step_id": "step_1", "percent": 42,
	}); err != nil {
		t.Fatalf("append: %v", err)
	}
	events, _ := store.GetEvents(ctx, m.ID, 10)
	// Index 0 is mission.created; 1 is the appended one.
	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(events))
	}
	if events[1].Type != "worker.progress" {
		t.Errorf("type = %q, want worker.progress", events[1].Type)
	}
	var payload map[string]interface{}
	_ = json.Unmarshal([]byte(events[1].PayloadJSON), &payload)
	if payload["step_id"] != "step_1" {
		t.Errorf("payload didn't round-trip: %+v", payload)
	}
}

// --- Tool registration tests ---

type mockRegistry struct {
	tools []string
	exec  map[string]func(ctx context.Context, input string) (string, error)
}

func newMockRegistry() *mockRegistry {
	return &mockRegistry{exec: make(map[string]func(ctx context.Context, input string) (string, error))}
}

func (m *mockRegistry) Register(name, _, _, _ string, exec func(ctx context.Context, input string) (string, error)) {
	m.tools = append(m.tools, name)
	m.exec[name] = exec
}

func TestRegisterTools_AllToolsRegistered(t *testing.T) {
	store := NewSQLiteStore(newMemDB(t))
	mgr := NewManager(store)
	reg := newMockRegistry()
	RegisterTools(reg, mgr, store)

	want := []string{"mission.create", "mission.list", "mission.get", "mission.events"}
	for _, n := range want {
		if _, ok := reg.exec[n]; !ok {
			t.Errorf("tool %q not registered", n)
		}
	}
}

func TestTool_Create_ReturnsMissionJSON(t *testing.T) {
	store := NewSQLiteStore(newMemDB(t))
	mgr := NewManager(store)
	reg := newMockRegistry()
	RegisterTools(reg, mgr, store)

	out, err := reg.exec["mission.create"](context.Background(), `{"goal":"do a thing","budget_usd":1.5}`)
	if err != nil {
		t.Fatalf("create tool: %v", err)
	}
	var m Mission
	if err := json.Unmarshal([]byte(out), &m); err != nil {
		t.Fatalf("output not JSON Mission: %v\nout: %s", err, out)
	}
	if m.Goal != "do a thing" || m.BudgetUSD != 1.5 {
		t.Errorf("unexpected mission: %+v", m)
	}
}

func TestTool_Create_MissingGoal(t *testing.T) {
	store := NewSQLiteStore(newMemDB(t))
	mgr := NewManager(store)
	reg := newMockRegistry()
	RegisterTools(reg, mgr, store)

	_, err := reg.exec["mission.create"](context.Background(), `{}`)
	if err == nil {
		t.Error("expected error for empty goal")
	}
}

func TestTool_Get_BundlesMissionStepsAndEvents(t *testing.T) {
	store := NewSQLiteStore(newMemDB(t))
	mgr := NewManager(store)
	reg := newMockRegistry()
	RegisterTools(reg, mgr, store)

	m, _ := mgr.Create(context.Background(), "x", "", 0)
	_ = mgr.SetPlan(context.Background(), m.ID, []Step{{Task: "a"}})

	out, err := reg.exec["mission.get"](context.Background(), `{"id":"`+m.ID+`"}`)
	if err != nil {
		t.Fatalf("get tool: %v", err)
	}
	var detail missionDetail
	if err := json.Unmarshal([]byte(out), &detail); err != nil {
		t.Fatalf("output not missionDetail: %v\nout: %s", err, out)
	}
	if detail.Mission.ID != m.ID {
		t.Errorf("Mission round-trip wrong: %+v", detail.Mission)
	}
	if len(detail.Steps) != 1 {
		t.Errorf("expected 1 step in detail, got %d", len(detail.Steps))
	}
	if len(detail.Events) < 2 {
		t.Errorf("expected at least 2 events (created + planned), got %d", len(detail.Events))
	}
}

func TestTool_List_FilterByState(t *testing.T) {
	store := NewSQLiteStore(newMemDB(t))
	mgr := NewManager(store)
	reg := newMockRegistry()
	RegisterTools(reg, mgr, store)

	for i := 0; i < 3; i++ {
		_, _ = mgr.Create(context.Background(), "g", "", 0)
	}

	out, _ := reg.exec["mission.list"](context.Background(), `{"state":"created"}`)
	var got []Mission
	_ = json.Unmarshal([]byte(out), &got)
	if len(got) != 3 {
		t.Errorf("expected 3 created missions, got %d", len(got))
	}
}

func TestTool_Events_QueryLog(t *testing.T) {
	store := NewSQLiteStore(newMemDB(t))
	mgr := NewManager(store)
	reg := newMockRegistry()
	RegisterTools(reg, mgr, store)

	m, _ := mgr.Create(context.Background(), "x", "", 0)
	_ = mgr.AppendEvent(context.Background(), m.ID, "worker.progress", nil)

	out, _ := reg.exec["mission.events"](context.Background(), `{"id":"`+m.ID+`"}`)
	var events []Event
	_ = json.Unmarshal([]byte(out), &events)
	if len(events) != 2 {
		t.Errorf("expected 2 events, got %d", len(events))
	}
}

func TestTool_InvalidJSON(t *testing.T) {
	store := NewSQLiteStore(newMemDB(t))
	mgr := NewManager(store)
	reg := newMockRegistry()
	RegisterTools(reg, mgr, store)

	for _, name := range []string{"mission.create", "mission.get", "mission.events"} {
		if _, err := reg.exec[name](context.Background(), `not json`); err == nil {
			t.Errorf("%s: expected error for invalid JSON", name)
		}
	}
}

// --- mission.interrupt tool tests ---

func TestTool_Interrupt_PauseAndResume(t *testing.T) {
	store := NewSQLiteStore(newMemDB(t))
	mgr := NewManager(store)
	reg := newMockRegistry()
	RegisterTools(reg, mgr, store)
	ctx := context.Background()

	// Build a mission and walk it to running.
	m, _ := mgr.Create(ctx, "x", "", 0)
	_ = mgr.SetPlan(ctx, m.ID, []Step{{Task: "a"}})
	_ = mgr.Start(ctx, m.ID)

	// Pause via tool.
	out, err := reg.exec["mission.interrupt"](ctx, `{"id":"`+m.ID+`","action":"pause","reason":"user away"}`)
	if err != nil {
		t.Fatalf("pause: %v", err)
	}
	var resp map[string]interface{}
	_ = json.Unmarshal([]byte(out), &resp)
	if resp["state"] != "waiting_user" {
		t.Errorf("after pause, state = %v, want waiting_user", resp["state"])
	}

	// Resume via tool.
	out, err = reg.exec["mission.interrupt"](ctx, `{"id":"`+m.ID+`","action":"resume"}`)
	if err != nil {
		t.Fatalf("resume: %v", err)
	}
	_ = json.Unmarshal([]byte(out), &resp)
	if resp["state"] != "running" {
		t.Errorf("after resume, state = %v, want running", resp["state"])
	}
}

func TestTool_Interrupt_Cancel(t *testing.T) {
	store := NewSQLiteStore(newMemDB(t))
	mgr := NewManager(store)
	reg := newMockRegistry()
	RegisterTools(reg, mgr, store)
	ctx := context.Background()

	m, _ := mgr.Create(ctx, "x", "", 0)
	_ = mgr.SetPlan(ctx, m.ID, []Step{{Task: "a"}})
	_ = mgr.Start(ctx, m.ID)

	out, err := reg.exec["mission.interrupt"](ctx, `{"id":"`+m.ID+`","action":"cancel","reason":"user stopped"}`)
	if err != nil {
		t.Fatalf("cancel: %v", err)
	}
	var resp map[string]interface{}
	_ = json.Unmarshal([]byte(out), &resp)
	if resp["state"] != "cancelled" {
		t.Errorf("after cancel, state = %v, want cancelled", resp["state"])
	}
}

func TestTool_Interrupt_UnknownAction(t *testing.T) {
	store := NewSQLiteStore(newMemDB(t))
	mgr := NewManager(store)
	reg := newMockRegistry()
	RegisterTools(reg, mgr, store)
	ctx := context.Background()

	m, _ := mgr.Create(ctx, "x", "", 0)
	_, err := reg.exec["mission.interrupt"](ctx, `{"id":"`+m.ID+`","action":"reboot"}`)
	if err == nil {
		t.Fatal("expected error for unknown action")
	}
	if !strings.Contains(err.Error(), "pause") {
		t.Errorf("error should list valid actions, got: %v", err)
	}
}

func TestTool_Interrupt_EmptyID(t *testing.T) {
	store := NewSQLiteStore(newMemDB(t))
	mgr := NewManager(store)
	reg := newMockRegistry()
	RegisterTools(reg, mgr, store)

	_, err := reg.exec["mission.interrupt"](context.Background(), `{"action":"pause"}`)
	if err == nil {
		t.Fatal("expected error for empty id")
	}
}

func TestTool_Interrupt_MissionNotFound(t *testing.T) {
	store := NewSQLiteStore(newMemDB(t))
	mgr := NewManager(store)
	reg := newMockRegistry()
	RegisterTools(reg, mgr, store)

	_, err := reg.exec["mission.interrupt"](context.Background(), `{"id":"ghost","action":"pause"}`)
	if err == nil {
		t.Fatal("expected error for nonexistent mission")
	}
}

func TestTool_Interrupt_RegisteredAlongsideOthers(t *testing.T) {
	store := NewSQLiteStore(newMemDB(t))
	mgr := NewManager(store)
	reg := newMockRegistry()
	RegisterTools(reg, mgr, store)

	want := []string{"mission.create", "mission.list", "mission.get", "mission.events", "mission.interrupt"}
	for _, n := range want {
		if _, ok := reg.exec[n]; !ok {
			t.Errorf("tool %q not registered", n)
		}
	}
}

// --- Replan tests ---

// TestManager_Replan_PersistsNewStepsAndSupersedesUnstarted verifies the
// happy path: a running mission with a failed step gets new replacement
// steps appended, the un-started original steps are marked
// StateCancelled with the supersede reason, and a mission.replanned
// event captures the audit trail.
func TestManager_Replan_PersistsNewStepsAndSupersedesUnstarted(t *testing.T) {
	mgr, store := newTestManager(t)
	ctx := context.Background()

	m, _ := mgr.Create(ctx, "rebuild after failure", "", 0)
	_ = mgr.SetPlan(ctx, m.ID, []Step{
		{Task: "do A"},
		{Task: "do B"},
		{Task: "do C"},
	})
	_ = mgr.Start(ctx, m.ID)

	steps, _ := store.GetSteps(ctx, m.ID)
	// Mark A completed, B failed (the trigger).
	steps[0].State = StateCompleted
	steps[0].Output = "did A"
	_ = store.UpdateStep(ctx, steps[0])
	steps[1].State = StateFailed
	steps[1].Error = "B blew up"
	_ = store.UpdateStep(ctx, steps[1])
	failedID := steps[1].ID
	cStepID := steps[2].ID

	// Replan: replacement is two new steps after B.
	err := mgr.Replan(ctx, m.ID, failedID, []Step{
		{Task: "do B-prime"},
		{Task: "do D"},
	}, "B failed, here's the recovery")
	if err != nil {
		t.Fatalf("Replan: %v", err)
	}

	got, _ := store.GetSteps(ctx, m.ID)
	if len(got) != 5 {
		t.Fatalf("steps count = %d, want 5 (A, B, C, B-prime, D)", len(got))
	}

	// A still completed, B still failed, C now cancelled (superseded),
	// B-prime + D new in state=created.
	byID := map[string]Step{}
	for _, st := range got {
		byID[st.ID] = st
	}
	if s := byID[steps[0].ID]; s.State != StateCompleted {
		t.Errorf("A state = %s, want completed", s.State)
	}
	if s := byID[failedID]; s.State != StateFailed {
		t.Errorf("B (failed) state = %s, want failed (unchanged by replan)", s.State)
	}
	c := byID[cStepID]
	if c.State != StateCancelled {
		t.Errorf("C state = %s, want cancelled (superseded by replan)", c.State)
	}
	if c.Error != "superseded by replan" {
		t.Errorf("C error = %q, want 'superseded by replan'", c.Error)
	}
	if c.CompletedAt == nil {
		t.Error("C CompletedAt should be set on cancellation")
	}

	// Find replacement steps (state=created, not in the original three).
	originalIDs := map[string]bool{steps[0].ID: true, failedID: true, cStepID: true}
	var replacements []Step
	for _, st := range got {
		if !originalIDs[st.ID] {
			replacements = append(replacements, st)
		}
	}
	if len(replacements) != 2 {
		t.Fatalf("replacement count = %d, want 2", len(replacements))
	}
	for _, r := range replacements {
		if r.State != StateCreated {
			t.Errorf("replacement step %s state = %s, want created", r.ID, r.State)
		}
		if r.Task != "do B-prime" && r.Task != "do D" {
			t.Errorf("replacement task = %q, want one of [do B-prime, do D]", r.Task)
		}
	}

	// PlanJSON updated to the new step list.
	mi, _ := store.GetMission(ctx, m.ID)
	plan := PlanFromJSON(mi.PlanJSON)
	if len(plan.Steps) != 5 {
		t.Errorf("PlanJSON steps = %d, want 5", len(plan.Steps))
	}

	// Mission state unchanged (still running).
	if mi.State != StateRunning {
		t.Errorf("mission state = %s, want running (replan does not transition)", mi.State)
	}

	// mission.replanned event with payload containing the failed ID.
	events, _ := store.GetEvents(ctx, m.ID, 100)
	var replanEvt *Event
	for i := range events {
		if events[i].Type == "mission.replanned" {
			replanEvt = &events[i]
		}
	}
	if replanEvt == nil {
		t.Fatal("expected mission.replanned event in log")
	}
	if !strings.Contains(replanEvt.PayloadJSON, failedID) {
		t.Errorf("event payload should mention failed step %s, got %q", failedID, replanEvt.PayloadJSON)
	}
	if !strings.Contains(replanEvt.PayloadJSON, cStepID) {
		t.Errorf("event payload should mention superseded step %s, got %q", cStepID, replanEvt.PayloadJSON)
	}
}

// TestManager_Replan_RejectsNonRunningMission verifies the state guard.
func TestManager_Replan_RejectsNonRunningMission(t *testing.T) {
	mgr, store := newTestManager(t)
	ctx := context.Background()
	m, _ := mgr.Create(ctx, "not started yet", "", 0)
	_ = mgr.SetPlan(ctx, m.ID, []Step{{Task: "do A"}})
	steps, _ := store.GetSteps(ctx, m.ID)

	err := mgr.Replan(ctx, m.ID, steps[0].ID, []Step{{Task: "rescue"}}, "fail")
	if err == nil {
		t.Fatal("expected error replanning a planned mission")
	}
	if !errors.Is(err, ErrInvalidTransition) {
		t.Errorf("error = %v, want wraps ErrInvalidTransition", err)
	}
}

// TestManager_Replan_RejectsUnknownStep verifies the ID validation.
func TestManager_Replan_RejectsUnknownStep(t *testing.T) {
	mgr, _ := newTestManager(t)
	ctx := context.Background()
	m, _ := mgr.Create(ctx, "active", "", 0)
	_ = mgr.SetPlan(ctx, m.ID, []Step{{Task: "do A"}})
	_ = mgr.Start(ctx, m.ID)

	err := mgr.Replan(ctx, m.ID, "step_does_not_exist", []Step{{Task: "rescue"}}, "fail")
	if err == nil {
		t.Fatal("expected error for unknown step ID")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error = %v, want 'not found'", err)
	}
}

// TestManager_Replan_RejectsEmptyNewSteps verifies the input validation.
func TestManager_Replan_RejectsEmptyNewSteps(t *testing.T) {
	mgr, store := newTestManager(t)
	ctx := context.Background()
	m, _ := mgr.Create(ctx, "active", "", 0)
	_ = mgr.SetPlan(ctx, m.ID, []Step{{Task: "do A"}})
	_ = mgr.Start(ctx, m.ID)
	steps, _ := store.GetSteps(ctx, m.ID)

	err := mgr.Replan(ctx, m.ID, steps[0].ID, nil, "fail")
	if err == nil {
		t.Fatal("expected error for empty new step list")
	}
}

// TestManager_Replan_SupersedesCreatedStepsBeforeFailure validates the
// v0.4.1 generalization: replanning supersedes EVERY still-created step,
// not just those positioned after the failed step. In parallel mode an
// unstarted step (blocked on a dependency) can sit before the failed
// step in created_at order; it must be superseded too, or it would
// orphan the post-replan DAG.
func TestManager_Replan_SupersedesCreatedStepsBeforeFailure(t *testing.T) {
	mgr, store := newTestManager(t)
	ctx := context.Background()

	m, _ := mgr.Create(ctx, "parallel replan supersede", "", 0)
	_ = mgr.SetPlanParallel(ctx, m.ID, []Step{
		{Task: "A-still-created"}, // earlier in created order, never started
		{Task: "B-completed"},
		{Task: "C-failed"}, // the failure trigger, LAST in created order
	})
	_ = mgr.Start(ctx, m.ID)

	steps, _ := store.GetSteps(ctx, m.ID)
	aID := steps[0].ID
	// A stays created (simulating a step blocked on a dependency).
	steps[1].State = StateCompleted
	steps[1].Output = "did B"
	_ = store.UpdateStep(ctx, steps[1])
	steps[2].State = StateFailed
	steps[2].Error = "C blew up"
	_ = store.UpdateStep(ctx, steps[2])
	failedID := steps[2].ID

	if err := mgr.Replan(ctx, m.ID, failedID, []Step{{Task: "RECOVER"}}, "C failed"); err != nil {
		t.Fatalf("Replan: %v", err)
	}

	got, _ := store.GetSteps(ctx, m.ID)
	byID := map[string]Step{}
	for _, st := range got {
		byID[st.ID] = st
	}
	// A (created, BEFORE the failed step) must be superseded → cancelled.
	if byID[aID].State != StateCancelled {
		t.Errorf("A state = %s, want cancelled (superseded even though it precedes the failed step)", byID[aID].State)
	}
	if byID[aID].Error != "superseded by replan" {
		t.Errorf("A error = %q, want 'superseded by replan'", byID[aID].Error)
	}
	// The failed step stays failed (audit relic).
	if byID[failedID].State != StateFailed {
		t.Errorf("failed step state = %s, want failed (unchanged)", byID[failedID].State)
	}
	// Parallel flag preserved across the replan.
	mi, _ := store.GetMission(ctx, m.ID)
	if !PlanFromJSON(mi.PlanJSON).Parallel {
		t.Error("Parallel flag lost across replan")
	}
}
