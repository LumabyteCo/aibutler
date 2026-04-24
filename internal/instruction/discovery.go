package instruction

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	maxFileSize  = 4096  // 4K per file
	maxTotalSize = 16384 // 16K total budget
)

// fileNames are the instruction filenames searched at each directory.
var fileNames = []string{
	"BUTLER.md",
	"BUTLER.local.md",
	".aibutler/BUTLER.md",
	".aibutler/instructions.md",
}

// FileInstruction represents a discovered instruction file.
type FileInstruction struct {
	Path    string
	Content string
	Scope   string // parent directory name
}

// DiscoverFiles walks from cwd to filesystem root, collecting instruction files.
// Results are returned in root-first order. Files are deduplicated by content hash
// and truncated to the per-file and total budgets.
func DiscoverFiles(cwd string) ([]FileInstruction, error) {
	cwd, err := filepath.Abs(cwd)
	if err != nil {
		return nil, fmt.Errorf("instruction: abs path: %w", err)
	}

	// Collect directories from cwd to root.
	var dirs []string
	dir := cwd
	for {
		dirs = append(dirs, dir)
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}

	// Reverse to get root-first order.
	for i, j := 0, len(dirs)-1; i < j; i, j = i+1, j-1 {
		dirs[i], dirs[j] = dirs[j], dirs[i]
	}

	var results []FileInstruction
	seen := make(map[string]bool) // content hash -> seen
	totalSize := 0

	for _, d := range dirs {
		for _, name := range fileNames {
			path := filepath.Join(d, name)
			data, err := os.ReadFile(path)
			if err != nil {
				continue // file not found
			}

			content := string(data)
			if len(content) > maxFileSize {
				content = content[:maxFileSize]
			}

			// Dedup by content hash.
			hash := fmt.Sprintf("%x", sha256.Sum256([]byte(content)))
			if seen[hash] {
				continue
			}
			seen[hash] = true

			// Check total budget.
			if totalSize+len(content) > maxTotalSize {
				remaining := maxTotalSize - totalSize
				if remaining <= 0 {
					return results, nil
				}
				content = content[:remaining]
			}
			totalSize += len(content)

			scope := filepath.Base(d)
			results = append(results, FileInstruction{
				Path:    path,
				Content: content,
				Scope:   scope,
			})
		}
	}

	return results, nil
}

// InstructionProvider is the interface for providing instructions to the prompt composer.
// This matches the prompt.InstructionProvider interface to avoid import cycles.
type InstructionProvider interface {
	ActiveForPrompt(ctx context.Context, channel, sessionID string) ([]InstructionEntry, error)
	Count(ctx context.Context) (int, error)
}

// InstructionEntry holds a single instruction entry for the composite provider.
type InstructionEntry struct {
	Content  string
	Category string
	Priority int
}

// CompositeProvider merges DB-backed instructions with file-based instructions.
type CompositeProvider struct {
	dbProvider       InstructionProvider
	fileInstructions []FileInstruction
}

// NewCompositeProvider creates a provider that combines DB and file instructions.
func NewCompositeProvider(db InstructionProvider, cwd string) *CompositeProvider {
	files, _ := DiscoverFiles(cwd)
	return &CompositeProvider{
		dbProvider:       db,
		fileInstructions: files,
	}
}

// ActiveForPrompt returns combined instructions from DB and files.
func (p *CompositeProvider) ActiveForPrompt(ctx context.Context, channel, sessionID string) ([]InstructionEntry, error) {
	var entries []InstructionEntry

	// File instructions first (lower priority, project context).
	for _, f := range p.fileInstructions {
		entries = append(entries, InstructionEntry{
			Content:  fmt.Sprintf("[%s] %s", f.Scope, strings.TrimSpace(f.Content)),
			Category: "project",
			Priority: 1,
		})
	}

	// DB instructions (higher priority, user-set).
	if p.dbProvider != nil {
		dbEntries, err := p.dbProvider.ActiveForPrompt(ctx, channel, sessionID)
		if err != nil {
			return entries, nil // degrade gracefully
		}
		entries = append(entries, dbEntries...)
	}

	return entries, nil
}

// Count returns the total number of instructions (DB + file).
func (p *CompositeProvider) Count(ctx context.Context) (int, error) {
	dbCount := 0
	if p.dbProvider != nil {
		var err error
		dbCount, err = p.dbProvider.Count(ctx)
		if err != nil {
			return len(p.fileInstructions), nil
		}
	}
	return dbCount + len(p.fileInstructions), nil
}
