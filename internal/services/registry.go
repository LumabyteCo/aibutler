package services

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/LumabyteCo/aibutler/internal/tool"
)


// RegisterEverydayServices registers weather, news, and maps tools.
// If client is nil, a default client with 15s timeout is used.
func RegisterEverydayServices(registry *tool.Registry, weatherKey, newsKey, sportsKey, transitKey, flightKey, trackingKey, recipeKey string, client *http.Client) {
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}

	if weatherKey != "" {
		registry.Register(&weatherTool{provider: NewWeatherProvider(weatherKey, client)})
	}
	if newsKey != "" {
		registry.Register(&newsTool{provider: NewNewsProvider(newsKey, client)})
	}
	// Maps is always available (free, no API key).
	registry.Register(&mapsTool{provider: NewMapsProvider(client)})

	// Sports scores
	sportsProv := NewSportsProvider(sportsKey, client)
	registry.Register(&tool.FuncTool{
		ToolName:   "sports.scores",
		ToolDesc:   "Get recent sports scores and standings.",
		ToolSchema: `{"type":"object","properties":{"sport":{"type":"string","description":"e.g. soccer, basketball"},"league":{"type":"string","description":"e.g. NBA, NFL, Premier League"}}}`,
		ToolCap:    "tool.web.fetch",
		Exec: func(ctx context.Context, input string) (string, error) {
			var args struct {
				Sport  string `json:"sport"`
				League string `json:"league"`
			}
			json.Unmarshal([]byte(input), &args)
			return sportsProv.Scores(ctx, args.Sport, args.League)
		},
	})

	// Transit departures
	transitProv := NewTransitProvider(transitKey, client)
	registry.Register(&tool.FuncTool{
		ToolName:   "transit.next_departure",
		ToolDesc:   "Get next transit departure times for a stop.",
		ToolSchema: `{"type":"object","properties":{"stop":{"type":"string"},"line":{"type":"string"}}}`,
		ToolCap:    "tool.web.fetch",
		Exec: func(ctx context.Context, input string) (string, error) {
			var args struct {
				Stop string `json:"stop"`
				Line string `json:"line"`
			}
			json.Unmarshal([]byte(input), &args)
			return transitProv.NextDepartures(ctx, args.Stop, args.Line)
		},
	})

	// Flight status
	flightProv := NewFlightProvider(flightKey, client)
	registry.Register(&tool.FuncTool{
		ToolName:   "flight.status",
		ToolDesc:   "Get the status of a flight by flight number.",
		ToolSchema: `{"type":"object","properties":{"flight_number":{"type":"string"}},"required":["flight_number"]}`,
		ToolCap:    "tool.web.fetch",
		Exec: func(ctx context.Context, input string) (string, error) {
			var args struct {
				FlightNumber string `json:"flight_number"`
			}
			json.Unmarshal([]byte(input), &args)
			return flightProv.FlightStatus(ctx, args.FlightNumber)
		},
	})

	// Package tracking
	trackProv := NewTrackingProvider(trackingKey, client)
	registry.Register(&tool.FuncTool{
		ToolName:   "tracking.package",
		ToolDesc:   "Track a package by tracking number and carrier.",
		ToolSchema: `{"type":"object","properties":{"tracking_number":{"type":"string"},"carrier":{"type":"string"}},"required":["tracking_number"]}`,
		ToolCap:    "tool.web.fetch",
		Exec: func(ctx context.Context, input string) (string, error) {
			var args struct {
				TrackingNumber string `json:"tracking_number"`
				Carrier        string `json:"carrier"`
			}
			json.Unmarshal([]byte(input), &args)
			return trackProv.TrackPackage(ctx, args.TrackingNumber, args.Carrier)
		},
	})

	// Recipe search
	recipeProv := NewRecipeProvider(recipeKey, client)
	registry.Register(&tool.FuncTool{
		ToolName:   "recipe.search",
		ToolDesc:   "Search for recipes by ingredient or dish name.",
		ToolSchema: `{"type":"object","properties":{"query":{"type":"string"}},"required":["query"]}`,
		ToolCap:    "tool.web.fetch",
		Exec: func(ctx context.Context, input string) (string, error) {
			var args struct {
				Query string `json:"query"`
			}
			json.Unmarshal([]byte(input), &args)
			return recipeProv.Search(ctx, args.Query)
		},
	})
}

// --- weather.current ---

type weatherTool struct{ provider *WeatherProvider }

func (t *weatherTool) Name() string        { return "weather.current" }
func (t *weatherTool) Description() string { return "Get current weather for a location" }
func (t *weatherTool) Capability() string  { return "tool.web.fetch" }
func (t *weatherTool) Schema() string {
	return `{"type":"object","properties":{"location":{"type":"string","description":"City name or coordinates"}},"required":["location"]}`
}

func (t *weatherTool) Execute(ctx context.Context, input string) (string, error) {
	var args struct {
		Location string `json:"location"`
	}
	if err := json.Unmarshal([]byte(input), &args); err != nil {
		return "", fmt.Errorf("weather.current: %w", err)
	}
	return t.provider.Current(ctx, args.Location)
}

// --- news.headlines ---

type newsTool struct{ provider *NewsProvider }

func (t *newsTool) Name() string        { return "news.headlines" }
func (t *newsTool) Description() string { return "Get top news headlines" }
func (t *newsTool) Capability() string  { return "tool.web.fetch" }
func (t *newsTool) Schema() string {
	return `{"type":"object","properties":{"query":{"type":"string"},"category":{"type":"string","description":"business, technology, sports, etc."},"country":{"type":"string","description":"2-letter country code"}}}`
}

func (t *newsTool) Execute(ctx context.Context, input string) (string, error) {
	var args struct {
		Query    string `json:"query"`
		Category string `json:"category"`
		Country  string `json:"country"`
	}
	if input != "" {
		_ = json.Unmarshal([]byte(input), &args)
	}
	return t.provider.Headlines(ctx, args.Query, args.Category, args.Country)
}

// --- maps.directions ---

type mapsTool struct{ provider *MapsProvider }

func (t *mapsTool) Name() string        { return "maps.directions" }
func (t *mapsTool) Description() string { return "Get driving directions between two locations" }
func (t *mapsTool) Capability() string  { return "tool.web.fetch" }
func (t *mapsTool) Schema() string {
	return `{"type":"object","properties":{"from":{"type":"string"},"to":{"type":"string"}},"required":["from","to"]}`
}

func (t *mapsTool) Execute(ctx context.Context, input string) (string, error) {
	var args struct {
		From string `json:"from"`
		To   string `json:"to"`
	}
	if err := json.Unmarshal([]byte(input), &args); err != nil {
		return "", fmt.Errorf("maps.directions: %w", err)
	}
	return t.provider.Directions(ctx, args.From, args.To)
}
