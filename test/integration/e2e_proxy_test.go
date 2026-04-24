//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/LumabyteCo/aibutler/internal/agent"
	"github.com/LumabyteCo/aibutler/internal/capability"
	"github.com/LumabyteCo/aibutler/internal/vault"
)

// proxyCaps returns a capability set with web proxy capabilities.
func proxyCaps() *capability.CapabilitySet {
	caps := capability.MessagingDefaults()
	return capability.NewCapabilitySet(caps)
}

// TestE2EWebFetch verifies the web.fetch tool makes a GET request and returns the response.
func TestE2EWebFetch(t *testing.T) {
	p := setupPipelineWithOpts(t, pipelineOpts{
		WithProxy: true,
		Responses: []agent.Response{
			toolCallResponse("Fetching data.",
				tc("wf1", "web.fetch", `{"url":"https://example.com/api/data"}`),
			),
			finalResponse("Got the data."),
		},
	})

	// Configure the proxy mux to handle the request.
	p.ProxyMux.HandleFunc("/api/data", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		w.Write([]byte(`{"items":[1,2,3]}`))
	})

	// Proxy tools need caps in context.
	ctx := capability.WithCaps(context.Background(), proxyCaps())
	p.sendMsgWithContext(t, ctx, "Fetch some data")

	// Verify model was called twice (tool call + final).
	if p.Fake.CallCount() != 2 {
		t.Fatalf("model calls = %d, want 2", p.Fake.CallCount())
	}

	// Verify the tool result was fed back to the model.
	// The fetchTool wraps the body in JSON: {"status_code":200,"body":"{\"items\":[1,2,3]}"}
	// so check for "items" which appears regardless of escaping.
	calls := p.Fake.Calls()
	found := false
	for _, msg := range calls[1] {
		if msg.Role == "tool" && strings.Contains(msg.Content, "items") {
			found = true
			break
		}
	}
	if !found {
		t.Error("tool result should contain the fetched JSON data")
	}

	// Verify final response.
	resp := p.lastResponse(t)
	if resp != "Got the data." {
		t.Errorf("response = %q", resp)
	}
}

// TestE2EWebFetchPost verifies web.fetch with POST method and body.
func TestE2EWebFetchPost(t *testing.T) {
	p := setupPipelineWithOpts(t, pipelineOpts{
		WithProxy: true,
		Responses: []agent.Response{
			toolCallResponse("Posting data.",
				tc("wf2", "web.fetch", `{"url":"https://example.com/api/submit","method":"POST","body":"{\"name\":\"test\"}"}`),
			),
			finalResponse("Data submitted."),
		},
	})

	var receivedMethod string
	var receivedBody string
	p.ProxyMux.HandleFunc("/api/submit", func(w http.ResponseWriter, r *http.Request) {
		receivedMethod = r.Method
		buf := make([]byte, 1024)
		n, _ := r.Body.Read(buf)
		receivedBody = string(buf[:n])
		w.WriteHeader(200)
		w.Write([]byte(`{"status":"ok"}`))
	})

	ctx := capability.WithCaps(context.Background(), proxyCaps())
	p.sendMsgWithContext(t, ctx, "Submit data")

	if receivedMethod != "POST" {
		t.Errorf("method = %q, want POST", receivedMethod)
	}
	if !strings.Contains(receivedBody, `"name":"test"`) {
		t.Errorf("body = %q, want to contain name:test", receivedBody)
	}

	resp := p.lastResponse(t)
	if resp != "Data submitted." {
		t.Errorf("response = %q", resp)
	}
}

