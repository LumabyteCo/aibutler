// Package irc provides a lightweight IRC channel adapter.
package irc

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// toolRegistry is the interface for registering tools. Using a local narrow interface
// avoids import cycles with the tool package.
type toolRegistry interface {
	Register(name, description, schema, capability string, exec func(ctx context.Context, input string) (string, error))
}

// Client is a lightweight IRC client for sending and receiving text messages.
type Client struct {
	server   string
	nick     string
	channels []string
	sendFn   func(ctx context.Context, target, message string) error
}

// NewClient creates an IRC client configured for the given server and nick.
func NewClient(server, nick string) *Client {
	return &Client{
		server: server,
		nick:   nick,
	}
}

// SetSendFunc sets the underlying send function (for testing or custom transport).
func (c *Client) SetSendFunc(fn func(ctx context.Context, target, message string) error) {
	c.sendFn = fn
}

// Connect attempts to connect to the IRC server.
// Returns an error because no real IRC library is available (no CGO/deps).
func (c *Client) Connect(_ context.Context) error {
	if c.server == "" {
		return fmt.Errorf("irc: server address is required")
	}
	if c.nick == "" {
		return fmt.Errorf("irc: nick is required")
	}
	// No real IRC library — this is a capability framework.
	return fmt.Errorf("irc: real IRC connection not available (no external dependencies); use SetSendFunc for testing")
}

// JoinChannel adds a channel to the client's channel list.
func (c *Client) JoinChannel(channel string) error {
	if channel == "" {
		return fmt.Errorf("irc: channel name is required")
	}
	if !strings.HasPrefix(channel, "#") {
		channel = "#" + channel
	}
	c.channels = append(c.channels, channel)
	return nil
}

// Channels returns the list of joined channels.
func (c *Client) Channels() []string {
	return c.channels
}

// Send sends a text message to a target (channel or user).
func (c *Client) Send(ctx context.Context, target, message string) error {
	if target == "" {
		return fmt.Errorf("irc: target is required")
	}
	if message == "" {
		return fmt.Errorf("irc: message is required")
	}
	if c.sendFn != nil {
		return c.sendFn(ctx, target, message)
	}
	return fmt.Errorf("irc: not connected (use Connect or SetSendFunc)")
}

// InboundMessage represents a parsed incoming IRC message.
type InboundMessage struct {
	From    string `json:"from"`
	Target  string `json:"target"`
	Text    string `json:"text"`
	Command string `json:"command"`
}

// ParseMessage parses a raw IRC protocol line into an InboundMessage.
// Supports PRIVMSG format: :nick!user@host PRIVMSG #channel :message text
func ParseMessage(raw string) (*InboundMessage, error) {
	if raw == "" {
		return nil, fmt.Errorf("irc: empty message")
	}
	raw = strings.TrimSpace(raw)

	// Parse prefix (source).
	var prefix string
	rest := raw
	if strings.HasPrefix(raw, ":") {
		idx := strings.Index(raw, " ")
		if idx < 0 {
			return nil, fmt.Errorf("irc: malformed message")
		}
		prefix = raw[1:idx]
		rest = raw[idx+1:]
	}

	parts := strings.SplitN(rest, " ", 3)
	if len(parts) < 2 {
		return nil, fmt.Errorf("irc: malformed message: too few parts")
	}

	command := parts[0]
	target := parts[1]
	var text string
	if len(parts) >= 3 {
		text = parts[2]
		if strings.HasPrefix(text, ":") {
			text = text[1:]
		}
	}

	// Extract nick from prefix (nick!user@host).
	from := prefix
	if idx := strings.Index(prefix, "!"); idx > 0 {
		from = prefix[:idx]
	}

	return &InboundMessage{
		From:    from,
		Target:  target,
		Text:    text,
		Command: command,
	}, nil
}

// RegisterIRCTools registers the irc.send tool.
func RegisterIRCTools(registry toolRegistry, client *Client) {
	registry.Register(
		"irc.send",
		"Send a text message to an IRC channel or user.",
		`{"type":"object","properties":{"target":{"type":"string","description":"IRC channel (e.g. #general) or nick to message"},"message":{"type":"string","description":"Message text"}},"required":["target","message"]}`,
		"tool.channel.send",
		func(ctx context.Context, input string) (string, error) {
			var args struct {
				Target  string `json:"target"`
				Message string `json:"message"`
			}
			if err := json.Unmarshal([]byte(input), &args); err != nil {
				return "", fmt.Errorf("irc.send: invalid input: %w", err)
			}
			if args.Target == "" || args.Message == "" {
				return "", fmt.Errorf("irc.send: target and message are required")
			}
			if err := client.Send(ctx, args.Target, args.Message); err != nil {
				return "", err
			}
			return `{"status":"sent"}`, nil
		},
	)
}
