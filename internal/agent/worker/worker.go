// Package worker is one half of the mission orchestration pair.
//
// A Worker subscribes to the supervisor's dispatch topic, executes
// tasks via a pluggable TaskExecutor, and publishes results back to
// the supervisor's event topic — both legs use the bus's at-least-
// once delivery so neither dispatch nor result can be silently dropped
// on a slow consumer.
//
// By default a Worker processes one task at a time. Set
// Worker.MaxConcurrent > 1 to fan out: tasks run in their own
// goroutines bounded by a semaphore, with the receive loop blocking
// at the cap so the bus's competing-consumer dispatch keeps routing
// new work to peer workers until a slot frees up. Useful when a
// worker handles long-tail I/O-bound tasks (e.g. LLM calls) and the
// pool size is smaller than the in-flight work the user wants.
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
	"sync"
	"time"

	"github.com/LumabyteCo/aibutler/internal/agent/bus"
	"github.com/LumabyteCo/aibutler/internal/capability"
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
//
// NeedsConfirmation distinguishes "this step did not run because the
// underlying capability requires explicit user approval" from a real
// failure. When NeedsConfirmation=true the supervisor auto-pauses the
// mission to waiting_user instead of failing or replanning;
// ConfirmationReason carries the capability + engine reason for the
// pause event payload. Success is false in this case (the step did
// not complete) but the supervisor's branch order checks
// NeedsConfirmation first.
type Result struct {
	StepID             string `json:"step_id"`
	MissionID          string `json:"mission_id"`
	WorkerID           string `json:"worker_id"`
	Output             string `json:"output,omitempty"`
	Error              string `json:"error,omitempty"`
	Success            bool   `json:"success"`
	NeedsConfirmation  bool   `json:"needs_confirmation,omitempty"`
	ConfirmationReason string `json:"confirmation_reason,omitempty"`
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
	// MaxConcurrent caps how many tasks this worker may process at
	// once. Default 1 (preserves the historical one-task-at-a-time
	// semantics — fully backwards compatible). Set to N > 1 to fan
	// out within this worker: up to N tasks run concurrently in
	// their own goroutines. When the worker is at the cap the
	// receive loop blocks before consuming the next dispatch, so the
	// bus's competing-consumer routing keeps work flowing to peer
	// workers until a slot frees up.
	MaxConcurrent int
}

// defaultMaxConcurrent is the per-worker concurrency cap when
// Worker.MaxConcurrent is unset or non-positive.
const defaultMaxConcurrent = 1

