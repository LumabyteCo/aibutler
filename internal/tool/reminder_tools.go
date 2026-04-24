package tool

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

// --- reminder.set ---

type reminderSetTool struct{ db *sql.DB }

func (t *reminderSetTool) Name() string        { return "reminder.set" }
func (t *reminderSetTool) Description() string  { return "Set a reminder with a message and time" }
func (t *reminderSetTool) Capability() string   { return "data.reminders.write" }
func (t *reminderSetTool) Schema() string {
	return `{"type":"object","properties":{"message":{"type":"string"},"remind_at":{"type":"string","description":"ISO 8601 datetime"},"recurrence":{"type":"string","description":"cron expression for recurring reminders"},"channel":{"type":"string"}},"required":["message","remind_at"]}`
}

func (t *reminderSetTool) Execute(ctx context.Context, input string) (string, error) {
	var args struct {
		Message    string `json:"message"`
		RemindAt   string `json:"remind_at"`
		Recurrence string `json:"recurrence"`
		Channel    string `json:"channel"`
	}
	if err := json.Unmarshal([]byte(input), &args); err != nil {
		return "", fmt.Errorf("reminder.set: invalid input: %w", err)
	}
	if args.Message == "" {
		return "", fmt.Errorf("reminder.set: message is required")
	}

	// Validate remind_at is parseable.
	if _, err := time.Parse(time.RFC3339, args.RemindAt); err != nil {
		// Try simpler formats.
		if _, err2 := time.Parse("2006-01-02 15:04", args.RemindAt); err2 != nil {
			return "", fmt.Errorf("reminder.set: invalid remind_at format (use ISO 8601): %w", err)
		}
	}

	result, err := t.db.ExecContext(ctx,
		`INSERT INTO user_reminders (message, remind_at, recurrence, channel) VALUES (?, ?, ?, ?)`,
		args.Message, args.RemindAt, args.Recurrence, args.Channel)
	if err != nil {
		return "", fmt.Errorf("reminder.set: %w", err)
	}
	id, _ := result.LastInsertId()

	out := fmt.Sprintf("Reminder set (id: %d) for %s", id, args.RemindAt)
	if args.Recurrence != "" {
		out += fmt.Sprintf(" (recurring: %s)", args.Recurrence)
	}
	return out, nil
}

// --- reminder.list ---

type reminderListTool struct{ db *sql.DB }

func (t *reminderListTool) Name() string        { return "reminder.list" }
func (t *reminderListTool) Description() string  { return "List active reminders" }
func (t *reminderListTool) Capability() string   { return "data.reminders.read" }
func (t *reminderListTool) Schema() string {
	return `{"type":"object","properties":{"status":{"type":"string","description":"Filter by status: active, fired, cancelled"}}}`
}

func (t *reminderListTool) Execute(ctx context.Context, input string) (string, error) {
	var args struct {
		Status string `json:"status"`
	}
	if input != "" {
		_ = json.Unmarshal([]byte(input), &args)
	}
	if args.Status == "" {
		args.Status = "active"
	}

	rows, err := t.db.QueryContext(ctx,
		`SELECT id, message, remind_at, recurrence, channel, status FROM user_reminders WHERE status = ? ORDER BY remind_at ASC LIMIT 50`,
		args.Status)
	if err != nil {
		return "", fmt.Errorf("reminder.list: %w", err)
	}
	defer rows.Close()

	type reminder struct {
		ID         int     `json:"id"`
		Message    string  `json:"message"`
		RemindAt   string  `json:"remind_at"`
		Recurrence *string `json:"recurrence,omitempty"`
		Channel    *string `json:"channel,omitempty"`
		Status     string  `json:"status"`
	}

	var reminders []reminder
	for rows.Next() {
		var r reminder
		if err := rows.Scan(&r.ID, &r.Message, &r.RemindAt, &r.Recurrence, &r.Channel, &r.Status); err != nil {
			return "", fmt.Errorf("reminder.list: scan: %w", err)
		}
		reminders = append(reminders, r)
	}

	out, _ := json.Marshal(reminders)
	return string(out), nil
}

// --- reminder.cancel ---

type reminderCancelTool struct{ db *sql.DB }

func (t *reminderCancelTool) Name() string        { return "reminder.cancel" }
func (t *reminderCancelTool) Description() string  { return "Cancel a reminder by ID" }
func (t *reminderCancelTool) Capability() string   { return "data.reminders.write" }
func (t *reminderCancelTool) Schema() string {
	return `{"type":"object","properties":{"id":{"type":"integer"}},"required":["id"]}`
}

func (t *reminderCancelTool) Execute(ctx context.Context, input string) (string, error) {
	var args struct {
		ID int `json:"id"`
	}
	if err := json.Unmarshal([]byte(input), &args); err != nil {
		return "", fmt.Errorf("reminder.cancel: invalid input: %w", err)
	}

	result, err := t.db.ExecContext(ctx,
		`UPDATE user_reminders SET status = 'cancelled' WHERE id = ? AND status = 'active'`,
		args.ID)
	if err != nil {
		return "", fmt.Errorf("reminder.cancel: %w", err)
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return "Reminder not found or already cancelled", nil
	}
	return fmt.Sprintf("Reminder %d cancelled", args.ID), nil
}
