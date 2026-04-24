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

// RecipeProvider searches for recipes.
type RecipeProvider struct {
	apiKey string
	client *http.Client
}

// NewRecipeProvider creates a recipe provider.
// If apiKey is empty, methods return stub responses.
func NewRecipeProvider(apiKey string, client *http.Client) *RecipeProvider {
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	return &RecipeProvider{apiKey: apiKey, client: client}
}

// Search returns recipes matching the query.
func (p *RecipeProvider) Search(ctx context.Context, query string) (string, error) {
	if p.apiKey == "" {
		result := map[string]interface{}{
			"query":   query,
			"recipes": []interface{}{},
			"note":    "Configure via: aibutler vault set recipe_api_key <key>",
		}
		out, _ := json.MarshalIndent(result, "", "  ")
		return string(out), nil
	}

	endpoint := "https://api.spoonacular.com/recipes/complexSearch"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", fmt.Errorf("recipe: build request: %w", err)
	}
	q := req.URL.Query()
	q.Set("apiKey", p.apiKey)
	q.Set("query", url.QueryEscape(query))
	q.Set("number", "5")
	req.URL.RawQuery = q.Encode()

	resp, err := p.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("recipe: request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("recipe: read response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("recipe: API returned %d: %s", resp.StatusCode, string(body))
	}

	var parsed struct {
		Results []interface{} `json:"results"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return string(body), nil
	}
	result := map[string]interface{}{
		"query":   query,
		"recipes": parsed.Results,
	}
	out, _ := json.MarshalIndent(result, "", "  ")
	return string(out), nil
}
