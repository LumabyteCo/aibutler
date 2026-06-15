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
// Failure policy:
//
//   - SEQUENTIAL mode: if Supervisor.Replanner is set, a failing step
//     consults it for a replacement sequence. The Replanner returns
//     new steps which are persisted via Manager.Replan and the loop
//     continues; up to Supervisor.MaxReplans attempts per mission. If
//     no Replanner is set, or it returns ErrReplanRejected, or the
//     attempt cap is exhausted, the supervisor fails the mission.
//   - PARALLEL mode: the first step to fail terminates the mission.
//     Replanning under parallel dispatch is a separate follow-up — peer
//     in-flight steps still get to publish their results, but no
//     replan attempt is made and no new work is dispatched after the
//     failure is observed.
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
	// Replanner, if non-nil, is consulted in sequential mode when a
	// step fails. It is asked for a replacement step sequence; on
	// success the supervisor persists the new steps via Manager.Replan
	// and continues. nil (the default) preserves the historical
	// "fail-on-first-failure" behaviour.
	Replanner Replanner
	// MaxReplans caps how many times one mission may be replanned
	// before the supervisor gives up and fails it. Default 3. Has no
	// effect when Replanner is nil.
	MaxReplans int
}

// defaultMaxReplans is the per-mission replan attempt cap when
// Supervisor.MaxReplans is unset or non-positive.
const defaultMaxReplans = 3

func (s *Supervisor) maxReplans() int {
	if s.MaxReplans <= 0 {
		return defaultMaxReplans
	}
	return s.MaxReplans
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
	managerTopic := "mission." + missionID + ".manager_dispatch"
	events := s.bus.SubscribeReliable(eventsTopic)
	defer s.bus.UnsubscribeReliable(eventsTopic, events)

	// Hydrate Step.SubSteps from PlanJSON onto the steps loaded from
	// the store. The mission_steps table doesn't carry a sub_steps
	// column (avoiding a migration in v0.4.0) — PlanJSON is the
	// authoritative source for plan structure, mission_steps is the
	// authoritative source for runtime state. We merge them here once
	// at Run entry so downstream code can branch on step.SubSteps
	// uniformly.
	hydrateSubSteps(steps, mission.PlanFromJSON(mi.PlanJSON))

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
		return s.runParallel(ctx, missionID, steps, stepTimeout, dispatchTopic, managerTopic, events)
	}
	return s.runSequential(ctx, missionID, steps, stepTimeout, dispatchTopic, managerTopic, events)
}

// hydrateSubSteps copies the SubSteps slice from each plan-step into
// the matching runtime step (matched by ID). Plan-steps that don't
// match any runtime step are ignored; runtime steps that don't match
// any plan-step keep their existing (empty) SubSteps.
func hydrateSubSteps(runtimeSteps []mission.Step, plan mission.Plan) {
	if len(plan.Steps) == 0 {
		return
	}
	bySubID := make(map[string][]mission.Step, len(plan.Steps))
	for _, ps := range plan.Steps {
		if len(ps.SubSteps) > 0 {
			bySubID[ps.ID] = ps.SubSteps
		}
	}
	if len(bySubID) == 0 {
		return
	}
	for i := range runtimeSteps {
		if subs, ok := bySubID[runtimeSteps[i].ID]; ok {
			runtimeSteps[i].SubSteps = subs
		}
	}
}

