package capability

import (
	"context"
	"sync"
	"time"
)

// CheckRequest describes what the agent is trying to do.
type CheckRequest struct {
	Resource string // e.g., "tool.shell.exec"
	Path     string // e.g., "/home/user/project/main.go"
	Command  string // e.g., "npm test"
	Domain   string // e.g., "api.github.com"
	Channel  string // e.g., "telegram"
	Device   string // e.g., "lock-front-door"
	// Probe is true when the caller is only asking "could this succeed?"
	// (e.g. filtering the visible tool list). Probe checks skip audit logging
	// and do NOT consume the rate-limit budget — only real, about-to-execute
	// calls should count against the budget.
	Probe bool
}

// CheckResult is the outcome of a capability check.
type CheckResult struct {
	Allowed              bool
	Reason               string // "granted", "no_capability", "rate_limited", "ttl_expired", "scope_denied", "max_calls_exceeded"
	RequiresConfirmation bool
	RequiresPIN          bool
	SafetyBounds         map[string]interface{}
}

// AuditEntry represents one row in resource_access_log.
type AuditEntry struct {
	Timestamp      time.Time
	AgentID        string
	AgentType      string
	SessionID      string
	ScheduleName   string
	ResourceType   string
	Service        string
	Action         string
	Target         string
	CapabilityUsed string
	CredentialKey  string
	Status         string
	Error          string
	TokensConsumed int
	CostUSD        float64
}

// Auditor is an interface for writing audit log entries.
type Auditor interface {
	LogAccess(ctx context.Context, entry AuditEntry) error
}

// noopAuditor discards all audit entries.
type noopAuditor struct{}

func (noopAuditor) LogAccess(_ context.Context, _ AuditEntry) error { return nil }

// Engine evaluates capability checks against a set of grants.
type Engine struct {
	auditor Auditor
	clock   func() time.Time
}

// NewEngine creates a capability engine with the given auditor.
// If auditor is nil, a no-op auditor is used.
func NewEngine(auditor Auditor) *Engine {
	if auditor == nil {
		auditor = noopAuditor{}
	}
	return &Engine{
		auditor: auditor,
		clock:   time.Now,
	}
}

// SetClock overrides the time source (for testing).
func (e *Engine) SetClock(fn func() time.Time) {
	e.clock = fn
}

// Check evaluates whether the given capability set grants access for the request.
// Probe requests (req.Probe=true) skip audit logging and do not consume
// the rate-limit budget.
func (e *Engine) Check(ctx context.Context, cs *CapabilitySet, req CheckRequest) CheckResult {
	result := cs.check(req, e.clock)

	// Audit based on the capability's audit level. Probe checks are never
	// audited — they are introspection, not action.
	if !req.Probe && (result.auditLevel == AuditFull || (result.auditLevel == AuditSummary && !result.Allowed)) {
		_ = e.auditor.LogAccess(ctx, AuditEntry{
			Timestamp:      e.clock(),
			CapabilityUsed: req.Resource,
			Action:         req.Resource,
			Status:         result.Reason,
		})
	}

	return result.CheckResult
}

// CapabilitySet is a collection of capabilities with rate limit tracking.
type CapabilitySet struct {
	capabilities []Capability
	mu           sync.Mutex
	calls        map[string][]time.Time // resource -> call timestamps
	totalCalls   map[string]int         // resource -> total calls (for MaxCalls)
}

// NewCapabilitySet creates a new set from a list of capabilities.
func NewCapabilitySet(caps []Capability) *CapabilitySet {
	now := time.Now()
	for i := range caps {
		if caps[i].GrantedAt.IsZero() {
			caps[i].GrantedAt = now
		}
	}
	return &CapabilitySet{
		capabilities: caps,
		calls:        make(map[string][]time.Time),
		totalCalls:   make(map[string]int),
	}
}

