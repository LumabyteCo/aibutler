package model

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/LumabyteCo/aibutler/internal/agent"
)

const geminiBaseURL = "https://generativelanguage.googleapis.com/v1beta/models"

// GeminiAdapter implements agent.ModelAdapter for the Google Gemini API.
type GeminiAdapter struct {
	apiKey      string
	model       string
	client      *http.Client
	retries     int
	baseURL     string
	tools       []agent.ToolDef
	maxTokens   int
	temperature float64
}

// NewGemini creates a Gemini model adapter.
func NewGemini(apiKey, model string, timeout time.Duration, retries int) *GeminiAdapter {
	if model == "" {
		model = "gemini-2.0-flash"
	}
	return &GeminiAdapter{
		apiKey:      apiKey,
		model:       model,
		client:      &http.Client{Timeout: timeout},
		retries:     retries,
		baseURL:     geminiBaseURL,
		maxTokens:   8192,
		temperature: 0.7,
	}
}

// NewGeminiWithBaseURL creates a Gemini adapter with a custom base URL (for testing).
func NewGeminiWithBaseURL(apiKey, modelName string, timeout time.Duration, retries int, baseURL string) *GeminiAdapter {
	g := NewGemini(apiKey, modelName, timeout, retries)
	g.baseURL = baseURL
	return g
}

// SetMaxTokens overrides the default max output tokens.
func (g *GeminiAdapter) SetMaxTokens(n int) {
	if n > 0 {
		g.maxTokens = n
	}
}

// SetTemperature overrides the default temperature.
func (g *GeminiAdapter) SetTemperature(t float64) {
	g.temperature = t
}

// SetHTTPClient replaces the adapter's HTTP client (e.g., with a pooled client).
func (g *GeminiAdapter) SetHTTPClient(client *http.Client) {
	if client != nil {
		g.client = client
	}
}

// SetTools sets tool definitions for subsequent Complete calls.
func (g *GeminiAdapter) SetTools(tools []agent.ToolDef) {
	g.tools = tools
}

// internal Gemini API types
type geminiPart struct {
	Text             string           `json:"text,omitempty"`
	FunctionCall     *geminiFuncCall  `json:"functionCall,omitempty"`
	FunctionResponse *geminiFuncResp  `json:"functionResponse,omitempty"`
}

type geminiFuncCall struct {
	Name string          `json:"name"`
	Args json.RawMessage `json:"args"`
}

type geminiFuncResp struct {
	Name     string          `json:"name"`
	Response json.RawMessage `json:"response"`
}

type geminiContent struct {
	Role  string       `json:"role"`
	Parts []geminiPart `json:"parts"`
}

type geminiFuncDecl struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
}

type geminiTool struct {
	FunctionDeclarations []geminiFuncDecl `json:"functionDeclarations"`
}

type geminiGenConfig struct {
	MaxOutputTokens int     `json:"maxOutputTokens,omitempty"`
	Temperature     float64 `json:"temperature,omitempty"`
}

type geminiRequest struct {
	Contents          []geminiContent `json:"contents"`
	Tools             []geminiTool    `json:"tools,omitempty"`
	GenerationConfig  geminiGenConfig `json:"generationConfig"`
	SystemInstruction *geminiContent  `json:"systemInstruction,omitempty"`
}

type geminiResponse struct {
	Candidates []struct {
		Content geminiContent `json:"content"`
	} `json:"candidates"`
	UsageMetadata struct {
		PromptTokenCount     int `json:"promptTokenCount"`
		CandidatesTokenCount int `json:"candidatesTokenCount"`
	} `json:"usageMetadata"`
}

// Complete sends messages to the Gemini API and returns the response.
func (g *GeminiAdapter) Complete(ctx context.Context, messages []agent.Message) (agent.Response, error) {
	body, err := g.buildRequest(messages)
	if err != nil {
		return agent.Response{}, fmt.Errorf("gemini: build request: %w", err)
	}

	apiURL := fmt.Sprintf("%s/%s:generateContent?key=%s", g.baseURL, g.model, g.apiKey)

	var lastErr error
	for attempt := 0; attempt <= g.retries; attempt++ {
		if attempt > 0 {
			wait := time.Duration(math.Pow(2, float64(attempt-1))) * 500 * time.Millisecond
			select {
			case <-ctx.Done():
				return agent.Response{}, ctx.Err()
			case <-time.After(wait):
			}
		}
		resp, err := g.doRequest(ctx, apiURL, body)
		if err != nil {
			lastErr = err
			continue
		}
		return resp, nil
	}
	return agent.Response{}, fmt.Errorf("gemini: all %d attempts failed: %w", g.retries+1, lastErr)
}

