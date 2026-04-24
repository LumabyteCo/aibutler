package stopphrase_test

import (
	"testing"

	"github.com/LumabyteCo/aibutler/internal/i18n"
	"github.com/LumabyteCo/aibutler/internal/stopphrase"
)

func newMatcher() *stopphrase.Matcher {
	b := i18n.New("en")
	return stopphrase.NewMatcher(b)
}

func TestEnglishStop(t *testing.T) {
	m := newMatcher()
	action, lang := m.Check("stop")
	if action != stopphrase.ActionStop {
		t.Errorf("action = %q, want stop", action)
	}
	if lang == "" {
		t.Error("expected a language match")
	}
	// "stop" exists in en, de, it, nl — any is valid.
}

func TestEnglishQuit(t *testing.T) {
	m := newMatcher()
	action, lang := m.Check("quit")
	if action != stopphrase.ActionStop {
		t.Errorf("action = %q, want stop", action)
	}
	if lang != "en" {
		t.Errorf("lang = %q, want en", lang)
	}
}

func TestEnglishCancel(t *testing.T) {
	m := newMatcher()
	action, _ := m.Check("cancel")
	if action != stopphrase.ActionCancel {
		t.Errorf("action = %q, want cancel", action)
	}
}

func TestFrenchStop(t *testing.T) {
	m := newMatcher()
	action, lang := m.Check("arrête")
	if action == stopphrase.ActionNone {
		t.Error("expected match for French stop")
	}
	if lang != "fr" {
		t.Errorf("lang = %q, want fr", lang)
	}
}

func TestSpanishStop(t *testing.T) {
	m := newMatcher()
	// "detener" is unique to es in the stop phrase set.
	action, lang := m.Check("detener")
	if action == stopphrase.ActionNone {
		t.Error("expected match for Spanish stop")
	}
	if lang != "es" {
		t.Errorf("lang = %q, want es", lang)
	}
}

func TestArabicStop(t *testing.T) {
	m := newMatcher()
	action, lang := m.Check("توقف")
	if action == stopphrase.ActionNone {
		t.Error("expected match for Arabic stop")
	}
	if lang != "ar" {
		t.Errorf("lang = %q, want ar", lang)
	}
}

func TestJapaneseStop(t *testing.T) {
	m := newMatcher()
	action, lang := m.Check("やめて")
	if action == stopphrase.ActionNone {
		t.Error("expected match for Japanese stop")
	}
	if lang != "ja" {
		t.Errorf("lang = %q, want ja", lang)
	}
}

func TestCaseInsensitive(t *testing.T) {
	m := newMatcher()
	for _, input := range []string{"STOP", "Stop", "sToP"} {
		action, _ := m.Check(input)
		if action == stopphrase.ActionNone {
			t.Errorf("expected match for %q", input)
		}
	}
}

func TestUnicodeNormalization(t *testing.T) {
	m := newMatcher()
	// Decomposed form of "arrête" (a + r + r + e + combining-circumflex + t + e).
	decomposed := "arre\u0302te"
	action, _ := m.Check(decomposed)
	if action == stopphrase.ActionNone {
		t.Error("expected match for decomposed arrête")
	}
}

func TestWhitespaceTrimming(t *testing.T) {
	m := newMatcher()
	action, _ := m.Check("  stop  ")
	if action == stopphrase.ActionNone {
		t.Error("expected match with whitespace")
	}
}

func TestNotAStopPhrase(t *testing.T) {
	m := newMatcher()
	action, _ := m.Check("hello")
	if action != stopphrase.ActionNone {
		t.Errorf("expected no match, got %q", action)
	}
}

func TestCustomPhrase(t *testing.T) {
	m := newMatcher()
	m.AddCustom([]string{"halt"}, stopphrase.ActionStop)
	action, _ := m.Check("halt")
	if action != stopphrase.ActionStop {
		t.Errorf("action = %q, want stop", action)
	}
}

func TestEmptyInput(t *testing.T) {
	m := newMatcher()
	action, _ := m.Check("")
	if action != stopphrase.ActionNone {
		t.Errorf("expected none for empty, got %q", action)
	}
}

func TestMultiWordPhrase(t *testing.T) {
	m := newMatcher()
	action, _ := m.Check("never mind")
	if action == stopphrase.ActionNone {
		t.Error("expected match for 'never mind'")
	}
}
