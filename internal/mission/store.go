package mission

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// Store is the persistence interface for missions, steps, and events.
type Store interface {
	// Mission CRUD.
	CreateMission(ctx context.Context, m Mission) error
	UpdateMission(ctx context.Context, m Mission) error
	GetMission(ctx context.Context, id string) (Mission, error)
	ListMissions(ctx context.Context, f ListFilter) ([]Mission, error)

	// Step CRUD.
	AddStep(ctx context.Context, s Step) error
	UpdateStep(ctx context.Context, s Step) error
	GetSteps(ctx context.Context, missionID string) ([]Step, error)

	// Event log.
	AppendEvent(ctx context.Context, e Event) error
	GetEvents(ctx context.Context, missionID string, limit int) ([]Event, error)
}

// ListFilter selects which missions ListMissions returns.
type ListFilter struct {
	State       State  // empty = any
	Supervisor  string // empty = any
	Limit       int    // default 50, max 500
	IncludeDone bool   // false = exclude terminal states
}

// SQLiteStore implements Store on top of the missions / mission_steps /
// mission_events tables created by migration 021.
type SQLiteStore struct {
	db *sql.DB
}

// NewSQLiteStore creates a SQLiteStore.
func NewSQLiteStore(db *sql.DB) *SQLiteStore {
	return &SQLiteStore{db: db}
}

// --- Mission CRUD ---

func (s *SQLiteStore) CreateMission(ctx context.Context, m Mission) error {
	if m.ID == "" {
		return errors.New("mission: ID is required")
	}
	if m.Goal == "" {
		return ErrEmptyGoal
	}
	if m.CreatedAt.IsZero() {
		m.CreatedAt = time.Now()
	}
	if m.State == "" {
		m.State = StateCreated
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO missions
		   (id, goal, state, plan_json, budget_usd, cost_so_far_usd,
		    supervisor_agent_id, created_at, started_at, completed_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		m.ID, m.Goal, string(m.State), m.PlanJSON, m.BudgetUSD,
		m.CostSoFarUSD, m.SupervisorAgentID, m.CreatedAt,
		nullTime(m.StartedAt), nullTime(m.CompletedAt),
	)
	if err != nil {
		return fmt.Errorf("mission.create: %w", err)
	}
	return nil
}

func (s *SQLiteStore) UpdateMission(ctx context.Context, m Mission) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE missions
		    SET goal = ?, state = ?, plan_json = ?, budget_usd = ?,
		        cost_so_far_usd = ?, supervisor_agent_id = ?,
		        started_at = ?, completed_at = ?
		  WHERE id = ?`,
		m.Goal, string(m.State), m.PlanJSON, m.BudgetUSD,
		m.CostSoFarUSD, m.SupervisorAgentID,
		nullTime(m.StartedAt), nullTime(m.CompletedAt),
		m.ID,
	)
	if err != nil {
		return fmt.Errorf("mission.update: %w", err)
	}
	rows, _ := res.RowsAffected()
	if rows == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *SQLiteStore) GetMission(ctx context.Context, id string) (Mission, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, goal, state, plan_json, budget_usd, cost_so_far_usd,
		        supervisor_agent_id, created_at, started_at, completed_at
		   FROM missions WHERE id = ?`, id,
	)
	return scanMission(row.Scan)
}

