package iot_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/LumabyteCo/aibutler/internal/capability"
	"github.com/LumabyteCo/aibutler/internal/iot"
	"github.com/LumabyteCo/aibutler/internal/tool"
	"github.com/LumabyteCo/aibutler/testutil"
)

// --- Tier constants ---

func TestTierConstants(t *testing.T) {
	if iot.TierSensor != 1 {
		t.Errorf("TierSensor = %d, want 1", iot.TierSensor)
	}
	if iot.TierComfort != 2 {
		t.Errorf("TierComfort = %d, want 2", iot.TierComfort)
	}
	if iot.TierSafety != 3 {
		t.Errorf("TierSafety = %d, want 3", iot.TierSafety)
	}
}

// --- PIN tests ---

func TestPINSet(t *testing.T) {
	v := testutil.NewFakeVault()
	pv := iot.NewPINVerifier(v)
	ctx := context.Background()

	if err := pv.SetPIN(ctx, "1234"); err != nil {
		t.Fatalf("SetPIN: %v", err)
	}

	// Verify something was stored in the vault.
	has, err := v.Has(ctx, "iot_pin")
	if err != nil {
		t.Fatalf("Has: %v", err)
	}
	if !has {
		t.Error("expected iot_pin to be stored in vault")
	}
}

func TestPINVerify(t *testing.T) {
	v := testutil.NewFakeVault()
	pv := iot.NewPINVerifier(v)
	ctx := context.Background()

	if err := pv.SetPIN(ctx, "1234"); err != nil {
		t.Fatalf("SetPIN: %v", err)
	}

	ok, err := pv.Verify(ctx, "1234")
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if !ok {
		t.Error("expected Verify(1234) to return true")
	}
}

func TestPINVerifyWrong(t *testing.T) {
	v := testutil.NewFakeVault()
	pv := iot.NewPINVerifier(v)
	ctx := context.Background()

	if err := pv.SetPIN(ctx, "1234"); err != nil {
		t.Fatalf("SetPIN: %v", err)
	}

	ok, err := pv.Verify(ctx, "9999")
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if ok {
		t.Error("expected Verify(9999) to return false")
	}
}

// --- StubAdapter tests ---

func TestStubAdapterReadSensor(t *testing.T) {
	stub := iot.NewStubAdapter()
	stub.AddDevice(iot.Device{ID: "temp-1", Tier: iot.TierSensor})
	stub.AddReading("temp-1", iot.SensorReading{
		DeviceID: "temp-1",
		Metric:   "temperature",
		Value:    21.5,
		Unit:     "°C",
	})

	readings, err := stub.ReadSensor(context.Background(), "temp-1")
	if err != nil {
		t.Fatalf("ReadSensor: %v", err)
	}
	if len(readings) != 1 {
		t.Fatalf("got %d readings, want 1", len(readings))
	}
	if readings[0].Value != 21.5 {
		t.Errorf("Value = %f, want 21.5", readings[0].Value)
	}
	if readings[0].Timestamp.IsZero() {
		t.Error("expected Timestamp to be set")
	}
}

func TestStubAdapterExecute(t *testing.T) {
	stub := iot.NewStubAdapter()
	stub.AddDevice(iot.Device{ID: "light-1", Tier: iot.TierComfort})

	cmd := iot.Command{DeviceID: "light-1", Action: "toggle"}
	if err := stub.Execute(context.Background(), cmd); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	executed := stub.Executed()
	if len(executed) != 1 {
		t.Fatalf("got %d executed commands, want 1", len(executed))
	}
	if executed[0].DeviceID != "light-1" {
		t.Errorf("DeviceID = %q, want %q", executed[0].DeviceID, "light-1")
	}
	if executed[0].Action != "toggle" {
		t.Errorf("Action = %q, want %q", executed[0].Action, "toggle")
	}
}

