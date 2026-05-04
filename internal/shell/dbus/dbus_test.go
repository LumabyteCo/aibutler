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
		"any", "any", "any", "any",
	)
	if !got {
		t.Fatal("expected full wildcard to match anything")
	}
}

func TestMatchAllowlistEntry_PrefixWildcard(t *testing.T) {
	got := matchAllowlistEntry(
		"org.mpris.MediaPlayer2.*:*:org.mpris.MediaPlayer2.Player:*",
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
	if matchAllowlistEntry("not:enough:parts", "a", "b", "c", "d") {
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
