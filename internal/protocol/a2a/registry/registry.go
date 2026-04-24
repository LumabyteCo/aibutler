package registry

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"time"
)

// AgentRecord is a peer agent entry in the registry.
type AgentRecord struct {
	ID                  int64
	Name                string
	URL                 string
	Capabilities        []string
	LastSeen            string
	SuccessCount        int
	FailureCount        int
	ConsecutiveFailures int
	Quarantined         bool
	RegisteredAt        string
}

// Registry maintains a dynamic catalog of known peer agents.
type Registry struct {
	db *sql.DB
}

// New creates an agent registry backed by the given database.
func New(db *sql.DB) *Registry {
	return &Registry{db: db}
}

// Register inserts or updates an agent entry.
func (r *Registry) Register(ctx context.Context, name, url string, capabilities []string, healthCheckURL string) error {
	caps, _ := json.Marshal(capabilities)
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO agent_registry (name, url, capabilities, health_check_url, last_seen, registered_at)
		 VALUES (?, ?, ?, ?, ?, ?)
		 ON CONFLICT(name) DO UPDATE SET
		     url = excluded.url,
		     capabilities = excluded.capabilities,
		     health_check_url = excluded.health_check_url,
		     last_seen = excluded.last_seen`,
		name, url, string(caps), healthCheckURL, now, now)
	if err != nil {
		return fmt.Errorf("registry: register: %w", err)
	}
	return nil
}

// Heartbeat updates the last_seen timestamp for an agent and unquarantines it.
func (r *Registry) Heartbeat(ctx context.Context, name string) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE agent_registry SET last_seen = ?, quarantined = 0, consecutive_failures = 0 WHERE name = ?`,
		time.Now().UTC().Format(time.RFC3339), name)
	return err
}

// Quarantine manually quarantines an agent.
func (r *Registry) Quarantine(ctx context.Context, name string) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE agent_registry SET quarantined = 1 WHERE name = ?`, name)
	return err
}

// Unquarantine manually removes quarantine from an agent.
func (r *Registry) Unquarantine(ctx context.Context, name string) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE agent_registry SET quarantined = 0, consecutive_failures = 0 WHERE name = ?`, name)
	return err
}

// Discover returns healthy, non-quarantined agents that have the given capability.
func (r *Registry) Discover(ctx context.Context, capability string) ([]AgentRecord, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, name, url, capabilities, COALESCE(last_seen,''), success_count, failure_count,
		        COALESCE(consecutive_failures, 0), COALESCE(quarantined, 0), registered_at
		 FROM agent_registry
		 WHERE capabilities LIKE ?
		   AND COALESCE(quarantined, 0) = 0
		   AND (success_count + failure_count < 5
		        OR CAST(success_count AS REAL) / (success_count + failure_count) >= 0.8)
		 ORDER BY success_count DESC`,
		"%"+capability+"%")
	if err != nil {
		return nil, fmt.Errorf("registry: discover: %w", err)
	}
	defer rows.Close()
	return scanRecords(rows)
}

// DiscoverAll returns all registered agents regardless of capability.
func (r *Registry) DiscoverAll(ctx context.Context) ([]AgentRecord, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, name, url, capabilities, COALESCE(last_seen,''), success_count, failure_count,
		        COALESCE(consecutive_failures, 0), COALESCE(quarantined, 0), registered_at
		 FROM agent_registry ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("registry: discover all: %w", err)
	}
	defer rows.Close()
	return scanRecords(rows)
}

// MarkSuccess increments the success count and resets consecutive failures for an agent.
func (r *Registry) MarkSuccess(ctx context.Context, name string) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE agent_registry SET success_count = success_count + 1, consecutive_failures = 0, last_seen = ? WHERE name = ?`,
		time.Now().UTC().Format(time.RFC3339), name)
	return err
}

// MarkFailure increments the failure count and consecutive failure count for an agent.
// Auto-quarantines agents with 5+ consecutive failures.
func (r *Registry) MarkFailure(ctx context.Context, name string) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE agent_registry SET
			failure_count = failure_count + 1,
			consecutive_failures = COALESCE(consecutive_failures, 0) + 1,
			quarantined = CASE WHEN COALESCE(consecutive_failures, 0) + 1 >= 5 THEN 1 ELSE COALESCE(quarantined, 0) END
		 WHERE name = ?`, name)
	return err
}

// Deregister removes an agent from the registry.
func (r *Registry) Deregister(ctx context.Context, name string) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM agent_registry WHERE name = ?`, name)
	return err
}

func scanRecords(rows *sql.Rows) ([]AgentRecord, error) {
	var records []AgentRecord
	for rows.Next() {
		var rec AgentRecord
		var capsJSON string
		var quarantinedInt int
		if err := rows.Scan(&rec.ID, &rec.Name, &rec.URL, &capsJSON,
			&rec.LastSeen, &rec.SuccessCount, &rec.FailureCount,
			&rec.ConsecutiveFailures, &quarantinedInt, &rec.RegisteredAt); err != nil {
			return nil, fmt.Errorf("registry: scan: %w", err)
		}
		rec.Quarantined = quarantinedInt != 0
		if err := json.Unmarshal([]byte(capsJSON), &rec.Capabilities); err != nil {
			log.Printf("registry: unmarshal capabilities for agent %s: %v", rec.Name, err)
		}
		records = append(records, rec)
	}
	return records, rows.Err()
}
