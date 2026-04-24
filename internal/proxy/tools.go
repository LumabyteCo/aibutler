package proxy

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/LumabyteCo/aibutler/internal/capability"
	"github.com/LumabyteCo/aibutler/internal/tool"
)

// RegisterProxyTools registers all proxy tools in the tool registry.
func RegisterProxyTools(registry *tool.Registry, proxy *Proxy) {
	registry.Register(&fetchTool{proxy: proxy})
	registry.Register(&webSearchTool{proxy: proxy})
}

// --- web.fetch ---

type fetchTool struct {
	proxy *Proxy
}

func (t *fetchTool) Name() string        { return "web.fetch" }
func (t *fetchTool) Description() string { return "Fetch a URL with automatic credential injection" }
func (t *fetchTool) Capability() string  { return "tool.web.fetch" }
func (t *fetchTool) Schema() string {
	return `{"type":"object","properties":{"url":{"type":"string","description":"URL to fetch"},"method":{"type":"string","description":"HTTP method (default GET)"},"headers":{"type":"object","description":"Additional headers"},"body":{"type":"string","description":"Request body"}},"required":["url"]}`
}

func (t *fetchTool) Execute(ctx context.Context, input string) (string, error) {
	var args struct {
		URL     string            `json:"url"`
		Method  string            `json:"method"`
		Headers map[string]string `json:"headers"`
		Body    string            `json:"body"`
	}
	if err := json.Unmarshal([]byte(input), &args); err != nil {
		return "", fmt.Errorf("web.fetch: invalid input: %w", err)
	}
	if args.URL == "" {
		return "", fmt.Errorf("web.fetch: url is required")
	}
	if args.Method == "" {
		args.Method = "GET"
	}

	caps := capability.CapsFromContext(ctx)
	if caps == nil {
		return "", fmt.Errorf("web.fetch: no capabilities in context")
	}

	resp, err := t.proxy.AccessResource(ctx, caps, AccessRequest{
		Method:  args.Method,
		URL:     args.URL,
		Headers: args.Headers,
		Body:    []byte(args.Body),
	})
	if err != nil {
		return "", err
	}

	result := struct {
		StatusCode int               `json:"status_code"`
		Headers    map[string]string `json:"headers"`
		Body       string            `json:"body"`
	}{
		StatusCode: resp.StatusCode,
		Headers:    resp.Headers,
		Body:       string(resp.Body),
	}
	data, _ := json.Marshal(result)
	return string(data), nil
}

// --- web.search ---

type webSearchTool struct {
	proxy *Proxy
}

func (t *webSearchTool) Name() string        { return "web.search" }
func (t *webSearchTool) Description() string { return "Search the web using configured search API" }
func (t *webSearchTool) Capability() string  { return "tool.web.search" }
func (t *webSearchTool) Schema() string {
	return `{"type":"object","properties":{"query":{"type":"string","description":"Search query"},"count":{"type":"integer","description":"Number of results (default 5)"}},"required":["query"]}`
}

func (t *webSearchTool) Execute(ctx context.Context, input string) (string, error) {
	var args struct {
		Query string `json:"query"`
		Count int    `json:"count"`
	}
	if err := json.Unmarshal([]byte(input), &args); err != nil {
		return "", fmt.Errorf("web.search: invalid input: %w", err)
	}
	if args.Query == "" {
		return "", fmt.Errorf("web.search: query is required")
	}
	if args.Count == 0 {
		args.Count = 5
	}

	caps := capability.CapsFromContext(ctx)
	if caps == nil {
		return "", fmt.Errorf("web.search: no capabilities in context")
	}

	// Build search API request (Tavily format).
	searchBody, _ := json.Marshal(map[string]interface{}{
		"query":       args.Query,
		"max_results": args.Count,
	})

	resp, err := t.proxy.AccessResource(ctx, caps, AccessRequest{
		Method:  "POST",
		URL:     "https://api.tavily.com/search",
		Headers: map[string]string{"Content-Type": "application/json"},
		Body:    searchBody,
	})
	if err != nil {
		return "", err
	}

	return string(resp.Body), nil
}
