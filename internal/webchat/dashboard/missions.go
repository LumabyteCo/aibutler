package dashboard

import (
	"context"
	"database/sql"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Mission row, dashboard-projection. Mirrors internal/mission.Mission JSON
// shape but defined locally to keep the dashboard package free of an
// upward dependency on the mission package (read-only viewer).
type missionRow struct {
	ID                string  `json:"id"`
	Goal              string  `json:"goal"`
	State             string  `json:"state"`
	BudgetUSD         float64 `json:"budget_usd"`
	CostSoFarUSD      float64 `json:"cost_so_far_usd"`
	SupervisorAgentID string  `json:"supervisor_agent_id,omitempty"`
	StepCount         int     `json:"step_count"`
	EventCount        int     `json:"event_count"`
	CreatedAt         string  `json:"created_at"`
	StartedAt         string  `json:"started_at,omitempty"`
	CompletedAt       string  `json:"completed_at,omitempty"`
	AgeSeconds        int64   `json:"age_seconds"`
}

type missionStepRow struct {
	ID               string `json:"id"`
	Task             string `json:"task"`
	State            string `json:"state"`
	AssignedWorkerID string `json:"assigned_worker_id,omitempty"`
	Output           string `json:"output,omitempty"`
	Error            string `json:"error,omitempty"`
	CreatedAt        string `json:"created_at"`
	StartedAt        string `json:"started_at,omitempty"`
	CompletedAt      string `json:"completed_at,omitempty"`
}

type missionEventRow struct {
	ID          int64  `json:"id"`
	Timestamp   string `json:"timestamp"`
	Type        string `json:"event_type"`
	PayloadJSON string `json:"payload_json,omitempty"`
}

type missionDetail struct {
	Mission missionRow        `json:"mission"`
	Steps   []missionStepRow  `json:"steps"`
	Events  []missionEventRow `json:"events"`
}

type missionStats struct {
	Total         int     `json:"total"`
	Active        int     `json:"active"` // running or waiting_user
	Completed     int     `json:"completed"`
	Failed        int     `json:"failed"`
	Cancelled     int     `json:"cancelled"`
	TotalCostUSD  float64 `json:"total_cost_usd"`
	ActiveCostUSD float64 `json:"active_cost_usd"`
}

// RegisterMissionRoutes registers the mission dashboard endpoints.
//
// All endpoints are read-only — the dashboard is a viewer; mission state
// changes happen via the agent-facing mission.* tools or, in future
// commits, the supervisor agent.
func (d *Dashboard) RegisterMissionRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/dashboard/missions", d.handleMissions)
	mux.HandleFunc("/api/dashboard/missions/stats", d.handleMissionStats)
	mux.HandleFunc("/api/dashboard/missions/", d.handleMissionByPath) // /missions/{id} and /missions/{id}/events
}

// handleMissions — list missions with filters.
//
//   GET /api/dashboard/missions?state=running&limit=20&include_done=true
func (d *Dashboard) handleMissions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	ctx := r.Context()
	q := r.URL.Query()

	state := q.Get("state")
	supervisor := q.Get("supervisor")
	includeDone := q.Get("include_done") == "true" || q.Get("include_done") == "1"

	limit := 50
	if l := q.Get("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil && n > 0 && n <= 500 {
			limit = n
		}
	}

	sqlQ := `SELECT id, goal, state, budget_usd, cost_so_far_usd,
	                supervisor_agent_id, created_at, started_at, completed_at,
	                (SELECT COUNT(*) FROM mission_steps WHERE mission_id = missions.id) AS step_count,
	                (SELECT COUNT(*) FROM mission_events WHERE mission_id = missions.id) AS event_count
	         FROM missions WHERE 1=1`
	args := []interface{}{}
	if state != "" {
		sqlQ += " AND state = ?"
		args = append(args, state)
	}
	if supervisor != "" {
		sqlQ += " AND supervisor_agent_id = ?"
		args = append(args, supervisor)
	}
	if !includeDone {
		sqlQ += " AND state NOT IN ('completed','failed','cancelled')"
	}
	sqlQ += " ORDER BY created_at DESC LIMIT ?"
	args = append(args, limit)

	rows, err := d.db.QueryContext(ctx, sqlQ, args...)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query missions: "+err.Error())
		return
	}
	defer rows.Close()

	out := make([]missionRow, 0)
	for rows.Next() {
		m, err := scanMissionRow(rows)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "scan: "+err.Error())
			return
		}
		out = append(out, m)
	}
	writeJSON(w, http.StatusOK, out)
}

