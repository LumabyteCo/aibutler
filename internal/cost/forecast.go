// Package cost provides pre-action token / USD forecasts.
//
// Where the existing cost.status tool reports historical usage from the
// transactions table, this package answers "what will THIS action cost
// before I run it?" — useful for the supervisor agent to surface a
// cost-approval prompt before kicking off an expensive mission.
//
// Pricing data is approximate and **may drift from current vendor pricing**.
// The starter table below is a small set of well-known model-name prefixes;
// callers can override or extend at runtime via RegisterPrice.
//
// Token estimation reuses internal/prompt.EstimateTokens — a byte/word
// hybrid that handles non-ASCII text (Arabic, CJK) better than naive
// chars/4. The estimate is intentionally on the high side for safety.
package cost

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/LumabyteCo/aibutler/internal/prompt"
)

// PriceSource indicates where the price came from.
type PriceSource string

const (
	// SourceTable — looked up from the built-in starter table.
	SourceTable PriceSource = "table"
	// SourceFree — known-free local backend (Ollama, LM Studio, vLLM, llama.cpp).
	SourceFree PriceSource = "free-local"
	// SourceUserRegistered — registered via RegisterPrice at runtime.
	SourceUserRegistered PriceSource = "user-registered"
	// SourceUnknown — no price available; forecast returns zero with a note.
	SourceUnknown PriceSource = "unknown"
)

// Price is the per-model pricing tuple in USD per 1 million tokens.
type Price struct {
	InputUSDPerMillion  float64
	OutputUSDPerMillion float64
}

// Forecast is the result of a cost forecast.
type Forecast struct {
	Model         string      `json:"model"`
	InputTokens   int         `json:"input_tokens"`
	OutputTokens  int         `json:"output_tokens"`
	InputCostUSD  float64     `json:"input_cost_usd"`
	OutputCostUSD float64     `json:"output_cost_usd"`
	TotalCostUSD  float64     `json:"total_cost_usd"`
	PriceSource   PriceSource `json:"price_source"`
	Note          string      `json:"note,omitempty"`
}

// Forecaster estimates token usage and USD cost for a planned action.
type Forecaster struct {
	mu        sync.RWMutex
	overrides map[string]Price // user-registered prices, exact match
}

// NewForecaster creates a Forecaster initialised with the starter table.
func NewForecaster() *Forecaster {
	return &Forecaster{
		overrides: map[string]Price{},
	}
}

// RegisterPrice registers or overrides a price for the given model name.
// Lookup is exact (case-insensitive) before falling back to the starter
// table's prefix matching.
func (f *Forecaster) RegisterPrice(model string, p Price) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.overrides[strings.ToLower(model)] = p
}

// ForecastFromText estimates tokens for the given input text and returns
// the projected cost. Output tokens are caller-provided (typically the
// max-output budget for the model).
func (f *Forecaster) ForecastFromText(model, text string, expectedOutputTokens int) Forecast {
	in := prompt.EstimateTokens(text)
	return f.ForecastFromTokens(model, in, expectedOutputTokens)
}

// ForecastFromTokens computes the cost given known token counts.
func (f *Forecaster) ForecastFromTokens(model string, inputTokens, outputTokens int) Forecast {
	if inputTokens < 0 {
		inputTokens = 0
	}
	if outputTokens < 0 {
		outputTokens = 0
	}

	price, source, note := f.lookupPrice(model)

	inCost := float64(inputTokens) / 1_000_000 * price.InputUSDPerMillion
	outCost := float64(outputTokens) / 1_000_000 * price.OutputUSDPerMillion

	return Forecast{
		Model:         model,
		InputTokens:   inputTokens,
		OutputTokens:  outputTokens,
		InputCostUSD:  round4(inCost),
		OutputCostUSD: round4(outCost),
		TotalCostUSD:  round4(inCost + outCost),
		PriceSource:   source,
		Note:          note,
	}
}

func (f *Forecaster) lookupPrice(model string) (Price, PriceSource, string) {
	key := strings.ToLower(model)

	// 1. User-registered exact match.
	f.mu.RLock()
	if p, ok := f.overrides[key]; ok {
		f.mu.RUnlock()
		return p, SourceUserRegistered, ""
	}
	f.mu.RUnlock()

	// 2. Free local backends — match by name prefix.
	for _, prefix := range freeLocalPrefixes {
		if strings.HasPrefix(key, prefix) {
			return Price{}, SourceFree, "local-only inference; no per-token cost"
		}
	}

	// 3. Starter table — prefix match.
	for _, entry := range starterTable {
		if strings.HasPrefix(key, entry.prefix) {
			return entry.price, SourceTable, "approximate price; verify against current vendor pricing"
		}
	}

	// 4. Unknown — zero cost + clear note.
	return Price{}, SourceUnknown, fmt.Sprintf(
		"no pricing for %q — register via RegisterPrice or in config to get a real forecast",
		model,
	)
}

