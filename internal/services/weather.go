package services

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
)

// WeatherProvider fetches current weather.
type WeatherProvider struct {
	apiKey string
	client *http.Client
}

// NewWeatherProvider creates a weather provider using OpenWeatherMap.
func NewWeatherProvider(apiKey string, client *http.Client) *WeatherProvider {
	if client == nil {
		client = http.DefaultClient
	}
	return &WeatherProvider{apiKey: apiKey, client: client}
}

// Current returns current weather for a location.
func (w *WeatherProvider) Current(ctx context.Context, location string) (string, error) {
	u := fmt.Sprintf("https://api.openweathermap.org/data/2.5/weather?q=%s&appid=%s&units=metric",
		url.QueryEscape(location), w.apiKey)

	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return "", fmt.Errorf("weather: %w", err)
	}

	resp, err := w.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("weather: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("weather: read body: %w", err)
	}

	if resp.StatusCode != 200 {
		return "", fmt.Errorf("weather: API returned %d: %s", resp.StatusCode, string(body))
	}

	var data struct {
		Name string `json:"name"`
		Main struct {
			Temp     float64 `json:"temp"`
			Humidity int     `json:"humidity"`
		} `json:"main"`
		Weather []struct {
			Description string `json:"description"`
		} `json:"weather"`
		Wind struct {
			Speed float64 `json:"speed"`
		} `json:"wind"`
	}
	if err := json.Unmarshal(body, &data); err != nil {
		return "", fmt.Errorf("weather: parse: %w", err)
	}

	desc := ""
	if len(data.Weather) > 0 {
		desc = data.Weather[0].Description
	}

	type result struct {
		Location    string  `json:"location"`
		Temperature float64 `json:"temperature_c"`
		Humidity    int     `json:"humidity_pct"`
		Description string  `json:"description"`
		WindSpeed   float64 `json:"wind_speed_ms"`
	}

	out, _ := json.Marshal(result{
		Location:    data.Name,
		Temperature: data.Main.Temp,
		Humidity:    data.Main.Humidity,
		Description: desc,
		WindSpeed:   data.Wind.Speed,
	})
	return string(out), nil
}
