package contact

import "strings"

// NormalizeArabic normalizes Arabic text for fuzzy matching:
//   - Folds hamza/alef variants (أ إ آ ٱ) to bare alef (ا)
//   - Strips diacritics/tashkeel (U+064B–U+065F)
//   - Normalizes taa marbuta (ة) to haa (ه) for search
//   - Strips tatweel/kashida (ـ)
//
// Pure Go, no external dependencies.
func NormalizeArabic(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		// Skip diacritics (tashkeel).
		if r >= 0x064B && r <= 0x065F {
			continue
		}
		// Skip tatweel.
		if r == 0x0640 {
			continue
		}
		// Fold hamza/alef variants.
		switch r {
		case 0x0623, 0x0625, 0x0622, 0x0671: // أ إ آ ٱ → ا
			b.WriteRune(0x0627)
		case 0x0629: // ة → ه
			b.WriteRune(0x0647)
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}