func (w *Worker) maxConcurrent() int {
	if w.MaxConcurrent <= 0 {
		return defaultMaxConcurrent
	}
	return w.MaxConcurrent
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
// With Worker.MaxConcurrent == 1 (the default), Run handles tasks
// strictly sequentially. With MaxConcurrent > 1, Run fans tasks out
// into goroutines bounded by a semaphore: when N tasks are in flight,
// the receive loop blocks before consuming the next dispatch, so the
// bus's competing-consumer dispatch routes that dispatch to a peer
// worker (or queues briefly until a slot frees up). Run does not
// return until every in-flight handler completes, so cancellation
// drains cleanly rather than leaking goroutines.
//
// Run is safe to call once per Worker. To re-use the bus, create a
// new Worker.
func (w *Worker) Run(ctx context.Context, missionID string) error {
	if missionID == "" {
		return errors.New("worker: missionID is required")
	}
	dispatchTopic := "mission." + missionID + ".dispatch"
	eventsTopic := "mission." + missionID + ".events"

	// Subscribe as a competing consumer — each dispatched task should
	// reach EXACTLY ONE worker in the pool, not be broadcast to every
	// worker (which would duplicate the executor's work). The bus's
	// competing-consumer mode handles fair distribution via per-publish
	// shuffle + unbuffered send semantics with a fall-through timeout
	// for busy peers.
	in := w.bus.SubscribeCompeting(dispatchTopic)
	defer w.bus.UnsubscribeCompeting(dispatchTopic, in)

	// Bounded fan-out. sem capacity == maxConcurrent(): a token is
	// acquired BEFORE the worker competes for a dispatch, and released
	// when the handler goroutine returns. With cap=1 the loop is
	// effectively sequential (matches the historical Worker), preserving
	// full backwards compatibility.
	//
	// The "acquire before receive" ordering matters: if the worker
	// received from `in` first and then blocked on sem, a fully-saturated
	// worker could still pull a message off the dispatch chan and queue
	// it internally — leaving idle peer workers untouched. Acquiring
	// sem first means a saturated worker never reads from `in`, so the
	// bus's competing-consumer SendTimeout fall-through correctly routes
	// the next dispatch to an idle peer.
	sem := make(chan struct{}, w.maxConcurrent())

	// wg tracks in-flight handlers so Run can wait for them to drain
	// before returning. Without this, a Run that exits on ctx.Done
	// could leave handler goroutines running with a cancelled context
	// — they'd eventually exit but only after the caller assumed
	// shutdown completed.
	var wg sync.WaitGroup
	defer wg.Wait()

	for {
		// Stage 1: claim a concurrency slot. Block here while at the
		// cap so the bus's PublishCompeting falls through to peer
		// workers via SendTimeout rather than queuing inside this one.
		select {
		case sem <- struct{}{}:
		case <-ctx.Done():
			return ctx.Err()
		}

		// Stage 2: have a slot — now compete for the next dispatch.
		select {
		case <-ctx.Done():
			<-sem
			return ctx.Err()
		case msg, ok := <-in:
			if !ok {
				<-sem
				return nil // channel closed (Unsubscribe)
			}
			wg.Add(1)
			go func(msg bus.ReliableMessage) {
				defer wg.Done()
				defer func() { <-sem }()
				w.handle(ctx, msg, eventsTopic)
			}(msg)
		}
	}
}

func (w *Worker) handle(ctx context.Context, msg bus.ReliableMessage, eventsTopic string) {
	// Ack-on-receipt: confirm "I got the message" immediately so the
	// supervisor's reliable-publish returns quickly. The actual work
	// can take seconds (or minutes for LLM-backed task execution) —
	// blocking the dispatch ack on completion would force every
	// supervisor PublishReliable to use a long timeout to cover the
	// worst-case worker. Instead the supervisor relies on its own
	// StepTimeout (waiting on the result event) to detect stuck
	// workers; the dispatch ack is purely a delivery confirmation.
	//
	// Trade-off: if the worker process crashes between Ack and
	// publishing the result, the dispatch is lost (the supervisor
	// times out its step-wait and replans / fails). For at-least-
	// once mission orchestration this is the right call — re-running
	// an expensive LLM step blindly would burn budget faster than
	// just failing and surfacing the issue to the user.
	msg.Ack()

	var task Task
	if err := json.Unmarshal([]byte(msg.Payload), &task); err != nil {
		// Malformed payload — publish an error event so the supervisor
		// can record the failure rather than wait until step timeout.
		go w.publishResult(ctx, eventsTopic, Result{
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
		// Detect "this step needs user confirmation" as a structured
		// pause signal rather than a hard failure. The supervisor will
		// auto-pause the mission to waiting_user; the step state goes
		// to waiting_user too so resume picks it up cleanly. The
		// underlying error message is captured in both Error and
		// ConfirmationReason for audit-log convenience.
		var confirmErr *capability.ConfirmationRequiredError
		if errors.As(err, &confirmErr) {
			res.NeedsConfirmation = true
			res.ConfirmationReason = confirmErr.Error()
			res.Error = confirmErr.Error()
		} else {
			res.Error = err.Error()
		}
		res.Success = false
	} else {
		res.Output = output
		res.Success = true
	}

	// Publish the result asynchronously so the worker can immediately
	// process the NEXT dispatch. PublishOpts caps total time spent
	// (default 5s × MaxAttempts) so goroutines drain naturally;
	// ctx cancellation also tears them down.
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
