package iot

import (
	"context"
	"fmt"

	"github.com/LumabyteCo/aibutler/internal/capability"
)

// Controller orchestrates IoT operations with three-tier security.
type Controller struct {
	adapter DeviceAdapter
	engine  *capability.Engine
	pin     *PINVerifier
	devices map[string]*Device
}

// NewController creates a new IoT controller.
func NewController(adapter DeviceAdapter, engine *capability.Engine, pin *PINVerifier) *Controller {
	return &Controller{
		adapter: adapter,
		engine:  engine,
		pin:     pin,
		devices: make(map[string]*Device),
	}
}

// RegisterDevice adds a device to the controller's registry.
func (c *Controller) RegisterDevice(d Device) {
	c.devices[d.ID] = &d
}

// ReadSensor reads a sensor value. Requires iot.sensor.read capability.
func (c *Controller) ReadSensor(ctx context.Context, caps *capability.CapabilitySet, deviceID string) ([]SensorReading, error) {
	d, ok := c.devices[deviceID]
	if !ok {
		return nil, ErrDeviceNotFound
	}
	if d.Tier != TierSensor {
		return nil, fmt.Errorf("iot: device %q is tier %d, not a sensor", deviceID, d.Tier)
	}

	result := c.engine.Check(ctx, caps, capability.CheckRequest{
		Resource: "iot.sensor.read",
		Device:   deviceID,
	})
	if !result.Allowed {
		return nil, fmt.Errorf("iot: capability denied: %s", result.Reason)
	}

	return c.adapter.ReadSensor(ctx, deviceID)
}

// ExecuteCommand executes an IoT command with tier-appropriate security.
func (c *Controller) ExecuteCommand(ctx context.Context, caps *capability.CapabilitySet, cmd Command) error {
	d, ok := c.devices[cmd.DeviceID]
	if !ok {
		return ErrDeviceNotFound
	}

	// Determine required capability based on tier
	var capResource string
	switch d.Tier {
	case TierSensor:
		return fmt.Errorf("iot: use ReadSensor for tier 1 devices")
	case TierComfort:
		capResource = "iot.device.control"
	case TierSafety:
		capResource = "iot.safety.control"
	default:
		return fmt.Errorf("iot: unknown tier %d", d.Tier)
	}

	// Check capability (includes rate limiting)
	result := c.engine.Check(ctx, caps, capability.CheckRequest{
		Resource: capResource,
		Device:   cmd.DeviceID,
	})
	if !result.Allowed {
		return fmt.Errorf("iot: capability denied: %s", result.Reason)
	}

	// Tier 2: Check safety bounds
	if d.Tier == TierComfort {
		if err := checkSafetyBounds(cmd); err != nil {
			return err
		}
	}

	// Tier 3: Require confirmation + PIN
	if d.Tier == TierSafety {
		if !cmd.Confirmed {
			return ErrConfirmationRequired
		}
		if cmd.PIN == "" {
			return ErrPINRequired
		}
		ok, err := c.pin.Verify(ctx, cmd.PIN)
		if err != nil {
			return fmt.Errorf("iot: pin verify: %w", err)
		}
		if !ok {
			return ErrPINInvalid
		}
	}

	return c.adapter.Execute(ctx, cmd)
}

// ListDevices returns all registered devices.
func (c *Controller) ListDevices() []Device {
	devices := make([]Device, 0, len(c.devices))
	for _, d := range c.devices {
		devices = append(devices, *d)
	}
	return devices
}

// checkSafetyBounds enforces safety bounds for comfort devices.
func checkSafetyBounds(cmd Command) error {
	// Check thermostat bounds via command params
	if temp, ok := cmd.Params["temperature"]; ok {
		tempF, _ := toFloat64(temp)
		// Hardcoded safety bounds: 5°C min, 32°C max
		if tempF < 5 {
			return fmt.Errorf("%w: temperature %.1f below minimum 5°C", ErrSafetyBound, tempF)
		}
		if tempF > 32 {
			return fmt.Errorf("%w: temperature %.1f above maximum 32°C", ErrSafetyBound, tempF)
		}
	}
	return nil
}

func toFloat64(v interface{}) (float64, bool) {
	switch val := v.(type) {
	case float64:
		return val, true
	case float32:
		return float64(val), true
	case int:
		return float64(val), true
	case int64:
		return float64(val), true
	default:
		return 0, false
	}
}
