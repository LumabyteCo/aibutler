// Package missionruntime is the auto-orchestration layer for mode=mission.
//
// When the agent is configured to run in mission mode, the runtime polls
// the mission store for planned missions and spawns a supervisor+worker
// pair for each one. The pair drives the mission to a terminal state,
// then the runtime picks up the next pending mission. Missions can be
// created at any time via the mission.create tool — the runtime finds
// them on the next poll tick and starts them automatically.
//
// Initial implementation:
//
//   - Sequential dispatch within a single mission (already true in the
//     supervisor); concurrent missions across the runtime (one
//     supervisor goroutine per running mission, capped by MaxConcurrent).
//   - TaskExecutor is pluggable — the default EchoExecutor lets the
//     orchestration mechanics ship today. Real LLM-backed task execution
//     is a clearly-scoped follow-up: replace the executor with a function
//     that runs the task text through the agent loop and returns the
//     result string.
package missionruntime

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/LumabyteCo/aibutler/internal/agent/bus"
	"github.com/LumabyteCo/aibutler/internal/agent/supervisor"
	"github.com/LumabyteCo/aibutler/internal/agent/worker"
	"github.com/LumabyteCo/aibutler/internal/mission"
)

// Defaults — overridable via Options on New.
const (
	defaultPollInterval  = 2 * time.Second
	defaultMaxConcurrent = 4
)

// Options configures the runtime.
type Options struct {
	// PollInterval — how often to scan the store for pending missions.
	// Default 2s.
	PollInterval time.Duration
	// MaxConcurrent caps the number of simultaneously-running missions.
	// Once at the cap, additional pending missions wait until a slot
	// frees up. Default 4.
	MaxConcurrent int
	// Executor is the TaskExecutor passed to every spawned worker. If
	// nil, worker.EchoExecutor is used.
	Executor worker.TaskExecutor
	// Replanner, if non-nil, is set on every spawned Supervisor as
	// the recovery policy for step failures. nil (the default) keeps
	// the historical fail-on-first-step-failure behaviour. The
	// runtime does not own the Replanner's lifecycle — callers are
	// responsible for constructing it (typically via
	// NewLLMReplanner) before passing it in.
	Replanner supervisor.Replanner
	// MaxReplans caps how many times one mission may be replanned
	// before the supervisor gives up. Default 3 when Replanner is
	// non-nil; ignored when Replanner is nil.
	MaxReplans int
	// Logger optionally captures runtime lifecycle messages. Defaults
	// to the standard log package.
	Logger *log.Logger
}

// Runtime polls for planned missions and spawns a supervisor+worker pair
// per running mission.
type Runtime struct {
	mgr   *mission.Manager
	store mission.Store
	bus   *bus.Bus
	opts  Options

	mu      sync.Mutex
	running map[string]context.CancelFunc // missionID → cancel hook for supervisor+worker

	stopped chan struct{}
}

// New creates a Runtime with the given dependencies and options.
// Zero-valued option fields resolve to defaults.
func New(mgr *mission.Manager, store mission.Store, b *bus.Bus, opts Options) *Runtime {
	if opts.PollInterval <= 0 {
		opts.PollInterval = defaultPollInterval
	}
	if opts.MaxConcurrent <= 0 {
		opts.MaxConcurrent = defaultMaxConcurrent
	}
	if opts.Executor == nil {
		opts.Executor = worker.EchoExecutor
	}
	return &Runtime{
		mgr:     mgr,
		store:   store,
		bus:     b,
		opts:    opts,
		running: map[string]context.CancelFunc{},
	}
}

// Start blocks until ctx is cancelled, polling the store at PollInterval
// and spawning supervisor+worker pairs for newly-planned missions.
//
// Safe to call exactly once per Runtime. The returned error is ctx.Err()
// when ctx is cancelled, never anything else — per-mission failures are
// logged and the runtime carries on.
func (r *Runtime) Start(ctx context.Context) error {
	r.stopped = make(chan struct{})
	defer close(r.stopped)

	r.logf("mission runtime: started (poll=%s, max_concurrent=%d)",
		r.opts.PollInterval, r.opts.MaxConcurrent)

	// Run an initial scan immediately so a mission already pending at
	// startup doesn't have to wait one whole poll interval.
	r.scan(ctx)

	ticker := time.NewTicker(r.opts.PollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			r.logf("mission runtime: stopping (ctx done)")
			r.stopAll()
			return ctx.Err()
		case <-ticker.C:
			r.scan(ctx)
		}
	}
}

// Wait blocks until Start returns. Useful in tests that want to confirm
// shutdown completed.
func (r *Runtime) Wait() {
	if r.stopped == nil {
		return
	}
	<-r.stopped
}

// RunningCount returns the number of supervisor goroutines currently active.
func (r *Runtime) RunningCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.running)
}

