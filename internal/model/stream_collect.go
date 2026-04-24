package model

import (
	"encoding/json"
	"strings"

	"github.com/LumabyteCo/aibutler/internal/agent"
)

// CollectStreamResponse aggregates all events from a stream channel into a
// single agent.Response. This is used when the caller needs both streaming
// output (for display) and a final Response (for cost tracking and the agent loop).
func CollectStreamResponse(ch <-chan agent.StreamEvent) agent.Response {
	var resp agent.Response
	var textBuf strings.Builder

	// Track tool calls by ID.
	toolMap := make(map[string]*agent.ToolCall)
	var toolOrder []string

	for evt := range ch {
		switch evt.Type {
		case "text_delta":
			textBuf.WriteString(evt.Text)

		case "tool_use_start":
			tc := &agent.ToolCall{
				ID:   evt.ToolCallID,
				Name: evt.ToolName,
			}
			toolMap[evt.ToolCallID] = tc
			toolOrder = append(toolOrder, evt.ToolCallID)

		case "input_json_delta":
			if tc, ok := toolMap[evt.ToolCallID]; ok {
				tc.Input += evt.PartialJSON
			}

		case "usage":
			resp.TokensIn += evt.TokensIn
			resp.TokensOut += evt.TokensOut

		case "error":
			// Errors are noted but collection continues.

		case "message_stop":
			// Stream complete.
		}
	}

	resp.Content = textBuf.String()

	// Preserve tool call ordering.
	for _, id := range toolOrder {
		if tc, ok := toolMap[id]; ok {
			// Validate the JSON input is well-formed; if not, wrap it.
			if tc.Input != "" && !json.Valid([]byte(tc.Input)) {
				tc.Input = `{"raw":"` + tc.Input + `"}`
			}
			resp.ToolCalls = append(resp.ToolCalls, *tc)
		}
	}

	return resp
}
