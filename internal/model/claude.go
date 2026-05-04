package model

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/LumabyteCo/aibutler/internal/agent"
)

const claudeAPIURL = "https://api.anthropic.com/v1/messages"
const claudeAPIVersion = "2023-06-01"

// ClaudeAdapter implements agent.ModelAdapter for the Anthropic Messages API.
type ClaudeAdapter struct {
	apiKey      string
	model       string
	client      *http.Client
	retries     int
	baseURL     string // overridable for testing
	tools       []agent.ToolDef
	maxTokens   int
	temperature float64
}

// NewClaude creates a Claude model adapter.
func NewClaude(apiKey, model string, timeout time.Duration, retries int) *ClaudeAdapter {
	return &ClaudeAdapter{
		apiKey:      apiKey,
		model:       model,
		client:      &http.Client{Timeout: timeout},
		retries:     retries,
		baseURL:     claudeAPIURL,
		maxTokens:   8192,
		temperature: 0.7,
	}
}

// SetMaxTokens overrides the default max_tokens for API requests.
func (c *ClaudeAdapter) SetMaxTokens(n int) {
	if n > 0 {
		c.maxTokens = n
	}
}

// SetTemperature overrides the default temperature for API requests.
func (c *ClaudeAdapter) SetTemperature(t float64) {
	c.temperature = t
}

// SetHTTPClient replaces the adapter's HTTP client (e.g., with a pooled client).
func (c *ClaudeAdapter) SetHTTPClient(client *http.Client) {
	if client != nil {
		c.client = client
	}
}

// SetTools sets the tool definitions for subsequent Complete calls.
func (c *ClaudeAdapter) SetTools(tools []agent.ToolDef) {
	c.tools = tools
}

// Complete sends messages to the Anthropic Messages API and returns the response.
func (c *ClaudeAdapter) Complete(ctx context.Context, messages []agent.Message) (agent.Response, error) {
	reqBody, err := c.buildRequest(messages)
	if err != nil {
		return agent.Response{}, fmt.Errorf("claude: build request: %w", err)
	}

	var lastErr error
	for attempt := 0; attempt <= c.retries; attempt++ {
		if attempt > 0 {
			backoff := time.Duration(math.Pow(2, float64(attempt-1))) * time.Second
			select {
			case <-ctx.Done():
				return agent.Response{}, ctx.Err()
			case <-time.After(backoff):
			}
		}

		resp, err := c.doRequest(ctx, reqBody)
		if err != nil {
			lastErr = err
			continue
		}
		return resp, nil
	}
	return agent.Response{}, fmt.Errorf("claude: all %d attempts failed: %w", c.retries+1, lastErr)
}

func (c *ClaudeAdapter) buildRequest(messages []agent.Message) ([]byte, error) {
	req := claudeRequest{
		Model:     c.model,
		MaxTokens: c.maxTokens,
	}

	// Separate system message from conversation messages.
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
			// Assistant message with tool calls: include text + tool_use blocks.
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
		} else if len(m.Images) > 0 {
			// Multimodal: text block (if any) followed by one image block per Image.
			if m.Content != "" {
				msg.Content = append(msg.Content, claudeContentBlock{
					Type: "text",
					Text: m.Content,
				})
			}
			for _, img := range m.Images {
				if block, ok := claudeImageBlock(img); ok {
					msg.Content = append(msg.Content, block)
				}
			}
		} else {
			msg.Content = []claudeContentBlock{{
				Type: "text",
				Text: m.Content,
			}}
		}
		req.Messages = append(req.Messages, msg)
	}

	// Add tools if available.
	// Claude API requires tool names matching ^[a-zA-Z0-9_-]{1,128}$,
	// so we convert dots to underscores (e.g., "task.add" → "task_add").
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

