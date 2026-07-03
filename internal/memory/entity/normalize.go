// Normalization and validation for extracted entity names.
// Filters out the noise that overly-permissive regex patterns pull in:
// pronouns, helper verbs, common sentence fragments, and trailing clauses.
package entity

import (
	"regexp"
	"strings"
	"unicode"
)

// personStopwords rejects strings that the regex can capture in practice
// but are never plausible person names. The check is case-insensitive.
//
// These are drawn from the noise we actually see in extraction:
// pronouns, contracted endings ("ve", "ll", "re"), auxiliaries,
// determiners, generic conversation words, and common sentence starters
// that happen to be capitalized.
var personStopwords = map[string]struct{}{
	// Pronouns (subject, object, possessive).
	"i": {}, "me": {}, "my": {}, "mine": {}, "myself": {},
	"you": {}, "your": {}, "yours": {}, "yourself": {},
	"he": {}, "him": {}, "his": {}, "himself": {},
	"she": {}, "her": {}, "hers": {}, "herself": {},
	"it": {}, "its": {}, "itself": {},
	"we": {}, "us": {}, "our": {}, "ours": {}, "ourselves": {},
	"they": {}, "them": {}, "their": {}, "theirs": {}, "themselves": {},

	// Contraction leftovers (from "I've", "we'll", "they're" etc. when
	// the apostrophe/lowercase start gets stripped).
	"ve": {}, "ll": {}, "re": {}, "s": {}, "d": {}, "m": {}, "t": {},

	// Auxiliaries and modals.
	"am": {}, "is": {}, "are": {}, "was": {}, "were": {}, "be": {}, "been": {}, "being": {},
	"do": {}, "does": {}, "did": {}, "doing": {}, "done": {},
	"have": {}, "has": {}, "had": {}, "having": {},
	"will": {}, "would": {}, "shall": {}, "should": {},
	"can": {}, "could": {}, "may": {}, "might": {}, "must": {},

	// Determiners and quantifiers.
	"a": {}, "an": {}, "the": {}, "this": {}, "that": {}, "these": {}, "those": {},
	"some": {}, "any": {}, "all": {}, "each": {}, "every": {}, "few": {}, "many": {}, "much": {},
	"most": {}, "other": {}, "another": {}, "such": {}, "same": {},

	// Common sentence fragments that can be captured as single capitalized words.
	"when": {}, "where": {}, "why": {}, "how": {}, "what": {}, "who": {}, "which": {},
	"if": {}, "then": {}, "so": {}, "because": {}, "since": {}, "while": {},
	"yes": {}, "no": {}, "ok": {}, "okay": {}, "sure": {}, "maybe": {},
	"hello": {}, "hi": {}, "hey": {}, "thanks": {},

	// Generic task/noun words that leak through.
	"todo": {}, "note": {}, "fyi": {}, "tbd": {}, "wip": {},
	"today": {}, "tomorrow": {}, "yesterday": {}, "now": {}, "later": {},
	"everyone": {}, "someone": {}, "anyone": {}, "nobody": {}, "somebody": {},
}

// nameFragmentPattern matches the start of a clause/conjunction that signals
// the captured "name" has run into the next part of the sentence. We strip
// from the first match onward. Case-insensitive; word-boundaried.
//
// Example: "Nimbus that weather-aware scheduling" → "Nimbus"
//
//	"AI Butler and the swarm" → "AI Butler"
var nameFragmentPattern = regexp.MustCompile(
	`(?i)\s+(?:that|which|who|whose|whom|where|when|and|but|or|for|with|without|because|since|while|although|though|so|thus|hence|then|after|before|until|unless|if|as)\b.*$`,
)

// trailingPunct trims punctuation the regex pulls in from the end of a match.
// Includes ASCII hyphen plus unicode en-dash (U+2013) and em-dash (U+2014).
var trailingPunct = regexp.MustCompile("[\\s\\.,:;!?\\-–—'\"\\)\\]]+$")

// leadingPunct trims punctuation from the start of a match.
var leadingPunct = regexp.MustCompile("^[\\s\\.,:;!?\\-–—'\"\\(\\[]+")

// isPlausiblePerson validates that a captured string could actually be a
// person name. Conservative: false-negatives are fine (we lose a few legit
// names), false-positives pollute the memory (we stored "ve" as a person).
func isPlausiblePerson(name string) bool {
	clean := strings.TrimSpace(name)
	if clean == "" {
		return false
	}

	// Reject if it's a stopword (case-insensitive).
	if _, bad := personStopwords[strings.ToLower(clean)]; bad {
		return false
	}

	// Minimum length: 2 characters. Real names like "Li", "Bo", "Jo" exist
	// but single letters are always noise.
	if len(clean) < 2 {
		return false
	}

	// Must start with an uppercase letter — regex already enforces this
	// but defensive check in case of direct calls.
	first := []rune(clean)[0]
	if !unicode.IsUpper(first) {
		return false
	}

	// Reject if it contains digits — real names don't.
	for _, r := range clean {
		if unicode.IsDigit(r) {
			return false
		}
	}

	return true
}

// normalizeProjectName strips trailing clauses and punctuation so that
// "Nimbus", "Nimbus that", and "Nimbus that weather-aware" all collapse
// to the same canonical "Nimbus".
func normalizeProjectName(name string) string {
	clean := strings.TrimSpace(name)
	clean = leadingPunct.ReplaceAllString(clean, "")
	clean = nameFragmentPattern.ReplaceAllString(clean, "")
	clean = trailingPunct.ReplaceAllString(clean, "")
	return strings.TrimSpace(clean)
}

// isPlausibleProject validates that a captured string is a non-trivial
// project name. Same philosophy as isPlausiblePerson: conservative filter.
func isPlausibleProject(name string) bool {
	clean := strings.TrimSpace(name)
	if clean == "" {
		return false
	}
	// Minimum length: 2 characters after normalization.
	if len(clean) < 2 {
		return false
	}
	// Reject pure stopwords (rare but possible after the clause stripper).
	if _, bad := personStopwords[strings.ToLower(clean)]; bad {
		return false
	}
	return true
}

// CanonicalFact reduces a fact string to a canonical form suitable for
// dedup: lowercased, whitespace-collapsed, trailing punctuation stripped.
// Exported for SaveKeyFact dedup lookup.
func CanonicalFact(fact string) string {
	clean := strings.ToLower(strings.TrimSpace(fact))
	clean = trailingPunct.ReplaceAllString(clean, "")
	// Collapse internal whitespace runs to single spaces.
	clean = regexp.MustCompile(`\s+`).ReplaceAllString(clean, " ")
	return clean
}
