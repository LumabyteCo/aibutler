package agent

import (
	"strings"
	"time"
)

// AgentType identifies the kind of agent.
type AgentType string

const (
	TypePrimary   AgentType = "primary"
	TypeScheduled AgentType = "scheduled"
	// TypeSubagent and TypeBackground are defined in tools.go.
)

// ToolOutput records a single tool call and its output for post-run indexing.
type ToolOutput struct {
	ToolName string
	Output   string
}

const (
	maxToolOutputLen   = 10240 // 10KB max per tool output for indexing
	maxToolOutputCount = 100   // Max tool outputs to keep for post-processing
)

// appendToolOutput adds a ToolOutput to the result, respecting size/count limits.
// Tool outputs are sanitized to remove text that could impersonate LLM role markers
// (prompt injection via tool output).
func (r *Result) appendToolOutput(name, output string) {
	if len(r.ToolOutputs) >= maxToolOutputCount {
		return
	}
	if len(output) > maxToolOutputLen {
		output = output[:maxToolOutputLen] + "...[truncated]"
	}
	output = SanitizeToolOutput(output)
	r.ToolOutputs = append(r.ToolOutputs, ToolOutput{ToolName: name, Output: output})
}

// roleMarkerPrefixes lists strings that could be used in tool outputs to impersonate
// LLM conversation roles and inject malicious instructions.
var roleMarkerPrefixes = []string{
	"system:",
	"assistant:",
	"user:",
	"<|system|>",
	"<|assistant|>",
	"<|user|>",
	"<|im_start|>",
	"<|im_end|>",
}

// SanitizeToolOutput strips or escapes text patterns that could confuse the LLM
// into treating tool output as role markers or system instructions.
func SanitizeToolOutput(output string) string {
	for _, prefix := range roleMarkerPrefixes {
		// Replace at start of lines to prevent injection of fake role markers.
		output = strings.ReplaceAll(output, "\n"+prefix, "\n[filtered]"+prefix[len(prefix)-1:])
		if strings.HasPrefix(output, prefix) {
			output = "[filtered]" + prefix[len(prefix)-1:] + output[len(prefix):]
		}
	}
	return output
}

// Result holds the outcome of an agent run.
type Result struct {
	ID          string
	ParentID    string
	SessionID   string
	Status      State
	Output      string
	Artifacts   []Artifact
	ToolOutputs []ToolOutput // Tool call outputs for FTS5/entity indexing
	TokensIn    int
	TokensOut   int
	CostUSD     float64
	ToolCalls   int
	Duration    time.Duration
	Error       string
}

// Artifact is a file or output produced by the agent.
type Artifact struct {
	Type string // "file", "message", "data"
	Path string
	Data []byte
}
