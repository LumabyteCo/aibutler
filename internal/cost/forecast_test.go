package cost

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

type mockRegistry struct {
	tools []string
	exec  map[string]func(ctx context.Context, input string) (string, error)
}

func newMockRegistry() *mockRegistry {
	return &mockRegistry{exec: make(map[string]func(ctx context.Context, input string) (string, error))}
}

func (m *mockRegistry) Register(name, _, _, _ string, exec func(ctx context.Context, input string) (string, error)) {
	m.tools = append(m.tools, name)
	m.exec[name] = exec
}

func TestForecast_Sonnet_KnownPrefix(t *testing.T) {
	f := NewForecaster()
	got := f.ForecastFromTokens("claude-sonnet-4-5", 1_000_000, 500_000)
	if got.PriceSource != SourceTable {
		t.Errorf("expected PriceSource=table, got %q", got.PriceSource)
	}
	// Sonnet starter price is $3/M input + $15/M output.
	wantInputCost := 3.0   // 1M tokens × $3
	wantOutputCost := 7.5  // 0.5M × $15
	wantTotal := 10.5
	if got.InputCostUSD != wantInputCost {
		t.Errorf("InputCostUSD = %v, want %v", got.InputCostUSD, wantInputCost)
	}
	if got.OutputCostUSD != wantOutputCost {
		t.Errorf("OutputCostUSD = %v, want %v", got.OutputCostUSD, wantOutputCost)
	}
	if got.TotalCostUSD != wantTotal {
		t.Errorf("TotalCostUSD = %v, want %v", got.TotalCostUSD, wantTotal)
	}
}

func TestForecast_Ollama_FreeLocal(t *testing.T) {
	f := NewForecaster()
	got := f.ForecastFromTokens("ollama:llama3.2", 100_000, 50_000)
	if got.PriceSource != SourceFree {
		t.Errorf("expected PriceSource=free-local, got %q", got.PriceSource)
	}
	if got.TotalCostUSD != 0 {
		t.Errorf("free-local should be $0, got %v", got.TotalCostUSD)
	}
	if !strings.Contains(got.Note, "local") {
		t.Errorf("expected note to mention local, got %q", got.Note)
	}
}

func TestForecast_UnknownModel(t *testing.T) {
	f := NewForecaster()
	got := f.ForecastFromTokens("rando-model-9000", 1000, 500)
	if got.PriceSource != SourceUnknown {
		t.Errorf("expected PriceSource=unknown, got %q", got.PriceSource)
	}
	if got.TotalCostUSD != 0 {
		t.Errorf("unknown model should report $0, got %v", got.TotalCostUSD)
	}
	if !strings.Contains(got.Note, "rando-model-9000") {
		t.Errorf("expected note to name the unknown model, got %q", got.Note)
	}
	if !strings.Contains(got.Note, "RegisterPrice") && !strings.Contains(got.Note, "config") {
		t.Errorf("expected note to suggest registration path, got %q", got.Note)
	}
}

func TestForecast_UserRegistered_TakesPrecedence(t *testing.T) {
	f := NewForecaster()
	f.RegisterPrice("claude-sonnet-4-5", Price{InputUSDPerMillion: 99, OutputUSDPerMillion: 99})
	got := f.ForecastFromTokens("claude-sonnet-4-5", 1_000_000, 0)
	if got.PriceSource != SourceUserRegistered {
		t.Errorf("expected user-registered to win over table, got %q", got.PriceSource)
	}
	if got.InputCostUSD != 99 {
		t.Errorf("expected override price to apply, got %v", got.InputCostUSD)
	}
}

func TestForecast_FromText_EstimatesTokens(t *testing.T) {
	f := NewForecaster()
	text := "Hello world, this is a sample prompt with several words to count."
	got := f.ForecastFromText("claude-haiku-4-5", text, 100)
	if got.InputTokens == 0 {
		t.Errorf("expected non-zero estimated input tokens for non-empty text")
	}
	if got.PriceSource != SourceTable {
		t.Errorf("expected haiku-prefix to hit starter table, got %q", got.PriceSource)
	}
}

