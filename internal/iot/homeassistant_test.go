package iot_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/LumabyteCo/aibutler/internal/iot"
)

// mockHA is a minimal Home Assistant REST API double.
type mockHA struct {
	mu         sync.Mutex
	entities   []map[string]interface{}
	services   []string // "light.turn_on:light.kitchen" records
	statusCode int      // if non-zero, all calls return this
}

func newMockHA() *mockHA {
	return &mockHA{
		entities: []map[string]interface{}{
			{
				"entity_id":    "light.kitchen",
				"state":        "on",
				"last_changed": "2026-10-04T10:00:00+00:00",
				"attributes":   map[string]interface{}{"friendly_name": "Kitchen Light"},
			},
			{
				"entity_id":    "light.living_room",
				"state":        "off",
				"last_changed": "2026-10-04T09:00:00+00:00",
				"attributes":   map[string]interface{}{"friendly_name": "Living Room Light"},
			},
			{
				"entity_id":    "lock.front_door",
				"state":        "locked",
				"last_changed": "2026-10-04T08:00:00+00:00",
				"attributes":   map[string]interface{}{"friendly_name": "Front Door"},
			},
			{
				"entity_id":    "climate.main",
				"state":        "21",
				"last_changed": "2026-10-04T07:00:00+00:00",
				"attributes":   map[string]interface{}{"friendly_name": "Main Thermostat", "unit_of_measurement": "°C"},
			},
			{
				"entity_id":    "sensor.outside_temp",
				"state":        "14.2",
				"last_changed": "2026-10-04T11:00:00+00:00",
				"attributes":   map[string]interface{}{"friendly_name": "Outside", "unit_of_measurement": "°C", "device_class": "temperature"},
			},
			{
				"entity_id":    "binary_sensor.front_door",
				"state":        "off",
				"last_changed": "2026-10-04T11:00:00+00:00",
				"attributes":   map[string]interface{}{"friendly_name": "Front Door Open"},
			},
			{
				"entity_id":    "automation.morning",
				"state":        "on",
				"attributes":   map[string]interface{}{},
				"last_changed": "2026-10-04T06:00:00+00:00",
			},
			{
				"entity_id":    "cover.garage",
				"state":        "closed",
				"last_changed": "2026-10-04T06:00:00+00:00",
				"attributes":   map[string]interface{}{"friendly_name": "Garage Door"},
			},
		},
	}
}

func (m *mockHA) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		m.mu.Lock()
		defer m.mu.Unlock()

		if m.statusCode != 0 {
			w.WriteHeader(m.statusCode)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "boom"})
			return
		}
		if r.Header.Get("Authorization") != "Bearer test-token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}

		switch {
		case r.URL.Path == "/api/states" && r.Method == "GET":
			_ = json.NewEncoder(w).Encode(m.entities)

		case strings.HasPrefix(r.URL.Path, "/api/states/") && r.Method == "GET":
			id := strings.TrimPrefix(r.URL.Path, "/api/states/")
			for _, e := range m.entities {
				if e["entity_id"] == id {
					_ = json.NewEncoder(w).Encode(e)
					return
				}
			}
			w.WriteHeader(http.StatusNotFound)

		case strings.HasPrefix(r.URL.Path, "/api/services/") && r.Method == "POST":
			domain := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/services/"), "/")[0]
			service := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/services/"), "/")[1]
			var body map[string]interface{}
			_ = json.NewDecoder(r.Body).Decode(&body)
			entityID, _ := body["entity_id"].(string)
			m.services = append(m.services, domain+"."+service+":"+entityID)
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode([]map[string]interface{}{})

		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})
}

func adapterFor(t *testing.T, m *mockHA, policy map[string]iot.Tier) (*iot.HomeAssistantAdapter, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(m.handler())
	t.Cleanup(srv.Close)
	return iot.NewHomeAssistantAdapter(srv.URL, "test-token", policy), srv
}

