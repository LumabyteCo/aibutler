package iot

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// HomeAssistantAdapter talks to a Home Assistant instance over its REST API
// (http://<host>:8123/api/...). It implements DeviceAdapter so the existing
// Controller, capability engine, and three-tier safety model work unchanged.
//
// Auth: a long-lived access token (Profile → Security → Long-lived access
// tokens in HA) stored in the Butler vault under the "homeassistant_token"
// key. The instance URL comes from configurations.iot.ha_url.
//
// Device IDs are HA entity IDs ("light.kitchen", "lock.front_door"). The
// tier classification is deliberate and default-deny:
//
//	Tier 1 (sensor)  — *-measurement / binary sensors, input helpers
//	Tier 2 (comfort) — lights, switches, fans, climate, covers, media
//	Tier 3 (safety)  — locks, alarms, garages — ALWAYS PIN, user cannot
//	                   downgrade below tier 3 via config (only upgrade).
type HomeAssistantAdapter struct {
	baseURL string
	token   string
	client  *http.Client

	mu         sync.RWMutex
	known      map[string]haEntity   // entity_id → last seen state
	tierPolicy map[string]Tier        // user overrides from config (id → tier)
}

// haEntity is the subset of the HA /api/states payload we consume.
type haEntity struct {
	EntityID string `json:"entity_id"`
	State    string `json:"state"`
	Attrs    struct {
		FriendlyName string `json:"friendly_name"`
		Unit         string `json:"unit_of_measurement"`
		DeviceClass  string `json:"device_class"`
	} `json:"attributes"`
	LastChanged string `json:"last_changed"`
}

// NewHomeAssistantAdapter creates the adapter. baseURL is the HA instance
// root (e.g. "http://homeassistant.local:8123" — trailing /api is appended).
// token is the long-lived access token; tierPolicy optionally carries
// per-device tier overrides from configuration (only upgrades to tier 3 are
// honored; downgrades of safety-critical device classes are not).
func NewHomeAssistantAdapter(baseURL, token string, tierPolicy map[string]Tier) *HomeAssistantAdapter {
	baseURL = strings.TrimRight(baseURL, "/")
	if !strings.HasSuffix(baseURL, "/api") {
		baseURL += "/api"
	}
	if tierPolicy == nil {
		tierPolicy = map[string]Tier{}
	}
	return &HomeAssistantAdapter{
		baseURL:    baseURL,
		token:      token,
		client:     &http.Client{Timeout: 10 * time.Second},
		known:      map[string]haEntity{},
		tierPolicy: tierPolicy,
	}
}

// Discover lists all controllable/sensing entities from HA and maps them to
// Butler devices with safety tiers.
func (h *HomeAssistantAdapter) Discover(ctx context.Context) ([]Device, error) {
	var entities []haEntity
	if err := h.get(ctx, "/states", &entities); err != nil {
		return nil, fmt.Errorf("ha: discover: %w", err)
	}

	h.mu.Lock()
	for _, e := range entities {
		h.known[e.EntityID] = e
	}
	h.mu.Unlock()

	devices := make([]Device, 0, len(entities))
	for _, e := range entities {
		tier, ok := classifyTier(e.EntityID)
		if !ok {
			continue // unsupported/uninteresting domain (automation, script, zone…)
		}
		// Config overrides may tighten (upgrade) a tier but never loosen a
		// safety tier — "lock.front_door stays tier 3" is a security
		// invariant, not a preference.
		if t, exists := h.tierPolicy[e.EntityID]; exists && t >= tier {
			tier = t
		}
		devices = append(devices, Device{
			ID:         e.EntityID,
			Name:       coalesce(e.Attrs.FriendlyName, e.EntityID),
			DeviceType: domainOf(e.EntityID),
			Adapter:    "homeassistant",
			Tier:       tier,
			Enabled:    e.State != "unavailable",
		})
	}
	return devices, nil
}

