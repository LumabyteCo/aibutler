package migration

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// ClaudeImporter imports from Claude's exported JSON format.
type ClaudeImporter struct{}

func (c *ClaudeImporter) Name() string { return "claude" }

type claudeExport []claudeConversation

type claudeConversation struct {
	UUID         string              `json:"uuid"`
	Name         string              `json:"name"`
	CreatedAt    string              `json:"created_at"`
	ChatMessages []claudeChatMessage `json:"chat_messages"`
}

type claudeChatMessage struct {
	Sender string `json:"sender"` // "human" or "assistant"
	Text   string `json:"text"`
}

func (c *ClaudeImporter) Parse(ctx context.Context, r io.Reader, save SaveFunc) error {
	data, err := io.ReadAll(LimitedReader(r))
	if err != nil {
		return fmt.Errorf("claude: read: %w", err)
	}

	var conversations claudeExport
	if err := json.Unmarshal(data, &conversations); err != nil {
		return fmt.Errorf("claude: parse JSON: %w", err)
	}

	for _, conv := range conversations {
		if err := ctx.Err(); err != nil {
			return err
		}
		tags := []string{"import", "claude"}
		if conv.Name != "" {
			tags = append(tags, conv.Name)
		}

		// Combine user+assistant pairs into thoughts.
		var parts []string
		for _, msg := range conv.ChatMessages {
			if strings.TrimSpace(msg.Text) == "" {
				continue
			}
			role := "User"
			if msg.Sender == "assistant" {
				role = "Assistant"
			}
			parts = append(parts, fmt.Sprintf("%s: %s", role, msg.Text))
		}

		if len(parts) == 0 {
			continue
		}

		content := strings.Join(parts, "\n\n")
		if err := save(ctx, content, "claude", tags); err != nil {
			return err
		}
	}

	return nil
}
