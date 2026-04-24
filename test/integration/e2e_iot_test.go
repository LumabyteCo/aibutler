//go:build integration

package integration

import (
	"context"
	"strings"
	"testing"

	"github.com/LumabyteCo/aibutler/internal/agent"
	"github.com/LumabyteCo/aibutler/internal/capability"
)

// iotCaps builds the full IoT capability set including messaging defaults,
// IoT defaults, and explicit device.control + safety.control grants.
func iotCaps() *capability.CapabilitySet {
	allCaps := append(capability.MessagingDefaults(), capability.IoTDefaults()...)
	allCaps = append(allCaps,
		capability.Capability{Resource: "iot.device.control", Devices: []string{"*"}},
		capability.Capability{Resource: "iot.safety.control", Devices: []string{"*"}},
	)
	return capability.NewCapabilitySet(allCaps)
}

// TestE2EIoTSensorRead verifies that the model can read a sensor via
// iot.sensor.read and the tool result contains the pre-registered reading.
func TestE2EIoTSensorRead(t *testing.T) {
	caps := iotCaps()

	p := setupPipelineWithOpts(t, pipelineOpts{
		WithIoT:     true,
		CapOverride: caps,
		Responses: []agent.Response{
			toolCallResponse("Reading sensor.",
				tc("iot1", "iot.sensor.read", `{"device_id":"temp-1"}`),
			),
			finalResponse("Temperature is 22.5°C."),
		},
	})

	ctx := capability.WithCaps(context.Background(), caps)
	p.sendMsgWithContext(t, ctx, "What's the temperature?")

	// Verify model called twice (tool call + final).
	if p.Fake.CallCount() != 2 {
		t.Fatalf("model calls = %d, want 2", p.Fake.CallCount())
	}

	// Verify tool result contains temperature reading.
	calls := p.Fake.Calls()
	found := false
	for _, msg := range calls[1] {
		if msg.Role == "tool" && strings.Contains(msg.Content, "22.5") && strings.Contains(msg.Content, "temperature") {
			found = true
			break
		}
	}
	if !found {
		t.Error("sensor read tool result should contain '22.5' and 'temperature'")
	}

	resp := p.lastResponse(t)
	if resp != "Temperature is 22.5°C." {
		t.Errorf("response = %q, want %q", resp, "Temperature is 22.5°C.")
	}
}

// TestE2EIoTDeviceControl verifies that the model can toggle a comfort device
// via iot.device.control and the command is recorded by the stub adapter.
func TestE2EIoTDeviceControl(t *testing.T) {
	caps := iotCaps()

	p := setupPipelineWithOpts(t, pipelineOpts{
		WithIoT:     true,
		CapOverride: caps,
		Responses: []agent.Response{
			toolCallResponse("Toggling the light.",
				tc("iot1", "iot.device.control", `{"device_id":"light-1","action":"toggle","params":{}}`),
			),
			finalResponse("Light toggled."),
		},
	})

	ctx := capability.WithCaps(context.Background(), caps)
	p.sendMsgWithContext(t, ctx, "Toggle the living room light")

	// Verify model called twice.
	if p.Fake.CallCount() != 2 {
		t.Fatalf("model calls = %d, want 2", p.Fake.CallCount())
	}

	// Verify tool result contains success message.
	calls := p.Fake.Calls()
	found := false
	for _, msg := range calls[1] {
		if msg.Role == "tool" && strings.Contains(msg.Content, "light-1") && strings.Contains(msg.Content, "toggle executed") {
			found = true
			break
		}
	}
	if !found {
		t.Error("device control tool result should contain 'light-1' and 'toggle executed'")
	}

	// Verify the stub adapter recorded the command.
	executed := p.IoTAdapter.Executed()
	if len(executed) != 1 {
		t.Fatalf("executed commands = %d, want 1", len(executed))
	}
	if executed[0].DeviceID != "light-1" {
		t.Errorf("executed device = %q, want %q", executed[0].DeviceID, "light-1")
	}
	if executed[0].Action != "toggle" {
		t.Errorf("executed action = %q, want %q", executed[0].Action, "toggle")
	}

	resp := p.lastResponse(t)
	if resp != "Light toggled." {
		t.Errorf("response = %q, want %q", resp, "Light toggled.")
	}
}

