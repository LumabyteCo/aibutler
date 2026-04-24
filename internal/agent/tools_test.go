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

func TestNewDelegateTool(t *testing.T) {
	cfg := agent.DelegateConfig{
		Model:   testutil.NewFakeModel(agent.Response{Content: "delegated result"}),
		Tools:   testutil.NewFakeToolExecutor(nil),
		Caps:    capability.NewCapabilitySet(capability.MessagingDefaults()),
		Timeout: 30 * time.Second,
	}

	name, desc, schema, cap, exec := agent.NewDelegateTool(cfg)

	if name != "agent.delegate" {
		t.Errorf("name = %q, want agent.delegate", name)
	}
	if desc == "" {
		t.Error("description should not be empty")
	}
	if schema == "" {
		t.Error("schema should not be empty")
	}
	if cap != "agent.delegate" {
		t.Errorf("capability = %q, want agent.delegate", cap)
	}

	// Execute with a simple task.
	result, err := exec(context.Background(), `{"task":"test delegation"}`)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}

	var out map[string]interface{}
	if err := json.Unmarshal([]byte(result), &out); err != nil {
		t.Fatalf("parse result: %v", err)
	}
	if out["status"] != "completed" {
		t.Errorf("status = %v, want completed", out["status"])
	}
	if out["output"] != "delegated result" {
		t.Errorf("output = %v, want 'delegated result'", out["output"])
	}
}

func TestDelegateToolEmptyTask(t *testing.T) {
	cfg := agent.DelegateConfig{
		Model: testutil.NewFakeModel(),
		Tools: testutil.NewFakeToolExecutor(nil),
	}
	_, _, _, _, exec := agent.NewDelegateTool(cfg)

	_, err := exec(context.Background(), `{"task":""}`)
	if err == nil {
		t.Error("expected error for empty task")
	}
}

func TestDelegateToolInvalidJSON(t *testing.T) {
	cfg := agent.DelegateConfig{
		Model: testutil.NewFakeModel(),
		Tools: testutil.NewFakeToolExecutor(nil),
	}
	_, _, _, _, exec := agent.NewDelegateTool(cfg)

	_, err := exec(context.Background(), `not json`)
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestDelegateToolTimeoutCap(t *testing.T) {
	cfg := agent.DelegateConfig{
		Model:   testutil.NewFakeModel(agent.Response{Content: "ok"}),
		Tools:   testutil.NewFakeToolExecutor(nil),
		Caps:    capability.NewCapabilitySet(nil),
		Timeout: 10 * time.Second, // Config max
	}
	_, _, _, _, exec := agent.NewDelegateTool(cfg)

	// Request 999s timeout — should be capped to 10s (config max).
	result, err := exec(context.Background(), `{"task":"test","timeout_seconds":999}`)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result == "" {
		t.Error("expected non-empty result")
	}
}

func TestNewSpawnTool(t *testing.T) {
	cfg := agent.DelegateConfig{
		Model:   testutil.NewFakeModel(agent.Response{Content: "bg result"}),
		Tools:   testutil.NewFakeToolExecutor(nil),
		Caps:    capability.NewCapabilitySet(nil),
		Timeout: 30 * time.Second,
	}

	name, desc, schema, cap, exec := agent.NewSpawnTool(cfg)

	if name != "agent.spawn" {
		t.Errorf("name = %q, want agent.spawn", name)
	}
	if desc == "" {
		t.Error("description should not be empty")
	}
	if schema == "" {
		t.Error("schema should not be empty")
	}
	if cap != "agent.delegate" {
		t.Errorf("capability = %q, want agent.delegate", cap)
	}

	// Execute — should return immediately with agent_id.
	result, err := exec(context.Background(), `{"task":"background work"}`)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}

	var out map[string]interface{}
	if err := json.Unmarshal([]byte(result), &out); err != nil {
		t.Fatalf("parse result: %v", err)
	}
	if out["status"] != "spawned" {
		t.Errorf("status = %v, want spawned", out["status"])
	}
	if out["agent_id"] == "" {
		t.Error("expected non-empty agent_id")
	}
}

func TestSpawnToolEmptyTask(t *testing.T) {
	cfg := agent.DelegateConfig{
		Model: testutil.NewFakeModel(),
		Tools: testutil.NewFakeToolExecutor(nil),
	}
	_, _, _, _, exec := agent.NewSpawnTool(cfg)

	_, err := exec(context.Background(), `{"task":""}`)
	if err == nil {
		t.Error("expected error for empty task")
	}
}
