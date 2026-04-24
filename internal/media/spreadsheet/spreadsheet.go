// Package spreadsheet provides CSV read/write tools using stdlib only.
package spreadsheet

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
)

// toolRegistry is the narrow interface for registering tools (avoids import cycles).
type toolRegistry interface {
	Register(name, description, schema, capability string, exec func(ctx context.Context, input string) (string, error))
}

// ReadCSV reads a CSV file and returns all rows as a 2D string slice.
func ReadCSV(_ context.Context, path string) ([][]string, error) {
	f, err := os.Open(path) //nolint:gosec
	if err != nil {
		return nil, fmt.Errorf("spreadsheet: open %q: %w", path, err)
	}
	defer f.Close()

	r := csv.NewReader(f)
	rows, err := r.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("spreadsheet: read %q: %w", path, err)
	}
	return rows, nil
}

// WriteCSV writes rows to a CSV file, creating or overwriting it.
func WriteCSV(_ context.Context, path string, rows [][]string) error {
	f, err := os.Create(path) //nolint:gosec
	if err != nil {
		return fmt.Errorf("spreadsheet: create %q: %w", path, err)
	}
	defer f.Close()

	w := csv.NewWriter(f)
	if err := w.WriteAll(rows); err != nil {
		return fmt.Errorf("spreadsheet: write %q: %w", path, err)
	}
	w.Flush()
	return w.Error()
}

// RegisterSpreadsheetTools registers media.spreadsheet.read and media.spreadsheet.write.
func RegisterSpreadsheetTools(registry toolRegistry) {
	registry.Register(
		"media.spreadsheet.read",
		"Read a CSV file and return its contents as a JSON array of rows.",
		`{"type":"object","properties":{"path":{"type":"string","description":"Path to the CSV file"}},"required":["path"]}`,
		"tool.file.read",
		func(ctx context.Context, input string) (string, error) {
			var args struct {
				Path string `json:"path"`
			}
			if err := json.Unmarshal([]byte(input), &args); err != nil {
				return "", fmt.Errorf("media.spreadsheet.read: invalid input: %w", err)
			}
			if args.Path == "" {
				return "", fmt.Errorf("media.spreadsheet.read: path is required")
			}
			rows, err := ReadCSV(ctx, args.Path)
			if err != nil {
				return "", err
			}
			out, _ := json.Marshal(map[string]interface{}{
				"rows":  rows,
				"count": len(rows),
			})
			return string(out), nil
		},
	)

	registry.Register(
		"media.spreadsheet.write",
		"Write rows to a CSV file.",
		`{"type":"object","properties":{"path":{"type":"string","description":"Destination file path"},"rows":{"type":"array","items":{"type":"array","items":{"type":"string"}},"description":"2D array of strings (rows)"}},"required":["path","rows"]}`,
		"tool.file.write",
		func(ctx context.Context, input string) (string, error) {
			var args struct {
				Path string     `json:"path"`
				Rows [][]string `json:"rows"`
			}
			if err := json.Unmarshal([]byte(input), &args); err != nil {
				return "", fmt.Errorf("media.spreadsheet.write: invalid input: %w", err)
			}
			if args.Path == "" {
				return "", fmt.Errorf("media.spreadsheet.write: path is required")
			}
			if err := WriteCSV(ctx, args.Path, args.Rows); err != nil {
				return "", err
			}
			return fmt.Sprintf(`{"status":"written","path":%q,"rows":%d}`, args.Path, len(args.Rows)), nil
		},
	)
}
