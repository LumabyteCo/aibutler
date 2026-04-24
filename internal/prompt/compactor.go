package prompt

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/LumabyteCo/aibutler/internal/agent"
)

// Compaction preamble constants.
const (
	CompactContinuationPreamble = "This session is being continued from a previous conversation that ran out of context."
	CompactResumeInstruction    = "Continue the conversation from where it left off without asking the user any further questions."
)

// CompactorConfig holds configuration for context compaction.
type CompactorConfig struct {
	MaxEstimatedTokens  int      // Threshold to trigger compaction (default 80000)
	PreserveRecentCount int      // Number of recent messages to keep verbatim (default 4)
	FileExtensions      []string // Extensions to extract as key files
}

// DefaultCompactorConfig returns sensible defaults.
func DefaultCompactorConfig() CompactorConfig {
	return CompactorConfig{
		MaxEstimatedTokens:  80000,
		PreserveRecentCount: 4,
		FileExtensions:      []string{"go", "mod", "ts", "tsx", "js", "json", "md", "py", "rs"},
	}
}

// Compactor performs algorithmic context compaction (no LLM calls).
type Compactor struct {
	cfg CompactorConfig
}

// NewCompactor creates a new Compactor with the given config.
func NewCompactor(cfg CompactorConfig) *Compactor {
	if cfg.MaxEstimatedTokens <= 0 {
		cfg.MaxEstimatedTokens = 80000
	}
	if cfg.PreserveRecentCount <= 0 {
		cfg.PreserveRecentCount = 4
	}
	if len(cfg.FileExtensions) == 0 {
		cfg.FileExtensions = DefaultCompactorConfig().FileExtensions
	}
	return &Compactor{cfg: cfg}
}

// CompactionMetadata describes what happened during compaction.
type CompactionMetadata struct {
	OriginalCount  int
	CompactedCount int
	RemovedCount   int
	PreservedCount int

	EstTokensBefore int
	EstTokensAfter  int

	ToolsUsed   []string
	KeyFiles    []string
	PendingWork []string
}

// EstimateTokens estimates the total token count for a message slice.
func (c *Compactor) EstimateTokens(messages []agent.Message) int {
	return estimateMessageTokens(messages)
}

// ShouldCompact returns true if the messages exceed the configured token threshold.
func (c *Compactor) ShouldCompact(messages []agent.Message) bool {
	return c.EstimateTokens(messages) > c.cfg.MaxEstimatedTokens
}

