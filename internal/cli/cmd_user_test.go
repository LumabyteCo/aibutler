package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestCmdUserNoArgs(t *testing.T) {
	app := testApp(t)
	if app.RBAC == nil {
		t.Skip("RBAC not initialized in test app")
	}
	var buf bytes.Buffer
	if err := CmdUser(app, nil, &buf); err != nil {
		t.Fatalf("CmdUser: %v", err)
	}
	if !strings.Contains(buf.String(), "Usage:") {
		t.Errorf("expected usage text, got: %s", buf.String())
	}
}

func TestCmdUserNoRBAC(t *testing.T) {
	app := testApp(t)
	app.RBAC = nil
	var buf bytes.Buffer
	err := CmdUser(app, []string{"list"}, &buf)
	if err == nil {
		t.Fatal("expected error when RBAC is nil")
	}
}
