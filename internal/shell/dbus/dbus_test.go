package dbus

import (
	"context"
	"runtime"
	"strings"
	"testing"
)

type mockRegistry struct {
	tools []string
	exec  map[string]func(ctx context.Context, input string) (string, error)
}

func newMockRegistry() *mockRegistry {
	return &mockRegistry{exec: make(map[string]func(ctx context.Context, input string) (string, error))}
}

func (m *mockRegistry) Register(name, _, _, _ string, exec func(ctx context.Context, input string) (string, error)) {
	m.tools = append(m.tools, name)
	m.exec[name] = exec
}

func TestMatchAllowlistEntry_ExactMatch(t *testing.T) {
	got := matchAllowlistEntry(
		"org.mpris.MediaPlayer2.spotify:/org/mpris/MediaPlayer2:org.mpris.MediaPlayer2.Player:Play",
		BusSession,
		"org.mpris.MediaPlayer2.spotify",
		"/org/mpris/MediaPlayer2",
		"org.mpris.MediaPlayer2.Player",
		"Play",
	)
	if !got {
		t.Fatal("expected exact-match allowlist entry to match")
	}
}

func TestMatchAllowlistEntry_FullWildcard(t *testing.T) {
	got := matchAllowlistEntry(
		"*:*:*:*",
		BusSession,
		"any", "any", "any", "any",
	)
	if !got {
		t.Fatal("expected full wildcard to match anything")
	}
}

func TestMatchAllowlistEntry_PrefixWildcard(t *testing.T) {
	got := matchAllowlistEntry(
		"org.mpris.MediaPlayer2.*:*:org.mpris.MediaPlayer2.Player:*",
		BusSession,
		"org.mpris.MediaPlayer2.spotify",
		"/org/mpris/MediaPlayer2",
		"org.mpris.MediaPlayer2.Player",
		"PlayPause",
	)
	if !got {
		t.Fatal("expected prefix-wildcard service + wildcard method to match")
	}
}

func TestMatchAllowlistEntry_NoMatch(t *testing.T) {
	got := matchAllowlistEntry(
		"org.mpris.MediaPlayer2.spotify:/org/mpris/MediaPlayer2:org.mpris.MediaPlayer2.Player:Play",
		BusSession,
		"org.mpris.MediaPlayer2.vlc",
		"/org/mpris/MediaPlayer2",
		"org.mpris.MediaPlayer2.Player",
		"Play",
	)
	if got {
		t.Fatal("expected service mismatch to deny")
	}
}

func TestMatchAllowlistEntry_BadFormat(t *testing.T) {
	if matchAllowlistEntry("not:enough:parts", BusSession, "a", "b", "c", "d") {
		t.Fatal("expected entry with fewer than 4 parts to never match")
	}
}

func TestCall_Denied_EmptyAllowlist(t *testing.T) {
	if runtime.GOOS != "linux" && runtime.GOOS != "freebsd" {
		t.Skip("D-Bus tier-2 executor is Linux-targeted")
	}
	c := NewClient([]string{})
	_, err := c.Call(context.Background(), BusSession,
		"org.mpris.MediaPlayer2.spotify",
		"/org/mpris/MediaPlayer2",
		"org.mpris.MediaPlayer2.Player",
		"Play",
		nil,
	)
	if err == nil {
		t.Fatal("expected error when allowlist is empty")
	}
}

func TestCall_Denied_NoMatch(t *testing.T) {
	if runtime.GOOS != "linux" && runtime.GOOS != "freebsd" {
		t.Skip("D-Bus tier-2 executor is Linux-targeted")
	}
	c := NewClient([]string{
		"org.freedesktop.Notifications:*:*:Notify",
	})
	_, err := c.Call(context.Background(), BusSession,
		"org.mpris.MediaPlayer2.spotify",
		"/org/mpris/MediaPlayer2",
		"org.mpris.MediaPlayer2.Player",
		"Play",
		nil,
	)
	if err == nil {
		t.Fatal("expected error when allowlist doesn't cover the call")
	}
}

func TestCall_MissingFields(t *testing.T) {
	c := NewClient([]string{"*:*:*:*"})
	_, err := c.Call(context.Background(), BusSession, "", "/path", "iface", "method", nil)
	if err == nil {
		t.Fatal("expected error when service is empty")
	}
}

func TestCall_NonLinux_FailsLoudly(t *testing.T) {
	if runtime.GOOS == "linux" || runtime.GOOS == "freebsd" {
		t.Skip("non-linux gate only fires off Linux/FreeBSD")
	}
	c := NewClient([]string{"*:*:*:*"})
	_, err := c.Call(context.Background(), BusSession,
		"org.example", "/x", "org.example.iface", "DoIt", nil,
	)
	if err == nil {
		t.Fatal("expected error on unsupported GOOS")
	}
}

