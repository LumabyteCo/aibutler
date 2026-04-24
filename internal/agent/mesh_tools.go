package agent

import (
	"context"
	"encoding/json"
	"fmt"
)

// ToolDef is the return type used by all New*Tool functions, mirroring the existing pattern
// of returning (name, desc, schema, cap, exec) tuples.
// (We reuse the existing approach from tools.go rather than introducing a new struct.)

// RegistryLookup discovers peer agents by capability.
// Narrow interface to avoid import cycles with protocol/a2a/registry.
type RegistryLookup interface {
	Discover(ctx context.Context, capability string) ([]RegistryEntry, error)
}

// RegistryEntry is a peer agent returned from discovery.
type RegistryEntry struct {
	Name         string
	URL          string
	Capabilities []string
}

// TaskRunner executes a task locally and returns the result.
// Narrow interface reused by swarm and A2A delegation.
type TaskRunner interface {
	RunTask(ctx context.Context, task string) (string, error)
}

// A2AClient communicates with peer agents over the A2A protocol.
// Narrow interface to avoid importing protocol/a2a directly.
type A2AClient interface {
	// Discover fetches the agent card from a peer URL.
	Discover(ctx context.Context, url string) (interface{}, error)
	// Delegate sends a task to a peer and returns a result.
	Delegate(ctx context.Context, peerURL, token, task string) (A2ATaskResult, error)
	// GetTaskStatus polls the task status from a peer.
	GetTaskStatus(ctx context.Context, peerURL, taskID string) (string, error)
}

// A2ATaskResult is the result of a delegated task.
type A2ATaskResult interface {
	GetStatus() string
	GetOutput() string
	GetError() string
	GetTaskID() string
}

// NewPeerTool returns the "agent.peer" tool.
// It discovers a peer agent via the registry by capability, delegates the task via
// the A2A client, and falls back to the local runner if no peer is found.
func NewPeerTool(registry RegistryLookup, localRunner TaskRunner, a2aClient A2AClient) (name, desc, schema, cap string, exec func(ctx context.Context, input string) (string, error)) {
	return "agent.peer",
		"Delegate a task to a peer agent discovered by capability. Falls back to local execution if no peer is available.",
		`{"type":"object","properties":{` +
			`"capability":{"type":"string","description":"Required capability for the peer agent (e.g. memory.search)"},` +
			`"task":{"type":"string","description":"Task description to delegate"},` +
			`"token":{"type":"string","description":"Bearer token for the peer agent"},` +
			`"async":{"type":"boolean","description":"If true, delegate asynchronously and return task_id immediately","default":false}` +
			`},"required":["capability","task"]}`,
		"agent.delegate",
		func(ctx context.Context, input string) (string, error) {
			var args struct {
				Capability string `json:"capability"`
				Task       string `json:"task"`
				Token      string `json:"token"`
				Async      bool   `json:"async"`
			}
			if err := json.Unmarshal([]byte(input), &args); err != nil {
				return "", fmt.Errorf("agent.peer: invalid input: %w", err)
			}
			if args.Capability == "" {
				return "", fmt.Errorf("agent.peer: capability is required")
			}
			if args.Task == "" {
				return "", fmt.Errorf("agent.peer: task is required")
			}

			// Discover peers by capability.
			var peers []RegistryEntry
			if registry != nil {
				var err error
				peers, err = registry.Discover(ctx, args.Capability)
				if err != nil {
					// Non-fatal: fall back to local.
					peers = nil
				}
			}

			// No peer found → fall back to local runner.
			if len(peers) == 0 {
				if localRunner == nil {
					return "", fmt.Errorf("agent.peer: no peer found for capability %q and no local runner configured", args.Capability)
				}
				result, err := localRunner.RunTask(ctx, args.Task)
				if err != nil {
					return "", fmt.Errorf("agent.peer: local fallback: %w", err)
				}
				out, _ := json.Marshal(map[string]interface{}{
					"source": "local",
					"output": result,
				})
				return string(out), nil
			}

			// Delegate to first available peer.
			peer := peers[0]
			if a2aClient == nil {
				return "", fmt.Errorf("agent.peer: A2A client not configured")
			}

			result, err := a2aClient.Delegate(ctx, peer.URL, args.Token, args.Task)
			if err != nil {
				// Delegation failed; fall back to local.
				if localRunner != nil {
					localResult, localErr := localRunner.RunTask(ctx, args.Task)
					if localErr != nil {
						return "", fmt.Errorf("agent.peer: delegate failed (%v) and local fallback failed (%w)", err, localErr)
					}
					out, _ := json.Marshal(map[string]interface{}{
						"source": "local_fallback",
						"output": localResult,
					})
					return string(out), nil
				}
				return "", fmt.Errorf("agent.peer: delegate to %s: %w", peer.URL, err)
			}

			if args.Async {
				out, _ := json.Marshal(map[string]interface{}{
					"task_id": result.GetTaskID(),
					"status":  "submitted",
					"peer":    peer.Name,
				})
				return string(out), nil
			}

			if result.GetError() != "" {
				return "", fmt.Errorf("agent.peer: peer returned error: %s", result.GetError())
			}

			out, _ := json.Marshal(map[string]interface{}{
				"source": "peer",
				"peer":   peer.Name,
				"status": result.GetStatus(),
				"output": result.GetOutput(),
			})
			return string(out), nil
		}
}

