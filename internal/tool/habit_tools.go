package tool

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

// --- habit.create ---

type habitCreateTool struct{ db *sql.DB }

func (t *habitCreateTool) Name() string        { return "habit.create" }
func (t *habitCreateTool) Description() string  { return "Create a new habit to track" }
func (t *habitCreateTool) Capability() string   { return "data.habits.write" }
func (t *habitCreateTool) Schema() string {
	return `{"type":"object","properties":{"name":{"type":"string"},"frequency":{"type":"string","description":"daily, weekly, or custom"}},"required":["name"]}`
}

func (t *habitCreateTool) Execute(ctx context.Context, input string) (string, error) {
	var args struct {
		Name      string `json:"name"`
		Frequency string `json:"frequency"`
	}
	if err := json.Unmarshal([]byte(input), &args); err != nil {
		return "", fmt.Errorf("habit.create: invalid input: %w", err)
	}
	if args.Name == "" {
		return "", fmt.Errorf("habit.create: name is required")
	}
	if args.Frequency == "" {
		args.Frequency = "daily"
	}

	result, err := t.db.ExecContext(ctx,
		`INSERT INTO user_habits (name, frequency) VALUES (?, ?)`,
		args.Name, args.Frequency)
	if err != nil {
		return "", fmt.Errorf("habit.create: %w", err)
	}
	id, _ := result.LastInsertId()
	return fmt.Sprintf("Habit created: %s (id: %d, frequency: %s)", args.Name, id, args.Frequency), nil
}

// --- habit.log ---

type habitLogTool struct{ db *sql.DB }

func (t *habitLogTool) Name() string        { return "habit.log" }
func (t *habitLogTool) Description() string  { return "Log completion of a habit for today (or a specific date)" }
func (t *habitLogTool) Capability() string   { return "data.habits.write" }
func (t *habitLogTool) Schema() string {
	return `{"type":"object","properties":{"name":{"type":"string","description":"Habit name"},"date":{"type":"string","description":"YYYY-MM-DD, defaults to today"},"notes":{"type":"string"}},"required":["name"]}`
}

func (t *habitLogTool) Execute(ctx context.Context, input string) (string, error) {
	var args struct {
		Name  string `json:"name"`
		Date  string `json:"date"`
		Notes string `json:"notes"`
	}
	if err := json.Unmarshal([]byte(input), &args); err != nil {
		return "", fmt.Errorf("habit.log: invalid input: %w", err)
	}
	if args.Name == "" {
		return "", fmt.Errorf("habit.log: name is required")
	}
	if args.Date == "" {
		args.Date = time.Now().UTC().Format("2006-01-02")
	}

	// Look up habit by name.
	var habitID int
	err := t.db.QueryRowContext(ctx, `SELECT id FROM user_habits WHERE name = ?`, args.Name).Scan(&habitID)
	if err == sql.ErrNoRows {
		return "", fmt.Errorf("habit.log: habit %q not found", args.Name)
	}
	if err != nil {
		return "", fmt.Errorf("habit.log: %w", err)
	}

	_, err = t.db.ExecContext(ctx,
		`INSERT INTO user_habit_logs (habit_id, date, notes) VALUES (?, ?, ?)`,
		habitID, args.Date, args.Notes)
	if err != nil {
		return "", fmt.Errorf("habit.log: %w", err)
	}
	return fmt.Sprintf("Logged %s for %s", args.Name, args.Date), nil
}

// --- habit.streak ---

type habitStreakTool struct{ db *sql.DB }

func (t *habitStreakTool) Name() string        { return "habit.streak" }
func (t *habitStreakTool) Description() string  { return "Get the current streak for a habit" }
func (t *habitStreakTool) Capability() string   { return "data.habits.read" }
func (t *habitStreakTool) Schema() string {
	return `{"type":"object","properties":{"name":{"type":"string"}},"required":["name"]}`
}

func (t *habitStreakTool) Execute(ctx context.Context, input string) (string, error) {
	var args struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal([]byte(input), &args); err != nil {
		return "", fmt.Errorf("habit.streak: invalid input: %w", err)
	}

	var habitID int
	err := t.db.QueryRowContext(ctx, `SELECT id FROM user_habits WHERE name = ?`, args.Name).Scan(&habitID)
	if err == sql.ErrNoRows {
		return "", fmt.Errorf("habit.streak: habit %q not found", args.Name)
	}
	if err != nil {
		return "", fmt.Errorf("habit.streak: %w", err)
	}

	// Get all log dates in descending order.
	rows, err := t.db.QueryContext(ctx,
		`SELECT date FROM user_habit_logs WHERE habit_id = ? ORDER BY date DESC`, habitID)
	if err != nil {
		return "", fmt.Errorf("habit.streak: %w", err)
	}
	defer rows.Close()

	streak := 0
	expected := time.Now().UTC().Truncate(24 * time.Hour)

	for rows.Next() {
		var dateStr string
		if err := rows.Scan(&dateStr); err != nil {
			return "", fmt.Errorf("habit.streak: scan: %w", err)
		}
		logDate, err := time.Parse("2006-01-02", dateStr)
		if err != nil {
			continue
		}
		logDate = logDate.Truncate(24 * time.Hour)

		if logDate.Equal(expected) {
			streak++
			expected = expected.AddDate(0, 0, -1)
		} else if logDate.Before(expected) {
			// Streak broken — allow skipping today if not logged yet.
			if streak == 0 && logDate.Equal(expected.AddDate(0, 0, -1)) {
				expected = expected.AddDate(0, 0, -1)
				streak++
				expected = expected.AddDate(0, 0, -1)
			} else {
				break
			}
		}
	}

	type streakResult struct {
		Habit  string `json:"habit"`
		Streak int    `json:"streak"`
		Unit   string `json:"unit"`
	}

	out, _ := json.Marshal(streakResult{Habit: args.Name, Streak: streak, Unit: "days"})
	return string(out), nil
}
