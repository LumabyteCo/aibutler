//go:build !darwin

package permissions

import (
	"context"
	"runtime"
)

// Check on non-darwin OSes returns a single not-applicable entry. Future
// work could probe XDG portals on Linux (Camera, Microphone, Notifications)
// or UAC / SeDebugPrivilege state on Windows.
func Check(_ context.Context) Report {
	return Report{
		Platform: runtime.GOOS,
		Permissions: []Permission{
			{
				Name:   "macOS TCC permissions",
				Status: StatusNotApplicable,
				Why:    "Butler's permission wizard currently covers macOS Automation and Screen Recording. Other OSes don't have an equivalent TCC system to probe.",
			},
		},
		Summary: "permission wizard is currently macOS-only — non-darwin OSes don't have an equivalent TCC system to probe",
	}
}
