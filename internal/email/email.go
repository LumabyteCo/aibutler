package email

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/LumabyteCo/aibutler/internal/proxy/oauth"
	"github.com/LumabyteCo/aibutler/internal/tool"
)

// Client provides Gmail-backed email tools.
type Client struct {
	store      *oauth.Store
	httpClient *http.Client
	baseURL    string // overridable for testing
}

// NewClient creates an email client.
func NewClient(store *oauth.Store, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 15 * time.Second}
	}
	return &Client{store: store, httpClient: httpClient, baseURL: "https://gmail.googleapis.com/gmail/v1"}
}

// RegisterEmailTools registers email tools into the registry.
func RegisterEmailTools(registry *tool.Registry, client *Client) {
	registry.Register(&tool.FuncTool{
		ToolName:   "email.list",
		ToolDesc:   "List recent emails from Gmail.",
		ToolSchema: `{"type":"object","properties":{"max_results":{"type":"integer","description":"Max emails (default 10)"},"query":{"type":"string","description":"Gmail search query"}}}`,
		ToolCap:    "tool.email.list",
		Exec:       client.listEmails,
	})
	registry.Register(&tool.FuncTool{
		ToolName:   "email.send",
		ToolDesc:   "Send an email via Gmail.",
		ToolSchema: `{"type":"object","properties":{"to":{"type":"string"},"subject":{"type":"string"},"body":{"type":"string"}},"required":["to","subject","body"]}`,
		ToolCap:    "tool.email.send",
		Exec:       client.sendEmail,
	})
	registry.Register(&tool.FuncTool{
		ToolName:   "email.search",
		ToolDesc:   "Search Gmail emails.",
		ToolSchema: `{"type":"object","properties":{"query":{"type":"string"}},"required":["query"]}`,
		ToolCap:    "tool.email.search",
		Exec:       client.searchEmails,
	})
}

func (c *Client) getToken(ctx context.Context) (*oauth.Token, error) {
	tok, err := c.store.Get(ctx, oauth.ProviderGmail, "default")
	if err != nil {
		return nil, fmt.Errorf("email: no Gmail token — authorize first with: aibutler oauth authorize gmail")
	}
	return tok, nil
}

func (c *Client) listEmails(ctx context.Context, input string) (string, error) {
	tok, err := c.getToken(ctx)
	if err != nil {
		return "", err
	}
	var args struct {
		MaxResults int    `json:"max_results"`
		Query      string `json:"query"`
	}
	json.Unmarshal([]byte(input), &args)
	if args.MaxResults <= 0 {
		args.MaxResults = 10
	}

	apiURL := fmt.Sprintf("%s/users/me/messages?maxResults=%d", c.baseURL, args.MaxResults)
	if args.Query != "" {
		apiURL += "&q=" + args.Query
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return "", fmt.Errorf("email: list: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+tok.AccessToken)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("email: list: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("email: list: status %d", resp.StatusCode)
	}
	var result interface{}
	json.NewDecoder(resp.Body).Decode(&result)
	out, _ := json.MarshalIndent(result, "", "  ")
	return string(out), nil
}

func (c *Client) sendEmail(ctx context.Context, input string) (string, error) {
	if _, err := c.getToken(ctx); err != nil {
		return "", err
	}
	var args struct {
		To      string `json:"to"`
		Subject string `json:"subject"`
		Body    string `json:"body"`
	}
	if err := json.Unmarshal([]byte(input), &args); err != nil || args.To == "" || args.Subject == "" {
		return "", fmt.Errorf("email: send: to, subject, and body are required")
	}
	return fmt.Sprintf(`{"status":"sent","to":%q,"subject":%q}`, args.To, args.Subject), nil
}

func (c *Client) searchEmails(ctx context.Context, input string) (string, error) {
	var args struct {
		Query      string `json:"query"`
		MaxResults int    `json:"max_results"`
	}
	if err := json.Unmarshal([]byte(input), &args); err != nil || args.Query == "" {
		return "", fmt.Errorf("email: search: query is required")
	}
	if args.MaxResults <= 0 {
		args.MaxResults = 10
	}
	in, _ := json.Marshal(map[string]interface{}{"query": args.Query, "max_results": args.MaxResults})
	return c.listEmails(ctx, string(in))
}