// NewCriticTool returns the "agent.critic" tool.
// Spawns a second agent call with a critique prompt asking to review and improve the input.
func NewCriticTool(criticRunner TaskRunner) (name, desc, schema, cap string, exec func(ctx context.Context, input string) (string, error)) {
	return "agent.critic",
		"Review and critique content for quality, accuracy, or other focus areas. Returns critique and suggested improvements.",
		`{"type":"object","properties":{` +
			`"content":{"type":"string","description":"The content to critique"},` +
			`"focus":{"type":"string","description":"Area of focus for the critique (e.g. accuracy, clarity, completeness)","default":"quality"}` +
			`},"required":["content"]}`,
		"agent.delegate",
		func(ctx context.Context, input string) (string, error) {
			var args struct {
				Content string `json:"content"`
				Focus   string `json:"focus"`
			}
			if err := json.Unmarshal([]byte(input), &args); err != nil {
				return "", fmt.Errorf("agent.critic: invalid input: %w", err)
			}
			if args.Content == "" {
				return "", fmt.Errorf("agent.critic: content is required")
			}
			if args.Focus == "" {
				args.Focus = "quality"
			}
			if criticRunner == nil {
				return "", fmt.Errorf("agent.critic: critic runner not configured")
			}

			prompt := fmt.Sprintf(
				"You are a critic. Review this content for %s. "+
					"Content:\n%s\n\n"+
					"Return your critique and an improved version.",
				args.Focus, args.Content,
			)

			critique, err := criticRunner.RunTask(ctx, prompt)
			if err != nil {
				return "", fmt.Errorf("agent.critic: %w", err)
			}
			out, _ := json.Marshal(map[string]string{
				"critique": critique,
				"focus":    args.Focus,
			})
			return string(out), nil
		}
}

// NewTaskStatusTool returns the "agent.task_status" tool.
// Polls GET /a2a/tasks/{id} on a given peer URL using the A2A client.
func NewTaskStatusTool(a2aClient A2AClient) (name, desc, schema, cap string, exec func(ctx context.Context, input string) (string, error)) {
	return "agent.task_status",
		"Poll the status of a delegated task on a peer agent.",
		`{"type":"object","properties":{` +
			`"peer_url":{"type":"string","description":"Base URL of the peer agent (e.g. http://peer:8081)"},` +
			`"task_id":{"type":"string","description":"Task ID returned when the task was submitted"}` +
			`},"required":["peer_url","task_id"]}`,
		"agent.delegate",
		func(ctx context.Context, input string) (string, error) {
			var args struct {
				PeerURL string `json:"peer_url"`
				TaskID  string `json:"task_id"`
			}
			if err := json.Unmarshal([]byte(input), &args); err != nil {
				return "", fmt.Errorf("agent.task_status: invalid input: %w", err)
			}
			if args.PeerURL == "" {
				return "", fmt.Errorf("agent.task_status: peer_url is required")
			}
			if args.TaskID == "" {
				return "", fmt.Errorf("agent.task_status: task_id is required")
			}
			if a2aClient == nil {
				return "", fmt.Errorf("agent.task_status: A2A client not configured")
			}

			status, err := a2aClient.GetTaskStatus(ctx, args.PeerURL, args.TaskID)
			if err != nil {
				return "", fmt.Errorf("agent.task_status: %w", err)
			}
			out, _ := json.Marshal(map[string]string{
				"task_id":  args.TaskID,
				"peer_url": args.PeerURL,
				"status":   status,
			})
			return string(out), nil
		}
}