// TestHADiscoverAndTiers verifies: entity discovery, friendly names, the
// tier classification matrix, and that non-device domains (automation) are
// skipped.
func TestHADiscoverAndTiers(t *testing.T) {
	m := newMockHA()
	ha, _ := adapterFor(t, m, nil)

	devices, err := ha.Discover(context.Background())
	if err != nil {
		t.Fatalf("discover: %v", err)
	}

	byID := map[string]iot.Device{}
	for _, d := range devices {
		byID[d.ID] = d
	}

	tiers := []struct {
		id   string
		want iot.Tier
	}{
		{"light.kitchen", iot.TierComfort},
		{"light.living_room", iot.TierComfort},
		{"climate.main", iot.TierComfort},
		{"sensor.outside_temp", iot.TierSensor},
		{"binary_sensor.front_door", iot.TierSensor},
		{"lock.front_door", iot.TierSafety},
		{"cover.garage", iot.TierSafety},
	}
	for _, tc := range tiers {
		d, ok := byID[tc.id]
		if !ok {
			t.Errorf("device %s missing from discovery", tc.id)
			continue
		}
		if d.Tier != tc.want {
			t.Errorf("%s tier = %d, want %d", tc.id, d.Tier, tc.want)
		}
		if d.Adapter != "homeassistant" {
			t.Errorf("%s adapter = %q, want homeassistant", tc.id, d.Adapter)
		}
	}

	if _, ok := byID["automation.morning"]; ok {
		t.Error("automation entities must not be surfaced as devices")
	}

	if byID["light.kitchen"].Name != "Kitchen Light" {
		t.Errorf("friendly name not picked up: %q", byID["light.kitchen"].Name)
	}
}

// TestHATierPolicyNeverDowngradesSafety is the SECURITY test: configuration
// may never downgrade a lock below tier 3. Trying to set lock.front_door to
// tier 2 must be ignored.
func TestHATierPolicyNeverDowngradesSafety(t *testing.T) {
	m := newMockHA()
	// Malicious or careless config: lock as comfort.
	ha, _ := adapterFor(t, m, map[string]iot.Tier{"lock.front_door": iot.TierComfort})

	devices, err := ha.Discover(context.Background())
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	for _, d := range devices {
		if d.ID == "lock.front_door" {
			if d.Tier != iot.TierSafety {
				t.Errorf("SAFETY VIOLATION: lock.front_door tier = %d — config downgrades of safety devices must be ignored", d.Tier)
			}
		}
	}
}

// TestHATierPolicyUpgrades honors legitimate upgrades (a switch treated as
// safety-critical).
func TestHATierPolicyUpgrades(t *testing.T) {
	m := newMockHA()
	ha, _ := adapterFor(t, m, map[string]iot.Tier{"light.kitchen": iot.TierSafety})

	devices, _ := ha.Discover(context.Background())
	for _, d := range devices {
		if d.ID == "light.kitchen" && d.Tier != iot.TierSafety {
			t.Errorf("light.kitchen tier = %d, want upgraded to 3", d.Tier)
		}
	}
}