// runSequential drives the mission one step at a time, in plan-order.
// This is the historical default behaviour, used when the plan was set
// via Manager.SetPlan.
//
// When Supervisor.Replanner is set, a failing step triggers a replan
// attempt: the Replanner is asked for a replacement sequence, those
// new steps are persisted via Manager.Replan, and the loop continues
// from the next non-completed step (which is now the first of the
// replacement set). Up to maxReplans() attempts per mission; after
// that, or if the Replanner rejects, the mission fails.
//
// The step list is re-fetched from the store after every successful
// replan so the new steps are picked up. The initial `steps` argument
// is the planned snapshot the caller passed in; on entry it's used
// directly to avoid an extra round-trip on the common case (no
// replan needed).
func (s *Supervisor) runSequential(
	ctx context.Context, missionID string,
	steps []mission.Step, stepTimeout time.Duration,
	dispatchTopic, managerTopic string, events <-chan bus.ReliableMessage,
) error {
	replanCount := 0
	current := steps

	for {
		// Find the first step in the freshest snapshot that is neither
		// completed nor in a terminal failure state. A failed step from
		// an earlier replan iteration must be skipped — replanning
		// leaves it on the audit log but the replacement is the path
		// forward.
		idx := -1
		for i := range current {
			switch current[i].State {
			case mission.StateCompleted, mission.StateFailed, mission.StateCancelled:
				continue
			}
			idx = i
			break
		}
		if idx < 0 {
			// Every step has reached a step-level terminal state. If
			// any are failed, that's a bug in the loop (the failure
			// path above should have failed the mission already); if
			// all are completed, the mission is done.
			var completedCount int
			for _, st := range current {
				if st.State == mission.StateCompleted {
					completedCount++
				}
			}
			return s.mgr.Complete(ctx, missionID, fmt.Sprintf("%d steps completed", completedCount))
		}
		step := &current[idx]

		// External-interrupt check: re-fetch the mission's current state
		// before each step. If a mission.interrupt tool call has moved it
		// to waiting_user / cancelled / failed, exit the run loop
		// cleanly — don't dispatch the next step on top of an
		// already-stopped mission.
		if exitErr := s.checkExternalState(ctx, missionID); exitErr != nil {
			return exitErr
		}

		stepCtx, cancel := context.WithTimeout(ctx, stepTimeout)
		err := s.runStep(stepCtx, missionID, step, dispatchTopic, managerTopic, events)
		cancel()
		if err == nil {
			// Move on. The in-memory step state was already mutated by
			// runStep, so the next "find non-completed" iteration will
			// skip it.
			continue
		}

		// Worker signalled "this step needs user confirmation." This is
		// neither a success nor a failure — it's a pause request. Auto-
		// transition the mission to waiting_user, emit the
		// mission.confirmation_required event, and exit Run with
		// ErrMissionPaused so the runtime treats it like any other
		// externally-paused mission. The step is already in
		// state=waiting_user (set by runStep) so the resume path picks
		// it up on the next Run via the runSequential cursor.
		var confirmErr *stepNeedsConfirmationError
		if errors.As(err, &confirmErr) {
			payload := map[string]string{
				"step_id": confirmErr.StepID,
				"reason":  confirmErr.Reason,
			}
			_ = s.mgr.AppendEvent(ctx, missionID, "mission.confirmation_required", payload)
			if pauseErr := s.mgr.Pause(ctx, missionID, confirmErr.Reason); pauseErr != nil {
				// Persistence error transitioning the mission state.
				// Surface it as a mission failure since the supervisor
				// can no longer trust the mission's recorded state.
				_ = s.mgr.Fail(ctx, missionID, fmt.Sprintf("auto-pause failed: %v", pauseErr))
				return fmt.Errorf("supervisor: auto-pause: %w", pauseErr)
			}
			return ErrMissionPaused
		}

		// Step failed. Try to replan if configured.
		if s.Replanner != nil && replanCount < s.maxReplans() {
			refreshed, replanErr := s.tryReplan(ctx, missionID, step, err, current, replanCount)
			if replanErr == nil {
				replanCount++
				current = refreshed
				continue
			}
			if !errors.Is(replanErr, ErrReplanRejected) {
				// A non-rejection error from the Replanner (LLM call
				// failed, persistence failed, etc.) — surface it in
				// the mission.failed reason but still take the fail
				// path. We don't burn a replan attempt for impl errors.
				err = fmt.Errorf("%w (replan also failed: %v)", err, replanErr)
			}
			// On ErrReplanRejected, fall through to the fail path with
			// the original step error as the cause.
		}

		_ = s.mgr.Fail(ctx, missionID, fmt.Sprintf("step %s: %v", step.ID, err))
		return fmt.Errorf("supervisor: step %s: %w", step.ID, err)
	}
}

