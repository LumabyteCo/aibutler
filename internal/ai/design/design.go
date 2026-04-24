// Package design provides AI design generation tools (Canva, Figma).
package design

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// toolRegistry is the narrow interface used by registration functions.
type toolRegistry interface {
	Register(name, description, schema, capability string, exec func(ctx context.Context, input string) (string, error))
}

// CanvaProvider generates designs via the Canva API.
type CanvaProvider struct {
	apiKey     string
	httpClient *http.Client
}

// NewCanva creates a new CanvaProvider.
func NewCanva(apiKey string) *CanvaProvider {
	return &CanvaProvider{
		apiKey:     apiKey,
		httpClient: &http.Client{},
	}
}

// SetHTTPClient overrides the HTTP client (useful for testing).
func (p *CanvaProvider) SetHTTPClient(c *http.Client) { p.httpClient = c }

// Generate creates a design from a prompt and optional template.
func (p *CanvaProvider) Generate(ctx context.Context, prompt, template string) (string, error) {
	if p.apiKey == "" {
		return "", fmt.Errorf("Canva API key not configured. Run: aibutler vault set canva_api_key YOUR_KEY")
	}

	reqBody := map[string]interface{}{
		"design_type": "custom",
		"title":       prompt,
	}
	if template != "" {
		reqBody["template_id"] = template
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("canva: marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.canva.com/rest/v1/designs", bytes.NewReader(bodyBytes))
	if err != nil {
		return "", fmt.Errorf("canva: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+p.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("canva: request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("canva: read response: %w", err)
	}
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return "", fmt.Errorf("canva: API returned %d: %s", resp.StatusCode, string(respBody))
	}

	var result map[string]interface{}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return string(respBody), nil
	}
	result["provider"] = "canva"
	out, _ := json.MarshalIndent(result, "", "  ")
	return string(out), nil
}

// FigmaProvider generates mockups via the Figma API.
type FigmaProvider struct {
	apiKey     string
	httpClient *http.Client
}

// NewFigma creates a new FigmaProvider.
func NewFigma(apiKey string) *FigmaProvider {
	return &FigmaProvider{
		apiKey:     apiKey,
		httpClient: &http.Client{},
	}
}

// SetHTTPClient overrides the HTTP client (useful for testing).
func (p *FigmaProvider) SetHTTPClient(c *http.Client) { p.httpClient = c }

// GenerateMockup creates a mockup from a prompt.
func (p *FigmaProvider) GenerateMockup(ctx context.Context, prompt string) (string, error) {
	if p.apiKey == "" {
		return "", fmt.Errorf("Figma API key not configured. Run: aibutler vault set figma_api_key YOUR_KEY")
	}

	reqBody := map[string]interface{}{
		"name":        prompt,
		"description": prompt,
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("figma: marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.figma.com/v1/files", bytes.NewReader(bodyBytes))
	if err != nil {
		return "", fmt.Errorf("figma: build request: %w", err)
	}
	req.Header.Set("X-Figma-Token", p.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("figma: request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("figma: read response: %w", err)
	}
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return "", fmt.Errorf("figma: API returned %d: %s", resp.StatusCode, string(respBody))
	}

	var result map[string]interface{}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return string(respBody), nil
	}
	result["provider"] = "figma"
	out, _ := json.MarshalIndent(result, "", "  ")
	return string(out), nil
}

// RegisterDesignTools registers design generation tools.
func RegisterDesignTools(registry toolRegistry, canva *CanvaProvider, figma *FigmaProvider) {
	registry.Register(
		"design.generate.canva",
		"Generate a design using Canva AI.",
		`{"type":"object","properties":{"prompt":{"type":"string","description":"Design description"},"template":{"type":"string","description":"Optional Canva template ID"}},"required":["prompt"]}`,
		"tool.ai.design",
		func(ctx context.Context, input string) (string, error) {
			var args struct {
				Prompt   string `json:"prompt"`
				Template string `json:"template"`
			}
			if err := json.Unmarshal([]byte(input), &args); err != nil {
				return "", fmt.Errorf("invalid input: %w", err)
			}
			return canva.Generate(ctx, args.Prompt, args.Template)
		},
	)

	registry.Register(
		"design.generate.figma",
		"Generate a UI mockup using Figma AI.",
		`{"type":"object","properties":{"prompt":{"type":"string","description":"Mockup description"}},"required":["prompt"]}`,
		"tool.ai.design",
		func(ctx context.Context, input string) (string, error) {
			var args struct {
				Prompt string `json:"prompt"`
			}
			if err := json.Unmarshal([]byte(input), &args); err != nil {
				return "", fmt.Errorf("invalid input: %w", err)
			}
			return figma.GenerateMockup(ctx, args.Prompt)
		},
	)
}
