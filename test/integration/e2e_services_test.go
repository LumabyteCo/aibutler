//go:build integration

package integration

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/LumabyteCo/aibutler/internal/agent"
)

// TestE2EWeatherCurrent verifies the weather.current tool fetches and parses weather data.
func TestE2EWeatherCurrent(t *testing.T) {
	p := setupPipelineWithOpts(t, pipelineOpts{
		WithServices: true,
		Responses: []agent.Response{
			toolCallResponse("Getting weather.",
				tc("svc1", "weather.current", `{"location":"London"}`),
			),
			finalResponse("It's 15°C in London with clear skies."),
		},
	})

	// Configure the service mux to handle OpenWeatherMap API.
	p.ServiceMux.HandleFunc("/data/2.5/weather", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query().Get("q")
		if q != "London" {
			t.Errorf("weather query = %q, want 'London'", q)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"name": "London",
			"main": map[string]interface{}{
				"temp":     15.2,
				"humidity": 72,
			},
			"weather": []map[string]interface{}{
				{"description": "clear sky"},
			},
			"wind": map[string]interface{}{
				"speed": 3.5,
			},
		})
	})

	p.sendMsg(t, "What's the weather in London?")

	// Verify model was called twice (tool call + final).
	if p.Fake.CallCount() != 2 {
		t.Fatalf("model calls = %d, want 2", p.Fake.CallCount())
	}

	// Verify tool result contains parsed weather.
	calls := p.Fake.Calls()
	found := false
	for _, msg := range calls[1] {
		if msg.Role == "tool" && strings.Contains(msg.Content, "London") && strings.Contains(msg.Content, "clear sky") {
			found = true
			break
		}
	}
	if !found {
		t.Error("weather tool result should contain 'London' and 'clear sky'")
	}

	resp := p.lastResponse(t)
	if resp != "It's 15°C in London with clear skies." {
		t.Errorf("response = %q", resp)
	}
}

// TestE2ENewsHeadlines verifies the news.headlines tool fetches and parses news data.
func TestE2ENewsHeadlines(t *testing.T) {
	p := setupPipelineWithOpts(t, pipelineOpts{
		WithServices: true,
		Responses: []agent.Response{
			toolCallResponse("Getting news.",
				tc("svc2", "news.headlines", `{"category":"technology","country":"us"}`),
			),
			finalResponse("Here are the top tech headlines."),
		},
	})

	p.ServiceMux.HandleFunc("/v2/top-headlines", func(w http.ResponseWriter, r *http.Request) {
		cat := r.URL.Query().Get("category")
		if cat != "technology" {
			t.Errorf("category = %q, want 'technology'", cat)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"articles": []map[string]interface{}{
				{
					"title":       "Go 1.26 Released",
					"source":      map[string]interface{}{"name": "TechNews"},
					"description": "New features in Go 1.26",
					"url":         "https://example.com/go126",
					"publishedAt": "2026-03-21T10:00:00Z",
				},
				{
					"title":       "AI Advances in 2026",
					"source":      map[string]interface{}{"name": "AIDaily"},
					"description": "Major breakthroughs",
					"url":         "https://example.com/ai2026",
					"publishedAt": "2026-03-21T09:00:00Z",
				},
			},
		})
	})

	p.sendMsg(t, "Show me tech news")

	if p.Fake.CallCount() != 2 {
		t.Fatalf("model calls = %d, want 2", p.Fake.CallCount())
	}

	// Verify tool result contains news articles.
	calls := p.Fake.Calls()
	found := false
	for _, msg := range calls[1] {
		if msg.Role == "tool" && strings.Contains(msg.Content, "Go 1.26") && strings.Contains(msg.Content, "AI Advances") {
			found = true
			break
		}
	}
	if !found {
		t.Error("news tool result should contain 'Go 1.26' and 'AI Advances'")
	}

	resp := p.lastResponse(t)
	if resp != "Here are the top tech headlines." {
		t.Errorf("response = %q", resp)
	}
}

// TestE2EMapsDirections verifies the maps.directions tool geocodes and routes.
func TestE2EMapsDirections(t *testing.T) {
	p := setupPipelineWithOpts(t, pipelineOpts{
		WithServices: true,
		Responses: []agent.Response{
			toolCallResponse("Getting directions.",
				tc("svc3", "maps.directions", `{"from":"Berlin","to":"Munich"}`),
			),
			finalResponse("The drive is about 585 km, taking around 5.5 hours."),
		},
	})

	// Handle both Nominatim geocode requests and OSRM routing.
	p.ServiceMux.HandleFunc("/search", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query().Get("q")
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.Contains(q, "Berlin"):
			w.Write([]byte(`[{"lat":"52.5200","lon":"13.4050"}]`))
		case strings.Contains(q, "Munich"):
			w.Write([]byte(`[{"lat":"48.1351","lon":"11.5820"}]`))
		default:
			w.Write([]byte(`[]`))
		}
	})

	p.ServiceMux.HandleFunc("/route/v1/driving/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"routes": []map[string]interface{}{
				{
					"distance": 585000.0, // meters
					"duration": 19800.0,  // seconds (~5.5 hours)
				},
			},
		})
	})

	p.sendMsg(t, "How do I drive from Berlin to Munich?")

	if p.Fake.CallCount() != 2 {
		t.Fatalf("model calls = %d, want 2", p.Fake.CallCount())
	}

	// Verify tool result contains route info.
	calls := p.Fake.Calls()
	found := false
	for _, msg := range calls[1] {
		if msg.Role == "tool" && strings.Contains(msg.Content, "Berlin") && strings.Contains(msg.Content, "Munich") {
			found = true
			break
		}
	}
	if !found {
		t.Error("maps tool result should contain 'Berlin' and 'Munich'")
	}

	resp := p.lastResponse(t)
	if resp != "The drive is about 585 km, taking around 5.5 hours." {
		t.Errorf("response = %q", resp)
	}
}
