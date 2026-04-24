package marketplace

import (
	"context"
	"testing"
)

func seedRegistry(t *testing.T) *Registry {
	t.Helper()
	r := New(nil)
	entries := []PluginEntry{
		{Name: "telegram-bridge", Version: "1.0.0", Description: "Telegram channel bridge", Author: "alice", Category: "channel", Downloads: 500, Rating: 4.5, Verified: true, InstallURL: "https://example.com/telegram-bridge.wasm"},
		{Name: "openai-gpt4", Version: "2.1.0", Description: "OpenAI GPT-4 model adapter", Author: "bob", Category: "model", Downloads: 1200, Rating: 4.8, Verified: true, InstallURL: "https://example.com/openai-gpt4.wasm"},
		{Name: "hue-lights", Version: "0.9.0", Description: "Philips Hue smart light control", Author: "charlie", Category: "iot", Downloads: 300, Rating: 4.2, Verified: false, InstallURL: "https://example.com/hue-lights.wasm"},
		{Name: "web-search", Version: "1.5.0", Description: "Web search tool plugin", Author: "dave", Category: "tool", Downloads: 800, Rating: 4.6, Verified: true, InstallURL: "https://example.com/web-search.wasm"},
	}
	ctx := context.Background()
	for _, e := range entries {
		if err := r.Submit(ctx, e); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	return r
}

func TestMarketplaceSearch(t *testing.T) {
	r := seedRegistry(t)
	ctx := context.Background()

	results, err := r.Search(ctx, "telegram")
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Name != "telegram-bridge" {
		t.Errorf("expected telegram-bridge, got %s", results[0].Name)
	}

	// Search by category keyword.
	results, err = r.Search(ctx, "model")
	if err != nil {
		t.Fatalf("search model: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 model result, got %d", len(results))
	}
}

func TestMarketplaceListByCategory(t *testing.T) {
	r := seedRegistry(t)
	ctx := context.Background()

	results, err := r.List(ctx, "iot", 10)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 iot result, got %d", len(results))
	}
	if results[0].Name != "hue-lights" {
		t.Errorf("expected hue-lights, got %s", results[0].Name)
	}

	// List all (no category filter).
	all, err := r.List(ctx, "", 0)
	if err != nil {
		t.Fatalf("list all: %v", err)
	}
	if len(all) != 4 {
		t.Errorf("expected 4 total, got %d", len(all))
	}
}

func TestMarketplaceSubmit(t *testing.T) {
	r := New(nil)
	ctx := context.Background()

	entry := PluginEntry{
		Name:        "test-plugin",
		Version:     "0.1.0",
		Description: "A test plugin",
		Author:      "tester",
		Category:    "tool",
	}
	if err := r.Submit(ctx, entry); err != nil {
		t.Fatalf("submit: %v", err)
	}
	if r.Count() != 1 {
		t.Errorf("expected count 1, got %d", r.Count())
	}

	// Update existing.
	entry.Version = "0.2.0"
	if err := r.Submit(ctx, entry); err != nil {
		t.Fatalf("submit update: %v", err)
	}
	if r.Count() != 1 {
		t.Errorf("expected count still 1 after update, got %d", r.Count())
	}

	got, err := r.GetByName(ctx, "test-plugin")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Version != "0.2.0" {
		t.Errorf("expected version 0.2.0, got %s", got.Version)
	}

	// Submit with empty name should fail.
	if err := r.Submit(ctx, PluginEntry{}); err == nil {
		t.Error("expected error for empty name, got nil")
	}
}

func TestMarketplaceGetByName(t *testing.T) {
	r := seedRegistry(t)
	ctx := context.Background()

	entry, err := r.GetByName(ctx, "web-search")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if entry.Author != "dave" {
		t.Errorf("expected author dave, got %s", entry.Author)
	}

	// Not found.
	_, err = r.GetByName(ctx, "nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent plugin, got nil")
	}
}
