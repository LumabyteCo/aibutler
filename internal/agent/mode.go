package agent

import (
	"log"
	"strings"
)

// Mode controls how the agent selects and uses tools.
type Mode string

const (
	ModeAuto   Mode = "auto"   // Resolved to ModeMulti at runtime
	ModeSingle Mode = "single" // Sequential tool execution, no delegation
	ModeMulti  Mode = "multi"  // Parallel tool execution, delegation enabled
	ModeCustom Mode = "custom" // User-defined custom roles with routing
	ModeSwarm  Mode = "swarm"  // Router auto-activates, all orchestration patterns enabled
)

// effectiveMode resolves "auto" to the actual mode.
func effectiveMode(m Mode) Mode {
	if m == ModeAuto {
		return ModeMulti // auto resolves to multi (delegation enabled)
	}
	if m == ModeSwarm {
		return ModeMulti // Swarm uses multi execution with router overlay
	}
	return m
}

// ValidMode returns true if m is a recognized agent mode.
func ValidMode(m string) bool {
	switch Mode(m) {
	case ModeAuto, ModeSingle, ModeMulti, ModeCustom, ModeSwarm:
		return true
	}
	return false
}

// ParseModeOverride extracts a per-turn mode override from the task prefix.
// Supported formats: "[mode:multi]", "[mode:single]", "[mode:custom]", "[mode:auto]".
// Returns the override mode and the cleaned task with the prefix removed.
// If no override is found, returns empty string and the original task unchanged.
// The currentMode parameter prevents escalation: overrides that would grant more
// capabilities than the configured mode are silently blocked.
func ParseModeOverride(task string, currentMode ...Mode) (Mode, string) {
	trimmed := strings.TrimSpace(task)
	if !strings.HasPrefix(trimmed, "[mode:") {
		return "", task
	}
	end := strings.Index(trimmed, "]")
	if end == -1 {
		return "", task
	}
	modeStr := trimmed[6:end] // extract between "[mode:" and "]"
	if !ValidMode(modeStr) {
		return "", task
	}
	cleaned := strings.TrimSpace(trimmed[end+1:])
	override := Mode(modeStr)

	// Block escalation: single→multi/swarm, multi→swarm.
	if len(currentMode) > 0 {
		cur := currentMode[0]
		if (cur == ModeSingle && (override == ModeMulti || override == ModeSwarm)) ||
			(cur == ModeMulti && override == ModeSwarm) {
			log.Printf("agent: mode override %q blocked — exceeds configured mode %q", override, cur)
			return "", task
		}
	}

	return override, cleaned
}
