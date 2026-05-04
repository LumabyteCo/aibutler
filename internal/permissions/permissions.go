// Package permissions checks OS-level permissions Butler may need and
// surfaces actionable instructions for granting any that are missing.
//
// On macOS this currently covers:
//
//   - Automation: System Events     (required for AppleScript / keystroke / menu-bar control)
//   - Automation: Finder            (required for AppleScript file operations)
//   - Screen Recording              (required for screen capture / vision)
//
// The probe is intentionally a one-shot snapshot — running the tool may
// trigger macOS TCC consent dialogs on first use (that's the point: probing
// surfaces state that's otherwise opaque). Subsequent runs are silent
// once permissions have been decided.
//
// Linux and Windows currently return a "not applicable" report. Future
// work could check XDG portals on Linux or UAC elevation state on Windows.
package permissions

import (
	"context"
	"encoding/json"
	"fmt"
)

// Status indicates whether a given permission is granted.
type Status string

const (
	// StatusGranted — permission is currently granted.
	StatusGranted Status = "granted"
	// StatusDenied — permission was prompted and denied (or revoked).
	StatusDenied Status = "denied"
	// StatusUnknown — probe couldn't determine state (probe error,
	// missing tooling, etc.). LastError will explain.
	StatusUnknown Status = "unknown"
	// StatusNotApplicable — permission category doesn't exist on this OS.
	StatusNotApplicable Status = "not_applicable"
)

// Permission is a single permission's status + actionable next steps.
type Permission struct {
	Name        string `json:"name"`
	Status      Status `json:"status"`
	Why         string `json:"why"`
	SettingsURL string `json:"settings_url,omitempty"`
	HowToGrant  string `json:"how_to_grant,omitempty"`
	LastError   string `json:"last_error,omitempty"`
}

// Report is the full snapshot returned by Check.
type Report struct {
	Platform    string       `json:"platform"`
	Permissions []Permission `json:"permissions"`
	Summary     string       `json:"summary"`
}

// summarize computes the "X of Y granted" summary line and an actionable
// hint when any are denied.
func summarize(perms []Permission) string {
	granted := 0
	denied := 0
	for _, p := range perms {
		switch p.Status {
		case StatusGranted:
			granted++
		case StatusDenied:
			denied++
		}
	}
	if len(perms) == 0 {
		return "no permission categories applicable on this OS"
	}
	base := fmt.Sprintf("%d of %d permissions granted", granted, len(perms))
	if denied > 0 {
		base += fmt.Sprintf(" — open the settings_url for each denied permission to enable it (%d remaining)", denied)
	}
	return base
}

// toolRegistry is the narrow interface for registering tools (avoids import cycles).
type toolRegistry interface {
	Register(name, description, schema, capability string, exec func(ctx context.Context, input string) (string, error))
}

// RegisterTool registers permissions.check.
func RegisterTool(registry toolRegistry) {
	registry.Register(
		"permissions.check",
		"Check OS-level permissions Butler needs and report status with actionable next steps. "+
			"On macOS: Automation (System Events, Finder) and Screen Recording. "+
			"On other OSes: returns a 'not applicable' report. "+
			"Note: running this tool on macOS may trigger TCC consent dialogs on first use — that's expected.",
		`{"type":"object","properties":{},"additionalProperties":false}`,
		"", // No capability — diagnostic / read-only.
		func(ctx context.Context, _ string) (string, error) {
			report := Check(ctx)
			out, err := json.Marshal(report)
			if err != nil {
				return "", fmt.Errorf("permissions.check: marshal: %w", err)
			}
			return string(out), nil
		},
	)
}
