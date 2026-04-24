package router

import (
	"context"
	"testing"

	"github.com/LumabyteCo/aibutler/internal/agent"
)

// mockModel implements agent.ModelAdapter for LLM fallback tests.
type mockModel struct {
	response string
}

func (m *mockModel) Complete(_ context.Context, _ []agent.Message) (agent.Response, error) {
	return agent.Response{Content: m.response}, nil
}

var testRoutes = []Route{
	{AgentName: "coding", Description: "Code editing, shell, git operations", Keywords: []string{"code", "git", "programming", "debug"}},
	{AgentName: "home", Description: "Smart home, IoT, scheduling", Keywords: []string{"lights", "thermostat", "schedule", "alarm"}},
	{AgentName: "creative", Description: "AI image, video, music generation", Keywords: []string{"image", "video", "music", "art"}},
	{AgentName: "research", Description: "Web search, memory, knowledge", Keywords: []string{"search", "find", "lookup", "research"}},
	{AgentName: "general", Description: "General assistant, tasks, contacts", Keywords: []string{}},
}

func TestKeywordMatch(t *testing.T) {
	r := New(testRoutes, nil, nil)

	tests := []struct {
		message string
		want    string
	}{
		{"Can you help me debug this code?", "coding"},
		{"Turn on the lights in the living room", "home"},
		{"Generate an image of a sunset", "creative"},
		{"Search for information about Go generics", "research"},
	}

	for _, tt := range tests {
		got, err := r.Route(context.Background(), tt.message)
		if err != nil {
			t.Fatalf("Route(%q) error: %v", tt.message, err)
		}
		if got != tt.want {
			t.Errorf("Route(%q) = %q, want %q", tt.message, got, tt.want)
		}
	}
}

func TestExplicitHandoff(t *testing.T) {
	r := New(testRoutes, nil, nil)

	tests := []struct {
		message string
		want    string
	}{
		{"ask the coding agent to fix this bug", "coding"},
		{"have the home agent turn on the lights", "home"},
		{"send to research agent please", "research"},
		{"use the creative agent for this", "creative"},
	}

	for _, tt := range tests {
		got, err := r.Route(context.Background(), tt.message)
		if err != nil {
			t.Fatalf("Route(%q) error: %v", tt.message, err)
		}
		if got != tt.want {
			t.Errorf("Route(%q) = %q, want %q", tt.message, got, tt.want)
		}
	}
}

func TestLLMFallback(t *testing.T) {
	model := &mockModel{response: "coding"}
	r := New(testRoutes, model, nil)

	// A message with no keyword match should fall back to LLM.
	got, err := r.Route(context.Background(), "refactor the widget class")
	if err != nil {
		t.Fatalf("Route error: %v", err)
	}
	if got != "coding" {
		t.Errorf("Route = %q, want 'coding'", got)
	}
}

func TestNoMatchDefaultsToGeneral(t *testing.T) {
	r := New(testRoutes, nil, nil)

	// A message with no keyword matches and no LLM should return "general".
	got, err := r.Route(context.Background(), "hello there")
	if err != nil {
		t.Fatalf("Route error: %v", err)
	}
	if got != "general" {
		t.Errorf("Route = %q, want 'general'", got)
	}
}

func TestEmptyRoutes(t *testing.T) {
	r := New(nil, nil, nil)

	got, err := r.Route(context.Background(), "anything")
	if err != nil {
		t.Fatalf("Route error: %v", err)
	}
	if got != "general" {
		t.Errorf("Route = %q, want 'general'", got)
	}
}
