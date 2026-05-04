package model

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"time"

	"github.com/LumabyteCo/aibutler/internal/agent"
)

const openaiAPIURL = "https://api.openai.com/v1/chat/completions"

// OpenAIAdapter implements agent.ModelAdapter for the OpenAI Chat Completions API.
type OpenAIAdapter struct {
	apiKey      string
	model       string
	client      *http.Client
	retries     int
	baseURL     string // overridable for testing and OpenAI-compatible endpoints
	tools       []agent.ToolDef
	maxTokens   int
	temperature float64
}

// NewOpenAI creates an OpenAI model adapter.
func NewOpenAI(apiKey, model string, timeout time.Duration, retries int) *OpenAIAdapter {
	return &OpenAIAdapter{
		apiKey:      apiKey,
		model:       model,
		client:      &http.Client{Timeout: timeout},
		retries:     retries,
		baseURL:     openaiAPIURL,
		maxTokens:   8192,
		temperature: 0.7,
	}
}

// SetMaxTokens overrides the default max_tokens for API requests.
func (o *OpenAIAdapter) SetMaxTokens(n int) {
	if n > 0 {
		o.maxTokens = n
	}
}

// SetTemperature overrides the default temperature for API requests.
func (o *OpenAIAdapter) SetTemperature(t float64) {
	o.temperature = t
}

// SetHTTPClient replaces the adapter's HTTP client (e.g., with a pooled client).
func (o *OpenAIAdapter) SetHTTPClient(client *http.Client) {
	if client != nil {
		o.client = client
	}
}

// SetTools sets the tool definitions for subsequent Complete calls.
func (o *OpenAIAdapter) SetTools(tools []agent.ToolDef) {
	o.tools = tools
}

// Complete sends messages to the OpenAI Chat Completions API and returns the response.
func (o *OpenAIAdapter) Complete(ctx context.Context, messages []agent.Message) (agent.Response, error) {
	reqBody, err := o.buildRequest(messages)
	if err != nil {
		return agent.Response{}, fmt.Errorf("openai: build request: %w", err)
	}

	var lastErr error
	for attempt := 0; attempt <= o.retries; attempt++ {
		if attempt > 0 {
			backoff := time.Duration(math.Pow(2, float64(attempt-1))) * time.Second
			select {
			case <-ctx.Done():
				return agent.Response{}, ctx.Err()
			case <-time.After(backoff):
			}
		}

		resp, err := o.doRequest(ctx, reqBody)
		if err != nil {
			lastErr = err
			continue
		}
		return resp, nil
	}
	return agent.Response{}, fmt.Errorf("openai: all %d attempts failed: %w", o.retries+1, lastErr)
}

func (o *OpenAIAdapter) buildRequest(messages []agent.Message) ([]byte, error) {
	req := openaiRequest{
		Model:       o.model,
		MaxTokens:   o.maxTokens,
		Temperature: &o.temperature,
	}

	for _, m := range messages {
		msg := openaiOutMessage{Role: m.Role}
		switch {
		case m.Role == "tool":
			msg.Content = m.Content
			msg.ToolCallID = m.ToolID
		case m.Role == "assistant" && len(m.ToolCalls) > 0:
			// Assistant message with tool calls: include tool_calls array.
			msg.Content = m.Content
			for _, tc := range m.ToolCalls {
				msg.ToolCalls = append(msg.ToolCalls, openaiToolCall{
					ID:   tc.ID,
					Type: "function",
					Function: struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					}{
						Name:      sanitizeToolName(tc.Name),
						Arguments: tc.Input,
					},
				})
			}
		case len(m.Images) > 0:
			// Multimodal: render Content as an array of typed parts.
			parts := make([]openaiContentPart, 0, 1+len(m.Images))
			if m.Content != "" {
				parts = append(parts, openaiContentPart{Type: "text", Text: m.Content})
			}
			for _, img := range m.Images {
				url := imageToDataURL(img)
				if url == "" {
					continue
				}
				parts = append(parts, openaiContentPart{
					Type:     "image_url",
					ImageURL: &openaiImageURL{URL: url},
				})
			}
			msg.Content = parts
		default:
			msg.Content = m.Content
		}
		req.Messages = append(req.Messages, msg)
	}

	// Add tools if available.
	// OpenAI API requires function names matching ^[a-zA-Z0-9_-]{1,64}$,
	// so we convert dots to double underscores (e.g., "task.add" → "task__add").
	for _, t := range o.tools {
		var params json.RawMessage
		if t.Schema != "" {
			params = json.RawMessage(t.Schema)
		} else {
			params = json.RawMessage(`{"type":"object","properties":{}}`)
		}
		req.Tools = append(req.Tools, openaiTool{
			Type: "function",
			Function: openaiFunction{
				Name:        sanitizeToolName(t.Name),
				Description: t.Description,
				Parameters:  params,
			},
		})
	}

	return json.Marshal(req)
}