// tryReplan consults the configured Replanner for a replacement
// sequence, persists those steps via Manager.Replan, and returns the
// fresh step list. Returns (nil, ErrReplanRejected) when the Replanner
// declines; (nil, otherErr) when the Replanner or persistence fails.
// On success the returned slice is the fresh GetSteps result including
// the failed step (now state=failed) and the new replacement steps.
func (s *Supervisor) tryReplan(
	ctx context.Context, missionID string,
	failedStep *mission.Step, stepErr error,
	allSteps []mission.Step, priorReplans int,
) ([]mission.Step, error) {
	mi, err := s.store.GetMission(ctx, missionID)
	if err != nil {
		return nil, fmt.Errorf("load mission for replan: %w", err)
	}

	var completed, remaining []mission.Step
	for _, st := range allSteps {
		switch {
		case st.State == mission.StateCompleted:
			completed = append(completed, st)
		case st.ID == failedStep.ID:
			// The failed step itself — not "remaining."
		default:
			if st.State != mission.StateFailed {
				remaining = append(remaining, st)
			}
		}
	}

	req := ReplanRequest{
		MissionID:      missionID,
		Goal:           mi.Goal,
		CompletedSteps: completed,
		FailedStep:     *failedStep,
		FailureReason:  stepErr.Error(),
		RemainingSteps: remaining,
		PriorReplans:   priorReplans,
	}

	newSteps, err := s.Replanner.Replan(ctx, req)
	if err != nil {
		return nil, err
	}
	if len(newSteps) == 0 {
		return nil, errors.New("replanner returned empty step list")
	}

	reason := fmt.Sprintf("replan attempt %d after step %s: %v",
		priorReplans+1, failedStep.ID, stepErr)
	if err := s.mgr.Replan(ctx, missionID, failedStep.ID, newSteps, reason); err != nil {
		return nil, fmt.Errorf("persist replan: %w", err)
	}

	refreshed, err := s.store.GetSteps(ctx, missionID)
	if err != nil {
		return nil, fmt.Errorf("reload steps after replan: %w", err)
	}
	return refreshed, nil
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
	dispatchTopic, managerTopic string, events <-chan bus.ReliableMessage,
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

		// Dispatch every ready step via PublishCompeting — each lands
		// on exactly one worker (or one manager, for grouped steps).
		// Per-publish shuffle in the bus gives fair distribution across
		// the pool; busy peers fall through via SendTimeout.
		for _, step := range ready {
			step.State = mission.StateRunning
			now := time.Now()
			step.StartedAt = &now
			stepStarts[step.ID] = now
			if err := s.store.UpdateStep(missionCtx, *step); err != nil {
				return fmt.Errorf("supervisor: parallel update step running: %w", err)
			}

			// Route grouped steps to the manager topic with the sub-step
			// list in Task.Input; leaf steps go to the worker topic.
			targetTopic := dispatchTopic
			taskInput := ""
			if len(step.SubSteps) > 0 {
				targetTopic = managerTopic
				subJSON, err := json.Marshal(step.SubSteps)
				if err != nil {
					return fmt.Errorf("supervisor: parallel marshal sub-steps %s: %w", step.ID, err)
				}
				taskInput = string(subJSON)
			}

			taskPayload, err := json.Marshal(worker.Task{
				StepID:    step.ID,
				MissionID: missionID,
				Task:      step.Task,
				Input:     taskInput,
			})
			if err != nil {
				return fmt.Errorf("supervisor: parallel marshal task: %w", err)
			}
			if err := s.bus.PublishCompeting(missionCtx, targetTopic, s.agentID, string(taskPayload), s.DispatchOpts); err != nil {
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
	dispatchTopic, managerTopic string, events <-chan bus.ReliableMessage,
) error {
	now := time.Now()
	step.State = mission.StateRunning
	step.StartedAt = &now
	if err := s.store.UpdateStep(ctx, *step); err != nil {
		return fmt.Errorf("update step running: %w", err)
	}

	// Route grouped steps (those carrying SubSteps) to the manager
	// dispatch topic; leaf steps go straight to a worker. The manager
	// decomposes the sub-step list, runs each on the worker pool, and
	// publishes a single aggregated Result for this parent step — the
	// supervisor's wait loop below treats both paths identically (it's
	// matching on step.ID either way).
	targetTopic := dispatchTopic
	taskInput := ""
	if len(step.SubSteps) > 0 {
		targetTopic = managerTopic
		subJSON, err := json.Marshal(step.SubSteps)
		if err != nil {
			return fmt.Errorf("marshal sub-steps: %w", err)
		}
		taskInput = string(subJSON)
	}

	// Dispatch via competing-consumer — lands on exactly one worker (or
	// one manager, for grouped steps).
	taskPayload, err := json.Marshal(worker.Task{
		StepID:    step.ID,
		MissionID: missionID,
		Task:      step.Task,
		Input:     taskInput,
	})
	if err != nil {
		return fmt.Errorf("marshal task: %w", err)
	}
	if err := s.bus.PublishCompeting(ctx, targetTopic, s.agentID, string(taskPayload), s.DispatchOpts); err != nil {
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

			// Found our result. Branch on the three outcomes:
			//   1. NeedsConfirmation — auto-pause path. Step state goes
			//      to waiting_user so the resume cursor picks it up
			//      again. Mission gets transitioned to waiting_user via
			//      Manager.Pause in the outer runSequential loop.
			//   2. Success — normal completion.
			//   3. Failure — replan or fail-fast (handled by outer loop).
			now := time.Now()
			if res.NeedsConfirmation {
				step.State = mission.StateWaitingUser
				step.Error = res.ConfirmationReason
				// Leave CompletedAt unset — the step hasn't finished, it's
				// paused. Resume re-dispatches and runStep stamps a fresh
				// StartedAt on the next attempt.
				if err := s.store.UpdateStep(ctx, *step); err != nil {
					return fmt.Errorf("update step waiting_user: %w", err)
				}
				_ = s.mgr.AppendEvent(ctx, missionID, "supervisor.step_paused", res)
				return &stepNeedsConfirmationError{
					StepID: step.ID,
					Reason: res.ConfirmationReason,
				}
			}

			step.CompletedAt = &now
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

// stepNeedsConfirmationError is the sentinel runStep returns when the
// worker signalled NeedsConfirmation. runSequential catches it via
// errors.As, transitions the mission to waiting_user, and exits with
// ErrMissionPaused — the existing graceful-pause return.
type stepNeedsConfirmationError struct {
	StepID string
	Reason string
}

func (e *stepNeedsConfirmationError) Error() string {
	return fmt.Sprintf("step %s requires user confirmation: %s", e.StepID, e.Reason)
}
