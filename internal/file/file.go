package file

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/LumabyteCo/aibutler/internal/tool"
)

// RegisterFileTools registers file manipulation tools.
func RegisterFileTools(registry *tool.Registry, allowedPaths []string) {
	registry.Register(&fileReadTool{allowed: allowedPaths})
	registry.Register(&fileWriteTool{allowed: allowedPaths})
	registry.Register(&fileEditTool{allowed: allowedPaths})
	registry.Register(&fileListTool{allowed: allowedPaths})
	registry.Register(&fileSearchTool{allowed: allowedPaths})
}

// CheckPathBoundary verifies that a path is within one of the allowed directories.
// It resolves symlinks and checks prefix matching.
func CheckPathBoundary(path string, allowed []string) error {
	if len(allowed) == 0 {
		return nil // No restrictions.
	}

	abs, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("file: resolve path: %w", err)
	}
	abs = resolveSymlinks(abs)

	for _, a := range allowed {
		allowedAbs, err := filepath.Abs(a)
		if err != nil {
			continue
		}
		allowedAbs = resolveSymlinks(allowedAbs)

		// Ensure trailing separator for prefix match to avoid
		// /home/user matching /home/username.
		if !strings.HasSuffix(allowedAbs, string(filepath.Separator)) {
			allowedAbs += string(filepath.Separator)
		}

		if abs+"/" == allowedAbs || strings.HasPrefix(abs, allowedAbs) {
			return nil
		}
	}

	return fmt.Errorf("file: path %q is outside allowed boundaries", path)
}

// resolveSymlinks resolves symlinks for existing paths, or resolves
// the closest existing parent for paths that don't exist yet.
func resolveSymlinks(p string) string {
	if _, err := os.Lstat(p); err == nil {
		if resolved, err := filepath.EvalSymlinks(p); err == nil {
			return resolved
		}
		return p
	}
	// Path doesn't exist — resolve parent.
	parent := filepath.Dir(p)
	if parent == p {
		return p
	}
	return filepath.Join(resolveSymlinks(parent), filepath.Base(p))
}

// --- file.read ---

type fileReadTool struct{ allowed []string }

func (t *fileReadTool) Name() string        { return "file.read" }
func (t *fileReadTool) Description() string { return "Read the contents of a file" }
func (t *fileReadTool) Capability() string  { return "tool.file.read" }
func (t *fileReadTool) Schema() string {
	return `{"type":"object","properties":{"path":{"type":"string"},"offset":{"type":"integer","description":"Line offset to start from (0-based)"},"limit":{"type":"integer","description":"Max lines to read"}},"required":["path"]}`
}

func (t *fileReadTool) Execute(_ context.Context, input string) (string, error) {
	var args struct {
		Path   string `json:"path"`
		Offset int    `json:"offset"`
		Limit  int    `json:"limit"`
	}
	if err := json.Unmarshal([]byte(input), &args); err != nil {
		return "", fmt.Errorf("file.read: invalid input: %w", err)
	}
	if err := CheckPathBoundary(args.Path, t.allowed); err != nil {
		return "", err
	}

	data, err := os.ReadFile(args.Path)
	if err != nil {
		return "", fmt.Errorf("file.read: %w", err)
	}

	content := string(data)
	if args.Offset > 0 || args.Limit > 0 {
		lines := strings.Split(content, "\n")
		if args.Offset >= len(lines) {
			return "", nil
		}
		lines = lines[args.Offset:]
		if args.Limit > 0 && args.Limit < len(lines) {
			lines = lines[:args.Limit]
		}
		content = strings.Join(lines, "\n")
	}

	// Truncate large files.
	const maxSize = 100 * 1024
	if len(content) > maxSize {
		content = content[:maxSize] + "\n... [truncated]"
	}

	return content, nil
}

// --- file.write ---

type fileWriteTool struct{ allowed []string }

func (t *fileWriteTool) Name() string        { return "file.write" }
func (t *fileWriteTool) Description() string { return "Write content to a file (creates or overwrites)" }
func (t *fileWriteTool) Capability() string  { return "tool.file.write" }
func (t *fileWriteTool) Schema() string {
	return `{"type":"object","properties":{"path":{"type":"string"},"content":{"type":"string"}},"required":["path","content"]}`
}

func (t *fileWriteTool) Execute(_ context.Context, input string) (string, error) {
	var args struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	}
	if err := json.Unmarshal([]byte(input), &args); err != nil {
		return "", fmt.Errorf("file.write: invalid input: %w", err)
	}
	if err := CheckPathBoundary(args.Path, t.allowed); err != nil {
		return "", err
	}

	// Ensure parent directory exists.
	dir := filepath.Dir(args.Path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", fmt.Errorf("file.write: mkdir: %w", err)
	}

	if err := os.WriteFile(args.Path, []byte(args.Content), 0644); err != nil {
		return "", fmt.Errorf("file.write: %w", err)
	}
	return fmt.Sprintf("Written %d bytes to %s", len(args.Content), args.Path), nil
}

// --- file.edit ---

type fileEditTool struct{ allowed []string }

