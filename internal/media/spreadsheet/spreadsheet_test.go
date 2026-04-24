package spreadsheet_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/LumabyteCo/aibutler/internal/media/spreadsheet"
)

type mockRegistry struct {
	tools []string
	exec  map[string]func(ctx context.Context, input string) (string, error)
}

func newMockRegistry() *mockRegistry {
	return &mockRegistry{exec: make(map[string]func(ctx context.Context, input string) (string, error))}
}

func (m *mockRegistry) Register(name, description, schema, capability string, exec func(ctx context.Context, input string) (string, error)) {
	m.tools = append(m.tools, name)
	m.exec[name] = exec
}

func TestReadCSV(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.csv")
	os.WriteFile(path, []byte("a,b,c\n1,2,3\n4,5,6\n"), 0600)

	rows, err := spreadsheet.ReadCSV(context.Background(), path)
	if err != nil {
		t.Fatalf("ReadCSV: unexpected error: %v", err)
	}
	if len(rows) != 3 {
		t.Errorf("expected 3 rows, got %d", len(rows))
	}
	if rows[0][0] != "a" {
		t.Errorf("expected first cell 'a', got %q", rows[0][0])
	}
}

func TestWriteCSV_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.csv")

	rows := [][]string{
		{"name", "age"},
		{"Alice", "30"},
		{"Bob", "25"},
	}
	if err := spreadsheet.WriteCSV(context.Background(), path, rows); err != nil {
		t.Fatalf("WriteCSV: unexpected error: %v", err)
	}

	got, err := spreadsheet.ReadCSV(context.Background(), path)
	if err != nil {
		t.Fatalf("ReadCSV after write: %v", err)
	}
	if len(got) != len(rows) {
		t.Errorf("expected %d rows, got %d", len(rows), len(got))
	}
	for i, row := range rows {
		for j, cell := range row {
			if got[i][j] != cell {
				t.Errorf("row %d col %d: expected %q, got %q", i, j, cell, got[i][j])
			}
		}
	}
}

func TestReadCSV_Empty(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.csv")
	os.WriteFile(path, []byte(""), 0600)

	rows, err := spreadsheet.ReadCSV(context.Background(), path)
	if err != nil {
		t.Fatalf("ReadCSV empty file: unexpected error: %v", err)
	}
	if len(rows) != 0 {
		t.Errorf("expected 0 rows for empty file, got %d", len(rows))
	}
}

func TestRegisterSpreadsheetTools(t *testing.T) {
	reg := newMockRegistry()
	spreadsheet.RegisterSpreadsheetTools(reg)

	want := map[string]bool{
		"media.spreadsheet.read":  false,
		"media.spreadsheet.write": false,
	}
	for _, name := range reg.tools {
		if _, ok := want[name]; ok {
			want[name] = true
		}
	}
	for name, found := range want {
		if !found {
			t.Errorf("tool %q was not registered", name)
		}
	}
}

func TestReadCSVTool_Execute(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "data.csv")
	os.WriteFile(path, []byte("x,y\n1,2\n"), 0600)

	reg := newMockRegistry()
	spreadsheet.RegisterSpreadsheetTools(reg)

	readExec := reg.exec["media.spreadsheet.read"]
	if readExec == nil {
		t.Fatal("media.spreadsheet.read not registered")
	}

	input, _ := json.Marshal(map[string]string{"path": path})
	out, err := readExec(context.Background(), string(input))
	if err != nil {
		t.Fatalf("read tool exec: %v", err)
	}
	var result map[string]interface{}
	json.Unmarshal([]byte(out), &result)
	if result["count"].(float64) != 2 {
		t.Errorf("expected count=2, got %v", result["count"])
	}
}
