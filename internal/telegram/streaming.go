package telegram

import (
	"context"
	"strconv"
)

// StreamWriter sends progressive updates by editing a message.
type StreamWriter struct {
	api    APIClient
	chatID int64
	msgID  int
}

// NewStreamWriter creates a stream writer for progressive message delivery.
// It first sends an initial message, then edits it as content arrives.
func NewStreamWriter(ctx context.Context, api APIClient, chatID int64, initialText string) (*StreamWriter, error) {
	msg, err := api.SendMessage(ctx, chatID, initialText, 0)
	if err != nil {
		return nil, err
	}
	return &StreamWriter{
		api:    api,
		chatID: chatID,
		msgID:  msg.MessageID,
	}, nil
}

// Update replaces the message text with new content.
func (sw *StreamWriter) Update(ctx context.Context, text string) error {
	return sw.api.EditMessage(ctx, sw.chatID, sw.msgID, text)
}

// MessageID returns the ID of the message being edited.
func (sw *StreamWriter) MessageID() string {
	return strconv.Itoa(sw.msgID)
}
