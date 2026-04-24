package stopphrase

import (
	"strings"

	"github.com/LumabyteCo/aibutler/internal/i18n"
	"golang.org/x/text/unicode/norm"
)

// Action is the result of matching a stop phrase.
type Action string

const (
	ActionNone   Action = ""
	ActionStop   Action = "stop"
	ActionCancel Action = "cancel"
)

type phrase struct {
	normalized string
	action     Action
}

// Matcher detects stop phrases with fuzzy matching and Unicode normalization.
type Matcher struct {
	phrases map[string][]phrase // lang → phrases
	all     []phrase            // flattened for language-agnostic matching
	custom  []phrase
	bundle  *i18n.Bundle
}

// NewMatcher creates a matcher with embedded 14-language phrases.
func NewMatcher(bundle *i18n.Bundle) *Matcher {
	m := &Matcher{
		phrases: make(map[string][]phrase),
		bundle:  bundle,
	}

	// Build cancel set for quick lookup.
	cancelSet := make(map[string]map[string]bool)
	for lang, phrases := range cancelPhrases {
		cancelSet[lang] = make(map[string]bool)
		for _, p := range phrases {
			cancelSet[lang][normalize(p)] = true
		}
	}

	// Build phrase index.
	for lang, phrases := range defaultPhrases {
		for _, p := range phrases {
			n := normalize(p)
			action := ActionStop
			if cs, ok := cancelSet[lang]; ok && cs[n] {
				action = ActionCancel
			}
			entry := phrase{normalized: n, action: action}
			m.phrases[lang] = append(m.phrases[lang], entry)
			m.all = append(m.all, entry)
		}
	}

	return m
}

// AddCustom adds user-defined stop phrases.
func (m *Matcher) AddCustom(phrases []string, action Action) {
	for _, p := range phrases {
		entry := phrase{normalized: normalize(p), action: action}
		m.custom = append(m.custom, entry)
	}
}

// Check tests if the input text is a stop phrase.
// Returns the detected action and the matched language (empty if no match).
func (m *Matcher) Check(text string) (Action, string) {
	n := normalize(text)
	if n == "" {
		return ActionNone, ""
	}

	// Check custom phrases first.
	for _, p := range m.custom {
		if n == p.normalized {
			return p.action, ""
		}
	}

	// Check per-language phrases (returns specific language).
	for lang, phrases := range m.phrases {
		for _, p := range phrases {
			if n == p.normalized {
				return p.action, lang
			}
		}
	}

	return ActionNone, ""
}

// normalize applies Unicode NFKC normalization, case folding, and whitespace trimming.
func normalize(s string) string {
	s = strings.TrimSpace(s)
	s = norm.NFKC.String(s)
	s = strings.ToLower(s)
	return s
}
