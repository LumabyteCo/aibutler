package i18n

import "fmt"

// Bundle holds translations for all supported languages.
type Bundle struct {
	translations map[string]map[string]string
	fallback     string
}

// New creates a Bundle with embedded translations and the given fallback language.
func New(fallback string) *Bundle {
	b := &Bundle{
		translations: make(map[string]map[string]string),
		fallback:     fallback,
	}
	for lang, kv := range defaultTranslations {
		b.translations[lang] = kv
	}
	return b
}

// T translates a key for the given language.
// Falls back to the fallback language if the key is missing.
// Returns the key itself if not found in any language.
func (b *Bundle) T(lang, key string) string {
	if kv, ok := b.translations[lang]; ok {
		if v, ok := kv[key]; ok {
			return v
		}
	}
	// Fallback language.
	if lang != b.fallback {
		if kv, ok := b.translations[b.fallback]; ok {
			if v, ok := kv[key]; ok {
				return v
			}
		}
	}
	return key
}

// TF translates a key with fmt.Sprintf-style arguments.
func (b *Bundle) TF(lang, key string, args ...interface{}) string {
	tmpl := b.T(lang, key)
	if len(args) == 0 {
		return tmpl
	}
	return fmt.Sprintf(tmpl, args...)
}

// Languages returns all available language codes.
func (b *Bundle) Languages() []string {
	langs := make([]string, 0, len(b.translations))
	for lang := range b.translations {
		langs = append(langs, lang)
	}
	return langs
}

// HasLanguage checks if a language is loaded.
func (b *Bundle) HasLanguage(lang string) bool {
	_, ok := b.translations[lang]
	return ok
}
