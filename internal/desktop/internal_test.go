package desktop

import (
	"strings"
	"testing"
)

// TestEscapeSendKeys verifies SendKeys metacharacters are brace-escaped
// so injected text types literally rather than acting as key commands.
func TestEscapeSendKeys(t *testing.T) {
	cases := []struct{ in, want string }{
		{"hello", "hello"},
		{"a+b", "a{+}b"},
		{"50%", "50{%}"},
		{"^c", "{^}c"},
		{"~user", "{~}user"},
		{"f(x)", "f{(}x{)}"},
		{"a{b}c", "a{{}b{}}c"},
		{"x[y]", "x{[}y{]}"},
		{"", ""},
	}
	for _, tc := range cases {
		if got := escapeSendKeys(tc.in); got != tc.want {
			t.Errorf("escapeSendKeys(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// TestKeyAliasesComplete verifies every key the tool advertises resolves
// to a full per-OS mapping (macOS code, X11 keysym, Windows SendKeys
// token) — so no supported key is missing a backend representation.
func TestKeyAliasesComplete(t *testing.T) {
	for _, name := range strings.Split(supportedKeyList, ", ") {
		m, ok := keyAliases[name]
		if !ok {
			t.Errorf("supported key %q has no mapping", name)
			continue
		}
		if m.macCode == 0 {
			t.Errorf("key %q has no macOS key code", name)
		}
		if m.x11 == "" {
			t.Errorf("key %q has no X11 keysym", name)
		}
		if m.winSend == "" {
			t.Errorf("key %q has no Windows SendKeys token", name)
		}
	}
	// Aliases (enter/esc/backspace) should also resolve.
	for _, alias := range []string{"enter", "esc", "backspace"} {
		if _, ok := keyAliases[alias]; !ok {
			t.Errorf("alias %q should resolve", alias)
		}
	}
}

// TestLinuxCaptureToolsOrdered sanity-checks the Linux screenshot tool
// preference list: non-empty, each entry produces args that include the
// output path.
func TestLinuxCaptureToolsOrdered(t *testing.T) {
	if len(linuxCaptureTools) == 0 {
		t.Fatal("linuxCaptureTools must not be empty")
	}
	// Wayland-first ordering: grim should precede the X11 tools.
	idx := map[string]int{}
	for i, tool := range linuxCaptureTools {
		idx[tool.bin] = i
		args := tool.argsFor("/tmp/x.png")
		found := false
		for _, a := range args {
			if a == "/tmp/x.png" {
				found = true
			}
		}
		if !found {
			t.Errorf("tool %q args %v omit the output path", tool.bin, args)
		}
	}
	if g, ok := idx["grim"]; ok {
		if s, ok2 := idx["scrot"]; ok2 && g > s {
			t.Error("grim (Wayland) should be tried before scrot (X11)")
		}
	}
}

// TestResolvePowerShell returns a non-empty binary name regardless of OS.
func TestResolvePowerShell(t *testing.T) {
	if resolvePowerShell() == "" {
		t.Error("resolvePowerShell must return a non-empty binary name")
	}
}
