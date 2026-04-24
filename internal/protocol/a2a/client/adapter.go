package client

import (
	"context"
	"fmt"

	"github.com/LumabyteCo/aibutler/internal/agent"
	"github.com/LumabyteCo/aibutler/internal/protocol/a2a"
)

// taskResultAdapter wraps a2a.TaskResult to implement agent.A2ATaskResult.
type taskResultAdapter struct {
	result *a2a.TaskResult
}

func (r *taskResultAdapter) GetStatus() string { return r.result.Status }
func (r *taskResultAdapter) GetOutput() string { return r.result.Output }
func (r *taskResultAdapter) GetError() string  { return r.result.Error }
func (r *taskResultAdapter) GetTaskID() string { return r.result.ID }

// MeshAdapter wraps the A2A v2 Client to implement agent.A2AClient.
type MeshAdapter struct {
	client *Client
}

// NewMeshAdapter creates an adapter that bridges the A2A v2 Client to the agent.A2AClient interface.
func NewMeshAdapter(c *Client) *MeshAdapter {
	return &MeshAdapter{client: c}
}

// Discover fetches the agent card from a peer URL.
func (a *MeshAdapter) Discover(ctx context.Context, url string) (interface{}, error) {
	card, err := a.client.Discover(ctx, url)
	if err != nil {
		return nil, err
	}
	return card, nil
}

// Delegate sends a task to a peer and returns a result.
func (a *MeshAdapter) Delegate(ctx context.Context, peerURL, token, task string) (agent.A2ATaskResult, error) {
	result, err := a.client.Delegate(ctx, peerURL, token, task)
	if err != nil {
		return nil, err
	}
	return &taskResultAdapter{result: result}, nil
}

// GetTaskStatus polls the task status from a peer.
func (a *MeshAdapter) GetTaskStatus(ctx context.Context, peerURL, taskID string) (string, error) {
	status, err := a.client.GetTask(ctx, peerURL, taskID)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s: %s", status.LifecycleState, status.Output), nil
}
