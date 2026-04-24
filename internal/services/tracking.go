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

// TrackingProvider fetches package tracking information.
type TrackingProvider struct {
	apiKey string
	client *http.Client
}

// NewTrackingProvider creates a tracking provider.
// If apiKey is empty, methods return stub responses.
func NewTrackingProvider(apiKey string, client *http.Client) *TrackingProvider {
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	return &TrackingProvider{apiKey: apiKey, client: client}
}

// TrackPackage returns the tracking status for a package.
func (p *TrackingProvider) TrackPackage(ctx context.Context, trackingNumber, carrier string) (string, error) {
	if p.apiKey == "" {
		result := map[string]interface{}{
			"tracking_number": trackingNumber,
			"carrier":         carrier,
			"status":          "unknown",
			"events":          []interface{}{},
			"note":            "Configure via: aibutler vault set tracking_api_key <key>",
		}
		out, _ := json.MarshalIndent(result, "", "  ")
		return string(out), nil
	}

	endpoint := "https://api.17track.net/track/v2/gettrackinfo"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", fmt.Errorf("tracking: build request: %w", err)
	}
	q := req.URL.Query()
	q.Set("key", p.apiKey)
	q.Set("number", url.QueryEscape(trackingNumber))
	if carrier != "" {
		q.Set("carrier", url.QueryEscape(carrier))
	}
	req.URL.RawQuery = q.Encode()

	resp, err := p.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("tracking: request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("tracking: read response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("tracking: API returned %d: %s", resp.StatusCode, string(body))
	}

	var parsed interface{}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return string(body), nil
	}
	out, _ := json.MarshalIndent(map[string]interface{}{
		"tracking_number": trackingNumber,
		"carrier":         carrier,
		"data":            parsed,
	}, "", "  ")
	return string(out), nil
}
