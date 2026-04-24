// Package monitor provides security event monitoring and alerting.
// It records security-relevant events (failed logins, capability denials,
// cost spikes, etc.) to a SQLite database and supports threshold-based alerting.
package monitor

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"
)

// EventType categorizes security events.
type EventType string

const (
	EventFailedLogin       EventType = "failed_login"
	EventCapabilityDenied  EventType = "capability_denied"
	EventA2ARejected       EventType = "a2a_rejected"
	EventPluginQuarantined EventType = "plugin_quarantined"
	EventCostSpike         EventType = "cost_spike"
	EventUnusualToolCall   EventType = "unusual_tool_call"
)

// SecurityEvent represents a single recorded security event.
type SecurityEvent struct {
	ID        int64     `json:"id"`
	Type      EventType `json:"type"`
	Severity  string    `json:"severity"` // "info", "warning", "critical"
	Details   string    `json:"details"`  // JSON string
	IPAddress string    `json:"ip_address"`
	UserAgent string    `json:"user_agent"`
	Timestamp time.Time `json:"timestamp"`
}

// AlertThresholds defines when automated alerts should fire.
type AlertThresholds struct {
	FailedLoginsPerMinute int     // default 5
	CostSpikeMultiplier   float64 // default 3.0 (3x normal)
	UnusualToolCallCount  int     // default 50 per minute
}

// DefaultThresholds returns sensible default alert thresholds.
func DefaultThresholds() AlertThresholds {
	return AlertThresholds{
		FailedLoginsPerMinute: 5,
		CostSpikeMultiplier:   3.0,
		UnusualToolCallCount:  50,
	}
}

// Alert represents a triggered threshold alert.
type Alert struct {
	Type    EventType `json:"type"`
	Message string    `json:"message"`
	Count   int       `json:"count"`
	Since   time.Time `json:"since"`
}

// Monitor records and queries security events, and checks thresholds.
type Monitor struct {
	db         *sql.DB
	thresholds AlertThresholds
}

// New creates a Monitor with the given database and thresholds.
func New(db *sql.DB, thresholds AlertThresholds) *Monitor {
	if thresholds.FailedLoginsPerMinute == 0 {
		thresholds.FailedLoginsPerMinute = 5
	}
	if thresholds.CostSpikeMultiplier == 0 {
		thresholds.CostSpikeMultiplier = 3.0
	}
	if thresholds.UnusualToolCallCount == 0 {
		thresholds.UnusualToolCallCount = 50
	}
	return &Monitor{db: db, thresholds: thresholds}
}

// Record writes a security event to the database.
func (m *Monitor) Record(ctx context.Context, eventType EventType, severity, details, ip, ua string) error {
	if severity == "" {
		severity = "info"
	}
	_, err := m.db.ExecContext(ctx,
		`INSERT INTO security_events (event_type, severity, details, ip_address, user_agent, timestamp)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		string(eventType), severity, details, ip, ua, time.Now().UTC())
	if err != nil {
		return fmt.Errorf("monitor: record event: %w", err)
	}
	return nil
}

// RecentEvents returns the most recent security events, up to limit.
func (m *Monitor) RecentEvents(ctx context.Context, limit int) ([]SecurityEvent, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := m.db.QueryContext(ctx,
		`SELECT id, event_type, severity, COALESCE(details,''), COALESCE(ip_address,''),
		        COALESCE(user_agent,''), timestamp
		 FROM security_events ORDER BY timestamp DESC LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("monitor: recent events: %w", err)
	}
	defer rows.Close()

	return scanEvents(rows)
}