// handleMissionStats — quick numbers for the panel header.
//
//   GET /api/dashboard/missions/stats
func (d *Dashboard) handleMissionStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	ctx := r.Context()

	var stats missionStats
	row := d.db.QueryRowContext(ctx, `
SELECT
  COUNT(*),
  COALESCE(SUM(CASE WHEN state IN ('running','waiting_user') THEN 1 ELSE 0 END), 0),
  COALESCE(SUM(CASE WHEN state = 'completed' THEN 1 ELSE 0 END), 0),
  COALESCE(SUM(CASE WHEN state = 'failed'    THEN 1 ELSE 0 END), 0),
  COALESCE(SUM(CASE WHEN state = 'cancelled' THEN 1 ELSE 0 END), 0),
  COALESCE(SUM(cost_so_far_usd), 0),
  COALESCE(SUM(CASE WHEN state IN ('running','waiting_user') THEN cost_so_far_usd ELSE 0 END), 0)
FROM missions`)
	if err := row.Scan(
		&stats.Total, &stats.Active, &stats.Completed, &stats.Failed, &stats.Cancelled,
		&stats.TotalCostUSD, &stats.ActiveCostUSD,
	); err != nil {
		writeError(w, http.StatusInternalServerError, "stats: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, stats)
}

// handleMissionByPath dispatches /api/dashboard/missions/{id} and
// /api/dashboard/missions/{id}/events.
func (d *Dashboard) handleMissionByPath(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	rest := strings.TrimPrefix(r.URL.Path, "/api/dashboard/missions/")
	if rest == "" {
		writeError(w, http.StatusBadRequest, "mission id required")
		return
	}

	parts := strings.SplitN(rest, "/", 2)
	missionID := parts[0]
	subRoute := ""
	if len(parts) == 2 {
		subRoute = parts[1]
	}

	switch subRoute {
	case "":
		d.serveMissionDetail(w, r, missionID)
	case "events":
		d.serveMissionEvents(w, r, missionID)
	default:
		writeError(w, http.StatusNotFound, "unknown mission sub-route: "+subRoute)
	}
}

func (d *Dashboard) serveMissionDetail(w http.ResponseWriter, r *http.Request, id string) {
	ctx := r.Context()

	missionRow, err := d.queryMissionRow(ctx, id)
	if err != nil {
		if err == sql.ErrNoRows {
			writeError(w, http.StatusNotFound, "mission not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "mission: "+err.Error())
		return
	}

	steps, err := d.queryMissionSteps(ctx, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "steps: "+err.Error())
		return
	}

	eventLimit := 100
	if l := r.URL.Query().Get("event_limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil && n > 0 && n <= 1000 {
			eventLimit = n
		}
	}
	events, err := d.queryMissionEvents(ctx, id, eventLimit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "events: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, missionDetail{
		Mission: missionRow,
		Steps:   steps,
		Events:  events,
	})
}

func (d *Dashboard) serveMissionEvents(w http.ResponseWriter, r *http.Request, id string) {
	ctx := r.Context()

	limit := 100
	if l := r.URL.Query().Get("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil && n > 0 && n <= 1000 {
			limit = n
		}
	}

	events, err := d.queryMissionEvents(ctx, id, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "events: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, events)
}

// --- query helpers ---