func TestStubAdapterDiscover(t *testing.T) {
	stub := iot.NewStubAdapter()
	stub.AddDevice(iot.Device{ID: "dev-1", Name: "Device 1"})
	stub.AddDevice(iot.Device{ID: "dev-2", Name: "Device 2"})

	devices, err := stub.Discover(context.Background())
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(devices) != 2 {
		t.Fatalf("got %d devices, want 2", len(devices))
	}
}

// --- Controller tests ---

// helper: create a Controller with an engine and stub adapter.
func newTestController(t *testing.T) (*iot.Controller, *iot.StubAdapter, *iot.PINVerifier) {
	t.Helper()
	stub := iot.NewStubAdapter()
	engine := capability.NewEngine(nil)
	v := testutil.NewFakeVault()
	pin := iot.NewPINVerifier(v)
	ctrl := iot.NewController(stub, engine, pin)
	return ctrl, stub, pin
}

func TestControllerReadSensor(t *testing.T) {
	ctrl, stub, _ := newTestController(t)

	// Register a tier 1 sensor device in both controller and stub.
	dev := iot.Device{ID: "temp-living", Tier: iot.TierSensor, Enabled: true}
	ctrl.RegisterDevice(dev)
	stub.AddDevice(dev)
	stub.AddReading("temp-living", iot.SensorReading{
		DeviceID: "temp-living",
		Metric:   "temperature",
		Value:    22.0,
		Unit:     "°C",
	})

	// Create capability set with sensor.read grant.
	caps := capability.NewCapabilitySet([]capability.Capability{
		{Resource: "iot.sensor.read", Devices: []string{"*"}},
	})

	readings, err := ctrl.ReadSensor(context.Background(), caps, "temp-living")
	if err != nil {
		t.Fatalf("ReadSensor: %v", err)
	}
	if len(readings) != 1 {
		t.Fatalf("got %d readings, want 1", len(readings))
	}
	if readings[0].Value != 22.0 {
		t.Errorf("Value = %f, want 22.0", readings[0].Value)
	}
}

func TestControllerComfortDevice(t *testing.T) {
	ctrl, stub, _ := newTestController(t)

	dev := iot.Device{ID: "light-bedroom", Tier: iot.TierComfort, Enabled: true}
	ctrl.RegisterDevice(dev)
	stub.AddDevice(dev)

	caps := capability.NewCapabilitySet([]capability.Capability{
		{Resource: "iot.device.control", Devices: []string{"*"}},
	})

	cmd := iot.Command{DeviceID: "light-bedroom", Action: "toggle"}
	if err := ctrl.ExecuteCommand(context.Background(), caps, cmd); err != nil {
		t.Fatalf("ExecuteCommand: %v", err)
	}

	executed := stub.Executed()
	if len(executed) != 1 {
		t.Fatalf("got %d executed, want 1", len(executed))
	}
}

func TestControllerComfortSafetyBounds(t *testing.T) {
	ctrl, stub, _ := newTestController(t)

	dev := iot.Device{ID: "thermo-1", Tier: iot.TierComfort, DeviceType: "thermostat", Enabled: true}
	ctrl.RegisterDevice(dev)
	stub.AddDevice(dev)

	caps := capability.NewCapabilitySet([]capability.Capability{
		{Resource: "iot.device.control", Devices: []string{"*"}},
	})

	// Temperature above maximum (32°C).
	cmd := iot.Command{
		DeviceID: "thermo-1",
		Action:   "set",
		Params:   map[string]interface{}{"temperature": 40.0},
	}
	err := ctrl.ExecuteCommand(context.Background(), caps, cmd)
	if err == nil {
		t.Fatal("expected error for temperature above max")
	}
	if !errors.Is(err, iot.ErrSafetyBound) {
		t.Errorf("got %v, want ErrSafetyBound", err)
	}
}

