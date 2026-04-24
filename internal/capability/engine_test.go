package capability_test

import (
	"context"
	"testing"
	"time"

	"github.com/LumabyteCo/aibutler/internal/capability"
)

func TestCheckNoCapability(t *testing.T) {
	engine := capability.NewEngine(nil)
	cs := capability.NewCapabilitySet(nil)

	result := engine.Check(context.Background(), cs, capability.CheckRequest{
		Resource: "tool.shell.exec",
	})
	if result.Allowed {
		t.Error("expected denied, got allowed")
	}
	if result.Reason != "no_capability" {
		t.Errorf("reason = %q, want 'no_capability'", result.Reason)
	}
}

func TestCheckGranted(t *testing.T) {
	engine := capability.NewEngine(nil)
	cs := capability.NewCapabilitySet([]capability.Capability{
		{Resource: "tool.shell.exec", Commands: []string{"npm test"}},
	})

	result := engine.Check(context.Background(), cs, capability.CheckRequest{
		Resource: "tool.shell.exec",
		Command:  "npm test",
	})
	if !result.Allowed {
		t.Errorf("expected allowed, got denied: %s", result.Reason)
	}
}

func TestCheckScopeDenied(t *testing.T) {
	engine := capability.NewEngine(nil)
	cs := capability.NewCapabilitySet([]capability.Capability{
		{Resource: "tool.shell.exec", Commands: []string{"npm test"}},
	})

	result := engine.Check(context.Background(), cs, capability.CheckRequest{
		Resource: "tool.shell.exec",
		Command:  "rm -rf /",
	})
	if result.Allowed {
		t.Error("expected denied for out-of-scope command")
	}
	if result.Reason != "scope_denied" {
		t.Errorf("reason = %q, want 'scope_denied'", result.Reason)
	}
}

func TestCheckPathTraversalRejected(t *testing.T) {
	engine := capability.NewEngine(nil)
	cs := capability.NewCapabilitySet([]capability.Capability{
		{Resource: "tool.file.read", Paths: []string{"./project/"}},
	})

	result := engine.Check(context.Background(), cs, capability.CheckRequest{
		Resource: "tool.file.read",
		Path:     "./project/../../../etc/passwd",
	})
	if result.Allowed {
		t.Error("expected denied for path traversal")
	}
}

func TestCheckRateLimit(t *testing.T) {
	engine := capability.NewEngine(nil)
	cs := capability.NewCapabilitySet([]capability.Capability{
		{Resource: "tool.web.fetch", RateLimit: &capability.RateLimit{MaxCalls: 2, Window: time.Minute}},
	})

	ctx := context.Background()
	req := capability.CheckRequest{Resource: "tool.web.fetch"}

	// First two should succeed.
	for i := 0; i < 2; i++ {
		r := engine.Check(ctx, cs, req)
		if !r.Allowed {
			t.Fatalf("call %d: expected allowed", i+1)
		}
	}

	// Third should be rate limited.
	r := engine.Check(ctx, cs, req)
	if r.Allowed {
		t.Error("expected rate limited")
	}
	if r.Reason != "rate_limited" {
		t.Errorf("reason = %q, want 'rate_limited'", r.Reason)
	}
}

func TestCheckTTLExpired(t *testing.T) {
	engine := capability.NewEngine(nil)

	now := time.Date(2026, 3, 6, 12, 0, 0, 0, time.UTC)
	engine.SetClock(func() time.Time { return now })

	cs := capability.NewCapabilitySet([]capability.Capability{
		{Resource: "tool.web.search", TTL: time.Hour, GrantedAt: now},
	})

	ctx := context.Background()
	req := capability.CheckRequest{Resource: "tool.web.search"}

	// Should work within TTL.
	r := engine.Check(ctx, cs, req)
	if !r.Allowed {
		t.Fatalf("expected allowed within TTL: %s", r.Reason)
	}

	// Advance past TTL.
	now = now.Add(2 * time.Hour)
	r = engine.Check(ctx, cs, req)
	if r.Allowed {
		t.Error("expected denied after TTL")
	}
	if r.Reason != "ttl_expired" {
		t.Errorf("reason = %q, want 'ttl_expired'", r.Reason)
	}
}

