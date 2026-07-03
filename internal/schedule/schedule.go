package schedule

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/LumabyteCo/aibutler/internal/security"

	"github.com/LumabyteCo/aibutler/internal/agent"
)

// AgentRunner runs an agent for a scheduled task.
type AgentRunner interface {
	Run(ctx context.Context, sessionID, task, channel string) (*agent.Result, error)
}

// CapabilityRunner is implemented by runners that can execute a scheduled
// task under a restricted capability set. When a schedule declares
// capabilities and the runner supports scoping, the job gets exactly those
// resources — background work holds only the permissions it needs.
type CapabilityRunner interface {
	RunWithCapabilities(ctx context.Context, sessionID, task, channel string, capResources []string) (*agent.Result, error)
}

// BuiltinTask is deterministic maintenance code dispatched by the scheduler
// instead of the agent loop. Builtins get cron scheduling, persistence,
// run history, and downtime recovery without involving a model.
type BuiltinTask func(ctx context.Context) (summary string, err error)

// BuiltinPrefix marks a schedule's Task as a builtin dispatch key.
const BuiltinPrefix = "builtin:"

// Scheduler runs scheduled agents based on cron expressions.
type Scheduler struct {
	store    *Store
	runner   AgentRunner
	interval time.Duration
	cancel   context.CancelFunc
	wg       sync.WaitGroup

	builtinMu sync.RWMutex
	builtins  map[string]BuiltinTask
}

// NewScheduler creates a new scheduler.
func NewScheduler(store *Store, runner AgentRunner, tickInterval time.Duration) *Scheduler {
	if tickInterval == 0 {
		tickInterval = 60 * time.Second
	}
	return &Scheduler{
		store:    store,
		runner:   runner,
		interval: tickInterval,
	}
}

// SetRunner sets the agent runner for executing scheduled tasks.
// Called after the factory is created in cmd_run, since the factory depends
// on components initialized after the scheduler.
func (s *Scheduler) SetRunner(r AgentRunner) {
	s.runner = r
}

// RegisterBuiltin registers deterministic maintenance code under
// "builtin:<name>". Schedules whose Task equals that key dispatch to fn
// instead of the agent loop.
func (s *Scheduler) RegisterBuiltin(name string, fn BuiltinTask) {
	s.builtinMu.Lock()
	defer s.builtinMu.Unlock()
	if s.builtins == nil {
		s.builtins = make(map[string]BuiltinTask)
	}
	s.builtins[name] = fn
}

func (s *Scheduler) builtinFor(task string) (BuiltinTask, bool) {
	if !hasBuiltinPrefix(task) {
		return nil, false
	}
	s.builtinMu.RLock()
	defer s.builtinMu.RUnlock()
	fn, ok := s.builtins[task[len(BuiltinPrefix):]]
	return fn, ok
}

// EnsureBuiltinSchedule creates the schedule row for a builtin task if no
// schedule with that name exists yet. Idempotent across restarts.
func (s *Scheduler) EnsureBuiltinSchedule(ctx context.Context, name, cronExpr, builtinName string, capabilities []string) error {
	existing, err := s.store.List(ctx)
	if err != nil {
		return err
	}
	for _, e := range existing {
		if e.Name == name {
			return nil
		}
	}
	return s.store.create(ctx, &Schedule{
		ID:           fmt.Sprintf("sched_builtin_%s", builtinName),
		Name:         name,
		CronExpr:     cronExpr,
		Task:         BuiltinPrefix + builtinName,
		Channel:      "internal",
		AccountID:    "system",
		Capabilities: capabilities,
		Enabled:      true,
	})
}

// DisableBuiltinSchedule turns off a builtin's schedule row if it exists —
// used when the owning feature is disabled, so the row doesn't fire
// "unknown builtin" failures forever.
func (s *Scheduler) DisableBuiltinSchedule(ctx context.Context, builtinName string) {
	id := fmt.Sprintf("sched_builtin_%s", builtinName)
	if err := s.store.SetEnabled(ctx, id, false); err == nil {
		log.Printf("scheduler: disabled builtin schedule %s (feature opted out)", id)
	}
}

func hasBuiltinPrefix(task string) bool {
	return len(task) >= len(BuiltinPrefix) && task[:len(BuiltinPrefix)] == BuiltinPrefix
}

// Start begins the scheduler tick loop.
func (s *Scheduler) Start(ctx context.Context) {
	ctx, s.cancel = context.WithCancel(ctx)
	s.wg.Add(1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("PANIC recovered in scheduler: %v", r)
			}
		}()
		defer s.wg.Done()
		ticker := time.NewTicker(s.interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := s.Tick(ctx); err != nil {
					log.Printf("scheduler: tick error: %v", err)
				}
			}
		}
	}()
}

