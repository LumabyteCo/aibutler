package channel

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/LumabyteCo/aibutler/internal/contact"
	"github.com/LumabyteCo/aibutler/internal/tool"
)

// RegisterChannelTools registers channel-specific tools with the tool registry.
func RegisterChannelTools(registry *tool.Registry, channels *Registry) {
	registry.Register(&sendTool{channels: channels})
	registry.Register(&readTool{})
	registry.Register(&relayTool{channels: channels})
}

// RegisterChannelToolsWithDeps registers channel tools with full dependencies.
func RegisterChannelToolsWithDeps(registry *tool.Registry, channels *Registry, db *sql.DB, resolver *contact.Resolver) {
	registry.Register(&sendTool{channels: channels})
	registry.Register(&readTool{db: db})
	registry.Register(&relayTool{channels: channels, resolver: resolver})
}

// sendTool sends a message via a named channel.
type sendTool struct {
	channels *Registry
}

type sendInput struct {
	Channel   string `json:"channel"`
	AccountID string `json:"account_id"`
	Text      string `json:"text"`
}

func (t *sendTool) Name() string        { return "channel.send" }
func (t *sendTool) Description() string { return "Send a message to a user via a messaging channel." }
func (t *sendTool) Capability() string  { return "channel.send" }

func (t *sendTool) Schema() string {
	return `{
		"type": "object",
		"properties": {
			"channel":    {"type": "string", "description": "Channel name (e.g. webchat, telegram)"},
			"account_id": {"type": "string", "description": "Target user/account identifier"},
			"text":       {"type": "string", "description": "Message text to send"}
		},
		"required": ["channel", "account_id", "text"]
	}`
}

func (t *sendTool) Execute(ctx context.Context, input string) (string, error) {
	var in sendInput
	if err := json.Unmarshal([]byte(input), &in); err != nil {
		return "", fmt.Errorf("channel.send: %w", err)
	}

	ch, ok := t.channels.Get(in.Channel)
	if !ok {
		return "", fmt.Errorf("channel.send: unknown channel %q", in.Channel)
	}

	err := ch.Send(ctx, in.AccountID, OutgoingMessage{Text: in.Text})
	if err != nil {
		return "", fmt.Errorf("channel.send: %w", err)
	}
	return "Message sent.", nil
}

// readTool reads recent messages from a session.
type readTool struct {
	db *sql.DB
}

func (t *readTool) Name() string        { return "channel.read" }
func (t *readTool) Description() string { return "Read recent messages from the current session." }
func (t *readTool) Capability() string  { return "channel.read" }

func (t *readTool) Schema() string {
	return `{
		"type": "object",
		"properties": {
			"session_id": {"type": "string", "description": "Session ID to read messages from"},
			"limit":      {"type": "integer", "description": "Maximum messages to return (default 20)"}
		},
		"required": ["session_id"]
	}`
}

func (t *readTool) Execute(ctx context.Context, input string) (string, error) {
	if t.db == nil {
		return "channel.read: database not configured", nil
	}

	var args struct {
		SessionID string `json:"session_id"`
		Limit     int    `json:"limit"`
	}
	if err := json.Unmarshal([]byte(input), &args); err != nil {
		return "", fmt.Errorf("channel.read: %w", err)
	}
	if args.Limit <= 0 || args.Limit > 100 {
		args.Limit = 20
	}

	rows, err := t.db.QueryContext(ctx,
		`SELECT role, content FROM (
			SELECT id, role, content FROM messages
			WHERE session_id = ? ORDER BY id DESC LIMIT ?
		) sub ORDER BY id ASC`,
		args.SessionID, args.Limit)
	if err != nil {
		return "", fmt.Errorf("channel.read: %w", err)
	}
	defer rows.Close()

	type msg struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}

	var msgs []msg
	for rows.Next() {
		var m msg
		if err := rows.Scan(&m.Role, &m.Content); err != nil {
			return "", fmt.Errorf("channel.read: scan: %w", err)
		}
		msgs = append(msgs, m)
	}

	out, _ := json.Marshal(msgs)
	return string(out), nil
}

// relayTool sends a message to a contact via their preferred channel.
type relayTool struct {
	channels *Registry
	resolver *contact.Resolver
}

type relayInput struct {
	Contact string `json:"contact"`
	Text    string `json:"text"`
	Channel string `json:"channel"` // Optional explicit channel override.
}

func (t *relayTool) Name() string        { return "channel.relay" }
func (t *relayTool) Description() string { return "Send a message to a contact via their preferred channel." }
func (t *relayTool) Capability() string  { return "channel.relay" }

func (t *relayTool) Schema() string {
	return `{
		"type": "object",
		"properties": {
			"contact": {"type": "string", "description": "Contact name or ID to relay message to"},
			"text":    {"type": "string", "description": "Message text to relay"},
			"channel": {"type": "string", "description": "Optional: override the contact's preferred channel"}
		},
		"required": ["contact", "text"]
	}`
}

func (t *relayTool) Execute(ctx context.Context, input string) (string, error) {
	if t.resolver == nil {
		return "channel.relay: contact resolver not configured", nil
	}

	var in relayInput
	if err := json.Unmarshal([]byte(input), &in); err != nil {
		return "", fmt.Errorf("channel.relay: %w", err)
	}

	// Resolve contact.
	resolved, err := t.resolver.ResolveOne(ctx, in.Contact)
	if err != nil {
		return "", fmt.Errorf("channel.relay: %w", err)
	}

	// Determine channel.
	chName := in.Channel
	if chName == "" {
		chName = resolved.PreferredChannel
	}
	if chName == "" {
		// Fall back to first available channel.
		channels := t.channels.All()
		for name := range channels {
			chName = name
			break
		}
	}
	if chName == "" {
		return "", fmt.Errorf("channel.relay: no channel available for contact %q", resolved.Name)
	}

	ch, ok := t.channels.Get(chName)
	if !ok {
		// Suggest available channels.
		available := t.channels.All()
		var names []string
		for n := range available {
			names = append(names, n)
		}
		return "", fmt.Errorf("channel.relay: channel %q not available (available: %v)", chName, names)
	}

	// Determine account ID from channel_ids or fallback.
	accountID := resolveAccountID(resolved, chName)
	if accountID == "" {
		return "", fmt.Errorf("channel.relay: no account ID for %q on channel %q", resolved.Name, chName)
	}

	err = ch.Send(ctx, accountID, OutgoingMessage{Text: in.Text})
	if err != nil {
		return "", fmt.Errorf("channel.relay: send to %s via %s: %w", resolved.Name, chName, err)
	}

	return fmt.Sprintf("Message relayed to %s via %s.", resolved.Name, chName), nil
}

// resolveAccountID extracts the account ID for a channel from the contact's channel_ids.
func resolveAccountID(c *contact.Contact, chName string) string {
	if c.ChannelIDs != "" {
		var ids map[string]string
		if err := json.Unmarshal([]byte(c.ChannelIDs), &ids); err == nil {
			if id, ok := ids[chName]; ok {
				return id
			}
		}
	}
	// Fallback: use phone for telegram/whatsapp, email for email.
	switch chName {
	case "telegram", "whatsapp":
		return c.Phone
	case "email":
		return c.Email
	default:
		return c.Email
	}
}