func TestCheckMaxCalls(t *testing.T) {
	engine := capability.NewEngine(nil)
	cs := capability.NewCapabilitySet([]capability.Capability{
		{Resource: "tool.web.search", MaxCalls: 3},
	})

	ctx := context.Background()
	req := capability.CheckRequest{Resource: "tool.web.search"}

	for i := 0; i < 3; i++ {
		r := engine.Check(ctx, cs, req)
		if !r.Allowed {
			t.Fatalf("call %d: expected allowed", i+1)
		}
	}

	r := engine.Check(ctx, cs, req)
	if r.Allowed {
		t.Error("expected denied after max calls")
	}
	if r.Reason != "max_calls_exceeded" {
		t.Errorf("reason = %q, want 'max_calls_exceeded'", r.Reason)
	}
}

func TestCheckWildcardResource(t *testing.T) {
	engine := capability.NewEngine(nil)
	cs := capability.NewCapabilitySet([]capability.Capability{
		{Resource: "tool.lsp.*"},
	})

	tests := []struct {
		resource string
		allowed  bool
	}{
		{"tool.lsp.hover", true},
		{"tool.lsp.completion", true},
		{"tool.shell.exec", false},
	}

	ctx := context.Background()
	for _, tt := range tests {
		r := engine.Check(ctx, cs, capability.CheckRequest{Resource: tt.resource})
		if r.Allowed != tt.allowed {
			t.Errorf("resource %q: allowed=%v, want %v", tt.resource, r.Allowed, tt.allowed)
		}
	}
}

func TestCheckDomainWildcard(t *testing.T) {
	engine := capability.NewEngine(nil)
	cs := capability.NewCapabilitySet([]capability.Capability{
		{Resource: "tool.web.fetch", Domains: []string{"*.github.com"}},
	})

	ctx := context.Background()
	tests := []struct {
		domain  string
		allowed bool
	}{
		{"api.github.com", true},
		{"raw.github.com", true},
		{"github.com", false},
		{"evil.com", false},
	}

	for _, tt := range tests {
		r := engine.Check(ctx, cs, capability.CheckRequest{
			Resource: "tool.web.fetch",
			Domain:   tt.domain,
		})
		if r.Allowed != tt.allowed {
			t.Errorf("domain %q: allowed=%v, want %v", tt.domain, r.Allowed, tt.allowed)
		}
	}
}

func TestCheckIoTSafety(t *testing.T) {
	engine := capability.NewEngine(nil)
	cs := capability.NewCapabilitySet([]capability.Capability{
		{
			Resource:             "iot.safety.control",
			Devices:              []string{"lock-*"},
			RequiresConfirmation: true,
			RequiresPIN:          true,
			SafetyBounds:         map[string]interface{}{"cooldown": 60},
			AuditLevel:           capability.AuditFull,
		},
	})

	r := engine.Check(context.Background(), cs, capability.CheckRequest{
		Resource: "iot.safety.control",
		Device:   "lock-front-door",
	})
	if !r.Allowed {
		t.Fatalf("expected allowed: %s", r.Reason)
	}
	if !r.RequiresConfirmation {
		t.Error("expected RequiresConfirmation=true")
	}
	if !r.RequiresPIN {
		t.Error("expected RequiresPIN=true")
	}
	if r.SafetyBounds["cooldown"] != 60 {
		t.Errorf("SafetyBounds[cooldown] = %v, want 60", r.SafetyBounds["cooldown"])
	}
}

func TestCheckDeviceGlobDenied(t *testing.T) {
	engine := capability.NewEngine(nil)
	cs := capability.NewCapabilitySet([]capability.Capability{
		{Resource: "iot.device.control", Devices: []string{"lights-*"}},
	})

	r := engine.Check(context.Background(), cs, capability.CheckRequest{
		Resource: "iot.device.control",
		Device:   "lock-front-door",
	})
	if r.Allowed {
		t.Error("expected denied for non-matching device glob")
	}
}

func TestDefaultSets(t *testing.T) {
	terminal := capability.TerminalDefaults()
	if len(terminal) == 0 {
		t.Error("TerminalDefaults returned empty")
	}

	messaging := capability.MessagingDefaults()
	if len(messaging) == 0 {
		t.Error("MessagingDefaults returned empty")
	}

	iot := capability.IoTDefaults()
	if len(iot) == 0 {
		t.Error("IoTDefaults returned empty")
	}

	// Terminal should have file.read but not channel.write.
	engine := capability.NewEngine(nil)
	cs := capability.NewCapabilitySet(terminal)
	ctx := context.Background()

	r := engine.Check(ctx, cs, capability.CheckRequest{Resource: "tool.file.read", Path: "./main.go"})
	if !r.Allowed {
		t.Error("terminal: file.read should be allowed")
	}

	r = engine.Check(ctx, cs, capability.CheckRequest{Resource: "channel.write"})
	if r.Allowed {
		t.Error("terminal: channel.write should not be allowed")
	}
}
