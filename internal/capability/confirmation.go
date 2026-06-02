package capability

import (
	"errors"
	"fmt"
)

// ErrConfirmationRequired is the sentinel returned by the tool
// dispatcher when a capability check resolves to "allowed but the user
// must explicitly confirm before this action runs." Use errors.Is to
// detect it; use errors.As with *ConfirmationRequiredError to recover
// the structured capability + reason context.
//
// Wired in v0.3.x to surface up to the mission supervisor, which
// auto-pauses the mission to waiting_user and emits a
// mission.confirmation_required event. Without a mission engine in
// play, callers see this as an ordinary tool error.
var ErrConfirmationRequired = errors.New("capability: confirmation required")

// ConfirmationRequiredError pairs the sentinel with the capability
// resource and a human-readable reason. The agent / worker / supervisor
// chain unwraps this to surface the capability identifier (e.g.
// "tool.shell.exec") and the engine's reason string in the
// mission.confirmation_required event payload so a UI can render an
// informative prompt without re-deriving anything.
type ConfirmationRequiredError struct {
	// Capability is the resource identifier from CheckRequest.Resource
	// (e.g. "tool.shell.exec", "data.calendar.write").
	Capability string
	// Reason is the engine's classification of the check result
	// (typically "granted" — the capability IS granted, it just
	// requires confirmation). Carried verbatim so audit + UI layers
	// can correlate against the capability engine's check vocabulary.
	Reason string
}

// Error renders a one-line description suitable for logs and the
// event payload.
func (e *ConfirmationRequiredError) Error() string {
	if e.Reason != "" {
		return fmt.Sprintf("capability %q: confirmation required (%s)", e.Capability, e.Reason)
	}
	return fmt.Sprintf("capability %q: confirmation required", e.Capability)
}

// Unwrap lets errors.Is(err, ErrConfirmationRequired) match.
func (e *ConfirmationRequiredError) Unwrap() error {
	return ErrConfirmationRequired
}
