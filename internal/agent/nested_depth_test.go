package agent_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/LumabyteCo/aibutler/internal/agent"
	"github.com/LumabyteCo/aibutler/internal/capability"
	"github.com/LumabyteCo/aibutler/testutil"
)

// TestNestedDepth3Succeeds verifies that delegation at depth 2 (max 3) succeeds.
// This represents agent A -> B -> C (3 levels, depth 0->1->2).
func TestNestedDepth3Succeeds(t *testing.T) {
	cfg := agent.DelegateConfig{
		Model:        testutil.NewFakeModel(agent.Response{Content: "deep result"}),
		Tools:        testutil.NewFakeToolExecutor(nil),
		Caps:         capability.NewCapabilitySet(nil),
		Timeout:      10 * time.Second,
		MaxDepth:     3,
		CurrentDepth: 2, // At depth 2, one more level is allowed (depth < maxDepth)
	}
	_, _, _, _, exec := agent.NewDelegateTool(cfg)

	result, err := exec(context.Background(), `{"task":"depth 3 task"}`)
	if err != nil {
		t.Fatalf("depth 3 should succeed: %v", err)
	}

	var out map[string]interface{}
	if err := json.Unmarshal([]byte(result), &out); err != nil {
		t.Fatalf("parse result: %v", err)
	}
	if out["status"] != "completed" {
		t.Errorf("status = %v, want completed", out["status"])
	}
}

// TestNestedDepth4Rejected verifies that delegation at depth 3 (max 3) is rejected.
// This represents trying to go A -> B -> C -> D (depth 3 == maxDepth, rejected).
func TestNestedDepth4Rejected(t *testing.T) {
	cfg := agent.DelegateConfig{
		Model:        testutil.NewFakeModel(agent.Response{Content: "should not run"}),
		Tools:        testutil.NewFakeToolExecutor(nil),
		Caps:         capability.NewCapabilitySet(nil),
		Timeout:      10 * time.Second,
		MaxDepth:     3,
		CurrentDepth: 3, // At max depth — rejected
	}
	_, _, _, _, exec := agent.NewDelegateTool(cfg)

	_, err := exec(context.Background(), `{"task":"too deep"}`)
	if err == nil {
		t.Error("depth 4 delegation should be rejected")
	}
}

// TestNestedSpawnDepth3Succeeds verifies spawn at depth 2 (max 3) succeeds.
func TestNestedSpawnDepth3Succeeds(t *testing.T) {
	cfg := agent.DelegateConfig{
		Model:        testutil.NewFakeModel(agent.Response{Content: "bg done"}),
		Tools:        testutil.NewFakeToolExecutor(nil),
		Caps:         capability.NewCapabilitySet(nil),
		Timeout:      5 * time.Second,
		MaxDepth:     3,
		CurrentDepth: 2,
	}
	_, _, _, _, exec := agent.NewSpawnTool(cfg)

	result, err := exec(context.Background(), `{"task":"spawn at depth 2"}`)
	if err != nil {
		t.Fatalf("spawn at depth 2 should succeed: %v", err)
	}

	var out map[string]interface{}
	json.Unmarshal([]byte(result), &out)
	if out["status"] != "spawned" {
		t.Errorf("status = %v, want spawned", out["status"])
	}

	// Let background goroutine finish.
	time.Sleep(100 * time.Millisecond)
}
