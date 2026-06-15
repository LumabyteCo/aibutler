// Package dbus provides a Linux D-Bus method-call client.
//
// D-Bus is the standard IPC mechanism on modern Linux desktops. Most desktop
// apps and system services expose interfaces over the session or system bus
// (Spotify, GNOME Shell, KDE/Plasma, NetworkManager, systemd-logind, MPRIS
// media players, etc.). Calling them is faster, cheaper, and more reliable
// than driving the GUI.
//
// This package wraps github.com/godbus/dbus/v5 with the same security model
// as the other native-script executors:
//
//   - Allowlist of `[bus:]service:object_path:interface:method` patterns
//     (with `*` wildcards). Empty allowlist denies everything.
//   - Bounded call timeout.
//   - Capability gating via tool.dbus.call at the dispatcher layer.
//
// Bus-scoped matching (hardened in v0.4.2): the bus kind is part of the
// match so a session-bus grant cannot authorize a privileged system-bus
// call. A 5-part entry `bus:service:path:interface:method` matches the
// named bus ("session"/"system"/"*"). A legacy 4-part entry (no bus
// prefix) is scoped to the SESSION bus ONLY — system-bus calls now
// require an explicit `system:`-prefixed entry. Trailing-`*` wildcards on
// the dot-separated service/interface and slash-separated object path are
// segment-bounded, so `org.freedesktop.login1*` does not leak into the
// sibling service `org.freedesktop.login1Manager`; method-name wildcards
// remain plain prefixes.
//
// Allowlist examples:
//
//	org.mpris.MediaPlayer2.spotify:/org/mpris/MediaPlayer2:org.mpris.MediaPlayer2.Player:Play
//	org.mpris.MediaPlayer2.*:*:org.mpris.MediaPlayer2.Player:*
//	system:org.freedesktop.login1:/org/freedesktop/login1:org.freedesktop.login1.Manager:Reboot
package dbus

import (
	"context"
	"encoding/json"
	"fmt"
	"runtime"
	"strings"
	"time"

	dbus "github.com/godbus/dbus/v5"

	"github.com/LumabyteCo/aibutler/internal/action"
)

const defaultTimeout = 10 * time.Second

// toolRegistry is the narrow interface for registering tools (avoids import cycles).
type toolRegistry interface {
	Register(name, description, schema, capability string, exec func(ctx context.Context, input string) (string, error))
}

// BusKind identifies which D-Bus instance to use.
type BusKind string

const (
	// BusSession — the per-user session bus (most desktop apps).
	BusSession BusKind = "session"
	// BusSystem — the system-wide bus (logind, NetworkManager, hostname1, etc.).
	BusSystem BusKind = "system"
)

// busOpener allows tests to inject a fake connection. In production this is
// the real godbus connector.
type busOpener func(kind BusKind) (busConn, error)

// busConn abstracts the parts of *dbus.Conn we use, so tests can mock.
type busConn interface {
	Object(dest string, path dbus.ObjectPath) dbus.BusObject
	Close() error
}

// Client is an allowlisted D-Bus method-call client.
type Client struct {
	allowlist []string
	timeout   time.Duration
	opener    busOpener
	recorder  action.Recorder // optional — nil disables recording
}

// NewClient creates a D-Bus client with the given allowlist.
// Empty allowlist denies everything.
func NewClient(allowlist []string) *Client {
	return &Client{
		allowlist: allowlist,
		timeout:   defaultTimeout,
		opener:    defaultBusOpener,
	}
}

// SetTimeout overrides the default call timeout.
func (c *Client) SetTimeout(d time.Duration) { c.timeout = d }

// SetRecorder attaches an action recorder. Pass nil to disable.
// Each Call emits one Action row when a recorder is set.
func (c *Client) SetRecorder(r action.Recorder) { c.recorder = r }

