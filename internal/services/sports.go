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

// SportsProvider fetches sports scores and standings.
type SportsProvider struct {
	apiKey string
	client *http.Client
}

// NewSportsProvider creates a sports provider.
// If apiKey is empty, methods return stub responses.
func NewSportsProvider(apiKey string, client *http.Client) *SportsProvider {
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	return &SportsProvider{apiKey: apiKey, client: client}
}

// Scores returns recent scores for the given sport and league.
func (p *SportsProvider) Scores(ctx context.Context, sport, league string) (string, error) {
	if p.apiKey == "" {
		result := map[string]interface{}{
			"sport":  sport,
			"league": league,
			"note":   "Configure a sports API key via: aibutler vault set sports_api_key <key>",
			"scores": []interface{}{},
		}
		out, _ := json.MarshalIndent(result, "", "  ")
		return string(out), nil
	}

	date := time.Now().Format("2006-JAN-02")
	endpoint := fmt.Sprintf("https://api.sportsdata.io/v3/%s/scores/json/GamesByDate/%s",
		url.PathEscape(sport), url.PathEscape(date))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", fmt.Errorf("sports: build request: %w", err)
	}
	q := req.URL.Query()
	q.Set("key", p.apiKey)
	req.URL.RawQuery = q.Encode()

	resp, err := p.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("sports: request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("sports: read response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("sports: API returned %d: %s", resp.StatusCode, string(body))
	}

	// Parse as array of games and return top 5.
	var games []map[string]interface{}
	if err := json.Unmarshal(body, &games); err != nil {
		// Return raw if not an array.
		return string(body), nil
	}

	limit := 5
	if len(games) < limit {
		limit = len(games)
	}
	result := map[string]interface{}{
		"sport":  sport,
		"league": league,
		"scores": games[:limit],
	}
	out, _ := json.MarshalIndent(result, "", "  ")
	return string(out), nil
}