func (c *ClaudeAdapter) doRequest(ctx context.Context, body []byte) (agent.Response, error) {
	req, err := http.NewRequestWithContext(ctx, "POST", c.baseURL, bytes.NewReader(body))
	if err != nil {
		return agent.Response{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", c.apiKey)
	req.Header.Set("anthropic-version", claudeAPIVersion)

	resp, err := c.client.Do(req)
	if err != nil {
		return agent.Response{}, err
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return agent.Response{}, fmt.Errorf("claude: read response: %w", err)
	}

	if resp.StatusCode == 429 || resp.StatusCode >= 500 {
		return agent.Response{}, fmt.Errorf("claude: retryable status %d: %s", resp.StatusCode, string(data))
	}
	if resp.StatusCode != 200 {
		return agent.Response{}, fmt.Errorf("claude: status %d: %s", resp.StatusCode, string(data))
	}

	return c.parseResponse(data)
}

func (c *ClaudeAdapter) parseResponse(data []byte) (agent.Response, error) {
	var resp claudeResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return agent.Response{}, fmt.Errorf("claude: parse response: %w", err)
	}

	result := agent.Response{
		TokensIn:  resp.Usage.InputTokens,
		TokensOut: resp.Usage.OutputTokens,
	}

	var textParts []string
	for _, block := range resp.Content {
		switch block.Type {
		case "text":
			textParts = append(textParts, block.Text)
		case "tool_use":
			input, _ := json.Marshal(block.Input)
			result.ToolCalls = append(result.ToolCalls, agent.ToolCall{
				ID:    block.ID,
				Name:  unsanitizeToolName(block.Name),
				Input: string(input),
			})
		}
	}
	result.Content = strings.Join(textParts, "")

	return result, nil
}

// sanitizeToolName converts tool names like "task.add" to "task__add"
// for APIs that don't allow dots in tool names. Uses double underscore
// as separator to avoid ambiguity with single underscores in names.
func sanitizeToolName(name string) string {
	return strings.ReplaceAll(name, ".", "__")
}

// unsanitizeToolName converts tool names back from "task__add" to "task.add".
func unsanitizeToolName(name string) string {
	return strings.ReplaceAll(name, "__", ".")
}

// --- Anthropic API types ---

// claudeImageBlock converts an agent.Image into a claudeContentBlock with
// type="image". Returns ok=false when the image has no usable data so the
// caller can skip it cleanly.
func claudeImageBlock(img agent.Image) (claudeContentBlock, bool) {
	switch img.Source {
	case agent.ImageSourceBase64:
		if img.Data == "" {
			return claudeContentBlock{}, false
		}
		mt := img.MimeType
		if mt == "" {
			mt = "image/png"
		}
		return claudeContentBlock{
			Type: "image",
			Source: &claudeImageSource{
				Type:      "base64",
				MediaType: mt,
				Data:      img.Data,
			},
		}, true
	case agent.ImageSourceURL:
		if img.Data == "" {
			return claudeContentBlock{}, false
		}
		return claudeContentBlock{
			Type: "image",
			Source: &claudeImageSource{
				Type: "url",
				URL:  img.Data,
			},
		}, true
	default:
		// Backward-compat: empty Source with non-empty Data is treated as base64.
		if img.Data == "" {
			return claudeContentBlock{}, false
		}
		mt := img.MimeType
		if mt == "" {
			mt = "image/png"
		}
		return claudeContentBlock{
			Type: "image",
			Source: &claudeImageSource{
				Type:      "base64",
				MediaType: mt,
				Data:      img.Data,
			},
		}, true
	}
}

type claudeRequest struct {
	Model     string           `json:"model"`
	MaxTokens int              `json:"max_tokens"`
	System    string           `json:"system,omitempty"`
	Messages  []claudeMessage  `json:"messages"`
	Tools     []claudeTool     `json:"tools,omitempty"`
}

type claudeMessage struct {
	Role    string               `json:"role"`
	Content []claudeContentBlock `json:"content"`
}

type claudeContentBlock struct {
	Type      string              `json:"type"`
	Text      string              `json:"text,omitempty"`
	ID        string              `json:"id,omitempty"`
	Name      string              `json:"name,omitempty"`
	Input     json.RawMessage     `json:"input,omitempty"`
	ToolUseID string              `json:"tool_use_id,omitempty"`
	Content   string              `json:"content,omitempty"`
	Source    *claudeImageSource  `json:"source,omitempty"`
}

// claudeImageSource is the Anthropic image-source shape. For base64
// images, Type="base64" with MediaType + Data set. For URL-fetched
// images, Type="url" with URL set.
type claudeImageSource struct {
	Type      string `json:"type"` // "base64" | "url"
	MediaType string `json:"media_type,omitempty"`
	Data      string `json:"data,omitempty"`
	URL       string `json:"url,omitempty"`
}

type claudeTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"input_schema"`
}

type claudeResponse struct {
	Content []claudeContentBlock `json:"content"`
	Usage   claudeUsage          `json:"usage"`
}

type claudeUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}
