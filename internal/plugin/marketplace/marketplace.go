// Package marketplace provides a plugin registry for discovering and sharing Butler plugins.
package marketplace

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// PluginEntry describes a plugin available in the marketplace.
type PluginEntry struct {
	Name        string  `json:"name"`
	Version     string  `json:"version"`
	Description string  `json:"description"`
	Author      string  `json:"author"`
	Category    string  `json:"category"` // "channel", "tool", "model", "iot", "ai-service"
	Downloads   int     `json:"downloads"`
	Rating      float64 `json:"rating"`
	Verified    bool    `json:"verified"`
	InstallURL  string  `json:"install_url"`
}

// Registry manages the plugin marketplace catalog.
type Registry struct {
	entries []PluginEntry
	db      *sql.DB
}

// New creates a new marketplace Registry.
func New(db *sql.DB) *Registry {
	return &Registry{
		db: db,
	}
}

// Search returns entries matching the query string (case-insensitive substring match
// on name, description, and category).
func (r *Registry) Search(_ context.Context, query string) ([]PluginEntry, error) {
	if query == "" {
		return nil, nil
	}
	lower := strings.ToLower(query)
	var results []PluginEntry
	for _, e := range r.entries {
		if strings.Contains(strings.ToLower(e.Name), lower) ||
			strings.Contains(strings.ToLower(e.Description), lower) ||
			strings.Contains(strings.ToLower(e.Category), lower) {
			results = append(results, e)
		}
	}
	return results, nil
}

// GetByName returns a single entry by exact name, or nil if not found.
func (r *Registry) GetByName(_ context.Context, name string) (*PluginEntry, error) {
	for i, e := range r.entries {
		if e.Name == name {
			return &r.entries[i], nil
		}
	}
	return nil, fmt.Errorf("marketplace: plugin %q not found", name)
}

// Submit adds or updates a plugin entry in the marketplace.
func (r *Registry) Submit(_ context.Context, entry PluginEntry) error {
	if entry.Name == "" {
		return fmt.Errorf("marketplace: plugin name is required")
	}
	if entry.Version == "" {
		return fmt.Errorf("marketplace: plugin version is required")
	}
	// Update existing entry if present.
	for i, e := range r.entries {
		if e.Name == entry.Name {
			r.entries[i] = entry
			return nil
		}
	}
	r.entries = append(r.entries, entry)
	return nil
}

// List returns entries filtered by category (empty category returns all).
// Results are limited to the given count (0 = unlimited).
func (r *Registry) List(_ context.Context, category string, limit int) ([]PluginEntry, error) {
	var results []PluginEntry
	for _, e := range r.entries {
		if category != "" && e.Category != category {
			continue
		}
		results = append(results, e)
		if limit > 0 && len(results) >= limit {
			break
		}
	}
	return results, nil
}

// Count returns the total number of entries in the marketplace.
func (r *Registry) Count() int {
	return len(r.entries)
}

// toolRegistry is a narrow interface for tool registration.
type toolRegistry interface {
	Register(name, desc, schema, cap string, exec func(ctx context.Context, input string) (string, error))
}

// RegisterMarketplaceTools registers marketplace tools in the tool registry.
func RegisterMarketplaceTools(registry toolRegistry, mr *Registry) {
	registry.Register(
		"plugin.marketplace.search",
		"Search the plugin marketplace for available plugins.",
		`{"type":"object","properties":{"query":{"type":"string","description":"Search query"}},"required":["query"]}`,
		"tool.plugin.read",
		func(ctx context.Context, input string) (string, error) {
			var args struct {
				Query string `json:"query"`
			}
			if err := json.Unmarshal([]byte(input), &args); err != nil {
				return "", fmt.Errorf("invalid input: %w", err)
			}
			results, err := mr.Search(ctx, args.Query)
			if err != nil {
				return "", err
			}
			out, _ := json.Marshal(results)
			return string(out), nil
		},
	)

	registry.Register(
		"plugin.marketplace.install",
		"Install a plugin from the marketplace by name.",
		`{"type":"object","properties":{"name":{"type":"string","description":"Plugin name to install"}},"required":["name"]}`,
		"tool.plugin.write",
		func(ctx context.Context, input string) (string, error) {
			var args struct {
				Name string `json:"name"`
			}
			if err := json.Unmarshal([]byte(input), &args); err != nil {
				return "", fmt.Errorf("invalid input: %w", err)
			}
			entry, err := mr.GetByName(ctx, args.Name)
			if err != nil {
				return "", err
			}
			result := map[string]interface{}{
				"status":      "ready_to_install",
				"name":        entry.Name,
				"version":     entry.Version,
				"install_url": entry.InstallURL,
				"timestamp":   time.Now().UTC().Format(time.RFC3339),
			}
			out, _ := json.Marshal(result)
			return string(out), nil
		},
	)
}
