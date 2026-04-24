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

// CompleteStream sends a streaming request to the OpenAI Chat Completions API.
// It returns a channel that receives StreamEvent values as they arrive.
func (o *OpenAIAdapter) CompleteStream(ctx context.Context, messages []agent.Message) (<-chan agent.StreamEvent, error) {
	reqBody, err := o.buildStreamRequest(messages)
	if err != nil {
		return nil, fmt.Errorf("openai: build stream request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", o.baseURL, bytes.NewReader(reqBody))
	if err != nil {
		return nil, fmt.Errorf("openai: create stream request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if o.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+o.apiKey)
	}

	resp, err := o.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("openai: stream connect: %w", err)
	}

	if resp.StatusCode != 200 {
		resp.Body.Close()
		return nil, fmt.Errorf("openai: stream status %d", resp.StatusCode)
	}

	ch := make(chan agent.StreamEvent, 64)
	go o.parseOpenAIStream(ctx, resp, ch)
	return ch, nil
}

func (o *OpenAIAdapter) buildStreamRequest(messages []agent.Message) ([]byte, error) {
	req := openaiStreamRequest{
		Model:       o.model,
		MaxTokens:   o.maxTokens,
		Temperature: &o.temperature,
		Stream:      true,
	}

	for _, m := range messages {
		msg := openaiMessage{Role: m.Role}
		if m.Role == "tool" {
			msg.Content = m.Content
			msg.ToolCallID = m.ToolID
		} else if m.Role == "assistant" && len(m.ToolCalls) > 0 {
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
		} else {
			msg.Content = m.Content
		}
		req.Messages = append(req.Messages, msg)
	}

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

// toolCallState tracks partial tool call accumulation during streaming.
type toolCallState struct {
	id        string
	name      string
	arguments string
	started   bool
}

func (o *OpenAIAdapter) parseOpenAIStream(ctx context.Context, resp *http.Response, ch chan<- agent.StreamEvent) {
	defer resp.Body.Close()
	defer close(ch)

	scanner := bufio.NewScanner(resp.Body)
	toolCalls := make(map[int]*toolCallState)

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			ch <- agent.StreamEvent{Type: "error", Error: ctx.Err()}
			return
		default:
		}

		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")

		if data == "[DONE]" {
			ch <- agent.StreamEvent{Type: "message_stop"}
			return
		}

		var chunk openaiStreamChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue
		}

		if len(chunk.Choices) == 0 {
			continue
		}

		choice := chunk.Choices[0]
		delta := choice.Delta

		// Text content.
		if delta.Content != "" {
			ch <- agent.StreamEvent{
				Type: "text_delta",
				Text: delta.Content,
			}
		}

		// Tool calls.
		for _, tc := range delta.ToolCalls {
			state, exists := toolCalls[tc.Index]
			if !exists {
				state = &toolCallState{}
				toolCalls[tc.Index] = state
			}

			if tc.ID != "" {
				state.id = tc.ID
			}
			if tc.Function.Name != "" {
				state.name = tc.Function.Name
			}
			if tc.Function.Arguments != "" {
				state.arguments += tc.Function.Arguments

				// Emit tool_use_start once we have the name (deferred start).
				if !state.started && state.name != "" {
					state.started = true
					ch <- agent.StreamEvent{
						Type:       "tool_use_start",
						ToolCallID: state.id,
						ToolName:   unsanitizeToolName(state.name),
					}
				}

				// Emit input_json_delta for the new portion.
				ch <- agent.StreamEvent{
					Type:        "input_json_delta",
					ToolCallID:  state.id,
					ToolName:    unsanitizeToolName(state.name),
					PartialJSON: tc.Function.Arguments,
				}
			}
		}

		// Finish reason.
		if choice.FinishReason != "" {
			// Emit any tool_use_start for tools that never got argument deltas.
			for _, state := range toolCalls {
				if !state.started && state.name != "" {
					state.started = true
					ch <- agent.StreamEvent{
						Type:       "tool_use_start",
						ToolCallID: state.id,
						ToolName:   unsanitizeToolName(state.name),
					}
				}
			}
		}
	}

	if err := scanner.Err(); err != nil {
		ch <- agent.StreamEvent{Type: "error", Error: fmt.Errorf("openai stream scan: %w", err)}
	}
}

// --- OpenAI streaming types ---

type openaiStreamRequest struct {
	Model       string          `json:"model"`
	Messages    []openaiMessage `json:"messages"`
	Tools       []openaiTool    `json:"tools,omitempty"`
	MaxTokens   int             `json:"max_tokens,omitempty"`
	Temperature *float64        `json:"temperature,omitempty"`
	Stream      bool            `json:"stream"`
}

type openaiStreamChunk struct {
	Choices []openaiStreamChoice `json:"choices"`
}

type openaiStreamChoice struct {
	Delta        openaiStreamDelta `json:"delta"`
	FinishReason string            `json:"finish_reason"`
}

type openaiStreamDelta struct {
	Content   string                   `json:"content"`
	ToolCalls []openaiStreamToolCall   `json:"tool_calls"`
}

type openaiStreamToolCall struct {
	Index    int    `json:"index"`
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}