func TestControllerComfortSafetyBoundsMin(t *testing.T) {
	ctrl, stub, _ := newTestController(t)

	dev := iot.Device{ID: "thermo-1", Tier: iot.TierComfort, DeviceType: "thermostat", Enabled: true}
	ctrl.RegisterDevice(dev)
	stub.AddDevice(dev)

	caps := capability.NewCapabilitySet([]capability.Capability{
		{Resource: "iot.device.control", Devices: []string{"*"}},
	})

	// Temperature below minimum (5°C).
	cmd := iot.Command{
		DeviceID: "thermo-1",
		Action:   "set",
		Params:   map[string]interface{}{"temperature": 2.0},
	}
	err := ctrl.ExecuteCommand(context.Background(), caps, cmd)
	if err == nil {
		t.Fatal("expected error for temperature below min")
	}
	if !errors.Is(err, iot.ErrSafetyBound) {
		t.Errorf("got %v, want ErrSafetyBound", err)
	}
}

func TestControllerSafetyDevice(t *testing.T) {
	ctrl, stub, pin := newTestController(t)
	ctx := context.Background()

	dev := iot.Device{ID: "lock-front", Tier: iot.TierSafety, DeviceType: "lock", Enabled: true}
	ctrl.RegisterDevice(dev)
	stub.AddDevice(dev)

	// Set up PIN.
	if err := pin.SetPIN(ctx, "5678"); err != nil {
		t.Fatalf("SetPIN: %v", err)
	}

	caps := capability.NewCapabilitySet([]capability.Capability{
		{Resource: "iot.safety.control", Devices: []string{"*"}},
	})

	cmd := iot.Command{
		DeviceID:  "lock-front",
		Action:    "unlock",
		Confirmed: true,
		PIN:       "5678",
	}
	if err := ctrl.ExecuteCommand(ctx, caps, cmd); err != nil {
		t.Fatalf("ExecuteCommand: %v", err)
	}

	executed := stub.Executed()
	if len(executed) != 1 {
		t.Fatalf("got %d executed, want 1", len(executed))
	}
}

func TestControllerSafetyNoPIN(t *testing.T) {
	ctrl, stub, _ := newTestController(t)

	dev := iot.Device{ID: "lock-front", Tier: iot.TierSafety, Enabled: true}
	ctrl.RegisterDevice(dev)
	stub.AddDevice(dev)

	caps := capability.NewCapabilitySet([]capability.Capability{
		{Resource: "iot.safety.control", Devices: []string{"*"}},
	})

	cmd := iot.Command{
		DeviceID:  "lock-front",
		Action:    "unlock",
		Confirmed: true,
		PIN:       "", // No PIN
	}
	err := ctrl.ExecuteCommand(context.Background(), caps, cmd)
	if !errors.Is(err, iot.ErrPINRequired) {
		t.Errorf("got %v, want ErrPINRequired", err)
	}
}

func TestControllerSafetyBadPIN(t *testing.T) {
	ctrl, stub, pin := newTestController(t)
	ctx := context.Background()

	dev := iot.Device{ID: "lock-front", Tier: iot.TierSafety, Enabled: true}
	ctrl.RegisterDevice(dev)
	stub.AddDevice(dev)

	if err := pin.SetPIN(ctx, "5678"); err != nil {
		t.Fatalf("SetPIN: %v", err)
	}

	caps := capability.NewCapabilitySet([]capability.Capability{
		{Resource: "iot.safety.control", Devices: []string{"*"}},
	})

	cmd := iot.Command{
		DeviceID:  "lock-front",
		Action:    "unlock",
		Confirmed: true,
		PIN:       "0000", // Wrong PIN
	}
	err := ctrl.ExecuteCommand(ctx, caps, cmd)
	if !errors.Is(err, iot.ErrPINInvalid) {
		t.Errorf("got %v, want ErrPINInvalid", err)
	}
}

