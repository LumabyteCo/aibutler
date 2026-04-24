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

- **Today (v0.1):** `StubAdapter` for testing (in-memory device state)
- **Planned:** Home Assistant, MQTT integrations
- Config: `configurations.iot.adapter: "stub"` (default)

## Source Files

- `internal/iot/iot.go` -- Controller, ReadSensor, ExecuteCommand, checkSafetyBounds
- `internal/iot/pin.go` -- PINVerifier (bcrypt + vault)
- `internal/iot/types.go` -- Device, Command, Tier constants, SensorReading, DeviceAdapter
- `internal/iot/stub.go` -- StubAdapter for testing
