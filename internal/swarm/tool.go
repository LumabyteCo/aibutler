package swarm

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	swarmws "github.com/LumabyteCo/aibutler/internal/memory/swarm"
)

// NewSwarmTool returns the agent.swarm tool for use with tool.FuncTool.
func NewSwarmTool(orch *Orchestrator, ws *swarmws.Workspace) (name, desc, schema, cap string, exec func(ctx context.Context, input string) (string, error)) {
	return "agent.swarm",
		"Decompose a complex goal into subtasks and execute them as a swarm of agents, returning the synthesized result.",
		`{"type":"object","properties":{"goal":{"type":"string","description":"High-level goal to accomplish"},"run_id":{"type":"string","description":"Optional run ID for tracking"}},"required":["goal"]}`,
		"agent.swarm",
		func(ctx context.Context, input string) (string, error) {
			var args struct {
				Goal  string `json:"goal"`
				RunID string `json:"run_id"`
			}
			if err := json.Unmarshal([]byte(input), &args); err != nil {
				return "", fmt.Errorf("agent.swarm: invalid input: %w", err)
			}
			if args.Goal == "" {
				return "", fmt.Errorf("agent.swarm: goal is required")
			}
			runID := args.RunID
			if runID == "" {
				runID = fmt.Sprintf("swarm-%d", time.Now().UnixNano())
			}
			_ = ws // workspace available for future use by subtasks
			return orch.Run(ctx, runID, args.Goal)
		}
}