// TestE2EWebFetchWithCredentials verifies that stored credentials are injected as auth headers.
func TestE2EWebFetchWithCredentials(t *testing.T) {
	p := setupPipelineWithOpts(t, pipelineOpts{
		WithProxy: true,
		Responses: []agent.Response{
			toolCallResponse("Fetching with auth.",
				tc("wf3", "web.fetch", `{"url":"https://api.openai.com/v1/models"}`),
			),
			finalResponse("Authenticated fetch complete."),
		},
	})

	// Store a credential for openai in the vault (openai is in default service registry).
	err := p.FakeVault.Store(context.Background(), vault.Credential{
		Key:   "openai",
		Type:  vault.CredAPIKey,
		Value: []byte("sk-test-key-12345"),
	})
	if err != nil {
		t.Fatalf("store credential: %v", err)
	}

	var receivedAuth string
	p.ProxyMux.HandleFunc("/v1/models", func(w http.ResponseWriter, r *http.Request) {
		receivedAuth = r.Header.Get("Authorization")
		w.WriteHeader(200)
		w.Write([]byte(`{"models":["gpt-4"]}`))
	})

	ctx := capability.WithCaps(context.Background(), proxyCaps())
	p.sendMsgWithContext(t, ctx, "List OpenAI models")

	// Verify the Authorization header was injected.
	if receivedAuth != "Bearer sk-test-key-12345" {
		t.Errorf("Authorization = %q, want 'Bearer sk-test-key-12345'", receivedAuth)
	}

	resp := p.lastResponse(t)
	if resp != "Authenticated fetch complete." {
		t.Errorf("response = %q", resp)
	}
}

// TestE2EWebSearch verifies the web.search tool sends a search query to the Tavily API.
func TestE2EWebSearch(t *testing.T) {
	p := setupPipelineWithOpts(t, pipelineOpts{
		WithProxy: true,
		Responses: []agent.Response{
			toolCallResponse("Searching.",
				tc("ws1", "web.search", `{"query":"Go programming language","count":3}`),
			),
			finalResponse("Found search results."),
		},
	})

	// web.search hits api.tavily.com which is in the default ServiceRegistry.
	// We must store a credential for "tavily" so the resolver doesn't error.
	_ = p.FakeVault.Store(context.Background(), vault.Credential{
		Key:   "tavily",
		Type:  vault.CredAPIKey,
		Value: []byte("tvly-test-key"),
	})

	var receivedQuery string
	p.ProxyMux.HandleFunc("/search", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]interface{}
		json.NewDecoder(r.Body).Decode(&body)
		if q, ok := body["query"].(string); ok {
			receivedQuery = q
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"results":[{"title":"Go Lang","url":"https://go.dev","content":"Go programming language"}]}`))
	})

	ctx := capability.WithCaps(context.Background(), proxyCaps())
	p.sendMsgWithContext(t, ctx, "Search for Go programming")

	if receivedQuery != "Go programming language" {
		t.Errorf("query = %q, want 'Go programming language'", receivedQuery)
	}

	// Verify tool result was fed back.
	calls := p.Fake.Calls()
	found := false
	for _, msg := range calls[1] {
		if msg.Role == "tool" && strings.Contains(msg.Content, "Go Lang") {
			found = true
			break
		}
	}
	if !found {
		t.Error("search tool result should contain 'Go Lang'")
	}

	resp := p.lastResponse(t)
	if resp != "Found search results." {
		t.Errorf("response = %q", resp)
	}
}

// TestE2EWebFetchNoCaps verifies that web.fetch fails gracefully when the
// caller has NOT been granted the tool.web.fetch capability.
func TestE2EWebFetchNoCaps(t *testing.T) {
	// Provide an empty capability set — no tool.web.fetch grant. The
	// dispatcher's capability check should reject the call before it reaches
	// the fetch tool.
	emptyCaps := capability.NewCapabilitySet(nil)

	p := setupPipelineWithOpts(t, pipelineOpts{
		WithProxy:   true,
		CapOverride: emptyCaps,
		Responses: []agent.Response{
			toolCallResponse("Fetching without caps.",
				tc("wf4", "web.fetch", `{"url":"https://example.com/api/data"}`),
			),
			// After error, model responds gracefully.
			finalResponse("I cannot access the web right now."),
		},
	})

	p.ProxyMux.HandleFunc("/api/data", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"data":"should not reach here"}`))
	})

	p.sendMsg(t, "Fetch data")

	// The tool should fail with a capability-denied error naming tool.web.fetch.
	calls := p.Fake.Calls()
	if len(calls) < 2 {
		t.Fatalf("model calls = %d, want >= 2", len(calls))
	}

	found := false
	for _, msg := range calls[1] {
		if msg.Role == "tool" && strings.Contains(msg.Content, "tool.web.fetch") && strings.Contains(msg.Content, "no_capability") {
			found = true
			break
		}
	}
	if !found {
		t.Error("tool result should contain a capability-denied error for 'tool.web.fetch' with reason 'no_capability'")
	}

	resp := p.lastResponse(t)
	if resp != "I cannot access the web right now." {
		t.Errorf("response = %q", resp)
	}
}
