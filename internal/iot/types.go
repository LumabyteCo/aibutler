package iot

import (
	"context"
	"errors"
	"time"
)

// Tier categorizes IoT device security levels.
type Tier int

const (
	TierSensor  Tier = 1 // Auto-approved, low risk
	TierComfort Tier = 2 // Logged, rate-limited, safety-bounded
	TierSafety  Tier = 3 // Multi-factor required (confirmation + PIN)
)

// ErrDeviceNotFound is returned when a device ID is unknown.
var ErrDeviceNotFound = errors.New("iot: device not found")

// ErrSafetyBound is returned when a command violates safety bounds.
var ErrSafetyBound = errors.New("iot: safety bound violated")

// ErrConfirmationRequired is returned when a tier 3 command lacks confirmation.
var ErrConfirmationRequired = errors.New("iot: confirmation required for safety-critical device")

// ErrPINRequired is returned when a tier 3 command lacks a valid PIN.
var ErrPINRequired = errors.New("iot: PIN verification required for safety-critical device")

// ErrPINInvalid is returned when PIN verification fails.
var ErrPINInvalid = errors.New("iot: invalid PIN")

// Device represents a registered IoT device.
type Device struct {
	ID         string
	Name       string
	DeviceType string // "thermostat", "lock", "light", "sensor", etc.
	Adapter    string // "stub", "homeassistant"
	Tier       Tier
	Config     map[string]interface{}
	Enabled    bool
}

// Command represents an action to perform on a device.
type Command struct {
	DeviceID  string
	Action    string // "set", "toggle", "read", etc.
	Params    map[string]interface{}
	Confirmed bool   // Whether user has confirmed (for tier 3)
	PIN       string // PIN code (for tier 3)
}

// SensorReading holds a single sensor data point.
type SensorReading struct {
	DeviceID  string
	Metric    string
	Value     float64
	Unit      string
	Timestamp time.Time
}

// DeviceAdapter is the interface for communicating with IoT platforms.
type DeviceAdapter interface {
	ReadSensor(ctx context.Context, deviceID string) ([]SensorReading, error)
	Execute(ctx context.Context, cmd Command) error
	Discover(ctx context.Context) ([]Device, error)
}
