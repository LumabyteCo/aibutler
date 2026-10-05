# Smart Home (IoT)

## Quick Example

```
User: "What's the living room temperature?"
  -> Controller.ReadSensor(caps, "temperature-living-room")
  -> Capability check: iot.sensor.read for device "temperature-living-room" -- OK
  -> adapter.ReadSensor("temperature-living-room")
  -> [{Metric: "temperature", Value: 22.5, Unit: "C"}]

User: "Set thermostat to 24C"
  -> Controller.ExecuteCommand(caps, {DeviceID: "thermostat-main", Action: "set", Params: {temperature: 24}})
  -> Capability check: iot.device.control -- OK
  -> Safety bounds: 24C within 5-32C range -- OK
  -> adapter.Execute(cmd)

User: "Unlock the front door"
  -> Controller.ExecuteCommand(caps, {DeviceID: "lock-front", Action: "toggle", Confirmed: true, PIN: "1234"})
  -> Capability check: iot.safety.control -- OK
  -> Confirmation required: true -- provided
  -> PIN required: bcrypt verify against vault -- OK
  -> adapter.Execute(cmd)
```

## 3-Tier Security Model

| Tier | Name    | Examples               | Security                              |
|------|---------|------------------------|---------------------------------------|
| 1    | Sensor  | Temperature, humidity   | Capability check only (auto-approved) |
| 2    | Comfort | Thermostat, lights      | Capability + rate limit + safety bounds |
| 3    | Safety  | Locks, garage doors     | Capability + confirmation + PIN       |

### Tier 2: Safety Bounds

Comfort devices enforce hard limits. Example: thermostat temperature must be 5-32C. Commands outside bounds are rejected with `ErrSafetyBound`.

### Tier 3: PIN Verification

- PIN stored as bcrypt hash in the credential vault (key: `iot_pin`, type: `CredIoTPIN`)
- `PINVerifier.SetPIN()` hashes with `bcrypt.DefaultCost`
- `PINVerifier.Verify()` compares with `bcrypt.CompareHashAndPassword`
- Both confirmation flag AND valid PIN required -- missing either returns an error

## Adapters

`DeviceAdapter` interface: `ReadSensor`, `Execute`, `Discover`.

- **Stub** (`configurations.iot.adapter: "stub"`, default) — in-memory demo
  devices so the natural-language flow works out of the box
- **Home Assistant** (`configurations.iot.adapter: "homeassistant"`) — real
  integration, shipped: REST API discovery, service-call mapping, URL
  normalization, bearer-token auth
- Config: `configurations.iot.adapter` selects between them

### Home Assistant Setup

1. Create a long-lived access token in HA: **Profile → Security → Long-lived access tokens**
2. Store it in the Butler vault:
   ```bash
   aibutler vault set homeassistant_token <your-token>
   ```
3. Configure the adapter in `~/.aibutler/config.yaml`:
   ```yaml
   configurations:
     iot:
       adapter: homeassistant
       ha_url: http://homeassistant.local:8123
   ```
4. Restart (`aibutler run`) — entities are discovered at boot; say
   *"list my smart home devices"* or use `iot.device.discover` to re-scan
   without restarting.

### How HA entities map to Butler devices

| HA domain | Butler tier | Notes |
|---|---|---|
| `sensor`, `binary_sensor`, `weather`, `person`, `device_tracker` | 1 — Sensor | auto-approved reads |
| `light`, `switch`, `fan`, `climate`, `cover` (non-garage), `media_player`, `scene`, `vacuum`, `input_boolean`, `input_button` | 2 — Comfort | logged, rate-limited, safety-bounded |
| `lock`, `alarm_control_panel`, `cover.garage*` | 3 — Safety | **always** confirmation + PIN |

Device IDs are HA entity IDs (`light.kitchen`, `lock.front_door`). Friendly
names come from HA attributes.

**Tier policy (security invariant):** per-entity overrides via
`configurations.iot.tier_policy` may *upgrade* a device's tier but can
**never downgrade a safety device below tier 3**. A lock stays PIN-gated no
matter what the config says. Runtime discovery follows the same rule.

**Tier-3 capability (deny by default):** chat control of locks/alarms/garages
additionally requires `configurations.iot.safety_control_enabled: true`, and
every call still demands confirmation + the safety PIN
(`aibutler iot set-pin`). The flag opens the capability gate, never the PIN gate.

**When HA is unreachable at boot:** Butler starts normally, warns in the
log, and `iot.device.discover` retries the connection on demand — the rest
of the assistant is unaffected.

### Fast intents + the hybrid router (offline smart home)

Smart-home commands are usually short — "turn off the kitchen light",
"goodnight". Those don't need a frontier model. Configure a small local
model and Butler routes them locally:

```yaml
configurations:
  models:
    primary: glm-5.3            # your reasoning model (cloud or big local)
    local: qwen3:4b             # small Ollama model (any 3-8B works)
    routing:
      fast_intent_local: true   # opt-in
```

- Short device commands answer in ~1-2s from the local model — **and keep
  working with the internet completely down** (the router falls back to
  local automatically when the primary is unreachable).
- Everything else — memory recall, files, reasoning — goes to the primary
  model, unchanged.
- The local model is pre-warmed at boot so the first command doesn't pay
  the model-load time.
- Mid-flight agent turns (tool loops) always use the primary: tiny models
  don't reason over tool schemas.

This is the configuration the Raspberry Pi story targets: a Pi running
Butler + Ollama with a 4B model controls the house offline; the cloud
model adds the brainpower when the network is there.

## Source Files

- `internal/iot/iot.go` -- Controller, ReadSensor, ExecuteCommand, Sync, checkSafetyBounds
- `internal/iot/pin.go` -- PINVerifier (bcrypt + vault)
- `internal/iot/homeassistant.go` -- HomeAssistantAdapter (REST API, discovery, service mapping)
- `internal/iot/types.go` -- Device, Command, Tier constants, SensorReading, DeviceAdapter
- `internal/iot/stub.go` -- StubAdapter for testing
