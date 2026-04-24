package file_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/LumabyteCo/aibutler/internal/agent"
	"github.com/LumabyteCo/aibutler/internal/capability"
	"github.com/LumabyteCo/aibutler/internal/file"
	"github.com/LumabyteCo/aibutler/internal/tool"
)

func setup(t *testing.T) (string, *tool.Dispatcher) {
	t.Helper()
	dir := t.TempDir()
	reg := tool.NewRegistry()
	file.RegisterFileTools(reg, []string{dir})
	disp := tool.NewDispatcher(reg, capability.NewEngine(nil), nil)
	return dir, disp
}

func TestFileReadWrite(t *testing.T) {
	dir, disp := setup(t)
	ctx := context.Background()
	path := filepath.Join(dir, "test.txt")

	// Write.
	result, err := disp.Execute(ctx, agent.ToolCall{
		Name:  "file.write",
		Input: `{"path":"` + path + `","content":"Hello World\nLine 2\n"}`,
	})
	if err != nil {
		t.Fatalf("file.write: %v", err)
	}
	if result == "" {
		t.Error("expected non-empty write result")
	}

	// Read.
	result, err = disp.Execute(ctx, agent.ToolCall{
		Name:  "file.read",
		Input: `{"path":"` + path + `"}`,
	})
	if err != nil {
		t.Fatalf("file.read: %v", err)
	}
	if result != "Hello World\nLine 2\n" {
		t.Errorf("read = %q", result)
	}
}

func TestFileReadWithOffsetLimit(t *testing.T) {
	dir, disp := setup(t)
	ctx := context.Background()
	path := filepath.Join(dir, "multi.txt")

	os.WriteFile(path, []byte("line0\nline1\nline2\nline3\nline4\n"), 0644)

	result, err := disp.Execute(ctx, agent.ToolCall{
		Name:  "file.read",
		Input: `{"path":"` + path + `","offset":1,"limit":2}`,
	})
	if err != nil {
		t.Fatalf("file.read: %v", err)
	}
	if result != "line1\nline2" {
		t.Errorf("read = %q, want 'line1\\nline2'", result)
	}
}

func TestFileEdit(t *testing.T) {
	dir, disp := setup(t)
	ctx := context.Background()
	path := filepath.Join(dir, "edit.txt")

	os.WriteFile(path, []byte("Hello World"), 0644)

	result, err := disp.Execute(ctx, agent.ToolCall{
		Name:  "file.edit",
		Input: `{"path":"` + path + `","old":"World","new":"Go"}`,
	})
	if err != nil {
		t.Fatalf("file.edit: %v", err)
	}
	if result == "" {
		t.Error("expected non-empty result")
	}

	data, _ := os.ReadFile(path)
	if string(data) != "Hello Go" {
		t.Errorf("content = %q, want 'Hello Go'", string(data))
	}
}

func TestFileEditNotFound(t *testing.T) {
	dir, disp := setup(t)
	path := filepath.Join(dir, "edit.txt")
	os.WriteFile(path, []byte("Hello"), 0644)

	_, err := disp.Execute(context.Background(), agent.ToolCall{
		Name:  "file.edit",
		Input: `{"path":"` + path + `","old":"NotHere","new":"X"}`,
	})
	if err == nil {
		t.Error("expected error for string not found")
	}
}

func TestFileList(t *testing.T) {
	dir, disp := setup(t)
	ctx := context.Background()

	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a"), 0644)
	os.WriteFile(filepath.Join(dir, "b.go"), []byte("b"), 0644)
	os.Mkdir(filepath.Join(dir, "subdir"), 0755)

	result, err := disp.Execute(ctx, agent.ToolCall{
		Name:  "file.list",
		Input: `{"path":"` + dir + `"}`,
	})
	if err != nil {
		t.Fatalf("file.list: %v", err)
	}

	var files []struct {
		Name  string `json:"name"`
		IsDir bool   `json:"is_dir"`
	}
	json.Unmarshal([]byte(result), &files)
	if len(files) != 3 {
		t.Errorf("files = %d, want 3", len(files))
	}
}

func TestFileListWithPattern(t *testing.T) {
	dir, disp := setup(t)
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a"), 0644)
	os.WriteFile(filepath.Join(dir, "b.go"), []byte("b"), 0644)

	result, err := disp.Execute(context.Background(), agent.ToolCall{
		Name:  "file.list",
		Input: `{"path":"` + dir + `","pattern":"*.go"}`,
	})
	if err != nil {
		t.Fatalf("file.list: %v", err)
	}

	var files []struct{ Name string }
	json.Unmarshal([]byte(result), &files)
	if len(files) != 1 || files[0].Name != "b.go" {
		t.Errorf("files = %v, want [b.go]", files)
	}
}

func TestFileSearch(t *testing.T) {
	dir, disp := setup(t)
	os.WriteFile(filepath.Join(dir, "code.go"), []byte("func main() {\n\tfmt.Println(\"hello\")\n}"), 0644)
	os.WriteFile(filepath.Join(dir, "other.txt"), []byte("nothing here"), 0644)

	result, err := disp.Execute(context.Background(), agent.ToolCall{
		Name:  "file.search",
		Input: `{"path":"` + dir + `","query":"Println"}`,
	})
	if err != nil {
		t.Fatalf("file.search: %v", err)
	}

	var matches []struct {
		File string `json:"file"`
		Line int    `json:"line"`
	}
	json.Unmarshal([]byte(result), &matches)
	if len(matches) != 1 {
		t.Errorf("matches = %d, want 1", len(matches))
	}
}

// --- Boundary Tests ---

func TestPathBoundaryAllowed(t *testing.T) {
	dir := t.TempDir()
	err := file.CheckPathBoundary(filepath.Join(dir, "sub", "file.txt"), []string{dir})
	if err != nil {
		t.Errorf("expected allowed: %v", err)
	}
}

func TestPathBoundaryDenied(t *testing.T) {
	err := file.CheckPathBoundary("/etc/passwd", []string{"/home/user"})
	if err == nil {
		t.Error("expected boundary error")
	}
}

func TestPathBoundaryNoRestrictions(t *testing.T) {
	err := file.CheckPathBoundary("/anywhere", nil)
	if err != nil {
		t.Errorf("expected no error with nil allowed: %v", err)
	}
}

func TestFileReadOutsideBoundary(t *testing.T) {
	dir := t.TempDir()
	reg := tool.NewRegistry()
	file.RegisterFileTools(reg, []string{dir})
	disp := tool.NewDispatcher(reg, capability.NewEngine(nil), nil)

	_, err := disp.Execute(context.Background(), agent.ToolCall{
		Name:  "file.read",
		Input: `{"path":"/etc/passwd"}`,
	})
	if err == nil {
		t.Error("expected boundary error")
	}
}

func TestFileWriteOutsideBoundary(t *testing.T) {
	dir := t.TempDir()
	reg := tool.NewRegistry()
	file.RegisterFileTools(reg, []string{dir})
	disp := tool.NewDispatcher(reg, capability.NewEngine(nil), nil)

	_, err := disp.Execute(context.Background(), agent.ToolCall{
		Name:  "file.write",
		Input: `{"path":"/tmp/evil.txt","content":"hack"}`,
	})
	if err == nil {
		t.Error("expected boundary error")
	}
}
