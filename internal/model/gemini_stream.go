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

// CompleteStream sends a streaming request to the Gemini API.
// It returns a channel that receives StreamEvent values as they arrive.
func (g *GeminiAdapter) CompleteStream(ctx context.Context, messages []agent.Message) (<-chan agent.StreamEvent, error) {
	body, err := g.buildRequest(messages)
	if err != nil {
		return nil, fmt.Errorf("gemini: build stream request: %w", err)
	}

	apiURL := fmt.Sprintf("%s/%s:streamGenerateContent?alt=sse&key=%s", g.baseURL, g.model, g.apiKey)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiURL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("gemini: create stream request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := g.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("gemini: stream connect: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("gemini: stream status %d", resp.StatusCode)
	}

	ch := make(chan agent.StreamEvent, 64)
	go g.parseGeminiStream(ctx, resp, ch)
	return ch, nil
}

func (g *GeminiAdapter) parseGeminiStream(ctx context.Context, resp *http.Response, ch chan<- agent.StreamEvent) {
	defer resp.Body.Close()
	defer close(ch)

	scanner := bufio.NewScanner(resp.Body)
	funcCallIndex := 0

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

		var chunk geminiResponse
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue
		}

		// Emit usage if available.
		if chunk.UsageMetadata.PromptTokenCount > 0 || chunk.UsageMetadata.CandidatesTokenCount > 0 {
			ch <- agent.StreamEvent{
				Type:      "usage",
				TokensIn:  chunk.UsageMetadata.PromptTokenCount,
				TokensOut: chunk.UsageMetadata.CandidatesTokenCount,
			}
		}

		if len(chunk.Candidates) == 0 {
			continue
		}

		candidate := chunk.Candidates[0].Content
		for _, part := range candidate.Parts {
			if part.Text != "" {
				ch <- agent.StreamEvent{
					Type: "text_delta",
					Text: part.Text,
				}
			}
			if part.FunctionCall != nil {
				callID := fmt.Sprintf("call-%d", funcCallIndex)
				funcCallIndex++
				toolName := unsanitizeToolName(part.FunctionCall.Name)

				ch <- agent.StreamEvent{
					Type:       "tool_use_start",
					ToolCallID: callID,
					ToolName:   toolName,
				}

				argsJSON := string(part.FunctionCall.Args)
				if argsJSON != "" {
					ch <- agent.StreamEvent{
						Type:        "input_json_delta",
						ToolCallID:  callID,
						ToolName:    toolName,
						PartialJSON: argsJSON,
					}
				}
			}
		}
	}

	if err := scanner.Err(); err != nil {
		ch <- agent.StreamEvent{Type: "error", Error: fmt.Errorf("gemini stream scan: %w", err)}
		return
	}

	ch <- agent.StreamEvent{Type: "message_stop"}
}
