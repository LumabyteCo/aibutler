// chrome.go adds a real, chromedp-backed browser driver to the package.
//
// Where Client (browser.go) does HTTP-only static fetches and
// InteractiveClient historically returned action descriptions, ChromeClient
// drives an actual headless Chrome/Chromium via the DevTools Protocol
// (github.com/chromedp/chromedp — pure Go, no CGO). It renders JavaScript,
// clicks/types on live elements, reads the post-render DOM, and captures
// real screenshots.
//
// Chrome is an external runtime dependency: the binary must be installed on
// the host. ChromeClient detects it and degrades gracefully — Available()
// reports whether a browser was found, and every method returns a clear,
// actionable error when one is not, so callers (and the InteractiveClient
// fallback) can route around it rather than crashing.
//
// A ChromeClient holds ONE persistent browser context for its lifetime so
// multi-step flows (navigate → fill → click → submit) share live page
// state. Calls are serialized by a mutex; the context is created lazily on
// first use and torn down by Close().
package browser

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"sync"
	"time"

	"github.com/chromedp/chromedp"
)

// ErrChromeUnavailable is returned by ChromeClient methods when no
// Chrome/Chromium binary could be located on the host.
var ErrChromeUnavailable = errors.New(
	"browser: no Chrome/Chromium found — install Google Chrome or Chromium to enable live browser automation")

// ChromeClient drives a headless Chrome/Chromium instance via chromedp.
// The zero value is not usable — construct with NewChromeClient.
type ChromeClient struct {
	execPath string        // resolved Chrome binary, "" if none found
	timeout  time.Duration // per-action wall-clock cap

	mu          sync.Mutex
	allocCancel context.CancelFunc
	ctxCancel   context.CancelFunc
	browserCtx  context.Context // nil until first use
	currentURL  string          // last URL navigated to (for EnsureOn reuse)
}

// NewChromeClient creates a ChromeClient, locating a Chrome/Chromium binary
// on the host. If none is found the client is still returned (so tools can
// register), but Available() reports false and every method returns
// ErrChromeUnavailable.
func NewChromeClient() *ChromeClient {
	return &ChromeClient{
		execPath: findChrome(),
		timeout:  30 * time.Second,
	}
}

// SetTimeout overrides the per-action wall-clock cap.
func (c *ChromeClient) SetTimeout(d time.Duration) {
	if d > 0 {
		c.timeout = d
	}
}

// Available reports whether a Chrome/Chromium binary was located.
func (c *ChromeClient) Available() bool { return c.execPath != "" }

// ExecPath returns the resolved browser binary path ("" if none).
func (c *ChromeClient) ExecPath() string { return c.execPath }

// ensureContext lazily creates the persistent allocator + browser context.
// Caller must hold c.mu.
func (c *ChromeClient) ensureContext() error {
	if c.execPath == "" {
		return ErrChromeUnavailable
	}
	if c.browserCtx != nil {
		return nil
	}
	opts := append([]chromedp.ExecAllocatorOption{},
		chromedp.DefaultExecAllocatorOptions[:]...)
	opts = append(opts,
		chromedp.ExecPath(c.execPath),
		chromedp.Headless,
		chromedp.DisableGPU,
		// NoSandbox is required when running as root (common in
		// containers/CI). On a normal desktop it's a no-op.
		chromedp.NoSandbox,
	)
	allocCtx, allocCancel := chromedp.NewExecAllocator(context.Background(), opts...)
	browserCtx, ctxCancel := chromedp.NewContext(allocCtx)

	// Start the browser eagerly on the PERSISTENT context. chromedp ties
	// the browser tab's lifetime to the context of its first Run, so the
	// first Run must be browserCtx itself — not a short-lived per-call
	// timeout child. Otherwise the first action's timeout cancel would
	// tear down the tab and every later call would see "context
	// canceled". An empty Run just launches the browser.
	if err := chromedp.Run(browserCtx); err != nil {
		allocCancel()
		ctxCancel()
		return fmt.Errorf("browser: start chrome: %w", err)
	}

	c.allocCancel = allocCancel
	c.ctxCancel = ctxCancel
	c.browserCtx = browserCtx
	return nil
}

// run executes the chromedp tasks against the persistent context with a
// per-call timeout derived from the caller's ctx. Serialized by c.mu.
func (c *ChromeClient) run(ctx context.Context, tasks ...chromedp.Action) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.ensureContext(); err != nil {
		return err
	}
	// Derive a timeout context that respects BOTH the caller's ctx and
	// the per-action cap, rooted at the persistent browser context so
	// the live page/session is reused across calls.
	runCtx, cancel := context.WithTimeout(c.browserCtx, c.timeout)
	defer cancel()
	// Honor caller cancellation too.
	stop := context.AfterFunc(ctx, cancel)
	defer stop()
	return chromedp.Run(runCtx, tasks...)
}

