// Package supervisor is the second half of the mission orchestration
// pair. A Supervisor drives one mission from planned → running →
// completed by dispatching plan steps to workers via the reliable bus
// and aggregating their results back into the mission's state.
//
// Sequence:
//
//   1. Subscribe to mission.{id}.events                     (results inbound)
//   2. Walk plan steps. For each step:
//      a. Publish dispatch payload to mission.{id}.dispatch (reliable)
//      b. Wait for matching worker.* event on .events       (reliable)
//      c. Update step state in the mission store
//   3. When all steps complete (or any fails), transition the mission
//      to its terminal state via the Manager.
//
// Limits in this initial implementation:
//
//   - Steps dispatch SEQUENTIALLY (one at a time). Parallel dispatch
//     is a follow-up — the bus and store both already support it,
//     but sequential makes the audit trail simpler to reason about.
//   - No replanning yet. A failed step → mission.failed terminal.
//     Replanning is a separate follow-up.
package supervisor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/LumabyteCo/aibutler/internal/agent/bus"
	"github.com/LumabyteCo/aibutler/internal/agent/worker"
	"github.com/LumabyteCo/aibutler/internal/mission"
)

// Supervisor owns one mission and coordinates worker dispatch over the bus.
type Supervisor struct {
	mgr     *mission.Manager
	store   mission.Store
	bus     *bus.Bus
	agentID string

	// DispatchOpts controls reliable-publish parameters for each task
	// dispatched to a worker. Zero values resolve to bus defaults.
	DispatchOpts bus.ReliableOpts
	// StepTimeout caps how long the supervisor waits for one step's
	// result event before declaring the step failed. Default 30s.
	StepTimeout time.Duration
}

// New creates a Supervisor.
func New(mgr *mission.Manager, store mission.Store, b *bus.Bus, agentID string) *Supervisor {
	return &Supervisor{
		mgr:     mgr,
		store:   store,
		bus:     b,
		agentID: agentID,
	}
}

// Run drives the mission to a terminal state. The mission must already
// be in `planned` state (created + SetPlan). Returns nil on a clean
// completion, an error on terminal failure or context cancellation.
func (s *Supervisor) Run(ctx context.Context, missionID string) error {
	if missionID == "" {
		return errors.New("supervisor: missionID is required")
	}

	stepTimeout := s.StepTimeout
	if stepTimeout <= 0 {
		stepTimeout = 30 * time.Second
	}

	mi, err := s.store.GetMission(ctx, missionID)
	if err != nil {
		return fmt.Errorf("supervisor: load mission: %w", err)
	}
	if mi.State != mission.StatePlanned {
		return fmt.Errorf("supervisor: mission %s is in state %s, want planned",
			missionID, mi.State)
	}

	steps, err := s.store.GetSteps(ctx, missionID)
	if err != nil {
		return fmt.Errorf("supervisor: load steps: %w", err)
	}
	if len(steps) == 0 {
		// Nothing to do — start then complete to keep the state machine
		// strict (planned → completed isn't a legal direct transition).
		if err := s.mgr.Start(ctx, missionID); err != nil {
			return fmt.Errorf("supervisor: start (empty plan): %w", err)
		}
		return s.mgr.Complete(ctx, missionID, "no steps in plan")
	}

	// Subscribe to events BEFORE starting; otherwise a fast worker
	// could publish before the supervisor is listening.
	eventsTopic := "mission." + missionID + ".events"
	dispatchTopic := "mission." + missionID + ".dispatch"
	events := s.bus.SubscribeReliable(eventsTopic)
	defer s.bus.UnsubscribeReliable(eventsTopic, events)

	if err := s.mgr.Start(ctx, missionID); err != nil {
		return fmt.Errorf("supervisor: start mission: %w", err)
	}

	for i := range steps {
		step := &steps[i]
		// Skip already-completed steps (resume-after-restart support).
		if step.State == mission.StateCompleted {
			continue
		}

		stepCtx, cancel := context.WithTimeout(ctx, stepTimeout)
		err := s.runStep(stepCtx, missionID, step, dispatchTopic, events)
		cancel()
		if err != nil {
			// Step failed — fail the whole mission. Replanning is a
			// follow-up.
			_ = s.mgr.Fail(ctx, missionID, fmt.Sprintf("step %s: %v", step.ID, err))
			return fmt.Errorf("supervisor: step %s: %w", step.ID, err)
		}
	}

	return s.mgr.Complete(ctx, missionID, fmt.Sprintf("%d steps completed", len(steps)))
}

// runStep dispatches one step and waits for its matching result event.
func (s *Supervisor) runStep(
	ctx context.Context, missionID string, step *mission.Step,
	dispatchTopic string, events <-chan bus.ReliableMessage,
) error {
	now := time.Now()
	step.State = mission.StateRunning
	step.StartedAt = &now
	if err := s.store.UpdateStep(ctx, *step); err != nil {
		return fmt.Errorf("update step running: %w", err)
	}

	// Dispatch.
	taskPayload, err := json.Marshal(worker.Task{
		StepID:    step.ID,
		MissionID: missionID,
		Task:      step.Task,
	})
	if err != nil {
		return fmt.Errorf("marshal task: %w", err)
	}
	if err := s.bus.PublishReliable(ctx, dispatchTopic, s.agentID, string(taskPayload), s.DispatchOpts); err != nil {
		return fmt.Errorf("dispatch: %w", err)
	}

	// Wait for a matching worker.* event.
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case msg, ok := <-events:
			if !ok {
				return errors.New("event channel closed")
			}
			var res worker.Result
			if err := json.Unmarshal([]byte(msg.Payload), &res); err != nil {
				// Drop malformed events but ack them so the worker
				// doesn't keep retrying forever.
				msg.Ack()
				continue
			}
			if res.StepID != step.ID {
				// Result for a different step — ack and keep waiting.
				// (In sequential dispatch this shouldn't happen, but
				// it's cheap to handle correctly.)
				msg.Ack()
				continue
			}
			msg.Ack()

			// Found our result. Persist and return.
			completed := time.Now()
			step.CompletedAt = &completed
			if res.Success {
				step.State = mission.StateCompleted
				step.Output = res.Output
			} else {
				step.State = mission.StateFailed
				step.Error = res.Error
			}
			if err := s.store.UpdateStep(ctx, *step); err != nil {
				return fmt.Errorf("update step done: %w", err)
			}
			_ = s.mgr.AppendEvent(ctx, missionID, "supervisor.step_done", res)

			if !res.Success {
				return fmt.Errorf("worker reported failure: %s", res.Error)
			}
			return nil
		}
	}
}
