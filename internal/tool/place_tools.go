package tool

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
)

// --- place.save ---

type placeSaveTool struct{ db *sql.DB }

func (t *placeSaveTool) Name() string        { return "place.save" }
func (t *placeSaveTool) Description() string  { return "Save a place/location" }
func (t *placeSaveTool) Capability() string   { return "data.places.write" }
func (t *placeSaveTool) Schema() string {
	return `{"type":"object","properties":{"name":{"type":"string"},"address":{"type":"string"},"lat":{"type":"number"},"lon":{"type":"number"},"category":{"type":"string"},"notes":{"type":"string"},"rating":{"type":"integer","minimum":1,"maximum":5}},"required":["name"]}`
}

func (t *placeSaveTool) Execute(ctx context.Context, input string) (string, error) {
	var args struct {
		Name     string   `json:"name"`
		Address  string   `json:"address"`
		Lat      *float64 `json:"lat"`
		Lon      *float64 `json:"lon"`
		Category string   `json:"category"`
		Notes    string   `json:"notes"`
		Rating   *int     `json:"rating"`
	}
	if err := json.Unmarshal([]byte(input), &args); err != nil {
		return "", fmt.Errorf("place.save: invalid input: %w", err)
	}
	if args.Name == "" {
		return "", fmt.Errorf("place.save: name is required")
	}

	result, err := t.db.ExecContext(ctx,
		`INSERT INTO user_places (name, address, lat, lon, category, notes, rating) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		args.Name, args.Address, args.Lat, args.Lon, args.Category, args.Notes, args.Rating)
	if err != nil {
		return "", fmt.Errorf("place.save: %w", err)
	}
	id, _ := result.LastInsertId()
	return fmt.Sprintf("Place saved: %s (id: %d)", args.Name, id), nil
}

// --- place.search ---

type placeSearchTool struct{ db *sql.DB }

func (t *placeSearchTool) Name() string        { return "place.search" }
func (t *placeSearchTool) Description() string  { return "Search saved places by name or category" }
func (t *placeSearchTool) Capability() string   { return "data.places.read" }
func (t *placeSearchTool) Schema() string {
	return `{"type":"object","properties":{"query":{"type":"string"},"category":{"type":"string"}}}`
}

func (t *placeSearchTool) Execute(ctx context.Context, input string) (string, error) {
	var args struct {
		Query    string `json:"query"`
		Category string `json:"category"`
	}
	if input != "" {
		_ = json.Unmarshal([]byte(input), &args)
	}

	query := "SELECT id, name, address, lat, lon, category, notes, rating FROM user_places WHERE 1=1"
	var params []interface{}
	if args.Query != "" {
		query += " AND (name LIKE ? OR address LIKE ?)"
		params = append(params, "%"+args.Query+"%", "%"+args.Query+"%")
	}
	if args.Category != "" {
		query += " AND category = ?"
		params = append(params, args.Category)
	}
	query += " ORDER BY name ASC LIMIT 50"

	rows, err := t.db.QueryContext(ctx, query, params...)
	if err != nil {
		return "", fmt.Errorf("place.search: %w", err)
	}
	defer rows.Close()

	type place struct {
		ID       int      `json:"id"`
		Name     string   `json:"name"`
		Address  *string  `json:"address,omitempty"`
		Lat      *float64 `json:"lat,omitempty"`
		Lon      *float64 `json:"lon,omitempty"`
		Category *string  `json:"category,omitempty"`
		Notes    *string  `json:"notes,omitempty"`
		Rating   *int     `json:"rating,omitempty"`
	}

	var places []place
	for rows.Next() {
		var p place
		if err := rows.Scan(&p.ID, &p.Name, &p.Address, &p.Lat, &p.Lon, &p.Category, &p.Notes, &p.Rating); err != nil {
			return "", fmt.Errorf("place.search: scan: %w", err)
		}
		places = append(places, p)
	}
	if err := rows.Err(); err != nil {
		return "", fmt.Errorf("place.search: rows: %w", err)
	}

	out, _ := json.Marshal(places)
	return string(out), nil
}

// --- place.update ---

type placeUpdateTool struct{ db *sql.DB }

func (t *placeUpdateTool) Name() string        { return "place.update" }
func (t *placeUpdateTool) Description() string  { return "Update a saved place by ID" }
func (t *placeUpdateTool) Capability() string   { return "data.places.write" }
func (t *placeUpdateTool) Schema() string {
	return `{"type":"object","properties":{"id":{"type":"integer"},"name":{"type":"string"},"address":{"type":"string"},"category":{"type":"string"},"notes":{"type":"string"},"rating":{"type":"integer","minimum":1,"maximum":5}},"required":["id"]}`
}

func (t *placeUpdateTool) Execute(ctx context.Context, input string) (string, error) {
	var args struct {
		ID       int     `json:"id"`
		Name     *string `json:"name"`
		Address  *string `json:"address"`
		Category *string `json:"category"`
		Notes    *string `json:"notes"`
		Rating   *int    `json:"rating"`
	}
	if err := json.Unmarshal([]byte(input), &args); err != nil {
		return "", fmt.Errorf("place.update: invalid input: %w", err)
	}

	sets := []string{}
	params := []interface{}{}
	if args.Name != nil {
		sets = append(sets, "name = ?")
		params = append(params, *args.Name)
	}
	if args.Address != nil {
		sets = append(sets, "address = ?")
		params = append(params, *args.Address)
	}
	if args.Category != nil {
		sets = append(sets, "category = ?")
		params = append(params, *args.Category)
	}
	if args.Notes != nil {
		sets = append(sets, "notes = ?")
		params = append(params, *args.Notes)
	}
	if args.Rating != nil {
		sets = append(sets, "rating = ?")
		params = append(params, *args.Rating)
	}

	if len(sets) == 0 {
		return "No fields to update", nil
	}

	query := "UPDATE user_places SET "
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
		return "", fmt.Errorf("place.update: %w", err)
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return "Place not found", nil
	}
	return fmt.Sprintf("Place %d updated", args.ID), nil
}

// --- place.delete ---

type placeDeleteTool struct{ db *sql.DB }

func (t *placeDeleteTool) Name() string        { return "place.delete" }
func (t *placeDeleteTool) Description() string  { return "Delete a saved place by ID" }
func (t *placeDeleteTool) Capability() string   { return "data.places.write" }
func (t *placeDeleteTool) Schema() string {
	return `{"type":"object","properties":{"id":{"type":"integer"}},"required":["id"]}`
}

func (t *placeDeleteTool) Execute(ctx context.Context, input string) (string, error) {
	var args struct {
		ID int `json:"id"`
	}
	if err := json.Unmarshal([]byte(input), &args); err != nil {
		return "", fmt.Errorf("place.delete: invalid input: %w", err)
	}

	result, err := t.db.ExecContext(ctx, `DELETE FROM user_places WHERE id = ?`, args.ID)
	if err != nil {
		return "", fmt.Errorf("place.delete: %w", err)
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return "Place not found", nil
	}
	return fmt.Sprintf("Place %d deleted", args.ID), nil
}
