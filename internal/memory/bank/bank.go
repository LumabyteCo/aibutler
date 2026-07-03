// Package bank scopes memory to an isolation namespace. Every profile —
// the primary user context, each background worker in a multi-agent run —
// reads and writes its own bank by default; nothing crosses banks unless a
// caller was explicitly handed another bank's scope.
//
// The bank travels via context: stores read it at query time, so tools and
// stores need no per-call parameter plumbing, and code that never sets a
// bank gets the default one — existing single-profile behavior is unchanged
// (every pre-existing row carries the default bank).
package bank

import "context"

// Default is the primary user's bank; all rows created before banks existed
// belong to it.
const Default = "main"

type ctxKey struct{}

// With returns a context scoped to the given bank. Empty means Default.
func With(ctx context.Context, bank string) context.Context {
	if bank == "" {
		bank = Default
	}
	return context.WithValue(ctx, ctxKey{}, bank)
}

// FromContext returns the context's bank, or Default when unset.
func FromContext(ctx context.Context) string {
	if v, ok := ctx.Value(ctxKey{}).(string); ok && v != "" {
		return v
	}
	return Default
}
