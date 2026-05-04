// Package mission defines the mission data model — the persistent,
// replannable goal a hierarchy of agents collaborates to achieve.
//
// Where the existing agent loop handles one user turn at a time, missions
// introduce long-running work units with their own state machine, plan,
// budget, and audit trail. This package is the foundation: types, state
// machine, persistence, manager API. Supervisor/worker orchestration, bus
// refactor, mode plumbing, and dashboard panel are separate follow-up
// commits — by design, this commit ships only the data layer.
//
// Lifecycle:
//
//	created → planned → running ⇄ waiting_user → completed
//	                       │
//	                       ├──→ failed
//	                       │
//	                       └──→ cancelled
//
// State transitions are enforced by the Manager: requests that don't
// match the allowed graph return ErrInvalidTransition rather than
// silently corrupt state.
package mission

import (
	"errors"
	"time"
)

// State enumerates the mission lifecycle.
type State string

const (
	// StateCreated — mission record exists, no plan yet.
	StateCreated State = "created"
	// StatePlanned — plan steps are recorded, no workers running.
	StatePlanned State = "planned"
	// StateRunning — at least one worker is active.
	StateRunning State = "running"
	// StateWaitingUser — blocked on confirmation or steering input.
	StateWaitingUser State = "waiting_user"
	// StateCompleted — terminal: goal achieved.
	StateCompleted State = "completed"
	// StateFailed — terminal: replan budget exhausted or fatal error.
	StateFailed State = "failed"
	// StateCancelled — terminal: user stopped or budget exhausted.
	StateCancelled State = "cancelled"
)

// IsTerminal reports whether the state allows no further transitions.
func (s State) IsTerminal() bool {
	switch s {
	case StateCompleted, StateFailed, StateCancelled:
		return true
	}
	return false
}

// Mission is one persistent goal record.
type Mission struct {
	ID                string     `json:"id"`
	Goal              string     `json:"goal"`
	State             State      `json:"state"`
	PlanJSON          string     `json:"plan_json,omitempty"`
	BudgetUSD         float64    `json:"budget_usd"`
	CostSoFarUSD      float64    `json:"cost_so_far_usd"`
	SupervisorAgentID string     `json:"supervisor_agent_id,omitempty"`
	CreatedAt         time.Time  `json:"created_at"`
	StartedAt         *time.Time `json:"started_at,omitempty"`
	CompletedAt       *time.Time `json:"completed_at,omitempty"`
}

// Step is one task within a mission's plan.
type Step struct {
	ID               string     `json:"id"`
	MissionID        string     `json:"mission_id"`
	Task             string     `json:"task"`
	DependsOn        []string   `json:"depends_on,omitempty"`
	AssignedWorkerID string     `json:"assigned_worker_id,omitempty"`
	State            State      `json:"state"`
	Output           string     `json:"output,omitempty"`
	Error            string     `json:"error,omitempty"`
	CreatedAt        time.Time  `json:"created_at"`
	StartedAt        *time.Time `json:"started_at,omitempty"`
	CompletedAt      *time.Time `json:"completed_at,omitempty"`
}

// Event is one entry in a mission's append-only event log.
type Event struct {
	ID          int64     `json:"id"`
	MissionID   string    `json:"mission_id"`
	Timestamp   time.Time `json:"timestamp"`
	Type        string    `json:"event_type"`
	PayloadJSON string    `json:"payload_json,omitempty"`
}

// Sentinel errors.
var (
	ErrNotFound           = errors.New("mission: not found")
	ErrInvalidTransition  = errors.New("mission: invalid state transition")
	ErrTerminal           = errors.New("mission: already in terminal state")
	ErrEmptyGoal          = errors.New("mission: goal is required")
)

// CanTransition reports whether the state machine permits moving from
// `from` to `to`. The matrix is intentionally restrictive — terminal
// states are sinks, and the only legal mid-flight pause/resume is
// running ⇄ waiting_user.
func CanTransition(from, to State) bool {
	if from == to {
		return true
	}
	if from.IsTerminal() {
		return false
	}
	switch from {
	case StateCreated:
		return to == StatePlanned || to == StateCancelled || to == StateFailed
	case StatePlanned:
		return to == StateRunning || to == StateCancelled || to == StateFailed
	case StateRunning:
		return to == StateWaitingUser || to == StateCompleted || to == StateFailed || to == StateCancelled
	case StateWaitingUser:
		return to == StateRunning || to == StateCompleted || to == StateFailed || to == StateCancelled
	}
	return false
}
