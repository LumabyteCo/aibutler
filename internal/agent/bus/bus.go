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
	mathrand "math/rand/v2"
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
	mu            sync.RWMutex
	subscribers   map[string][]chan Message
	reliableSubs  map[string][]chan ReliableMessage
	competingSubs map[string][]chan ReliableMessage
}

// New creates a new message bus.
func New() *Bus {
	return &Bus{
		subscribers:   make(map[string][]chan Message),
		reliableSubs:  make(map[string][]chan ReliableMessage),
		competingSubs: make(map[string][]chan ReliableMessage),
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

// --- Competing-consumer (queue group) delivery ---

// SubscribeCompeting registers a channel as one consumer in the
// competing-consumer group for the given topic. When the publisher
// calls PublishCompeting, the message is delivered to EXACTLY ONE
// subscriber in the group — the first one to ack wins. Other peers
// don't see the message at all.
//
// This is the right model for "fan-out work to a pool" — e.g. mission
// dispatch where multiple workers compete for incoming tasks. Use
// SubscribeReliable when every subscriber should see every message
// (broadcast model — e.g. event tailing in the dashboard).
//
// The channel is intentionally UNBUFFERED. With any buffer at all,
// supervisor-side dispatch concurrency would queue work onto whichever
// subscriber the bus picked first — even if that subscriber is busy
// processing a prior message — leaving peers idle. Unbuffered + the
// publish-side SendTimeout gives clean "busy peer skipped, try next"
// semantics: the supervisor's send only completes when a subscriber is
// actively waiting in its receive select (i.e. truly idle).
func (b *Bus) SubscribeCompeting(topic string) <-chan ReliableMessage {
	ch := make(chan ReliableMessage)
	b.mu.Lock()
	b.competingSubs[topic] = append(b.competingSubs[topic], ch)
	b.mu.Unlock()
	return ch
}

// UnsubscribeCompeting removes a competing-consumer subscription and
// closes the channel. Any pending message that hadn't been acked is
// treated as lost — the publisher's outer retry loop handles it.
func (b *Bus) UnsubscribeCompeting(topic string, ch <-chan ReliableMessage) {
	b.mu.Lock()
	defer b.mu.Unlock()
	subs := b.competingSubs[topic]
	for i, sub := range subs {
		if sub == ch {
			b.competingSubs[topic] = append(subs[:i], subs[i+1:]...)
			close(sub)
			return
		}
	}
}

// PublishCompeting sends a message with at-least-once semantics to
// EXACTLY ONE subscriber in the competing-consumer group for the topic.
// The publisher iterates subscribers in a per-call randomised order
// (so over many publishes the load distributes fairly) and stops at
// the first ack.
//
// Errors:
//   - ErrNoSubscribers — no competing subscribers exist at publish time.
//   - ErrAckTimeout    — every subscriber either had a full buffer
//                        (busy with a prior message) or accepted but
//                        didn't ack within opts.Timeout.
//   - ErrAllNacked     — every subscriber explicitly nacked.
//   - ctx.Err()        — context cancelled mid-publish.
//
// The same msgID is reused across the outer retry loop so a subscriber
// can detect a duplicate if a prior attempt landed mid-process.
func (b *Bus) PublishCompeting(ctx context.Context, topic, from, payload string, opts ReliableOpts) error {
	opts = applyReliableDefaults(opts)
	// Competing-consumer should fall through busy peers quickly. The
	// broadcast-style 200 ms SendTimeout default would otherwise stall
	// the dispatch loop on the first busy peer for the same duration
	// as a typical worker handler, defeating the parallelism. 50 ms
	// gives ample slack for scheduling jitter while still skipping
	// busy peers fast.
	if opts.SendTimeout > 50*time.Millisecond {
		opts.SendTimeout = 50 * time.Millisecond
	}
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

		err := b.tryPublishCompeting(ctx, msgID, topic, from, payload, opts)
		if err == nil {
			return nil
		}
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

// tryPublishCompeting runs a single attempt. Returns nil on first
// successful ack, an aggregate error otherwise.
func (b *Bus) tryPublishCompeting(ctx context.Context, msgID, topic, from, payload string, opts ReliableOpts) error {
	b.mu.RLock()
	subs := make([]chan ReliableMessage, len(b.competingSubs[topic]))
	copy(subs, b.competingSubs[topic])
	b.mu.RUnlock()

	if len(subs) == 0 {
		return ErrNoSubscribers
	}

	// Shuffle for per-call fair distribution. math/rand/v2 is safe for
	// concurrent use (no global Mutex required) and seeded from the OS.
	mathrand.Shuffle(len(subs), func(i, j int) { subs[i], subs[j] = subs[j], subs[i] })

	nackCount := 0
	now := time.Now()

	for _, ch := range subs {
		if err := ctx.Err(); err != nil {
			return err
		}
		// Each attempt-per-subscriber gets a fresh reply channel + once
		// so an earlier subscriber's late ack can't unblock this one.
		reply := make(chan ackResult, 1)
		msg := ReliableMessage{
			ID:      msgID,
			Topic:   topic,
			From:    from,
			Payload: payload,
			Time:    now,
			reply:   reply,
			once:    &sync.Once{},
		}

		// Try to enqueue with a bounded send-timeout. If the subscriber's
		// buffer is full (already processing prior work — buffer-of-1
		// guarantees only one in-flight at a time), skip to the next.
		sendTimer := time.NewTimer(opts.SendTimeout)
		var enqueued bool
		select {
		case ch <- msg:
			enqueued = true
		case <-sendTimer.C:
			// busy peer; try next
		case <-ctx.Done():
			sendTimer.Stop()
			return ctx.Err()
		}
		sendTimer.Stop()
		if !enqueued {
			continue
		}

		// Wait for this subscriber to ack/nack.
		ackTimer := time.NewTimer(opts.Timeout)
		select {
		case res := <-reply:
			ackTimer.Stop()
			if res.ok {
				return nil
			}
			nackCount++
		case <-ackTimer.C:
			// Subscriber didn't ack — they may still process the
			// message and ack late, in which case the work would happen
			// twice (the next subscriber gets a publish too). That's
			// acceptable for at-least-once; handlers should be
			// idempotent or use msg.ID to deduplicate.
		case <-ctx.Done():
			ackTimer.Stop()
			return ctx.Err()
		}
	}

	if nackCount == len(subs) {
		return ErrAllNacked
	}
	return ErrAckTimeout
}

// CompetingTopicCount returns the number of topics with active
// competing-consumer subscribers.
func (b *Bus) CompetingTopicCount() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.competingSubs)
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
