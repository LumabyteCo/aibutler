//go:build integration

package integration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ============================================================================
// File Tools (7 tests)
// ============================================================================

func TestE2EFileRead(t *testing.T) {
	p := setupPipelineWithOpts(t, pipelineOpts{WithFile: true})

	// Create test file in the sandboxed directory.
	testFile := filepath.Join(p.FileDir, "hello.txt")
	if err := os.WriteFile(testFile, []byte("Hello World"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// Add responses now that we know p.FileDir.
	p.Fake.AddResponses(
		toolCallResponse("Reading file.",
			tc("tc1", "file.read", `{"path":"`+testFile+`"}`),
		),
		finalResponse("Here is the file content."),
	)

	p.sendMsg(t, "Read the file hello.txt")

	// Verify two model calls: tool call + final response.
	if got := p.Fake.CallCount(); got != 2 {
		t.Fatalf("CallCount = %d, want 2", got)
	}

	// Verify the tool result (sent to model in call 2) contains the file content.
	calls := p.Fake.Calls()
	found := false
	for _, msg := range calls[1] {
		if msg.Role == "tool" && strings.Contains(msg.Content, "Hello World") {
			found = true
			break
		}
	}
	if !found {
		t.Error("tool result does not contain 'Hello World'")
	}

	// Verify the final response was delivered.
	resp := p.lastResponse(t)
	if resp != "Here is the file content." {
		t.Errorf("response = %q, want %q", resp, "Here is the file content.")
	}
}

func TestE2EFileWrite(t *testing.T) {
	p := setupPipelineWithOpts(t, pipelineOpts{WithFile: true})

	outFile := filepath.Join(p.FileDir, "output.txt")

	p.Fake.AddResponses(
		toolCallResponse("Writing file.",
			tc("tc1", "file.write", `{"path":"`+outFile+`","content":"test data 123"}`),
		),
		finalResponse("File written successfully."),
	)

	p.sendMsg(t, "Write test data to output.txt")

	// Verify two model calls.
	if got := p.Fake.CallCount(); got != 2 {
		t.Fatalf("CallCount = %d, want 2", got)
	}

	// Verify the file exists on disk with correct content.
	data, err := os.ReadFile(outFile)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(data) != "test data 123" {
		t.Errorf("file content = %q, want %q", string(data), "test data 123")
	}

	// Verify tool result mentions bytes written.
	calls := p.Fake.Calls()
	found := false
	for _, msg := range calls[1] {
		if msg.Role == "tool" && strings.Contains(msg.Content, "Written") {
			found = true
			break
		}
	}
	if !found {
		t.Error("tool result does not contain 'Written'")
	}

	resp := p.lastResponse(t)
	if resp != "File written successfully." {
		t.Errorf("response = %q, want %q", resp, "File written successfully.")
	}
}

func TestE2EFileEdit(t *testing.T) {
	p := setupPipelineWithOpts(t, pipelineOpts{WithFile: true})

	// Create a file with content to be edited.
	editFile := filepath.Join(p.FileDir, "edit.txt")
	if err := os.WriteFile(editFile, []byte("Hello Alice, welcome!"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	p.Fake.AddResponses(
		toolCallResponse("Editing file.",
			tc("tc1", "file.edit", `{"path":"`+editFile+`","old":"Alice","new":"Bob"}`),
		),
		finalResponse("File edited."),
	)

	p.sendMsg(t, "Replace Alice with Bob in edit.txt")

	// Verify two model calls.
	if got := p.Fake.CallCount(); got != 2 {
		t.Fatalf("CallCount = %d, want 2", got)
	}

	// Verify the file was updated on disk.
	data, err := os.ReadFile(editFile)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(data) != "Hello Bob, welcome!" {
		t.Errorf("file content = %q, want %q", string(data), "Hello Bob, welcome!")
	}

	// Verify tool result mentions replacement.
	calls := p.Fake.Calls()
	found := false
	for _, msg := range calls[1] {
		if msg.Role == "tool" && strings.Contains(msg.Content, "Replaced") {
			found = true
			break
		}
	}
	if !found {
		t.Error("tool result does not contain 'Replaced'")
	}

	resp := p.lastResponse(t)
	if resp != "File edited." {
		t.Errorf("response = %q, want %q", resp, "File edited.")
	}
}

func TestE2EFileList(t *testing.T) {
	p := setupPipelineWithOpts(t, pipelineOpts{WithFile: true})

	// Create 3 files in the sandboxed directory.
	for _, name := range []string{"alpha.txt", "beta.txt", "gamma.txt"} {
		if err := os.WriteFile(filepath.Join(p.FileDir, name), []byte(name), 0644); err != nil {
			t.Fatalf("WriteFile(%s): %v", name, err)
		}
	}

	p.Fake.AddResponses(
		toolCallResponse("Listing files.",
			tc("tc1", "file.list", `{"path":"`+p.FileDir+`"}`),
		),
		finalResponse("Here are the files."),
	)

	p.sendMsg(t, "List files in the directory")

	// Verify two model calls.
	if got := p.Fake.CallCount(); got != 2 {
		t.Fatalf("CallCount = %d, want 2", got)
	}

	// Verify tool result contains all 3 filenames.
	calls := p.Fake.Calls()
	var toolContent string
	for _, msg := range calls[1] {
		if msg.Role == "tool" {
			toolContent = msg.Content
			break
		}
	}
	for _, name := range []string{"alpha.txt", "beta.txt", "gamma.txt"} {
		if !strings.Contains(toolContent, name) {
			t.Errorf("tool result does not contain %q", name)
		}
	}

	resp := p.lastResponse(t)
	if resp != "Here are the files." {
		t.Errorf("response = %q, want %q", resp, "Here are the files.")
	}
}

func TestE2EFileSearch(t *testing.T) {
	p := setupPipelineWithOpts(t, pipelineOpts{WithFile: true})

	// Create files with specific content.
	if err := os.WriteFile(filepath.Join(p.FileDir, "notes.txt"), []byte("The secret code is 42."), 0644); err != nil {
		t.Fatalf("WriteFile(notes.txt): %v", err)
	}
	if err := os.WriteFile(filepath.Join(p.FileDir, "readme.txt"), []byte("Nothing interesting here."), 0644); err != nil {
		t.Fatalf("WriteFile(readme.txt): %v", err)
	}

	p.Fake.AddResponses(
		toolCallResponse("Searching files.",
			tc("tc1", "file.search", `{"path":"`+p.FileDir+`","query":"secret"}`),
		),
		finalResponse("Found the secret."),
	)

	p.sendMsg(t, "Search for secret in files")

	// Verify two model calls.
	if got := p.Fake.CallCount(); got != 2 {
		t.Fatalf("CallCount = %d, want 2", got)
	}

	// Verify tool result contains the matching file and text.
	calls := p.Fake.Calls()
	var toolContent string
	for _, msg := range calls[1] {
		if msg.Role == "tool" {
			toolContent = msg.Content
			break
		}
	}
	if !strings.Contains(toolContent, "notes.txt") {
		t.Error("tool result does not contain 'notes.txt'")
	}
	if !strings.Contains(toolContent, "secret") {
		t.Error("tool result does not contain 'secret'")
	}

	resp := p.lastResponse(t)
	if resp != "Found the secret." {
		t.Errorf("response = %q, want %q", resp, "Found the secret.")
	}
}

func TestE2EFilePathBoundary(t *testing.T) {
	p := setupPipelineWithOpts(t, pipelineOpts{WithFile: true})

	// Attempt to read a file outside the sandboxed directory.
	p.Fake.AddResponses(
		toolCallResponse("Reading file.",
			tc("tc1", "file.read", `{"path":"/etc/hosts"}`),
		),
		finalResponse("Cannot read that file."),
	)

	p.sendMsg(t, "Read /etc/hosts")

	// Verify two model calls.
	if got := p.Fake.CallCount(); got != 2 {
		t.Fatalf("CallCount = %d, want 2", got)
	}

	// Verify the tool result contains the boundary error.
	calls := p.Fake.Calls()
	found := false
	for _, msg := range calls[1] {
		if msg.Role == "tool" && strings.Contains(msg.Content, "outside allowed boundaries") {
			found = true
			break
		}
	}
	if !found {
		t.Error("tool result does not contain 'outside allowed boundaries' error")
	}

	resp := p.lastResponse(t)
	if resp != "Cannot read that file." {
		t.Errorf("response = %q, want %q", resp, "Cannot read that file.")
	}
}

func TestE2EFileEditReplaceAll(t *testing.T) {
	p := setupPipelineWithOpts(t, pipelineOpts{WithFile: true})

	// Create a file with repeated text.
	editFile := filepath.Join(p.FileDir, "repeated.txt")
	content := "foo bar foo baz foo"
	if err := os.WriteFile(editFile, []byte(content), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	p.Fake.AddResponses(
		toolCallResponse("Replacing all occurrences.",
			tc("tc1", "file.edit", `{"path":"`+editFile+`","old":"foo","new":"qux","replace_all":true}`),
		),
		finalResponse("All occurrences replaced."),
	)

	p.sendMsg(t, "Replace all foo with qux in repeated.txt")

	// Verify two model calls.
	if got := p.Fake.CallCount(); got != 2 {
		t.Fatalf("CallCount = %d, want 2", got)
	}

	// Verify the file was updated: all 3 occurrences replaced.
	data, err := os.ReadFile(editFile)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	expected := "qux bar qux baz qux"
	if string(data) != expected {
		t.Errorf("file content = %q, want %q", string(data), expected)
	}

	// Verify tool result mentions 3 replacements.
	calls := p.Fake.Calls()
	found := false
	for _, msg := range calls[1] {
		if msg.Role == "tool" && strings.Contains(msg.Content, "3 occurrence") {
			found = true
			break
		}
	}
	if !found {
		t.Error("tool result does not contain '3 occurrence'")
	}

	resp := p.lastResponse(t)
	if resp != "All occurrences replaced." {
		t.Errorf("response = %q, want %q", resp, "All occurrences replaced.")
	}
}
