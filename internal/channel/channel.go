package channel

import (
	"context"
	"errors"
	"sync"
)

// ErrNotImplemented is returned by stub adapters.
var ErrNotImplemented = errors.New("channel: not implemented")

// MessageHandler processes incoming messages from a channel.
type MessageHandler func(ctx context.Context, env Envelope) error

// Channel is the interface that all messaging adapters must implement.
type Channel interface {
	Name() string
	Start(ctx context.Context, handler MessageHandler) error
	Stop(ctx context.Context) error
	Send(ctx context.Context, accountID string, msg OutgoingMessage) error
	SendTyping(ctx context.Context, accountID string) error
}

// Registry holds all registered channel adapters.
type Registry struct {
	mu       sync.RWMutex
	channels map[string]Channel
}

// NewRegistry creates an empty channel registry.
func NewRegistry() *Registry {
	return &Registry{channels: make(map[string]Channel)}
}

// Register adds a channel adapter to the registry.
func (r *Registry) Register(ch Channel) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.channels[ch.Name()] = ch
}

// Get retrieves a channel by name.
func (r *Registry) Get(name string) (Channel, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	ch, ok := r.channels[name]
	return ch, ok
}

// All returns a copy of all registered channels.
func (r *Registry) All() map[string]Channel {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make(map[string]Channel, len(r.channels))
	for k, v := range r.channels {
		out[k] = v
	}
	return out
}
