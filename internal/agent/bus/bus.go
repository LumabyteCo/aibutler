package bus

import (
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

// Bus is a publish-subscribe message bus for agent-to-agent communication.
type Bus struct {
	mu          sync.RWMutex
	subscribers map[string][]chan Message
}

// New creates a new message bus.
func New() *Bus {
	return &Bus{
		subscribers: make(map[string][]chan Message),
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

// TopicCount returns the number of topics with active subscribers.
func (b *Bus) TopicCount() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.subscribers)
}
