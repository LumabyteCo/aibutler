package browser_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/LumabyteCo/aibutler/internal/browser"
)

// chromeOrSkip returns a ready ChromeClient, or skips the test when no
// Chrome/Chromium binary is installed (e.g. on CI). The browser is torn
// down via t.Cleanup.
func chromeOrSkip(t *testing.T) *browser.ChromeClient {
	t.Helper()
	c := browser.NewChromeClient()
	if !c.Available() {
		t.Skip("no Chrome/Chromium installed — skipping live browser test")
	}
	c.SetTimeout(20 * time.Second)
	t.Cleanup(c.Close)
	return c
}

func htmlServer(t *testing.T, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestChrome_NavigateReadsTitleAndText(t *testing.T) {
	c := chromeOrSkip(t)
	srv := htmlServer(t, `<html><head><title>Widget Co</title></head>
		<body><h1>Welcome</h1><p>hello rendered world</p></body></html>`)

	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()

	title, text, err := c.Navigate(ctx, srv.URL)
	if err != nil {
		t.Fatalf("Navigate: %v", err)
	}
	if title != "Widget Co" {
		t.Errorf("title = %q, want 'Widget Co'", title)
	}
	if !strings.Contains(text, "hello rendered world") {
		t.Errorf("text = %q, want it to contain the body copy", text)
	}
}

func TestChrome_ExecutesJavaScript(t *testing.T) {
	c := chromeOrSkip(t)
	// The paragraph is empty in static HTML; JS fills it. A static HTTP
	// fetch would miss this — proving the browser actually renders.
	srv := htmlServer(t, `<html><head><title>JS</title></head>
		<body><p id="out"></p>
		<script>document.getElementById('out').textContent = 'filled by javascript';</script>
		</body></html>`)

	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()

	_, text, err := c.Navigate(ctx, srv.URL)
	if err != nil {
		t.Fatalf("Navigate: %v", err)
	}
	if !strings.Contains(text, "filled by javascript") {
		t.Errorf("rendered text = %q, want JS-injected content", text)
	}
}

func TestChrome_FillClickReflectsOnPage(t *testing.T) {
	c := chromeOrSkip(t)
	// Typing into the input and clicking the button copies the input's
	// value into #result via JS. Reading the page back proves fill+click
	// drove the live DOM.
	srv := htmlServer(t, `<html><head><title>Form</title></head><body>
		<input id="name" type="text">
		<button id="go" onclick="document.getElementById('result').textContent = document.getElementById('name').value">Go</button>
		<div id="result"></div>
		</body></html>`)

	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()

	if _, _, err := c.Navigate(ctx, srv.URL); err != nil {
		t.Fatalf("Navigate: %v", err)
	}
	if err := c.Fill(ctx, "#name", "Ada"); err != nil {
		t.Fatalf("Fill: %v", err)
	}
	if err := c.Click(ctx, "#go"); err != nil {
		t.Fatalf("Click: %v", err)
	}
	text, err := c.ReadText(ctx)
	if err != nil {
		t.Fatalf("ReadText: %v", err)
	}
	if !strings.Contains(text, "Ada") {
		t.Errorf("after fill+click, page text = %q, want it to contain 'Ada'", text)
	}
}

func TestChrome_ScreenshotReturnsPNG(t *testing.T) {
	c := chromeOrSkip(t)
	srv := htmlServer(t, `<html><body style="background:#fff"><h1>shot</h1></body></html>`)

	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()

	if _, _, err := c.Navigate(ctx, srv.URL); err != nil {
		t.Fatalf("Navigate: %v", err)
	}
	png, err := c.Screenshot(ctx)
	if err != nil {
		t.Fatalf("Screenshot: %v", err)
	}
	// PNG magic number: 0x89 'P' 'N' 'G'.
	if len(png) < 8 || png[0] != 0x89 || png[1] != 'P' || png[2] != 'N' || png[3] != 'G' {
		t.Errorf("screenshot is not a PNG (len=%d, first bytes=%v)", len(png), firstBytes(png, 8))
	}
}

// TestInteractive_LiveExecution drives the real click path through the
// InteractiveClient (with a Chrome backend attached), confirming the
// validation pre-checks and live execution compose.
func TestInteractive_LiveExecution(t *testing.T) {
	c := chromeOrSkip(t)
	srv := htmlServer(t, `<html><head><title>Live</title></head><body>
		<button id="b" onclick="document.title='clicked'">B</button>
		</body></html>`)

	ic := browser.NewInteractiveClient()
	ic.SetChrome(c)

	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()

	res, err := ic.Click(ctx, srv.URL, "#b")
	if err != nil {
		t.Fatalf("Click: %v", err)
	}
	// Live mode returns "Clicked ..." (not the "Action: click ..." stub).
	if !strings.HasPrefix(res, "Clicked ") {
		t.Errorf("live click result = %q, want it to start with 'Clicked '", res)
	}
}

// TestChrome_RenderTextViaClient covers the HTTP Client's RenderText path
// (used by the browser.read_page tool) with a Chrome backend attached.
func TestChrome_RenderTextViaClient(t *testing.T) {
	c := chromeOrSkip(t)
	srv := htmlServer(t, `<html><head><title>Read</title></head><body><p>render me</p></body></html>`)

	client := browser.NewClient()
	client.SetChrome(c)

	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()

	title, text, err := client.RenderText(ctx, srv.URL)
	if err != nil {
		t.Fatalf("RenderText: %v", err)
	}
	if title != "Read" || !strings.Contains(text, "render me") {
		t.Errorf("RenderText = (%q, %q), want title 'Read' + body 'render me'", title, text)
	}
}

func firstBytes(b []byte, n int) []byte {
	if len(b) < n {
		return b
	}
	return b[:n]
}