// TestE2EIoTSafetyControlWithPIN verifies that a safety device (lock) can be
// unlocked when the correct PIN and confirmation are provided.
func TestE2EIoTSafetyControlWithPIN(t *testing.T) {
	caps := iotCaps()

	p := setupPipelineWithOpts(t, pipelineOpts{
		WithIoT:     true,
		CapOverride: caps,
		Responses: []agent.Response{
			toolCallResponse("Unlocking the door.",
				tc("iot1", "iot.safety.control", `{"device_id":"lock-1","action":"unlock","confirmed":true,"pin":"1234","params":{}}`),
			),
			finalResponse("Front door unlocked."),
		},
	})

	ctx := capability.WithCaps(context.Background(), caps)
	p.sendMsgWithContext(t, ctx, "Unlock the front door, PIN 1234")

	// Verify model called twice.
	if p.Fake.CallCount() != 2 {
		t.Fatalf("model calls = %d, want 2", p.Fake.CallCount())
	}

	// Verify tool result contains success message.
	calls := p.Fake.Calls()
	found := false
	for _, msg := range calls[1] {
		if msg.Role == "tool" && strings.Contains(msg.Content, "lock-1") && strings.Contains(msg.Content, "unlock executed") {
			found = true
			break
		}
	}
	if !found {
		t.Error("safety control tool result should contain 'lock-1' and 'unlock executed'")
	}

	// Verify the stub adapter recorded the command.
	executed := p.IoTAdapter.Executed()
	if len(executed) != 1 {
		t.Fatalf("executed commands = %d, want 1", len(executed))
	}
	if executed[0].DeviceID != "lock-1" {
		t.Errorf("executed device = %q, want %q", executed[0].DeviceID, "lock-1")
	}

	resp := p.lastResponse(t)
	if resp != "Front door unlocked." {
		t.Errorf("response = %q, want %q", resp, "Front door unlocked.")
	}
}

// TestE2EIoTSafetyControlNoPIN verifies that a safety device rejects an
// unlock attempt with a wrong PIN, returning an error in the tool result.
func TestE2EIoTSafetyControlNoPIN(t *testing.T) {
	caps := iotCaps()

	p := setupPipelineWithOpts(t, pipelineOpts{
		WithIoT:     true,
		CapOverride: caps,
		Responses: []agent.Response{
			toolCallResponse("Trying to unlock.",
				tc("iot1", "iot.safety.control", `{"device_id":"lock-1","action":"unlock","confirmed":true,"pin":"wrong","params":{}}`),
			),
			finalResponse("Sorry, the PIN was incorrect."),
		},
	})

	ctx := capability.WithCaps(context.Background(), caps)
	p.sendMsgWithContext(t, ctx, "Unlock the front door, PIN wrong")

	// Verify model called twice (error tool result + final response).
	if p.Fake.CallCount() != 2 {
		t.Fatalf("model calls = %d, want 2", p.Fake.CallCount())
	}

	// Verify tool result contains an error about invalid PIN.
	calls := p.Fake.Calls()
	found := false
	for _, msg := range calls[1] {
		if msg.Role == "tool" && strings.Contains(msg.Content, "error:") && strings.Contains(msg.Content, "invalid PIN") {
			found = true
			break
		}
	}
	if !found {
		t.Error("safety control tool result should contain 'error:' and 'invalid PIN'")
	}

	// Verify NO command was executed on the adapter.
	executed := p.IoTAdapter.Executed()
	if len(executed) != 0 {
		t.Errorf("executed commands = %d, want 0 (PIN was wrong)", len(executed))
	}

	resp := p.lastResponse(t)
	if resp != "Sorry, the PIN was incorrect." {
		t.Errorf("response = %q, want %q", resp, "Sorry, the PIN was incorrect.")
	}
}

