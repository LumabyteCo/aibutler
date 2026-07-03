package schedule

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"time"
)

// Schedule represents a cron-based schedule for running agents.
type Schedule struct {
	ID        string
	Name      string
	CronExpr  string
	Task      string
	Channel   string
	AccountID string
	Skills    []string
	// Capabilities lists the capability resources this job runs with.
	// Empty = the legacy default set. A populated list means the job gets
	// exactly these resources and nothing else — background work should
	// hold only the permissions it needs.
	Capabilities []string
	Enabled      bool
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// Run represents a single execution of a schedule.
type Run struct {
	ID          int
	ScheduleID  string
	Status      string // "running", "completed", "failed"
	StartedAt   time.Time
	CompletedAt *time.Time
	AgentID     string
	Error       string
}

// Store provides CRUD operations for schedules and runs.
type Store struct {
	db *sql.DB
}

// NewStore creates a new schedule store.
func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}

// Create inserts a new schedule. Builtin task keys are reserved for
// code-registered maintenance — enforced here so EVERY creation surface
// (agent tool, MCP adapters, HTTP) inherits the rule; no caller can alias
// an agent task onto registered builtin code or schedule extra fires of it.
func (s *Store) Create(ctx context.Context, sched *Schedule) error {
	if hasBuiltinPrefix(sched.Task) {
		return fmt.Errorf("schedule.create: task names starting with %q are reserved", BuiltinPrefix)
	}
	return s.create(ctx, sched)
}

// create is the reservation-exempt insert used by the scheduler itself to
// register builtin schedules.
func (s *Store) create(ctx context.Context, sched *Schedule) error {
	skills, _ := json.Marshal(sched.Skills)
	var capsJSON interface{}
	if len(sched.Capabilities) > 0 {
		b, _ := json.Marshal(sched.Capabilities)
		capsJSON = string(b)
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO schedules (id, name, cron_expr, task, channel, account_id, skills, capabilities, enabled)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		sched.ID, sched.Name, sched.CronExpr, sched.Task, sched.Channel, sched.AccountID,
		string(skills), capsJSON, boolToInt(sched.Enabled))
	if err != nil {
		return fmt.Errorf("schedule.create: %w", err)
	}
	return nil
}

// Get retrieves a schedule by ID.
func (s *Store) Get(ctx context.Context, id string) (*Schedule, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, name, cron_expr, task, channel, account_id, skills, COALESCE(capabilities, ''), enabled, created_at, updated_at
		 FROM schedules WHERE id = ?`, id)
	return scanSchedule(row)
}

// List returns all schedules.
func (s *Store) List(ctx context.Context) ([]Schedule, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, name, cron_expr, task, channel, account_id, skills, COALESCE(capabilities, ''), enabled, created_at, updated_at
		 FROM schedules ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("schedule.list: %w", err)
	}
	defer rows.Close()

	var result []Schedule
	for rows.Next() {
		sched, err := scanScheduleRow(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, *sched)
	}
	return result, rows.Err()
}

// Delete removes a schedule by ID.
func (s *Store) Delete(ctx context.Context, id string) error {
	// Delete run history first to satisfy the FK constraint on schedule_runs.schedule_id.
	if _, err := s.db.ExecContext(ctx, `DELETE FROM schedule_runs WHERE schedule_id = ?`, id); err != nil {
		return fmt.Errorf("schedule.delete: %w", err)
	}
	res, err := s.db.ExecContext(ctx, `DELETE FROM schedules WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("schedule.delete: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("schedule.delete: not found")
	}
	return nil
}

