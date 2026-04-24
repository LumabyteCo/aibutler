//go:build integration

package integration

import (
	"strings"
	"testing"

	"github.com/LumabyteCo/aibutler/internal/agent"
)

// TestE2EWatchlistAdd verifies that market.watchlist with action "add"
// persists an item in the finance_watchlist table.
func TestE2EWatchlistAdd(t *testing.T) {
	p := setupPipelineWithOpts(t, pipelineOpts{
		WithFinance: true,
		Responses: []agent.Response{
			// Turn 1: model calls market.watchlist add
			toolCallResponse("Adding to watchlist.",
				tc("tc1", "market.watchlist", `{"action":"add","symbol":"AAPL","type":"stock"}`),
			),
			// Turn 1 continued: final reply
			finalResponse("Added AAPL to your watchlist."),
		},
	})

	p.sendMsg(t, "Add AAPL to my watchlist")

	// Verify final response.
	resp := p.lastResponse(t)
	if !strings.Contains(resp, "AAPL") {
		t.Errorf("response = %q, want mention of AAPL", resp)
	}

	// Verify watchlist item was persisted.
	count := p.countRows(t, "finance_watchlist")
	if count != 1 {
		t.Fatalf("finance_watchlist rows = %d, want 1", count)
	}

	symbol := p.querySingleString(t, "SELECT symbol FROM finance_watchlist LIMIT 1")
	if symbol != "AAPL" {
		t.Errorf("symbol = %q, want 'AAPL'", symbol)
	}

	typ := p.querySingleString(t, "SELECT type FROM finance_watchlist LIMIT 1")
	if typ != "stock" {
		t.Errorf("type = %q, want 'stock'", typ)
	}
}

// TestE2EWatchlistList adds 2 items, then lists them.
func TestE2EWatchlistList(t *testing.T) {
	p := setupPipelineWithOpts(t, pipelineOpts{
		WithFinance: true,
		Responses: []agent.Response{
			// Turn 1: add AAPL
			toolCallResponse("Adding AAPL.",
				tc("tc1", "market.watchlist", `{"action":"add","symbol":"AAPL","type":"stock"}`),
			),
			finalResponse("Added AAPL."),

			// Turn 2: add MSFT
			toolCallResponse("Adding MSFT.",
				tc("tc2", "market.watchlist", `{"action":"add","symbol":"MSFT","type":"stock"}`),
			),
			finalResponse("Added MSFT."),

			// Turn 3: list watchlist
			toolCallResponse("Listing watchlist.",
				tc("tc3", "market.watchlist", `{"action":"list"}`),
			),
			finalResponse("Your watchlist: AAPL and MSFT."),
		},
	})

	// Turn 1: add AAPL.
	p.sendMsg(t, "Add AAPL to watchlist")
	if p.countRows(t, "finance_watchlist") != 1 {
		t.Fatal("expected 1 item after first add")
	}

	// Turn 2: add MSFT.
	p.sendMsg(t, "Add MSFT too")
	if p.countRows(t, "finance_watchlist") != 2 {
		t.Fatal("expected 2 items after second add")
	}

	// Turn 3: list.
	p.sendMsg(t, "Show my watchlist")

	resp := p.lastResponse(t)
	if !strings.Contains(resp, "AAPL") || !strings.Contains(resp, "MSFT") {
		t.Errorf("list response = %q, want mention of both AAPL and MSFT", resp)
	}

	if p.responseCount() != 3 {
		t.Errorf("response count = %d, want 3", p.responseCount())
	}
}

// TestE2EWatchlistRemove adds an item, then removes it.
func TestE2EWatchlistRemove(t *testing.T) {
	p := setupPipelineWithOpts(t, pipelineOpts{
		WithFinance: true,
		Responses: []agent.Response{
			// Turn 1: add AAPL
			toolCallResponse("Adding AAPL.",
				tc("tc1", "market.watchlist", `{"action":"add","symbol":"AAPL","type":"stock"}`),
			),
			finalResponse("Added AAPL."),

			// Turn 2: remove AAPL
			toolCallResponse("Removing AAPL.",
				tc("tc2", "market.watchlist", `{"action":"remove","symbol":"AAPL","type":"stock"}`),
			),
			finalResponse("Removed AAPL from your watchlist."),
		},
	})

	// Turn 1: add.
	p.sendMsg(t, "Add AAPL to watchlist")
	if p.countRows(t, "finance_watchlist") != 1 {
		t.Fatal("expected 1 item after add")
	}

	// Turn 2: remove.
	p.sendMsg(t, "Remove AAPL from watchlist")

	// Verify row is deleted.
	count := p.countRows(t, "finance_watchlist")
	if count != 0 {
		t.Errorf("finance_watchlist rows = %d, want 0 after removal", count)
	}

	resp := p.lastResponse(t)
	if !strings.Contains(resp, "Removed") {
		t.Errorf("response = %q, want mention of Removed", resp)
	}
}
