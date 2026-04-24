package instruction

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDiscoverSingleFile(t *testing.T) {
	dir := t.TempDir()
	butlerFile := filepath.Join(dir, "BUTLER.md")
	if err := os.WriteFile(butlerFile, []byte("Always use British English."), 0644); err != nil {
		t.Fatal(err)
	}

	files, err := DiscoverFiles(dir)
	if err != nil {
		t.Fatalf("DiscoverFiles: %v", err)
	}

	found := false
	for _, f := range files {
		if f.Path == butlerFile {
			found = true
			if !strings.Contains(f.Content, "British English") {
				t.Errorf("expected content with 'British English', got: %s", f.Content)
			}
		}
	}
	if !found {
		t.Errorf("BUTLER.md not found in results: %+v", files)
	}
}

func TestAncestorWalking(t *testing.T) {
	root := t.TempDir()
	child := filepath.Join(root, "project", "src")
	if err := os.MkdirAll(child, 0755); err != nil {
		t.Fatal(err)
	}

	// Write BUTLER.md in root and project.
	rootFile := filepath.Join(root, "BUTLER.md")
	if err := os.WriteFile(rootFile, []byte("Root instruction"), 0644); err != nil {
		t.Fatal(err)
	}
	projectFile := filepath.Join(root, "project", "BUTLER.md")
	if err := os.WriteFile(projectFile, []byte("Project instruction"), 0644); err != nil {
		t.Fatal(err)
	}

	files, err := DiscoverFiles(child)
	if err != nil {
		t.Fatalf("DiscoverFiles: %v", err)
	}

	// Should find both, root first.
	if len(files) < 2 {
		t.Fatalf("expected at least 2 files, got %d", len(files))
	}

	// Root should come before project in root-first order.
	rootIdx := -1
	projectIdx := -1
	for i, f := range files {
		if f.Path == rootFile {
			rootIdx = i
		}
		if f.Path == projectFile {
			projectIdx = i
		}
	}
	if rootIdx == -1 || projectIdx == -1 {
		t.Fatalf("expected both root and project files, got %+v", files)
	}
	if rootIdx >= projectIdx {
		t.Errorf("root (%d) should come before project (%d)", rootIdx, projectIdx)
	}
}

func TestDedup(t *testing.T) {
	root := t.TempDir()
	child := filepath.Join(root, "sub")
	if err := os.MkdirAll(child, 0755); err != nil {
		t.Fatal(err)
	}

	// Write identical content in both directories.
	content := "Same instruction content"
	if err := os.WriteFile(filepath.Join(root, "BUTLER.md"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(child, "BUTLER.md"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	files, err := DiscoverFiles(child)
	if err != nil {
		t.Fatalf("DiscoverFiles: %v", err)
	}

	// Should deduplicate identical content.
	count := 0
	for _, f := range files {
		if strings.Contains(f.Content, "Same instruction") {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected 1 deduped file, got %d", count)
	}
}

func TestBudgetTruncation(t *testing.T) {
	dir := t.TempDir()

	// Write a file larger than maxFileSize (4KB).
	bigContent := strings.Repeat("x", 5000)
	if err := os.WriteFile(filepath.Join(dir, "BUTLER.md"), []byte(bigContent), 0644); err != nil {
		t.Fatal(err)
	}

	files, err := DiscoverFiles(dir)
	if err != nil {
		t.Fatalf("DiscoverFiles: %v", err)
	}

	for _, f := range files {
		if strings.Contains(f.Path, "BUTLER.md") {
			if len(f.Content) > maxFileSize {
				t.Errorf("content should be truncated to %d, got %d", maxFileSize, len(f.Content))
			}
			return
		}
	}
	t.Error("BUTLER.md not found")
}

func TestCompositeProviderMerge(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "BUTLER.md"), []byte("file instruction"), 0644); err != nil {
		t.Fatal(err)
	}

	dbProv := &mockDBProvider{
		entries: []InstructionEntry{
			{Content: "db instruction", Category: "rule", Priority: 5},
		},
		count: 1,
	}

	cp := NewCompositeProvider(dbProv, dir)
	entries, err := cp.ActiveForPrompt(context.Background(), "webchat", "sess1")
	if err != nil {
		t.Fatalf("ActiveForPrompt: %v", err)
	}

	// Should have at least 2: file + db.
	if len(entries) < 2 {
		t.Fatalf("expected at least 2 entries, got %d: %+v", len(entries), entries)
	}

	count, err := cp.Count(context.Background())
	if err != nil {
		t.Fatalf("Count: %v", err)
	}
	if count < 2 {
		t.Errorf("expected count >= 2, got %d", count)
	}
}

// mockDBProvider implements InstructionProvider for testing.
type mockDBProvider struct {
	entries []InstructionEntry
	count   int
}

func (m *mockDBProvider) ActiveForPrompt(ctx context.Context, channel, sessionID string) ([]InstructionEntry, error) {
	return m.entries, nil
}

func (m *mockDBProvider) Count(ctx context.Context) (int, error) {
	return m.count, nil
}
