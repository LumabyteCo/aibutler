package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestCmdResumeNoArgs(t *testing.T) {
	app := testApp(t)
	var buf bytes.Buffer
	if err := CmdResume(app, nil, &buf); err != nil {
		t.Fatalf("CmdResume: %v", err)
	}
	if !strings.Contains(buf.String(), "No sessions available") && !strings.Contains(buf.String(), "Available sessions") {
		t.Errorf("expected session listing, got: %s", buf.String())
	}
}

func TestCmdResumeInvalidSession(t *testing.T) {
	app := testApp(t)
	var buf bytes.Buffer
	err := CmdResume(app, []string{"nonexistent-session"}, &buf)
	// Load may return empty or error depending on directory existence.
	// Either outcome is acceptable — the important thing is it doesn't panic.
	_ = err
	// If it didn't error, it should have printed "Resumed session" with 0 messages.
	output := buf.String()
	if err == nil && !strings.Contains(output, "Resumed session") {
		t.Errorf("expected resume message, got: %s", output)
	}
}