// Stop cancels the scheduler tick loop and waits for completion.
func (s *Scheduler) Stop() {
	if s.cancel != nil {
		s.cancel()
	}
	s.wg.Wait()
}

// Tick checks all enabled schedules and runs any that are due.
func (s *Scheduler) Tick(ctx context.Context) error {
	schedules, err := s.store.List(ctx)
	if err != nil {
		return fmt.Errorf("scheduler.tick: %w", err)
	}

	now := time.Now().UTC()
	for _, sched := range schedules {
		if !sched.Enabled {
			continue
		}
		// Builtins run without a model runner; agent tasks need one.
		if s.runner == nil && !hasBuiltinPrefix(sched.Task) {
			continue
		}

		cron, err := ParseCron(sched.CronExpr)
		if err != nil {
			log.Printf("scheduler: invalid cron %q for %s: %v", security.SanitizeLogValue(sched.CronExpr), security.SanitizeLogValue(sched.Name), err)
			continue
		}

		lastRun, err := s.store.LastRun(ctx, sched.ID)
		if err != nil {
			continue
		}

		var lastTime time.Time
		if lastRun != nil {
			lastTime = lastRun.StartedAt
		} else {
			// No previous run: use creation time minus 1 minute so first tick runs immediately
			lastTime = sched.CreatedAt.Add(-time.Minute)
		}

		next := cron.Next(lastTime)
		if next.After(now) {
			continue
		}

		// Schedule is due — run it.
		run := &Run{
			ScheduleID: sched.ID,
			Status:     "running",
			StartedAt:  now,
		}
		if err := s.store.RecordRun(ctx, run); err != nil {
			continue
		}

		// Run agent in goroutine
		schedCopy := sched
		runCopy := run
		go func() {
			defer func() {
				if r := recover(); r != nil {
					log.Printf("PANIC recovered in scheduler-run %s: %v", security.SanitizeLogValue(schedCopy.Name), r)
				}
			}()
			s.executeAndRecord(ctx, schedCopy, runCopy)
		}()
	}
	return nil
}

// executeAndRecord dispatches one due schedule and records its outcome. The
// single dispatch decision — builtin, capability-scoped, or default — is
// shared by the tick loop and downtime recovery so both paths behave
// identically, and the log states what actually happens on the chosen path.
func (s *Scheduler) executeAndRecord(ctx context.Context, sched Schedule, run *Run) {
	var agentID string
	var runErr error

	switch {
	case hasBuiltinPrefix(sched.Task):
		if fn, ok := s.builtinFor(sched.Task); ok {
			// Deterministic maintenance code — no model, no tools. The
			// capabilities column is informational for builtins: they work
			// through their own stores, not the tool dispatcher.
			log.Printf("scheduler: executing builtin %q (deterministic; no model)", security.SanitizeLogValue(sched.Name))
			_, runErr = fn(ctx)
		} else {
			runErr = fmt.Errorf("unknown builtin task %q", sched.Task)
		}
	case len(sched.Capabilities) > 0:
		// Fail closed: a declared capability profile must never silently
		// degrade to the full default set.
		if cr, ok := s.runner.(CapabilityRunner); ok {
			log.Printf("scheduler: executing job %q with scoped capabilities %v", security.SanitizeLogValue(sched.Name), sched.Capabilities)
			var result *agent.Result
			result, runErr = cr.RunWithCapabilities(ctx, sched.ID, sched.Task, sched.Channel, sched.Capabilities)
			if result != nil {
				agentID = result.ID
			}
		} else {
			runErr = fmt.Errorf("schedule declares capabilities but the runner cannot scope them — refusing to run with the full default set")
		}
	case s.runner != nil:
		log.Printf("scheduler: executing job %q — runs with the default capability set (declare capabilities on the schedule to restrict)", security.SanitizeLogValue(sched.Name))
		var result *agent.Result
		result, runErr = s.runner.Run(ctx, sched.ID, sched.Task, sched.Channel)
		if result != nil {
			agentID = result.ID
		}
	default:
		runErr = fmt.Errorf("no runner configured")
	}

	completedAt := time.Now().UTC()
	run.CompletedAt = &completedAt
	if runErr != nil {
		run.Status = "failed"
		run.Error = runErr.Error()
	} else {
		run.Status = "completed"
		run.AgentID = agentID
	}
	s.store.RecordRun(ctx, run)
}
