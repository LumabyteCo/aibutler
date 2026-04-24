package testutil

import (
	"context"
	"errors"
	"sync"

	"github.com/LumabyteCo/aibutler/internal/agent"
	"github.com/LumabyteCo/aibutler/internal/capability"
)

// FakeModel is a canned LLM for testing.
type FakeModel struct {
	mu        sync.Mutex
	responses []agent.Response
	calls     [][]agent.Message
	callIndex int
	err       error // If set, all Complete calls return this error
}

// NewFakeModel creates a fake model with the given canned responses.
// Responses are returned in order. After exhaustion, returns an error.
func NewFakeModel(responses ...agent.Response) *FakeModel {
	return &FakeModel{responses: responses}
}

// SetError makes all subsequent Complete calls return the given error.
func (m *FakeModel) SetError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.err = err
}

// Complete returns the next canned response.
func (m *FakeModel) Complete(_ context.Context, messages []agent.Message) (agent.Response, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.calls = append(m.calls, messages)

	if m.err != nil {
		return agent.Response{}, m.err
	}

	if m.callIndex >= len(m.responses) {
		return agent.Response{}, errors.New("fakemodel: no more canned responses")
	}

	resp := m.responses[m.callIndex]
	m.callIndex++
	return resp, nil
}

// CallCount returns how many times Complete was called.
func (m *FakeModel) CallCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.calls)
}

// AddResponses appends additional canned responses to the queue.
func (m *FakeModel) AddResponses(responses ...agent.Response) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.responses = append(m.responses, responses...)
}

// Calls returns all recorded message histories.
func (m *FakeModel) Calls() [][]agent.Message {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := make([][]agent.Message, len(m.calls))
	copy(cp, m.calls)
	return cp
}

// FakeToolExecutor is a canned tool executor for testing.
type FakeToolExecutor struct {
	mu      sync.Mutex
	results map[string]string
	calls   []agent.ToolCall
	err     error
}

// NewFakeToolExecutor creates a fake executor with canned results keyed by tool name.
func NewFakeToolExecutor(results map[string]string) *FakeToolExecutor {
	return &FakeToolExecutor{results: results}
}

// Execute returns the canned result for the given tool.
func (e *FakeToolExecutor) Execute(_ context.Context, call agent.ToolCall) (string, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.calls = append(e.calls, call)
	if e.err != nil {
		return "", e.err
	}
	result, ok := e.results[call.Name]
	if !ok {
		return "unknown tool: " + call.Name, nil
	}
	return result, nil
}

// AvailableTools returns a fixed set of tool definitions.
func (e *FakeToolExecutor) AvailableTools(_ context.Context, _ agent.Mode, _ *capability.CapabilitySet) []agent.ToolDef {
	defs := make([]agent.ToolDef, 0, len(e.results))
	for name := range e.results {
		defs = append(defs, agent.ToolDef{Name: name})
	}
	return defs
}

// ToolCalls returns all recorded tool calls.
func (e *FakeToolExecutor) ToolCalls() []agent.ToolCall {
	e.mu.Lock()
	defer e.mu.Unlock()
	cp := make([]agent.ToolCall, len(e.calls))
	copy(cp, e.calls)
	return cp
}

// FakeStreamingModel implements both ModelAdapter and StreamingModelAdapter for testing.
type FakeStreamingModel struct {
	FakeModel
	mu         sync.Mutex
	streamEvts [][]agent.StreamEvent
	streamIdx  int
	streamErr  error
}

// NewFakeStreamingModel creates a fake streaming model with canned responses and stream events.
func NewFakeStreamingModel(responses []agent.Response, streamEvents [][]agent.StreamEvent) *FakeStreamingModel {
	m := &FakeStreamingModel{
		streamEvts: streamEvents,
	}
	m.FakeModel.responses = responses
	return m
}

// SetStreamError makes subsequent CompleteStream calls return this error.
func (m *FakeStreamingModel) SetStreamError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.streamErr = err
}

// CompleteStream returns the next canned stream event sequence as a channel.
func (m *FakeStreamingModel) CompleteStream(_ context.Context, _ []agent.Message) (<-chan agent.StreamEvent, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.streamErr != nil {
		return nil, m.streamErr
	}

	if m.streamIdx >= len(m.streamEvts) {
		return nil, errors.New("fakestreamingmodel: no more canned stream events")
	}

	events := m.streamEvts[m.streamIdx]
	m.streamIdx++

	ch := make(chan agent.StreamEvent, len(events))
	for _, e := range events {
		ch <- e
	}
	close(ch)
	return ch, nil
}
