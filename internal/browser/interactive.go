package browser

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
)

// InteractiveClient provides browser automation command descriptions.
// Since we cannot add browser automation dependencies (no Rod/CDP), this
// validates inputs and returns structured responses describing the intended
// action. This is a capability framework ready for real browser integration later.
type InteractiveClient struct {
	// lastDomain tracks the domain of the last navigation to enforce cross-domain blocking.
	lastDomain string
}

// NewInteractiveClient creates a new interactive browser client.
func NewInteractiveClient() *InteractiveClient {
	return &InteractiveClient{}
}

// Click describes clicking an element by CSS selector on the given URL.
func (ic *InteractiveClient) Click(_ context.Context, pageURL, selector string) (string, error) {
	if pageURL == "" {
		return "", fmt.Errorf("browser.click: url is required")
	}
	if selector == "" {
		return "", fmt.Errorf("browser.click: selector is required")
	}
	if err := ic.checkCrossDomain(pageURL); err != nil {
		return "", err
	}
	return fmt.Sprintf("Action: click element matching selector %q on %s", selector, pageURL), nil
}

// Type describes typing text into an input field.
// Refuses to type into password fields for safety.
func (ic *InteractiveClient) Type(_ context.Context, pageURL, selector, text string) (string, error) {
	if pageURL == "" {
		return "", fmt.Errorf("browser.type: url is required")
	}
	if selector == "" {
		return "", fmt.Errorf("browser.type: selector is required")
	}
	if text == "" {
		return "", fmt.Errorf("browser.type: text is required")
	}
	if err := ic.checkCrossDomain(pageURL); err != nil {
		return "", err
	}

	// NeverHandlePasswords: refuse if selector targets a password field.
	lower := strings.ToLower(selector)
	if strings.Contains(lower, "password") || strings.Contains(lower, "[type=\"password\"]") || strings.Contains(lower, "[type='password']") {
		return "", fmt.Errorf("browser.type: refused — cannot type into password fields for security reasons")
	}

	return fmt.Sprintf("Action: type %q into element matching selector %q on %s", text, selector, pageURL), nil
}

// Select describes selecting a dropdown option.
func (ic *InteractiveClient) Select(_ context.Context, pageURL, selector, value string) (string, error) {
	if pageURL == "" {
		return "", fmt.Errorf("browser.select: url is required")
	}
	if selector == "" {
		return "", fmt.Errorf("browser.select: selector is required")
	}
	if value == "" {
		return "", fmt.Errorf("browser.select: value is required")
	}
	if err := ic.checkCrossDomain(pageURL); err != nil {
		return "", err
	}
	return fmt.Sprintf("Action: select value %q in element matching selector %q on %s", value, selector, pageURL), nil
}

// SubmitResult holds the result of a submit action.
type SubmitResult struct {
	Status      string `json:"status"`
	Description string `json:"description"`
	Confirmed   bool   `json:"confirmed"`
}

// Submit describes submitting a form. Requires a second call with confirmed=true.
func (ic *InteractiveClient) Submit(_ context.Context, pageURL, selector string, confirmed bool) (string, error) {
	if pageURL == "" {
		return "", fmt.Errorf("browser.submit: url is required")
	}
	if selector == "" {
		return "", fmt.Errorf("browser.submit: selector is required")
	}
	if err := ic.checkCrossDomain(pageURL); err != nil {
		return "", err
	}

	if !confirmed {
		result := SubmitResult{
			Status:      "confirmation_required",
			Description: fmt.Sprintf("Confirmation required: submit form matching selector %q on %s. Call again with confirmed=true to proceed.", selector, pageURL),
			Confirmed:   false,
		}
		out, _ := json.Marshal(result)
		return string(out), nil
	}

	result := SubmitResult{
		Status:      "submitted",
		Description: fmt.Sprintf("Action: submit form matching selector %q on %s", selector, pageURL),
		Confirmed:   true,
	}
	out, _ := json.Marshal(result)
	return string(out), nil
}

// SandboxFetch fetches a URL with no cookies and no JavaScript (static HTML only).
func (ic *InteractiveClient) SandboxFetch(ctx context.Context, rawURL string, client *Client) (string, string, error) {
	if rawURL == "" {
		return "", "", fmt.Errorf("browser.sandbox: url is required")
	}
	// Delegate to the existing headless Navigate which already fetches static HTML.
	return client.Navigate(ctx, rawURL)
}

