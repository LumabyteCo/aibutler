package sandbox

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// Manifest is the narrow interface used by sandbox to avoid import cycles.
type Manifest struct {
	Name         string
	Capabilities []string
}

// Policy defines execution constraints for a plugin.
type Policy struct {
	MaxMemoryMB      int
	MaxExecutionSecs int
	AllowNetwork     bool
	AllowFileSystem  bool
	MaxOutputBytes   int
}

// DefaultPolicy returns a permissive policy for trusted plugins.
func DefaultPolicy() Policy {
	return Policy{
		MaxMemoryMB:      64,
		MaxExecutionSecs: 60,
		AllowNetwork:     true,
		AllowFileSystem:  false,
		MaxOutputBytes:   10 * 1024 * 1024, // 10MB
	}
}

// StrictPolicy returns a locked-down policy: no network, no filesystem, 32MB, 30s.
func StrictPolicy() Policy {
	return Policy{
		MaxMemoryMB:      32,
		MaxExecutionSecs: 30,
		AllowNetwork:     false,
		AllowFileSystem:  false,
		MaxOutputBytes:   1 * 1024 * 1024, // 1MB
	}
}

// Sandbox enforces policy-based restrictions on plugin execution.
type Sandbox struct {
	policy Policy
}

// New creates a sandbox with the given policy.
func New(policy Policy) *Sandbox {
	return &Sandbox{policy: policy}
}

// Validate checks that a manifest conforms to the sandbox policy.
func (s *Sandbox) Validate(m *Manifest) error {
	if !s.policy.AllowNetwork {
		for _, cap := range m.Capabilities {
			lower := strings.ToLower(cap)
			if strings.HasPrefix(lower, "http") || strings.HasPrefix(lower, "web") || lower == "network" {
				return fmt.Errorf("sandbox: plugin %q requests network capability %q but policy disallows network", m.Name, cap)
			}
		}
	}
	if !s.policy.AllowFileSystem {
		for _, cap := range m.Capabilities {
			lower := strings.ToLower(cap)
			if strings.HasPrefix(lower, "fs.") || lower == "filesystem" || strings.HasPrefix(lower, "file.") {
				return fmt.Errorf("sandbox: plugin %q requests filesystem capability %q but policy disallows filesystem", m.Name, cap)
			}
		}
	}
	return nil
}

// WrapExecution enforces timeout and output size limits around a plugin call.
func (s *Sandbox) WrapExecution(ctx context.Context, fn func(ctx context.Context) ([]byte, error)) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, time.Duration(s.policy.MaxExecutionSecs)*time.Second)
	defer cancel()

	out, err := fn(ctx)
	if err != nil {
		return nil, err
	}
	if s.policy.MaxOutputBytes > 0 && len(out) > s.policy.MaxOutputBytes {
		return nil, fmt.Errorf("sandbox: output size %d exceeds limit %d", len(out), s.policy.MaxOutputBytes)
	}
	return out, nil
}
