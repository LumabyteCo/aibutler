package mission

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Manager orchestrates the mission state machine over a Store. It enforces
// allowed state transitions and emits one mission_events entry per
// state change so the audit trail is automatic.
//
// The supervisor agent (a separate Theme 2 commit) drives the Manager —
// for now the API is callable directly via the mission.* tools, which
// lets a human or the existing single-agent loop manage missions
// end-to-end before the supervisor logic lands.
type Manager struct {
	store Store
	now   func() time.Time // overridable for tests
}

// NewManager creates a Manager backed by the given Store.
func NewManager(s Store) *Manager {
	return &Manager{store: s, now: time.Now}
}

// SetClock overrides the time source (for deterministic tests).
func (m *Manager) SetClock(fn func() time.Time) { m.now = fn }

// Create starts a new mission in the `created` state. Returns the full
// Mission record with a server-allocated ID.
func (m *Manager) Create(ctx context.Context, goal, supervisorAgentID string, budgetUSD float64) (Mission, error) {
	goal = strings.TrimSpace(goal)
	if goal == "" {
		return Mission{}, ErrEmptyGoal
	}
	mission := Mission{
		ID:                "mis_" + randID(12),
		Goal:              goal,
		State:             StateCreated,
		BudgetUSD:         budgetUSD,
		SupervisorAgentID: supervisorAgentID,
		CreatedAt:         m.now(),
	}
	if err := m.store.CreateMission(ctx, mission); err != nil {
		return Mission{}, err
	}
	_ = m.appendStateEvent(ctx, mission.ID, "mission.created", goal)
	return mission, nil
}

// SetPlan transitions the mission to the planned state and records the
// plan steps. Each step gets a server-allocated ID.
//
// Steps run sequentially under SetPlan — see SetPlanParallel for DAG
// dispatch. The two methods share storage; the only difference is the
// Plan.Parallel flag persisted in Mission.PlanJSON.
func (m *Manager) SetPlan(ctx context.Context, missionID string, steps []Step) error {
	return m.setPlan(ctx, missionID, steps, false)
}

// SetPlanParallel is like SetPlan but enables parallel dispatch. The
// supervisor walks Step.DependsOn as a DAG and runs steps with
// satisfied dependencies concurrently. Steps with no dependencies are
// ready immediately.
func (m *Manager) SetPlanParallel(ctx context.Context, missionID string, steps []Step) error {
	return m.setPlan(ctx, missionID, steps, true)
}

func (m *Manager) setPlan(ctx context.Context, missionID string, steps []Step, parallel bool) error {
	mission, err := m.store.GetMission(ctx, missionID)
	if err != nil {
		return err
	}
	if !CanTransition(mission.State, StatePlanned) {
		return fmt.Errorf("%w: %s → %s", ErrInvalidTransition, mission.State, StatePlanned)
	}

	planJSON, _ := json.Marshal(Plan{Steps: steps, Parallel: parallel})
	mission.State = StatePlanned
	mission.PlanJSON = string(planJSON)
	if err := m.store.UpdateMission(ctx, mission); err != nil {
		return err
	}

	for i := range steps {
		if steps[i].ID == "" {
			steps[i].ID = "step_" + randID(10)
		}
		steps[i].MissionID = missionID
		if steps[i].State == "" {
			steps[i].State = StateCreated
		}
		if err := m.store.AddStep(ctx, steps[i]); err != nil {
			return err
		}
	}
	msg := fmt.Sprintf("%d steps", len(steps))
	if parallel {
		msg += " (parallel)"
	}
	_ = m.appendStateEvent(ctx, missionID, "mission.planned", msg)
	return nil
}

