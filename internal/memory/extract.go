package memory

import (
	"regexp"
	"strings"
)

// Fact categories.
const (
	CategoryPreference = "preference"
	CategoryIdentity   = "identity"
	CategoryProject    = "project"
	CategoryDecision   = "decision"
)

// ExtractionResult holds one extracted fact.
//
// Key is a canonical subject.attribute slug (e.g. "user.location") set only
// for facts describing a single-valued attribute — a person has one name, one
// age, one current city. Facts sharing a Key are mutually exclusive: storing a
// new one supersedes the old. Multi-valued categories (preferences, decisions,
// projects) leave Key empty and accumulate without conflict.
type ExtractionResult struct {
	Fact     string
	Category string
	Key      string
}

type extractionRule struct {
	pattern  *regexp.Regexp
	category string
	format   string // Go fmt template using %s for capture group
	key      string // canonical attribute slug; "" for multi-valued facts
}

var extractionRules = []extractionRule{
	// Identity patterns — single-valued attributes carry a key so a later
	// contradicting statement replaces rather than accumulates.
	{regexp.MustCompile(`(?i)\bmy name is\s+(.+?)(?:\.|,|$)`), CategoryIdentity, "User's name is %s", "user.name"},
	{regexp.MustCompile(`(?i)\bi(?:'m| am)\s+called\s+(.+?)(?:\.|,|$)`), CategoryIdentity, "User is called %s", "user.name"},
	{regexp.MustCompile(`(?i)\bUser lives in\s+(.+?)(?:\.|,|$)`), CategoryIdentity, "User lives in %s", "user.location"},
	{regexp.MustCompile(`(?i)\bi live in\s+(.+?)(?:\.|,|$)`), CategoryIdentity, "User lives in %s", "user.location"},
	// Moved/relocated — the user announcing a NEW home is the same shape as
	// "live in X": supersede the old location, never accumulate.
	// "I moved to Tokyo", "I now live in Tokyo", "my home is in Tokyo",
	// "I've moved to Tokyo", "I'm living in Tokyo now".
	{regexp.MustCompile(`(?i)\bi(?:'ve| have)?\s+moved to\s+(.+?)(?:\.|,|$)`), CategoryIdentity, "User lives in %s (moved recently)", "user.location"},
	// Third-person shape: when the agent paraphrases a user statement into
	// a memory ("Sam moved to Tokyo", "User lives in Berlin"), the key must
	// still match so supersession fires. This is what the model's own
	// reply format looks like — and the memory_capture path needs a key too.
	// The single %s receives "Person lives in City" — the capture groups are
	// merged into "Subject lives in Location".
	{regexp.MustCompile(`(?i)\b(\w[\w ]*?)\s+moved to\s+(.+?)(?:\.|,|$)`), CategoryIdentity, "{{SUBJECT_LOC}}", "user.location"},
	{regexp.MustCompile(`(?i)\bi(?:'m| am)?\s+(?:now )?(?:live|living|located) in\s+(.+?)(?:\.|,|$)`), CategoryIdentity, "User lives in %s", "user.location"},
	{regexp.MustCompile(`(?i)\bmy home (?:is|city is)\s+(?:in\s+)?(.+?)(?:\.|,|$)`), CategoryIdentity, "User lives in %s", "user.location"},
	{regexp.MustCompile(`(?i)\bi(?:'m| am) (?:a |an )?(\w+(?:\s+\w+)?)\s+(?:developer|engineer|designer|manager|student|teacher|doctor|nurse|lawyer|writer|artist)`), CategoryIdentity, "User is a %s", "user.profession"},
	{regexp.MustCompile(`(?i)\bi(?:'m| am)\s+(\d+)\s+years?\s+old`), CategoryIdentity, "User is %s years old", "user.age"},
	{regexp.MustCompile(`(?i)\bi work (?:at|for)\s+(.+?)(?:\.|,|$)`), CategoryIdentity, "User works at %s", "user.employer"},

	// Preference patterns — multi-valued except favorites, which are
	// single-valued per subject ("my favorite editor is X" replaces the
	// previous favorite editor, not the favorite color). The favorites rule
	// derives its key from the first capture at extraction time.
	{regexp.MustCompile(`(?i)\bi prefer\s+(.+?)(?:\.|,|$)`), CategoryPreference, "User prefers %s", ""},
	{regexp.MustCompile(`(?i)\bi(?:'d| would) rather\s+(.+?)(?:\.|,|$)`), CategoryPreference, "User would rather %s", ""},
	{regexp.MustCompile(`(?i)\bmy favorite\s+(\w+(?:\s+\w+)?)\s+is\s+(.+?)(?:\.|,|$)`), CategoryPreference, "", ""},
	{regexp.MustCompile(`(?i)\bi always\s+(.+?)(?:\.|,|$)`), CategoryPreference, "User always %s", ""},
	{regexp.MustCompile(`(?i)\bi like\s+(.+?)(?:\.|,|$)`), CategoryPreference, "User likes %s", ""},

	// Decision patterns — multi-valued; different decisions coexist.
	{regexp.MustCompile(`(?i)\bi(?:'ve| have) decided\s+(?:to\s+)?(.+?)(?:\.|,|$)`), CategoryDecision, "User decided to %s", ""},
	{regexp.MustCompile(`(?i)\b(?:let's|let us) go with\s+(.+?)(?:\.|,|$)`), CategoryDecision, "Decided to go with %s", ""},
	{regexp.MustCompile(`(?i)\bi(?:'ll| will) go with\s+(.+?)(?:\.|,|$)`), CategoryDecision, "User will go with %s", ""},
	{regexp.MustCompile(`(?i)\bthe decision is\s+(.+?)(?:\.|,|$)`), CategoryDecision, "Decision: %s", ""},

	// Project patterns — multi-valued; people work on several things.
	{regexp.MustCompile(`(?i)\bi(?:'m| am) (?:working on|building)\s+(.+?)(?:\.|,|$)`), CategoryProject, "User is working on %s", ""},
	{regexp.MustCompile(`(?i)\bwe(?:'re| are) (?:working on|building)\s+(.+?)(?:\.|,|$)`), CategoryProject, "Working on %s", ""},
	{regexp.MustCompile(`(?i)\bthe project is\s+(?:called\s+)?(.+?)(?:\.|,|$)`), CategoryProject, "Project: %s", ""},
	{regexp.MustCompile(`(?i)\b(?:this|our) project (?:uses|is about)\s+(.+?)(?:\.|,|$)`), CategoryProject, "Project %s", ""},
}

