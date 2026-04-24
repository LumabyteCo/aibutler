package tool

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
)

// --- journal.read ---

type journalReadTool struct{ db *sql.DB }

func (t *journalReadTool) Name() string        { return "journal.read" }
func (t *journalReadTool) Description() string  { return "Read journal entries by date range" }
func (t *journalReadTool) Capability() string   { return "data.journal.read" }
func (t *journalReadTool) Schema() string {
	return `{"type":"object","properties":{"from":{"type":"string","description":"Start date YYYY-MM-DD"},"to":{"type":"string","description":"End date YYYY-MM-DD"},"mood":{"type":"string","description":"Filter by mood"},"limit":{"type":"integer"}}}`
}

func (t *journalReadTool) Execute(ctx context.Context, input string) (string, error) {
	var args struct {
		From  string `json:"from"`
		To    string `json:"to"`
		Mood  string `json:"mood"`
		Limit int    `json:"limit"`
	}
	if input != "" {
		_ = json.Unmarshal([]byte(input), &args)
	}
	if args.Limit <= 0 || args.Limit > 100 {
		args.Limit = 20
	}

	query := "SELECT id, type, content, mood, date FROM user_journal WHERE 1=1"
	var params []interface{}
	if args.From != "" {
		query += " AND date >= ?"
		params = append(params, args.From)
	}
	if args.To != "" {
		query += " AND date <= ?"
		params = append(params, args.To)
	}
	if args.Mood != "" {
		query += " AND mood = ?"
		params = append(params, args.Mood)
	}
	query += " ORDER BY date DESC LIMIT ?"
	params = append(params, args.Limit)

	rows, err := t.db.QueryContext(ctx, query, params...)
	if err != nil {
		return "", fmt.Errorf("journal.read: %w", err)
	}
	defer rows.Close()

	type entry struct {
		ID      int     `json:"id"`
		Type    string  `json:"type"`
		Content string  `json:"content"`
		Mood    *string `json:"mood,omitempty"`
		Date    string  `json:"date"`
	}

	var entries []entry
	for rows.Next() {
		var e entry
		if err := rows.Scan(&e.ID, &e.Type, &e.Content, &e.Mood, &e.Date); err != nil {
			return "", fmt.Errorf("journal.read: scan: %w", err)
		}
		entries = append(entries, e)
	}
	if err := rows.Err(); err != nil {
		return "", fmt.Errorf("journal.read: rows: %w", err)
	}

	out, _ := json.Marshal(entries)
	return string(out), nil
}

// --- journal.mood_trend ---

type journalMoodTrendTool struct{ db *sql.DB }

func (t *journalMoodTrendTool) Name() string        { return "journal.mood_trend" }
func (t *journalMoodTrendTool) Description() string  { return "Get mood trends over time" }
func (t *journalMoodTrendTool) Capability() string   { return "data.journal.read" }
func (t *journalMoodTrendTool) Schema() string {
	return `{"type":"object","properties":{"from":{"type":"string","description":"Start date YYYY-MM-DD"},"to":{"type":"string","description":"End date YYYY-MM-DD"}}}`
}

func (t *journalMoodTrendTool) Execute(ctx context.Context, input string) (string, error) {
	var args struct {
		From string `json:"from"`
		To   string `json:"to"`
	}
	if input != "" {
		_ = json.Unmarshal([]byte(input), &args)
	}

	query := `SELECT mood, COUNT(*) as count FROM user_journal WHERE mood IS NOT NULL AND mood != ''`
	var params []interface{}
	if args.From != "" {
		query += " AND date >= ?"
		params = append(params, args.From)
	}
	if args.To != "" {
		query += " AND date <= ?"
		params = append(params, args.To)
	}
	query += " GROUP BY mood ORDER BY count DESC"

	rows, err := t.db.QueryContext(ctx, query, params...)
	if err != nil {
		return "", fmt.Errorf("journal.mood_trend: %w", err)
	}
	defer rows.Close()

	type moodCount struct {
		Mood  string `json:"mood"`
		Count int    `json:"count"`
	}

	var trends []moodCount
	for rows.Next() {
		var m moodCount
		if err := rows.Scan(&m.Mood, &m.Count); err != nil {
			return "", fmt.Errorf("journal.mood_trend: scan: %w", err)
		}
		trends = append(trends, m)
	}
	if err := rows.Err(); err != nil {
		return "", fmt.Errorf("journal.mood_trend: rows: %w", err)
	}

	out, _ := json.Marshal(trends)
	return string(out), nil
}
