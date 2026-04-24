package finance

import (
	"context"
	"database/sql"
	"fmt"
)

// WatchItem represents a watched financial instrument.
type WatchItem struct {
	ID         int      `json:"id"`
	Symbol     string   `json:"symbol"`
	Name       string   `json:"name,omitempty"`
	Type       string   `json:"type"` // "stock", "crypto", "currency"
	AlertAbove *float64 `json:"alert_above,omitempty"`
	AlertBelow *float64 `json:"alert_below,omitempty"`
}

// Alert represents a triggered price alert.
type Alert struct {
	Item      WatchItem `json:"item"`
	Price     float64   `json:"price"`
	Condition string    `json:"condition"` // "above" or "below"
}

// WatchlistStore manages the finance watchlist in SQLite.
type WatchlistStore struct {
	db *sql.DB
}

// NewWatchlistStore creates a new watchlist store.
func NewWatchlistStore(db *sql.DB) *WatchlistStore {
	return &WatchlistStore{db: db}
}

// Add adds an item to the watchlist.
func (w *WatchlistStore) Add(ctx context.Context, item WatchItem) error {
	_, err := w.db.ExecContext(ctx,
		`INSERT INTO finance_watchlist (symbol, name, type, alert_above, alert_below)
		 VALUES (?, ?, ?, ?, ?)`,
		item.Symbol, item.Name, item.Type, item.AlertAbove, item.AlertBelow)
	if err != nil {
		return fmt.Errorf("watchlist.add: %w", err)
	}
	return nil
}

// Remove removes an item by symbol and type.
func (w *WatchlistStore) Remove(ctx context.Context, symbol, typ string) error {
	res, err := w.db.ExecContext(ctx,
		`DELETE FROM finance_watchlist WHERE symbol = ? AND type = ?`, symbol, typ)
	if err != nil {
		return fmt.Errorf("watchlist.remove: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("watchlist.remove: not found")
	}
	return nil
}

// List returns all watchlist items.
func (w *WatchlistStore) List(ctx context.Context) ([]WatchItem, error) {
	rows, err := w.db.QueryContext(ctx,
		`SELECT id, symbol, name, type, alert_above, alert_below FROM finance_watchlist ORDER BY symbol`)
	if err != nil {
		return nil, fmt.Errorf("watchlist.list: %w", err)
	}
	defer rows.Close()

	var items []WatchItem
	for rows.Next() {
		var item WatchItem
		if err := rows.Scan(&item.ID, &item.Symbol, &item.Name, &item.Type,
			&item.AlertAbove, &item.AlertBelow); err != nil {
			return nil, fmt.Errorf("watchlist.scan: %w", err)
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

// CheckAlerts checks watchlist items against current prices and returns triggered alerts.
func (w *WatchlistStore) CheckAlerts(ctx context.Context, provider Provider) ([]Alert, error) {
	items, err := w.List(ctx)
	if err != nil {
		return nil, err
	}

	var alerts []Alert
	for _, item := range items {
		quote, err := provider.Quote(ctx, item.Symbol)
		if err != nil {
			continue // Skip items that fail to fetch
		}

		if item.AlertAbove != nil && quote.Price >= *item.AlertAbove {
			alerts = append(alerts, Alert{Item: item, Price: quote.Price, Condition: "above"})
		}
		if item.AlertBelow != nil && quote.Price <= *item.AlertBelow {
			alerts = append(alerts, Alert{Item: item, Price: quote.Price, Condition: "below"})
		}
	}
	return alerts, nil
}