// Call invokes a D-Bus method on the given service+path+interface+method,
// passing the args verbatim. Returns the response body marshalled to JSON.
//
// SECURITY: An empty allowlist denies everything. The match key is
// "service:object_path:interface:method" with `*` wildcards supported.
//
// When a recorder is attached (SetRecorder), every call emits one Action
// row with the call target, allowlist outcome, duration, and a truncated
// result summary.
func (c *Client) Call(ctx context.Context, bus BusKind, service, objectPath, iface, method string, args []interface{}) (string, error) {
	start := time.Now()
	result, err := c.call(ctx, bus, service, objectPath, iface, method, args)
	c.recordAction(ctx, bus, service, objectPath, iface, method, args, result, err, time.Since(start))
	return result, err
}

func (c *Client) call(ctx context.Context, bus BusKind, service, objectPath, iface, method string, args []interface{}) (string, error) {
	if runtime.GOOS != "linux" {
		// D-Bus is also available on FreeBSD and (rarely) macOS, but this
		// executor is Linux-targeted; FreeBSD is allowed through because the
		// godbus client works there, while everything else returns a clear
		// suggestion to use a sibling executor.
		if runtime.GOOS != "freebsd" {
			return "", fmt.Errorf(
				"shell.dbus: Linux-targeted — you're on %s. "+
					"Use shell.applescript for macOS native scripting, shell.powershell on Windows, "+
					"or shell.script for cross-OS routing.", runtime.GOOS)
		}
	}
	if service == "" || objectPath == "" || iface == "" || method == "" {
		return "", fmt.Errorf("shell.dbus: service, object_path, interface, and method are all required")
	}
	if bus == "" {
		bus = BusSession
	}
	key := fmt.Sprintf("%s:%s:%s:%s:%s", bus, service, objectPath, iface, method)
	if !c.inAllowlist(bus, service, objectPath, iface, method) {
		return "", fmt.Errorf("shell.dbus: call not in allowlist: %q", key)
	}

	conn, err := c.opener(bus)
	if err != nil {
		return "", fmt.Errorf("shell.dbus: connect %s bus: %w", bus, err)
	}
	defer conn.Close()

	callCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	obj := conn.Object(service, dbus.ObjectPath(objectPath))
	call := obj.CallWithContext(callCtx, iface+"."+method, 0, args...)
	if call.Err != nil {
		return "", fmt.Errorf("shell.dbus: %w", call.Err)
	}

	body, err := json.Marshal(call.Body)
	if err != nil {
		return fmt.Sprintf("%v", call.Body), nil
	}
	return string(body), nil
}

func (c *Client) inAllowlist(bus BusKind, service, objectPath, iface, method string) bool {
	for _, allowed := range c.allowlist {
		if matchAllowlistEntry(allowed, bus, service, objectPath, iface, method) {
			return true
		}
	}
	return false
}

// matchAllowlistEntry compares an allowlist entry against a call. The bus
// kind is part of the match so a session-bus grant cannot authorize a
// privileged system-bus call (and vice-versa).
//
// Entry forms:
//
//   - 5-part "bus:service:object_path:interface:method" — the bus
//     component matches the call's bus ("session" / "system" / "*").
//   - 4-part "service:object_path:interface:method" — legacy form,
//     scoped to the SESSION bus only (the safe default). System-bus
//     calls require an explicit 5-part entry with bus=system or *.
//
// D-Bus service names, interfaces, methods, and object paths never
// contain ':', so colons are unambiguous separators.
func matchAllowlistEntry(entry string, bus BusKind, service, objectPath, iface, method string) bool {
	parts := strings.Split(entry, ":")
	switch len(parts) {
	case 4:
		// Legacy 4-part entry: session bus only. A session-scoped grant
		// must not silently authorize a system-bus call.
		if bus != BusSession {
			return false
		}
		return matchName(parts[0], service) &&
			matchPath(parts[1], objectPath) &&
			matchName(parts[2], iface) &&
			matchMethod(parts[3], method)
	case 5:
		if !matchBus(parts[0], bus) {
			return false
		}
		return matchName(parts[1], service) &&
			matchPath(parts[2], objectPath) &&
			matchName(parts[3], iface) &&
			matchMethod(parts[4], method)
	default:
		return false
	}
}

// matchBus matches an allowlist bus component ("session" / "system" /
// "*") against the call's bus, case-insensitively.
func matchBus(pattern string, bus BusKind) bool {
	if pattern == "*" {
		return true
	}
	return strings.EqualFold(pattern, string(bus))
}

