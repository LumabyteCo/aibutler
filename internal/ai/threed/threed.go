// Package threed provides AI 3D model generation tools (Meshy, Tripo, Luma).
package threed

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

// MeshyProvider generates 3D models via the Meshy API.
type MeshyProvider struct {
	apiKey     string
	httpClient *http.Client
}

// NewMeshy creates a new MeshyProvider.
func NewMeshy(apiKey string) *MeshyProvider {
	return &MeshyProvider{apiKey: apiKey, httpClient: &http.Client{}}
}

// SetHTTPClient overrides the HTTP client (useful for testing).
func (p *MeshyProvider) SetHTTPClient(c *http.Client) { p.httpClient = c }

// Generate creates a 3D model from a text prompt.
func (p *MeshyProvider) Generate(ctx context.Context, prompt string) (string, error) {
	if p.apiKey == "" {
		return "", fmt.Errorf("Meshy API key not configured. Run: aibutler vault set meshy_api_key YOUR_KEY")
	}

	reqBody := map[string]interface{}{
		"mode":        "preview",
		"prompt":      prompt,
		"art_style":   "realistic",
		"negative_prompt": "",
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("meshy: marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.meshy.ai/v1/text-to-3d", bytes.NewReader(bodyBytes))
	if err != nil {
		return "", fmt.Errorf("meshy: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+p.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("meshy: request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("meshy: read response: %w", err)
	}
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return "", fmt.Errorf("meshy: API returned %d: %s", resp.StatusCode, string(respBody))
	}

	var result map[string]interface{}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return string(respBody), nil
	}
	result["provider"] = "meshy"
	out, _ := json.MarshalIndent(result, "", "  ")
	return string(out), nil
}

// TripoProvider generates 3D models via the Tripo API.
type TripoProvider struct {
	apiKey     string
	httpClient *http.Client
}

// NewTripo creates a new TripoProvider.
func NewTripo(apiKey string) *TripoProvider {
	return &TripoProvider{apiKey: apiKey, httpClient: &http.Client{}}
}

// SetHTTPClient overrides the HTTP client (useful for testing).
func (p *TripoProvider) SetHTTPClient(c *http.Client) { p.httpClient = c }

// Generate creates a 3D model from a text prompt.
func (p *TripoProvider) Generate(ctx context.Context, prompt string) (string, error) {
	if p.apiKey == "" {
		return "", fmt.Errorf("Tripo API key not configured. Run: aibutler vault set tripo_api_key YOUR_KEY")
	}

	reqBody := map[string]interface{}{
		"type":   "text_to_model",
		"prompt": prompt,
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("tripo: marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.tripo3d.ai/v2/openapi/task", bytes.NewReader(bodyBytes))
	if err != nil {
		return "", fmt.Errorf("tripo: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+p.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("tripo: request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("tripo: read response: %w", err)
	}
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return "", fmt.Errorf("tripo: API returned %d: %s", resp.StatusCode, string(respBody))
	}

	var result map[string]interface{}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return string(respBody), nil
	}
	result["provider"] = "tripo"
	out, _ := json.MarshalIndent(result, "", "  ")
	return string(out), nil
}

// LumaProvider generates 3D models via the Luma AI API.
type LumaProvider struct {
	apiKey     string
	httpClient *http.Client
}

// NewLuma creates a new LumaProvider.
func NewLuma(apiKey string) *LumaProvider {
	return &LumaProvider{apiKey: apiKey, httpClient: &http.Client{}}
}

// SetHTTPClient overrides the HTTP client (useful for testing).
func (p *LumaProvider) SetHTTPClient(c *http.Client) { p.httpClient = c }

// Generate creates a 3D model from a text prompt.
func (p *LumaProvider) Generate(ctx context.Context, prompt string) (string, error) {
	if p.apiKey == "" {
		return "", fmt.Errorf("Luma API key not configured. Run: aibutler vault set luma_api_key YOUR_KEY")
	}

	reqBody := map[string]interface{}{
		"prompt": prompt,
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("luma: marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://webapp.engineeringlumalabs.com/api/v1/generations", bytes.NewReader(bodyBytes))
	if err != nil {
		return "", fmt.Errorf("luma: build request: %w", err)
	}
	req.Header.Set("Authorization", "luma-api-key="+p.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("luma: request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("luma: read response: %w", err)
	}
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return "", fmt.Errorf("luma: API returned %d: %s", resp.StatusCode, string(respBody))
	}

	var result map[string]interface{}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return string(respBody), nil
	}
	result["provider"] = "luma"
	out, _ := json.MarshalIndent(result, "", "  ")
	return string(out), nil
}

// RegisterThreeDTools registers 3D model generation tools.
func RegisterThreeDTools(registry toolRegistry, meshy *MeshyProvider, tripo *TripoProvider, luma *LumaProvider) {
	registry.Register(
		"3d.generate.meshy",
		"Generate a 3D model using Meshy AI.",
		`{"type":"object","properties":{"prompt":{"type":"string","description":"3D model description"}},"required":["prompt"]}`,
		"tool.ai.3d",
		func(ctx context.Context, input string) (string, error) {
			var args struct {
				Prompt string `json:"prompt"`
			}
			if err := json.Unmarshal([]byte(input), &args); err != nil {
				return "", fmt.Errorf("invalid input: %w", err)
			}
			return meshy.Generate(ctx, args.Prompt)
		},
	)

	registry.Register(
		"3d.generate.tripo",
		"Generate a 3D model using Tripo AI.",
		`{"type":"object","properties":{"prompt":{"type":"string","description":"3D model description"}},"required":["prompt"]}`,
		"tool.ai.3d",
		func(ctx context.Context, input string) (string, error) {
			var args struct {
				Prompt string `json:"prompt"`
			}
			if err := json.Unmarshal([]byte(input), &args); err != nil {
				return "", fmt.Errorf("invalid input: %w", err)
			}
			return tripo.Generate(ctx, args.Prompt)
		},
	)

	registry.Register(
		"3d.generate.luma",
		"Generate a 3D model using Luma AI.",
		`{"type":"object","properties":{"prompt":{"type":"string","description":"3D model description"}},"required":["prompt"]}`,
		"tool.ai.3d",
		func(ctx context.Context, input string) (string, error) {
			var args struct {
				Prompt string `json:"prompt"`
			}
			if err := json.Unmarshal([]byte(input), &args); err != nil {
				return "", fmt.Errorf("invalid input: %w", err)
			}
			return luma.Generate(ctx, args.Prompt)
		},
	)
}
