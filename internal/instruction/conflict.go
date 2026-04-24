package instruction

import (
	"context"
	"fmt"
	"strings"
)

// ConflictResult describes a detected conflict between instructions.
type ConflictResult struct {
	ExistingID      int64
	ExistingContent string
	Reason          string
}

// antonymPairs maps words to their opposites.
var antonymPairs = map[string]string{
	"verbose":  "concise",
	"concise":  "verbose",
	"brief":    "detailed",
	"detailed": "brief",
	"formal":   "informal",
	"informal": "formal",
	"casual":   "formal",
	"long":     "short",
	"short":    "long",
}

// CheckConflicts compares a new instruction against existing active instructions.
// Returns any detected conflicts (antonym pairs or direct negation).
func (s *Store) CheckConflicts(ctx context.Context, newContent string) ([]ConflictResult, error) {
	existing, err := s.List(ctx, ListQuery{ActiveOnly: true})
	if err != nil {
		return nil, err
	}

	var conflicts []ConflictResult
	newLower := strings.ToLower(newContent)

	for _, inst := range existing {
		existLower := strings.ToLower(inst.Content)

		// Check direct negation: "always X" vs "never X".
		if reason := checkNegation(newLower, existLower); reason != "" {
			conflicts = append(conflicts, ConflictResult{
				ExistingID:      inst.ID,
				ExistingContent: inst.Content,
				Reason:          reason,
			})
			continue
		}

		// Check antonym pairs.
		if reason := checkAntonyms(newLower, existLower); reason != "" {
			conflicts = append(conflicts, ConflictResult{
				ExistingID:      inst.ID,
				ExistingContent: inst.Content,
				Reason:          reason,
			})
		}
	}

	return conflicts, nil
}

// checkNegation detects "always X" vs "never X" patterns.
func checkNegation(a, b string) string {
	aStripped := stripPrefix(a)
	bStripped := stripPrefix(b)

	if aStripped == "" || bStripped == "" {
		return ""
	}

	// Same content but one has "always" and other has "never"
	if aStripped == bStripped {
		aHasAlways := strings.HasPrefix(a, "always ")
		aHasNever := strings.HasPrefix(a, "never ")
		bHasAlways := strings.HasPrefix(b, "always ")
		bHasNever := strings.HasPrefix(b, "never ")

		if (aHasAlways && bHasNever) || (aHasNever && bHasAlways) {
			return fmt.Sprintf("direct negation: both reference %q", aStripped)
		}
	}

	return ""
}

// checkAntonyms checks if instructions contain opposing words.
func checkAntonyms(a, b string) string {
	aWords := strings.Fields(a)
	bWords := strings.Fields(b)

	for _, aw := range aWords {
		antonym, ok := antonymPairs[aw]
		if !ok {
			continue
		}
		for _, bw := range bWords {
			if bw == antonym {
				return fmt.Sprintf("antonym conflict: %q vs %q", aw, antonym)
			}
		}
	}
	return ""
}

// stripPrefix removes common instruction prefixes for comparison.
func stripPrefix(s string) string {
	prefixes := []string{
		"always ", "never ", "do not ", "don't ", "don't ever ",
		"from now on ", "from now on, ", "please always ", "please ",
	}
	for _, p := range prefixes {
		if strings.HasPrefix(s, p) {
			return strings.TrimSpace(s[len(p):])
		}
	}
	return ""
}
