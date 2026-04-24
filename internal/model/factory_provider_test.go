package model

import "testing"

func TestResolveProvider(t *testing.T) {
	tests := []struct {
		model    string
		expected string
	}{
		{"claude-sonnet-4-6", "anthropic"},
		{"claude-3-opus", "anthropic"},
		{"gpt-4o", "openai"},
		{"gpt-3.5-turbo", "openai"},
		{"gemini-2.0-flash", "gemini"},
		{"gemini-pro", "gemini"},
		{"grok-2", "xai"},
		{"grok-beta", "xai"},
		{"llama3", "local"},
		{"mistral-7b", "local"},
		{"", "local"},
	}

	for _, tt := range tests {
		got := resolveProvider(tt.model)
		if got != tt.expected {
			t.Errorf("resolveProvider(%q) = %q, want %q", tt.model, got, tt.expected)
		}
	}
}

func TestEstimateCost(t *testing.T) {
	// Verify cost estimation for all providers.
	tests := []struct {
		provider string
		in, out  int
		wantZero bool
	}{
		{"anthropic", 1000, 500, false},
		{"openai", 1000, 500, false},
		{"gemini", 1000, 500, false},
		{"xai", 1000, 500, false},
		{"local", 1000, 500, true},
	}

	for _, tt := range tests {
		cost := estimateCost(tt.provider, tt.in, tt.out)
		if tt.wantZero && cost != 0 {
			t.Errorf("estimateCost(%q, %d, %d) = %f, want 0", tt.provider, tt.in, tt.out, cost)
		}
		if !tt.wantZero && cost <= 0 {
			t.Errorf("estimateCost(%q, %d, %d) = %f, want > 0", tt.provider, tt.in, tt.out, cost)
		}
	}

	// Verify EstimateCostPublic delegates correctly.
	pub := EstimateCostPublic("anthropic", 1000, 500)
	priv := estimateCost("anthropic", 1000, 500)
	if pub != priv {
		t.Errorf("EstimateCostPublic = %f, estimateCost = %f, should be equal", pub, priv)
	}
}
