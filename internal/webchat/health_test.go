package webchat_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/LumabyteCo/aibutler/internal/db"
	"github.com/LumabyteCo/aibutler/internal/webchat"
)

func testDB(t *testing.T) *db.DB {
	t.Helper()
	database, err := db.Open(db.Config{Path: ":memory:"})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := database.ApplySchema(context.Background()); err != nil {
		t.Fatalf("apply schema: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	return database
}

func TestHealthHandler_Healthy(t *testing.T) {
	database := testDB(t)
	handler := webchat.HealthHandler(database.Conn())

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var resp map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp["status"] != "healthy" {
		t.Errorf("status = %q, want %q", resp["status"], "healthy")
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("content-type = %q, want application/json", ct)
	}
}

func TestHealthHandler_Unhealthy(t *testing.T) {
	database := testDB(t)
	conn := database.Conn()
	// Close the DB to simulate an unhealthy state.
	database.Close()

	handler := webchat.HealthHandler(conn)

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusServiceUnavailable)
	}

	var resp map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp["status"] != "unhealthy" {
		t.Errorf("status = %q, want %q", resp["status"], "unhealthy")
	}
	if resp["error"] != "db" {
		t.Errorf("error = %q, want %q", resp["error"], "db")
	}
}
