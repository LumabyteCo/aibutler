// Package supervisor is the second half of the mission orchestration
// pair. A Supervisor drives one mission from planned → running →
// completed by dispatching plan steps to workers via the reliable bus
// and aggregating their results back into the mission's state.
//
// Sequence:
//
//   1. Subscribe to mission.{id}.events                     (results inbound)
//   2. Read the plan's dispatch mode (sequential or parallel):
//      - SEQUENTIAL (default, set by Manager.SetPlan):
//          Walk plan steps one at a time. Dispatch, wait for the
//          matching worker result, persist, advance.
//      - PARALLEL (set by Manager.SetPlanParallel):
//          Walk Step.DependsOn as a DAG. Dispatch every step whose
//          dependencies are completed. Wait for ANY result, persist,
//          re-evaluate the DAG, dispatch newly-ready steps. Loop until
//          done or any step fails.
//   3. When all steps complete (or any fails), transition the mission
//      to its terminal state via the Manager.
//
// Failure policy: in both modes, the first step to fail terminates the
// whole mission. In parallel mode, already-in-flight peer steps are
// allowed to complete naturally (their results are still processed)
// but no new steps are dispatched after the failure is observed.
// Replanning on failure is a separate follow-up.
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

// ErrMissionPaused indicates Run exited because the mission was
// externally moved to waiting_user (e.g. via mission.interrupt
// action=pause). The caller decides whether to Resume + re-Run or
// Cancel. Distinguishing this from a genuine error helps callers
// avoid logging it as a failure.
var ErrMissionPaused = errors.New("supervisor: mission paused (waiting_user)")

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
	// Accept `planned` (fresh start) and `running` (resume after Pause/
	// Resume). Already-completed steps are skipped further down so re-Run
	// on a partially-completed mission picks up where it left off.
	if mi.State != mission.StatePlanned && mi.State != mission.StateRunning {
		return fmt.Errorf("supervisor: mission %s is in state %s, want planned or running",
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

	// Only fire Start on a fresh (planned) mission — a re-Run after
	// Resume is already in `running` state, and re-Starting would
	// overwrite the original StartedAt timestamp.
	if mi.State == mission.StatePlanned {
		if err := s.mgr.Start(ctx, missionID); err != nil {
			return fmt.Errorf("supervisor: start mission: %w", err)
		}
	}

	// Branch on dispatch mode. The plan's Parallel flag is set at
	// SetPlan-time (false) or SetPlanParallel-time (true) and stored
	// in mission.PlanJSON.
	plan := mission.PlanFromJSON(mi.PlanJSON)
	if plan.Parallel {
		return s.runParallel(ctx, missionID, steps, stepTimeout, dispatchTopic, events)
	}
	return s.runSequential(ctx, missionID, steps, stepTimeout, dispatchTopic, events)
}

// runSequential drives the mission one step at a time, in plan-order.
// This is the historical default behaviour, used when the plan was set
// via Manager.SetPlan.
func (s *Supervisor) runSequential(
	ctx context.Context, missionID string,
	steps []mission.Step, stepTimeout time.Duration,
	dispatchTopic string, events <-chan bus.ReliableMessage,
) error {
	for i := range steps {
		step := &steps[i]
		// Skip already-completed steps (resume-after-restart support).
		if step.State == mission.StateCompleted {
			continue
		}

		// External-interrupt check: re-fetch the mission's current state
		// before each step. If a mission.interrupt tool call has moved it
		// to waiting_user / cancelled / failed, exit the run loop
		// cleanly — don't dispatch the next step on top of an
		// already-stopped mission.
		if exitErr := s.checkExternalState(ctx, missionID); exitErr != nil {
			return exitErr
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

// runParallel drives the mission as a DAG. At each iteration:
//   1. Find every step whose state isn't terminal and whose DependsOn
//      are all completed.
//   2. Dispatch each ready step (concurrently).
//   3. Block on the next worker result event; update the matching
//      step's state.
//   4. If the result was a failure, stop dispatching new work and
//      fail the mission.
//   5. Loop until no in-flight work remains and either every step is
//      completed (success) or the failure was observed.
//
// Deadlocks (a step's DependsOn references a step that doesn't exist,
// or a cycle) are detected when scan() returns no ready steps AND
// nothing is in flight AND not everything is done. The supervisor
// fails fast with a clear error rather than hanging.
func (s *Supervisor) runParallel(
	ctx context.Context, missionID string,
	steps []mission.Step, stepTimeout time.Duration,
	dispatchTopic string, events <-chan bus.ReliableMessage,
) error {
	// Per-mission ctx that cancels every step's inner deadline when the
	// supervisor itself unwinds (e.g. on failure).
	missionCtx, missionCancel := context.WithCancel(ctx)
	defer missionCancel()

	// Step pointers for fast in-place updates while iterating.
	byID := make(map[string]*mission.Step, len(steps))
	for i := range steps {
		byID[steps[i].ID] = &steps[i]
	}

	completed := make(map[string]bool, len(steps))
	failed := make(map[string]bool, len(steps))
	inFlight := make(map[string]bool, len(steps))

	// Pre-seed completed/failed from any resume-after-restart state.
	for _, st := range steps {
		switch st.State {
		case mission.StateCompleted:
			completed[st.ID] = true
		case mission.StateFailed:
			failed[st.ID] = true
		}
	}

	stepStarts := make(map[string]time.Time, len(steps))
	var firstFailure string // empty until set

	for {
		// External-interrupt + deadline check at the top of every loop.
		if exitErr := s.checkExternalState(ctx, missionID); exitErr != nil {
			return exitErr
		}

		// Identify ready-to-dispatch steps.
		var ready []*mission.Step
		if firstFailure == "" {
			ready = readyForDispatch(steps, byID, completed, failed, inFlight)
		}

		// Dispatch every ready step. PublishReliable is fast (worker
		// acks on receipt now), so doing this in a tight loop is fine.
		for _, step := range ready {
			step.State = mission.StateRunning
			now := time.Now()
			step.StartedAt = &now
			stepStarts[step.ID] = now
			if err := s.store.UpdateStep(missionCtx, *step); err != nil {
				return fmt.Errorf("supervisor: parallel update step running: %w", err)
			}

			taskPayload, err := json.Marshal(worker.Task{
				StepID:    step.ID,
				MissionID: missionID,
				Task:      step.Task,
			})
			if err != nil {
				return fmt.Errorf("supervisor: parallel marshal task: %w", err)
			}
			if err := s.bus.PublishReliable(missionCtx, dispatchTopic, s.agentID, string(taskPayload), s.DispatchOpts); err != nil {
				return fmt.Errorf("supervisor: parallel dispatch %s: %w", step.ID, err)
			}
			inFlight[step.ID] = true
		}

		// Termination conditions.
		if len(inFlight) == 0 {
			// Nothing to wait for. The mission is either fully done,
			// failed with remaining in-flight drained, or deadlocked on
			// dependencies that can never resolve.
			if firstFailure != "" {
				_ = s.mgr.Fail(ctx, missionID,
					fmt.Sprintf("step %s failed; parallel mission terminated", firstFailure))
				return fmt.Errorf("supervisor: step %s: failed", firstFailure)
			}
			allDone := true
			for _, st := range steps {
				if !completed[st.ID] {
					allDone = false
					break
				}
			}
			if allDone {
				return s.mgr.Complete(ctx, missionID,
					fmt.Sprintf("%d steps completed (parallel)", len(steps)))
			}
			// Deadlock — no ready steps, no in-flight, not done.
			_ = s.mgr.Fail(ctx, missionID, "deadlock: unsatisfiable dependencies")
			return errors.New("supervisor: parallel deadlock — step DependsOn cannot be satisfied")
		}

		// Wait for the next result event. Per-mission deadline is the
		// outer ctx; per-step deadline is computed from stepStarts.
		waitDeadline := minDeadline(stepStarts, stepTimeout)
		waitCtx, waitCancel := context.WithDeadline(missionCtx, waitDeadline)
		msg, err := receiveOne(waitCtx, events)
		waitCancel()
		if err != nil {
			// Either the outer ctx cancelled, a per-step timeout
			// elapsed, or the wait deadline tripped spuriously without
			// any step actually being over budget. handleParallelWaitErr
			// returns nil only for the spurious case — fall through to
			// the next loop iteration in that case.
			if exitErr := s.handleParallelWaitErr(ctx, missionID, err, inFlight, stepStarts, stepTimeout); exitErr != nil {
				return exitErr
			}
			continue
		}

		var res worker.Result
		if jerr := json.Unmarshal([]byte(msg.Payload), &res); jerr != nil {
			msg.Ack()
			continue
		}
		msg.Ack()

		step, ok := byID[res.StepID]
		if !ok || !inFlight[res.StepID] {
			// Result for an unknown or already-resolved step — discard.
			continue
		}
		delete(inFlight, res.StepID)

		done := time.Now()
		step.CompletedAt = &done
		if res.Success {
			step.State = mission.StateCompleted
			step.Output = res.Output
			completed[step.ID] = true
		} else {
			step.State = mission.StateFailed
			step.Error = res.Error
			failed[step.ID] = true
			if firstFailure == "" {
				firstFailure = step.ID
			}
		}
		if err := s.store.UpdateStep(ctx, *step); err != nil {
			return fmt.Errorf("supervisor: parallel update step done: %w", err)
		}
		_ = s.mgr.AppendEvent(ctx, missionID, "supervisor.step_done", res)
	}
}

// readyForDispatch returns the steps that are ready to dispatch — not
// already terminal, not already in flight, and whose DependsOn entries
// are all in `completed`.
func readyForDispatch(
	steps []mission.Step, byID map[string]*mission.Step,
	completed, failed, inFlight map[string]bool,
) []*mission.Step {
	ready := []*mission.Step{}
	for i := range steps {
		st := &steps[i]
		if completed[st.ID] || failed[st.ID] || inFlight[st.ID] {
			continue
		}
		allOK := true
		for _, dep := range st.DependsOn {
			if _, exists := byID[dep]; !exists {
				// Dangling reference — treat as unsatisfiable; the
				// deadlock path will surface this clearly.
				allOK = false
				break
			}
			if !completed[dep] {
				allOK = false
				break
			}
		}
		if allOK {
			ready = append(ready, st)
		}
	}
	return ready
}

// checkExternalState re-reads the mission's current state and returns
// a non-nil error if the supervisor should exit (external cancel,
// external fail, or pause).
func (s *Supervisor) checkExternalState(ctx context.Context, missionID string) error {
	current, err := s.store.GetMission(ctx, missionID)
	if err != nil {
		return fmt.Errorf("supervisor: re-check state: %w", err)
	}
	switch current.State {
	case mission.StateCancelled:
		return fmt.Errorf("supervisor: mission cancelled externally")
	case mission.StateFailed:
		return fmt.Errorf("supervisor: mission failed externally")
	case mission.StateWaitingUser:
		// Pause requested — exit gracefully so the caller can decide
		// whether to Resume + re-Run or Cancel. The supervisor is
		// stateless across runs; resuming creates a new Run call.
		return ErrMissionPaused
	}
	return nil
}

// minDeadline returns the earliest per-step deadline among in-flight
// starts, or now+stepTimeout if stepStarts is empty.
func minDeadline(stepStarts map[string]time.Time, stepTimeout time.Duration) time.Time {
	if len(stepStarts) == 0 {
		return time.Now().Add(stepTimeout)
	}
	var earliest time.Time
	for _, started := range stepStarts {
		deadline := started.Add(stepTimeout)
		if earliest.IsZero() || deadline.Before(earliest) {
			earliest = deadline
		}
	}
	return earliest
}

// receiveOne pulls one message from the events channel, respecting ctx.
func receiveOne(ctx context.Context, events <-chan bus.ReliableMessage) (bus.ReliableMessage, error) {
	select {
	case <-ctx.Done():
		return bus.ReliableMessage{}, ctx.Err()
	case msg, ok := <-events:
		if !ok {
			return bus.ReliableMessage{}, errors.New("event channel closed")
		}
		return msg, nil
	}
}

// handleParallelWaitErr decides what to do when the wait-for-event
// select unblocked with an error — either the outer ctx fired or one
// of the in-flight per-step timeouts elapsed.
func (s *Supervisor) handleParallelWaitErr(
	ctx context.Context, missionID string, err error,
	inFlight map[string]bool, stepStarts map[string]time.Time,
	stepTimeout time.Duration,
) error {
	// If the outer (parent) ctx is done, propagate that.
	if ctx.Err() != nil {
		return ctx.Err()
	}
	// Otherwise it's a per-step timeout. Find which step(s) overshot.
	now := time.Now()
	var timedOut []string
	for stepID := range inFlight {
		if started, ok := stepStarts[stepID]; ok && now.Sub(started) >= stepTimeout {
			timedOut = append(timedOut, stepID)
		}
	}
	if len(timedOut) == 0 {
		// Wait deadline tripped but no individual step is over budget yet.
		// Re-enter the main loop and try again.
		return nil
	}
	_ = s.mgr.Fail(ctx, missionID,
		fmt.Sprintf("step %s exceeded StepTimeout %s", timedOut[0], stepTimeout))
	return fmt.Errorf("supervisor: parallel step %s timed out: %w", timedOut[0], err)
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