// favoriteKey builds the per-subject key for "my favorite X is Y" facts:
// lowercase, inner whitespace collapsed to '_' ("coffee shop" → "coffee_shop").
func favoriteKey(subject string) string {
	fields := strings.Fields(strings.ToLower(subject))
	return "user.favorite." + strings.Join(fields, "_")
}

// ExtractKeyFacts applies rule-based pattern matching to extract facts from text.
// No LLM calls — pure regex matching.
func ExtractKeyFacts(text string) []ExtractionResult {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}

	var results []ExtractionResult
	seen := make(map[string]bool)

	for _, rule := range extractionRules {
		// FindAll (not FindStringSubmatch) so a rule that matches several times in
		// one text — e.g. "I like tea. I like coffee." — yields a fact per match
		// instead of only the first.
		for _, matches := range rule.pattern.FindAllStringSubmatch(text, -1) {
			var fact string
			key := rule.key
			if rule.format == "" {
				// Special case for "my favorite X is Y" (2 capture groups)
				if len(matches) >= 3 {
					subject := strings.TrimSpace(matches[1])
					fact = "User's favorite " + subject + " is " + strings.TrimSpace(matches[2])
					key = favoriteKey(subject)
				} else {
					continue
				}
			} else if rule.format == "{{SUBJECT_LOC}}" {
				// Third-person: "Sam moved to Tokyo" → "Sam lives in Tokyo"
				// (two captures; merge into one fact)
				if len(matches) >= 3 {
					subject := strings.TrimSpace(matches[1])
					loc := strings.TrimSpace(matches[2])
					fact = subject + " lives in " + loc
				} else {
					continue
				}
			} else {
				captured := strings.TrimSpace(matches[1])
				if captured == "" {
					continue
				}
				fact = strings.Replace(rule.format, "%s", captured, 1)
			}

			if !seen[fact] {
				seen[fact] = true
				results = append(results, ExtractionResult{
					Fact:     fact,
					Category: rule.category,
					Key:      key,
				})
			}
		}
	}

	return results
}
