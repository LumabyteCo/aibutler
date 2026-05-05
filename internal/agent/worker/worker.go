// Package worker is one half of the mission orchestration pair.
//
// A Worker subscribes to the supervisor's dispatch topic, executes one
// task at a time via a pluggable TaskExecutor, and publishes results
// back to the supervisor's event topic — both legs use the bus's
// at-least-once delivery so neither dispatch nor result can be silently
// dropped on a slow consumer.
//
// The Worker itself is execution-agnostic: it doesn't know how to "do"
// anything. It just shuttles tasks through whatever TaskExecutor the
// caller wires in. This split lets the orchestration mechanics ship
// today (with a stub or echo executor for tests) and bolt on real
// LLM-driven execution as a separate follow-up.
//
// Topics (both reliable):
//
//   - mission.{id}.dispatch  — supervisor → worker, one Task per message
//   - mission.{id}.events    — worker → supervisor, worker.* notifications
package worker

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/LumabyteCo/aibutler/internal/agent/bus"
)

// Task is the dispatch payload — what the supervisor wants the worker
// to do. PayloadJSON is opaque to the bus and parsed by TaskExecutor.
type Task struct {
	StepID    string `json:"step_id"`
	MissionID string `json:"mission_id"`
	Task      string `json:"task"`
	Input     string `json:"input,omitempty"`
}

// Result is the event payload emitted on worker completion or failure.
type Result struct {
	StepID    string `json:"step_id"`
	MissionID string `json:"mission_id"`
	WorkerID  string `json:"worker_id"`
	Output    string `json:"output,omitempty"`
	Error     string `json:"error,omitempty"`
	Success   bool   `json:"success"`
}

// TaskExecutor is the pluggable "do the work" function. Returns the
// task output on success, or an error to be reported back to the
// supervisor as a failed step.
type TaskExecutor func(ctx context.Context, task Task) (output string, err error)

// EchoExecutor is a trivial TaskExecutor that returns the task's Input
// (or "ok" if Input is empty). Useful for integration tests and as a
// placeholder until real LLM-driven execution lands.
func EchoExecutor(_ context.Context, t Task) (string, error) {
	if t.Input != "" {
		return t.Input, nil
	}
	return "ok", nil
}

// Worker drains tasks from the dispatch topic and emits results.
type Worker struct {
	bus      *bus.Bus
	agentID  string
	executor TaskExecutor
	// PublishOpts controls how results are reported back to the
	// supervisor. Zero values resolve to bus defaults.
	PublishOpts bus.ReliableOpts
}

// New creates a Worker. agentID identifies this worker in result
// payloads; executor is the pluggable do-the-work callback.
func New(b *bus.Bus, agentID string, executor TaskExecutor) *Worker {
	if executor == nil {
		executor = EchoExecutor
	}
	return &Worker{bus: b, agentID: agentID, executor: executor}
}

// Run subscribes to the mission's dispatch topic and processes tasks
// until ctx is cancelled or the subscription channel closes. Each task
// is acked on receipt (whether the executor succeeds or fails) — the
// failure is reported via the result event, not via Nack. Nack is
// reserved for transient delivery problems the supervisor should retry
// (no current callers; reserved for future use).
//
// Run is safe to call once per Worker — subsequent calls return
// ErrAlreadyRunning. To re-use the bus, create a new Worker.
func (w *Worker) Run(ctx context.Context, missionID string) error {
	if missionID == "" {
		return errors.New("worker: missionID is required")
	}
	dispatchTopic := "mission." + missionID + ".dispatch"
	eventsTopic := "mission." + missionID + ".events"

	in := w.bus.SubscribeReliable(dispatchTopic)
	defer w.bus.UnsubscribeReliable(dispatchTopic, in)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case msg, ok := <-in:
			if !ok {
				return nil // channel closed (Unsubscribe)
			}
			w.handle(ctx, msg, eventsTopic)
		}
	}
}

func (w *Worker) handle(ctx context.Context, msg bus.ReliableMessage, eventsTopic string) {
	var task Task
	if err := json.Unmarshal([]byte(msg.Payload), &task); err != nil {
		// Malformed payload — ack so it doesn't loop forever, then
		// publish an error event.
		msg.Ack()
		w.publishResult(ctx, eventsTopic, Result{
			MissionID: msg.Topic, // best we can do without a parsed StepID
			WorkerID:  w.agentID,
			Error:     "malformed task: " + err.Error(),
			Success:   false,
		})
		return
	}

	output, err := w.executor(ctx, task)
	res := Result{
		StepID:    task.StepID,
		MissionID: task.MissionID,
		WorkerID:  w.agentID,
	}
	if err != nil {
		res.Error = err.Error()
		res.Success = false
	} else {
		res.Output = output
		res.Success = true
	}

	// Ack the dispatch BEFORE publishing the result — otherwise a slow
	// supervisor (waiting on the result publish) would keep the
	// dispatch un-acked and the supervisor would retry the dispatch
	// before getting the result.
	msg.Ack()

	// Publish the result asynchronously so a slow supervisor doesn't
	// block this worker from processing the NEXT dispatch. PublishOpts
	// caps total time spent (default 5s × MaxAttempts) so goroutines
	// drain naturally; ctx cancellation also tears them down. This is
	// the correct trade-off for at-least-once: the dispatch is acked
	// (no duplicate dispatch) and the result publish has its own
	// retry budget if the supervisor is briefly unavailable.
	go w.publishResult(ctx, eventsTopic, res)
}

func (w *Worker) publishResult(ctx context.Context, topic string, res Result) {
	payload, _ := json.Marshal(res)
	opts := w.PublishOpts
	if opts.Timeout == 0 {
		opts.Timeout = 5 * time.Second
	}
	// Best effort — if the supervisor isn't listening, the worker can't
	// fix that. Fire-and-forget.
	_ = w.bus.PublishReliable(ctx, topic, w.agentID, string(payload), opts)
}
