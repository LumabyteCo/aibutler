package channel

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/LumabyteCo/aibutler/internal/agent"
	"github.com/LumabyteCo/aibutler/internal/config"
	"github.com/LumabyteCo/aibutler/internal/i18n"
	"github.com/LumabyteCo/aibutler/internal/media"
	"github.com/LumabyteCo/aibutler/internal/session"
	"github.com/LumabyteCo/aibutler/internal/stopphrase"
	"github.com/LumabyteCo/aibutler/internal/voice"
)

// AgentFactory creates and runs an agent for a given session and task.
type AgentFactory interface {
	Run(ctx context.Context, sessionID, task, channel string) (*agent.Result, error)
}

// BudgetTracker checks spending against budget thresholds.
type BudgetTracker interface {
	// BudgetStatus returns an alert message and action ("info", "warn", "pause")
	// if a threshold is crossed. Returns empty strings if under budget.
	BudgetStatus(ctx context.Context) (message, action string)
}

// RouterConfig holds the dependencies for the message router.
// StreamDeliverFunc delivers streamed tokens to a channel, returning the aggregated Response.
type StreamDeliverFunc func(ctx context.Context, ch Channel, accountID string, events <-chan agent.StreamEvent) agent.Response

type RouterConfig struct {
	Sessions      *session.Manager
	Stop          *stopphrase.Matcher
	Typing        *TypingManager
	Channels      *Registry
	Config        *config.Config
	I18n          *i18n.Bundle
	DB            *sql.DB
	Agent         AgentFactory
	Voice         *voice.Pipeline      // Optional: voice STT/TTS processing
	Media         *media.Pipeline      // Optional: file/image/PDF processing
	Tracker       BudgetTracker        // Optional: budget alert checking
	StreamDeliver StreamDeliverFunc    // Optional: streaming token delivery to channels
}

// Router dispatches incoming messages to the agent and sends responses.
type Router struct {
	sessions   *session.Manager
	stop       *stopphrase.Matcher
	typing     *TypingManager
	channels   *Registry
	cfg        *config.Config
	i18n       *i18n.Bundle
	db         *sql.DB
	agent      AgentFactory
	voice      *voice.Pipeline
	media      *media.Pipeline
	tracker    BudgetTracker
	sessionMap sync.Map // composite key -> session ID
}

// NewRouter creates a message router with the given configuration.
func NewRouter(cfg RouterConfig) *Router {
	return &Router{
		sessions: cfg.Sessions,
		stop:     cfg.Stop,
		typing:   cfg.Typing,
		channels: cfg.Channels,
		cfg:      cfg.Config,
		i18n:     cfg.I18n,
		db:       cfg.DB,
		agent:    cfg.Agent,
		voice:    cfg.Voice,
		media:    cfg.Media,
		tracker:  cfg.Tracker,
	}
}

// HandleMessage processes an incoming message envelope.
func (r *Router) HandleMessage(ctx context.Context, env Envelope) error {
	// 1. Check stop phrase.
	if env.Type == TypeText && env.Text != "" {
		action, lang := r.stop.Check(env.Text)
		if action != stopphrase.ActionNone {
			return r.handleStopPhrase(ctx, env, action, lang)
		}
	}

	// 2. Find or create session.
	key := sessionKey(env.Channel, env.AccountID, env.ThreadID)
	sessID, err := r.resolveSession(ctx, key, env)
	if err != nil {
		return fmt.Errorf("channel: resolve session: %w", err)
	}

	// 3. Start typing indicator.
	ch, ok := r.channels.Get(env.Channel)
	if !ok {
		return fmt.Errorf("channel: unknown channel %q", env.Channel)
	}
	if r.typing != nil {
		r.typing.Start(ctx, ch, env.AccountID)
		defer r.typing.Stop(env.AccountID)
	}

	// 3a. Pre-process voice/audio attachments → transcribe to text.
	if (env.Type == TypeVoice || env.Type == TypeAudio) && r.voice != nil {
		env = r.processVoiceInput(ctx, env)
		if env.Text == "" {
			return r.sendError(ctx, ch, env)
		}
	}

	// 3b. Pre-process non-voice attachments → extract text content.
	if len(env.Attachments) > 0 && r.media != nil {
		env = r.processAttachments(ctx, env)
	}

	// 4. Store inbound message.
	if err := r.sessions.AddMessage(ctx, sessID, agent.Message{
		Role:    "user",
		Content: env.Text,
	}); err != nil {
		log.Printf("channel: store inbound message: %v", err)
	}

	// 5. Run agent.
	result, err := r.agent.Run(ctx, sessID, env.Text, env.Channel)
	if err != nil {
		log.Printf("channel: agent error: %v", err)
		return r.sendError(ctx, ch, env)
	}

	// 5a. Handle budget pause — persist the pause event to messages.
	if result.Error == "global_budget_paused" {
		if err := r.sessions.AddMessage(ctx, sessID, agent.Message{
			Role:    "assistant",
			Content: "[Budget paused]",
		}); err != nil {
			log.Printf("channel: store budget pause: %v", err)
		}
		return r.sendBudgetPaused(ctx, ch, env)
	}

	// 5b. Handle empty response — don't send blank messages.
	if result.Output == "" && result.Error == "" {
		result.Output = "I processed your request but have no response to share."
	}

	// 6. Store outbound message.
	if err := r.sessions.AddMessage(ctx, sessID, agent.Message{
		Role:    "assistant",
		Content: result.Output,
	}); err != nil {
		log.Printf("channel: store outbound message: %v", err)
	}

	// 7. Build and send response.
	responseText := result.Output

	// Append budget alert if threshold crossed.
	if r.tracker != nil {
		if msg, _ := r.tracker.BudgetStatus(ctx); msg != "" {
			responseText += "\n\n---\n" + msg
		}
	}

	outMsg := OutgoingMessage{
		Text:    responseText,
		ReplyTo: env.ID,
	}

	// If the original message was voice, optionally generate audio response.
	if (env.Type == TypeVoice || env.Type == TypeAudio) && r.voice != nil {
		lang := r.cfg.Settings.Language
		audioData, format, err := r.voice.GenerateVoiceResponse(ctx, result.Output, lang)
		if err == nil && audioData != nil {
			outMsg.Attachments = append(outMsg.Attachments, Attachment{
				Type:     TypeVoice,
				MimeType: string(format),
				Data:     audioData,
			})
		}
	}

	return r.sendWithRetry(ctx, ch, env.AccountID, outMsg)
}

