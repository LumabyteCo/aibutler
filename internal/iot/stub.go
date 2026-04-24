package iot

import (
	"context"
	"fmt"
	"time"
)

// StubAdapter is a test/demo adapter that returns canned data.
type StubAdapter struct {
	devices  map[string]Device
	readings map[string][]SensorReading
	executed []Command
}

// NewStubAdapter creates a stub adapter with preconfigured devices.
func NewStubAdapter() *StubAdapter {
	return &StubAdapter{
		devices:  make(map[string]Device),
		readings: make(map[string][]SensorReading),
	}
}

// AddDevice registers a device in the stub.
func (s *StubAdapter) AddDevice(d Device) {
	s.devices[d.ID] = d
}

// AddReading adds a canned sensor reading.
func (s *StubAdapter) AddReading(deviceID string, readings ...SensorReading) {
	s.readings[deviceID] = append(s.readings[deviceID], readings...)
}

// Executed returns all executed commands (for test assertions).
func (s *StubAdapter) Executed() []Command {
	return s.executed
}

func (s *StubAdapter) ReadSensor(_ context.Context, deviceID string) ([]SensorReading, error) {
	readings, ok := s.readings[deviceID]
	if !ok {
		return nil, fmt.Errorf("stub: no readings for %q", deviceID)
	}
	// Set timestamps to now if zero
	for i := range readings {
		if readings[i].Timestamp.IsZero() {
			readings[i].Timestamp = time.Now().UTC()
		}
	}
	return readings, nil
}

func (s *StubAdapter) Execute(_ context.Context, cmd Command) error {
	if _, ok := s.devices[cmd.DeviceID]; !ok {
		return fmt.Errorf("stub: unknown device %q", cmd.DeviceID)
	}
	s.executed = append(s.executed, cmd)
	return nil
}

func (s *StubAdapter) Discover(_ context.Context) ([]Device, error) {
	devices := make([]Device, 0, len(s.devices))
	for _, d := range s.devices {
		devices = append(devices, d)
	}
	return devices, nil
}