// EventsByType returns events of a specific type since the given time.
func (m *Monitor) EventsByType(ctx context.Context, eventType EventType, since time.Time) ([]SecurityEvent, error) {
	rows, err := m.db.QueryContext(ctx,
		`SELECT id, event_type, severity, COALESCE(details,''), COALESCE(ip_address,''),
		        COALESCE(user_agent,''), timestamp
		 FROM security_events WHERE event_type = ? AND timestamp >= ?
		 ORDER BY timestamp DESC`, string(eventType), since.UTC())
	if err != nil {
		return nil, fmt.Errorf("monitor: events by type: %w", err)
	}
	defer rows.Close()

	return scanEvents(rows)
}

// CheckThresholds evaluates current event counts against configured thresholds
// and returns any alerts that should be raised.
func (m *Monitor) CheckThresholds(ctx context.Context) ([]Alert, error) {
	var alerts []Alert
	oneMinuteAgo := time.Now().UTC().Add(-1 * time.Minute)

	// Check failed logins per minute.
	var failedLogins int
	err := m.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM security_events
		 WHERE event_type = ? AND timestamp >= ?`,
		string(EventFailedLogin), oneMinuteAgo).Scan(&failedLogins)
	if err != nil {
		return nil, fmt.Errorf("monitor: check failed logins: %w", err)
	}
	if failedLogins >= m.thresholds.FailedLoginsPerMinute {
		alerts = append(alerts, Alert{
			Type:    EventFailedLogin,
			Message: fmt.Sprintf("high rate of failed logins: %d in last minute (threshold: %d)", failedLogins, m.thresholds.FailedLoginsPerMinute),
			Count:   failedLogins,
			Since:   oneMinuteAgo,
		})
	}

	// Check unusual tool calls per minute.
	var toolCalls int
	err = m.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM security_events
		 WHERE event_type = ? AND timestamp >= ?`,
		string(EventUnusualToolCall), oneMinuteAgo).Scan(&toolCalls)
	if err != nil {
		return nil, fmt.Errorf("monitor: check tool calls: %w", err)
	}
	if toolCalls >= m.thresholds.UnusualToolCallCount {
		alerts = append(alerts, Alert{
			Type:    EventUnusualToolCall,
			Message: fmt.Sprintf("unusual tool call activity: %d in last minute (threshold: %d)", toolCalls, m.thresholds.UnusualToolCallCount),
			Count:   toolCalls,
			Since:   oneMinuteAgo,
		})
	}

	return alerts, nil
}

// Handler returns an HTTP handler for the security events dashboard endpoint.
// GET /api/dashboard/security/events?limit=N&type=event_type
func (m *Monitor) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
			return
		}

		ctx := r.Context()
		limit := 50
		if l := r.URL.Query().Get("limit"); l != "" {
			if n, err := strconv.Atoi(l); err == nil && n > 0 && n <= 500 {
				limit = n
			}
		}

		eventType := r.URL.Query().Get("type")

		var events []SecurityEvent
		var err error

		if eventType != "" {
			events, err = m.EventsByType(ctx, EventType(eventType), time.Now().Add(-24*time.Hour))
		} else {
			events, err = m.RecentEvents(ctx, limit)
		}

		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		if events == nil {
			events = []SecurityEvent{}
		}

		// Also check thresholds.
		alerts, _ := m.CheckThresholds(ctx)
		if alerts == nil {
			alerts = []Alert{}
		}

		writeJSON(w, http.StatusOK, map[string]interface{}{
			"events": events,
			"alerts": alerts,
		})
	})
}

func scanEvents(rows *sql.Rows) ([]SecurityEvent, error) {
	var events []SecurityEvent
	for rows.Next() {
		var e SecurityEvent
		var ts string
		if err := rows.Scan(&e.ID, &e.Type, &e.Severity, &e.Details, &e.IPAddress, &e.UserAgent, &ts); err != nil {
			return nil, fmt.Errorf("monitor: scan event: %w", err)
		}
		e.Timestamp, _ = time.Parse("2006-01-02 15:04:05", ts)
		if e.Timestamp.IsZero() {
			e.Timestamp, _ = time.Parse(time.RFC3339, ts)
		}
		events = append(events, e)
	}
	return events, nil
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
