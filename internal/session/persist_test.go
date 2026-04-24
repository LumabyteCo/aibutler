package session

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestAppendAndLoad(t *testing.T) {
	dir := t.TempDir()
	p := NewFilePersister(dir)

	msgs := []PersistMessage{
		{Role: "user", Content: "hello", Timestamp: time.Now().UTC()},
		{Role: "assistant", Content: "hi there", Timestamp: time.Now().UTC()},
		{Role: "system", Content: "", Marker: "completed", Timestamp: time.Now().UTC()},
	}

	for _, msg := range msgs {
		if err := p.Append("sess-1", msg); err != nil {
			t.Fatalf("Append: %v", err)
		}
	}

	loaded, err := p.Load("sess-1")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(loaded) != 3 {
		t.Fatalf("loaded count = %d, want 3", len(loaded))
	}
	if loaded[0].Role != "user" || loaded[0].Content != "hello" {
		t.Errorf("msg[0] = %+v, want user/hello", loaded[0])
	}
	if loaded[1].Role != "assistant" || loaded[1].Content != "hi there" {
		t.Errorf("msg[1] = %+v, want assistant/hi there", loaded[1])
	}
	if loaded[2].Marker != "completed" {
		t.Errorf("msg[2] marker = %q, want 'completed'", loaded[2].Marker)
	}
}

func TestRotation(t *testing.T) {
	dir := t.TempDir()
	p := NewFilePersister(dir)
	p.maxFileSize = 100 // Very small threshold for testing.

	// Write enough messages to trigger rotation.
	for i := 0; i < 10; i++ {
		msg := PersistMessage{
			Role:      "user",
			Content:   "message that is long enough to trigger rotation eventually",
			Timestamp: time.Now().UTC(),
		}
		if err := p.Append("sess-rot", msg); err != nil {
			t.Fatalf("Append %d: %v", i, err)
		}
		// Small delay to ensure unique rotation timestamps.
		time.Sleep(time.Millisecond)
	}

	// Check that rotated files exist.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}

	fileCount := 0
	for _, e := range entries {
		if !e.IsDir() {
			fileCount++
		}
	}
	// With 100-byte threshold and ~100-byte messages, we should have multiple files.
	if fileCount < 2 {
		t.Errorf("expected multiple files from rotation, got %d", fileCount)
	}

	// Load should return all messages across all segments.
	loaded, err := p.Load("sess-rot")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(loaded) != 10 {
		t.Errorf("loaded count = %d, want 10", len(loaded))
	}
}

func TestDetectIncomplete(t *testing.T) {
	dir := t.TempDir()
	p := NewFilePersister(dir)

	// Complete session.
	p.Append("sess-complete", PersistMessage{Role: "user", Content: "hi"})
	p.Append("sess-complete", PersistMessage{Marker: "completed"})

	// Incomplete session.
	p.Append("sess-incomplete", PersistMessage{Role: "user", Content: "hi"})
	p.Append("sess-incomplete", PersistMessage{Role: "assistant", Content: "hello"})

	incomplete, err := p.DetectIncomplete(context.Background())
	if err != nil {
		t.Fatalf("DetectIncomplete: %v", err)
	}

	found := false
	for _, sid := range incomplete {
		if sid == "sess-incomplete" {
			found = true
		}
		if sid == "sess-complete" {
			t.Error("sess-complete should not be incomplete")
		}
	}
	if !found {
		t.Error("sess-incomplete not found in incomplete list")
	}
}

func TestSessionsList(t *testing.T) {
	dir := t.TempDir()
	p := NewFilePersister(dir)

	p.Append("sess-a", PersistMessage{Role: "user", Content: "a"})
	p.Append("sess-b", PersistMessage{Role: "user", Content: "b"})

	sessions, err := p.Sessions()
	if err != nil {
		t.Fatalf("Sessions: %v", err)
	}
	if len(sessions) != 2 {
		t.Errorf("sessions count = %d, want 2", len(sessions))
	}

	names := make(map[string]bool)
	for _, s := range sessions {
		names[s] = true
	}
	if !names["sess-a"] || !names["sess-b"] {
		t.Errorf("sessions = %v, want sess-a and sess-b", sessions)
	}
}
