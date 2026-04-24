package testutil

import (
	"context"
	"sync"

	"github.com/LumabyteCo/aibutler/internal/capability"
)

// FakeAuditor records audit entries for testing.
type FakeAuditor struct {
	mu      sync.Mutex
	entries []capability.AuditEntry
}

// NewFakeAuditor creates a new FakeAuditor.
func NewFakeAuditor() *FakeAuditor {
	return &FakeAuditor{}
}

// LogAccess records the audit entry.
func (a *FakeAuditor) LogAccess(_ context.Context, entry capability.AuditEntry) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.entries = append(a.entries, entry)
	return nil
}

// Entries returns all recorded audit entries.
func (a *FakeAuditor) Entries() []capability.AuditEntry {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make([]capability.AuditEntry, len(a.entries))
	copy(out, a.entries)
	return out
}
