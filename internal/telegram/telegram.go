package telegram

import (
	"context"
	"crypto/subtle"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/LumabyteCo/aibutler/internal/channel"
)

// Adapter implements channel.Channel for Telegram.
type Adapter struct {
	api     APIClient
	handler channel.MessageHandler
	offset  int
	cancel  context.CancelFunc
	wg      sync.WaitGroup
}

// New creates a Telegram adapter with the given API client.
func New(api APIClient) *Adapter {
	return &Adapter{api: api}
}

func (a *Adapter) Name() string { return "telegram" }

// Start begins long polling for updates.
func (a *Adapter) Start(ctx context.Context, handler channel.MessageHandler) error {
	a.handler = handler

	ctx, a.cancel = context.WithCancel(ctx)
	a.wg.Add(1)
	go a.pollLoop(ctx)
	return nil
}

// Stop cancels the polling loop and waits for it to exit.
func (a *Adapter) Stop(_ context.Context) error {
	if a.cancel != nil {
		a.cancel()
	}
	a.wg.Wait()
	return nil
}

// Send sends a text message to a Telegram chat.
func (a *Adapter) Send(ctx context.Context, accountID string, msg channel.OutgoingMessage) error {
	chatID, err := strconv.ParseInt(accountID, 10, 64)
	if err != nil {
		return fmt.Errorf("telegram: invalid chat ID %q: %w", accountID, err)
	}

	replyTo := 0
	if msg.ReplyTo != "" {
		replyTo, _ = strconv.Atoi(msg.ReplyTo)
	}

	if msg.EditID != "" {
		msgID, _ := strconv.Atoi(msg.EditID)
		return a.api.EditMessage(ctx, chatID, msgID, msg.Text)
	}

	_, err = a.api.SendMessage(ctx, chatID, msg.Text, replyTo)
	return err
}

// SendTyping sends a "typing" chat action.
func (a *Adapter) SendTyping(ctx context.Context, accountID string) error {
	chatID, err := strconv.ParseInt(accountID, 10, 64)
	if err != nil {
		return fmt.Errorf("telegram: invalid chat ID %q: %w", accountID, err)
	}
	return a.api.SendChatAction(ctx, chatID, "typing")
}

func (a *Adapter) pollLoop(ctx context.Context) {
	defer a.wg.Done()
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		updates, err := a.api.GetUpdates(ctx, a.offset, 30)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			log.Printf("telegram: poll error: %v", err)
			time.Sleep(2 * time.Second)
			continue
		}

		for _, u := range updates {
			a.offset = u.UpdateID + 1
			if u.Message != nil {
				env := normalizeUpdate(u)
				if a.handler != nil {
					if err := a.handler(ctx, env); err != nil {
						log.Printf("telegram: handler error: %v", err)
					}
				}
			}
		}
	}
}

// normalizeUpdate converts a Telegram update to a channel.Envelope.
func normalizeUpdate(u Update) channel.Envelope {
	m := u.Message
	env := channel.Envelope{
		ID:        strconv.Itoa(m.MessageID),
		Channel:   "telegram",
		AccountID: strconv.FormatInt(m.Chat.ID, 10),
		Timestamp: time.Now(),
	}

	switch {
	case len(m.Photo) > 0:
		// Use the largest photo.
		largest := m.Photo[len(m.Photo)-1]
		env.Type = channel.TypeImage
		env.Text = m.Text
		env.Attachments = []channel.Attachment{{
			Type:     channel.TypeImage,
			MimeType: "image/jpeg", // Telegram photos are always JPEG.
			URL:      largest.FileID,
			Size:     int64(largest.FileSize),
		}}
	case m.Voice != nil:
		env.Type = channel.TypeVoice
		env.Attachments = []channel.Attachment{{
			Type:     channel.TypeVoice,
			MimeType: m.Voice.MimeType,
			URL:      m.Voice.FileID,
			Size:     int64(m.Voice.FileSize),
		}}
	case m.Document != nil:
		env.Type = channel.TypeFile
		env.Attachments = []channel.Attachment{{
			Type:     channel.TypeFile,
			MimeType: m.Document.MimeType,
			URL:      m.Document.FileID,
			Filename: m.Document.FileName,
			Size:     int64(m.Document.FileSize),
		}}
	default:
		env.Type = channel.TypeText
		env.Text = m.Text
	}

	return env
}

// VerifyWebhookSecret checks the X-Telegram-Bot-Api-Secret-Token header.
// If secret is empty, verification is skipped (backward compatible).
func (a *Adapter) VerifyWebhookSecret(r *http.Request, secret string) bool {
	if secret == "" {
		return true // no verification configured
	}
	return subtle.ConstantTimeCompare(
		[]byte(r.Header.Get("X-Telegram-Bot-Api-Secret-Token")),
		[]byte(secret),
	) == 1
}

// Verify interface compliance.
var _ channel.Channel = (*Adapter)(nil)
