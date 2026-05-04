// Package action records fine-grained side-effect events (each AppleScript
// invocation, each D-Bus call, each shell command) at a level finer than
// the existing compliance_audit log.
//
// Where compliance_audit captures call-level state (which tool was called
// with what input), actions captures effect-level state derived from the
// call: the target app/service/path, a short human-readable payload
// summary, duration, status, result summary, and any error.
//
// Every native-script executor that has a Recorder set will produce one
// Action row per Execute. Other tools can opt in by accepting a Recorder
// via a SetRecorder method. The Recorder interface is intentionally
// minimal so adoption is a small, mechanical change per tool.
package action

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/LumabyteCo/aibutler/internal/audit"
)

const maxPayloadFullBytes = 8 * 1024 // 8 KiB cap on stored payload JSON
const maxResultSummaryBytes = 1024   // 1 KiB cap on result summary

// Action describes one recorded effect.
type Action struct {
	Timestamp      time.Time
	AgentID        string
	SessionID      string
	Type           string // e.g. "applescript.exec", "dbus.call"
	Target         string // semantic target — app name, D-Bus service, etc.
	PayloadSummary string // one-line human-readable summary
	PayloadFull    string // full JSON of the call's input (auto-redacted, capped)
	DurationMS     int64
	Status         string // "success" | "error" | "denied"
	ResultSummary  string // truncated result snippet
	Error          string
}

// Recorder writes Actions to a backing store.
type Recorder interface {
	Record(ctx context.Context, a Action) error
}

// NopRecorder discards everything. Useful when action recording is disabled
// or in tests that don't care about the side effect.
type NopRecorder struct{}

// Record implements Recorder by doing nothing.
func (NopRecorder) Record(_ context.Context, _ Action) error { return nil }

// SQLiteRecorder writes Actions to the `actions` SQLite table created by
// migration 020.
type SQLiteRecorder struct {
	db *sql.DB
}

// NewSQLiteRecorder creates a SQLiteRecorder bound to the given DB handle.
func NewSQLiteRecorder(db *sql.DB) *SQLiteRecorder {
	return &SQLiteRecorder{db: db}
}

// Record persists the action. Empty Timestamp is filled with time.Now().
// Payloads are size-capped and auto-redacted via the audit package.
func (r *SQLiteRecorder) Record(ctx context.Context, a Action) error {
	if r == nil || r.db == nil {
		return nil
	}
	if a.Timestamp.IsZero() {
		a.Timestamp = time.Now()
	}
	if a.Status == "" {
		a.Status = "success"
	}

	payloadFull := capString(audit.Redact(a.PayloadFull), maxPayloadFullBytes)
	resultSummary := capString(a.ResultSummary, maxResultSummaryBytes)

	_, err := r.db.ExecContext(ctx,
		`INSERT INTO actions
		   (timestamp, agent_id, session_id, action_type, target,
		    payload_summary, payload_full, duration_ms, status,
		    result_summary, error)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		a.Timestamp, a.AgentID, a.SessionID, a.Type, a.Target,
		a.PayloadSummary, payloadFull, a.DurationMS, a.Status,
		resultSummary, a.Error,
	)
	if err != nil {
		return fmt.Errorf("action.record: %w", err)
	}
	return nil
}

// capString truncates s at n bytes and appends a marker if truncated.
// Multi-byte rune safety is best-effort; the marker makes truncation visible.
func capString(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "...[truncated]"
}

// QueryFilter selects which Actions to return from a Query call.
type QueryFilter struct {
	AgentID    string
	Type       string
	Status     string
	Since      *time.Time
	Until      *time.Time
	Limit      int // default 50, max 500
}

// Query returns recent actions matching the filter, newest first.
func (r *SQLiteRecorder) Query(ctx context.Context, f QueryFilter) ([]Action, error) {
	if r == nil || r.db == nil {
		return nil, nil
	}
	limit := f.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > 500 {
		limit = 500
	}

	q := `SELECT timestamp, agent_id, session_id, action_type, target,
	             payload_summary, payload_full, duration_ms, status,
	             result_summary, error
	      FROM actions
	      WHERE 1=1`
	args := []interface{}{}

	if f.AgentID != "" {
		q += " AND agent_id = ?"
		args = append(args, f.AgentID)
	}
	if f.Type != "" {
		q += " AND action_type = ?"
		args = append(args, f.Type)
	}
	if f.Status != "" {
		q += " AND status = ?"
		args = append(args, f.Status)
	}
	if f.Since != nil {
		q += " AND timestamp >= ?"
		args = append(args, *f.Since)
	}
	if f.Until != nil {
		q += " AND timestamp <= ?"
		args = append(args, *f.Until)
	}
	q += " ORDER BY timestamp DESC, id DESC LIMIT ?"
	args = append(args, limit)

	rows, err := r.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("action.query: %w", err)
	}
	defer rows.Close()

	var out []Action
	for rows.Next() {
		var a Action
		var session, agent, target, summary, payload, result, errStr sql.NullString
		var duration sql.NullInt64
		if err := rows.Scan(
			&a.Timestamp, &agent, &session, &a.Type, &target,
			&summary, &payload, &duration, &a.Status, &result, &errStr,
		); err != nil {
			return nil, err
		}
		a.AgentID = agent.String
		a.SessionID = session.String
		a.Target = target.String
		a.PayloadSummary = summary.String
		a.PayloadFull = payload.String
		a.DurationMS = duration.Int64
		a.ResultSummary = result.String
		a.Error = errStr.String
		out = append(out, a)
	}
	return out, rows.Err()
}

// toolRegistry is the narrow interface for registering tools (avoids import cycles).
type toolRegistry interface {
	Register(name, description, schema, capability string, exec func(ctx context.Context, input string) (string, error))
}

// RegisterListTool registers actions.list — a read-only query against the
// recorder. No capability gate (advisory / inspection).
func RegisterListTool(registry toolRegistry, r *SQLiteRecorder) {
	registry.Register(
		"actions.list",
		"List recent fine-grained actions Butler has performed (AppleScript calls, D-Bus calls, shell commands, etc.). "+
			"Filter by agent_id, action_type, status, or time window. Newest first. Default limit 50, max 500.",
		`{"type":"object","properties":{`+
			`"agent_id":{"type":"string"},`+
			`"action_type":{"type":"string","description":"e.g. applescript.exec, dbus.call"},`+
			`"status":{"type":"string","enum":["success","error","denied"]},`+
			`"limit":{"type":"integer","minimum":1,"maximum":500,"description":"Default 50, max 500"}`+
			`},"additionalProperties":false}`,
		"", // No capability — read-only inspection.
		func(ctx context.Context, input string) (string, error) {
			var args struct {
				AgentID    string `json:"agent_id"`
				ActionType string `json:"action_type"`
				Status     string `json:"status"`
				Limit      int    `json:"limit"`
			}
			input = strings.TrimSpace(input)
			if input != "" && input != "{}" {
				if err := json.Unmarshal([]byte(input), &args); err != nil {
					return "", fmt.Errorf("actions.list: invalid input: %w", err)
				}
			}
			rows, err := r.Query(ctx, QueryFilter{
				AgentID: args.AgentID,
				Type:    args.ActionType,
				Status:  args.Status,
				Limit:   args.Limit,
			})
			if err != nil {
				return "", err
			}
			out, err := json.Marshal(rows)
			if err != nil {
				return "", fmt.Errorf("actions.list: marshal: %w", err)
			}
			return string(out), nil
		},
	)
}