// sendWithRetry attempts to send a message with exponential backoff (max 3 attempts).
func (r *Router) sendWithRetry(ctx context.Context, ch Channel, accountID string, msg OutgoingMessage) error {
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(time.Duration(attempt) * 500 * time.Millisecond):
			}
		}
		lastErr = ch.Send(ctx, accountID, msg)
		if lastErr == nil {
			return nil
		}
		log.Printf("channel: send attempt %d failed: %v", attempt+1, lastErr)
	}
	return fmt.Errorf("channel: send failed after 3 attempts: %w", lastErr)
}

// processVoiceInput transcribes voice/audio attachments into text.
func (r *Router) processVoiceInput(ctx context.Context, env Envelope) Envelope {
	var failures int
	for _, att := range env.Attachments {
		if att.Type != TypeVoice && att.Type != TypeAudio {
			continue
		}
		result, err := r.voice.ProcessVoiceInput(ctx, att.Data, voice.AudioFormat(att.MimeType))
		if err != nil {
			log.Printf("channel: voice STT failed: %v", err)
			failures++
			continue
		}
		if env.Text != "" {
			env.Text += "\n"
		}
		env.Text += result.Text
	}
	if failures > 0 && env.Text != "" {
		env.Text += fmt.Sprintf("\n[Note: %d audio attachment(s) could not be transcribed]", failures)
	}
	return env
}

// processAttachments runs non-voice attachments through the media pipeline
// and appends extracted content to the envelope's text.
func (r *Router) processAttachments(ctx context.Context, env Envelope) Envelope {
	var parts []string
	var failures []string
	for _, att := range env.Attachments {
		if att.Type == TypeVoice || att.Type == TypeAudio {
			continue // Already handled by voice pipeline.
		}
		result, err := r.media.Process(ctx, att.Data, att.Filename)
		if err != nil {
			log.Printf("channel: media process %q: %v", att.Filename, err)
			failures = append(failures, att.Filename)
			continue
		}
		header := fmt.Sprintf("[Attachment: %s (%s)]", att.Filename, result.Type)
		if result.Content != "" {
			parts = append(parts, header+"\n"+result.Content)
		}
	}
	if len(parts) > 0 {
		if env.Text != "" {
			env.Text += "\n\n"
		}
		env.Text += strings.Join(parts, "\n\n")
	}
	if len(failures) > 0 {
		if env.Text != "" {
			env.Text += "\n\n"
		}
		env.Text += fmt.Sprintf("[Note: failed to process %d attachment(s): %s]", len(failures), strings.Join(failures, ", "))
	}
	return env
}

// sendBudgetPaused sends a budget-paused message to the user.
func (r *Router) sendBudgetPaused(ctx context.Context, ch Channel, env Envelope) error {
	msg := "Service paused: monthly budget reached."
	if r.tracker != nil {
		if alertMsg, _ := r.tracker.BudgetStatus(ctx); alertMsg != "" {
			msg = alertMsg
		}
	}
	return ch.Send(ctx, env.AccountID, OutgoingMessage{Text: msg, ReplyTo: env.ID})
}

// resolveSession finds an existing session or creates a new one for the given key.
// Uses LoadOrStore to prevent race conditions when concurrent messages arrive.
func (r *Router) resolveSession(ctx context.Context, key string, env Envelope) (string, error) {
	if id, ok := r.sessionMap.Load(key); ok {
		return id.(string), nil
	}

	// Create new session — use LoadOrStore to handle concurrent creation atomically.
	id, err := r.sessions.Create(ctx, env.Channel, env.AccountID, "default")
	if err != nil {
		return "", err
	}
	actual, loaded := r.sessionMap.LoadOrStore(key, id)
	if loaded {
		// Another goroutine won the race — use their session ID.
		return actual.(string), nil
	}
	return id, nil
}

func (r *Router) handleStopPhrase(ctx context.Context, env Envelope, action stopphrase.Action, lang string) error {
	ch, ok := r.channels.Get(env.Channel)
	if !ok {
		return fmt.Errorf("channel: unknown channel %q", env.Channel)
	}

	if lang == "" {
		lang = r.cfg.Settings.Language
	}

	var key string
	switch action {
	case stopphrase.ActionCancel:
		key = "stop.cancelled"
	default:
		key = "stop.confirmed"
	}

	msg := r.i18n.T(lang, key)
	return ch.Send(ctx, env.AccountID, OutgoingMessage{Text: msg})
}

func (r *Router) sendError(ctx context.Context, ch Channel, env Envelope) error {
	lang := r.cfg.Settings.Language
	msg := r.i18n.T(lang, "error.internal")
	return ch.Send(ctx, env.AccountID, OutgoingMessage{
		Text:    msg,
		ReplyTo: env.ID,
	})
}

// sessionKey generates a unique key for session lookup.
func sessionKey(channel, accountID, threadID string) string {
	if threadID != "" {
		return fmt.Sprintf("%s:%s:%s", channel, accountID, threadID)
	}
	return fmt.Sprintf("%s:%s", channel, accountID)
}
