// Package elevenlabs provides a TTS adapter for the ElevenLabs API.
package elevenlabs

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// toolRegistry is the narrow interface for registering tools (avoids import cycles).
type toolRegistry interface {
	Register(name, description, schema, capability string, exec func(ctx context.Context, input string) (string, error))
}

const defaultModel = "eleven_monolingual_v1"

// Voice is an ElevenLabs voice entry.
type Voice struct {
	ID   string `json:"voice_id"`
	Name string `json:"name"`
}

// Client calls the ElevenLabs API for text-to-speech synthesis.
type Client struct {
	apiKey     string
	voiceID    string
	model      string
	httpClient *http.Client
	baseURL    string
}

// NewClient creates an ElevenLabs client with the given API key and voice ID.
func NewClient(apiKey, voiceID string) *Client {
	return &Client{
		apiKey:     apiKey,
		voiceID:    voiceID,
		model:      defaultModel,
		httpClient: &http.Client{Timeout: 60 * time.Second},
		baseURL:    "https://api.elevenlabs.io",
	}
}

// SetBaseURL overrides the API base URL (for testing).
func (c *Client) SetBaseURL(u string) { c.baseURL = u }

// SetHTTPClient overrides the HTTP client (for testing).
func (c *Client) SetHTTPClient(h *http.Client) { c.httpClient = h }

// Synthesize converts text to speech and returns MP3 audio bytes.
func (c *Client) Synthesize(ctx context.Context, text string) ([]byte, error) {
	payload := map[string]interface{}{
		"text":     text,
		"model_id": c.model,
		"voice_settings": map[string]interface{}{
			"stability":        0.5,
			"similarity_boost": 0.5,
		},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("elevenlabs: marshal payload: %w", err)
	}

	url := fmt.Sprintf("%s/v1/text-to-speech/%s", c.baseURL, c.voiceID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("elevenlabs: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("xi-api-key", c.apiKey)
	req.Header.Set("Accept", "audio/mpeg")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("elevenlabs: request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		data, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("elevenlabs: API error %d: %s", resp.StatusCode, string(data))
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("elevenlabs: read audio: %w", err)
	}
	return data, nil
}

// voiceListResponse is the API response for GET /voices.
type voiceListResponse struct {
	Voices []Voice `json:"voices"`
}

// ListVoices returns all available ElevenLabs voices.
func (c *Client) ListVoices(ctx context.Context) ([]Voice, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/v1/voices", nil)
	if err != nil {
		return nil, fmt.Errorf("elevenlabs: list voices request: %w", err)
	}
	req.Header.Set("xi-api-key", c.apiKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("elevenlabs: list voices: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		data, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("elevenlabs: API error %d: %s", resp.StatusCode, string(data))
	}

	var result voiceListResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("elevenlabs: decode voices: %w", err)
	}
	return result.Voices, nil
}

// RegisterElevenLabsTools registers voice.elevenlabs.synthesize and voice.elevenlabs.list_voices.
func RegisterElevenLabsTools(registry toolRegistry, client *Client) {
	registry.Register(
		"voice.elevenlabs.synthesize",
		"Synthesize text to speech using ElevenLabs. Returns base64-encoded MP3 audio.",
		`{"type":"object","properties":{"text":{"type":"string","description":"Text to synthesize"}},"required":["text"]}`,
		"voice.speak",
		func(ctx context.Context, input string) (string, error) {
			var args struct {
				Text string `json:"text"`
			}
			if err := json.Unmarshal([]byte(input), &args); err != nil {
				return "", fmt.Errorf("voice.elevenlabs.synthesize: invalid input: %w", err)
			}
			if args.Text == "" {
				return "", fmt.Errorf("voice.elevenlabs.synthesize: text is required")
			}
			audio, err := client.Synthesize(ctx, args.Text)
			if err != nil {
				return "", err
			}
			out, _ := json.Marshal(map[string]interface{}{
				"audio_base64": base64.StdEncoding.EncodeToString(audio),
				"format":       "mp3",
				"bytes":        len(audio),
			})
			return string(out), nil
		},
	)

	registry.Register(
		"voice.elevenlabs.list_voices",
		"List all available ElevenLabs voices.",
		`{"type":"object","properties":{}}`,
		"voice.speak",
		func(ctx context.Context, input string) (string, error) {
			voices, err := client.ListVoices(ctx)
			if err != nil {
				return "", err
			}
			out, _ := json.Marshal(map[string]interface{}{
				"voices": voices,
				"count":  len(voices),
			})
			return string(out), nil
		},
	)
}