// Capabilities returns the underlying capabilities slice.
func (cs *CapabilitySet) Capabilities() []Capability {
	return cs.capabilities
}

type checkResultInternal struct {
	CheckResult
	auditLevel AuditLevel
}

func (cs *CapabilitySet) check(req CheckRequest, clock func() time.Time) checkResultInternal {
	// 1. Find matching grant.
	cap, found := cs.findGrant(req.Resource)
	if !found {
		return checkResultInternal{
			CheckResult: CheckResult{Allowed: false, Reason: "no_capability"},
			auditLevel:  AuditSummary, // always log denials at summary level
		}
	}

	// 2. Check scope.
	if !matchPath(cap.Paths, req.Path) ||
		!matchCommand(cap.Commands, req.Command) ||
		!matchDomain(cap.Domains, req.Domain) ||
		!matchChannel(cap.Channels, req.Channel) ||
		!matchDevice(cap.Devices, req.Device) {
		return checkResultInternal{
			CheckResult: CheckResult{Allowed: false, Reason: "scope_denied"},
			auditLevel:  cap.AuditLevel,
		}
	}

	// 3. Check rate limit.
	if cap.RateLimit != nil {
		cs.mu.Lock()
		now := clock()
		windowStart := now.Add(-cap.RateLimit.Window)
		// Clean old entries.
		var recent []time.Time
		for _, t := range cs.calls[req.Resource] {
			if t.After(windowStart) {
				recent = append(recent, t)
			}
		}
		if len(recent) >= cap.RateLimit.MaxCalls {
			cs.mu.Unlock()
			return checkResultInternal{
				CheckResult: CheckResult{Allowed: false, Reason: "rate_limited"},
				auditLevel:  cap.AuditLevel,
			}
		}
		// Only real calls (not probes) consume the rate-limit budget.
		if !req.Probe {
			cs.calls[req.Resource] = append(recent, now)
		}
		cs.mu.Unlock()
	}

	// 4. Check TTL.
	if cap.TTL > 0 {
		if clock().After(cap.GrantedAt.Add(cap.TTL)) {
			return checkResultInternal{
				CheckResult: CheckResult{Allowed: false, Reason: "ttl_expired"},
				auditLevel:  cap.AuditLevel,
			}
		}
	}

	// 5. Check MaxCalls.
	if cap.MaxCalls > 0 {
		cs.mu.Lock()
		if cs.totalCalls[req.Resource] >= cap.MaxCalls {
			cs.mu.Unlock()
			return checkResultInternal{
				CheckResult: CheckResult{Allowed: false, Reason: "max_calls_exceeded"},
				auditLevel:  cap.AuditLevel,
			}
		}
		cs.totalCalls[req.Resource]++
		cs.mu.Unlock()
	}

	// 6. Record call for rate limiting (if not already done above).
	if cap.RateLimit == nil {
		cs.mu.Lock()
		cs.calls[req.Resource] = append(cs.calls[req.Resource], clock())
		cs.mu.Unlock()
	}

	// 7. Return allow.
	return checkResultInternal{
		CheckResult: CheckResult{
			Allowed:              true,
			Reason:               "granted",
			RequiresConfirmation: cap.RequiresConfirmation,
			RequiresPIN:          cap.RequiresPIN,
			SafetyBounds:         cap.SafetyBounds,
		},
		auditLevel: cap.AuditLevel,
	}
}

func (cs *CapabilitySet) findGrant(resource string) (Capability, bool) {
	for _, cap := range cs.capabilities {
		if cap.Resource == resource {
			return cap, true
		}
		// Wildcard match: "tool.lsp.*" matches "tool.lsp.hover"
		if len(cap.Resource) > 0 && cap.Resource[len(cap.Resource)-1] == '*' {
			prefix := cap.Resource[:len(cap.Resource)-1]
			if len(resource) >= len(prefix) && resource[:len(prefix)] == prefix {
				return cap, true
			}
		}
	}
	return Capability{}, false
}
