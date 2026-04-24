package i18n_test

import (
	"sort"
	"testing"

	"github.com/LumabyteCo/aibutler/internal/i18n"
)

func TestTranslateEnglish(t *testing.T) {
	b := i18n.New("en")
	got := b.T("en", "stop.confirmed")
	if got != "Okay, I've stopped." {
		t.Errorf("got %q", got)
	}
}

func TestTranslateFrench(t *testing.T) {
	b := i18n.New("en")
	got := b.T("fr", "stop.confirmed")
	if got != "D'accord, j'ai arrêté." {
		t.Errorf("got %q", got)
	}
}

func TestFallbackToEnglish(t *testing.T) {
	b := i18n.New("en")
	// "mcp.tool_not_found" exists in English but not in French.
	got := b.T("fr", "mcp.tool_not_found")
	want := "Tool %s not found on server %s."
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestFallbackMissingKey(t *testing.T) {
	b := i18n.New("en")
	got := b.T("en", "nonexistent.key")
	if got != "nonexistent.key" {
		t.Errorf("got %q, want key returned as-is", got)
	}
}

func TestTFFormatting(t *testing.T) {
	b := i18n.New("en")
	got := b.TF("en", "media.too_large", 20)
	want := "File is too large (max 20 MB)."
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestLanguages(t *testing.T) {
	b := i18n.New("en")
	langs := b.Languages()
	if len(langs) != 14 {
		t.Errorf("got %d languages, want 14", len(langs))
	}
	sort.Strings(langs)
	expected := []string{"ar", "de", "en", "es", "fr", "hi", "it", "ja", "ko", "nl", "pt", "ru", "tr", "zh"}
	for i, l := range expected {
		if langs[i] != l {
			t.Errorf("langs[%d] = %q, want %q", i, langs[i], l)
		}
	}
}

func TestHasLanguage(t *testing.T) {
	b := i18n.New("en")
	if !b.HasLanguage("en") {
		t.Error("expected HasLanguage(en) = true")
	}
	if !b.HasLanguage("ar") {
		t.Error("expected HasLanguage(ar) = true")
	}
	if b.HasLanguage("xx") {
		t.Error("expected HasLanguage(xx) = false")
	}
}

func TestArabicRTL(t *testing.T) {
	b := i18n.New("en")
	got := b.T("ar", "stop.confirmed")
	want := "حسنًا، لقد توقفت."
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
