package model_test

import (
	"testing"

	"github.com/LumabyteCo/aibutler/internal/model"
)

// TestResolveProviderForCost guards the v0.2.1 fix (B3): models served from
// Ollama Cloud are PAID per token but their names (glm-*, kimi-*, …) don't
// match any known cloud prefix, so they fell through to "local" ($0.00) and
// the Spending panel showed a full day of paid usage as free.
func TestResolveProviderForCost(t *testing.T) {
	tests := []struct {
		name      string
		model     string
		baseURL   string
		want      string
	}{
		{"claude routes by name", "claude-sonnet-4-6", "", "anthropic"},
		{"gpt routes by name", "gpt-4o", "", "openai"},
		{"gemini routes by name", "gemini-2.0-flash", "", "gemini"},
		{"grok routes by name", "grok-2", "", "xai"},

		{"ollama cloud by https URL", "glm-5.3", "https://ollama.com", "ollama_cloud"},
		{"ollama cloud by full path", "glm-5.3", "https://ollama.com/v1/chat/completions", "ollama_cloud"},
		{"ollama cloud mixed case", "glm-5.1", "https://Ollama.com", "ollama_cloud"},

		{"local ollama stays free", "llama3", "http://localhost:11434/v1/chat/completions", "local"},
		{"lm studio stays free", "qwen", "http://localhost:1234/v1/chat/completions", "local"},
		{"lan endpoint stays free", "llama3", "http://192.168.1.50:11434/v1/chat/completions", "local"},
		{"empty URL stays free", "llama3", "", "local"},

		// Regression: prefix stripping happens upstream in cmd_run; the
		// router must still classify ollama/ names pointing at the cloud.
		{"prefixed cloud name", "ollama/glm-5.3", "https://ollama.com", "ollama_cloud"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := model.ResolveProviderForCost(tt.model, tt.baseURL)
			if got != tt.want {
				t.Errorf("ResolveProviderForCost(%q, %q) = %q, want %q", tt.model, tt.baseURL, got, tt.want)
			}
		})
	}
}

// TestEstimateCostOllamaCloud ensures the ollama_cloud provider maps to a
// non-zero rate — a paid provider with a $0 rate is the exact bug (B3).
func TestEstimateCostOllamaCloud(t *testing.T) {
	cost := model.EstimateCostPublic("ollama_cloud", 100_000, 10_000)
	if cost <= 0 {
		t.Errorf("ollama_cloud cost = %f, want > 0 (paid provider must not price at $0)", cost)
	}
	// Sanity: local remains free.
	if cost := model.EstimateCostPublic("local", 100_000, 10_000); cost != 0 {
		t.Errorf("local cost = %f, want 0", cost)
	}
}