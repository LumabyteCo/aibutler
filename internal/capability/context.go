package capability

import "context"

type capsContextKey struct{}

// WithCaps adds a CapabilitySet to the context.
func WithCaps(ctx context.Context, caps *CapabilitySet) context.Context {
	return context.WithValue(ctx, capsContextKey{}, caps)
}

// CapsFromContext extracts a CapabilitySet from the context.
func CapsFromContext(ctx context.Context) *CapabilitySet {
	caps, _ := ctx.Value(capsContextKey{}).(*CapabilitySet)
	return caps
}