// SetExecutor replaces the TaskExecutor used by future supervisor+worker
// pairs. Existing in-flight workers keep the executor they were spawned
// with — only newly-spawned workers see the change.
//
// Used by app startup to swap the default EchoExecutor for an LLM-backed
// executor once the model adapter is resolved.
func (r *Runtime) SetExecutor(fn worker.TaskExecutor) {
	if fn == nil {
		fn = worker.EchoExecutor
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.opts.Executor = fn
}

// SetReplanner replaces the Replanner installed on future
// Supervisor instances. Existing in-flight supervisors keep the
// Replanner they were spawned with. Pass nil to revert to fail-fast.
//
// Same use-case as SetExecutor: app startup wires the LLM-backed
// replanner once the model adapter is resolved.
func (r *Runtime) SetReplanner(rp supervisor.Replanner, maxReplans int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.opts.Replanner = rp
	if maxReplans > 0 {
		r.opts.MaxReplans = maxReplans
	}
}

// scan looks for planned missions and spawns runner goroutines for any
// not already running, up to MaxConcurrent.
func (r *Runtime) scan(ctx context.Context) {
	r.mu.Lock()
	slotsLeft := r.opts.MaxConcurrent - len(r.running)
	r.mu.Unlock()
	if slotsLeft <= 0 {
		return
	}

	pending, err := r.store.ListMissions(ctx, mission.ListFilter{
		State: mission.StatePlanned,
		Limit: slotsLeft,
	})
	if err != nil {
		r.logf("mission runtime: list pending: %v", err)
		return
	}

	for _, m := range pending {
		r.spawn(ctx, m.ID)
	}
}

// spawn starts a supervisor + worker pair for the given mission.
func (r *Runtime) spawn(parentCtx context.Context, missionID string) {
	r.mu.Lock()
	if _, already := r.running[missionID]; already {
		r.mu.Unlock()
		return
	}
	if len(r.running) >= r.opts.MaxConcurrent {
		r.mu.Unlock()
		return
	}
	runCtx, cancel := context.WithCancel(parentCtx)
	r.running[missionID] = cancel
	r.mu.Unlock()

	// Identify the spawned agents per-mission so the audit trail can
	// distinguish missions running in parallel.
	short := missionID
	if len(short) > 8 {
		short = short[len(short)-8:]
	}

	w := worker.New(r.bus, "worker-"+short, r.opts.Executor)
	s := supervisor.New(r.mgr, r.store, r.bus, "supervisor-"+short)
	if r.opts.Replanner != nil {
		s.Replanner = r.opts.Replanner
		s.MaxReplans = r.opts.MaxReplans
	}

	r.logf("mission runtime: spawning supervisor+worker for %s", missionID)

	// Worker first so it's listening when the supervisor dispatches.
	workerDone := make(chan struct{})
	go func() {
		defer close(workerDone)
		_ = w.Run(runCtx, missionID)
	}()
	// Tiny delay to ensure worker.Run has called SubscribeReliable
	// before the supervisor publishes. Without this, the first dispatch
	// could hit ErrNoSubscribers and have to retry — correct, but
	// noisy in the audit log. Belt and braces.
	time.Sleep(20 * time.Millisecond)

	go func() {
		defer func() {
			cancel()
			<-workerDone
			r.mu.Lock()
			delete(r.running, missionID)
			r.mu.Unlock()
			r.logf("mission runtime: %s exited", missionID)
		}()
		err := s.Run(runCtx, missionID)
		if err != nil && !isCleanExit(err) {
			r.logf("mission runtime: supervisor for %s returned: %v", missionID, err)
		}
	}()
}

// stopAll cancels every running supervisor and waits for them to drain.
// Called when ctx is cancelled.
func (r *Runtime) stopAll() {
	r.mu.Lock()
	cancels := make([]context.CancelFunc, 0, len(r.running))
	for _, cancel := range r.running {
		cancels = append(cancels, cancel)
	}
	r.mu.Unlock()
	for _, cancel := range cancels {
		cancel()
	}
	// Wait briefly for the running map to drain — the supervisor's
	// defer hooks remove entries.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		r.mu.Lock()
		empty := len(r.running) == 0
		r.mu.Unlock()
		if empty {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// isCleanExit reports whether err represents a normal supervisor exit
// (mission paused, ctx cancelled) rather than an actionable failure.
func isCleanExit(err error) bool {
	if err == nil {
		return true
	}
	if err == context.Canceled || err == context.DeadlineExceeded {
		return true
	}
	if err == supervisor.ErrMissionPaused {
		return true
	}
	return false
}

func (r *Runtime) logf(format string, args ...interface{}) {
	if r.opts.Logger != nil {
		r.opts.Logger.Printf(format, args...)
		return
	}
	log.Printf(format, args...)
}