func (d *Dashboard) queryMissionRow(ctx context.Context, id string) (missionRow, error) {
	row := d.db.QueryRowContext(ctx,
		`SELECT id, goal, state, budget_usd, cost_so_far_usd,
		        supervisor_agent_id, created_at, started_at, completed_at,
		        (SELECT COUNT(*) FROM mission_steps WHERE mission_id = missions.id) AS step_count,
		        (SELECT COUNT(*) FROM mission_events WHERE mission_id = missions.id) AS event_count
		   FROM missions WHERE id = ?`, id,
	)
	return scanMissionRow(row)
}

func (d *Dashboard) queryMissionSteps(ctx context.Context, missionID string) ([]missionStepRow, error) {
	rows, err := d.db.QueryContext(ctx,
		`SELECT id, task, state, assigned_worker_id, output, error,
		        created_at, started_at, completed_at
		   FROM mission_steps WHERE mission_id = ?
		   ORDER BY created_at ASC, id ASC`, missionID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]missionStepRow, 0)
	for rows.Next() {
		var s missionStepRow
		var assigned, output, errStr sql.NullString
		var createdAt time.Time
		var startedAt, completedAt sql.NullTime
		if err := rows.Scan(&s.ID, &s.Task, &s.State, &assigned, &output, &errStr,
			&createdAt, &startedAt, &completedAt); err != nil {
			return nil, err
		}
		s.AssignedWorkerID = assigned.String
		s.Output = output.String
		s.Error = errStr.String
		s.CreatedAt = createdAt.UTC().Format(time.RFC3339)
		if startedAt.Valid {
			s.StartedAt = startedAt.Time.UTC().Format(time.RFC3339)
		}
		if completedAt.Valid {
			s.CompletedAt = completedAt.Time.UTC().Format(time.RFC3339)
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func (d *Dashboard) queryMissionEvents(ctx context.Context, missionID string, limit int) ([]missionEventRow, error) {
	rows, err := d.db.QueryContext(ctx,
		`SELECT id, timestamp, event_type, payload_json
		   FROM mission_events WHERE mission_id = ?
		   ORDER BY timestamp ASC, id ASC LIMIT ?`, missionID, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]missionEventRow, 0)
	for rows.Next() {
		var e missionEventRow
		var ts time.Time
		var payload sql.NullString
		if err := rows.Scan(&e.ID, &ts, &e.Type, &payload); err != nil {
			return nil, err
		}
		e.Timestamp = ts.UTC().Format(time.RFC3339)
		e.PayloadJSON = payload.String
		out = append(out, e)
	}
	return out, rows.Err()
}

// scanRow narrows the *sql.Row / *sql.Rows interface to what scanMissionRow needs.
type scanRow interface {
	Scan(dest ...interface{}) error
}

// scanMissionRow scans a missions row (with the two computed COUNT columns
// at the end) into a missionRow.
func scanMissionRow(s scanRow) (missionRow, error) {
	var m missionRow
	var supervisor sql.NullString
	var createdAt time.Time
	var startedAt, completedAt sql.NullTime
	if err := s.Scan(
		&m.ID, &m.Goal, &m.State, &m.BudgetUSD, &m.CostSoFarUSD,
		&supervisor, &createdAt, &startedAt, &completedAt,
		&m.StepCount, &m.EventCount,
	); err != nil {
		return missionRow{}, err
	}
	m.SupervisorAgentID = supervisor.String
	m.CreatedAt = createdAt.UTC().Format(time.RFC3339)
	if startedAt.Valid {
		m.StartedAt = startedAt.Time.UTC().Format(time.RFC3339)
	}
	if completedAt.Valid {
		m.CompletedAt = completedAt.Time.UTC().Format(time.RFC3339)
	}
	// Age = wall-clock seconds since CreatedAt.
	m.AgeSeconds = int64(time.Since(createdAt).Seconds())
	if m.AgeSeconds < 0 {
		m.AgeSeconds = 0
	}
	return m, nil
}
