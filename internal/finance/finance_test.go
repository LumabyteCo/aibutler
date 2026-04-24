package finance_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/LumabyteCo/aibutler/internal/finance"
	"github.com/LumabyteCo/aibutler/testutil"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// fakeProvider is a mock Provider for testing.
type fakeProvider struct {
	quotes map[string]*finance.Quote
}

func (f *fakeProvider) Quote(_ context.Context, symbol string) (*finance.Quote, error) {
	q, ok := f.quotes[symbol]
	if !ok {
		return nil, fmt.Errorf("not found")
	}
	return q, nil
}

// ---------------------------------------------------------------------------
// AlphaVantage / Quote parsing
// ---------------------------------------------------------------------------

func TestQuoteParsing(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{
			"Global Quote": {
				"01. symbol": "AAPL",
				"05. price": "178.72",
				"09. change": "-1.28",
				"10. change percent": "-0.7114%"
			}
		}`)
	}))
	defer srv.Close()

	// Create a provider that talks to our test server.
	p := finance.NewAlphaVantageProvider("test-key", nil)
	// Override client to point at mock server.
	finance.SetProviderURL(p, srv.URL+"?function=GLOBAL_QUOTE&symbol=%s&apikey=%s")

	q, err := p.Quote(context.Background(), "AAPL")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if q.Symbol != "AAPL" {
		t.Errorf("symbol = %q, want AAPL", q.Symbol)
	}
	if q.Price != 178.72 {
		t.Errorf("price = %f, want 178.72", q.Price)
	}
	if q.Currency != "USD" {
		t.Errorf("currency = %q, want USD", q.Currency)
	}
	if q.Change != -1.28 {
		t.Errorf("change = %f, want -1.28", q.Change)
	}
	if q.ChangePercent != -0.7114 {
		t.Errorf("change_percent = %f, want -0.7114", q.ChangePercent)
	}
}

func TestQuoteHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	p := finance.NewAlphaVantageProvider("test-key", nil)
	finance.SetProviderURL(p, srv.URL+"?function=GLOBAL_QUOTE&symbol=%s&apikey=%s")

	_, err := p.Quote(context.Background(), "AAPL")
	if err == nil {
		t.Fatal("expected error for HTTP 500")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("error = %q, want mention of 500", err.Error())
	}
}

func TestQuoteNoData(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"Global Quote": {}}`)
	}))
	defer srv.Close()

	p := finance.NewAlphaVantageProvider("test-key", nil)
	finance.SetProviderURL(p, srv.URL+"?function=GLOBAL_QUOTE&symbol=%s&apikey=%s")

	_, err := p.Quote(context.Background(), "INVALID")
	if err == nil {
		t.Fatal("expected error for empty quote")
	}
	if !strings.Contains(err.Error(), "no data") {
		t.Errorf("error = %q, want 'no data'", err.Error())
	}
}

// ---------------------------------------------------------------------------
// Rate limiter
// ---------------------------------------------------------------------------

func TestRateLimiter(t *testing.T) {
	rl := finance.NewTestRateLimiter(3, time.Hour)
	for i := range 3 {
		if !rl.Allow() {
			t.Fatalf("Allow() #%d = false, want true", i+1)
		}
	}
	if rl.Allow() {
		t.Fatal("Allow() #4 = true, want false")
	}
}

func TestRateLimiterReset(t *testing.T) {
	rl := finance.NewTestRateLimiter(1, time.Millisecond)
	if !rl.Allow() {
		t.Fatal("first Allow() = false, want true")
	}
	if rl.Allow() {
		t.Fatal("second Allow() before reset = true, want false")
	}
	time.Sleep(2 * time.Millisecond)
	if !rl.Allow() {
		t.Fatal("Allow() after window reset = false, want true")
	}
}

// ---------------------------------------------------------------------------
// Watchlist CRUD
// ---------------------------------------------------------------------------

func TestWatchlistAdd(t *testing.T) {
	database := testutil.TestDB(t)
	store := finance.NewWatchlistStore(database.Conn())
	ctx := context.Background()

	err := store.Add(ctx, finance.WatchItem{Symbol: "AAPL", Type: "stock"})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}

	items, err := store.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("len = %d, want 1", len(items))
	}
	if items[0].Symbol != "AAPL" {
		t.Errorf("symbol = %q, want AAPL", items[0].Symbol)
	}
	if items[0].Type != "stock" {
		t.Errorf("type = %q, want stock", items[0].Type)
	}
}

func TestWatchlistRemove(t *testing.T) {
	database := testutil.TestDB(t)
	store := finance.NewWatchlistStore(database.Conn())
	ctx := context.Background()

	if err := store.Add(ctx, finance.WatchItem{Symbol: "MSFT", Type: "stock"}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if err := store.Remove(ctx, "MSFT", "stock"); err != nil {
		t.Fatalf("Remove: %v", err)
	}

	items, err := store.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("len = %d, want 0", len(items))
	}
}

