package services

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
)

// NewsProvider fetches headlines from NewsAPI.
type NewsProvider struct {
	apiKey string
	client *http.Client
}

// NewNewsProvider creates a news provider.
func NewNewsProvider(apiKey string, client *http.Client) *NewsProvider {
	if client == nil {
		client = http.DefaultClient
	}
	return &NewsProvider{apiKey: apiKey, client: client}
}

// Headlines returns top headlines for a query or category.
func (n *NewsProvider) Headlines(ctx context.Context, query, category, country string) (string, error) {
	u := "https://newsapi.org/v2/top-headlines?"
	params := url.Values{}
	params.Set("apiKey", n.apiKey)
	if query != "" {
		params.Set("q", query)
	}
	if category != "" {
		params.Set("category", category)
	}
	if country != "" {
		params.Set("country", country)
	} else {
		params.Set("country", "us")
	}
	params.Set("pageSize", "5")

	req, err := http.NewRequestWithContext(ctx, "GET", u+params.Encode(), nil)
	if err != nil {
		return "", fmt.Errorf("news: %w", err)
	}

	resp, err := n.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("news: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("news: read body: %w", err)
	}

	if resp.StatusCode != 200 {
		return "", fmt.Errorf("news: API returned %d: %s", resp.StatusCode, string(body))
	}

	var data struct {
		Articles []struct {
			Title       string `json:"title"`
			Source      struct{ Name string } `json:"source"`
			Description string `json:"description"`
			URL         string `json:"url"`
			PublishedAt string `json:"publishedAt"`
		} `json:"articles"`
	}
	if err := json.Unmarshal(body, &data); err != nil {
		return "", fmt.Errorf("news: parse: %w", err)
	}

	type article struct {
		Title       string `json:"title"`
		Source      string `json:"source"`
		Description string `json:"description"`
		URL         string `json:"url"`
	}

	var articles []article
	for _, a := range data.Articles {
		articles = append(articles, article{
			Title:       a.Title,
			Source:      a.Source.Name,
			Description: a.Description,
			URL:         a.URL,
		})
	}

	out, _ := json.Marshal(articles)
	return string(out), nil
}