func (o *OpenAIAdapter) doRequest(ctx context.Context, body []byte) (agent.Response, error) {
	req, err := http.NewRequestWithContext(ctx, "POST", o.baseURL, bytes.NewReader(body))
	if err != nil {
		return agent.Response{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	if o.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+o.apiKey)
	}

	resp, err := o.client.Do(req)
	if err != nil {
		return agent.Response{}, err
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return agent.Response{}, fmt.Errorf("openai: read response: %w", err)
	}

	if resp.StatusCode == 429 || resp.StatusCode >= 500 {
		return agent.Response{}, fmt.Errorf("openai: retryable status %d: %s", resp.StatusCode, string(data))
	}
	if resp.StatusCode != 200 {
		return agent.Response{}, fmt.Errorf("openai: status %d: %s", resp.StatusCode, string(data))
	}

	return o.parseResponse(data)
}

func (o *OpenAIAdapter) parseResponse(data []byte) (agent.Response, error) {
	var resp openaiResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return agent.Response{}, fmt.Errorf("openai: parse response: %w", err)
	}

	result := agent.Response{
		TokensIn:  resp.Usage.PromptTokens,
		TokensOut: resp.Usage.CompletionTokens,
	}

	if len(resp.Choices) > 0 {
		choice := resp.Choices[0]
		result.Content = choice.Message.Content

		for _, tc := range choice.Message.ToolCalls {
			result.ToolCalls = append(result.ToolCalls, agent.ToolCall{
				ID:    tc.ID,
				Name:  unsanitizeToolName(tc.Function.Name),
				Input: tc.Function.Arguments,
			})
		}
	}

	return result, nil
}

// --- OpenAI API types ---

type openaiRequest struct {
	Model       string             `json:"model"`
	Messages    []openaiOutMessage `json:"messages"`
	Tools       []openaiTool       `json:"tools,omitempty"`
	MaxTokens   int                `json:"max_tokens,omitempty"`
	Temperature *float64           `json:"temperature,omitempty"`
}

// openaiOutMessage is the outbound message shape. Content is `interface{}`
// so it can serialise as either a plain string (text-only path) or an
// array of openaiContentPart (multimodal — text + image_url parts).
type openaiOutMessage struct {
	Role       string           `json:"role"`
	Content    interface{}      `json:"content"`
	ToolCallID string           `json:"tool_call_id,omitempty"`
	ToolCalls  []openaiToolCall `json:"tool_calls,omitempty"`
}

// openaiContentPart is one entry in a multimodal content array. The
// OpenAI Chat Completions API (and Ollama's /v1/chat/completions, and
// LM Studio, vLLM, etc.) accept this format for vision-capable models.
type openaiContentPart struct {
	Type     string          `json:"type"` // "text" | "image_url"
	Text     string          `json:"text,omitempty"`
	ImageURL *openaiImageURL `json:"image_url,omitempty"`
}

type openaiImageURL struct {
	URL string `json:"url"`
}

// openaiMessage is the inbound (response) message shape. The server
// always returns plain string content even when the request was
// multimodal, so this remains string-typed.
type openaiMessage struct {
	Role       string           `json:"role"`
	Content    string           `json:"content"`
	ToolCallID string           `json:"tool_call_id,omitempty"`
	ToolCalls  []openaiToolCall `json:"tool_calls,omitempty"`
}

type openaiTool struct {
	Type     string         `json:"type"`
	Function openaiFunction `json:"function"`
}

type openaiFunction struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}

// imageToDataURL renders an agent.Image as the URL string the OpenAI-compat
// API expects. Base64 images become a data: URL with the declared MIME
// type (defaulting to image/png if omitted). URL-source images pass
// through verbatim. Returns "" if the image is unusable.
func imageToDataURL(img agent.Image) string {
	switch img.Source {
	case agent.ImageSourceBase64:
		if img.Data == "" {
			return ""
		}
		mt := img.MimeType
		if mt == "" {
			mt = "image/png"
		}
		return "data:" + mt + ";base64," + img.Data
	case agent.ImageSourceURL:
		return img.Data
	default:
		// Backward-compat: treat empty Source as base64 if Data is set.
		if img.Data != "" {
			mt := img.MimeType
			if mt == "" {
				mt = "image/png"
			}
			return "data:" + mt + ";base64," + img.Data
		}
		return ""
	}
}

type openaiToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type openaiResponse struct {
	Choices []openaiChoice `json:"choices"`
	Usage   openaiUsage    `json:"usage"`
}

type openaiChoice struct {
	Message openaiMessage `json:"message"`
}

type openaiUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
}
