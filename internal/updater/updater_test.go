package updater_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/LumabyteCo/aibutler/internal/updater"
)

func TestCheckWithMockServer(t *testing.T) {
	rel := updater.Release{
		Version: "0.2.0",
		URL:     "https://example.com/download",
		SHA256:  "abc123",
		Notes:   "Bug fixes",
	}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(rel)
	}))
	defer ts.Close()

	u := updater.New("0.1.0", ts.URL, 1*time.Hour)
	u.SetHTTPClient(ts.Client())

	got, err := u.Check(context.Background())
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if got.Version != "0.2.0" {
		t.Errorf("version = %q, want '0.2.0'", got.Version)
	}
	if got.Notes != "Bug fixes" {
		t.Errorf("notes = %q, want 'Bug fixes'", got.Notes)
	}
}

func TestDownload(t *testing.T) {
	content := []byte("binary-content-here")
	hash := sha256.Sum256(content)
	hashHex := hex.EncodeToString(hash[:])

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(content)
	}))
	defer ts.Close()

	rel := &updater.Release{
		Version: "0.2.0",
		URL:     ts.URL,
		SHA256:  hashHex,
	}

	u := updater.New("0.1.0", "", 1*time.Hour)
	u.SetHTTPClient(ts.Client())

	dest := filepath.Join(t.TempDir(), "download")
	if err := u.Download(context.Background(), rel, dest); err != nil {
		t.Fatalf("Download: %v", err)
	}

	data, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(data) != string(content) {
		t.Errorf("downloaded content mismatch")
	}
}

func TestIsNewer(t *testing.T) {
	tests := []struct {
		current, remote string
		want            bool
	}{
		{"0.1.0", "0.2.0", true},
		{"0.2.0", "0.1.0", false},
		{"0.1.0", "0.1.0", false},
		{"1.0.0", "1.0.1", true},
		{"v1.0.0", "v2.0.0", true},
		{"0.9.9", "1.0.0", true},
	}
	for _, tt := range tests {
		got := updater.IsNewer(tt.current, tt.remote)
		if got != tt.want {
			t.Errorf("IsNewer(%q, %q) = %v, want %v", tt.current, tt.remote, got, tt.want)
		}
	}
}

func TestStartStop(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(updater.Release{Version: "0.1.0"})
	}))
	defer ts.Close()

	u := updater.New("0.1.0", ts.URL, 1*time.Hour)
	u.SetHTTPClient(ts.Client())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := u.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	// Double start should error.
	if err := u.Start(ctx); err == nil {
		t.Error("expected error on double Start")
	}

	if err := u.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
}
