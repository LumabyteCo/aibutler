package webchat

import (
	"context"
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
)

// HistoryEntry is one rendered chat message, ordered oldest → newest.
type HistoryEntry struct {
	SessionID string `json:"session_id"`
	Role      string `json:"role"` // "user" | "assistant"
	Content   string `json:"content"`
	CreatedAt string `json:"created_at"`
}

// HistoryStore supplies past conversations for the chat panel. The session
// manager satisfies this via a small adapter (cli wires it at startup).
type HistoryStore interface {
	// RecentForAccount returns up to `limit` messages across the account's
	// sessions, oldest → newest, ordered chronologically.
	RecentForAccount(ctx context.Context, accountID string, limit int) ([]HistoryEntry, error)
}

// historyStore wires a *session.Manager into the webchat's HistoryStore.
// The messages table holds user + assistant rows per session; sessions are
// keyed by channel+account via the sessions table's channel/account_id.
type historyStore struct {
	db *sql.DB
}

// NewHistoryStore creates a HistoryStore over the shared SQLite handle.
func NewHistoryStore(db *sql.DB) HistoryStore {
	return &historyStore{db: db}
}

// RecentForAccount loads the most recent messages for the account's webchat
// sessions. It mirrors what the chat panel displayed before a reload: the
// user's messages and the assistant's replies, in order, across the last
// few sessions.
//
// Webchat account IDs are ip:port per connection (the browser's port
// changes on every reload), so lookups match the IP prefix — every session
// this browser ever created. A caller passing a bare IP gets the same
// result; a full ip:port also works.
func (h *historyStore) RecentForAccount(ctx context.Context, accountID string, limit int) ([]HistoryEntry, error) {
	if limit <= 0 {
		limit = 50
	}
	// Strip a trailing :port — the browser's port differs per connection.
	ip := accountID
	if i := strings.LastIndex(accountID, ":"); i > 0 {
		ip = accountID[:i]
	}
	rows, err := h.db.QueryContext(ctx, `
		SELECT m.session_id, m.role, m.content, m.created_at
		FROM messages m
		JOIN sessions s ON s.id = m.session_id
		WHERE s.channel = 'webchat' AND s.account_id LIKE ? || '%'
		ORDER BY m.id DESC
		LIMIT ?`, ip, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []HistoryEntry
	for rows.Next() {
		var e HistoryEntry
		if err := rows.Scan(&e.SessionID, &e.Role, &e.Content, &e.CreatedAt); err != nil {
			return nil, err
		}
		entries = append(entries, e)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Newest-first from the query; the chat UI wants oldest → newest.
	sort.SliceStable(entries, func(i, j int) bool { return entries[i].CreatedAt < entries[j].CreatedAt })
	return entries, nil
}

// SetHistoryStore wires a history source; nil disables /api/history.
func (a *Adapter) SetHistoryStore(hs HistoryStore) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.history = hs
}

// handleHistory serves GET /api/history?limit=50 — the chat panel's
// recent conversation for the calling browser account.
func (a *Adapter) handleHistory(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	a.mu.RLock()
	hs := a.history
	a.mu.RUnlock()
	if hs == nil {
		http.Error(w, `{"error": "history unavailable"}`, http.StatusServiceUnavailable)
		return
	}

	// The WS account ID is the browser client address (set on connect).
	accountID := r.URL.Query().Get("account")
	if accountID == "" {
		accountID = clientAccount(r)
	}
	limit := 50
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := parseLimit(v); err == nil {
			limit = n
		}
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	entries, err := hs.RecentForAccount(ctx, accountID, limit)
	if err != nil {
		log.Printf("webchat: history: %v", err)
		http.Error(w, `{"error": "history query failed"}`, http.StatusInternalServerError)
		return
	}
	if entries == nil {
		entries = []HistoryEntry{}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"entries": entries,
		"count":   len(entries),
	})
}

// clientAccount derives the account ID the way the WS connection does —
// the remote address. This matches how sessions are keyed for the browser
// client, so history lines up with the session that sent the messages.
func clientAccount(r *http.Request) string {
	host := r.RemoteAddr
	if i := strings.LastIndex(host, ":"); i > 0 {
		host = host[:i]
	}
	return host
}

// parseLimit clamps a user-provided limit to a sane range.
func parseLimit(v string) (int, error) {
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0, err
	}
	if n < 1 {
		n = 1
	}
	if n > 200 {
		n = 200
	}
	return n, nil
}
// HistoryHandlerForTest exposes the history route in isolation for tests.
// In production the route is mounted by the adapter's mux.
func (a *Adapter) HistoryHandlerForTest() http.Handler {
	return http.HandlerFunc(a.handleHistory)
}

// noCache wraps an http.Handler with revalidation headers so browsers
// always check for updated static assets.
func noCache(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-cache, must-revalidate")
		next.ServeHTTP(w, r)
	})
}
