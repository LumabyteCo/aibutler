package instruction

import (
	"context"
	"regexp"
	"strings"
)

// DetectionResult holds one detected instruction.
type DetectionResult struct {
	Content  string
	Category string
}

type detectionRule struct {
	pattern  *regexp.Regexp
	category string
}

var detectionRules = []detectionRule{
	// Rule patterns: "always X", "never X", "from now on X", "do not X"
	{regexp.MustCompile(`(?i)\balways\s+(.+?)(?:\.|!|$)`), CategoryRule},
	{regexp.MustCompile(`(?i)\bfrom now on,?\s+(.+?)(?:\.|!|$)`), CategoryRule},
	{regexp.MustCompile(`(?i)\bnever\s+(.+?)(?:\.|!|$)`), CategoryRule},
	{regexp.MustCompile(`(?i)\bdon'?t\s+ever\s+(.+?)(?:\.|!|$)`), CategoryRule},
	{regexp.MustCompile(`(?i)\bdo\s+not\s+(.+?)(?:\.|!|$)`), CategoryRule},
	{regexp.MustCompile(`(?i)\bplease\s+(?:always|make sure to)\s+(.+?)(?:\.|!|$)`), CategoryRule},

	// Style patterns: "reply in X", "use a X tone", "be more X"
	{regexp.MustCompile(`(?i)\b(?:reply|respond|answer)\s+(?:in|with|using)\s+(.+?)(?:\.|!|$)`), CategoryStyle},
	{regexp.MustCompile(`(?i)\buse\s+(?:a\s+)?(\w+)\s+(?:tone|style|format|voice)(?:\.|!|$)`), CategoryStyle},
	{regexp.MustCompile(`(?i)\bbe\s+(?:more\s+)?(concise|verbose|formal|informal|casual|brief|detailed)(?:\.|!|$)`), CategoryStyle},

	// Behavior patterns: "when I say/ask X, do Y"
	{regexp.MustCompile(`(?i)\bwhen(?:ever)?\s+(?:i|I)\s+(?:say|ask|tell you)\s+(.+?)(?:\.|!|$)`), CategoryBehavior},

	// Knowledge patterns: "remember that X", "keep in mind X", "note that X"
	{regexp.MustCompile(`(?i)\bremember\s+that\s+(.+?)(?:\.|!|$)`), CategoryKnowledge},
	{regexp.MustCompile(`(?i)\bkeep\s+in\s+mind\s+(?:that\s+)?(.+?)(?:\.|!|$)`), CategoryKnowledge},
	{regexp.MustCompile(`(?i)\bnote\s+that\s+(.+?)(?:\.|!|$)`), CategoryKnowledge},

	// Preference patterns: "I want you to X"
	{regexp.MustCompile(`(?i)\bi\s+(?:want|need)\s+you\s+to\s+(.+?)(?:\.|!|$)`), CategoryPreference},
}

// DetectInstructions scans text for instruction-like patterns.
// Returns detected instructions (may be empty).
func DetectInstructions(text string) []DetectionResult {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}

	var results []DetectionResult
	seen := make(map[string]bool)

	for _, rule := range detectionRules {
		matches := rule.pattern.FindStringSubmatch(text)
		if matches == nil || len(matches) < 2 {
			continue
		}

		captured := strings.TrimSpace(matches[1])
		if captured == "" {
			continue
		}

		// Require minimum length to avoid false positives.
		words := strings.Fields(captured)
		if len(words) < 3 {
			continue
		}

		// Use full match as instruction content (more natural).
		fullMatch := strings.TrimSpace(matches[0])
		// Clean trailing punctuation for consistency.
		fullMatch = strings.TrimRight(fullMatch, ".!,;")

		if seen[fullMatch] {
			continue
		}
		seen[fullMatch] = true

		results = append(results, DetectionResult{
			Content:  fullMatch,
			Category: rule.category,
		})
	}

	return results
}

// Detector wraps a Store to implement the InstructionDetector interface
// expected by the memory package.
type Detector struct {
	store *Store
}

// NewDetector creates an instruction detector.
func NewDetector(store *Store) *Detector {
	return &Detector{store: store}
}

// DetectAndSave scans text for instruction patterns and saves any found.
// Returns the count of newly saved instructions.
func (d *Detector) DetectAndSave(ctx context.Context, text string) (int, error) {
	results := DetectInstructions(text)
	if len(results) == 0 {
		return 0, nil
	}

	saved := 0
	for _, r := range results {
		_, err := d.store.Save(ctx, r.Content, r.Category, 30, ScopeGlobal, "", SourceAuto, text)
		if err == nil {
			saved++
		}
		// Ignore duplicate errors silently.
	}
	return saved, nil
}
