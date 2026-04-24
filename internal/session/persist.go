package session

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const defaultMaxFileSize int64 = 256 * 1024 // 256KB

// PersistMessage is the JSONL-serialized message format.
type PersistMessage struct {
	Role      string    `json:"role"`
	Content   string    `json:"content"`
	ToolID    string    `json:"tool_id,omitempty"`
	Timestamp time.Time `json:"timestamp"`
	Marker    string    `json:"marker,omitempty"` // "completed" marks session end
}

// FilePersister appends session messages to JSONL files for crash recovery.
type FilePersister struct {
	dir         string
	maxFileSize int64
}

// NewFilePersister creates a file persister that stores JSONL files in dir.
func NewFilePersister(dir string) *FilePersister {
	return &FilePersister{
		dir:         dir,
		maxFileSize: defaultMaxFileSize,
	}
}

// Append writes a message to the session's JSONL file.
// If the file exceeds maxFileSize, it rotates to a new segment.
func (p *FilePersister) Append(sessionID string, msg PersistMessage) error {
	if err := os.MkdirAll(p.dir, 0700); err != nil {
		return fmt.Errorf("persist: mkdir: %w", err)
	}

	path := p.sessionPath(sessionID)

	// Check if rotation is needed.
	if info, err := os.Stat(path); err == nil && info.Size() >= p.maxFileSize {
		rotated := path + "." + time.Now().Format("20060102T150405.000000")
		if err := os.Rename(path, rotated); err != nil {
			return fmt.Errorf("persist: rotate: %w", err)
		}
	}

	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return fmt.Errorf("persist: open: %w", err)
	}
	defer f.Close()

	if msg.Timestamp.IsZero() {
		msg.Timestamp = time.Now().UTC()
	}

	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("persist: marshal: %w", err)
	}
	data = append(data, '\n')

	if _, err := f.Write(data); err != nil {
		return fmt.Errorf("persist: write: %w", err)
	}
	return nil
}

// Load reads all messages from a session's JSONL file(s).
func (p *FilePersister) Load(sessionID string) ([]PersistMessage, error) {
	path := p.sessionPath(sessionID)

	// Collect rotated segments + current file.
	pattern := path + ".*"
	rotated, _ := filepath.Glob(pattern)

	// Read rotated segments in order first.
	var msgs []PersistMessage
	for _, seg := range rotated {
		segMsgs, err := p.readJSONL(seg)
		if err != nil {
			return nil, fmt.Errorf("persist: load segment: %w", err)
		}
		msgs = append(msgs, segMsgs...)
	}

	// Read current file.
	current, err := p.readJSONL(path)
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("persist: load: %w", err)
	}
	msgs = append(msgs, current...)

	return msgs, nil
}

// Sessions lists all session IDs that have JSONL files.
func (p *FilePersister) Sessions() ([]string, error) {
	entries, err := os.ReadDir(p.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("persist: list: %w", err)
	}

	seen := make(map[string]bool)
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, ".jsonl") {
			continue
		}
		// Strip .jsonl and any rotation suffix.
		base := strings.TrimSuffix(name, ".jsonl")
		if idx := strings.Index(base, ".jsonl."); idx >= 0 {
			base = base[:idx]
		}
		// Handle rotated files: name.jsonl.20060102T150405
		if strings.Contains(name, ".jsonl.") {
			parts := strings.SplitN(name, ".jsonl", 2)
			if len(parts) > 0 {
				base = parts[0]
			}
		}
		seen[base] = true
	}

	var ids []string
	for id := range seen {
		ids = append(ids, id)
	}
	return ids, nil
}

// DetectIncomplete returns sessions without a "completed" marker.
func (p *FilePersister) DetectIncomplete(ctx context.Context) ([]string, error) {
	allSessions, err := p.Sessions()
	if err != nil {
		return nil, err
	}

	var incomplete []string
	for _, sid := range allSessions {
		msgs, err := p.Load(sid)
		if err != nil {
			continue
		}
		hasCompleted := false
		for _, msg := range msgs {
			if msg.Marker == "completed" {
				hasCompleted = true
				break
			}
		}
		if !hasCompleted {
			incomplete = append(incomplete, sid)
		}
	}
	return incomplete, nil
}

func (p *FilePersister) sessionPath(sessionID string) string {
	return filepath.Join(p.dir, sessionID+".jsonl")
}

func (p *FilePersister) readJSONL(path string) ([]PersistMessage, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var msgs []PersistMessage
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 256*1024), 256*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var msg PersistMessage
		if err := json.Unmarshal(line, &msg); err != nil {
			continue // Skip corrupted lines.
		}
		msgs = append(msgs, msg)
	}
	return msgs, scanner.Err()
}
