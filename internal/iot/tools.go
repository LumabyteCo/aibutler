package iot

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/LumabyteCo/aibutler/internal/capability"
	"github.com/LumabyteCo/aibutler/internal/tool"
)

// RegisterIoTTools registers IoT tools with the tool registry.
func RegisterIoTTools(registry *tool.Registry, controller *Controller) {
	registry.Register(&routineRunTool{controller: controller})
	registry.Register(&sensorReadTool{controller: controller})
	registry.Register(&deviceControlTool{controller: controller})
	registry.Register(&safetyControlTool{controller: controller})
	registry.Register(&deviceListTool{controller: controller})
	registry.Register(&deviceDiscoverTool{controller: controller})
}

// sensorReadTool reads a sensor value.
type sensorReadTool struct{ controller *Controller }

type sensorReadInput struct {
	DeviceID string `json:"device_id"`
}

func (t *sensorReadTool) Name() string        { return "iot.sensor.read" }
func (t *sensorReadTool) Description() string { return "Read a sensor value from an IoT device." }
func (t *sensorReadTool) Capability() string  { return "iot.sensor.read" }

func (t *sensorReadTool) Schema() string {
	return `{
		"type": "object",
		"properties": {
			"device_id": {"type": "string", "description": "Device ID to read from"}
		},
		"required": ["device_id"]
	}`
}

func (t *sensorReadTool) Execute(ctx context.Context, input string) (string, error) {
	var in sensorReadInput
	if err := json.Unmarshal([]byte(input), &in); err != nil {
		return "", fmt.Errorf("iot.sensor.read: %w", err)
	}
	caps := capability.CapsFromContext(ctx)
	if caps == nil {
		return "", fmt.Errorf("iot.sensor.read: no capabilities in context")
	}
	readings, err := t.controller.ReadSensor(ctx, caps, in.DeviceID)
	if err != nil {
		return "", err
	}
	data, _ := json.Marshal(readings)
	return string(data), nil
}

// deviceControlTool controls a comfort device.
type deviceControlTool struct{ controller *Controller }

type deviceControlInput struct {
	DeviceID string                 `json:"device_id"`
	Action   string                 `json:"action"`
	Params   map[string]interface{} `json:"params"`
}

func (t *deviceControlTool) Name() string        { return "iot.device.control" }
func (t *deviceControlTool) Description() string {
	return "Control a comfort IoT device (lights, switches, thermostat, fans, covers, media). " +
		"For tier-3 devices (locks, alarms, garages) use iot.safety.control instead."
}
func (t *deviceControlTool) Capability() string  { return "iot.device.control" }

func (t *deviceControlTool) Schema() string {
	return `{
		"type": "object",
		"properties": {
			"device_id": {"type": "string", "description": "Device ID to control"},
			"action":    {"type": "string", "description": "Action to perform (e.g. set, toggle)"},
			"params":    {"type": "object", "description": "Action parameters"}
		},
		"required": ["device_id", "action"]
	}`
}

func (t *deviceControlTool) Execute(ctx context.Context, input string) (string, error) {
	var in deviceControlInput
	if err := json.Unmarshal([]byte(input), &in); err != nil {
		return "", fmt.Errorf("iot.device.control: %w", err)
	}
	caps := capability.CapsFromContext(ctx)
	if caps == nil {
		return "", fmt.Errorf("iot.device.control: no capabilities in context")
	}
	cmd := Command{DeviceID: in.DeviceID, Action: in.Action, Params: in.Params}
	if err := t.controller.ExecuteCommand(ctx, caps, cmd); err != nil {
		return "", err
	}
	return fmt.Sprintf("Device %s: %s executed.", in.DeviceID, in.Action), nil
}

// safetyControlTool controls a safety-critical device.
type safetyControlTool struct{ controller *Controller }

type safetyControlInput struct {
	DeviceID  string                 `json:"device_id"`
	Action    string                 `json:"action"`
	Params    map[string]interface{} `json:"params"`
	Confirmed bool                   `json:"confirmed"`
	PIN       string                 `json:"pin"`
}

func (t *safetyControlTool) Name() string        { return "iot.safety.control" }
func (t *safetyControlTool) Description() string {
	return "Control a safety-critical IoT device (locks, alarm panels, garage doors). " +
		"Use when the user asks to lock/unlock a door, arm/disarm an alarm, or open/close a garage — " +
		"even if a previous attempt was denied. If the user provides a PIN in their message, pass it in the " +
		"'pin' field along with confirmed=true. If no PIN was given, attempt the call and report the " +
		"PIN-required error to the user."
}
func (t *safetyControlTool) Capability() string  { return "iot.safety.control" }

