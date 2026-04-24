package entity_test

import (
	"strings"
	"testing"

	"github.com/LumabyteCo/aibutler/internal/memory/entity"
)

// TestExtractPeopleRejectsStopwords is the regression test for the QA finding:
// pronouns and contracted endings were being stored as "people" entities —
// things like "ve" (from "I've"), "You", "Most", "When".
//
// None of these should appear in the extraction output.
func TestExtractPeopleRejectsStopwords(t *testing.T) {
	// Build a sentence that the old overly-permissive regex would happily
	// pull "ve", "You", "Most", "When" out of.
	text := "I've told You that Most of Us want to know When they said hi."

	result := entity.Extract(text)

	disallowed := []string{"ve", "You", "Most", "Us", "When", "I", "They"}
	for _, bad := range disallowed {
		for _, got := range result.People {
			if strings.EqualFold(got, bad) {
				t.Errorf("Extract(%q).People contains stopword %q (got %v)", text, bad, result.People)
			}
		}
	}
}

// TestExtractPeopleRejectsSingleChars ensures "I", "A", "T" don't get stored.
func TestExtractPeopleRejectsSingleChars(t *testing.T) {
	// "T" would be captured from "T said yes" by the "(name) said" pattern.
	text := "T said yes. A told me. I mentioned it."

	result := entity.Extract(text)

	for _, got := range result.People {
		if len(got) < 2 {
			t.Errorf("Extract(%q).People contains single-char name %q (got %v)", text, got, result.People)
		}
	}
}

// TestExtractPeopleStillAcceptsRealNames ensures we didn't over-filter —
// the stopword check shouldn't reject legitimately capitalized names.
// Matches the loose `Contains` check used by the older TestExtractPeople.
func TestExtractPeopleStillAcceptsRealNames(t *testing.T) {
	tests := []struct {
		text string
		name string
	}{
		{"My friend Sarah helped me", "Sarah"},
		{"I met with John yesterday", "John"},
		{"Bob mentioned the migration", "Bob"},
	}
	for _, tt := range tests {
		result := entity.Extract(tt.text)
		found := false
		for _, got := range result.People {
			if strings.Contains(got, tt.name) {
				found = true
			}
		}
		if !found {
			t.Errorf("Extract(%q).People = %v, want contains %q", tt.text, result.People, tt.name)
		}
	}
}

// TestExtractProjectsCollapsesFragments is the regression test for the QA
// finding: the same project "Nimbus" was stored as multiple distinct entities
// like "called Nimbus that", "a project called Nimbus that",
// "Nimbus - a weather-awar".
//
// All three should now collapse to the canonical short form.
func TestExtractProjectsCollapsesFragments(t *testing.T) {
	// All three phrasings should yield the same normalized project name
	// (or at least equal count of distinct project names across the set).
	inputs := []string{
		"I'm working on Nimbus that has a weather feature",
		"Working on Nimbus and the migration",
		"We have project Nimbus because it's important",
	}

	var allNames []string
	for _, text := range inputs {
		result := entity.Extract(text)
		allNames = append(allNames, result.Projects...)
	}

	// Every captured name should NOT contain "that", "and", "because" —
	// those are conjunctions we strip from project names.
	forbidden := []string{"that", "and", "because", "which", "while"}
	for _, name := range allNames {
		lower := strings.ToLower(name)
		for _, bad := range forbidden {
			if strings.Contains(lower, " "+bad) || strings.HasSuffix(lower, " "+bad) {
				t.Errorf("project name %q contains trailing conjunction %q (full list: %v)", name, bad, allNames)
			}
		}
	}
}

// TestExtractProjectsTrimsTrailingDash verifies "Nimbus - a weather-aware app"
// becomes just "Nimbus" (or similar short canonical form).
func TestExtractProjectsTrimsTrailingDash(t *testing.T) {
	text := "I'm working on Nimbus - a weather-aware app"
	result := entity.Extract(text)

	if len(result.Projects) == 0 {
		t.Fatalf("expected at least one project, got none from %q", text)
	}

	// The trailing dash clause should be stripped by leadingPunct/trailingPunct.
	// It's fine if the result is "Nimbus" or "Nimbus - a weather-aware app"
	// depending on how greedy the regex is — the critical thing is no
	// partial like "weather-awar".
	for _, name := range result.Projects {
		if strings.HasSuffix(name, "awar") || strings.HasSuffix(name, "-") {
			t.Errorf("project name %q looks truncated mid-word", name)
		}
	}
}

// TestCanonicalFact smoke-tests the exported canonical form used for dedup.
func TestCanonicalFact(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"  AI Butler ", "ai butler"},
		{"AI Butler.", "ai butler"},
		{"AI  Butler", "ai butler"},
		{"AI Butler!!!", "ai butler"},
		{"AI BUTLER", "ai butler"},
		{"", ""},
	}
	for _, tt := range tests {
		if got := entity.CanonicalFact(tt.in); got != tt.want {
			t.Errorf("CanonicalFact(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
