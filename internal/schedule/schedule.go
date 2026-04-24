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

// Scheduler runs scheduled agents based on cron expressions.
type Scheduler struct {
	store    *Store
	runner   AgentRunner
	interval time.Duration
	cancel   context.CancelFunc
	wg       sync.WaitGroup
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
	if s.runner == nil {
		return nil // No runner configured; skip silently.
	}
	schedules, err := s.store.List(ctx)
	if err != nil {
		return fmt.Errorf("scheduler.tick: %w", err)
	}

	now := time.Now().UTC()
	for _, sched := range schedules {
		if !sched.Enabled {
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
		// WARNING: scheduled agents run with full capabilities. Restrict via per-job config when available.
		log.Printf("scheduler: executing job %q — runs with full capabilities (restrict via config)", security.SanitizeLogValue(sched.Name))
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
			result, err := s.runner.Run(ctx, schedCopy.ID, schedCopy.Task, schedCopy.Channel)
			completedAt := time.Now().UTC()
			runCopy.CompletedAt = &completedAt
			if err != nil {
				runCopy.Status = "failed"
				runCopy.Error = err.Error()
			} else {
				runCopy.Status = "completed"
				runCopy.AgentID = result.ID
			}
			s.store.RecordRun(ctx, runCopy)
		}()
	}
	return nil
}