func (t *fileEditTool) Name() string        { return "file.edit" }
func (t *fileEditTool) Description() string { return "Edit a file by replacing a specific string" }
func (t *fileEditTool) Capability() string  { return "tool.file.edit" }
func (t *fileEditTool) Schema() string {
	return `{"type":"object","properties":{"path":{"type":"string"},"old":{"type":"string","description":"Text to find"},"new":{"type":"string","description":"Replacement text"},"replace_all":{"type":"boolean"}},"required":["path","old","new"]}`
}

func (t *fileEditTool) Execute(_ context.Context, input string) (string, error) {
	var args struct {
		Path       string `json:"path"`
		Old        string `json:"old"`
		New        string `json:"new"`
		ReplaceAll bool   `json:"replace_all"`
	}
	if err := json.Unmarshal([]byte(input), &args); err != nil {
		return "", fmt.Errorf("file.edit: invalid input: %w", err)
	}
	if err := CheckPathBoundary(args.Path, t.allowed); err != nil {
		return "", err
	}

	data, err := os.ReadFile(args.Path)
	if err != nil {
		return "", fmt.Errorf("file.edit: read: %w", err)
	}

	content := string(data)
	if !strings.Contains(content, args.Old) {
		return "", fmt.Errorf("file.edit: old string not found in %s", args.Path)
	}

	var count int
	if args.ReplaceAll {
		count = strings.Count(content, args.Old)
		content = strings.ReplaceAll(content, args.Old, args.New)
	} else {
		count = 1
		content = strings.Replace(content, args.Old, args.New, 1)
	}

	if err := os.WriteFile(args.Path, []byte(content), 0644); err != nil {
		return "", fmt.Errorf("file.edit: write: %w", err)
	}
	return fmt.Sprintf("Replaced %d occurrence(s) in %s", count, args.Path), nil
}

// --- file.list ---

type fileListTool struct{ allowed []string }

func (t *fileListTool) Name() string        { return "file.list" }
func (t *fileListTool) Description() string { return "List files in a directory" }
func (t *fileListTool) Capability() string  { return "tool.file.read" }
func (t *fileListTool) Schema() string {
	return `{"type":"object","properties":{"path":{"type":"string"},"pattern":{"type":"string","description":"Glob pattern filter"}},"required":["path"]}`
}

func (t *fileListTool) Execute(_ context.Context, input string) (string, error) {
	var args struct {
		Path    string `json:"path"`
		Pattern string `json:"pattern"`
	}
	if err := json.Unmarshal([]byte(input), &args); err != nil {
		return "", fmt.Errorf("file.list: invalid input: %w", err)
	}
	if err := CheckPathBoundary(args.Path, t.allowed); err != nil {
		return "", err
	}

	entries, err := os.ReadDir(args.Path)
	if err != nil {
		return "", fmt.Errorf("file.list: %w", err)
	}

	type fileInfo struct {
		Name  string `json:"name"`
		IsDir bool   `json:"is_dir"`
		Size  int64  `json:"size"`
	}

	var files []fileInfo
	for _, e := range entries {
		if args.Pattern != "" {
			matched, _ := filepath.Match(args.Pattern, e.Name())
			if !matched {
				continue
			}
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		files = append(files, fileInfo{
			Name:  e.Name(),
			IsDir: e.IsDir(),
			Size:  info.Size(),
		})
		if len(files) >= 200 {
			break
		}
	}

	out, _ := json.Marshal(files)
	return string(out), nil
}

// --- file.search ---

type fileSearchTool struct{ allowed []string }

func (t *fileSearchTool) Name() string        { return "file.search" }
func (t *fileSearchTool) Description() string { return "Search for a string in files within a directory" }
func (t *fileSearchTool) Capability() string  { return "tool.file.read" }
func (t *fileSearchTool) Schema() string {
	return `{"type":"object","properties":{"path":{"type":"string","description":"Directory to search"},"query":{"type":"string","description":"Text to search for"},"pattern":{"type":"string","description":"Filename glob pattern"}},"required":["path","query"]}`
}

func (t *fileSearchTool) Execute(_ context.Context, input string) (string, error) {
	var args struct {
		Path    string `json:"path"`
		Query   string `json:"query"`
		Pattern string `json:"pattern"`
	}
	if err := json.Unmarshal([]byte(input), &args); err != nil {
		return "", fmt.Errorf("file.search: invalid input: %w", err)
	}
	if err := CheckPathBoundary(args.Path, t.allowed); err != nil {
		return "", err
	}

	type match struct {
		File string `json:"file"`
		Line int    `json:"line"`
		Text string `json:"text"`
	}

	var matches []match
	const maxMatches = 50

	err := filepath.Walk(args.Path, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		if len(matches) >= maxMatches {
			return filepath.SkipAll
		}
		if args.Pattern != "" {
			matched, _ := filepath.Match(args.Pattern, info.Name())
			if !matched {
				return nil
			}
		}
		// Skip large files and binary files.
		if info.Size() > 1*1024*1024 {
			return nil
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		lines := strings.Split(string(data), "\n")
		for i, line := range lines {
			if strings.Contains(line, args.Query) {
				matches = append(matches, match{
					File: path,
					Line: i + 1,
					Text: truncateLine(line, 200),
				})
				if len(matches) >= maxMatches {
					return filepath.SkipAll
				}
			}
		}
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("file.search: %w", err)
	}

	out, _ := json.Marshal(matches)
	return string(out), nil
}

func truncateLine(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
