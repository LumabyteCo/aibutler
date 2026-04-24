package agent_test

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/LumabyteCo/aibutler/internal/agent"
)

// mockRegistry implements agent.RegistryLookup.
type mockMeshRegistry struct {
	entries []agent.RegistryEntry
	err     error
}

func (m *mockMeshRegistry) Discover(_ context.Context, _ string) ([]agent.RegistryEntry, error) {
	return m.entries, m.err
}

// mockTaskRunner implements agent.TaskRunner.
type mockTaskRunner struct {
	result string
	err    error
}

func (m *mockTaskRunner) RunTask(_ context.Context, _ string) (string, error) {
	return m.result, m.err
}

// mockA2AResult implements agent.A2ATaskResult.
type mockA2AResult struct {
	status  string
	output  string
	errMsg  string
	taskID  string
}

func (r *mockA2AResult) GetStatus() string  { return r.status }
func (r *mockA2AResult) GetOutput() string  { return r.output }
func (r *mockA2AResult) GetError() string   { return r.errMsg }
func (r *mockA2AResult) GetTaskID() string  { return r.taskID }

// mockA2AClient implements agent.A2AClient.
type mockA2AClient struct {
	delegateResult agent.A2ATaskResult
	delegateErr    error
	statusResult   string
	statusErr      error
}

func (m *mockA2AClient) Discover(_ context.Context, _ string) (interface{}, error) {
	return nil, nil
}
func (m *mockA2AClient) Delegate(_ context.Context, _, _, _ string) (agent.A2ATaskResult, error) {
	return m.delegateResult, m.delegateErr
}
func (m *mockA2AClient) GetTaskStatus(_ context.Context, _, _ string) (string, error) {
	return m.statusResult, m.statusErr
}

func TestPeerTool_DiscoversPeer(t *testing.T) {
	peers := []agent.RegistryEntry{
		{Name: "peer1", URL: "http://peer1:8081", Capabilities: []string{"memory.search"}},
	}
	reg := &mockMeshRegistry{entries: peers}
	localRunner := &mockTaskRunner{result: "local result"}
	a2aClient := &mockA2AClient{
		delegateResult: &mockA2AResult{status: "completed", output: "peer result", taskID: "task-1"},
	}

	_, _, _, _, exec := agent.NewPeerTool(reg, localRunner, a2aClient)

	input, _ := json.Marshal(map[string]interface{}{
		"capability": "memory.search",
		"task":       "find Alice's meetings",
	})
	out, err := exec(context.Background(), string(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var result map[string]interface{}
	json.Unmarshal([]byte(out), &result)
	if result["source"] != "peer" {
		t.Errorf("expected source=peer, got %v", result["source"])
	}
	if result["output"] != "peer result" {
		t.Errorf("expected output='peer result', got %v", result["output"])
	}
}

func TestPeerTool_FallsBackToLocal(t *testing.T) {
	reg := &mockMeshRegistry{entries: nil} // No peers found.
	localRunner := &mockTaskRunner{result: "local fallback result"}
	a2aClient := &mockA2AClient{}

	_, _, _, _, exec := agent.NewPeerTool(reg, localRunner, a2aClient)

	input, _ := json.Marshal(map[string]interface{}{
		"capability": "unknown.capability",
		"task":       "some task",
	})
	out, err := exec(context.Background(), string(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var result map[string]interface{}
	json.Unmarshal([]byte(out), &result)
	if result["source"] != "local" {
		t.Errorf("expected source=local, got %v", result["source"])
	}
}

func TestCriticTool_ReviewsContent(t *testing.T) {
	criticRunner := &mockTaskRunner{result: "This content has good accuracy but lacks detail. Improved: ..."}

	_, _, _, _, exec := agent.NewCriticTool(criticRunner)

	input, _ := json.Marshal(map[string]interface{}{
		"content": "The sky is blue.",
		"focus":   "accuracy",
	})
	out, err := exec(context.Background(), string(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var result map[string]string
	json.Unmarshal([]byte(out), &result)
	if result["focus"] != "accuracy" {
		t.Errorf("expected focus=accuracy, got %q", result["focus"])
	}
	if result["critique"] == "" {
		t.Error("expected non-empty critique")
	}
}

func TestTaskStatusTool_ReturnsStatus(t *testing.T) {
	a2aClient := &mockA2AClient{statusResult: "completed"}

	_, _, _, _, exec := agent.NewTaskStatusTool(a2aClient)

	input, _ := json.Marshal(map[string]string{
		"peer_url": "http://peer1:8081",
		"task_id":  "abc123",
	})
	out, err := exec(context.Background(), string(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var result map[string]string
	json.Unmarshal([]byte(out), &result)
	if result["status"] != "completed" {
		t.Errorf("expected status=completed, got %q", result["status"])
	}
	if result["task_id"] != "abc123" {
		t.Errorf("expected task_id=abc123, got %q", result["task_id"])
	}
}

func TestPeerTool_AsyncMode(t *testing.T) {
	peers := []agent.RegistryEntry{
		{Name: "peer1", URL: "http://peer1:8081"},
	}
	reg := &mockMeshRegistry{entries: peers}
	localRunner := &mockTaskRunner{}
	a2aClient := &mockA2AClient{
		delegateResult: &mockA2AResult{status: "submitted", taskID: "task-async-1"},
	}

	_, _, _, _, exec := agent.NewPeerTool(reg, localRunner, a2aClient)

	input, _ := json.Marshal(map[string]interface{}{
		"capability": "data.analyze",
		"task":       "analyze this dataset",
		"async":      true,
	})
	out, err := exec(context.Background(), string(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var result map[string]interface{}
	json.Unmarshal([]byte(out), &result)
	if result["status"] != "submitted" {
		t.Errorf("expected status=submitted, got %v", result["status"])
	}
	if result["task_id"] == "" {
		t.Error("expected non-empty task_id in async response")
	}
}

func TestMeshToolNames(t *testing.T) {
	reg := &mockMeshRegistry{}
	runner := &mockTaskRunner{}
	client := &mockA2AClient{}

	peerName, _, _, _, _ := agent.NewPeerTool(reg, runner, client)
	criticName, _, _, _, _ := agent.NewCriticTool(runner)
	statusName, _, _, _, _ := agent.NewTaskStatusTool(client)

	if peerName != "agent.peer" {
		t.Errorf("expected agent.peer, got %q", peerName)
	}
	if criticName != "agent.critic" {
		t.Errorf("expected agent.critic, got %q", criticName)
	}
	if statusName != "agent.task_status" {
		t.Errorf("expected agent.task_status, got %q", statusName)
	}
}

func TestCriticTool_RequiresContent(t *testing.T) {
	runner := &mockTaskRunner{err: fmt.Errorf("should not be called")}
	_, _, _, _, exec := agent.NewCriticTool(runner)

	_, err := exec(context.Background(), `{"focus":"accuracy"}`)
	if err == nil {
		t.Fatal("expected error when content is missing")
	}
}
