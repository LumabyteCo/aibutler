package setup_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/LumabyteCo/aibutler/internal/config"
	"github.com/LumabyteCo/aibutler/internal/webchat/setup"
)

func TestStatusEndpoint(t *testing.T) {
	cfg := config.Default()
	w := setup.New("", cfg)
	handler := w.Handler()

	req := httptest.NewRequest(http.MethodGet, "/api/setup/status", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	var resp map[string]interface{}
	json.NewDecoder(rec.Body).Decode(&resp)
	if resp["completed"].(bool) {
		t.Error("expected completed = false")
	}
	steps := resp["steps"].([]interface{})
	if len(steps) != 2 {
		t.Errorf("steps count = %d, want 2", len(steps))
	}
}

func TestModelConfig(t *testing.T) {
	cfg := config.Default()
	w := setup.New("", cfg)
	handler := w.Handler()

	body := `{"provider":"anthropic","model":"claude-sonnet-4-6","api_key":"test-key"}`
	req := httptest.NewRequest(http.MethodPost, "/api/setup/model", bytes.NewBufferString(body))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	if cfg.Configurations.Models.Primary != "claude-sonnet-4-6" {
		t.Errorf("model = %q, want 'claude-sonnet-4-6'", cfg.Configurations.Models.Primary)
	}
}

func TestChannelsConfig(t *testing.T) {
	cfg := config.Default()
	w := setup.New("", cfg)
	handler := w.Handler()

	body := `{"active_channels":["webchat","telegram"]}`
	req := httptest.NewRequest(http.MethodPost, "/api/setup/channels", bytes.NewBufferString(body))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	if len(cfg.Settings.ActiveChannels) != 2 {
		t.Errorf("active channels = %d, want 2", len(cfg.Settings.ActiveChannels))
	}
}

func TestCompleteEndpoint(t *testing.T) {
	cfg := config.Default()
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	w := setup.New(configPath, cfg)
	handler := w.Handler()

	// Complete setup.
	req := httptest.NewRequest(http.MethodPost, "/api/setup/complete", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}

	if !w.IsComplete() {
		t.Error("expected IsComplete() = true after /api/setup/complete")
	}

	// Verify config was written.
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		t.Error("expected config file to be written")
	}
}
