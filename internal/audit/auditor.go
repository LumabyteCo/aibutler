package audit

import (
	"context"
	"database/sql"
	"time"

	"github.com/LumabyteCo/aibutler/internal/capability"
)

// SQLiteAuditor writes audit entries to the resource_access_log table.
type SQLiteAuditor struct {
	db *sql.DB
}

// NewSQLiteAuditor creates a new SQLite-backed auditor.
func NewSQLiteAuditor(db *sql.DB) *SQLiteAuditor {
	return &SQLiteAuditor{db: db}
}

// LogAccess persists an audit entry to the database.
func (a *SQLiteAuditor) LogAccess(ctx context.Context, entry capability.AuditEntry) error {
	ts := entry.Timestamp
	if ts.IsZero() {
		ts = time.Now().UTC()
	}

	_, err := a.db.ExecContext(ctx,
		`INSERT INTO resource_access_log (timestamp, agent_id, agent_type, session_id, schedule_name,
			resource_type, service, action, target, capability_used, credential_key, status, error,
			tokens_consumed, cost_usd)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		ts.Format(time.RFC3339),
		entry.AgentID,
		entry.AgentType,
		entry.SessionID,
		entry.ScheduleName,
		entry.ResourceType,
		entry.Service,
		entry.Action,
		entry.Target,
		entry.CapabilityUsed,
		entry.CredentialKey,
		entry.Status,
		entry.Error,
		entry.TokensConsumed,
		entry.CostUSD,
	)
	return err
}
