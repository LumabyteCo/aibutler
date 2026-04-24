package agent

import "fmt"

// State represents the lifecycle state of an agent.
type State string

const (
	StateSpawned   State = "spawned"
	StateRunning   State = "running"
	StateWaiting   State = "waiting"
	StateCompleted State = "completed"
	StateFailed    State = "failed"
	StateCancelled State = "cancelled"
)

// IsTerminal returns true if the state is a terminal state.
func (s State) IsTerminal() bool {
	return s == StateCompleted || s == StateFailed || s == StateCancelled
}

// validTransitions defines allowed state transitions.
var validTransitions = map[State][]State{
	StateSpawned:   {StateRunning, StateFailed, StateCancelled},
	StateRunning:   {StateWaiting, StateCompleted, StateFailed, StateCancelled},
	StateWaiting:   {StateRunning, StateCompleted, StateFailed, StateCancelled},
	StateCompleted: {},
	StateFailed:    {},
	StateCancelled: {},
}

// CanTransition checks if transitioning from src to dst is valid.
func CanTransition(src, dst State) bool {
	for _, valid := range validTransitions[src] {
		if valid == dst {
			return true
		}
	}
	return false
}

// ErrInvalidTransition is returned for illegal state transitions.
type ErrInvalidTransition struct {
	From, To State
}

func (e ErrInvalidTransition) Error() string {
	return fmt.Sprintf("agent: invalid transition %s → %s", e.From, e.To)
}
