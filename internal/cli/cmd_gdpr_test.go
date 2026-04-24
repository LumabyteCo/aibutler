package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestCmdGDPRNoArgs(t *testing.T) {
	app := testApp(t)
	if app.ComplianceLogger == nil {
		t.Skip("ComplianceLogger not initialized in test app")
	}
	var buf bytes.Buffer
	if err := CmdGDPR(app, nil, &buf); err != nil {
		t.Fatalf("CmdGDPR: %v", err)
	}
	if !strings.Contains(buf.String(), "Usage:") {
		t.Errorf("expected usage text, got: %s", buf.String())
	}
}

func TestCmdGDPRNoCompliance(t *testing.T) {
	app := testApp(t)
	app.ComplianceLogger = nil
	var buf bytes.Buffer
	err := CmdGDPR(app, []string{"delete-user", "test"}, &buf)
	if err == nil {
		t.Fatal("expected error when ComplianceLogger is nil")
	}
}