// SetEnabled enables or disables a schedule.
func (s *Store) SetEnabled(ctx context.Context, id string, enabled bool) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE schedules SET enabled = ?, updated_at = datetime('now') WHERE id = ?`,
		boolToInt(enabled), id)
	if err != nil {
		return fmt.Errorf("schedule.setEnabled: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("schedule.setEnabled: not found")
	}
	return nil
}

// RecordRun inserts or updates a schedule run.
func (s *Store) RecordRun(ctx context.Context, run *Run) error {
	if run.ID == 0 {
		res, err := s.db.ExecContext(ctx,
			`INSERT INTO schedule_runs (schedule_id, status, started_at, agent_id)
			 VALUES (?, ?, ?, ?)`,
			run.ScheduleID, run.Status, run.StartedAt.Format(time.RFC3339), nullString(run.AgentID))
		if err != nil {
			return fmt.Errorf("schedule.recordRun: %w", err)
		}
		id, _ := res.LastInsertId()
		run.ID = int(id)
		return nil
	}
	// Update existing run
	var completedStr *string
	if run.CompletedAt != nil {
		s := run.CompletedAt.Format(time.RFC3339)
		completedStr = &s
	}
	_, err := s.db.ExecContext(ctx,
		`UPDATE schedule_runs SET status = ?, completed_at = ?, agent_id = ?, error = ? WHERE id = ?`,
		run.Status, completedStr, nullString(run.AgentID), nullString(run.Error), run.ID)
	if err != nil {
		return fmt.Errorf("schedule.recordRun: %w", err)
	}
	return nil
}

// LastRun returns the most recent run for a schedule.
func (s *Store) LastRun(ctx context.Context, scheduleID string) (*Run, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, schedule_id, status, started_at, completed_at, agent_id, error
		 FROM schedule_runs WHERE schedule_id = ? ORDER BY started_at DESC LIMIT 1`,
		scheduleID)

	var run Run
	var startedStr string
	var completedStr, agentID, errStr sql.NullString
	err := row.Scan(&run.ID, &run.ScheduleID, &run.Status, &startedStr,
		&completedStr, &agentID, &errStr)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("schedule.lastRun: %w", err)
	}
	if t, err := time.Parse(time.RFC3339, startedStr); err != nil {
		log.Printf("schedule.lastRun: parse started_at %q: %v", startedStr, err)
	} else {
		run.StartedAt = t
	}
	if completedStr.Valid {
		t, err := time.Parse(time.RFC3339, completedStr.String)
		if err != nil {
			log.Printf("schedule.lastRun: parse completed_at %q: %v", completedStr.String, err)
		} else {
			run.CompletedAt = &t
		}
	}
	run.AgentID = agentID.String
	run.Error = errStr.String
	return &run, nil
}

func scanSchedule(row *sql.Row) (*Schedule, error) {
	var sched Schedule
	var skillsJSON, capsJSON string
	var enabled int
	var createdStr, updatedStr string
	err := row.Scan(&sched.ID, &sched.Name, &sched.CronExpr, &sched.Task,
		&sched.Channel, &sched.AccountID, &skillsJSON, &capsJSON, &enabled,
		&createdStr, &updatedStr)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("schedule: not found")
	}
	if err != nil {
		return nil, fmt.Errorf("schedule.get: %w", err)
	}
	sched.Enabled = enabled != 0
	if t, err := time.Parse(time.DateTime, createdStr); err != nil {
		log.Printf("schedule.get: parse created_at %q: %v", createdStr, err)
	} else {
		sched.CreatedAt = t
	}
	if t, err := time.Parse(time.DateTime, updatedStr); err != nil {
		log.Printf("schedule.get: parse updated_at %q: %v", updatedStr, err)
	} else {
		sched.UpdatedAt = t
	}
	if skillsJSON != "" {
		json.Unmarshal([]byte(skillsJSON), &sched.Skills)
	}
	if capsJSON != "" {
		json.Unmarshal([]byte(capsJSON), &sched.Capabilities)
	}
	return &sched, nil
}

func scanScheduleRow(rows *sql.Rows) (*Schedule, error) {
	var sched Schedule
	var skillsJSON, capsJSON string
	var enabled int
	var createdStr, updatedStr string
	err := rows.Scan(&sched.ID, &sched.Name, &sched.CronExpr, &sched.Task,
		&sched.Channel, &sched.AccountID, &skillsJSON, &capsJSON, &enabled,
		&createdStr, &updatedStr)
	if err != nil {
		return nil, fmt.Errorf("schedule.scan: %w", err)
	}
	sched.Enabled = enabled != 0
	if t, err := time.Parse(time.DateTime, createdStr); err != nil {
		log.Printf("schedule.scan: parse created_at %q: %v", createdStr, err)
	} else {
		sched.CreatedAt = t
	}
	if t, err := time.Parse(time.DateTime, updatedStr); err != nil {
		log.Printf("schedule.scan: parse updated_at %q: %v", updatedStr, err)
	} else {
		sched.UpdatedAt = t
	}
	if skillsJSON != "" {
		json.Unmarshal([]byte(skillsJSON), &sched.Skills)
	}
	if capsJSON != "" {
		json.Unmarshal([]byte(capsJSON), &sched.Capabilities)
	}
	return &sched, nil
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func nullString(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
