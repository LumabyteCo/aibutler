// Package deepgram provides a speech-to-text adapter for the Deepgram API.
package deepgram

import (
	"bytes"
	"context"
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

// Client calls the Deepgram API for speech-to-text transcription.
type Client struct {
	apiKey     string
	httpClient *http.Client
	baseURL    string
}

// NewClient creates a Deepgram client with the given API key.
func NewClient(apiKey string) *Client {
	return &Client{
		apiKey:     apiKey,
		httpClient: &http.Client{Timeout: 60 * time.Second},
		baseURL:    "https://api.deepgram.com",
	}
}

// SetBaseURL overrides the API base URL (for testing).
func (c *Client) SetBaseURL(u string) { c.baseURL = u }

// SetHTTPClient overrides the HTTP client (for testing).
func (c *Client) SetHTTPClient(h *http.Client) { c.httpClient = h }

// transcribeResponse mirrors the Deepgram /v1/listen response.
type transcribeResponse struct {
	Results struct {
		Channels []struct {
			Alternatives []struct {
				Transcript string `json:"transcript"`
			} `json:"alternatives"`
		} `json:"channels"`
	} `json:"results"`
}

// Transcribe sends raw audio bytes to Deepgram and returns the transcript.
func (c *Client) Transcribe(ctx context.Context, audioBytes []byte, mimeType string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.baseURL+"/v1/listen?smart_format=true&model=nova-2",
		bytes.NewReader(audioBytes))
	if err != nil {
		return "", fmt.Errorf("deepgram: create request: %w", err)
	}
	if mimeType == "" {
		mimeType = "audio/wav"
	}
	req.Header.Set("Content-Type", mimeType)
	req.Header.Set("Authorization", "Token "+c.apiKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("deepgram: request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		data, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("deepgram: API error %d: %s", resp.StatusCode, string(data))
	}

	var result transcribeResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("deepgram: decode response: %w", err)
	}

	if len(result.Results.Channels) == 0 || len(result.Results.Channels[0].Alternatives) == 0 {
		return "", nil
	}
	return result.Results.Channels[0].Alternatives[0].Transcript, nil
}

// TranscribeURL sends a URL to Deepgram for transcription.
func (c *Client) TranscribeURL(ctx context.Context, url string) (string, error) {
	payload, err := json.Marshal(map[string]string{"url": url})
	if err != nil {
		return "", fmt.Errorf("deepgram: marshal URL payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.baseURL+"/v1/listen?smart_format=true&model=nova-2",
		bytes.NewReader(payload))
	if err != nil {
		return "", fmt.Errorf("deepgram: create URL request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Token "+c.apiKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("deepgram: URL request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		data, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("deepgram: API error %d: %s", resp.StatusCode, string(data))
	}

	var result transcribeResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("deepgram: decode URL response: %w", err)
	}

	if len(result.Results.Channels) == 0 || len(result.Results.Channels[0].Alternatives) == 0 {
		return "", nil
	}
	return result.Results.Channels[0].Alternatives[0].Transcript, nil
}

// RegisterDeepgramTools registers voice.deepgram.transcribe_url.
func RegisterDeepgramTools(registry toolRegistry, client *Client) {
	registry.Register(
		"voice.deepgram.transcribe_url",
		"Transcribe an audio file at a public URL using Deepgram.",
		`{"type":"object","properties":{"url":{"type":"string","description":"Public URL of the audio file to transcribe"}},"required":["url"]}`,
		"voice.transcribe",
		func(ctx context.Context, input string) (string, error) {
			var args struct {
				URL string `json:"url"`
			}
			if err := json.Unmarshal([]byte(input), &args); err != nil {
				return "", fmt.Errorf("voice.deepgram.transcribe_url: invalid input: %w", err)
			}
			if args.URL == "" {
				return "", fmt.Errorf("voice.deepgram.transcribe_url: url is required")
			}
			transcript, err := client.TranscribeURL(ctx, args.URL)
			if err != nil {
				return "", err
			}
			out, _ := json.Marshal(map[string]string{"transcript": transcript})
			return string(out), nil
		},
	)
}
