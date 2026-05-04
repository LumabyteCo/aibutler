// Package dispatch provides a cross-OS native-script router.
//
// The agent calls shell.script once with a per-OS payload, e.g.:
//
//	{
//	  "darwin":  {"script": "tell application \"Mail\" to get count of messages in inbox"},
//	  "windows": {"command": "Get-Date"},
//	  "linux":   {"service": "org.mpris.MediaPlayer2.spotify",
//	              "object_path": "/org/mpris/MediaPlayer2",
//	              "interface": "org.mpris.MediaPlayer2.Player",
//	              "method": "PlayPause"}
//	}
//
// At runtime the dispatcher forwards the payload for the current GOOS to the
// matching executor (AppleScript on darwin, PowerShell on windows, D-Bus on
// linux). The dispatcher itself imports nothing from those packages — they
// register Tool handlers via SetHandler. This keeps the build CGO-free and
// avoids forcing every OS-specific dependency to be importable on every OS.
//
// Capability: tool.os.script. Each underlying executor still applies its own
// allowlist + capability check — the dispatcher is purely a router.
package dispatch

import (
	"context"
	"encoding/json"
	"fmt"
	"runtime"
	"sort"
)

// Tool is the executor function signature shared by every tier-2 tool.
// Matches the shape that powershell, applescript, shortcuts, and dbus all
// register with the toolRegistry interface.
type Tool func(ctx context.Context, input string) (string, error)

// toolRegistry is the narrow interface for registering tools (avoids import cycles).
type toolRegistry interface {
	Register(name, description, schema, capability string, exec func(ctx context.Context, input string) (string, error))
}

// Dispatcher routes a per-OS payload to the executor for the running GOOS.
type Dispatcher struct {
	handlers map[string]Tool
}

// New creates an empty dispatcher.
func New() *Dispatcher {
	return &Dispatcher{handlers: make(map[string]Tool)}
}

// SetHandler registers a Tool as the handler for the given GOOS value
// ("darwin", "linux", "windows", etc.).
func (d *Dispatcher) SetHandler(goos string, t Tool) {
	if t == nil {
		delete(d.handlers, goos)
		return
	}
	d.handlers[goos] = t
}

// AvailableHandlers returns the registered GOOS values, sorted alphabetically.
func (d *Dispatcher) AvailableHandlers() []string {
	out := make([]string, 0, len(d.handlers))
	for k := range d.handlers {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// Dispatch unmarshals the per-OS map and forwards the entry for the current
// GOOS to its handler.
func (d *Dispatcher) Dispatch(ctx context.Context, input string) (string, error) {
	var perOS map[string]json.RawMessage
	if err := json.Unmarshal([]byte(input), &perOS); err != nil {
		return "", fmt.Errorf("shell.script: invalid input: %w", err)
	}
	if len(perOS) == 0 {
		return "", fmt.Errorf("shell.script: payload is empty (provide at least one of: %v)", d.AvailableHandlers())
	}

	goos := runtime.GOOS
	raw, ok := perOS[goos]
	if !ok {
		return "", fmt.Errorf("shell.script: no payload for GOOS=%s (provided: %v)", goos, keys(perOS))
	}

	handler, ok := d.handlers[goos]
	if !ok {
		return "", fmt.Errorf("shell.script: no executor registered for GOOS=%s (registered: %v)", goos, d.AvailableHandlers())
	}

	return handler(ctx, string(raw))
}

func keys(m map[string]json.RawMessage) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// RegisterDispatchTool registers the shell.script tool.
func RegisterDispatchTool(registry toolRegistry, dispatcher *Dispatcher) {
	registry.Register(
		"shell.script",
		"Cross-OS native-script dispatcher. Provide per-OS payloads (darwin/windows/linux) — the matching one runs on the host. Each underlying executor enforces its own allowlist.",
		`{"type":"object","description":"Map of GOOS to executor-specific payload. At least one OS key is required.","properties":{`+
			`"darwin":{"type":"object","description":"AppleScript / JXA payload. See shell.applescript for fields.","properties":{"script":{"type":"string"},"language":{"type":"string","enum":["AppleScript","JavaScript"]}}},`+
			`"windows":{"type":"object","description":"PowerShell payload. See shell.powershell for fields.","properties":{"command":{"type":"string"}}},`+
			`"linux":{"type":"object","description":"D-Bus method-call payload. See shell.dbus for fields.","properties":{"bus":{"type":"string","enum":["session","system"]},"service":{"type":"string"},"object_path":{"type":"string"},"interface":{"type":"string"},"method":{"type":"string"},"args":{"type":"array","items":{}}}}`+
			`}}`,
		"tool.os.script",
		func(ctx context.Context, input string) (string, error) {
			return dispatcher.Dispatch(ctx, input)
		},
	)
}
