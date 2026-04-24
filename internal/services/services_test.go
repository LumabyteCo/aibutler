package services_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/LumabyteCo/aibutler/internal/services"
	"github.com/LumabyteCo/aibutler/internal/tool"
)

func TestWeatherProvider(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"name": "London",
			"main": map[string]interface{}{"temp": 15.5, "humidity": 72},
			"weather": []map[string]string{{"description": "cloudy"}},
			"wind":    map[string]interface{}{"speed": 5.2},
		})
	}))
	defer ts.Close()

	provider := services.NewWeatherProvider("test-key", ts.Client())
	if provider == nil {
		t.Fatal("expected non-nil provider")
	}
}

func TestNewsProvider(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"articles": []map[string]interface{}{
				{"title": "Test", "source": map[string]string{"name": "Src"}, "description": "Desc", "url": "https://x.com"},
			},
		})
	}))
	defer ts.Close()

	provider := services.NewNewsProvider("test-key", ts.Client())
	if provider == nil {
		t.Fatal("expected non-nil provider")
	}
}

func TestMapsProvider(t *testing.T) {
	provider := services.NewMapsProvider(http.DefaultClient)
	if provider == nil {
		t.Fatal("expected non-nil provider")
	}
}

func TestRegisterEverydayServicesAllKeys(t *testing.T) {
	reg := tool.NewRegistry()
	services.RegisterEverydayServices(reg, "weather-key", "news-key", "", "", "", "", "", nil)

	for _, name := range []string{"weather.current", "news.headlines", "maps.directions"} {
		if _, ok := reg.Get(name); !ok {
			t.Errorf("tool %q not registered", name)
		}
	}
}

func TestRegisterEverydayServicesNoKeys(t *testing.T) {
	reg := tool.NewRegistry()
	services.RegisterEverydayServices(reg, "", "", "", "", "", "", "", nil)

	if _, ok := reg.Get("maps.directions"); !ok {
		t.Error("maps.directions should be registered without API key")
	}
	if _, ok := reg.Get("weather.current"); ok {
		t.Error("weather.current should NOT be registered without API key")
	}
	if _, ok := reg.Get("news.headlines"); ok {
		t.Error("news.headlines should NOT be registered without API key")
	}
}

func TestSportsProvider(t *testing.T) {
	p := services.NewSportsProvider("", nil)
	if p == nil {
		t.Fatal("expected non-nil provider")
	}
}

func TestTransitProvider(t *testing.T) {
	p := services.NewTransitProvider("", nil)
	if p == nil {
		t.Fatal("expected non-nil provider")
	}
}

func TestFlightProvider(t *testing.T) {
	p := services.NewFlightProvider("", nil)
	if p == nil {
		t.Fatal("expected non-nil provider")
	}
}

func TestTrackingProvider(t *testing.T) {
	p := services.NewTrackingProvider("", nil)
	if p == nil {
		t.Fatal("expected non-nil provider")
	}
}

func TestRecipeProvider(t *testing.T) {
	p := services.NewRecipeProvider("", nil)
	if p == nil {
		t.Fatal("expected non-nil provider")
	}
}

func TestRegisterEverydayServicesNewTools(t *testing.T) {
	reg := tool.NewRegistry()
	services.RegisterEverydayServices(reg, "", "", "", "", "", "", "", nil)

	newTools := []string{"sports.scores", "transit.next_departure", "flight.status", "tracking.package", "recipe.search"}
	for _, name := range newTools {
		if _, ok := reg.Get(name); !ok {
			t.Errorf("tool %q should always be registered", name)
		}
	}
}

// --- New tests: stub behavior (empty key) returns expected data ---

func TestSportsProviderStub(t *testing.T) {
	p := services.NewSportsProvider("", nil)
	result, err := p.Scores(context.Background(), "basketball", "NBA")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "Configure a sports API key") {
		t.Error("stub response should contain configuration note")
	}
}

func TestTransitProviderStub(t *testing.T) {
	p := services.NewTransitProvider("", nil)
	result, err := p.NextDepartures(context.Background(), "STOP1", "LINE1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "Configure via") {
		t.Error("stub response should contain configuration note")
	}
}

