package mission

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
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
func (m *Manager) SetPlan(ctx context.Context, missionID string, steps []Step) error {
	mission, err := m.store.GetMission(ctx, missionID)
	if err != nil {
		return err
	}
	if !CanTransition(mission.State, StatePlanned) {
		return fmt.Errorf("%w: %s → %s", ErrInvalidTransition, mission.State, StatePlanned)
	}

	planJSON, _ := json.Marshal(struct {
		Steps []Step `json:"steps"`
	}{Steps: steps})
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
	_ = m.appendStateEvent(ctx, missionID, "mission.planned", fmt.Sprintf("%d steps", len(steps)))
	return nil
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
