//go:build integration

package integration

import (
	"strings"
	"testing"

	"github.com/LumabyteCo/aibutler/internal/agent"
)

// TestE2EMemoryCapture verifies that memory.capture stores a thought in
// captured_thoughts AND auto-extracts key facts into key_facts.
func TestE2EMemoryCapture(t *testing.T) {
	p := setupPipelineWithOpts(t, pipelineOpts{
		WithMemory: true,
		Responses: []agent.Response{
			// Turn 1: model calls memory.capture
			toolCallResponse("Capturing your preference.",
				tc("tc1", "memory.capture", `{"content":"I prefer dark mode","tags":["preference"]}`),
			),
			// Turn 1 continued: model sees tool result, sends final reply
			finalResponse("Got it! I've noted that you prefer dark mode."),
		},
	})

	p.sendMsg(t, "Remember that I prefer dark mode")

	// Verify final response reached the channel.
	resp := p.lastResponse(t)
	if !strings.Contains(resp, "dark mode") {
		t.Errorf("response = %q, want mention of dark mode", resp)
	}

	// Verify the thought was persisted in captured_thoughts.
	thoughtCount := p.countRows(t, "captured_thoughts")
	if thoughtCount != 1 {
		t.Fatalf("captured_thoughts rows = %d, want 1", thoughtCount)
	}

	content := p.querySingleString(t, "SELECT content FROM captured_thoughts LIMIT 1")
	if content != "I prefer dark mode" {
		t.Errorf("thought content = %q, want 'I prefer dark mode'", content)
	}

	// Verify key facts were auto-extracted.
	// "I prefer dark mode" matches the "I prefer ..." extraction rule → category=preference.
	factCount := p.countRows(t, "key_facts")
	if factCount == 0 {
		t.Fatal("key_facts rows = 0, want >= 1 (auto-extraction should have fired)")
	}

	fact := p.querySingleString(t, "SELECT fact FROM key_facts LIMIT 1")
	if !strings.Contains(strings.ToLower(fact), "dark mode") {
		t.Errorf("extracted fact = %q, want it to mention dark mode", fact)
	}
}

// TestE2EMemorySearch captures 2 thoughts and then searches by query.
// The search result should contain the matching thought.
func TestE2EMemorySearch(t *testing.T) {
	p := setupPipelineWithOpts(t, pipelineOpts{
		WithMemory: true,
		Responses: []agent.Response{
			// Turn 1: capture first thought
			toolCallResponse("Capturing thought 1.",
				tc("tc1", "memory.capture", `{"content":"I prefer dark mode","tags":["preference"]}`),
			),
			finalResponse("Noted your dark mode preference."),

			// Turn 2: capture second thought
			toolCallResponse("Capturing thought 2.",
				tc("tc2", "memory.capture", `{"content":"Meeting with team at 3pm","tags":["schedule"]}`),
			),
			finalResponse("Noted your meeting."),

			// Turn 3: search for "dark"
			toolCallResponse("Searching memory.",
				tc("tc3", "memory.search", `{"query":"dark"}`),
			),
			finalResponse("Found your note about dark mode preference."),
		},
	})

	// Turn 1: capture first thought.
	p.sendMsg(t, "Remember I prefer dark mode")
	if p.countRows(t, "captured_thoughts") != 1 {
		t.Fatal("expected 1 thought after first capture")
	}

	// Turn 2: capture second thought.
	p.sendMsg(t, "Remember meeting at 3pm")
	if p.countRows(t, "captured_thoughts") != 2 {
		t.Fatal("expected 2 thoughts after second capture")
	}

	// Turn 3: search.
	p.sendMsg(t, "What did I say about dark?")

	resp := p.lastResponse(t)
	if !strings.Contains(resp, "dark mode") {
		t.Errorf("search response = %q, want mention of dark mode", resp)
	}

	// Model should have been called 6 times (2 per turn: tool call + final).
	if p.Fake.CallCount() != 6 {
		t.Errorf("model calls = %d, want 6", p.Fake.CallCount())
	}
}

// TestE2EMemoryFacts captures a thought with identity info, then uses
// memory.facts to verify the extracted facts are retrievable.
func TestE2EMemoryFacts(t *testing.T) {
	p := setupPipelineWithOpts(t, pipelineOpts{
		WithMemory: true,
		Responses: []agent.Response{
			// Turn 1: capture thought with identity info
			toolCallResponse("Capturing your info.",
				tc("tc1", "memory.capture", `{"content":"My name is Alex and I live in London","tags":["identity"]}`),
			),
			finalResponse("Got it! I've noted your name and location."),

			// Turn 2: retrieve facts
			toolCallResponse("Looking up facts.",
				tc("tc2", "memory.facts", `{}`),
			),
			finalResponse("Here are the facts I know: your name is Alex and you live in London."),
		},
	})

	// Turn 1: capture the thought.
	p.sendMsg(t, "My name is Alex and I live in London")

	// Verify key facts were extracted.
	factCount := p.countRows(t, "key_facts")
	if factCount < 2 {
		t.Fatalf("key_facts rows = %d, want >= 2 (name + location)", factCount)
	}

	// Turn 2: retrieve facts via memory.facts tool.
	p.sendMsg(t, "What do you know about me?")

	resp := p.lastResponse(t)
	if !strings.Contains(resp, "Alex") {
		t.Errorf("facts response = %q, want mention of Alex", resp)
	}
	if !strings.Contains(resp, "London") {
		t.Errorf("facts response = %q, want mention of London", resp)
	}

	// 4 model calls total (2 per turn).
	if p.Fake.CallCount() != 4 {
		t.Errorf("model calls = %d, want 4", p.Fake.CallCount())
	}
}