// Navigate loads the URL, waits for the body, and returns the page title
// and visible text. The URL is remembered so EnsureOn can skip a
// redundant reload that would discard live page state (e.g. a partly
// filled form).
func (c *ChromeClient) Navigate(ctx context.Context, url string) (title, text string, err error) {
	err = c.run(ctx,
		chromedp.Navigate(url),
		chromedp.WaitReady("body", chromedp.ByQuery),
		chromedp.Title(&title),
		chromedp.Text("body", &text, chromedp.ByQuery),
	)
	if err == nil {
		c.mu.Lock()
		c.currentURL = url
		c.mu.Unlock()
	}
	return title, text, err
}

// EnsureOn navigates to url only if the browser isn't already on it,
// preserving live page state across a multi-step flow (navigate → fill →
// submit). A navigation triggered by an earlier click may leave the
// browser on a different URL than recorded; in that case the caller's
// next EnsureOn to the original url will reload, which is the safe choice.
func (c *ChromeClient) EnsureOn(ctx context.Context, url string) error {
	c.mu.Lock()
	same := c.currentURL == url
	c.mu.Unlock()
	if same {
		return nil
	}
	_, _, err := c.Navigate(ctx, url)
	return err
}

// Click clicks the first element matching the CSS selector on the current
// page.
func (c *ChromeClient) Click(ctx context.Context, selector string) error {
	return c.run(ctx, chromedp.Click(selector, chromedp.ByQuery))
}

// Fill types text into the input element matching the CSS selector on the
// current page. Existing content is cleared first.
func (c *ChromeClient) Fill(ctx context.Context, selector, text string) error {
	return c.run(ctx,
		chromedp.Clear(selector, chromedp.ByQuery),
		chromedp.SendKeys(selector, text, chromedp.ByQuery),
	)
}

// SelectOption sets a <select> element's value on the current page.
func (c *ChromeClient) SelectOption(ctx context.Context, selector, value string) error {
	return c.run(ctx, chromedp.SetValue(selector, value, chromedp.ByQuery))
}

// Submit submits the form containing (or matching) the selector on the
// current page.
func (c *ChromeClient) Submit(ctx context.Context, selector string) error {
	return c.run(ctx, chromedp.Submit(selector, chromedp.ByQuery))
}

// ReadText returns the visible text of the current page's body.
func (c *ChromeClient) ReadText(ctx context.Context) (string, error) {
	var text string
	err := c.run(ctx, chromedp.Text("body", &text, chromedp.ByQuery))
	return text, err
}

// Screenshot captures a PNG of the current viewport and returns the raw
// bytes.
func (c *ChromeClient) Screenshot(ctx context.Context) ([]byte, error) {
	var buf []byte
	err := c.run(ctx, chromedp.CaptureScreenshot(&buf))
	return buf, err
}

// Close tears down the browser context and allocator. Safe to call
// multiple times.
func (c *ChromeClient) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.ctxCancel != nil {
		c.ctxCancel()
		c.ctxCancel = nil
	}
	if c.allocCancel != nil {
		c.allocCancel()
		c.allocCancel = nil
	}
	c.browserCtx = nil
}

// chromeCandidates are the binary names / paths searched for a usable
// Chrome or Chromium, in preference order. Mirrors chromedp's own lookup
// plus the standard macOS app-bundle paths.
func chromeCandidates() []string {
	switch runtime.GOOS {
	case "darwin":
		return []string{
			"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
			"/Applications/Chromium.app/Contents/MacOS/Chromium",
			"/Applications/Google Chrome Canary.app/Contents/MacOS/Google Chrome Canary",
			"google-chrome", "chromium",
		}
	case "windows":
		return []string{
			`C:\Program Files\Google\Chrome\Application\chrome.exe`,
			`C:\Program Files (x86)\Google\Chrome\Application\chrome.exe`,
			"chrome.exe", "chrome",
		}
	default: // linux, freebsd, etc.
		return []string{
			"google-chrome", "google-chrome-stable", "chromium",
			"chromium-browser", "headless-shell", "headless_shell",
			"/usr/bin/google-chrome", "/usr/bin/chromium",
		}
	}
}

// findChrome returns the path to a usable Chrome/Chromium binary, or "" if
// none is installed. Absolute candidates are stat-checked; bare names are
// resolved via PATH.
func findChrome() string {
	for _, cand := range chromeCandidates() {
		if isPathLike(cand) {
			if fi, err := os.Stat(cand); err == nil && !fi.IsDir() {
				return cand
			}
			continue
		}
		if p, err := exec.LookPath(cand); err == nil {
			return p
		}
	}
	return ""
}

// isPathLike reports whether s looks like a filesystem path rather than a
// bare command name to resolve via PATH.
func isPathLike(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] == '/' || s[i] == '\\' {
			return true
		}
	}
	return false
}
