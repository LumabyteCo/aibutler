package pwa

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestManifestHandler(t *testing.T) {
	handler := ManifestHandler()

	req := httptest.NewRequest("GET", "/manifest.json", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	if ct := w.Header().Get("Content-Type"); ct != "application/manifest+json" {
		t.Errorf("content-type = %q, want application/manifest+json", ct)
	}
	if cc := w.Header().Get("Cache-Control"); cc == "" {
		t.Error("expected Cache-Control header")
	}

	body := w.Body.String()
	if !strings.Contains(body, "AI Butler") {
		t.Error("manifest should contain app name")
	}
	if !strings.Contains(body, "standalone") {
		t.Error("manifest should specify standalone display mode")
	}
}

// TestManifestIsValidJSON ensures the manifest is machine-parseable and
// has the fields browsers actually require for installability.
func TestManifestIsValidJSON(t *testing.T) {
	handler := ManifestHandler()
	req := httptest.NewRequest("GET", "/manifest.json", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	var m map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &m); err != nil {
		t.Fatalf("manifest is not valid JSON: %v", err)
	}

	// Fields Chrome/Safari need to treat the site as installable:
	// name, start_url, display, icons (with 192 + 512).
	for _, key := range []string{"name", "short_name", "start_url", "display", "theme_color", "background_color", "icons"} {
		if _, ok := m[key]; !ok {
			t.Errorf("manifest missing required field %q", key)
		}
	}

	icons, ok := m["icons"].([]interface{})
	if !ok || len(icons) == 0 {
		t.Fatal("manifest icons must be a non-empty array")
	}

	// Every icon entry must have src, sizes, type.
	var has192, has512, hasMaskable bool
	for _, raw := range icons {
		icon, ok := raw.(map[string]interface{})
		if !ok {
			t.Errorf("icon entry is not an object: %v", raw)
			continue
		}
		for _, key := range []string{"src", "sizes", "type"} {
			if _, present := icon[key]; !present {
				t.Errorf("icon missing required field %q: %v", key, icon)
			}
		}
		switch icon["sizes"] {
		case "192x192":
			has192 = true
		case "512x512":
			has512 = true
		}
		if icon["purpose"] == "maskable" {
			hasMaskable = true
		}
	}
	if !has192 {
		t.Error("manifest must include a 192x192 icon")
	}
	if !has512 {
		t.Error("manifest must include a 512x512 icon")
	}
	if !hasMaskable {
		t.Error("manifest should include a maskable icon for Android")
	}
}

func TestServiceWorkerHandler(t *testing.T) {
	handler := ServiceWorkerHandler()

	req := httptest.NewRequest("GET", "/sw.js", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	if ct := w.Header().Get("Content-Type"); ct != "application/javascript" {
		t.Errorf("content-type = %q, want application/javascript", ct)
	}
	if w.Header().Get("Service-Worker-Allowed") != "/" {
		t.Error("expected Service-Worker-Allowed: / header (required to widen SW scope)")
	}

	body := w.Body.String()
	if !strings.Contains(body, "butler-cache-v") {
		t.Error("service worker should version the cache name")
	}

	// Regression guards for the critical SW lifecycle handlers.
	for _, hook := range []string{"'install'", "'activate'", "'fetch'"} {
		if !strings.Contains(body, hook) {
			t.Errorf("service worker missing %s handler", hook)
		}
	}

	// Must bypass /ws: service workers cannot intercept WebSocket upgrades
	// and trying to cache them breaks the chat.
	if !strings.Contains(body, "'/ws'") {
		t.Error("service worker should explicitly bypass /ws")
	}
}