// matchName matches a dot-separated D-Bus name (service or interface).
// Trailing-`*` is a segment-bounded prefix: it expands only at a `.`
// boundary, so `org.freedesktop.login1*` does NOT match the sibling
// `org.freedesktop.login1Manager`.
func matchName(pattern, value string) bool { return matchPart(pattern, value, ".") }

// matchPath matches a slash-separated D-Bus object path. Trailing-`*`
// expands only at a `/` boundary.
func matchPath(pattern, value string) bool { return matchPart(pattern, value, "/") }

// matchMethod matches a D-Bus method name. Method names are not
// hierarchical, so trailing-`*` is a plain (boundary-free) prefix —
// `Get*` still matches `GetAll`.
func matchMethod(pattern, value string) bool { return matchPart(pattern, value, "") }

// matchPart supports exact match, full wildcard `*`, and trailing-`*`
// prefix. When boundaries is non-empty, a trailing-`*` only matches if
// the wildcard expands at a separator boundary (the prefix ends with a
// boundary char, or the matched remainder starts with one) — closing
// the sibling-namespace leak. When boundaries is "", trailing-`*` is a
// plain byte prefix.
func matchPart(pattern, value, boundaries string) bool {
	if pattern == "*" {
		return true
	}
	if strings.HasSuffix(pattern, "*") {
		prefix := strings.TrimSuffix(pattern, "*")
		if !strings.HasPrefix(value, prefix) {
			return false
		}
		if boundaries == "" {
			return true
		}
		rest := value[len(prefix):]
		if rest == "" {
			return true // exact prefix match
		}
		// Either the prefix already ends at a boundary, or the remainder
		// begins at one.
		if n := len(prefix); n > 0 && strings.IndexByte(boundaries, prefix[n-1]) >= 0 {
			return true
		}
		return strings.IndexByte(boundaries, rest[0]) >= 0
	}
	return pattern == value
}

// defaultBusOpener is the production bus opener — uses real godbus connections.
func defaultBusOpener(kind BusKind) (busConn, error) {
	switch kind {
	case BusSystem:
		conn, err := dbus.SystemBus()
		if err != nil {
			return nil, err
		}
		return realConn{conn}, nil
	case BusSession, "":
		conn, err := dbus.SessionBus()
		if err != nil {
			return nil, err
		}
		return realConn{conn}, nil
	default:
		return nil, fmt.Errorf("unknown bus kind: %s", kind)
	}
}

// realConn wraps *dbus.Conn to satisfy busConn. The shared session/system
// buses are managed by godbus as singletons, so Close is a no-op.
type realConn struct{ *dbus.Conn }

func (realConn) Close() error { return nil }

// recordAction emits one Action row for the just-completed call when a
// recorder is attached.
func (c *Client) recordAction(ctx context.Context, bus BusKind, service, objectPath, iface, method string, args []interface{}, result string, err error, dur time.Duration) {
	if c.recorder == nil {
		return
	}

	target := service
	if iface != "" {
		target += ":" + iface
	}
	summary := fmt.Sprintf("%s.%s on %s", iface, method, service)

	payloadJSON, _ := json.Marshal(struct {
		Bus        BusKind       `json:"bus,omitempty"`
		Service    string        `json:"service"`
		ObjectPath string        `json:"object_path"`
		Interface  string        `json:"interface"`
		Method     string        `json:"method"`
		Args       []interface{} `json:"args,omitempty"`
	}{bus, service, objectPath, iface, method, args})

	status := "success"
	errStr := ""
	if err != nil {
		status = "error"
		errStr = err.Error()
		if strings.Contains(errStr, "not in allowlist") {
			status = "denied"
		}
	}

	resSnippet := result
	if len(resSnippet) > 200 {
		resSnippet = resSnippet[:200]
	}

	_ = c.recorder.Record(ctx, action.Action{
		Type:           "dbus.call",
		Target:         target,
		PayloadSummary: summary,
		PayloadFull:    string(payloadJSON),
		DurationMS:     dur.Milliseconds(),
		Status:         status,
		ResultSummary:  resSnippet,
		Error:          errStr,
	})
}