// freeLocalPrefixes lists model-name prefixes that always run free locally.
var freeLocalPrefixes = []string{
	"ollama",
	"lmstudio",
	"vllm",
	"llamacpp",
	"llama.cpp",
}

// starterTableEntry is one row of the conservative built-in pricing table.
type starterTableEntry struct {
	prefix string
	price  Price
}

// starterTable holds approximate pricing for commonly-named models. These
// are intentionally conservative and may be outdated relative to current
// vendor pricing — callers who need accuracy should override via
// RegisterPrice or a config-driven loader.
//
// Only Anthropic's current line is shipped here because their published
// pricing is stable and well-known. Other vendors (OpenAI, Google, xAI)
// should be added via config — shipping wrong defaults for them would
// undermine trust in the forecast.
var starterTable = []starterTableEntry{
	// Anthropic — USD per 1M tokens (input / output). Verify at
	// https://www.anthropic.com/pricing before relying on these for billing.
	{prefix: "claude-opus", price: Price{15.00, 75.00}},
	{prefix: "claude-sonnet", price: Price{3.00, 15.00}},
	{prefix: "claude-haiku", price: Price{1.00, 5.00}},
}

func round4(v float64) float64 {
	// Round to 4 decimal places (ten-thousandths of a dollar).
	const scale = 10000.0
	if v >= 0 {
		return float64(int64(v*scale+0.5)) / scale
	}
	return float64(int64(v*scale-0.5)) / scale
}

// toolRegistry is the narrow interface for registering tools (avoids import cycles).
type toolRegistry interface {
	Register(name, description, schema, capability string, exec func(ctx context.Context, input string) (string, error))
}

// RegisterForecastTool registers cost.forecast.
func RegisterForecastTool(registry toolRegistry, f *Forecaster) {
	registry.Register(
		"cost.forecast",
		"Estimate the USD cost of a planned model call before running it. Provide either input_text or input_tokens; expected_output_tokens is the planned output budget. Returns input/output/total USD plus a price-source indicator. Pricing for unknown models is reported as zero with a note.",
		`{"type":"object","properties":{`+
			`"model":{"type":"string","description":"Model name (e.g. claude-sonnet-4-5, ollama:llama3, gpt-4o)"},`+
			`"input_text":{"type":"string","description":"Optional. The prompt text — token count is estimated from this."},`+
			`"input_tokens":{"type":"integer","minimum":0,"description":"Optional. Pre-counted input token count. Takes precedence over input_text."},`+
			`"expected_output_tokens":{"type":"integer","minimum":0,"description":"Planned maximum output tokens (default 500 if omitted)."}`+
			`},"required":["model"]}`,
		"", // No capability — advisory only, always available.
		func(ctx context.Context, input string) (string, error) {
			var args struct {
				Model                string `json:"model"`
				InputText            string `json:"input_text"`
				InputTokens          int    `json:"input_tokens"`
				ExpectedOutputTokens int    `json:"expected_output_tokens"`
			}
			if err := json.Unmarshal([]byte(input), &args); err != nil {
				return "", fmt.Errorf("cost.forecast: invalid input: %w", err)
			}
			if strings.TrimSpace(args.Model) == "" {
				return "", fmt.Errorf("cost.forecast: model is required")
			}

			outTokens := args.ExpectedOutputTokens
			if outTokens == 0 {
				outTokens = 500 // sensible default
			}

			var forecast Forecast
			switch {
			case args.InputTokens > 0:
				forecast = f.ForecastFromTokens(args.Model, args.InputTokens, outTokens)
			case args.InputText != "":
				forecast = f.ForecastFromText(args.Model, args.InputText, outTokens)
			default:
				forecast = f.ForecastFromTokens(args.Model, 0, outTokens)
			}

			out, err := json.Marshal(forecast)
			if err != nil {
				return "", fmt.Errorf("cost.forecast: marshal: %w", err)
			}
			return string(out), nil
		},
	)
}
