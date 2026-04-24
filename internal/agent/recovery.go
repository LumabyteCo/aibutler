package agent

import (
	"context"
	"database/sql"
	"log"
)

// RecoverAgents finds agents stuck in non-terminal states and marks them as FAILED.
// Called on startup to handle crash recovery.
func RecoverAgents(ctx context.Context, db *sql.DB) (int, error) {
	if db == nil {
		return 0, nil
	}

	result, err := db.ExecContext(ctx,
		`UPDATE agents SET state = ? WHERE state IN (?, ?, ?)`,
		string(StateFailed), string(StateSpawned), string(StateRunning), string(StateWaiting))
	if err != nil {
		return 0, err
	}

	count, err := result.RowsAffected()
	if err != nil {
		return 0, err
	}

	if count > 0 {
		log.Printf("recovery: marked %d stuck agent(s) as failed", count)
	}
	return int(count), nil
}
