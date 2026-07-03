package schedule

import (
	"context"
	"fmt"
	"log"
	"math/rand"
	"time"

	"github.com/LumabyteCo/aibutler/internal/security"
)

// ReliabilityConfig controls retry, jitter, and missed-run recovery behavior.
type ReliabilityConfig struct {
	MaxRetries    int
	JitterMaxSecs int
	RecoverMissed bool
}

// DefaultReliability returns a default reliability configuration.
func DefaultReliability() ReliabilityConfig {
	return ReliabilityConfig{
		MaxRetries:    3,
		JitterMaxSecs: 60,
		RecoverMissed: true,
	}
}

// RecoverMissed checks all enabled schedules for runs that were missed
// (e.g., during downtime) and fires them. Returns the count of recovered runs.
func (s *Scheduler) RecoverMissed(ctx context.Context) (int, error) {
	schedules, err := s.store.List(ctx)
	if err != nil {
		return 0, fmt.Errorf("recover: list: %w", err)
	}

	now := time.Now().UTC()
	recovered := 0

	for _, sched := range schedules {
		if !sched.Enabled {
			continue
		}
		// Builtins recover without a model runner; agent tasks need one.
		if s.runner == nil && !hasBuiltinPrefix(sched.Task) {
			continue
		}

		cron, err := ParseCron(sched.CronExpr)
		if err != nil {
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
			lastTime = sched.CreatedAt.Add(-time.Minute)
		}

		// Check if there was at least one scheduled time between last run and now.
		nextDue := cron.Next(lastTime)
		if nextDue.Before(now) {
			// This run was missed. Execute it now.
			run := &Run{
				ScheduleID: sched.ID,
				Status:     "running",
				StartedAt:  now,
			}
			if err := s.store.RecordRun(ctx, run); err != nil {
				continue
			}

			// Same dispatch as the tick loop: builtins run their registered
			// code and capability profiles stay enforced — recovery must not
			// be a side door around either.
			go func(sc Schedule, r *Run) {
				defer func() {
					if rec := recover(); rec != nil {
						log.Printf("PANIC recovered in scheduler-recovery %s: %v", security.SanitizeLogValue(sc.Name), rec)
					}
				}()
				s.executeAndRecord(ctx, sc, r)
			}(sched, run)

			recovered++
			log.Printf("scheduler: recovered missed run for %s (last run: %s, due: %s)",
				security.SanitizeLogValue(sched.Name), lastTime.Format(time.RFC3339), nextDue.Format(time.RFC3339))
		}
	}

	return recovered, nil
}

// RunWithRetry executes a schedule with exponential backoff retries.
func (s *Scheduler) RunWithRetry(ctx context.Context, sched Schedule, maxRetries int) error {
	if s.runner == nil {
		return fmt.Errorf("scheduler: no runner configured")
	}

	var lastErr error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			// Exponential backoff: 1s, 2s, 4s, ...
			backoff := time.Duration(1<<uint(attempt-1)) * time.Second
			// Add jitter (up to 500ms).
			jitter := time.Duration(rand.Intn(500)) * time.Millisecond
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(backoff + jitter):
			}
		}

		_, err := s.runner.Run(ctx, sched.ID, sched.Task, sched.Channel)
		if err == nil {
			return nil
		}
		lastErr = err
		log.Printf("scheduler: attempt %d/%d for %s failed: %v", attempt+1, maxRetries+1, security.SanitizeLogValue(sched.Name), err)
	}
	return fmt.Errorf("scheduler: all %d retries exhausted for %s: %w", maxRetries+1, sched.Name, lastErr)
}

// Jitter returns a random duration between 0 and maxSecs seconds.
func Jitter(maxSecs int) time.Duration {
	if maxSecs <= 0 {
		return 0
	}
	return time.Duration(rand.Intn(maxSecs*1000)) * time.Millisecond
}
