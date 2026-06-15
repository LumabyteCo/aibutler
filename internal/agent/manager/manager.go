// Package manager is the middle tier of the 3-level mission hierarchy.
//
// A Manager sits between the Supervisor (mission-wide owner) and the
// Workers (leaf-level executors). When a mission plan has a step
// carrying SubSteps, the Supervisor delegates that step to a Manager
// rather than a Worker. The Manager:
//
//   1. Receives the parent step (with its sub-step list) on the
//      mission's manager_dispatch topic.
//   2. Decomposes the sub-step list and dispatches each one to the
//      same worker dispatch topic the Supervisor would have used.
//      Sub-steps therefore compete with any other worker traffic for
//      the same pool — the Manager does not own a separate pool.
//   3. Awaits each sub-step's worker.Result on the mission events
//      topic and continues to the next sub-step. Failure of any
//      sub-step terminates the group with a parent-level error.
//   4. Aggregates the sub-step outputs into a single parent-level
//      worker.Result and publishes it on the events topic so the
//      Supervisor sees a single "group complete" result for the
//      parent step.
//
// Backwards compatibility: missions without any SubSteps never trigger
// the Manager — the Supervisor dispatches every step directly to a
// Worker, exactly as in v0.3.x. The Manager only runs when grouped
// steps exist in the plan.
//
// Scope notes for v0.4.0:
//
//   - Sub-step execution within a group is SEQUENTIAL. Parallel sub-
//     step dispatch is a follow-up — the same DAG infrastructure the
//     supervisor uses for runParallel could be lifted in.
//   - Sub-steps are LEAF-LEVEL. A sub-step cannot itself have
//     sub-steps in v0.4.0; deeper nesting requires recursive Manager
//     dispatch and is intentionally deferred.
//   - One Manager goroutine per mission. The Manager subscribes via
//     competing-consumer, so multiple Managers could share the load
//     pool-wide if the runtime spawned more than one — but the
//     default missionruntime spawns exactly one Manager per mission.
package manager

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/LumabyteCo/aibutler/internal/agent/bus"
	"github.com/LumabyteCo/aibutler/internal/agent/worker"
	"github.com/LumabyteCo/aibutler/internal/mission"
)

// Manager drains group-level dispatches from the manager_dispatch
// topic, decomposes them into sub-step dispatches on the worker
// dispatch topic, and aggregates results back as a single
// parent-level Result.
type Manager struct {
	bus     *bus.Bus
	agentID string

	// DispatchOpts controls reliable-publish parameters for the
	// per-sub-step dispatches the manager issues. Zero values resolve
	// to bus defaults.
	DispatchOpts bus.ReliableOpts
	// SubStepTimeout caps how long the manager waits for ONE sub-step
	// result before declaring the group failed. Default 30s. The
	// parent step gets a single composite error in that case.
	SubStepTimeout time.Duration
}

// New creates a Manager. agentID identifies the manager in audit-log
// entries (typically "manager-<short-mission-id>"). The Manager shares
// the bus with the Supervisor and Workers — there's no separate
// transport per tier.
func New(b *bus.Bus, agentID string) *Manager {
	return &Manager{bus: b, agentID: agentID}
}

// Run subscribes to the mission's manager_dispatch topic and
// processes group tasks until ctx is cancelled. Each group task is
// acked on receipt; sub-step failures surface as a parent-level
// error in the published Result rather than as Nacks.
func (m *Manager) Run(ctx context.Context, missionID string) error {
	if missionID == "" {
		return errors.New("manager: missionID is required")
	}
	managerTopic := "mission." + missionID + ".manager_dispatch"
	workerTopic := "mission." + missionID + ".dispatch"
	eventsTopic := "mission." + missionID + ".events"

	in := m.bus.SubscribeCompeting(managerTopic)
	defer m.bus.UnsubscribeCompeting(managerTopic, in)

	// Subscribe to the broadcast events topic to receive sub-step
	// results from workers. The supervisor also subscribes here — both
	// see every event and filter by their own step IDs.
	events := m.bus.SubscribeReliable(eventsTopic)
	defer m.bus.UnsubscribeReliable(eventsTopic, events)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case msg, ok := <-in:
			if !ok {
				return nil
			}
			m.handle(ctx, msg, workerTopic, eventsTopic, events)
		}
	}
}

