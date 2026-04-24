package model

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/LumabyteCo/aibutler/internal/agent"
)

// CompleteStream sends a streaming request to the Anthropic Messages API.
// It returns a channel that receives StreamEvent values as they arrive.
func (c *ClaudeAdapter) CompleteStream(ctx context.Context, messages []agent.Message) (<-chan agent.StreamEvent, error) {
	reqBody, err := c.buildStreamRequest(messages)
	if err != nil {
		return nil, fmt.Errorf("claude: build stream request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", c.baseURL, bytes.NewReader(reqBody))
	if err != nil {
		return nil, fmt.Errorf("claude: create stream request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", c.apiKey)
	req.Header.Set("anthropic-version", claudeAPIVersion)

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("claude: stream connect: %w", err)
	}

	if resp.StatusCode != 200 {
		resp.Body.Close()
		return nil, fmt.Errorf("claude: stream status %d", resp.StatusCode)
	}

	ch := make(chan agent.StreamEvent, 64)
	go c.parseSSEStream(ctx, resp, ch)
	return ch, nil
}

func (c *ClaudeAdapter) buildStreamRequest(messages []agent.Message) ([]byte, error) {
	req := claudeStreamRequest{
		Model:     c.model,
		MaxTokens: c.maxTokens,
		Stream:    true,
	}

	for _, m := range messages {
		if m.Role == "system" {
			req.System = m.Content
			continue
		}

		msg := claudeMessage{Role: m.Role}
		if m.Role == "tool" {
			msg.Role = "user"
			msg.Content = []claudeContentBlock{{
				Type:      "tool_result",
				ToolUseID: m.ToolID,
				Content:   m.Content,
			}}
		} else if m.Role == "assistant" && len(m.ToolCalls) > 0 {
			if m.Content != "" {
				msg.Content = append(msg.Content, claudeContentBlock{
					Type: "text",
					Text: m.Content,
				})
			}
			for _, tc := range m.ToolCalls {
				msg.Content = append(msg.Content, claudeContentBlock{
					Type:  "tool_use",
					ID:    tc.ID,
					Name:  sanitizeToolName(tc.Name),
					Input: json.RawMessage(tc.Input),
				})
			}
		} else {
			msg.Content = []claudeContentBlock{{
				Type: "text",
				Text: m.Content,
			}}
		}
		req.Messages = append(req.Messages, msg)
	}

	for _, t := range c.tools {
		var schema json.RawMessage
		if t.Schema != "" {
			schema = json.RawMessage(t.Schema)
		} else {
			schema = json.RawMessage(`{"type":"object","properties":{}}`)
		}
		req.Tools = append(req.Tools, claudeTool{
			Name:        sanitizeToolName(t.Name),
			Description: t.Description,
			InputSchema: schema,
		})
	}

	return json.Marshal(req)
}

func (c *ClaudeAdapter) parseSSEStream(ctx context.Context, resp *http.Response, ch chan<- agent.StreamEvent) {
	defer resp.Body.Close()
	defer close(ch)

	scanner := bufio.NewScanner(resp.Body)

	// Track current content block for tool use.
	var currentBlockType string
	var currentToolID string
	var currentToolName string

	for scanner.Scan() {
		// Check context cancellation.
		select {
		case <-ctx.Done():
			ch <- agent.StreamEvent{Type: "error", Error: ctx.Err()}
			return
		default:
		}

		line := scanner.Text()

		if !strings.HasPrefix(line, "event: ") {
			continue
		}

		eventType := strings.TrimPrefix(line, "event: ")

		// Read the data line.
		if !scanner.Scan() {
			break
		}
		dataLine := scanner.Text()
		if !strings.HasPrefix(dataLine, "data: ") {
			continue
		}
		data := strings.TrimPrefix(dataLine, "data: ")

		switch eventType {
		case "message_start":
			var msg struct {
				Message struct {
					Usage claudeUsage `json:"usage"`
				} `json:"message"`
			}
			if err := json.Unmarshal([]byte(data), &msg); err == nil && msg.Message.Usage.InputTokens > 0 {
				ch <- agent.StreamEvent{
					Type:     "usage",
					TokensIn: msg.Message.Usage.InputTokens,
				}
			}

		case "content_block_start":
			var block struct {
				ContentBlock struct {
					Type string `json:"type"`
					ID   string `json:"id"`
					Name string `json:"name"`
				} `json:"content_block"`
			}
			if err := json.Unmarshal([]byte(data), &block); err == nil {
				currentBlockType = block.ContentBlock.Type
				if block.ContentBlock.Type == "tool_use" {
					currentToolID = block.ContentBlock.ID
					currentToolName = unsanitizeToolName(block.ContentBlock.Name)
					ch <- agent.StreamEvent{
						Type:       "tool_use_start",
						ToolCallID: currentToolID,
						ToolName:   currentToolName,
					}
				}
			}

		case "content_block_delta":
			var delta struct {
				Delta struct {
					Type        string `json:"type"`
					Text        string `json:"text"`
					PartialJSON string `json:"partial_json"`
				} `json:"delta"`
			}
			if err := json.Unmarshal([]byte(data), &delta); err == nil {
				switch delta.Delta.Type {
				case "text_delta":
					ch <- agent.StreamEvent{
						Type: "text_delta",
						Text: delta.Delta.Text,
					}
				case "input_json_delta":
					ch <- agent.StreamEvent{
						Type:        "input_json_delta",
						ToolCallID:  currentToolID,
						ToolName:    currentToolName,
						PartialJSON: delta.Delta.PartialJSON,
					}
				case "thinking_delta":
					ch <- agent.StreamEvent{
						Type: "thinking_delta",
						Text: delta.Delta.Text,
					}
				}
			}

		case "content_block_stop":
			currentBlockType = ""
			_ = currentBlockType // Reset state.

		case "message_delta":
			var delta struct {
				Usage struct {
					OutputTokens int `json:"output_tokens"`
				} `json:"usage"`
			}
			if err := json.Unmarshal([]byte(data), &delta); err == nil && delta.Usage.OutputTokens > 0 {
				ch <- agent.StreamEvent{
					Type:      "usage",
					TokensOut: delta.Usage.OutputTokens,
				}
			}

		case "message_stop":
			ch <- agent.StreamEvent{Type: "message_stop"}
			return

		case "ping":
			// Ignore pings.

		case "error":
			var errData struct {
				Error struct {
					Message string `json:"message"`
				} `json:"error"`
			}
			if err := json.Unmarshal([]byte(data), &errData); err == nil {
				ch <- agent.StreamEvent{
					Type:  "error",
					Error: fmt.Errorf("claude stream: %s", errData.Error.Message),
				}
			}
		}
	}

	if err := scanner.Err(); err != nil {
		ch <- agent.StreamEvent{Type: "error", Error: fmt.Errorf("claude stream scan: %w", err)}
	}
}

// claudeStreamRequest extends claudeRequest with the stream field.
type claudeStreamRequest struct {
	Model     string          `json:"model"`
	MaxTokens int             `json:"max_tokens"`
	Stream    bool            `json:"stream"`
	System    string          `json:"system,omitempty"`
	Messages  []claudeMessage `json:"messages"`
	Tools     []claudeTool    `json:"tools,omitempty"`
}
