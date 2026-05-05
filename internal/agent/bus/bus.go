// Package bus provides agent-to-agent pub/sub messaging.
//
// Two delivery modes coexist on the same Bus:
//
//   - Best-effort (Publish / Subscribe): non-blocking, drops on slow
//     subscriber. Right for status streams where occasional message
//     loss is fine and the publisher must not block. This is the
//     historical v0.1 mode and is unchanged by the reliable extension.
//
//   - At-least-once (PublishReliable / SubscribeReliable): publisher
//     waits for at least one subscriber Ack before returning, with a
//     bounded retry budget. Right for worker→supervisor reporting in
//     the mission engine, where every state-change event matters.
//     Subscribers MUST call Ack() on successful handling or Nack() on
//     failure — failure to do either causes the publisher to time out
//     and retry up to MaxAttempts.
//
// The two paths are separate — a Publish call does NOT reach
// SubscribeReliable subscribers and vice versa. Choose the path that
// matches the consumer's needs.
package bus

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"sync"
	"time"
)

// Message is an inter-agent message published on a topic.
type Message struct {
	Topic   string
	From    string // agent ID
	Payload string
	Time    time.Time
}

// ReliableMessage is delivered via SubscribeReliable. Subscribers MUST
// call Ack() on successful handling or Nack() on failure — without
// either, the publisher times out and retries. Both methods are
// idempotent: only the first call (Ack OR Nack) takes effect.
//
// The ID field is stable across retries — the same logical publish
// produces the same ID every attempt, so subscribers can short-circuit
// duplicate handling if their work isn't naturally idempotent.
type ReliableMessage struct {
	ID      string
	Topic   string
	From    string
	Payload string
	Time    time.Time

	reply chan<- ackResult
	once  *sync.Once
}

// Ack signals successful handling. Value receiver — safe to call on
// either a value or a pointer; idempotency is guarded by the embedded
// *sync.Once and shared via the channel-pointer copy.
func (m ReliableMessage) Ack() {
	if m.once == nil || m.reply == nil {
		return
	}
	m.once.Do(func() {
		m.reply <- ackResult{ok: true, msgID: m.ID}
	})
}

// Nack signals failed handling. The publisher counts nacks and retries
// up to MaxAttempts; if every attempt is nacked, PublishReliable returns
// ErrAllNacked.
func (m ReliableMessage) Nack() {
	if m.once == nil || m.reply == nil {
		return
	}
	m.once.Do(func() {
		m.reply <- ackResult{ok: false, msgID: m.ID}
	})
}

type ackResult struct {
	ok    bool
	msgID string
}

// ReliableOpts configures PublishReliable. Zero values resolve to safe
// defaults.
type ReliableOpts struct {
	// Timeout is the per-attempt deadline for receiving acks. Default 2s,
	// max 30s.
	Timeout time.Duration
	// MaxAttempts caps the number of retries. Default 3, max 10.
	MaxAttempts int
	// RetryDelay is the pause between attempts. Default 200ms.
	RetryDelay time.Duration
	// SendTimeout caps how long the publisher waits to deliver into each
	// subscriber's channel buffer (slow consumers). Default 200ms.
	SendTimeout time.Duration
}

// Sentinel errors for the reliable delivery path.
var (
	// ErrNoSubscribers — no SubscribeReliable subscribers exist for the
	// topic at the time of PublishReliable.
	ErrNoSubscribers = errors.New("bus: no reliable subscribers for topic")
	// ErrAckTimeout — every attempt timed out without any subscriber acking.
	ErrAckTimeout = errors.New("bus: timed out waiting for acknowledgement")
	// ErrAllNacked — every subscriber explicitly nacked, on every attempt.
	ErrAllNacked = errors.New("bus: all subscribers nacked across all attempts")
)

// Bus is a publish-subscribe message bus for agent-to-agent communication.
type Bus struct {
	mu           sync.RWMutex
	subscribers  map[string][]chan Message
	reliableSubs map[string][]chan ReliableMessage
}

