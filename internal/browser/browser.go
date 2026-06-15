// Package browser provides headless browser automation tools.
//
// Two backends:
//
//   - HTTP-only (the default Client): fast static fetches with title/text
//     and link extraction. No JavaScript, no external runtime.
//   - Chrome-backed (attach a ChromeClient via SetChrome, see chrome.go):
//     live JavaScript rendering, real screenshots, and the interactive
//     click/type/select/submit actions. Requires a Chrome/Chromium binary
//     on the host; degrades gracefully when absent.
package browser

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
	"unicode"

	"github.com/LumabyteCo/aibutler/internal/security/ssrf"
)

// toolRegistry is the narrow interface for registering tools (avoids import cycles).
type toolRegistry interface {
	Register(name, description, schema, capability string, exec func(ctx context.Context, input string) (string, error))
}

// Client is an HTTP-based headless browser client. With a ChromeClient
// attached (SetChrome) it also gains live JS-rendered reads and real
// screenshots; without one it stays HTTP-only.
type Client struct {
	httpClient *http.Client
	userAgent  string
	skipSSRF   bool
	chrome     *ChromeClient // optional — enables RenderText + real Screenshot
}

// NewClient creates a browser client with sensible defaults.
func NewClient() *Client {
	return &Client{
		httpClient: &http.Client{Timeout: 30 * time.Second},
		userAgent:  "Mozilla/5.0 (compatible; AIButler/1.0; +https://github.com/LumabyteCo/aibutler)",
	}
}

// SetChrome attaches a ChromeClient, enabling live JS-rendered reads
// (RenderText) and real screenshots when a browser binary is present.
func (c *Client) SetChrome(cc *ChromeClient) { c.chrome = cc }

// chromeLive reports whether a Chrome backend is attached and available.
func (c *Client) chromeLive() bool { return c.chrome != nil && c.chrome.Available() }

// SetHTTPClient overrides the HTTP client (for testing).
// It also disables SSRF validation since test servers bind to localhost.
func (c *Client) SetHTTPClient(h *http.Client) {
	c.httpClient = h
	c.skipSSRF = true // test servers bind to localhost
}

// Navigate fetches the URL and returns the page title and extracted body text.
func (c *Client) Navigate(ctx context.Context, rawURL string) (title, text string, err error) {
	body, err := c.fetch(ctx, rawURL)
	if err != nil {
		return "", "", err
	}
	title = extractTitle(body)
	text = extractText(body)
	return title, text, nil
}

// ExtractLinks fetches the URL and returns all absolute href links.
func (c *Client) ExtractLinks(ctx context.Context, rawURL string) ([]string, error) {
	body, err := c.fetch(ctx, rawURL)
	if err != nil {
		return nil, err
	}
	return extractLinks(body, rawURL), nil
}

