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

// TransitProvider fetches transit departure times.
type TransitProvider struct {
	apiKey string
	client *http.Client
}

// NewTransitProvider creates a transit provider.
// If apiKey is empty, methods return stub responses.
func NewTransitProvider(apiKey string, client *http.Client) *TransitProvider {
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	return &TransitProvider{apiKey: apiKey, client: client}
}

// NextDepartures returns upcoming departures for a stop and line.
func (p *TransitProvider) NextDepartures(ctx context.Context, stop, line string) (string, error) {
	if p.apiKey == "" {
		result := map[string]interface{}{
			"stop":       stop,
			"line":       line,
			"note":       "Configure via: aibutler vault set transit_api_key <key>",
			"departures": []interface{}{},
		}
		out, _ := json.MarshalIndent(result, "", "  ")
		return string(out), nil
	}

	endpoint := "https://api.511.org/transit/StopMonitoring"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", fmt.Errorf("transit: build request: %w", err)
	}
	q := req.URL.Query()
	q.Set("api_key", p.apiKey)
	q.Set("stopCode", url.QueryEscape(stop))
	if line != "" {
		q.Set("lineRef", url.QueryEscape(line))
	}
	q.Set("format", "json")
	req.URL.RawQuery = q.Encode()

	resp, err := p.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("transit: request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("transit: read response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("transit: API returned %d: %s", resp.StatusCode, string(body))
	}

	// Return the parsed JSON.
	var parsed interface{}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return string(body), nil
	}
	out, _ := json.MarshalIndent(map[string]interface{}{
		"stop":       stop,
		"line":       line,
		"departures": parsed,
	}, "", "  ")
	return string(out), nil
}
