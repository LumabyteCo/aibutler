package webchat_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/LumabyteCo/aibutler/internal/channel"
	"github.com/LumabyteCo/aibutler/internal/webchat"
	"github.com/LumabyteCo/aibutler/internal/webchat/pwa"
	"nhooyr.io/websocket"
)

func TestWebChatName(t *testing.T) {
	a := webchat.New(webchat.DefaultConfig())
	if a.Name() != "webchat" {
		t.Errorf("name = %q, want webchat", a.Name())
	}
}

// testServer creates an httptest server with the webchat adapter's handler.
func testServer(t *testing.T, handler channel.MessageHandler) (*httptest.Server, *webchat.Adapter) {
	t.Helper()
	a := webchat.New(webchat.DefaultConfig())

	// We can't easily call Start() (it binds a real port), so we test
	// the HTTP handlers directly via httptest.
	// For that, we need to invoke Start with a handler, then use the mux.
	// Instead, let's just test the adapter methods and static serving.

	// Start with a no-op context to set the handler.
	if err := a.Start(context.Background(), handler); err != nil {
		t.Fatal(err)
	}

	// Give the server a moment to start.
	time.Sleep(50 * time.Millisecond)

	return nil, a
}

func TestStaticFileServing(t *testing.T) {
	cfg := webchat.DefaultConfig()
	cfg.Port = 18090
	a := webchat.New(cfg)

	handler := func(_ context.Context, _ channel.Envelope) error { return nil }
	if err := a.Start(context.Background(), handler); err != nil {
		t.Fatal(err)
	}
	defer a.Stop(context.Background())

	time.Sleep(100 * time.Millisecond)

	resp, err := http.Get("http://localhost:18090/")
	if err != nil {
		t.Skipf("could not connect to webchat server: %v (port may be in use)", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "AI Butler") {
		t.Error("expected index.html to contain 'AI Butler'")
	}
}

func TestWebSocketConnectAndMessage(t *testing.T) {
	cfg := webchat.DefaultConfig()
	cfg.Port = 18091 // Use a non-standard port to avoid conflicts.
	a := webchat.New(cfg)

	received := make(chan channel.Envelope, 1)
	handler := func(_ context.Context, env channel.Envelope) error {
		received <- env
		return nil
	}

	if err := a.Start(context.Background(), handler); err != nil {
		t.Fatal(err)
	}
	defer a.Stop(context.Background())

	time.Sleep(100 * time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, _, err := websocket.Dial(ctx, "ws://localhost:18091/ws", nil)
	if err != nil {
		t.Skipf("could not connect to websocket: %v", err)
		return
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	// Send a message.
	msg := `{"type":"message","text":"hello from test"}`
	if err := conn.Write(ctx, websocket.MessageText, []byte(msg)); err != nil {
		t.Fatal(err)
	}

	// Wait for the handler to receive it.
	select {
	case env := <-received:
		if env.Text != "hello from test" {
			t.Errorf("text = %q, want 'hello from test'", env.Text)
		}
		if env.Channel != "webchat" {
			t.Errorf("channel = %q, want webchat", env.Channel)
		}
	case <-time.After(3 * time.Second):
		t.Error("timeout waiting for message")
	}
}

func TestStaticCSSServing(t *testing.T) {
	cfg := webchat.DefaultConfig()
	cfg.Port = 18095
	a := webchat.New(cfg)

	handler := func(_ context.Context, _ channel.Envelope) error { return nil }
	if err := a.Start(context.Background(), handler); err != nil {
		t.Fatal(err)
	}
	defer a.Stop(context.Background())

	time.Sleep(100 * time.Millisecond)

	resp, err := http.Get("http://localhost:18095/static/style.css")
	if err != nil {
		t.Skipf("could not connect: %v", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("style.css status = %d, want 200", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "#app") {
		t.Error("expected style.css to contain CSS rules")
	}
}

// TestPWAAssetsAndIndexWiring verifies the PWA-critical static assets are
// served AND the index page advertises the manifest + apple-touch-icon so
// browsers can discover it. The /manifest.json and /sw.js endpoints
// themselves are wired in cli/app.go via MountHandler — their reachability
// is covered by the pwa package's own tests. This test is the regression
// guard for the HTML/static side of the install flow.
func TestPWAAssetsAndIndexWiring(t *testing.T) {
	cfg := webchat.DefaultConfig()
	cfg.Port = 18096
	a := webchat.New(cfg)

	handler := func(_ context.Context, _ channel.Envelope) error { return nil }
	// Install the PWA handlers the same way cli/app.go does at runtime, so
	// the test reflects real behavior instead of an un-wired adapter.
	a.MountHandler("/manifest.json", pwa.ManifestHandler())
	a.MountHandler("/sw.js", pwa.ServiceWorkerHandler())
	if err := a.Start(context.Background(), handler); err != nil {
		t.Fatal(err)
	}
	defer a.Stop(context.Background())

	time.Sleep(100 * time.Millisecond)

	// /manifest.json must return JSON with application/manifest+json.
	resp, err := http.Get("http://localhost:18096/manifest.json")
	if err != nil {
		t.Skipf("could not connect: %v", err)
		return
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("/manifest.json status = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/manifest+json" {
		t.Errorf("/manifest.json content-type = %q, want application/manifest+json", ct)
	}
	resp.Body.Close()

	// /sw.js must return JS with the scope-widening header.
	resp, err = http.Get("http://localhost:18096/sw.js")
	if err != nil {
		t.Fatalf("GET /sw.js: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("/sw.js status = %d, want 200", resp.StatusCode)
	}
	if resp.Header.Get("Service-Worker-Allowed") != "/" {
		t.Error("/sw.js missing Service-Worker-Allowed: / header")
	}
	resp.Body.Close()

	// The icon PNGs must be served from the static FS (manifest references
	// them at /static/icon-*.png).
	for _, path := range []string{"/static/icon-192.png", "/static/icon-512.png", "/static/apple-touch-icon.png"} {
		resp, err := http.Get("http://localhost:18096" + path)
		if err != nil {
			t.Errorf("GET %s: %v", path, err)
			continue
		}
		if resp.StatusCode != http.StatusOK {
			t.Errorf("%s status = %d, want 200 (icon file missing from embedded static FS?)", path, resp.StatusCode)
		}
		resp.Body.Close()
	}

	// Index page must advertise the manifest so browsers discover it.
	resp, err = http.Get("http://localhost:18096/")
	if err != nil {
		t.Fatalf("GET /: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), `rel="manifest"`) {
		t.Error("index.html missing <link rel=\"manifest\">")
	}
	if !strings.Contains(string(body), `apple-touch-icon`) {
		t.Error("index.html missing apple-touch-icon link (iOS install support)")
	}
}

func TestStaticJSServing(t *testing.T) {
	cfg := webchat.DefaultConfig()
	cfg.Port = 18096
	a := webchat.New(cfg)

	handler := func(_ context.Context, _ channel.Envelope) error { return nil }
	if err := a.Start(context.Background(), handler); err != nil {
		t.Fatal(err)
	}
	defer a.Stop(context.Background())

	time.Sleep(100 * time.Millisecond)

	resp, err := http.Get("http://localhost:18096/static/chat.js")
	if err != nil {
		t.Skipf("could not connect: %v", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("chat.js status = %d, want 200", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "WebSocket") {
		t.Error("expected chat.js to contain WebSocket logic")
	}
}

func TestIndexReferencesStaticAssets(t *testing.T) {
	cfg := webchat.DefaultConfig()
	cfg.Port = 18097
	a := webchat.New(cfg)

	handler := func(_ context.Context, _ channel.Envelope) error { return nil }
	if err := a.Start(context.Background(), handler); err != nil {
		t.Fatal(err)
	}
	defer a.Stop(context.Background())

	time.Sleep(100 * time.Millisecond)

	resp, err := http.Get("http://localhost:18097/")
	if err != nil {
		t.Skipf("could not connect: %v", err)
		return
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	html := string(body)

	if !strings.Contains(html, "/static/style.css") {
		t.Error("index.html must reference /static/style.css (not relative path)")
	}
	if !strings.Contains(html, "/static/chat.js") {
		t.Error("index.html must reference /static/chat.js (not relative path)")
	}
}

// TestIndexContainsMissionsPanel verifies the v0.3.x missions panel
// markup is present in the embedded index.html. The panel consumes
// the existing /api/dashboard/missions/* endpoints (already covered
// by handler tests in internal/webchat/dashboard) — this test just
// ensures the frontend surface is wired up: nav button, panel
// section, and the live-tail / detail subview placeholders.
func TestIndexContainsMissionsPanel(t *testing.T) {
	cfg := webchat.DefaultConfig()
	cfg.Port = 18101
	a := webchat.New(cfg)

	handler := func(_ context.Context, _ channel.Envelope) error { return nil }
	if err := a.Start(context.Background(), handler); err != nil {
		t.Fatal(err)
	}
	defer a.Stop(context.Background())

	time.Sleep(100 * time.Millisecond)

	resp, err := http.Get("http://localhost:18101/")
	if err != nil {
		t.Skipf("could not connect: %v", err)
		return
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	html := string(body)

	wantMarkers := []string{
		`data-panel="missions"`,        // sidebar nav button + panel section both carry this
		`id="missions-list"`,           // mission list container
		`id="mission-detail"`,          // detail subview container
		`id="mission-detail-events"`,   // live event tail
		`id="missions-include-done"`,   // filter toggle
		`id="missions-stat-active"`,    // header stats tile
	}
	for _, want := range wantMarkers {
		if !strings.Contains(html, want) {
			t.Errorf("index.html missing missions-panel marker %q", want)
		}
	}
}

// TestStaticJSReferencesMissionLoader verifies the v0.3.x chat.js
// bundle includes the missions panel wiring — the loader and the
// polling helpers. Without this, the panel HTML would render empty.
func TestStaticJSReferencesMissionLoader(t *testing.T) {
	cfg := webchat.DefaultConfig()
	cfg.Port = 18102
	a := webchat.New(cfg)

	handler := func(_ context.Context, _ channel.Envelope) error { return nil }
	if err := a.Start(context.Background(), handler); err != nil {
		t.Fatal(err)
	}
	defer a.Stop(context.Background())

	time.Sleep(100 * time.Millisecond)

	resp, err := http.Get("http://localhost:18102/static/chat.js")
	if err != nil {
		t.Skipf("could not connect: %v", err)
		return
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	js := string(body)

	wantSymbols := []string{
		"loadMissionsData",
		"loadMissionDetail",
		"startMissionsPolling",
		"/api/dashboard/missions",
	}
	for _, want := range wantSymbols {
		if !strings.Contains(js, want) {
			t.Errorf("chat.js missing missions-panel symbol %q", want)
		}
	}
}

func TestWebSocketEchoResponse(t *testing.T) {
	cfg := webchat.DefaultConfig()
	cfg.Port = 18098
	a := webchat.New(cfg)

	// Handler that echoes back via the adapter (like cmd_run does).
	handler := func(ctx context.Context, env channel.Envelope) error {
		reply := channel.OutgoingMessage{
			Text: "Echo: " + env.Text,
		}
		return a.Send(ctx, env.AccountID, reply)
	}

	if err := a.Start(context.Background(), handler); err != nil {
		t.Fatal(err)
	}
	defer a.Stop(context.Background())

	time.Sleep(100 * time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, _, err := websocket.Dial(ctx, "ws://localhost:18098/ws", nil)
	if err != nil {
		t.Skipf("could not connect: %v", err)
		return
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	// Send a message.
	msg := `{"type":"message","text":"hello"}`
	if err := conn.Write(ctx, websocket.MessageText, []byte(msg)); err != nil {
		t.Fatal(err)
	}

	// Read the echo response.
	_, data, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("read error: %v", err)
	}

	var frame struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(data, &frame); err != nil {
		t.Fatal(err)
	}
	if frame.Type != "message" {
		t.Errorf("type = %q, want message", frame.Type)
	}
	if !strings.Contains(frame.Text, "hello") {
		t.Errorf("response text = %q, want it to contain 'hello'", frame.Text)
	}
}

func TestEnterKeySendsMessage(t *testing.T) {
	// Verify chat.js contains Enter key handler.
	// This is a static analysis test — ensures the JS has the keydown handler.
	cfg := webchat.DefaultConfig()
	cfg.Port = 18099
	a := webchat.New(cfg)

	handler := func(_ context.Context, _ channel.Envelope) error { return nil }
	if err := a.Start(context.Background(), handler); err != nil {
		t.Fatal(err)
	}
	defer a.Stop(context.Background())

	time.Sleep(100 * time.Millisecond)

	resp, err := http.Get("http://localhost:18099/static/chat.js")
	if err != nil {
		t.Skipf("could not connect: %v", err)
		return
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	js := string(body)

	if !strings.Contains(js, `e.key==="Enter"`) {
		t.Error("chat.js must handle Enter key to send messages")
	}
	if !strings.Contains(js, "e.preventDefault()") {
		t.Error("chat.js must call preventDefault on Enter to avoid newline")
	}
}

func TestGracefulShutdown(t *testing.T) {
	cfg := webchat.DefaultConfig()
	cfg.Port = 18092
	a := webchat.New(cfg)

	handler := func(_ context.Context, _ channel.Envelope) error { return nil }
	if err := a.Start(context.Background(), handler); err != nil {
		t.Fatal(err)
	}

	time.Sleep(50 * time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := a.Stop(ctx); err != nil {
		t.Errorf("Stop error: %v", err)
	}

	// Server should no longer accept connections.
	time.Sleep(50 * time.Millisecond)
	_, err := http.Get("http://localhost:18092/")
	if err == nil {
		t.Error("expected connection refused after shutdown")
	}
}

func TestSendToDisconnectedClient(t *testing.T) {
	a := webchat.New(webchat.DefaultConfig())
	err := a.Send(context.Background(), "nonexistent", channel.OutgoingMessage{Text: "hi"})
	if err == nil {
		t.Error("expected error for disconnected client")
	}
}

func TestFileUploadSizeLimit(t *testing.T) {
	cfg := webchat.DefaultConfig()
	cfg.Port = 18093
	cfg.MaxUploadSize = 100 // 100 bytes max
	a := webchat.New(cfg)

	handler := func(_ context.Context, _ channel.Envelope) error { return nil }
	if err := a.Start(context.Background(), handler); err != nil {
		t.Fatal(err)
	}
	defer a.Stop(context.Background())

	time.Sleep(100 * time.Millisecond)

	// Create a multipart body that exceeds the limit.
	body := strings.NewReader(strings.Repeat("x", 200))
	resp, err := http.Post("http://localhost:18093/upload", "multipart/form-data", body)
	if err != nil {
		t.Skipf("could not connect: %v", err)
		return
	}
	defer resp.Body.Close()

	// Should get a 400 or 413 error.
	if resp.StatusCode == http.StatusOK {
		t.Error("expected error status for oversized upload")
	}
}

func TestWebSocketTyping(t *testing.T) {
	cfg := webchat.DefaultConfig()
	cfg.Port = 18094
	a := webchat.New(cfg)

	handler := func(ctx context.Context, env channel.Envelope) error {
		// When we get a message, send a typing indicator back.
		return a.SendTyping(ctx, env.AccountID)
	}

	if err := a.Start(context.Background(), handler); err != nil {
		t.Fatal(err)
	}
	defer a.Stop(context.Background())

	time.Sleep(100 * time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, _, err := websocket.Dial(ctx, "ws://localhost:18094/ws", nil)
	if err != nil {
		t.Skipf("could not connect: %v", err)
		return
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	// Send a message to trigger the typing indicator.
	msg := `{"type":"message","text":"trigger typing"}`
	if err := conn.Write(ctx, websocket.MessageText, []byte(msg)); err != nil {
		t.Fatal(err)
	}

	// Read the typing indicator.
	_, data, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("read error: %v", err)
	}

	var frame struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(data, &frame); err != nil {
		t.Fatal(err)
	}
	if frame.Type != "typing" {
		t.Errorf("type = %q, want typing", frame.Type)
	}
}