// Replan replaces the un-started portion of a mission's plan with a
// new step sequence. The mission must already be in the running state —
// replanning is a mid-flight recovery, not a fresh plan.
//
// Semantics:
//
//   - The fromStepID step stays in state=failed (or whatever its current
//     state is) — replanning does NOT rewrite history. The audit log
//     still shows the failure as it happened.
//   - All steps that come after fromStepID (in created_at order) AND
//     are still in state=created get marked StateCancelled with reason
//     "superseded by replan". The original plan's remaining work was
//     predicated on the failed step's assumed output; the replan
//     supersedes those assumptions wholesale.
//   - newSteps get fresh server-allocated IDs and state=created and are
//     appended to the mission_steps table in order. The supervisor's
//     loop picks them up on its next iteration.
//   - Mission.PlanJSON is rewritten to the union of all steps so
//     dashboards and replay tools see the current plan.
//   - The Plan.Parallel flag from the original plan is preserved (a
//     parallel mission stays parallel after replan).
//   - One mission_events row is appended: {event_type: "mission.replanned",
//     payload: {from_step_id, new_step_count, superseded_step_ids, reason}}.
//
// Returns ErrInvalidTransition if the mission is not in StateRunning,
// and a wrapped sql error if any persistence step fails. Replan does
// NOT change Mission.State — the mission stays running.
func (m *Manager) Replan(ctx context.Context, missionID, fromStepID string, newSteps []Step, reason string) error {
	mi, err := m.store.GetMission(ctx, missionID)
	if err != nil {
		return err
	}
	if mi.State != StateRunning {
		return fmt.Errorf("%w: replan requires running state, got %s", ErrInvalidTransition, mi.State)
	}
	if fromStepID == "" {
		return errors.New("mission: replan fromStepID is required")
	}
	if len(newSteps) == 0 {
		return errors.New("mission: replan requires at least one new step")
	}

	existing, err := m.store.GetSteps(ctx, missionID)
	if err != nil {
		return err
	}

	// Locate the failed step. A stale or unknown ID surfaces as an
	// error rather than silently rewriting the plan against a
	// mismatched cursor.
	failedIdx := -1
	for i := range existing {
		if existing[i].ID == fromStepID {
			failedIdx = i
			break
		}
	}
	if failedIdx < 0 {
		return fmt.Errorf("mission: replan target step %s not found in mission %s", fromStepID, missionID)
	}

	now := m.now()

	// Supersede every un-started step that comes after the failed one.
	// Only state=created steps are touched — running/completed/failed
	// stay as they are. (In sequential mode there are no concurrent
	// running steps; parallel replan is deferred.)
	supersededIDs := []string{}
	for i := failedIdx + 1; i < len(existing); i++ {
		st := existing[i]
		if st.State != StateCreated {
			continue
		}
		st.State = StateCancelled
		st.Error = "superseded by replan"
		if st.CompletedAt == nil {
			t := now
			st.CompletedAt = &t
		}
		if err := m.store.UpdateStep(ctx, st); err != nil {
			return fmt.Errorf("supersede step %s: %w", st.ID, err)
		}
		supersededIDs = append(supersededIDs, st.ID)
	}

	// Allocate IDs and persist the replacement steps in order. The
	// CreatedAt stagger keeps the canonical (created_at, ID) order
	// stable so the supervisor's "first non-terminal" pick is
	// deterministic.
	for i := range newSteps {
		if newSteps[i].ID == "" {
			newSteps[i].ID = "step_" + randID(10)
		}
		newSteps[i].MissionID = missionID
		if newSteps[i].State == "" {
			newSteps[i].State = StateCreated
		}
		if newSteps[i].CreatedAt.IsZero() {
			newSteps[i].CreatedAt = now.Add(time.Duration(i+1) * time.Nanosecond)
		}
		if err := m.store.AddStep(ctx, newSteps[i]); err != nil {
			return err
		}
	}

	// Refresh and rewrite PlanJSON. We re-read from store to pick up
	// the updated step rows in canonical (created_at, ID) order.
	refreshed, err := m.store.GetSteps(ctx, missionID)
	if err != nil {
		return err
	}
	parallel := PlanFromJSON(mi.PlanJSON).Parallel
	planJSON, _ := json.Marshal(Plan{Steps: refreshed, Parallel: parallel})
	mi.PlanJSON = string(planJSON)
	if err := m.store.UpdateMission(ctx, mi); err != nil {
		return err
	}

	payload, _ := json.Marshal(map[string]interface{}{
		"from_step_id":        fromStepID,
		"new_step_count":      len(newSteps),
		"superseded_step_ids": supersededIDs,
		"reason":              reason,
	})
	return m.store.AppendEvent(ctx, Event{
		MissionID:   missionID,
		Type:        "mission.replanned",
		PayloadJSON: string(payload),
		Timestamp:   now,
	})
}

