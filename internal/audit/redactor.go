package audit

import (
	"io"
	"regexp"
	"strings"
)

// Patterns that look like API keys, tokens, or secrets.
var sensitivePatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)(api[_-]?key|api[_-]?secret|token|bearer|password|secret)[=:\s]+["']?[\w\-./+=]{16,}["']?`),
	regexp.MustCompile(`sk-[a-zA-Z0-9]{20,}`),          // OpenAI-style
	regexp.MustCompile(`xox[bprs]-[a-zA-Z0-9\-]+`),     // Slack tokens
	regexp.MustCompile(`ghp_[a-zA-Z0-9]{36}`),           // GitHub PAT
	regexp.MustCompile(`gho_[a-zA-Z0-9]{36}`),           // GitHub OAuth
	regexp.MustCompile(`glpat-[a-zA-Z0-9\-]{20,}`),      // GitLab PAT
	regexp.MustCompile(`AKIA[A-Z0-9]{16}`),              // AWS access key
	regexp.MustCompile(`(?i)basic\s+[a-zA-Z0-9+/=]{20,}`), // Basic auth
}

// Redact replaces sensitive patterns in text with [REDACTED].
func Redact(text string) string {
	result := text
	for _, pat := range sensitivePatterns {
		result = pat.ReplaceAllStringFunc(result, func(match string) string {
			// Keep the prefix label if there is one.
			idx := strings.IndexAny(match, "=: ")
			if idx >= 0 {
				return match[:idx+1] + "[REDACTED]"
			}
			return "[REDACTED]"
		})
	}
	return result
}

// ContainsSensitive checks if text contains any sensitive patterns.
func ContainsSensitive(text string) bool {
	for _, pat := range sensitivePatterns {
		if pat.MatchString(text) {
			return true
		}
	}
	return false
}

// RedactingWriter wraps an io.Writer and redacts sensitive data before writing.
type RedactingWriter struct {
	w io.Writer
}

// NewRedactingWriter creates a writer that sanitizes secrets from log output.
func NewRedactingWriter(w io.Writer) *RedactingWriter {
	return &RedactingWriter{w: w}
}

func (rw *RedactingWriter) Write(p []byte) (int, error) {
	cleaned := Redact(string(p))
	_, err := rw.w.Write([]byte(cleaned))
	return len(p), err // Return original length so log.Logger doesn't complain.
}
