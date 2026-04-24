package channel

import (
	"context"
	"strings"
	"time"
)

// StreamDelivery manages delivering streamed tokens to a channel.
// For WebSocket channels, tokens are sent immediately.
// For edit-based channels (Telegram, Slack, Discord), tokens are buffered
// and sent as edits at a configurable interval.
type StreamDelivery struct {
	channel   Channel
	accountID string
	buffer    strings.Builder
	interval  time.Duration // batch interval for edit-based channels (500ms)
	lastSend  time.Time
	editID    string // message ID for editing (set after first send)
	sent      bool   // whether we've sent at least once
}

// NewStreamDelivery creates a StreamDelivery for the given channel.
// interval controls how often buffered content is flushed via edits.
// Use 0 for immediate delivery (WebSocket/terminal).
func NewStreamDelivery(ch Channel, accountID string, interval time.Duration) *StreamDelivery {
	return &StreamDelivery{
		channel:   ch,
		accountID: accountID,
		interval:  interval,
	}
}

// DeliverToken receives a single text token and decides whether to send/edit.
// For zero-interval channels, each token is sent immediately as a new message.
// For edit-based channels, tokens are buffered and sent as edits at the configured interval.
func (d *StreamDelivery) DeliverToken(ctx context.Context, token string) error {
	d.buffer.WriteString(token)

	// Immediate mode: send every token.
	if d.interval == 0 {
		return d.channel.Send(ctx, d.accountID, OutgoingMessage{
			Text:      d.buffer.String(),
			Streaming: true,
		})
	}

	// Batched mode: only send if interval has elapsed.
	now := time.Now()
	if now.Sub(d.lastSend) < d.interval {
		return nil
	}

	return d.flush(ctx)
}

// Flush sends any remaining buffered content.
func (d *StreamDelivery) Flush(ctx context.Context) error {
	if d.buffer.Len() == 0 {
		return nil
	}
	return d.flush(ctx)
}

// flush sends the current buffer contents.
func (d *StreamDelivery) flush(ctx context.Context) error {
	text := d.buffer.String()
	if text == "" {
		return nil
	}

	msg := OutgoingMessage{
		Text:      text,
		Streaming: true,
	}

	// If we've already sent a message, mark this as an edit.
	if d.sent && d.editID != "" {
		msg.EditID = d.editID
	}

	err := d.channel.Send(ctx, d.accountID, msg)
	if err != nil {
		return err
	}

	d.sent = true
	d.lastSend = time.Now()
	return nil
}

// Accumulated returns the full accumulated text so far.
func (d *StreamDelivery) Accumulated() string {
	return d.buffer.String()
}

// SetEditID sets the message ID to use for subsequent edit-based sends.
// This should be called after the first Send returns a message ID.
func (d *StreamDelivery) SetEditID(id string) {
	d.editID = id
}
