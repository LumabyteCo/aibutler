package dashboard

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"testing"

	_ "github.com/ncruces/go-sqlite3/driver"
)

func setupTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}

	// Create tables used by enhanced endpoints.
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS resource_access_log (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			action TEXT NOT NULL,
			resource TEXT,
			account_id TEXT,
			result TEXT,
			created_at TEXT NOT NULL DEFAULT (datetime('now'))
		)`,
		`CREATE TABLE IF NOT EXISTS transaction_audit (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			type TEXT,
			description TEXT,
			amount_usd REAL DEFAULT 0,
			status TEXT,
			created_at TEXT NOT NULL DEFAULT (datetime('now'))
		)`,
	}
	for _, stmt := range stmts {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatal(err)
		}
	}

	return db
}

func TestHandleAudit(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	// Insert test data.
	db.Exec(`INSERT INTO resource_access_log (action, resource, account_id, result) VALUES ('data.read', '/api/test', 'user1', 'ok')`)
	db.Exec(`INSERT INTO resource_access_log (action, resource, account_id, result) VALUES ('shell.exec', '/bin/ls', 'user2', 'ok')`)

	d := New(db, nil, nil)
	handler := d.Handler()

	req := httptest.NewRequest("GET", "/api/dashboard/audit", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	body := w.Body.String()
	if body == "" {
		t.Fatal("expected non-empty response body")
	}
}

func TestHandleCapabilities(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	db.Exec(`INSERT INTO resource_access_log (action, resource) VALUES ('data.read', '/test')`)
	db.Exec(`INSERT INTO resource_access_log (action, resource) VALUES ('data.read', '/test2')`)
	db.Exec(`INSERT INTO resource_access_log (action, resource) VALUES ('shell.exec', '/bin/ls')`)

	d := New(db, nil, nil)
	handler := d.Handler()

	req := httptest.NewRequest("GET", "/api/dashboard/capabilities", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestHandleTransactions(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	db.Exec(`INSERT INTO transaction_audit (type, description, amount_usd, status) VALUES ('api_call', 'test call', 0.05, 'completed')`)

	d := New(db, nil, nil)
	handler := d.Handler()

	req := httptest.NewRequest("GET", "/api/dashboard/transactions", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestHandleConfigSchema(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	d := New(db, nil, nil)
	handler := d.Handler()

	req := httptest.NewRequest("GET", "/api/dashboard/config/schema", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}