func TestForecast_NegativeTokens_ClampedToZero(t *testing.T) {
	f := NewForecaster()
	got := f.ForecastFromTokens("claude-sonnet-4-5", -100, -50)
	if got.InputTokens != 0 || got.OutputTokens != 0 {
		t.Errorf("expected negative tokens clamped to 0, got input=%d output=%d", got.InputTokens, got.OutputTokens)
	}
}

func TestForecast_CaseInsensitiveModel(t *testing.T) {
	f := NewForecaster()
	got := f.ForecastFromTokens("CLAUDE-SONNET-4-5", 1_000_000, 0)
	if got.PriceSource != SourceTable {
		t.Errorf("expected case-insensitive match against table, got %q", got.PriceSource)
	}
}

func TestRegisterForecastTool(t *testing.T) {
	reg := newMockRegistry()
	f := NewForecaster()
	RegisterForecastTool(reg, f)
	found := false
	for _, name := range reg.tools {
		if name == "cost.forecast" {
			found = true
		}
	}
	if !found {
		t.Error("cost.forecast tool was not registered")
	}
}

func TestForecastTool_RoundTripJSON(t *testing.T) {
	reg := newMockRegistry()
	f := NewForecaster()
	RegisterForecastTool(reg, f)

	tool := reg.exec["cost.forecast"]
	if tool == nil {
		t.Fatal("cost.forecast not registered")
	}

	out, err := tool(context.Background(), `{"model":"claude-sonnet-4-5","input_tokens":2000,"expected_output_tokens":500}`)
	if err != nil {
		t.Fatalf("tool exec: %v", err)
	}
	var got Forecast
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("output isn't valid JSON Forecast: %v\noutput: %s", err, out)
	}
	if got.Model != "claude-sonnet-4-5" {
		t.Errorf("Model = %q, want claude-sonnet-4-5", got.Model)
	}
	if got.InputTokens != 2000 {
		t.Errorf("InputTokens = %d, want 2000", got.InputTokens)
	}
	if got.OutputTokens != 500 {
		t.Errorf("OutputTokens = %d, want 500", got.OutputTokens)
	}
	if got.PriceSource != SourceTable {
		t.Errorf("PriceSource = %q, want table", got.PriceSource)
	}
	if got.TotalCostUSD <= 0 {
		t.Errorf("TotalCostUSD should be > 0 for a known model, got %v", got.TotalCostUSD)
	}
}

func TestForecastTool_DefaultOutputTokens(t *testing.T) {
	reg := newMockRegistry()
	f := NewForecaster()
	RegisterForecastTool(reg, f)
	tool := reg.exec["cost.forecast"]

	out, err := tool(context.Background(), `{"model":"claude-sonnet-4-5","input_tokens":1000}`)
	if err != nil {
		t.Fatalf("tool exec: %v", err)
	}
	var got Forecast
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatal(err)
	}
	if got.OutputTokens != 500 {
		t.Errorf("expected default output_tokens=500, got %d", got.OutputTokens)
	}
}

func TestForecastTool_MissingModel(t *testing.T) {
	reg := newMockRegistry()
	f := NewForecaster()
	RegisterForecastTool(reg, f)
	tool := reg.exec["cost.forecast"]

	_, err := tool(context.Background(), `{"input_tokens":100}`)
	if err == nil {
		t.Fatal("expected error when model is missing")
	}
}

func TestForecastTool_InvalidJSON(t *testing.T) {
	reg := newMockRegistry()
	f := NewForecaster()
	RegisterForecastTool(reg, f)
	tool := reg.exec["cost.forecast"]

	_, err := tool(context.Background(), `not json`)
	if err == nil {
		t.Fatal("expected error for malformed JSON")
	}
}

func TestRound4(t *testing.T) {
	cases := []struct {
		in   float64
		want float64
	}{
		{1.234567, 1.2346},
		{0.00009, 0.0001},
		{1.0, 1.0},
		{-1.234567, -1.2346},
	}
	for _, tc := range cases {
		got := round4(tc.in)
		if got != tc.want {
			t.Errorf("round4(%v) = %v, want %v", tc.in, got, tc.want)
		}
	}
}