func TestDefaultAllowlist_NotEmpty(t *testing.T) {
	defs := DefaultAllowlist()
	if len(defs) == 0 {
		t.Fatal("expected non-empty default allowlist for D-Bus")
	}
	// Notifications must be in defaults — primary use case.
	wantPrefix := "org.freedesktop.Notifications:"
	found := false
	for _, d := range defs {
		if len(d) > len(wantPrefix) && d[:len(wantPrefix)] == wantPrefix {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected at least one notification entry in defaults, got %v", defs)
	}
}

func TestDefaultAllowlist_AllEntriesAreFourPart(t *testing.T) {
	defs := DefaultAllowlist()
	for i, d := range defs {
		// Each allowlist entry must split into exactly 4 colon-separated
		// parts (service:path:interface:method) to match the matcher's
		// expectations. A malformed default would silently never match.
		count := 0
		for _, c := range d {
			if c == ':' {
				count++
			}
		}
		if count != 3 {
			t.Errorf("default[%d]=%q has %d colons; expected 3 (service:path:interface:method)", i, d, count)
		}
	}
}

func TestDefaultAllowlist_PermitsNotify(t *testing.T) {
	if runtime.GOOS != "linux" && runtime.GOOS != "freebsd" {
		t.Skip("D-Bus allowlist effect tested on Linux/FreeBSD; other OSes gate earlier")
	}
	c := NewClient(DefaultAllowlist())
	// The test doesn't assert a successful Notify (no D-Bus session may be
	// available in the test environment) — it only verifies the allowlist
	// gate accepts the call.
	_, err := c.Call(context.Background(), BusSession,
		"org.freedesktop.Notifications",
		"/org/freedesktop/Notifications",
		"org.freedesktop.Notifications",
		"Notify",
		nil,
	)
	if err != nil && strings.Contains(err.Error(), "not in allowlist") {
		t.Errorf("default allowlist rejected a Notify call: %v", err)
	}
}

func TestRegisterDBusTool(t *testing.T) {
	reg := newMockRegistry()
	c := NewClient([]string{"*:*:*:*"})
	RegisterDBusTool(reg, c)

	found := false
	for _, name := range reg.tools {
		if name == "shell.dbus" {
			found = true
		}
	}
	if !found {
		t.Error("shell.dbus tool was not registered")
	}
}

func TestExecuteTool_InvalidJSON(t *testing.T) {
	reg := newMockRegistry()
	c := NewClient([]string{"*:*:*:*"})
	RegisterDBusTool(reg, c)

	dbExec := reg.exec["shell.dbus"]
	if dbExec == nil {
		t.Fatal("shell.dbus not registered")
	}
	_, err := dbExec(context.Background(), `not json`)
	if err == nil {
		t.Fatal("expected error for malformed JSON input")
	}
}

// --- Allowlist-bypass regression tests (v0.4.2 hardening) ---

// TestBypass_BusKindOmission: a 4-part (legacy) entry is session-bus
// only — it must NOT authorize the same call on the privileged system
// bus. (Confirmed HIGH by the v0.4.2 audit.)
func TestBypass_BusKindOmission(t *testing.T) {
	entry := "org.freedesktop.hostname1:/org/freedesktop/hostname1:org.freedesktop.hostname1:SetHostname"

	// Session bus: legacy entry applies.
	if !matchAllowlistEntry(entry, BusSession,
		"org.freedesktop.hostname1", "/org/freedesktop/hostname1",
		"org.freedesktop.hostname1", "SetHostname") {
		t.Error("legacy 4-part entry should match on the session bus")
	}
	// System bus: legacy entry must NOT apply (privilege escalation).
	if matchAllowlistEntry(entry, BusSystem,
		"org.freedesktop.hostname1", "/org/freedesktop/hostname1",
		"org.freedesktop.hostname1", "SetHostname") {
		t.Error("bus-kind bypass: a session-scoped 4-part entry authorized a SYSTEM-bus call")
	}

	// An explicit 5-part system entry authorizes the system bus.
	sysEntry := "system:" + entry
	if !matchAllowlistEntry(sysEntry, BusSystem,
		"org.freedesktop.hostname1", "/org/freedesktop/hostname1",
		"org.freedesktop.hostname1", "SetHostname") {
		t.Error("explicit system:-prefixed entry should authorize the system bus")
	}
	// ...but that system entry must not leak onto the session bus.
	if matchAllowlistEntry(sysEntry, BusSession,
		"org.freedesktop.hostname1", "/org/freedesktop/hostname1",
		"org.freedesktop.hostname1", "SetHostname") {
		t.Error("system:-prefixed entry should not authorize a session-bus call")
	}
}

// TestBypass_PrefixBoundaryLeak: a trailing-* name wildcard must not
// leak across a namespace segment boundary into a sibling. (MEDIUM.)
func TestBypass_PrefixBoundaryLeak(t *testing.T) {
	// Entry intends to allow the login1 service namespace.
	entry := "session:org.freedesktop.login1*:*:*:*"

	// The sibling service org.freedesktop.login1Manager must NOT match —
	// it's a different service that merely shares a byte prefix.
	if matchAllowlistEntry(entry, BusSession,
		"org.freedesktop.login1Manager", "/x", "org.x", "M") {
		t.Error("prefix-boundary leak: login1* matched sibling service login1Manager")
	}
	// The intended namespaced child DOES match (expands at a '.' boundary).
	if !matchAllowlistEntry(entry, BusSession,
		"org.freedesktop.login1.Session", "/x", "org.x", "M") {
		t.Error("login1* should match the namespaced child org.freedesktop.login1.Session")
	}
	// Exact service still matches.
	if !matchAllowlistEntry(entry, BusSession,
		"org.freedesktop.login1", "/x", "org.x", "M") {
		t.Error("login1* should match the exact service org.freedesktop.login1")
	}

	// Method prefix wildcards remain plain prefixes (not boundary-bound):
	// Get* still matches GetAll.
	mEntry := "session:*:*:*:Get*"
	if !matchAllowlistEntry(mEntry, BusSession, "s", "/p", "i", "GetAll") {
		t.Error("method Get* should still match GetAll (methods are not hierarchical)")
	}
}