// checkCrossDomain validates that the target URL doesn't cross domain boundaries.
func (ic *InteractiveClient) checkCrossDomain(rawURL string) error {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("browser: invalid URL: %w", err)
	}
	domain := parsed.Host
	if domain == "" {
		return fmt.Errorf("browser: URL must include a host")
	}

	if ic.lastDomain == "" {
		ic.lastDomain = domain
		return nil
	}

	if ic.lastDomain != domain {
		return fmt.Errorf("browser: cross-domain navigation blocked (from %s to %s) — use a new session for different domains", ic.lastDomain, domain)
	}
	return nil
}

// ResetDomain clears the tracked domain (for starting a new session).
func (ic *InteractiveClient) ResetDomain() {
	ic.lastDomain = ""
}

// RegisterInteractiveBrowserTools registers browser.click, browser.type, browser.select, and browser.submit tools.
func RegisterInteractiveBrowserTools(registry toolRegistry, ic *InteractiveClient) {
	registry.Register(
		"browser.click",
		"Click an element on a web page by CSS selector (returns action description).",
		`{"type":"object","properties":{"url":{"type":"string","description":"Page URL"},"selector":{"type":"string","description":"CSS selector of element to click"}},"required":["url","selector"]}`,
		"tool.web.interact",
		func(ctx context.Context, input string) (string, error) {
			var args struct {
				URL      string `json:"url"`
				Selector string `json:"selector"`
			}
			if err := json.Unmarshal([]byte(input), &args); err != nil {
				return "", fmt.Errorf("browser.click: invalid input: %w", err)
			}
			result, err := ic.Click(ctx, args.URL, args.Selector)
			if err != nil {
				return "", err
			}
			out, _ := json.Marshal(map[string]string{"result": result})
			return string(out), nil
		},
	)

	registry.Register(
		"browser.type",
		"Type text into an input field on a web page (refuses password fields).",
		`{"type":"object","properties":{"url":{"type":"string","description":"Page URL"},"selector":{"type":"string","description":"CSS selector of input element"},"text":{"type":"string","description":"Text to type"}},"required":["url","selector","text"]}`,
		"tool.web.interact",
		func(ctx context.Context, input string) (string, error) {
			var args struct {
				URL      string `json:"url"`
				Selector string `json:"selector"`
				Text     string `json:"text"`
			}
			if err := json.Unmarshal([]byte(input), &args); err != nil {
				return "", fmt.Errorf("browser.type: invalid input: %w", err)
			}
			result, err := ic.Type(ctx, args.URL, args.Selector, args.Text)
			if err != nil {
				return "", err
			}
			out, _ := json.Marshal(map[string]string{"result": result})
			return string(out), nil
		},
	)

	registry.Register(
		"browser.select",
		"Select a dropdown option on a web page.",
		`{"type":"object","properties":{"url":{"type":"string","description":"Page URL"},"selector":{"type":"string","description":"CSS selector of select element"},"value":{"type":"string","description":"Value to select"}},"required":["url","selector","value"]}`,
		"tool.web.interact",
		func(ctx context.Context, input string) (string, error) {
			var args struct {
				URL      string `json:"url"`
				Selector string `json:"selector"`
				Value    string `json:"value"`
			}
			if err := json.Unmarshal([]byte(input), &args); err != nil {
				return "", fmt.Errorf("browser.select: invalid input: %w", err)
			}
			result, err := ic.Select(ctx, args.URL, args.Selector, args.Value)
			if err != nil {
				return "", err
			}
			out, _ := json.Marshal(map[string]string{"result": result})
			return string(out), nil
		},
	)

	registry.Register(
		"browser.submit",
		"Submit a form on a web page (requires confirmation: first call returns confirmation prompt, second call with confirmed=true executes).",
		`{"type":"object","properties":{"url":{"type":"string","description":"Page URL"},"selector":{"type":"string","description":"CSS selector of form element"},"confirmed":{"type":"boolean","description":"Set to true to confirm and execute the submit"}},"required":["url","selector"]}`,
		"tool.web.interact",
		func(ctx context.Context, input string) (string, error) {
			var args struct {
				URL       string `json:"url"`
				Selector  string `json:"selector"`
				Confirmed bool   `json:"confirmed"`
			}
			if err := json.Unmarshal([]byte(input), &args); err != nil {
				return "", fmt.Errorf("browser.submit: invalid input: %w", err)
			}
			return ic.Submit(ctx, args.URL, args.Selector, args.Confirmed)
		},
	)
}
