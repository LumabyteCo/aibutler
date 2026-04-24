package capability

import "time"

// AuditLevel controls how much detail is logged for capability checks.
type AuditLevel int

const (
	AuditNone    AuditLevel = iota // No logging
	AuditSummary                   // Log denials only
	AuditFull                      // Log every check (allow + deny)
)

// RateLimit defines a sliding-window rate limit.
type RateLimit struct {
	MaxCalls int
	Window   time.Duration
}

// Capability represents a single permission grant.
type Capability struct {
	Resource string // e.g., "tool.shell.exec", "data.health.read"

	// Scope constraints (empty means unrestricted within the resource)
	Paths    []string // Allowed file/directory paths (glob patterns)
	Commands []string // Allowed shell commands
	Domains  []string // Allowed HTTP domains
	Channels []string // Allowed channel identifiers
	Devices  []string // Allowed IoT device identifiers

	// Limits
	RateLimit *RateLimit
	TTL       time.Duration // Auto-expiry (0 = no expiry)
	MaxCalls  int           // Total call limit (0 = unlimited)

	// Safety constraints
	RequiresConfirmation bool
	RequiresPIN          bool
	SafetyBounds         map[string]interface{}

	// Audit
	AuditLevel AuditLevel

	// Internal tracking (exported for testing and persistence)
	GrantedAt time.Time
}