// Screenshot captures the page. With a live Chrome backend it navigates
// and returns a base64-encoded PNG data URI; without one it returns the
// headless placeholder (no browser installed).
func (c *Client) Screenshot(ctx context.Context, rawURL string) (string, error) {
	if !c.chromeLive() {
		return "screenshot not available in headless mode", nil
	}
	if err := c.chrome.EnsureOn(ctx, rawURL); err != nil {
		return "", fmt.Errorf("browser.screenshot: navigate: %w", err)
	}
	png, err := c.chrome.Screenshot(ctx)
	if err != nil {
		return "", fmt.Errorf("browser.screenshot: %w", err)
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(png), nil
}

// RenderText loads the URL in a live headless browser (executing
// JavaScript) and returns the rendered title and visible text. Falls
// back to nothing — returns ErrChromeUnavailable — when no browser is
// installed; callers can use Navigate (static HTTP) instead.
func (c *Client) RenderText(ctx context.Context, rawURL string) (title, text string, err error) {
	if !c.chromeLive() {
		return "", "", ErrChromeUnavailable
	}
	return c.chrome.Navigate(ctx, rawURL)
}

func (c *Client) fetch(ctx context.Context, rawURL string) (string, error) {
	// Block requests to private/internal networks (SSRF protection).
	if !c.skipSSRF {
		if err := ssrf.ValidateURL(rawURL); err != nil {
			return "", fmt.Errorf("browser: %w", err)
		}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return "", fmt.Errorf("browser: create request: %w", err)
	}
	req.Header.Set("User-Agent", c.userAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,*/*")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("browser: fetch %s: %w", rawURL, err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(io.LimitReader(resp.Body, 2*1024*1024)) // 2 MB limit
	if err != nil {
		return "", fmt.Errorf("browser: read body: %w", err)
	}
	return string(data), nil
}

// extractTitle pulls the <title> content from HTML.
func extractTitle(html string) string {
	lower := strings.ToLower(html)
	start := strings.Index(lower, "<title")
	if start < 0 {
		return ""
	}
	close := strings.Index(lower[start:], ">")
	if close < 0 {
		return ""
	}
	start = start + close + 1
	end := strings.Index(lower[start:], "</title>")
	if end < 0 {
		return ""
	}
	return strings.TrimSpace(html[start : start+end])
}

// extractText strips HTML tags and returns plain text.
func extractText(html string) string {
	var sb strings.Builder
	inTag := false
	inScript := false
	inStyle := false

	lower := strings.ToLower(html)
	i := 0
	for i < len(html) {
		ch := html[i]
		if !inTag {
			// Detect script/style blocks.
			if strings.HasPrefix(lower[i:], "<script") {
				inScript = true
				inTag = true
			} else if strings.HasPrefix(lower[i:], "<style") {
				inStyle = true
				inTag = true
			} else if ch == '<' {
				// Check if closing script/style.
				if inScript && strings.HasPrefix(lower[i:], "</script") {
					inScript = false
				} else if inStyle && strings.HasPrefix(lower[i:], "</style") {
					inStyle = false
				}
				inTag = true
			} else if !inScript && !inStyle {
				r := rune(ch)
				if unicode.IsSpace(r) {
					if sb.Len() > 0 {
						last := rune(sb.String()[sb.Len()-1])
						if !unicode.IsSpace(last) {
							sb.WriteByte(' ')
						}
					}
				} else {
					sb.WriteByte(ch)
				}
			}
		} else if ch == '>' {
			inTag = false
		}
		i++
	}

	result := strings.TrimSpace(sb.String())
	// Collapse multiple spaces.
	for strings.Contains(result, "  ") {
		result = strings.ReplaceAll(result, "  ", " ")
	}
	// Limit output length.
	const maxLen = 8000
	if len(result) > maxLen {
		result = result[:maxLen] + "..."
	}
	return result
}

// extractLinks finds all href attributes from anchor tags.
func extractLinks(html, baseURL string) []string {
	var links []string
	seen := make(map[string]bool)

	lower := strings.ToLower(html)
	i := 0
	for i < len(html) {
		idx := strings.Index(lower[i:], "href=")
		if idx < 0 {
			break
		}
		i += idx + 5
		if i >= len(html) {
			break
		}

		var quote byte
		if html[i] == '"' || html[i] == '\'' {
			quote = html[i]
			i++
		}

		end := i
		if quote != 0 {
			for end < len(html) && html[end] != quote {
				end++
			}
		} else {
			for end < len(html) && html[end] != '>' && html[end] != ' ' {
				end++
			}
		}

		if end <= i {
			continue
		}

		href := html[i:end]
		href = strings.TrimSpace(href)
		if href == "" || strings.HasPrefix(href, "#") || strings.HasPrefix(href, "javascript:") {
			i = end + 1
			continue
		}

		// Make relative URLs absolute.
		if !strings.HasPrefix(href, "http://") && !strings.HasPrefix(href, "https://") {
			if strings.HasPrefix(href, "/") {
				// Extract scheme + host from baseURL.
				parts := strings.SplitN(baseURL, "/", 4)
				if len(parts) >= 3 {
					href = parts[0] + "//" + parts[2] + href
				}
			} else {
				href = baseURL + "/" + href
			}
		}

		if !seen[href] {
			seen[href] = true
			links = append(links, href)
		}
		i = end + 1
	}
	return links
}

// RegisterBrowserTools registers browser.navigate, browser.extract_links, and browser.screenshot.
func RegisterBrowserTools(registry toolRegistry, client *Client) {
	registry.Register(
		"browser.navigate",
		"Fetch a URL and return the page title and text content.",
		`{"type":"object","properties":{"url":{"type":"string","description":"URL to navigate to"}},"required":["url"]}`,
		"tool.web.fetch",
		func(ctx context.Context, input string) (string, error) {
			var args struct {
				URL string `json:"url"`
			}
			if err := json.Unmarshal([]byte(input), &args); err != nil {
				return "", fmt.Errorf("browser.navigate: invalid input: %w", err)
			}
			if args.URL == "" {
				return "", fmt.Errorf("browser.navigate: url is required")
			}
			title, text, err := client.Navigate(ctx, args.URL)
			if err != nil {
				return "", err
			}
			out, _ := json.Marshal(map[string]string{"title": title, "text": text, "url": args.URL})
			return string(out), nil
		},
	)

	registry.Register(
		"browser.extract_links",
		"Extract all hyperlinks from a web page.",
		`{"type":"object","properties":{"url":{"type":"string","description":"URL to extract links from"}},"required":["url"]}`,
		"tool.web.fetch",
		func(ctx context.Context, input string) (string, error) {
			var args struct {
				URL string `json:"url"`
			}
			if err := json.Unmarshal([]byte(input), &args); err != nil {
				return "", fmt.Errorf("browser.extract_links: invalid input: %w", err)
			}
			if args.URL == "" {
				return "", fmt.Errorf("browser.extract_links: url is required")
			}
			links, err := client.ExtractLinks(ctx, args.URL)
			if err != nil {
				return "", err
			}
			out, _ := json.Marshal(map[string]interface{}{"links": links, "count": len(links)})
			return string(out), nil
		},
	)

	registry.Register(
		"browser.screenshot",
		"Take a screenshot of a web page. Returns a base64 PNG data URI when a Chrome/Chromium browser is installed; a placeholder otherwise.",
		`{"type":"object","properties":{"url":{"type":"string","description":"URL to screenshot"}},"required":["url"]}`,
		"tool.web.fetch",
		func(ctx context.Context, input string) (string, error) {
			var args struct {
				URL string `json:"url"`
			}
			if err := json.Unmarshal([]byte(input), &args); err != nil {
				return "", fmt.Errorf("browser.screenshot: invalid input: %w", err)
			}
			if args.URL == "" {
				return "", fmt.Errorf("browser.screenshot: url is required")
			}
			msg, err := client.Screenshot(ctx, args.URL)
			if err != nil {
				return "", err
			}
			out, _ := json.Marshal(map[string]string{"result": msg, "url": args.URL})
			return string(out), nil
		},
	)

	registry.Register(
		"browser.read_page",
		"Load a URL in a real headless browser (executing JavaScript) and return the rendered title and visible text. Use this for JS-heavy pages where browser.navigate (static HTTP fetch) returns little content. Requires a Chrome/Chromium browser; falls back with a clear error when none is installed.",
		`{"type":"object","properties":{"url":{"type":"string","description":"URL to render"}},"required":["url"]}`,
		"tool.web.fetch",
		func(ctx context.Context, input string) (string, error) {
			var args struct {
				URL string `json:"url"`
			}
			if err := json.Unmarshal([]byte(input), &args); err != nil {
				return "", fmt.Errorf("browser.read_page: invalid input: %w", err)
			}
			if args.URL == "" {
				return "", fmt.Errorf("browser.read_page: url is required")
			}
			title, text, err := client.RenderText(ctx, args.URL)
			if err != nil {
				return "", err
			}
			out, _ := json.Marshal(map[string]string{"title": title, "text": text, "url": args.URL})
			return string(out), nil
		},
	)
}
