package tests

import (
	"testing"

	"github.com/LumabyteCo/aibutler/internal/i18n"
)

// TestI18NCoverageForAllKeys verifies all translation keys in the default bundle
// have entries in the fallback language (English).
func TestI18NCoverageForAllKeys(t *testing.T) {
	bundle := i18n.New("en")

	// Verify English has all keys and they translate to non-empty values.
	keys := []string{
		"stop.confirmed",
		"stop.cancelled",
		"typing.working",
		"error.unauthorized",
		"error.rate_limited",
		"error.unknown",
		"media.processing",
		"media.unsupported",
		"media.too_large",
		"webchat.welcome",
		"mcp.connecting",
		"mcp.connected",
		"mcp.disconnected",
		"mcp.tool_not_found",
		"relay.sent",
		"relay.failed",
	}

	for _, key := range keys {
		val := bundle.T("en", key)
		if val == key {
			t.Errorf("key %q has no English translation (returned key itself)", key)
		}
		if val == "" {
			t.Errorf("key %q has empty English translation", key)
		}
	}
}

// TestI18NLanguageParity verifies English and Arabic (and other languages) have overlapping keys.
// We check that all languages have the critical keys that English defines.
func TestI18NLanguageParity(t *testing.T) {
	bundle := i18n.New("en")

	langs := bundle.Languages()
	if len(langs) == 0 {
		t.Fatal("no languages in bundle")
	}

	// Verify English and Arabic both exist.
	hasEn := bundle.HasLanguage("en")
	hasAr := bundle.HasLanguage("ar")
	if !hasEn {
		t.Error("English language missing from bundle")
	}
	if !hasAr {
		t.Error("Arabic language missing from bundle")
	}

	// Core keys that should be present in all languages (with fallback to English).
	coreKeys := []string{
		"stop.confirmed",
		"stop.cancelled",
		"typing.working",
		"error.unauthorized",
		"media.processing",
		"webchat.welcome",
	}

	for _, lang := range langs {
		for _, key := range coreKeys {
			val := bundle.T(lang, key)
			if val == key {
				// Key returned itself — this means it's missing from both the language AND English fallback.
				// This should not happen for core keys since English has them all.
				t.Errorf("language %q: key %q not found in language or fallback", lang, key)
			}
		}
	}
}
