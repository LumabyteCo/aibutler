package voice

import (
	"strings"
	"unicode"
)

// MatchWakeWord reports whether a transcript contains the wake phrase.
// Matching is deliberately fuzzy because small local STT models mishear
// names: "Butler" is commonly transcribed as "Buggler" or "Butter". The
// final keyword token is the anchor, and it must appear near the start of
// the utterance (first three tokens, including a split pair like "but ler").
func MatchWakeWord(text, keyword string) bool {
	text = normalizeSpeech(text)
	keyword = normalizeSpeech(keyword)
	if text == "" {
		return false
	}
	if keyword == "" {
		keyword = "hey butler"
	}

	// Exact phrase (word-boundary safe after normalization).
	if strings.Contains(" "+text+" ", " "+keyword+" ") {
		return true
	}

	words := strings.Fields(keyword)
	anchor := words[len(words)-1]
	anchorRunes := []rune(anchor)
	if len(anchorRunes) < 4 {
		return false
	}

	tokens := strings.Fields(text)
	limit := len(tokens)
	if limit > 3 {
		limit = 3
	}
	for i := 0; i < limit; i++ {
		if fuzzyWordMatch(tokens[i], anchor) {
			return true
		}
		// STT sometimes splits a name: "but ler".
		if i+1 < len(tokens) && i+1 < limit+1 {
			if fuzzyWordMatch(tokens[i]+tokens[i+1], anchor) {
				return true
			}
		}
	}
	return false
}

func normalizeSpeech(s string) string {
	s = strings.ToLower(s)
	var b strings.Builder
	lastSpace := true
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			lastSpace = false
			continue
		}
		if !lastSpace {
			b.WriteByte(' ')
			lastSpace = true
		}
	}
	return strings.TrimSpace(b.String())
}

// fuzzyWordMatch allows small edit distances while keeping the first letter
// and rough length as guardrails.
func fuzzyWordMatch(got, want string) bool {
	if got == "" || want == "" {
		return false
	}
	if got == want {
		return true
	}
	g, w := []rune(got), []rune(want)
	if len(g) == 0 || len(w) == 0 || g[0] != w[0] {
		return false
	}
	if absInt(len(g)-len(w)) > 2 {
		return false
	}
	maxDist := 1
	if len(w) >= 6 {
		maxDist = 2
	}
	return levenshtein(g, w) <= maxDist
}

func levenshtein(a, b []rune) int {
	if len(a) == 0 {
		return len(b)
	}
	if len(b) == 0 {
		return len(a)
	}
	prev := make([]int, len(b)+1)
	cur := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			cur[j] = minInt(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev, cur = cur, prev
	}
	return prev[len(b)]
}

func minInt(vals ...int) int {
	m := vals[0]
	for _, v := range vals[1:] {
		if v < m {
			m = v
		}
	}
	return m
}

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