// TestHAExecuteCommandMapping verifies Butler actions translate to the
// correct HA service calls.
func TestHAExecuteCommandMapping(t *testing.T) {
	m := newMockHA()
	ha, _ := adapterFor(t, m, nil)

	cases := []struct {
		name string
		cmd  iot.Command
		want string
	}{
		{"light toggle", iot.Command{DeviceID: "light.kitchen", Action: "toggle"}, "light.toggle:light.kitchen"},
		{"light on", iot.Command{DeviceID: "light.kitchen", Action: "on"}, "light.turn_on:light.kitchen"},
		{"light off", iot.Command{DeviceID: "light.kitchen", Action: "off"}, "light.turn_off:light.kitchen"},
		{"lock", iot.Command{DeviceID: "lock.front_door", Action: "lock", Confirmed: true}, "lock.lock:lock.front_door"},
		{"unlock", iot.Command{DeviceID: "lock.front_door", Action: "unlock", Confirmed: true}, "lock.unlock:lock.front_door"},
		{"climate set", iot.Command{DeviceID: "climate.main", Action: "set", Params: map[string]interface{}{"temperature": 21.5}}, "climate.set_temperature:climate.main"},
		{"cover open", iot.Command{DeviceID: "cover.garage", Action: "open", Confirmed: true}, "cover.open_cover:cover.garage"},
		{"cover close", iot.Command{DeviceID: "cover.garage", Action: "close", Confirmed: true}, "cover.close_cover:cover.garage"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := ha.Execute(context.Background(), tc.cmd); err != nil {
				t.Fatalf("execute: %v", err)
			}
			m.mu.Lock()
			got := m.services[len(m.services)-1]
			m.mu.Unlock()
			if got != tc.want {
				t.Errorf("service call = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestHAExecuteRejectsBadAction ensures unknown actions fail loudly rather
// than firing a wrong service.
func TestHAExecuteRejectsBadAction(t *testing.T) {
	m := newMockHA()
	ha, _ := adapterFor(t, m, nil)

	err := ha.Execute(context.Background(), iot.Command{DeviceID: "light.kitchen", Action: "self-destruct"})
	if err == nil {
		t.Fatal("unsupported action must error")
	}
	if !strings.Contains(err.Error(), "unsupported action") {
		t.Errorf("error should name the problem, got: %v", err)
	}
}

// TestHAReadSensor checks sensor value/unit parsing for numeric and binary
// sensors.
func TestHAReadSensor(t *testing.T) {
	m := newMockHA()
	ha, _ := adapterFor(t, m, nil)

	readings, err := ha.ReadSensor(context.Background(), "sensor.outside_temp")
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(readings) != 1 {
		t.Fatalf("readings = %d, want 1", len(readings))
	}
	if readings[0].Value != 14.2 {
		t.Errorf("value = %f, want 14.2", readings[0].Value)
	}
	if readings[0].Unit != "°C" {
		t.Errorf("unit = %q, want °C", readings[0].Unit)
	}

	// Binary sensor: "off" → 0.
	readings, err = ha.ReadSensor(context.Background(), "binary_sensor.front_door")
	if err != nil {
		t.Fatalf("read binary: %v", err)
	}
	if readings[0].Value != 0 {
		t.Errorf("binary off value = %f, want 0", readings[0].Value)
	}
}

// TestHAAuthAndErrors covers 401 (bad token) and HA-down behavior.
func TestHAAuthAndErrors(t *testing.T) {
	m := newMockHA()
	srv := httptest.NewServer(m.handler())
	t.Cleanup(srv.Close)

	// Wrong token.
	ha := iot.NewHomeAssistantAdapter(srv.URL, "wrong-token", nil)
	if _, err := ha.Discover(context.Background()); err == nil {
		t.Error("discover with bad token must fail")
	} else if !strings.Contains(err.Error(), "401") {
		t.Errorf("expected 401 in error, got: %v", err)
	}

	// Server down.
	ha2 := iot.NewHomeAssistantAdapter("http://127.0.0.1:1", "test-token", nil)
	if _, err := ha2.Discover(context.Background()); err == nil {
		t.Error("discover against dead server must fail")
	}

	// Server erroring (5xx).
	m.statusCode = http.StatusBadGateway
	ha3 := iot.NewHomeAssistantAdapter(srv.URL, "test-token", nil)
	if _, err := ha3.Discover(context.Background()); err == nil {
		t.Error("discover must surface 502")
	}
	m.statusCode = 0
}

// TestHAURLNormalization verifies trailing slashes and missing /api are
// handled.
func TestHAURLNormalization(t *testing.T) {
	m := newMockHA()
	srv := httptest.NewServer(m.handler())
	t.Cleanup(srv.Close)

	for _, base := range []string{srv.URL, srv.URL + "/", srv.URL + "/api", srv.URL + "/api/"} {
		ha := iot.NewHomeAssistantAdapter(base, "test-token", nil)
		if _, err := ha.Discover(context.Background()); err != nil {
			t.Errorf("base URL %q should work: %v", base, err)
		}
	}
}

// TestControllerSyncUpgradesNotDowngrades guards the Controller.Sync tier
// invariant: runtime discovery may upgrade a device tier, never downgrade.
func TestControllerSyncUpgradesNotDowngrades(t *testing.T) {
	ctrl := iot.NewController(iot.NewStubAdapter(), nil, nil)

	// Register a lock at tier 3.
	ctrl.RegisterDevice(iot.Device{ID: "lock.x", DeviceType: "lock", Tier: iot.TierSafety, Enabled: true})

	// A discovery wave claims tier 2 for the same device (e.g. mis-parsed
	// entity) — must be ignored.
	ctrl.Sync([]iot.Device{{ID: "lock.x", DeviceType: "lock", Tier: iot.TierComfort, Enabled: true}})

	for _, d := range ctrl.ListDevices() {
		if d.ID == "lock.x" && d.Tier != iot.TierSafety {
			t.Errorf("SAFETY VIOLATION: Sync downgraded lock.x to tier %d", d.Tier)
		}
	}

	// A new device from discovery must appear.
	ctrl.Sync([]iot.Device{{ID: "light.new", DeviceType: "light", Tier: iot.TierComfort, Enabled: true}})
	found := false
	for _, d := range ctrl.ListDevices() {
		if d.ID == "light.new" {
			found = true
		}
	}
	if !found {
		t.Error("Sync must register new devices")
	}
}

// Silence the unused warning for fmt if test bodies change.
var _ = fmt.Sprintf