// New creates a new message bus.
func New() *Bus {
	return &Bus{
		subscribers:  make(map[string][]chan Message),
		reliableSubs: make(map[string][]chan ReliableMessage),
	}
}

// Subscribe registers a channel to receive messages on the given topic.
// Returns a channel that will receive messages.
func (b *Bus) Subscribe(topic string) <-chan Message {
	ch := make(chan Message, 64)
	b.mu.Lock()
	b.subscribers[topic] = append(b.subscribers[topic], ch)
	b.mu.Unlock()
	return ch
}

// Unsubscribe removes a channel from the given topic's subscriber list.
func (b *Bus) Unsubscribe(topic string, ch <-chan Message) {
	b.mu.Lock()
	defer b.mu.Unlock()

	subs := b.subscribers[topic]
	for i, sub := range subs {
		if sub == ch {
			b.subscribers[topic] = append(subs[:i], subs[i+1:]...)
			close(sub)
			return
		}
	}
}

// Publish sends a message to all subscribers of the given topic.
// Non-blocking: if a subscriber's channel is full, the message is dropped for that subscriber.
func (b *Bus) Publish(topic string, from, payload string) {
	msg := Message{
		Topic:   topic,
		From:    from,
		Payload: payload,
		Time:    time.Now(),
	}

	b.mu.RLock()
	// Copy subscriber slice under lock to prevent race with concurrent Unsubscribe.
	subs := make([]chan Message, len(b.subscribers[topic]))
	copy(subs, b.subscribers[topic])
	b.mu.RUnlock()

	for _, ch := range subs {
		// Recover from send-on-closed-channel if Unsubscribe closes ch concurrently.
		func() {
			defer func() { recover() }()
			select {
			case ch <- msg:
			default:
				// Drop message if subscriber is not keeping up.
			}
		}()
	}
}

// TopicCount returns the number of topics with active best-effort subscribers.
func (b *Bus) TopicCount() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.subscribers)
}

// --- At-least-once delivery ---

// SubscribeReliable registers a channel for at-least-once message delivery
// on the given topic. Subscribers MUST call Ack() / Nack() on each
// received ReliableMessage — the publisher's PublishReliable call is
// blocked until at least one subscriber acks (or all nack, or timeout).
//
// Buffer size is small (8) because publishers wait for acks; a flooded
// buffer means the consumer is overwhelmed and back-pressure should
// reach the publisher quickly.
func (b *Bus) SubscribeReliable(topic string) <-chan ReliableMessage {
	ch := make(chan ReliableMessage, 8)
	b.mu.Lock()
	b.reliableSubs[topic] = append(b.reliableSubs[topic], ch)
	b.mu.Unlock()
	return ch
}

// UnsubscribeReliable removes a reliable subscription and closes the channel.
// Pending acks from this subscriber are lost — pending PublishReliable
// calls treat that as a timeout and may retry.
func (b *Bus) UnsubscribeReliable(topic string, ch <-chan ReliableMessage) {
	b.mu.Lock()
	defer b.mu.Unlock()
	subs := b.reliableSubs[topic]
	for i, sub := range subs {
		if sub == ch {
			b.reliableSubs[topic] = append(subs[:i], subs[i+1:]...)
			close(sub)
			return
		}
	}
}

// ReliableTopicCount returns the number of topics with active reliable subscribers.
func (b *Bus) ReliableTopicCount() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.reliableSubs)
}

