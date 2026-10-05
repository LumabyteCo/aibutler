package iot

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/LumabyteCo/aibutler/internal/capability"
)

// Phase 3 — smart-home routines. A routine is a named list of device
// commands (a template) expanded against the live device registry and
// executed step-by-step through the Controller — so every existing gate
// (capability, tier, rate limit, PIN) applies to every step for free.
//
// Design notes:
//   - Routines NEVER carry a stored PIN. The caller passes the user's PIN
//     in the same message (same one-shot flow as a manual unlock); B10
//     redaction applies to it exactly as everywhere else.
//   - Unknown devices are skipped with a note, not a hard failure: a
//     routine written for a 3-bedroom house must still run in an studio.
//   - Partial success is honest: comfort steps complete even when a
//     safety step refuses (missing PIN). The result says exactly what
//     happened per step.

// routineStep is one command in an expanded routine.
type routineStep struct {
	// Selector resolves to device IDs at run time.
	Selector string // "all lights", "all locks", "thermostat", "climate"
	Action   string
	Params   map[string]interface{}
	// Label is the human description used in the result.
	Label string
}

// builtinRoutines maps a routine name to its template steps.
//
// Selectors are intentionally boring strings matched by the resolver
// below — no regex, no DSL. They resolve against DeviceType/Tier:
//
//	"all lights"  → every tier-2 light/switch (input_boolean included)
//	"all locks"   → every tier-3 lock
//	"climate"     → every tier-2 thermostat / climate device
//	"alarm"       → every tier-3 alarm_control_panel
//	"media"       → every tier-2 media_player
var builtinRoutines = map[string][]routineStep{
	"goodnight": {
		{Selector: "all lights", Action: "off", Label: "turn off all lights"},
		{Selector: "all locks", Action: "lock", Label: "lock all doors (PIN required)"},
		{Selector: "climate", Action: "set", Params: map[string]interface{}{"temperature": 18.0}, Label: "set temperature to 18°C"},
	},
	"good_morning": {
		{Selector: "climate", Action: "set", Params: map[string]interface{}{"temperature": 21.0}, Label: "set temperature to 21°C"},
	},
	"leaving_home": {
		{Selector: "all lights", Action: "off", Label: "turn off all lights"},
		{Selector: "all locks", Action: "lock", Label: "lock all doors (PIN required)"},
		{Selector: "alarm", Action: "arm_away", Label: "arm the alarm (PIN required)"},
	},
	"movie_time": {
		{Selector: "all lights", Action: "set", Params: map[string]interface{}{"brightness": 25}, Label: "dim lights to 25%"},
	},
}

// RoutineNames returns the available routine names (sorted, for schemas
// and listings).
func RoutineNames() []string {
	names := make([]string, 0, len(builtinRoutines))
	for n := range builtinRoutines {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// resolveSelector expands a selector into the matching live devices.
func (c *Controller) resolveSelector(selector string) []Device {
	c.mu.RLock()
	defer c.mu.RUnlock()

	var matched []Device
	for _, d := range c.devices {
		if !d.Enabled {
			continue
		}
		if matchesSelector(selector, *d) {
			matched = append(matched, *d)
		}
	}
	// Stable order: by device ID so routine results are deterministic.
	sort.Slice(matched, func(i, j int) bool { return matched[i].ID < matched[j].ID })
	return matched
}

func matchesSelector(selector string, d Device) bool {
	switch strings.ToLower(strings.TrimSpace(selector)) {
	case "all lights":
		return d.Tier == TierComfort &&
			(d.DeviceType == "light" || d.DeviceType == "switch" || d.DeviceType == "input_boolean")
	case "all locks":
		return d.Tier == TierSafety && d.DeviceType == "lock"
	case "climate":
		return d.Tier == TierComfort && d.DeviceType == "climate"
	case "alarm":
		return d.Tier == TierSafety && d.DeviceType == "alarm_control_panel"
	case "media":
		return d.Tier == TierComfort && d.DeviceType == "media_player"
	}
	// Exact device-ID match ("light.kitchen").
	return strings.EqualFold(selector, d.ID)
}

// StepResult reports what one routine step did.
type StepResult struct {
	Label   string `json:"label"`
	Device  string `json:"device,omitempty"`
	Status  string `json:"status"` // "ok", "pin_required", "denied", "skipped", "error"
	Details string `json:"details,omitempty"`
}

// RunRoutine expands a named routine against the live registry and
// executes it step-by-step through the Controller — meaning capability,
// tier, rate-limit, and PIN gates apply to every single command exactly
// as they do for manual calls. A safety step without a PIN does NOT
// abort the routine: it reports pin_required and the rest of the
// routine continues (comfort first, safety honest).
func (c *Controller) RunRoutine(ctx context.Context, caps *capability.CapabilitySet, name, pin string) ([]StepResult, error) {
	steps, ok := builtinRoutines[name]
	if !ok {
		return nil, fmt.Errorf("iot.routine: unknown routine %q (available: %s)", name, strings.Join(RoutineNames(), ", "))
	}

	var results []StepResult
	// Comfort steps run BEFORE safety steps in the built-ins' order;
	// execute in template order and let the gates speak for themselves.
	for _, step := range steps {
		devices := c.resolveSelector(step.Selector)
		if len(devices) == 0 {
			results = append(results, StepResult{
				Label:   step.Label,
				Status:  "skipped",
				Details: "no matching devices",
			})
			continue
		}
		for _, d := range devices {
			cmd := Command{
				DeviceID: d.ID,
				Action:   step.Action,
				Params:   step.Params,
				// Safety steps carry the user's one-shot PIN; comfort
				// steps don't need it (and ignore it).
				PIN:       pin,
				Confirmed: pin != "" || d.Tier != TierSafety,
			}
			res := StepResult{Label: step.Label, Device: d.ID}
			switch err := c.ExecuteCommand(ctx, caps, cmd); {
			case err == nil:
				res.Status = "ok"
			case err == ErrPINRequired || err == ErrConfirmationRequired:
				res.Status = "pin_required"
				res.Details = "safety device — provide the PIN in the same message"
			case err == ErrPINInvalid:
				res.Status = "error"
				res.Details = "invalid PIN"
			case err == ErrSafetyBound:
				res.Status = "error"
				res.Details = err.Error()
			default:
				// Capability denial or adapter error — report, continue.
				res.Status = "denied"
				res.Details = err.Error()
			}
			results = append(results, res)
		}
	}
	return results, nil
}