func (t *safetyControlTool) Schema() string {
	return `{
		"type": "object",
		"properties": {
			"device_id":  {"type": "string", "description": "Device ID to control"},
			"action":     {"type": "string", "description": "Action to perform"},
			"params":     {"type": "object", "description": "Action parameters"},
			"confirmed":  {"type": "boolean", "description": "User has confirmed the action"},
			"pin":        {"type": "string", "description": "User PIN code"}
		},
		"required": ["device_id", "action", "confirmed", "pin"]
	}`
}

func (t *safetyControlTool) Execute(ctx context.Context, input string) (string, error) {
	var in safetyControlInput
	if err := json.Unmarshal([]byte(input), &in); err != nil {
		return "", fmt.Errorf("iot.safety.control: %w", err)
	}
	caps := capability.CapsFromContext(ctx)
	if caps == nil {
		return "", fmt.Errorf("iot.safety.control: no capabilities in context")
	}
	cmd := Command{
		DeviceID: in.DeviceID, Action: in.Action, Params: in.Params,
		Confirmed: in.Confirmed, PIN: in.PIN,
	}
	if err := t.controller.ExecuteCommand(ctx, caps, cmd); err != nil {
		return "", err
	}
	return fmt.Sprintf("Safety device %s: %s executed.", in.DeviceID, in.Action), nil
}

// deviceListTool lists all registered devices.
type deviceListTool struct{ controller *Controller }

func (t *deviceListTool) Name() string        { return "iot.device.list" }
func (t *deviceListTool) Description() string { return "List all registered IoT devices." }
func (t *deviceListTool) Capability() string  { return "iot.device.discover" }

func (t *deviceListTool) Schema() string {
	return `{"type": "object", "properties": {}}`
}

func (t *deviceListTool) Execute(_ context.Context, _ string) (string, error) {
	devices := t.controller.ListDevices()
	data, _ := json.Marshal(devices)
	return string(data), nil
}

// deviceDiscoverTool discovers new devices via the adapter.
type deviceDiscoverTool struct{ controller *Controller }

func (t *deviceDiscoverTool) Name() string        { return "iot.device.discover" }
func (t *deviceDiscoverTool) Description() string { return "Discover new IoT devices on the network." }
func (t *deviceDiscoverTool) Capability() string  { return "iot.device.discover" }

func (t *deviceDiscoverTool) Schema() string {
	return `{"type": "object", "properties": {}}`
}

func (t *deviceDiscoverTool) Execute(ctx context.Context, _ string) (string, error) {
	devices, err := t.controller.adapter.Discover(ctx)
	if err != nil {
		return "", fmt.Errorf("iot.discover: %w", err)
	}
	// Register discovered devices with the controller so they are
	// immediately controllable — without this, HA entities found at
	// runtime could be listed but not executed until a restart.
	t.controller.Sync(devices)
	data, _ := json.Marshal(devices)
	return string(data), nil
}

// routineRunTool runs a named smart-home routine ("goodnight" =
// lights→locks→climate) with every existing gate applying per step.
type routineRunTool struct{ controller *Controller }

type routineRunInput struct {
	Name string `json:"name"`
	PIN  string `json:"pin"`
}

func (t *routineRunTool) Name() string { return "iot.routine.run" }
func (t *routineRunTool) Description() string {
	return "Run a smart-home routine (a named chain of device commands). CALL THIS whenever the user says " +
		"\"goodnight\", \"good morning\", \"leaving home\" or \"movie time\" — even as a single word with no other " +
		"context — instead of replying socially. Routines: goodnight (all lights off → lock all doors → set " +
		"temperature), good_morning, leaving_home, movie_time. Every step passes the same safety gates as a " +
		"manual command: locking doors requires the safety PIN, so pass the user's PIN in the 'pin' field when " +
		"the message provides one. Without a PIN, safety steps report pin_required and comfort steps still complete."
}
func (t *routineRunTool) Capability() string { return "iot.device.control" }

func (t *routineRunTool) Schema() string {
	return `{
		"type": "object",
		"properties": {
			"name": {"type": "string", "description": "Routine name (goodnight, good_morning, leaving_home, movie_time)"},
			"pin":  {"type": "string", "description": "Safety PIN if the user provided one (for lock/alarm steps)"}
		},
		"required": ["name"]
	}`
}

func (t *routineRunTool) Execute(ctx context.Context, input string) (string, error) {
	var in routineRunInput
	if err := json.Unmarshal([]byte(input), &in); err != nil {
		return "", fmt.Errorf("iot.routine.run: %w", err)
	}
	caps := capability.CapsFromContext(ctx)
	if caps == nil {
		return "", fmt.Errorf("iot.routine.run: no capabilities in context")
	}
	results, err := t.controller.RunRoutine(ctx, caps, in.Name, in.PIN)
	if err != nil {
		return "", err
	}
	data, _ := json.Marshal(results)
	return string(data), nil
}
