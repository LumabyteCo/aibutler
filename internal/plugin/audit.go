package plugin

import (
	"context"
	"database/sql"
	"fmt"
)

// SQLiteAuditWriter writes plugin audit entries to the plugin_audit table.
type SQLiteAuditWriter struct {
	db *sql.DB
}

// NewSQLiteAuditWriter creates an audit writer backed by SQLite.
func NewSQLiteAuditWriter(db *sql.DB) *SQLiteAuditWriter {
	return &SQLiteAuditWriter{db: db}
}

// WriteAudit records a plugin action to the audit log.
func (w *SQLiteAuditWriter) WriteAudit(ctx context.Context, pluginID int64, action, capability, status string) error {
	_, err := w.db.ExecContext(ctx,
		`INSERT INTO plugin_audit (plugin_id, action, capability_used, status) VALUES (?, ?, ?, ?)`,
		pluginID, action, capability, status)
	if err != nil {
		return fmt.Errorf("plugin audit: write: %w", err)
	}
	return nil
}

// WriteAuditWithError records a plugin action with an error message.
func (w *SQLiteAuditWriter) WriteAuditWithError(ctx context.Context, pluginID int64, action, capability, status, errMsg string) error {
	_, err := w.db.ExecContext(ctx,
		`INSERT INTO plugin_audit (plugin_id, action, capability_used, status, error_message) VALUES (?, ?, ?, ?, ?)`,
		pluginID, action, capability, status, errMsg)
	if err != nil {
		return fmt.Errorf("plugin audit: write: %w", err)
	}
	return nil
}
