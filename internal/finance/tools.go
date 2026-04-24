package finance

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/LumabyteCo/aibutler/internal/tool"
)

// RegisterFinanceTools registers finance tools with the registry.
func RegisterFinanceTools(registry *tool.Registry, provider Provider, watchlist *WatchlistStore) {
	registry.Register(&priceTool{provider: provider})
	registry.Register(&watchlistTool{store: watchlist})
	registry.Register(&alertsTool{store: watchlist, provider: provider})
}

// priceTool looks up a stock/crypto price.
type priceTool struct{ provider Provider }

type priceInput struct {
	Symbol string `json:"symbol"`
}

func (t *priceTool) Name() string        { return "market.price" }
func (t *priceTool) Description() string { return "Look up a stock or crypto price." }
func (t *priceTool) Capability() string  { return "data.finance.read" }

func (t *priceTool) Schema() string {
	return `{
		"type": "object",
		"properties": {
			"symbol": {"type": "string", "description": "Stock ticker (e.g. AAPL, MSFT)"}
		},
		"required": ["symbol"]
	}`
}

func (t *priceTool) Execute(ctx context.Context, input string) (string, error) {
	var in priceInput
	if err := json.Unmarshal([]byte(input), &in); err != nil {
		return "", fmt.Errorf("market.price: %w", err)
	}
	quote, err := t.provider.Quote(ctx, in.Symbol)
	if err != nil {
		return "", err
	}
	data, _ := json.Marshal(quote)
	return string(data), nil
}

// watchlistTool manages the watchlist.
type watchlistTool struct{ store *WatchlistStore }

type watchlistInput struct {
	Action     string   `json:"action"` // "add", "remove", "list"
	Symbol     string   `json:"symbol"`
	Type       string   `json:"type"`
	AlertAbove *float64 `json:"alert_above"`
	AlertBelow *float64 `json:"alert_below"`
}

func (t *watchlistTool) Name() string        { return "market.watchlist" }
func (t *watchlistTool) Description() string { return "Manage your stock/crypto watchlist." }
func (t *watchlistTool) Capability() string  { return "data.finance.write" }

func (t *watchlistTool) Schema() string {
	return `{
		"type": "object",
		"properties": {
			"action":      {"type": "string", "enum": ["add", "remove", "list"]},
			"symbol":      {"type": "string"},
			"type":        {"type": "string", "enum": ["stock", "crypto", "currency"]},
			"alert_above": {"type": "number"},
			"alert_below": {"type": "number"}
		},
		"required": ["action"]
	}`
}

func (t *watchlistTool) Execute(ctx context.Context, input string) (string, error) {
	var in watchlistInput
	if err := json.Unmarshal([]byte(input), &in); err != nil {
		return "", fmt.Errorf("market.watchlist: %w", err)
	}

	switch in.Action {
	case "add":
		typ := in.Type
		if typ == "" {
			typ = "stock"
		}
		item := WatchItem{Symbol: in.Symbol, Type: typ, AlertAbove: in.AlertAbove, AlertBelow: in.AlertBelow}
		if err := t.store.Add(ctx, item); err != nil {
			return "", err
		}
		return fmt.Sprintf("Added %s to watchlist.", in.Symbol), nil
	case "remove":
		typ := in.Type
		if typ == "" {
			typ = "stock"
		}
		if err := t.store.Remove(ctx, in.Symbol, typ); err != nil {
			return "", err
		}
		return fmt.Sprintf("Removed %s from watchlist.", in.Symbol), nil
	case "list":
		items, err := t.store.List(ctx)
		if err != nil {
			return "", err
		}
		if len(items) == 0 {
			return "Watchlist is empty.", nil
		}
		data, _ := json.MarshalIndent(items, "", "  ")
		return string(data), nil
	default:
		return "", fmt.Errorf("market.watchlist: unknown action %q", in.Action)
	}
}

// alertsTool checks for triggered alerts.
type alertsTool struct {
	store    *WatchlistStore
	provider Provider
}

func (t *alertsTool) Name() string        { return "market.alerts" }
func (t *alertsTool) Description() string { return "Check watchlist for triggered price alerts." }
func (t *alertsTool) Capability() string  { return "data.finance.read" }

func (t *alertsTool) Schema() string {
	return `{"type": "object", "properties": {}}`
}

func (t *alertsTool) Execute(ctx context.Context, _ string) (string, error) {
	alerts, err := t.store.CheckAlerts(ctx, t.provider)
	if err != nil {
		return "", err
	}
	if len(alerts) == 0 {
		return "No alerts triggered.", nil
	}
	data, _ := json.MarshalIndent(alerts, "", "  ")
	return string(data), nil
}
