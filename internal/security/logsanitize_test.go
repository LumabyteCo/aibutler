package security

import (
	"strings"
	"testing"
)

func TestSanitizeLogValue_NewlineInjection(t *testing.T) {
	input := "schedule-name\nfake log entry"
	result := SanitizeLogValue(input)

	if strings.Contains(result, "\n") {
		t.Fatalf("sanitized value should not contain newlines, got %q", result)
	}
}

func TestSanitizeLogValue_CarriageReturn(t *testing.T) {
	input := "value\roverwrite"
	result := SanitizeLogValue(input)

	if strings.Contains(result, "\r") {
		t.Fatalf("sanitized value should not contain carriage returns, got %q", result)
	}
}

func TestSanitizeLogValue_Tab(t *testing.T) {
	input := "value\twith\ttabs"
	result := SanitizeLogValue(input)

	if strings.Contains(result, "\t") {
		t.Fatalf("sanitized value should not contain tabs, got %q", result)
	}
}

func TestSanitizeLogValue_NullByte(t *testing.T) {
	input := "value\x00null"
	result := SanitizeLogValue(input)

	if strings.Contains(result, "\x00") {
		t.Fatalf("sanitized value should not contain null bytes, got %q", result)
	}
}

func TestSanitizeLogValue_CleanInput(t *testing.T) {
	input := "clean-schedule-name"
	result := SanitizeLogValue(input)

	if result != input {
		t.Fatalf("clean input should pass through unchanged, got %q", result)
	}
}

func TestSanitizeLogValue_EscapeSequence(t *testing.T) {
	input := "value\x1b[31mred"
	result := SanitizeLogValue(input)

	if strings.Contains(result, "\x1b") {
		t.Fatalf("sanitized value should not contain escape sequences, got %q", result)
	}
}