func (s *SQLiteStore) ListMissions(ctx context.Context, f ListFilter) ([]Mission, error) {
	limit := f.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > 500 {
		limit = 500
	}

	q := `SELECT id, goal, state, plan_json, budget_usd, cost_so_far_usd,
	             supervisor_agent_id, created_at, started_at, completed_at
	      FROM missions
	      WHERE 1=1`
	args := []interface{}{}
	if f.State != "" {
		q += " AND state = ?"
		args = append(args, string(f.State))
	}
	if f.Supervisor != "" {
		q += " AND supervisor_agent_id = ?"
		args = append(args, f.Supervisor)
	}
	if !f.IncludeDone {
		q += " AND state NOT IN ('completed','failed','cancelled')"
	}
	q += " ORDER BY created_at DESC, id DESC LIMIT ?"
	args = append(args, limit)

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("mission.list: %w", err)
	}
	defer rows.Close()

	var out []Mission
	for rows.Next() {
		m, err := scanMission(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// --- Step CRUD ---

func (s *SQLiteStore) AddStep(ctx context.Context, st Step) error {
	if st.ID == "" || st.MissionID == "" {
		return errors.New("mission.step: ID and MissionID are required")
	}
	if st.CreatedAt.IsZero() {
		st.CreatedAt = time.Now()
	}
	if st.State == "" {
		st.State = StateCreated
	}
	depJSON, _ := json.Marshal(st.DependsOn)
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO mission_steps
		   (id, mission_id, task, depends_on_json, assigned_worker_id,
		    state, output, error, created_at, started_at, completed_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		st.ID, st.MissionID, st.Task, string(depJSON), st.AssignedWorkerID,
		string(st.State), st.Output, st.Error, st.CreatedAt,
		nullTime(st.StartedAt), nullTime(st.CompletedAt),
	)
	if err != nil {
		return fmt.Errorf("mission.step.add: %w", err)
	}
	return nil
}

func (s *SQLiteStore) UpdateStep(ctx context.Context, st Step) error {
	depJSON, _ := json.Marshal(st.DependsOn)
	res, err := s.db.ExecContext(ctx,
		`UPDATE mission_steps
		    SET task = ?, depends_on_json = ?, assigned_worker_id = ?,
		        state = ?, output = ?, error = ?,
		        started_at = ?, completed_at = ?
		  WHERE id = ?`,
		st.Task, string(depJSON), st.AssignedWorkerID,
		string(st.State), st.Output, st.Error,
		nullTime(st.StartedAt), nullTime(st.CompletedAt),
		st.ID,
	)
	if err != nil {
		return fmt.Errorf("mission.step.update: %w", err)
	}
	rows, _ := res.RowsAffected()
	if rows == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *SQLiteStore) GetSteps(ctx context.Context, missionID string) ([]Step, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, mission_id, task, depends_on_json, assigned_worker_id,
		        state, output, error, created_at, started_at, completed_at
		   FROM mission_steps
		  WHERE mission_id = ?
		  ORDER BY created_at ASC, id ASC`, missionID,
	)
	if err != nil {
		return nil, fmt.Errorf("mission.step.list: %w", err)
	}
	defer rows.Close()

	var out []Step
	for rows.Next() {
		var st Step
		var depJSON sql.NullString
		var worker, output, errText sql.NullString
		var stateStr string
		var startedAt, completedAt sql.NullTime
		if err := rows.Scan(
			&st.ID, &st.MissionID, &st.Task, &depJSON, &worker,
			&stateStr, &output, &errText, &st.CreatedAt, &startedAt, &completedAt,
		); err != nil {
			return nil, err
		}
		st.AssignedWorkerID = worker.String
		st.State = State(stateStr)
		st.Output = output.String
		st.Error = errText.String
		if startedAt.Valid {
			t := startedAt.Time
			st.StartedAt = &t
		}
		if completedAt.Valid {
			t := completedAt.Time
			st.CompletedAt = &t
		}
		if depJSON.Valid && depJSON.String != "" && depJSON.String != "null" {
			_ = json.Unmarshal([]byte(depJSON.String), &st.DependsOn)
		}
		out = append(out, st)
	}
	return out, rows.Err()
}

// --- Event log ---

func (s *SQLiteStore) AppendEvent(ctx context.Context, e Event) error {
	if e.MissionID == "" || e.Type == "" {
		return errors.New("mission.event: MissionID and Type are required")
	}
	if e.Timestamp.IsZero() {
		e.Timestamp = time.Now()
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO mission_events (mission_id, timestamp, event_type, payload_json)
		 VALUES (?, ?, ?, ?)`,
		e.MissionID, e.Timestamp, e.Type, e.PayloadJSON,
	)
	if err != nil {
		return fmt.Errorf("mission.event.append: %w", err)
	}
	return nil
}

func (s *SQLiteStore) GetEvents(ctx context.Context, missionID string, limit int) ([]Event, error) {
	if limit <= 0 {
		limit = 100
	}
	if limit > 1000 {
		limit = 1000
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, mission_id, timestamp, event_type, payload_json
		   FROM mission_events
		  WHERE mission_id = ?
		  ORDER BY timestamp ASC, id ASC
		  LIMIT ?`, missionID, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("mission.event.list: %w", err)
	}
	defer rows.Close()

	var out []Event
	for rows.Next() {
		var e Event
		var payload sql.NullString
		if err := rows.Scan(&e.ID, &e.MissionID, &e.Timestamp, &e.Type, &payload); err != nil {
			return nil, err
		}
		e.PayloadJSON = payload.String
		out = append(out, e)
	}
	return out, rows.Err()
}

// --- helpers ---

func nullTime(t *time.Time) interface{} {
	if t == nil {
		return nil
	}
	return *t
}

// scanFunc abstracts row.Scan / rows.Scan so scanMission can serve both.
type scanFunc func(...interface{}) error

func scanMission(scan scanFunc) (Mission, error) {
	var m Mission
	var planJSON, supervisor sql.NullString
	var stateStr string
	var startedAt, completedAt sql.NullTime
	err := scan(&m.ID, &m.Goal, &stateStr, &planJSON, &m.BudgetUSD,
		&m.CostSoFarUSD, &supervisor, &m.CreatedAt, &startedAt, &completedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Mission{}, ErrNotFound
		}
		return Mission{}, fmt.Errorf("mission.scan: %w", err)
	}
	m.State = State(stateStr)
	m.PlanJSON = planJSON.String
	m.SupervisorAgentID = supervisor.String
	if startedAt.Valid {
		t := startedAt.Time
		m.StartedAt = &t
	}
	if completedAt.Valid {
		t := completedAt.Time
		m.CompletedAt = &t
	}
	return m, nil
}