// handle processes one group dispatch. The dispatch payload is a
// worker.Task whose Input is the JSON-encoded list of sub-steps.
func (m *Manager) handle(
	ctx context.Context,
	msg bus.ReliableMessage,
	workerTopic, eventsTopic string,
	events <-chan bus.ReliableMessage,
) {
	msg.Ack()

	var groupTask worker.Task
	if err := json.Unmarshal([]byte(msg.Payload), &groupTask); err != nil {
		go m.publishResult(ctx, eventsTopic, worker.Result{
			MissionID: msg.Topic,
			WorkerID:  m.agentID,
			Error:     "manager: malformed group task: " + err.Error(),
			Success:   false,
		})
		return
	}

	// Sub-step list rides in Input as a JSON array of mission.Step.
	var subSteps []mission.Step
	if err := json.Unmarshal([]byte(groupTask.Input), &subSteps); err != nil {
		go m.publishResult(ctx, eventsTopic, worker.Result{
			StepID:    groupTask.StepID,
			MissionID: groupTask.MissionID,
			WorkerID:  m.agentID,
			Error:     "manager: malformed sub-step list: " + err.Error(),
			Success:   false,
		})
		return
	}
	if len(subSteps) == 0 {
		go m.publishResult(ctx, eventsTopic, worker.Result{
			StepID:    groupTask.StepID,
			MissionID: groupTask.MissionID,
			WorkerID:  m.agentID,
			Error:     "manager: empty sub-step list",
			Success:   false,
		})
		return
	}

	// Execute sub-steps sequentially. The aggregate output is one line
	// per sub-step (id → output), readable both as a structured log
	// and as natural-language summary for downstream agents.
	timeout := m.SubStepTimeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}

	var aggregated strings.Builder
	for i := range subSteps {
		sub := &subSteps[i]
		subCtx, cancel := context.WithTimeout(ctx, timeout)
		out, err := m.runSubStep(subCtx, workerTopic, eventsTopic, events, sub, groupTask.MissionID)
		cancel()
		if err != nil {
			go m.publishResult(ctx, eventsTopic, worker.Result{
				StepID:    groupTask.StepID,
				MissionID: groupTask.MissionID,
				WorkerID:  m.agentID,
				Error:     fmt.Sprintf("manager: sub-step %s failed: %v", sub.ID, err),
				Success:   false,
			})
			return
		}
		// Aggregate as "sub_id: out" so the supervisor sees a structured
		// audit trail in the parent Result.Output.
		if aggregated.Len() > 0 {
			aggregated.WriteString("\n")
		}
		fmt.Fprintf(&aggregated, "%s: %s", sub.ID, out)
	}

	go m.publishResult(ctx, eventsTopic, worker.Result{
		StepID:    groupTask.StepID,
		MissionID: groupTask.MissionID,
		WorkerID:  m.agentID,
		Output:    aggregated.String(),
		Success:   true,
	})
}

// runSubStep dispatches one sub-step to the worker pool and awaits
// its matching Result event. Returns the worker's output or an error.
func (m *Manager) runSubStep(
	ctx context.Context,
	workerTopic, eventsTopic string,
	events <-chan bus.ReliableMessage,
	sub *mission.Step,
	missionID string,
) (string, error) {
	taskPayload, err := json.Marshal(worker.Task{
		StepID:    sub.ID,
		MissionID: missionID,
		Task:      sub.Task,
	})
	if err != nil {
		return "", fmt.Errorf("marshal sub-step task: %w", err)
	}
	if err := m.bus.PublishCompeting(ctx, workerTopic, m.agentID, string(taskPayload), m.DispatchOpts); err != nil {
		return "", fmt.Errorf("dispatch sub-step: %w", err)
	}

	for {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case msg, ok := <-events:
			if !ok {
				return "", errors.New("event channel closed")
			}
			var res worker.Result
			if err := json.Unmarshal([]byte(msg.Payload), &res); err != nil {
				msg.Ack()
				continue
			}
			if res.StepID != sub.ID {
				// Event for a different step (sibling sub-step from a
				// concurrent group, the parent step's eventual Result,
				// or any other unrelated event). Ack and keep waiting.
				msg.Ack()
				continue
			}
			msg.Ack()
			if !res.Success {
				return "", fmt.Errorf("worker reported failure: %s", res.Error)
			}
			return res.Output, nil
		}
	}
}

// publishResult emits the parent-step Result on the events topic so
// the supervisor can mark the group step completed (or failed).
func (m *Manager) publishResult(ctx context.Context, topic string, res worker.Result) {
	payload, _ := json.Marshal(res)
	opts := bus.ReliableOpts{Timeout: 5 * time.Second}
	_ = m.bus.PublishReliable(ctx, topic, m.agentID, string(payload), opts)
}
