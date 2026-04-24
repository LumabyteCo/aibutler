package tool

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

// --- contact.update ---

type contactUpdateTool struct{ db *sql.DB }

func (t *contactUpdateTool) Name() string        { return "contact.update" }
func (t *contactUpdateTool) Description() string  { return "Update a contact's information by ID" }
func (t *contactUpdateTool) Capability() string   { return "data.contacts.write" }
func (t *contactUpdateTool) Schema() string {
	return `{"type":"object","properties":{"id":{"type":"integer"},"name":{"type":"string"},"email":{"type":"string"},"phone":{"type":"string"},"relationship":{"type":"string"},"birthday":{"type":"string","description":"YYYY-MM-DD"},"notes":{"type":"string"},"preferred_channel":{"type":"string"}},"required":["id"]}`
}

func (t *contactUpdateTool) Execute(ctx context.Context, input string) (string, error) {
	var args struct {
		ID               int     `json:"id"`
		Name             *string `json:"name"`
		Email            *string `json:"email"`
		Phone            *string `json:"phone"`
		Relationship     *string `json:"relationship"`
		Birthday         *string `json:"birthday"`
		Notes            *string `json:"notes"`
		PreferredChannel *string `json:"preferred_channel"`
	}
	if err := json.Unmarshal([]byte(input), &args); err != nil {
		return "", fmt.Errorf("contact.update: invalid input: %w", err)
	}

	sets := []string{}
	params := []interface{}{}
	if args.Name != nil {
		sets = append(sets, "name = ?")
		params = append(params, *args.Name)
	}
	if args.Email != nil {
		sets = append(sets, "email = ?")
		params = append(params, *args.Email)
	}
	if args.Phone != nil {
		sets = append(sets, "phone = ?")
		params = append(params, *args.Phone)
	}
	if args.Relationship != nil {
		sets = append(sets, "relationship = ?")
		params = append(params, *args.Relationship)
	}
	if args.Birthday != nil {
		sets = append(sets, "birthday = ?")
		params = append(params, *args.Birthday)
	}
	if args.Notes != nil {
		sets = append(sets, "notes = ?")
		params = append(params, *args.Notes)
	}
	if args.PreferredChannel != nil {
		sets = append(sets, "preferred_channel = ?")
		params = append(params, *args.PreferredChannel)
	}

	if len(sets) == 0 {
		return "No fields to update", nil
	}

	query := "UPDATE user_contacts SET "
	for i, s := range sets {
		if i > 0 {
			query += ", "
		}
		query += s
	}
	query += " WHERE id = ?"
	params = append(params, args.ID)

	result, err := t.db.ExecContext(ctx, query, params...)
	if err != nil {
		return "", fmt.Errorf("contact.update: %w", err)
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return "Contact not found", nil
	}
	return fmt.Sprintf("Contact %d updated", args.ID), nil
}

// --- contact.birthdays ---

type contactBirthdaysTool struct{ db *sql.DB }

func (t *contactBirthdaysTool) Name() string        { return "contact.birthdays" }
func (t *contactBirthdaysTool) Description() string  { return "List upcoming birthdays within the next N days" }
func (t *contactBirthdaysTool) Capability() string   { return "data.contacts.read" }
func (t *contactBirthdaysTool) Schema() string {
	return `{"type":"object","properties":{"days":{"type":"integer","description":"Look ahead window in days (default 30)"}}}`
}

func (t *contactBirthdaysTool) Execute(ctx context.Context, input string) (string, error) {
	var args struct {
		Days int `json:"days"`
	}
	if input != "" {
		_ = json.Unmarshal([]byte(input), &args)
	}
	if args.Days <= 0 {
		args.Days = 30
	}

	// Get all contacts with birthdays set.
	rows, err := t.db.QueryContext(ctx,
		`SELECT id, name, birthday, relationship FROM user_contacts WHERE birthday IS NOT NULL AND birthday != '' ORDER BY name`)
	if err != nil {
		return "", fmt.Errorf("contact.birthdays: %w", err)
	}
	defer rows.Close()

	type upcoming struct {
		ID           int    `json:"id"`
		Name         string `json:"name"`
		Birthday     string `json:"birthday"`
		DaysAway     int    `json:"days_away"`
		Relationship string `json:"relationship,omitempty"`
	}

	now := time.Now().UTC()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	deadline := today.AddDate(0, 0, args.Days)

	var results []upcoming
	for rows.Next() {
		var id int
		var name, bday string
		var rel *string
		if err := rows.Scan(&id, &name, &bday, &rel); err != nil {
			return "", fmt.Errorf("contact.birthdays: scan: %w", err)
		}

		// Parse birthday (YYYY-MM-DD or MM-DD).
		var month, day int
		if len(bday) >= 10 {
			bd, err := time.Parse("2006-01-02", bday)
			if err != nil {
				continue
			}
			month = int(bd.Month())
			day = bd.Day()
		} else {
			continue
		}

		// Calculate next birthday this year.
		nextBday := time.Date(today.Year(), time.Month(month), day, 0, 0, 0, 0, time.UTC)
		if nextBday.Before(today) {
			nextBday = nextBday.AddDate(1, 0, 0)
		}

		if nextBday.Before(deadline) || nextBday.Equal(deadline) {
			u := upcoming{
				ID:       id,
				Name:     name,
				Birthday: bday,
				DaysAway: int(nextBday.Sub(today).Hours() / 24),
			}
			if rel != nil {
				u.Relationship = *rel
			}
			results = append(results, u)
		}
	}

	out, _ := json.Marshal(results)
	return string(out), nil
}