// DefaultAllowlist returns a curated set of D-Bus call patterns that cover
// the safest common operations on a Linux desktop: posting system
// notifications via org.freedesktop.Notifications, and controlling MPRIS
// media players (Spotify, VLC, mpv, Rhythmbox, etc.).
//
// All entries are read-or-control operations on user-facing surfaces — none
// touch system services like logind, NetworkManager, or hostname1. Callers
// who want to control system services must add those patterns explicitly.
//
// Defaults are NOT applied automatically. Callers must merge them into the
// allowlist explicitly (typically gated by a config flag) so secure-by-default
// (empty allowlist denies everything) still holds for fresh installs.
func DefaultAllowlist() []string {
	return []string{
		// System notifications — post, query capabilities, dismiss.
		"org.freedesktop.Notifications:/org/freedesktop/Notifications:org.freedesktop.Notifications:Notify",
		"org.freedesktop.Notifications:/org/freedesktop/Notifications:org.freedesktop.Notifications:GetCapabilities",
		"org.freedesktop.Notifications:/org/freedesktop/Notifications:org.freedesktop.Notifications:CloseNotification",

		// MPRIS media players — play / pause / toggle / skip / back / read metadata.
		"org.mpris.MediaPlayer2.*:/org/mpris/MediaPlayer2:org.mpris.MediaPlayer2.Player:Play",
		"org.mpris.MediaPlayer2.*:/org/mpris/MediaPlayer2:org.mpris.MediaPlayer2.Player:Pause",
		"org.mpris.MediaPlayer2.*:/org/mpris/MediaPlayer2:org.mpris.MediaPlayer2.Player:PlayPause",
		"org.mpris.MediaPlayer2.*:/org/mpris/MediaPlayer2:org.mpris.MediaPlayer2.Player:Stop",
		"org.mpris.MediaPlayer2.*:/org/mpris/MediaPlayer2:org.mpris.MediaPlayer2.Player:Next",
		"org.mpris.MediaPlayer2.*:/org/mpris/MediaPlayer2:org.mpris.MediaPlayer2.Player:Previous",
		"org.mpris.MediaPlayer2.*:/org/mpris/MediaPlayer2:org.freedesktop.DBus.Properties:Get",
		"org.mpris.MediaPlayer2.*:/org/mpris/MediaPlayer2:org.freedesktop.DBus.Properties:GetAll",
	}
}

// RegisterDBusTool registers the shell.dbus tool.
func RegisterDBusTool(registry toolRegistry, client *Client) {
	registry.Register(
		"shell.dbus",
		"Invoke a D-Bus method on the Linux session or system bus. Only allowed service:path:interface:method patterns (per allowlist) may be called.",
		`{"type":"object","properties":{`+
			`"bus":{"type":"string","enum":["session","system"],"description":"D-Bus instance — session (per-user) or system (host-wide). Defaults to session.","default":"session"},`+
			`"service":{"type":"string","description":"D-Bus service name, e.g. org.mpris.MediaPlayer2.spotify"},`+
			`"object_path":{"type":"string","description":"D-Bus object path, e.g. /org/mpris/MediaPlayer2"},`+
			`"interface":{"type":"string","description":"D-Bus interface name, e.g. org.mpris.MediaPlayer2.Player"},`+
			`"method":{"type":"string","description":"Method name, e.g. Play"},`+
			`"args":{"type":"array","description":"Positional arguments passed to the method","items":{}}`+
			`},"required":["service","object_path","interface","method"]}`,
		"tool.dbus.call",
		func(ctx context.Context, input string) (string, error) {
			var args struct {
				Bus        string        `json:"bus"`
				Service    string        `json:"service"`
				ObjectPath string        `json:"object_path"`
				Interface  string        `json:"interface"`
				Method     string        `json:"method"`
				Args       []interface{} `json:"args"`
			}
			if err := json.Unmarshal([]byte(input), &args); err != nil {
				return "", fmt.Errorf("shell.dbus: invalid input: %w", err)
			}
			return client.Call(ctx, BusKind(args.Bus), args.Service, args.ObjectPath, args.Interface, args.Method, args.Args)
		},
	)
}
