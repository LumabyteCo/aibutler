package agent_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/LumabyteCo/aibutler/internal/agent"
	"github.com/LumabyteCo/aibutler/testutil"
)

// pinCaptureModel responds with a safety-control tool call so we can verify
// what the agent stored in history vs what it executed.
type pinCaptureModel struct {
	calls int
}

func (m *pinCaptureModel) Complete(_ context.Context, messages []agent.Message) (agent.Response, error) {
	m.calls++
	if m.calls == 1 {
		// First turn: request the unlock with the PIN the user supplied.
		return agent.Response{
			ToolCalls: []agent.ToolCall{{
				ID:   "tc-1",
				Name: "iot.safety.control",
				Input: `{"device_id":"lock.front_door","action":"unlock","confirmed":true,"pin":"2468"}`,
			}},
		}, nil
	}
	// Second turn: the model sees the history — return it so the test can
	// inspect exactly what the model would be able to quote.
	var sb strings.Builder
	for _, msg := range messages {
		b, _ := json.Marshal(msg)
		sb.Write(b)
		sb.WriteString("\n")
	}
	return agent.Response{Content: sb.String()}, nil
}

// TestAgentHistoryRedactsPIN guards the B10 security fix at the agent-loop
// level: when a tool call carries a PIN, the executed call uses the REAL pin
// (verified against the vault by the tool), but the history copy the model
// sees in subsequent turns must show [REDACTED] — so the model can never
// quote the working PIN back in prose.
func TestAgentHistoryRedactsPIN(t *testing.T) {
	model := &pinCaptureModel{}

	a := agent.New(agent.Config{
		ID:        "agent-pin-redact",
		SessionID: "sess-pin",
		Task:      "Unlock the front door. I confirm. Safety PIN: 2468",
		Type:      agent.TypePrimary,
		Model:     model,
		Mode:      agent.ModeSingle,
		// No tool executor: the tool result is a stub string, which is fine —
		// we only care about what enters HISTORY.
		MaxToolCalls: 5,
	})

	result, err := a.Run(context.Background())
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if result.Status != agent.StateCompleted {
		t.Fatalf("status = %s, want completed", result.Status)
	}

	// The final output is the history dump from the model's perspective.
	// Invariant (B10 design): the CURRENT turn's user message stays intact
	// for the duration of the run (the model reads it to build follow-up
	// tool calls), but the tool-call INPUT recorded in history is
	// redacted — that's the artifact later turns would quote from.
	var toolInputSeen bool
	var toolInput string
	for _, line := range strings.Split(result.Output, "\n") {
		if strings.Contains(line, "iot.safety.control") {
			toolInputSeen = true
			toolInput = line
		}
	}
	if !toolInputSeen {
		t.Fatalf("expected the safety tool call in history dump:\n%s", result.Output[:min(len(result.Output), 800)])
	}
	if strings.Contains(toolInput, "2468") {
		t.Errorf("PIN VALUE LEAKED into the stored tool-call input:\n%s", toolInput)
	}
	if !strings.Contains(toolInput, "[REDACTED]") {
		t.Errorf("stored tool-call input should carry [REDACTED]:\n%s", toolInput)
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

var _ = testutil.NewFakeModel // keep the testutil import if cases expand