func (g *GeminiAdapter) buildRequest(messages []agent.Message) ([]byte, error) {
	var contents []geminiContent
	var sysInstruction *geminiContent

	for _, msg := range messages {
		switch msg.Role {
		case "system":
			sysInstruction = &geminiContent{
				Parts: []geminiPart{{Text: msg.Content}},
			}
		case "user":
			contents = append(contents, geminiContent{
				Role:  "user",
				Parts: []geminiPart{{Text: msg.Content}},
			})
		case "assistant":
			if len(msg.ToolCalls) > 0 {
				var parts []geminiPart
				if msg.Content != "" {
					parts = append(parts, geminiPart{Text: msg.Content})
				}
				for _, tc := range msg.ToolCalls {
					parts = append(parts, geminiPart{
						FunctionCall: &geminiFuncCall{
							Name: sanitizeToolName(tc.Name),
							Args: json.RawMessage(tc.Input),
						},
					})
				}
				contents = append(contents, geminiContent{Role: "model", Parts: parts})
			} else {
				contents = append(contents, geminiContent{
					Role:  "model",
					Parts: []geminiPart{{Text: msg.Content}},
				})
			}
		case "tool":
			respJSON, _ := json.Marshal(map[string]string{"output": msg.Content})
			contents = append(contents, geminiContent{
				Role: "user",
				Parts: []geminiPart{{
					FunctionResponse: &geminiFuncResp{
						Name:     sanitizeToolName(msg.ToolID),
						Response: respJSON,
					},
				}},
			})
		}
	}

	req := geminiRequest{
		Contents: contents,
		GenerationConfig: geminiGenConfig{
			MaxOutputTokens: g.maxTokens,
			Temperature:     g.temperature,
		},
		SystemInstruction: sysInstruction,
	}

	if len(g.tools) > 0 {
		var decls []geminiFuncDecl
		for _, t := range g.tools {
			decls = append(decls, geminiFuncDecl{
				Name:        sanitizeToolName(t.Name),
				Description: t.Description,
				Parameters:  json.RawMessage(t.Schema),
			})
		}
		req.Tools = []geminiTool{{FunctionDeclarations: decls}}
	}

	return json.Marshal(req)
}

func (g *GeminiAdapter) doRequest(ctx context.Context, apiURL string, body []byte) (agent.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiURL, bytes.NewReader(body))
	if err != nil {
		return agent.Response{}, fmt.Errorf("gemini: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := g.client.Do(req)
	if err != nil {
		return agent.Response{}, fmt.Errorf("gemini: http: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return agent.Response{}, fmt.Errorf("gemini: read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		if resp.StatusCode == 429 || resp.StatusCode >= 500 {
			return agent.Response{}, fmt.Errorf("gemini: status %d (retryable): %s", resp.StatusCode, respBody)
		}
		return agent.Response{}, fmt.Errorf("gemini: status %d: %s", resp.StatusCode, respBody)
	}

	var gemResp geminiResponse
	if err := json.Unmarshal(respBody, &gemResp); err != nil {
		return agent.Response{}, fmt.Errorf("gemini: parse response: %w", err)
	}

	if len(gemResp.Candidates) == 0 {
		return agent.Response{
			TokensIn:  gemResp.UsageMetadata.PromptTokenCount,
			TokensOut: gemResp.UsageMetadata.CandidatesTokenCount,
		}, nil
	}

	candidate := gemResp.Candidates[0].Content
	var textContent string
	var toolCalls []agent.ToolCall
	for i, part := range candidate.Parts {
		if part.Text != "" {
			textContent += part.Text
		}
		if part.FunctionCall != nil {
			toolCalls = append(toolCalls, agent.ToolCall{
				ID:    fmt.Sprintf("call-%d", i),
				Name:  unsanitizeToolName(part.FunctionCall.Name),
				Input: string(part.FunctionCall.Args),
			})
		}
	}

	return agent.Response{
		Content:   strings.TrimSpace(textContent),
		ToolCalls: toolCalls,
		TokensIn:  gemResp.UsageMetadata.PromptTokenCount,
		TokensOut: gemResp.UsageMetadata.CandidatesTokenCount,
	}, nil
}