// ReadSensor returns the current state of a sensor entity as readings.
func (h *HomeAssistantAdapter) ReadSensor(ctx context.Context, deviceID string) ([]SensorReading, error) {
	var e haEntity
	if err := h.get(ctx, "/states/"+url.PathEscape(deviceID), &e); err != nil {
		return nil, fmt.Errorf("ha: read %s: %w", deviceID, err)
	}

	readings := []SensorReading{{
		DeviceID:  deviceID,
		Metric:    "state",
		Unit:      e.Attrs.Unit,
		Timestamp: parseHATime(e.LastChanged),
	}}

	// Numeric states get the value parsed; binary states map to 1/0.
	if v, ok := parseFloatSafe(e.State); ok {
		readings[0].Value = v
		readings[0].Metric = metricName(e)
	} else {
		switch strings.ToLower(e.State) {
		case "on", "open", "unlocked", "detected", "home", "true":
			readings[0].Value = 1
		case "off", "closed", "locked", "clear", "not_home", "false":
			readings[0].Value = 0
		default:
			readings[0].Metric = "state"
			readings[0].Value = 0
		}
	}
	return readings, nil
}

// Execute performs an action on a device through HA's services API.
// Action names map to HA service calls:
//
//	toggle / on / off   → light.switch / switch.toggle domains
//	lock / unlock       → lock.lock / lock.unlock (tier 3, PIN already verified by Controller)
//	set                 → climate/set_temperature, cover/set_position, etc. via Params
//	read                → refresh state (no service call)
func (h *HomeAssistantAdapter) Execute(ctx context.Context, cmd Command) error {
	domain := domainOf(cmd.DeviceID)
	service, payload, err := h.serviceFor(domain, cmd)
	if err != nil {
		return err
	}
	if service == "" { // pure read-back; nothing to call
		return nil
	}
	body := map[string]interface{}{
		"entity_id": cmd.DeviceID,
	}
	for k, v := range payload {
		body[k] = v
	}
	if err := h.post(ctx, "/services/"+domain+"/"+service, body, nil); err != nil {
		return fmt.Errorf("ha: %s.%s on %s: %w", domain, service, cmd.DeviceID, err)
	}
	return nil
}

// serviceFor translates a Butler command into an HA domain+service pair.
func (h *HomeAssistantAdapter) serviceFor(domain string, cmd Command) (string, map[string]interface{}, error) {
	action := strings.ToLower(strings.TrimSpace(cmd.Action))
	switch domain {
	case "light", "switch", "fan", "humidifier", "water_heater", "input_boolean", "input_button", "button":
		switch action {
		case "toggle":
			return "toggle", nil, nil
		case "on", "turn_on", "set":
			return "turn_on", paramsExcept(cmd, "temperature"), nil
		case "off", "turn_off":
			return "turn_off", nil, nil
		}
		return "", nil, fmt.Errorf("ha: unsupported action %q for domain %q", cmd.Action, domain)

	case "lock":
		switch action {
		case "lock", "locked":
			return "lock", nil, nil
		case "unlock", "open":
			return "unlock", nil, nil
		}
		return "", nil, fmt.Errorf("ha: unsupported action %q for lock", cmd.Action)

	case "climate":
		switch action {
		case "set":
			return "set_temperature", cmd.Params, nil
		case "off", "turn_off":
			return "turn_off", nil, nil
		case "on", "turn_on":
			return "turn_on", nil, nil
		}
		return "", nil, fmt.Errorf("ha: unsupported action %q for climate", cmd.Action)

	case "cover":
		switch action {
		case "open":
			return "open_cover", nil, nil
		case "close":
			return "close_cover", nil, nil
		case "stop":
			return "stop_cover", nil, nil
		case "set":
			return "set_cover_position", cmd.Params, nil
		}
		return "", nil, fmt.Errorf("ha: unsupported action %q for cover", cmd.Action)

	case "alarm_control_panel":
		// Arm/disarm always carries through the Controller's tier-3 PIN
		// gate; HA's own code requirement (if configured) rides inside
		// cmd.Params["code"] — never persisted, forwarded only.
		switch action {
		case "arm", "arm_home", "arm_away", "arm_night", "arm_vacation":
			return "alarm_arm_" + strings.TrimPrefix(action, "arm_"), cmd.Params, nil
		case "disarm":
			return "alarm_disarm", cmd.Params, nil
		}
		return "", nil, fmt.Errorf("ha: unsupported action %q for alarm", cmd.Action)

	case "media_player":
		switch action {
		case "play", "play_media":
			return "media_play", nil, nil
		case "pause", "stop":
			return "media_pause", nil, nil
		case "on":
			return "turn_on", nil, nil
		case "off":
			return "turn_off", nil, nil
		case "set":
			return "volume_set", cmd.Params, nil
		}
		return "", nil, fmt.Errorf("ha: unsupported action %q for media_player", cmd.Action)

	case "sensor", "binary_sensor":
		// Sensors are read-only; a read command is a no-op refresh.
		if action == "read" || action == "" {
			return "", nil, nil
		}
		return "", nil, fmt.Errorf("ha: sensors are read-only")
	}
	return "", nil, fmt.Errorf("ha: unsupported domain %q", domain)
}