// TestE2EIoTSafetyNoConfirmation verifies that a safety device rejects an
// unlock attempt when confirmed=false, even if the PIN is correct.
func TestE2EIoTSafetyNoConfirmation(t *testing.T) {
	caps := iotCaps()

	p := setupPipelineWithOpts(t, pipelineOpts{
		WithIoT:     true,
		CapOverride: caps,
		Responses: []agent.Response{
			toolCallResponse("Attempting unlock without confirmation.",
				tc("iot1", "iot.safety.control", `{"device_id":"lock-1","action":"unlock","confirmed":false,"pin":"1234","params":{}}`),
			),
			finalResponse("Please confirm the unlock action first."),
		},
	})

	ctx := capability.WithCaps(context.Background(), caps)
	p.sendMsgWithContext(t, ctx, "Unlock the front door")

	// Verify model called twice.
	if p.Fake.CallCount() != 2 {
		t.Fatalf("model calls = %d, want 2", p.Fake.CallCount())
	}

	// Verify tool result contains an error about confirmation required.
	calls := p.Fake.Calls()
	found := false
	for _, msg := range calls[1] {
		if msg.Role == "tool" && strings.Contains(msg.Content, "error:") && strings.Contains(msg.Content, "confirmation required") {
			found = true
			break
		}
	}
	if !found {
		t.Error("safety control tool result should contain 'error:' and 'confirmation required'")
	}

	// Verify NO command was executed on the adapter.
	executed := p.IoTAdapter.Executed()
	if len(executed) != 0 {
		t.Errorf("executed commands = %d, want 0 (confirmation was false)", len(executed))
	}

	resp := p.lastResponse(t)
	if resp != "Please confirm the unlock action first." {
		t.Errorf("response = %q, want %q", resp, "Please confirm the unlock action first.")
	}
}

// TestE2EIoTDeviceList verifies that iot.device.list returns all pre-registered
// device IDs in its JSON result.
func TestE2EIoTDeviceList(t *testing.T) {
	caps := iotCaps()

	p := setupPipelineWithOpts(t, pipelineOpts{
		WithIoT:     true,
		CapOverride: caps,
		Responses: []agent.Response{
			toolCallResponse("Listing devices.",
				tc("iot1", "iot.device.list", `{}`),
			),
			finalResponse("You have 4 devices registered."),
		},
	})

	ctx := capability.WithCaps(context.Background(), caps)
	p.sendMsgWithContext(t, ctx, "List all my IoT devices")

	// Verify model called twice.
	if p.Fake.CallCount() != 2 {
		t.Fatalf("model calls = %d, want 2", p.Fake.CallCount())
	}

	// Verify tool result contains all device IDs.
	calls := p.Fake.Calls()
	deviceIDs := []string{"temp-1", "light-1", "thermo-1", "lock-1"}
	for _, msg := range calls[1] {
		if msg.Role == "tool" {
			for _, id := range deviceIDs {
				if !strings.Contains(msg.Content, id) {
					t.Errorf("device list tool result should contain %q", id)
				}
			}
			break
		}
	}

	resp := p.lastResponse(t)
	if resp != "You have 4 devices registered." {
		t.Errorf("response = %q, want %q", resp, "You have 4 devices registered.")
	}
}

// TestE2EIoTSafetyBounds verifies that the thermostat rejects a temperature
// that exceeds the safety bound (max 32°C) when set via iot.device.control.
func TestE2EIoTSafetyBounds(t *testing.T) {
	caps := iotCaps()

	p := setupPipelineWithOpts(t, pipelineOpts{
		WithIoT:     true,
		CapOverride: caps,
		Responses: []agent.Response{
			toolCallResponse("Setting thermostat to 50°C.",
				tc("iot1", "iot.device.control", `{"device_id":"thermo-1","action":"set","params":{"temperature":50}}`),
			),
			finalResponse("Cannot set temperature to 50°C — maximum is 32°C."),
		},
	})

	ctx := capability.WithCaps(context.Background(), caps)
	p.sendMsgWithContext(t, ctx, "Set thermostat to 50 degrees")

	// Verify model called twice (error tool result + final response).
	if p.Fake.CallCount() != 2 {
		t.Fatalf("model calls = %d, want 2", p.Fake.CallCount())
	}

	// Verify tool result contains safety bound error.
	calls := p.Fake.Calls()
	found := false
	for _, msg := range calls[1] {
		if msg.Role == "tool" && strings.Contains(msg.Content, "error:") && strings.Contains(msg.Content, "safety bound") {
			found = true
			break
		}
	}
	if !found {
		t.Error("device control tool result should contain 'error:' and 'safety bound'")
	}

	// Verify NO command was executed on the adapter.
	executed := p.IoTAdapter.Executed()
	if len(executed) != 0 {
		t.Errorf("executed commands = %d, want 0 (safety bound violated)", len(executed))
	}

	resp := p.lastResponse(t)
	if !strings.Contains(resp, "32°C") {
		t.Errorf("response = %q, want mention of 32°C limit", resp)
	}
}