// PlanFromJSON parses a mission's PlanJSON. Returns a zero-valued Plan
// if json is empty (mission not yet planned) or malformed.
func PlanFromJSON(planJSON string) Plan {
	var p Plan
	if planJSON == "" {
		return p
	}
	_ = json.Unmarshal([]byte(planJSON), &p)
	return p
}

// Start moves a planned mission into running. Records the wall-clock
// start time on the mission row.
func (m *Manager) Start(ctx context.Context, missionID string) error {
	return m.transition(ctx, missionID, StateRunning, "mission.started", func(mi *Mission) {
		t := m.now()
		mi.StartedAt = &t
	})
}

// Pause moves a running mission into waiting_user — the supervisor surfaces
// a confirmation prompt; the mission resumes when Resume is called.
func (m *Manager) Pause(ctx context.Context, missionID, reason string) error {
	return m.transition(ctx, missionID, StateWaitingUser, "mission.paused", nil, reason)
}

// Resume moves a waiting_user mission back into running.
func (m *Manager) Resume(ctx context.Context, missionID string) error {
	return m.transition(ctx, missionID, StateRunning, "mission.resumed", nil)
}

// Complete marks the mission completed (success). Records the wall-clock
// finish time.
func (m *Manager) Complete(ctx context.Context, missionID, summary string) error {
	return m.transition(ctx, missionID, StateCompleted, "mission.completed", func(mi *Mission) {
		t := m.now()
		mi.CompletedAt = &t
	}, summary)
}

// Fail marks the mission failed (terminal — replan budget exhausted or
// fatal error).
func (m *Manager) Fail(ctx context.Context, missionID, reason string) error {
	return m.transition(ctx, missionID, StateFailed, "mission.failed", func(mi *Mission) {
		t := m.now()
		mi.CompletedAt = &t
	}, reason)
}

// Cancel marks the mission cancelled (terminal — user stopped or budget
// exhausted before completion).
func (m *Manager) Cancel(ctx context.Context, missionID, reason string) error {
	return m.transition(ctx, missionID, StateCancelled, "mission.cancelled", func(mi *Mission) {
		t := m.now()
		mi.CompletedAt = &t
	}, reason)
}

// AppendEvent writes an arbitrary entry to the mission's event log. Used
// by workers to report progress (worker.started, worker.progress, etc.).
func (m *Manager) AppendEvent(ctx context.Context, missionID, eventType string, payload interface{}) error {
	payloadJSON := ""
	if payload != nil {
		b, _ := json.Marshal(payload)
		payloadJSON = string(b)
	}
	return m.store.AppendEvent(ctx, Event{
		MissionID:   missionID,
		Type:        eventType,
		PayloadJSON: payloadJSON,
		Timestamp:   m.now(),
	})
}

// transition atomically validates + applies a state change with an
// optional mutator. Every transition is logged to the event log.
func (m *Manager) transition(ctx context.Context, missionID string, to State, eventType string, mutate func(*Mission), reason ...string) error {
	mission, err := m.store.GetMission(ctx, missionID)
	if err != nil {
		return err
	}
	if mission.State.IsTerminal() {
		return fmt.Errorf("%w: %s", ErrTerminal, mission.State)
	}
	if !CanTransition(mission.State, to) {
		return fmt.Errorf("%w: %s → %s", ErrInvalidTransition, mission.State, to)
	}
	mission.State = to
	if mutate != nil {
		mutate(&mission)
	}
	if err := m.store.UpdateMission(ctx, mission); err != nil {
		return err
	}
	payload := ""
	if len(reason) > 0 {
		payload = reason[0]
	}
	return m.appendStateEvent(ctx, missionID, eventType, payload)
}

func (m *Manager) appendStateEvent(ctx context.Context, missionID, eventType, payload string) error {
	jsonPayload := ""
	if payload != "" {
		b, _ := json.Marshal(map[string]string{"detail": payload})
		jsonPayload = string(b)
	}
	return m.store.AppendEvent(ctx, Event{
		MissionID:   missionID,
		Type:        eventType,
		PayloadJSON: jsonPayload,
		Timestamp:   m.now(),
	})
}

// randID returns a hex-encoded random string of `bytes*2` characters.
func randID(bytes int) string {
	buf := make([]byte, bytes)
	_, _ = rand.Read(buf)
	return hex.EncodeToString(buf)
}
