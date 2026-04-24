package swarm

import (
	"context"
	"log"
	"time"
)

// StartTTLEnforcer starts a background goroutine that periodically purges
// old workspace entries. Returns a stop function.
func StartTTLEnforcer(ctx context.Context, ws *Workspace, interval time.Duration, ttlHours int) func() {
	ctx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})

	go func() {
		defer close(done)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				n, err := ws.PurgeOlderThan(ctx, ttlHours)
				if err != nil {
					log.Printf("workspace ttl: purge error: %v", err)
				} else if n > 0 {
					log.Printf("workspace ttl: purged %d old entries", n)
				}
			}
		}
	}()

	return func() {
		cancel()
		<-done
	}
}
