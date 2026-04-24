package services

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
)

// MapsProvider provides directions using free APIs (Nominatim + OSRM).
type MapsProvider struct {
	client *http.Client
}

// NewMapsProvider creates a maps provider (no API key required).
func NewMapsProvider(client *http.Client) *MapsProvider {
	if client == nil {
		client = http.DefaultClient
	}
	return &MapsProvider{client: client}
}

// Geocode resolves a place name to coordinates using Nominatim.
func (m *MapsProvider) Geocode(ctx context.Context, query string) (lat, lon float64, err error) {
	u := fmt.Sprintf("https://nominatim.openstreetmap.org/search?q=%s&format=json&limit=1",
		url.QueryEscape(query))

	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return 0, 0, fmt.Errorf("maps.geocode: %w", err)
	}
	req.Header.Set("User-Agent", "aibutler/0.1.0")

	resp, err := m.client.Do(req)
	if err != nil {
		return 0, 0, fmt.Errorf("maps.geocode: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, 0, err
	}

	var results []struct {
		Lat string `json:"lat"`
		Lon string `json:"lon"`
	}
	if err := json.Unmarshal(body, &results); err != nil {
		return 0, 0, fmt.Errorf("maps.geocode: parse: %w", err)
	}
	if len(results) == 0 {
		return 0, 0, fmt.Errorf("maps.geocode: no results for %q", query)
	}

	var latF, lonF float64
	fmt.Sscanf(results[0].Lat, "%f", &latF)
	fmt.Sscanf(results[0].Lon, "%f", &lonF)

	return latF, lonF, nil
}

// Directions returns driving directions between two locations.
func (m *MapsProvider) Directions(ctx context.Context, from, to string) (string, error) {
	fromLat, fromLon, err := m.Geocode(ctx, from)
	if err != nil {
		return "", err
	}
	toLat, toLon, err := m.Geocode(ctx, to)
	if err != nil {
		return "", err
	}

	// OSRM expects lon,lat order.
	u := fmt.Sprintf("https://router.project-osrm.org/route/v1/driving/%f,%f;%f,%f?overview=false&steps=false",
		fromLon, fromLat, toLon, toLat)

	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return "", fmt.Errorf("maps.directions: %w", err)
	}

	resp, err := m.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("maps.directions: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	var data struct {
		Routes []struct {
			Distance float64 `json:"distance"` // meters
			Duration float64 `json:"duration"` // seconds
		} `json:"routes"`
	}
	if err := json.Unmarshal(body, &data); err != nil {
		return "", fmt.Errorf("maps.directions: parse: %w", err)
	}
	if len(data.Routes) == 0 {
		return "", fmt.Errorf("maps.directions: no route found")
	}

	type result struct {
		From       string  `json:"from"`
		To         string  `json:"to"`
		DistanceKM float64 `json:"distance_km"`
		DurationM  float64 `json:"duration_minutes"`
	}

	out, _ := json.Marshal(result{
		From:       from,
		To:         to,
		DistanceKM: data.Routes[0].Distance / 1000,
		DurationM:  data.Routes[0].Duration / 60,
	})
	return string(out), nil
}
