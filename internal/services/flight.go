package services

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

// FlightProvider fetches flight status information.
type FlightProvider struct {
	apiKey string
	client *http.Client
}

// NewFlightProvider creates a flight provider.
// If apiKey is empty, methods return stub responses.
func NewFlightProvider(apiKey string, client *http.Client) *FlightProvider {
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	return &FlightProvider{apiKey: apiKey, client: client}
}

// FlightStatus returns the status of a flight by number.
func (p *FlightProvider) FlightStatus(ctx context.Context, flightNumber string) (string, error) {
	if p.apiKey == "" {
		result := map[string]interface{}{
			"flight": flightNumber,
			"status": "unknown",
			"note":   "Configure via: aibutler vault set flight_api_key <key>",
		}
		out, _ := json.MarshalIndent(result, "", "  ")
		return string(out), nil
	}

	endpoint := "https://aviation-edge.com/v2/public/flights"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", fmt.Errorf("flight: build request: %w", err)
	}
	q := req.URL.Query()
	q.Set("key", p.apiKey)
	q.Set("flight", url.QueryEscape(flightNumber))
	req.URL.RawQuery = q.Encode()

	resp, err := p.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("flight: request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("flight: read response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("flight: API returned %d: %s", resp.StatusCode, string(body))
	}

	// Parse as array and return the first match.
	var flights []map[string]interface{}
	if err := json.Unmarshal(body, &flights); err != nil {
		return string(body), nil
	}
	if len(flights) == 0 {
		return fmt.Sprintf(`{"flight":%q,"status":"not_found"}`, flightNumber), nil
	}
	out, _ := json.MarshalIndent(flights[0], "", "  ")
	return string(out), nil
}