// Compact performs algorithmic summarization, replacing older messages with a
// synthetic summary while preserving the most recent messages verbatim.
func (c *Compactor) Compact(messages []agent.Message) ([]agent.Message, *CompactionMetadata, error) {
	if len(messages) == 0 {
		return messages, &CompactionMetadata{}, nil
	}

	tokensBefore := c.EstimateTokens(messages)
	preserveCount := c.cfg.PreserveRecentCount
	if preserveCount > len(messages) {
		preserveCount = len(messages)
	}

	// Split into compactable (older) and preserved (recent).
	cutoff := len(messages) - preserveCount
	if cutoff < 0 {
		cutoff = 0
	}
	older := messages[:cutoff]
	preserved := messages[cutoff:]

	// Detect prior compaction.
	var priorCompaction string
	if len(older) > 0 && strings.HasPrefix(older[0].Content, CompactContinuationPreamble) {
		priorCompaction = older[0].Content
	}

	// 1. Count by role.
	roleCounts := make(map[string]int)
	for _, m := range older {
		roleCounts[m.Role]++
	}

	// 2. Extract unique tool names (sorted, deduped).
	toolSet := make(map[string]bool)
	for _, m := range older {
		for _, tc := range m.ToolCalls {
			toolSet[tc.Name] = true
		}
	}
	tools := sortedKeys(toolSet)

	// 3. Extract recent user requests (last 3, truncated 160 chars).
	var userRequests []string
	for i := len(older) - 1; i >= 0 && len(userRequests) < 3; i-- {
		if older[i].Role == "user" {
			userRequests = append(userRequests, truncate(older[i].Content, 160))
		}
	}

	// 4. Infer pending work.
	pendingKeywords := []string{"todo", "next", "pending", "follow up", "remaining"}
	var pendingWork []string
	for _, m := range older {
		lower := strings.ToLower(m.Content)
		for _, kw := range pendingKeywords {
			if strings.Contains(lower, kw) {
				pendingWork = append(pendingWork, truncate(m.Content, 160))
				break
			}
		}
	}
	if len(pendingWork) > 5 {
		pendingWork = pendingWork[len(pendingWork)-5:]
	}

	// 5. Collect key file paths by extension (up to 8).
	fileSet := make(map[string]bool)
	extSet := make(map[string]bool)
	for _, ext := range c.cfg.FileExtensions {
		extSet["."+ext] = true
	}
	for _, m := range older {
		words := strings.Fields(m.Content)
		for _, w := range words {
			// Simple heuristic: if a word looks like a file path.
			ext := filepath.Ext(w)
			if ext != "" && extSet[ext] && !fileSet[w] {
				fileSet[w] = true
				if len(fileSet) >= 8 {
					break
				}
			}
		}
		if len(fileSet) >= 8 {
			break
		}
	}
	keyFiles := sortedKeys(fileSet)

	// 6. Current work (last assistant text, 500 chars).
	var currentWork string
	for i := len(older) - 1; i >= 0; i-- {
		if older[i].Role == "assistant" && older[i].Content != "" {
			currentWork = truncate(older[i].Content, 500)
			break
		}
	}

	// 7. Build timeline (every message as "role: content", 160 chars max).
	var timeline []string
	for _, m := range older {
		if m.Content == "" {
			continue
		}
		entry := fmt.Sprintf("%s: %s", m.Role, truncate(m.Content, 160))
		timeline = append(timeline, entry)
	}

	// Build summary.
	var sb strings.Builder
	sb.WriteString(CompactContinuationPreamble)
	sb.WriteString("\n\n")

	// Handle iterative merging.
	if priorCompaction != "" {
		// Extract old highlights (everything after the preamble in the prior compaction).
		oldHighlights := strings.TrimPrefix(priorCompaction, CompactContinuationPreamble)
		oldHighlights = strings.TrimSpace(oldHighlights)
		if oldHighlights != "" {
			sb.WriteString("Previously compacted:\n")
			sb.WriteString(oldHighlights)
			sb.WriteString("\n\n")
		}
		sb.WriteString("Newly compacted:\n")
	}

	sb.WriteString(fmt.Sprintf("Message counts: user=%d, assistant=%d, tool=%d\n",
		roleCounts["user"], roleCounts["assistant"], roleCounts["tool"]))

	if len(tools) > 0 {
		sb.WriteString("Tools used: " + strings.Join(tools, ", ") + "\n")
	}

	if len(userRequests) > 0 {
		sb.WriteString("Recent requests:\n")
		for _, r := range userRequests {
			sb.WriteString("  - " + r + "\n")
		}
	}

	if len(pendingWork) > 0 {
		sb.WriteString("Pending work:\n")
		for _, p := range pendingWork {
			sb.WriteString("  - " + p + "\n")
		}
	}

	if len(keyFiles) > 0 {
		sb.WriteString("Key files: " + strings.Join(keyFiles, ", ") + "\n")
	}

	if currentWork != "" {
		sb.WriteString("Current work: " + currentWork + "\n")
	}

	if len(timeline) > 0 {
		sb.WriteString("\nTimeline:\n")
		for _, t := range timeline {
			sb.WriteString("  " + t + "\n")
		}
	}

	sb.WriteString("\n" + CompactResumeInstruction)

	// Build compacted result: summary + preserved messages.
	summary := agent.Message{
		Role:    "user",
		Content: sb.String(),
	}
	result := make([]agent.Message, 0, 1+len(preserved))
	result = append(result, summary)
	result = append(result, preserved...)

	meta := &CompactionMetadata{
		OriginalCount:  len(messages),
		CompactedCount: len(result),
		RemovedCount:   len(older),
		PreservedCount: len(preserved),

		EstTokensBefore: tokensBefore,
		EstTokensAfter:  c.EstimateTokens(result),

		ToolsUsed:   tools,
		KeyFiles:    keyFiles,
		PendingWork: pendingWork,
	}

	return result, meta, nil
}

// sortedKeys returns the keys of a map[string]bool sorted alphabetically.
func sortedKeys(m map[string]bool) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// truncate returns the first n characters of s, appending "..." if truncated.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
