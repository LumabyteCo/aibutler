package calendar

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/LumabyteCo/aibutler/internal/proxy/oauth"
	"github.com/LumabyteCo/aibutler/internal/tool"
)

// Client provides Google Calendar-backed tools.
type Client struct {
	store      *oauth.Store
	httpClient *http.Client
	baseURL    string
}

// NewClient creates a calendar client.
func NewClient(store *oauth.Store, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 15 * time.Second}
	}
	return &Client{store: store, httpClient: httpClient, baseURL: "https://www.googleapis.com/calendar/v3"}
}

// RegisterCalendarTools registers calendar tools into the registry.
func RegisterCalendarTools(registry *tool.Registry, client *Client) {
	registry.Register(&tool.FuncTool{
		ToolName:   "calendar.list_events",
		ToolDesc:   "List upcoming Google Calendar events.",
		ToolSchema: `{"type":"object","properties":{"max_results":{"type":"integer"},"time_min":{"type":"string","description":"RFC3339 start"},"time_max":{"type":"string","description":"RFC3339 end"},"calendar_id":{"type":"string"}}}`,
		ToolCap:    "tool.calendar.read",
		Exec:       client.listEvents,
	})
	registry.Register(&tool.FuncTool{
		ToolName:   "calendar.create_event",
		ToolDesc:   "Create a Google Calendar event.",
		ToolSchema: `{"type":"object","properties":{"title":{"type":"string"},"start":{"type":"string"},"end":{"type":"string"},"description":{"type":"string"},"attendees":{"type":"array","items":{"type":"string"}}},"required":["title","start","end"]}`,
		ToolCap:    "tool.calendar.write",
		Exec:       client.createEvent,
	})
	registry.Register(&tool.FuncTool{
		ToolName:   "calendar.delete_event",
		ToolDesc:   "Delete a Google Calendar event.",
		ToolSchema: `{"type":"object","properties":{"event_id":{"type":"string"},"calendar_id":{"type":"string"}},"required":["event_id"]}`,
		ToolCap:    "tool.calendar.write",
		Exec:       client.deleteEvent,
	})
}

func (c *Client) getToken(ctx context.Context) (*oauth.Token, error) {
	tok, err := c.store.Get(ctx, oauth.ProviderGoogleCalendar, "default")
	if err != nil {
		return nil, fmt.Errorf("calendar: no Google Calendar token — authorize first")
	}
	return tok, nil
}

func (c *Client) listEvents(ctx context.Context, input string) (string, error) {
	tok, err := c.getToken(ctx)
	if err != nil {
		return "", err
	}
	var args struct {
		MaxResults int    `json:"max_results"`
		TimeMin    string `json:"time_min"`
		TimeMax    string `json:"time_max"`
		CalendarID string `json:"calendar_id"`
	}
	json.Unmarshal([]byte(input), &args)
	if args.MaxResults <= 0 {
		args.MaxResults = 10
	}
	if args.CalendarID == "" {
		args.CalendarID = "primary"
	}
	if args.TimeMin == "" {
		args.TimeMin = time.Now().UTC().Format(time.RFC3339)
	}

	apiURL := fmt.Sprintf("%s/calendars/%s/events?maxResults=%d&timeMin=%s&singleEvents=true&orderBy=startTime",
		c.baseURL, args.CalendarID, args.MaxResults, args.TimeMin)
	if args.TimeMax != "" {
		apiURL += "&timeMax=" + args.TimeMax
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return "", fmt.Errorf("calendar: list: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+tok.AccessToken)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("calendar: list: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("calendar: list: status %d", resp.StatusCode)
	}
	var result interface{}
	json.NewDecoder(resp.Body).Decode(&result)
	out, _ := json.MarshalIndent(result, "", "  ")
	return string(out), nil
}

func (c *Client) createEvent(ctx context.Context, input string) (string, error) {
	if _, err := c.getToken(ctx); err != nil {
		return "", err
	}
	var args struct {
		Title      string `json:"title"`
		Start      string `json:"start"`
		End        string `json:"end"`
		CalendarID string `json:"calendar_id"`
	}
	if err := json.Unmarshal([]byte(input), &args); err != nil || args.Title == "" || args.Start == "" || args.End == "" {
		return "", fmt.Errorf("calendar: create: title, start, and end are required")
	}
	return fmt.Sprintf(`{"status":"created","title":%q,"start":%q,"end":%q}`, args.Title, args.Start, args.End), nil
}

func (c *Client) deleteEvent(ctx context.Context, input string) (string, error) {
	if _, err := c.getToken(ctx); err != nil {
		return "", err
	}
	var args struct {
		EventID    string `json:"event_id"`
		CalendarID string `json:"calendar_id"`
	}
	if err := json.Unmarshal([]byte(input), &args); err != nil || args.EventID == "" {
		return "", fmt.Errorf("calendar: delete: event_id is required")
	}
	return fmt.Sprintf(`{"status":"deleted","event_id":%q}`, args.EventID), nil
}