func TestControllerSafetyNoConfirm(t *testing.T) {
	ctrl, stub, _ := newTestController(t)

	dev := iot.Device{ID: "lock-front", Tier: iot.TierSafety, Enabled: true}
	ctrl.RegisterDevice(dev)
	stub.AddDevice(dev)

	caps := capability.NewCapabilitySet([]capability.Capability{
		{Resource: "iot.safety.control", Devices: []string{"*"}},
	})

	cmd := iot.Command{
		DeviceID:  "lock-front",
		Action:    "unlock",
		Confirmed: false, // Not confirmed
		PIN:       "5678",
	}
	err := ctrl.ExecuteCommand(context.Background(), caps, cmd)
	if !errors.Is(err, iot.ErrConfirmationRequired) {
		t.Errorf("got %v, want ErrConfirmationRequired", err)
	}
}

func TestControllerDeviceNotFound(t *testing.T) {
	ctrl, _, _ := newTestController(t)

	caps := capability.NewCapabilitySet([]capability.Capability{
		{Resource: "iot.sensor.read", Devices: []string{"*"}},
	})

	_, err := ctrl.ReadSensor(context.Background(), caps, "nonexistent")
	if !errors.Is(err, iot.ErrDeviceNotFound) {
		t.Errorf("ReadSensor: got %v, want ErrDeviceNotFound", err)
	}

	cmd := iot.Command{DeviceID: "nonexistent", Action: "toggle"}
	err = ctrl.ExecuteCommand(context.Background(), caps, cmd)
	if !errors.Is(err, iot.ErrDeviceNotFound) {
		t.Errorf("ExecuteCommand: got %v, want ErrDeviceNotFound", err)
	}
}

func TestControllerNoCapability(t *testing.T) {
	ctrl, stub, _ := newTestController(t)

	dev := iot.Device{ID: "light-1", Tier: iot.TierComfort, Enabled: true}
	ctrl.RegisterDevice(dev)
	stub.AddDevice(dev)

	// Empty capability set — no grants at all.
	caps := capability.NewCapabilitySet(nil)

	cmd := iot.Command{DeviceID: "light-1", Action: "toggle"}
	err := ctrl.ExecuteCommand(context.Background(), caps, cmd)
	if err == nil {
		t.Fatal("expected capability denied error")
	}
	// The error message should contain "capability denied".
	if !containsStr(err.Error(), "capability denied") {
		t.Errorf("error = %q, want to contain 'capability denied'", err.Error())
	}
}

// --- Tool integration test ---

func TestSensorReadTool(t *testing.T) {
	ctrl, stub, _ := newTestController(t)

	dev := iot.Device{ID: "temp-kitchen", Tier: iot.TierSensor, Enabled: true}
	ctrl.RegisterDevice(dev)
	stub.AddDevice(dev)
	stub.AddReading("temp-kitchen", iot.SensorReading{
		DeviceID: "temp-kitchen",
		Metric:   "temperature",
		Value:    23.5,
		Unit:     "°C",
	})

	// Create a tool registry and register IoT tools.
	registry := tool.NewRegistry()
	iot.RegisterIoTTools(registry, ctrl)

	// Get the sensor read tool.
	sensorTool, ok := registry.Get("iot.sensor.read")
	if !ok {
		t.Fatal("tool iot.sensor.read not found")
	}

	// Build a context with capability set.
	caps := capability.NewCapabilitySet([]capability.Capability{
		{Resource: "iot.sensor.read", Devices: []string{"*"}},
	})
	ctx := capability.WithCaps(context.Background(), caps)

	input, _ := json.Marshal(map[string]string{"device_id": "temp-kitchen"})
	output, err := sensorTool.Execute(ctx, string(input))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	var readings []iot.SensorReading
	if err := json.Unmarshal([]byte(output), &readings); err != nil {
		t.Fatalf("Unmarshal output: %v", err)
	}
	if len(readings) != 1 {
		t.Fatalf("got %d readings, want 1", len(readings))
	}
	if readings[0].Value != 23.5 {
		t.Errorf("Value = %f, want 23.5", readings[0].Value)
	}
}

// --- Helpers ---

func containsStr(s, substr string) bool {
	return len(s) >= len(substr) && searchStr(s, substr)
}

func searchStr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
