package migration

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// ChatGPTImporter imports from ChatGPT's exported JSON format.
type ChatGPTImporter struct{}

func (c *ChatGPTImporter) Name() string { return "chatgpt" }

type chatgptExport []chatgptConversation

type chatgptConversation struct {
	Title   string                       `json:"title"`
	Mapping map[string]chatgptMappingNode `json:"mapping"`
}

type chatgptMappingNode struct {
	Message  *chatgptMessage `json:"message"`
	Children []string        `json:"children"`
}

type chatgptMessage struct {
	Author  chatgptAuthor  `json:"author"`
	Content chatgptContent `json:"content"`
}

type chatgptAuthor struct {
	Role string `json:"role"` // "user", "assistant", "system"
}

type chatgptContent struct {
	Parts []interface{} `json:"parts"`
}

func (c *ChatGPTImporter) Parse(ctx context.Context, r io.Reader, save SaveFunc) error {
	data, err := io.ReadAll(LimitedReader(r))
	if err != nil {
		return fmt.Errorf("chatgpt: read: %w", err)
	}

	var conversations chatgptExport
	if err := json.Unmarshal(data, &conversations); err != nil {
		return fmt.Errorf("chatgpt: parse JSON: %w", err)
	}

	for _, conv := range conversations {
		if err := ctx.Err(); err != nil {
			return err
		}
		tags := []string{"import", "chatgpt"}
		if conv.Title != "" {
			tags = append(tags, conv.Title)
		}

		// Walk the mapping tree to extract messages in order.
		messages := extractChatGPTMessages(conv.Mapping)
		if len(messages) == 0 {
			continue
		}

		content := strings.Join(messages, "\n\n")
		if err := save(ctx, content, "chatgpt", tags); err != nil {
			return err
		}
	}

	return nil
}

// extractChatGPTMessages walks the mapping tree and returns formatted messages.
func extractChatGPTMessages(mapping map[string]chatgptMappingNode) []string {
	// Find root nodes (nodes not referenced as children).
	childSet := make(map[string]bool)
	for _, node := range mapping {
		for _, child := range node.Children {
			childSet[child] = true
		}
	}

	var roots []string
	for id := range mapping {
		if !childSet[id] {
			roots = append(roots, id)
		}
	}

	// BFS from roots.
	var messages []string
	visited := make(map[string]bool)
	queue := roots

	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		if visited[id] {
			continue
		}
		visited[id] = true

		node, ok := mapping[id]
		if !ok {
			continue
		}

		if node.Message != nil {
			role := node.Message.Author.Role
			if role == "user" || role == "assistant" {
				text := extractParts(node.Message.Content.Parts)
				if text != "" {
					label := "User"
					if role == "assistant" {
						label = "Assistant"
					}
					messages = append(messages, fmt.Sprintf("%s: %s", label, text))
				}
			}
		}

		queue = append(queue, node.Children...)
	}

	return messages
}

func extractParts(parts []interface{}) string {
	var texts []string
	for _, p := range parts {
		switch v := p.(type) {
		case string:
			if strings.TrimSpace(v) != "" {
				texts = append(texts, v)
			}
		}
	}
	return strings.Join(texts, "\n")
}