// --- HTTP plumbing ---

func (h *HomeAssistantAdapter) get(ctx context.Context, path string, out interface{}) error {
	req, err := http.NewRequestWithContext(ctx, "GET", h.baseURL+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+h.token)
	req.Header.Set("Accept", "application/json")

	resp, err := h.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func (h *HomeAssistantAdapter) post(ctx context.Context, path string, body interface{}, out interface{}) error {
	data, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, "POST", h.baseURL+path, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+h.token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := h.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("status %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}
	if out == nil {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// --- classification helpers ---

// classifyTier maps an HA entity to a Butler safety tier. ok=false for
// domains we don't surface (automations, scenes, zones, config entities…).
func classifyTier(entityID string) (Tier, bool) {
	domain := domainOf(entityID)
	switch domain {
	// Tier 1 — sensing only, auto-approved reads.
	case "sensor", "binary_sensor", "input_number", "sun", "weather", "person", "device_tracker", "zone":
		return TierSensor, true

	// Tier 2 — comfort: reversible, logged, bounded.
	// input_boolean/input_button act as virtual switches in many HA
	// setups (including demos) — they classify as controllable comfort
	// devices, not sensors, so "turn on the kitchen light" works.
	case "light", "switch", "fan", "climate", "humidifier", "media_player", "scene", "script", "water_heater", "input_boolean", "input_button", "vacuum", "button":
		return TierComfort, true

	// Tier 3 — safety: locks, alarms, garages. Always PIN-gated.
	// Note: garage doors are covers, but cover.garage* entities are the
	// physical door to the house — they gate entry like a lock does, so
	// they classify as tier 3 regardless of their HA domain.
	case "lock", "alarm_control_panel", "garage_door":
		return TierSafety, true
	}

	// Garage covers: HA usually names them cover.garage_door etc.
	if domain == "cover" {
		if strings.Contains(entityID, "garage") {
			return TierSafety, true
		}
		return TierComfort, true
	}
	return TierSensor, false
}

// domainOf extracts "light" from "light.kitchen".
func domainOf(entityID string) string {
	if i := strings.IndexByte(entityID, '.'); i > 0 {
		return entityID[:i]
	}
	return entityID
}

// coalesce returns the first non-empty string.
func coalesce(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// paramsExcept copies cmd.Params minus the given keys.
func paramsExcept(cmd Command, drop ...string) map[string]interface{} {
	if len(cmd.Params) == 0 {
		return nil
	}
	out := make(map[string]interface{}, len(cmd.Params))
Drop:
	for k, v := range cmd.Params {
		for _, d := range drop {
			if k == d {
				continue Drop
			}
		}
		out[k] = v
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// parseFloatSafe parses HA numeric state strings ("21.3", "-2.5").
func parseFloatSafe(s string) (float64, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, false
	}
	var v float64
	if _, err := fmt.Sscanf(s, "%g", &v); err != nil {
		return 0, false
	}
	return v, true
}

// parseHATime parses HA's ISO-8601 timestamps, zero on failure.
func parseHATime(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02T15:04:05.999999"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t
		}
	}
	return time.Time{}
}

// metricName derives a readable metric name for a numeric sensor.
func metricName(e haEntity) string {
	if e.Attrs.DeviceClass != "" {
		return e.Attrs.DeviceClass
	}
	if e.Attrs.Unit != "" {
		return e.Attrs.Unit
	}
	return "value"
}