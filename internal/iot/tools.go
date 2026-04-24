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
func (t *deviceControlTool) Description() string { return "Control a comfort IoT device (lights, thermostat, etc.)." }
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
func (t *safetyControlTool) Description() string { return "Control a safety-critical IoT device (locks, alarms). Requires confirmation and PIN." }
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
	data, _ := json.Marshal(devices)
	return string(data), nil
}
