package agent

import "time"

// AutonomyLevel controls how independently the agent operates.
type AutonomyLevel string

const (
	// AutonomyL1 is the default: agent responds to user messages, one turn at a time.
	AutonomyL1 AutonomyLevel = "l1"
	// AutonomyL2 is semi-autonomous: agent can take multiple actions without
	// prompting the user for each step. Some actions still require confirmation
	// based on L2AutoActions/L2AskActions configuration.
	AutonomyL2 AutonomyLevel = "l2"
	// AutonomyL3 is fully autonomous: agent executes all granted capabilities
	// without confirmation, time-bounded. Financial/safety-critical actions
	// ALWAYS require confirmation even at L3.
	AutonomyL3 AutonomyLevel = "l3"
)

// L3Config holds L3-specific autonomy settings.
type L3Config struct {
	TimeBound     time.Duration // Max duration for L3 autonomy (default: 30m, max: 24h)
	SafetyActions []string      // Actions that ALWAYS require confirmation at L3
	StartedAt     time.Time     // When L3 mode was activated
}

// DefaultL3Config returns an L3Config with default values.
func DefaultL3Config() L3Config {
	return L3Config{
		TimeBound: 30 * time.Minute,
		SafetyActions: []string{
			"finance.transfer", "finance.payment",
			"account.delete", "account.modify",
			"system.shutdown", "system.reboot",
		},
	}
}

// IsExpired returns true if the L3 time bound has elapsed.
func (c L3Config) IsExpired() bool {
	if c.StartedAt.IsZero() {
		return false
	}
	return time.Since(c.StartedAt) > c.TimeBound
}

// AutonomyConfig configures autonomy behavior.
type AutonomyConfig struct {
	Level       AutonomyLevel
	AutoActions []string // Tool names auto-approved at L2
	AskActions  []string // Tool names requiring confirmation at L2
	L3          L3Config // L3-specific configuration
}

// ShouldAutoApprove returns true if the given tool should be auto-approved
// at the current autonomy level.
func (ac AutonomyConfig) ShouldAutoApprove(toolName string) bool {
	switch ac.Level {
	case AutonomyL3:
		return ac.shouldAutoApproveL3(toolName)
	case AutonomyL2:
		return ac.shouldAutoApproveL2(toolName)
	default:
		return true // L1: all tools auto-approved (gated by capabilities instead)
	}
}

// shouldAutoApproveL3 implements L3 autonomy logic.
// All actions auto-approved EXCEPT safety-critical ones.
// Returns false if the time bound has expired.
func (ac AutonomyConfig) shouldAutoApproveL3(toolName string) bool {
	// If time bound expired, revert to requiring confirmation.
	if ac.L3.IsExpired() {
		return false
	}

	// Safety-critical actions ALWAYS require confirmation.
	for _, safe := range ac.L3.SafetyActions {
		if safe == toolName {
			return false
		}
	}

	return true
}

// shouldAutoApproveL2 implements L2 autonomy logic.
func (ac AutonomyConfig) shouldAutoApproveL2(toolName string) bool {
	// If ask list is configured and tool is in it, don't auto-approve.
	if len(ac.AskActions) > 0 {
		for _, ask := range ac.AskActions {
			if ask == toolName {
				return false
			}
		}
		return true
	}

	// If auto list is configured, only auto-approve listed tools.
	if len(ac.AutoActions) > 0 {
		for _, auto := range ac.AutoActions {
			if auto == toolName {
				return true
			}
		}
		return false
	}

	// No lists configured: auto-approve everything.
	return true
}