func TestWatchlistList(t *testing.T) {
	database := testutil.TestDB(t)
	store := finance.NewWatchlistStore(database.Conn())
	ctx := context.Background()

	if err := store.Add(ctx, finance.WatchItem{Symbol: "MSFT", Type: "stock"}); err != nil {
		t.Fatalf("Add MSFT: %v", err)
	}
	if err := store.Add(ctx, finance.WatchItem{Symbol: "AAPL", Type: "stock"}); err != nil {
		t.Fatalf("Add AAPL: %v", err)
	}

	items, err := store.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("len = %d, want 2", len(items))
	}
	// Ordered by symbol
	if items[0].Symbol != "AAPL" {
		t.Errorf("items[0].Symbol = %q, want AAPL", items[0].Symbol)
	}
	if items[1].Symbol != "MSFT" {
		t.Errorf("items[1].Symbol = %q, want MSFT", items[1].Symbol)
	}
}

func TestWatchlistDuplicate(t *testing.T) {
	database := testutil.TestDB(t)
	store := finance.NewWatchlistStore(database.Conn())
	ctx := context.Background()

	if err := store.Add(ctx, finance.WatchItem{Symbol: "AAPL", Type: "stock"}); err != nil {
		t.Fatalf("first Add: %v", err)
	}
	err := store.Add(ctx, finance.WatchItem{Symbol: "AAPL", Type: "stock"})
	if err == nil {
		t.Fatal("expected UNIQUE constraint error on duplicate add")
	}
	if !strings.Contains(err.Error(), "UNIQUE") {
		t.Errorf("error = %q, want UNIQUE constraint mention", err.Error())
	}
}

// ---------------------------------------------------------------------------
// Watchlist Alerts
// ---------------------------------------------------------------------------

func TestWatchlistAlertAbove(t *testing.T) {
	database := testutil.TestDB(t)
	store := finance.NewWatchlistStore(database.Conn())
	ctx := context.Background()

	above := 150.0
	if err := store.Add(ctx, finance.WatchItem{
		Symbol: "AAPL", Type: "stock", AlertAbove: &above,
	}); err != nil {
		t.Fatalf("Add: %v", err)
	}

	provider := &fakeProvider{quotes: map[string]*finance.Quote{
		"AAPL": {Symbol: "AAPL", Price: 160.0, Currency: "USD"},
	}}

	alerts, err := store.CheckAlerts(ctx, provider)
	if err != nil {
		t.Fatalf("CheckAlerts: %v", err)
	}
	if len(alerts) != 1 {
		t.Fatalf("len(alerts) = %d, want 1", len(alerts))
	}
	if alerts[0].Condition != "above" {
		t.Errorf("condition = %q, want above", alerts[0].Condition)
	}
	if alerts[0].Price != 160.0 {
		t.Errorf("price = %f, want 160.0", alerts[0].Price)
	}
}

func TestWatchlistAlertBelow(t *testing.T) {
	database := testutil.TestDB(t)
	store := finance.NewWatchlistStore(database.Conn())
	ctx := context.Background()

	below := 100.0
	if err := store.Add(ctx, finance.WatchItem{
		Symbol: "TSLA", Type: "stock", AlertBelow: &below,
	}); err != nil {
		t.Fatalf("Add: %v", err)
	}

	provider := &fakeProvider{quotes: map[string]*finance.Quote{
		"TSLA": {Symbol: "TSLA", Price: 90.0, Currency: "USD"},
	}}

	alerts, err := store.CheckAlerts(ctx, provider)
	if err != nil {
		t.Fatalf("CheckAlerts: %v", err)
	}
	if len(alerts) != 1 {
		t.Fatalf("len(alerts) = %d, want 1", len(alerts))
	}
	if alerts[0].Condition != "below" {
		t.Errorf("condition = %q, want below", alerts[0].Condition)
	}
	if alerts[0].Price != 90.0 {
		t.Errorf("price = %f, want 90.0", alerts[0].Price)
	}
}

// ---------------------------------------------------------------------------
// Tools
// ---------------------------------------------------------------------------

func TestMarketPriceTool(t *testing.T) {
	provider := &fakeProvider{quotes: map[string]*finance.Quote{
		"GOOG": {Symbol: "GOOG", Price: 141.80, Currency: "USD", Change: 2.30, ChangePercent: 1.65},
	}}

	pt := finance.NewPriceTool(provider)

	input := `{"symbol":"GOOG"}`
	result, err := pt.Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	var q finance.Quote
	if err := json.Unmarshal([]byte(result), &q); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if q.Symbol != "GOOG" {
		t.Errorf("symbol = %q, want GOOG", q.Symbol)
	}
	if q.Price != 141.80 {
		t.Errorf("price = %f, want 141.80", q.Price)
	}
}