// PublishReliable sends a message with at-least-once semantics. Returns
// nil when at least one subscriber acks. Returns:
//
//   - ErrNoSubscribers — no reliable subscribers exist for the topic
//     at the moment of publish (no retry — situation won't improve).
//   - ErrAckTimeout — every attempt timed out without acks.
//   - ErrAllNacked — every subscriber nacked on every attempt.
//   - ctx.Err() — context cancelled mid-retry.
//
// The same msgID is reused across retries so subscribers can detect
// duplicates and skip non-idempotent work.
func (b *Bus) PublishReliable(ctx context.Context, topic, from, payload string, opts ReliableOpts) error {
	opts = applyReliableDefaults(opts)
	msgID := newReliableMsgID()

	var lastErr error
	for attempt := 0; attempt < opts.MaxAttempts; attempt++ {
		if attempt > 0 {
			select {
			case <-time.After(opts.RetryDelay):
			case <-ctx.Done():
				return ctx.Err()
			}
		}

		err := b.tryPublishReliable(ctx, msgID, topic, from, payload, opts)
		if err == nil {
			return nil
		}
		// ErrNoSubscribers won't get better — fail fast.
		if errors.Is(err, ErrNoSubscribers) {
			return err
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		lastErr = err
	}

	if lastErr == nil {
		lastErr = ErrAckTimeout
	}
	return lastErr
}

// tryPublishReliable runs a single attempt of the publish-and-await-ack loop.
func (b *Bus) tryPublishReliable(ctx context.Context, msgID, topic, from, payload string, opts ReliableOpts) error {
	b.mu.RLock()
	subs := make([]chan ReliableMessage, len(b.reliableSubs[topic]))
	copy(subs, b.reliableSubs[topic])
	b.mu.RUnlock()

	if len(subs) == 0 {
		return ErrNoSubscribers
	}

	reply := make(chan ackResult, len(subs))
	delivered := 0
	now := time.Now()

	for _, ch := range subs {
		// Each delivery gets its own once — Ack/Nack from one subscriber
		// must not affect others.
		msg := ReliableMessage{
			ID:      msgID,
			Topic:   topic,
			From:    from,
			Payload: payload,
			Time:    now,
			reply:   reply,
			once:    &sync.Once{},
		}

		// Bounded send — if subscriber's buffer is full beyond
		// SendTimeout, skip them this attempt (they may catch up on retry).
		sendDeadline := time.NewTimer(opts.SendTimeout)
		select {
		case ch <- msg:
			delivered++
		case <-sendDeadline.C:
			// Subscriber too slow this round.
		case <-ctx.Done():
			sendDeadline.Stop()
			return ctx.Err()
		}
		sendDeadline.Stop()
	}

	if delivered == 0 {
		// Couldn't enqueue to any subscriber — treat as timeout and let
		// the outer retry loop handle it.
		return ErrAckTimeout
	}

	// Wait for ack/nack/timeout.
	nackCount := 0
	deadline := time.NewTimer(opts.Timeout)
	defer deadline.Stop()
	for nackCount < delivered {
		select {
		case res := <-reply:
			if res.ok {
				return nil
			}
			nackCount++
		case <-deadline.C:
			return ErrAckTimeout
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return ErrAllNacked
}

// applyReliableDefaults fills in zero-valued fields with safe defaults
// and clamps unsafe values.
func applyReliableDefaults(opts ReliableOpts) ReliableOpts {
	if opts.Timeout <= 0 {
		opts.Timeout = 2 * time.Second
	}
	if opts.Timeout > 30*time.Second {
		opts.Timeout = 30 * time.Second
	}
	if opts.MaxAttempts <= 0 {
		opts.MaxAttempts = 3
	}
	if opts.MaxAttempts > 10 {
		opts.MaxAttempts = 10
	}
	if opts.RetryDelay <= 0 {
		opts.RetryDelay = 200 * time.Millisecond
	}
	if opts.SendTimeout <= 0 {
		opts.SendTimeout = 200 * time.Millisecond
	}
	return opts
}

// newReliableMsgID generates a stable per-publish ID. Same logical
// publish call uses the same ID across retries so subscribers can
// detect duplicates.
func newReliableMsgID() string {
	buf := make([]byte, 8)
	_, _ = rand.Read(buf)
	return "msg_" + hex.EncodeToString(buf)
}
