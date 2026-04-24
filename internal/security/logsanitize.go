// Package security provides security utilities for AI Butler.
package security

import "strings"

// SanitizeLogValue removes control characters (newlines, carriage returns, tabs)
// from a string to prevent log injection attacks. User-controlled values should
// be passed through this function before inclusion in log messages.
func SanitizeLogValue(s string) string {
	r := strings.NewReplacer(
		"\n", "\\n",
		"\r", "\\r",
		"\t", "\\t",
		"\x00", "",
		"\x1b", "", // escape sequences
	)
	return r.Replace(s)
}