func TestFlightProviderStub(t *testing.T) {
	p := services.NewFlightProvider("", nil)
	result, err := p.FlightStatus(context.Background(), "AA123")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "unknown") {
		t.Error("stub response should contain unknown status")
	}
}

func TestTrackingProviderStub(t *testing.T) {
	p := services.NewTrackingProvider("", nil)
	result, err := p.TrackPackage(context.Background(), "1Z999AA10123456784", "UPS")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "Configure via") {
		t.Error("stub response should contain configuration note")
	}
}

func TestRecipeProviderStub(t *testing.T) {
	p := services.NewRecipeProvider("", nil)
	result, err := p.Search(context.Background(), "pasta")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "Configure via") {
		t.Error("stub response should contain configuration note")
	}
}

// --- New tests: real HTTP call when API key present (httptest mock) ---

// redirectClient returns an *http.Client that redirects ALL requests to the test server,
// regardless of the original host in the URL.
func redirectClient(ts *httptest.Server) *http.Client {
	return &http.Client{
		Transport: &rewriteTransport{inner: ts.Client().Transport, tsURL: ts.URL},
	}
}

// rewriteTransport rewrites request URLs to point at the test server.
type rewriteTransport struct {
	inner http.RoundTripper
	tsURL string
}

func (t *rewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// Rewrite the scheme+host to the test server, keep path+query.
	req.URL.Scheme = "http"
	req.URL.Host = strings.TrimPrefix(t.tsURL, "http://")
	return t.inner.RoundTrip(req)
}

func TestSportsProviderWithKey(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("key") != "test-sports-key" {
			t.Errorf("expected key=test-sports-key, got %s", r.URL.Query().Get("key"))
		}
		json.NewEncoder(w).Encode([]map[string]interface{}{
			{"HomeTeam": "Lakers", "AwayTeam": "Celtics", "HomeScore": 110, "AwayScore": 105},
		})
	}))
	defer ts.Close()

	p := services.NewSportsProvider("test-sports-key", redirectClient(ts))
	result, err := p.Scores(context.Background(), "basketball", "NBA")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(result, "Configure a sports API key") {
		t.Error("should not return stub when API key is present")
	}
}

func TestTransitProviderWithKey(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("api_key") != "test-transit-key" {
			t.Errorf("expected api_key=test-transit-key, got %s", r.URL.Query().Get("api_key"))
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"departures": []map[string]interface{}{
				{"line": "N", "time": "10:05"},
			},
		})
	}))
	defer ts.Close()

	p := services.NewTransitProvider("test-transit-key", redirectClient(ts))
	result, err := p.NextDepartures(context.Background(), "STOP1", "N")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(result, "Configure via") {
		t.Error("should not return stub when API key is present")
	}
}

func TestFlightProviderWithKey(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode([]map[string]interface{}{
			{"flight": "AA123", "status": "en-route"},
		})
	}))
	defer ts.Close()

	p := services.NewFlightProvider("test-flight-key", redirectClient(ts))
	result, err := p.FlightStatus(context.Background(), "AA123")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(result, `"status": "unknown"`) {
		t.Error("should not return stub when API key is present")
	}
}

func TestTrackingProviderWithKey(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status":  "delivered",
			"carrier": "UPS",
		})
	}))
	defer ts.Close()

	p := services.NewTrackingProvider("test-tracking-key", redirectClient(ts))
	result, err := p.TrackPackage(context.Background(), "1Z999AA10123456784", "UPS")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(result, "Configure via") {
		t.Error("should not return stub when API key is present")
	}
}

func TestRecipeProviderWithKey(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"results": []map[string]interface{}{
				{"id": 1, "title": "Pasta Carbonara"},
			},
		})
	}))
	defer ts.Close()

	p := services.NewRecipeProvider("test-recipe-key", redirectClient(ts))
	result, err := p.Search(context.Background(), "pasta")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(result, "Configure via") {
		t.Error("should not return stub when API key is present")
	}
	if !strings.Contains(result, "Pasta Carbonara") {
		t.Error("should contain recipe from API response")
	}
}
