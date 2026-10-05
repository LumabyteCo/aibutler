package iot_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/LumabyteCo/aibutler/internal/capability"
	"github.com/LumabyteCo/aibutler/internal/iot"
	"github.com/LumabyteCo/aibutler/testutil"
)

// routineFixture builds a controller with a realistic device set (stub
// adapter) plus the full default capability overlay, mirroring how the
// CLI wires iot for agent runs.
func routineFixture(t *testing.T) (*iot.Controller, *iot.StubAdapter, *capability.CapabilitySet) {
	t.Helper()
	database := testutil.TestDB(t)
	_ = database // PIN vault uses the real keychain-less env fallback in tests below
	engine := capability.NewEngine(nil)
	adapter := iot.NewStubAdapter()
	ctrl := iot.NewController(adapter, engine, iot.NewPINVerifier(testutil.NewFakeVault()))

	devices := []iot.Device{
		{ID: "light.living", Name: "Living", DeviceType: "light", Adapter: "stub", Tier: iot.TierComfort, Enabled: true},
		{ID: "light.kitchen", Name: "Kitchen", DeviceType: "light", Adapter: "stub", Tier: iot.TierComfort, Enabled: true},
		{ID: "lock.front", Name: "Front Door", DeviceType: "lock", Adapter: "stub", Tier: iot.TierSafety, Enabled: true},
		{ID: "climate.main", Name: "Main", DeviceType: "climate", Adapter: "stub", Tier: iot.TierComfort, Enabled: true},
	}
	for _, d := range devices {
		adapter.AddDevice(d)
		ctrl.RegisterDevice(d)
	}

	caps := capability.NewCapabilitySet(append(
		capability.IoTDefaults(),
		capability.Capability{
			Resource:   "iot.safety.control",
			Devices:    []string{"*"},
			AuditLevel: capability.AuditFull,
		},
	))
	return ctrl, adapter, caps
}

// TestRoutineGoodnightHappyPath — the Phase 3 headline: "goodnight" runs
// lights → lock → climate, with the PIN flowing through to the safety
// step.
func TestRoutineGoodnightHappyPath(t *testing.T) {
	ctrl, adapter, caps := routineFixture(t)

	// Set the safety PIN first.
	if err := ctrl.SetPINForTest(context.Background(), "2468"); err != nil {
		t.Fatalf("set pin: %v", err)
	}

	results, err := ctrl.RunRoutine(context.Background(), caps, "goodnight", "2468")
	if err != nil {
		t.Fatalf("run routine: %v", err)
	}

	// 4 devices matched: 2 lights + 1 lock + 1 climate = 4 steps.
	if len(results) != 4 {
		t.Fatalf("steps = %d, want 4: %+v", len(results), results)
	}
	for _, r := range results {
		if r.Status != "ok" {
			t.Errorf("step %q (%s) status = %q (%s), want ok", r.Label, r.Device, r.Status, r.Details)
		}
	}

	// The stub adapter recorded the executions.
	if len(adapter.Executed()) != 4 {
		t.Errorf("executed commands = %d, want 4", len(adapter.Executed()))
	}
}

// TestRoutineSafetyGateInsideRoutine — THE security invariant: a routine
// must NOT bypass the tier-3 PIN gate. "goodnight" without a PIN: lights
// and climate complete, the lock reports pin_required, nothing silently
// unlocks.
func TestRoutineSafetyGateInsideRoutine(t *testing.T) {
	ctrl, adapter, caps := routineFixture(t)
	if err := ctrl.SetPINForTest(context.Background(), "2468"); err != nil {
		t.Fatalf("set pin: %v", err)
	}

	results, err := ctrl.RunRoutine(context.Background(), caps, "goodnight", "")
	if err != nil {
		t.Fatalf("run routine: %v", err)
	}

	var pinRequired, ok int
	for _, r := range results {
		switch r.Status {
		case "pin_required":
			pinRequired++
			if r.Device != "lock.front" {
				t.Errorf("unexpected pin_required on %s", r.Device)
			}
		case "ok":
			ok++
		}
	}
	if pinRequired != 1 {
		t.Errorf("pin_required steps = %d, want 1 (the lock)", pinRequired)
	}
	if ok != 3 {
		t.Errorf("ok steps = %d, want 3 (lights+climate complete honestly)", ok)
	}
	// The lock must NOT have executed.
	for _, cmd := range adapter.Executed() {
		if cmd.DeviceID == "lock.front" {
			t.Error("SECURITY: lock executed without PIN inside a routine")
		}
	}
}

// TestRoutineWrongPIN — the wrong PIN refuses the safety step only.
func TestRoutineWrongPIN(t *testing.T) {
	ctrl, _, caps := routineFixture(t)
	if err := ctrl.SetPINForTest(context.Background(), "2468"); err != nil {
		t.Fatalf("set pin: %v", err)
	}

	results, err := ctrl.RunRoutine(context.Background(), caps, "goodnight", "9999")
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	for _, r := range results {
		if r.Device == "lock.front" {
			if r.Status != "error" || r.Details != "invalid PIN" {
				t.Errorf("lock step = %q (%s), want invalid-PIN error", r.Status, r.Details)
			}
		}
	}
}

// TestRoutineSkipsMissingDevices — a routine written for a full house
// must run in a home that lacks some device classes.
func TestRoutineSkipsMissingDevices(t *testing.T) {
	database := testutil.TestDB(t)
	_ = database
	engine := capability.NewEngine(nil)
	adapter := iot.NewStubAdapter()
	ctrl := iot.NewController(adapter, engine, iot.NewPINVerifier(testutil.NewFakeVault()))
	// ONLY a light — no lock, no climate.
	adapter.AddDevice(iot.Device{ID: "light.only", Name: "Only", DeviceType: "light", Adapter: "stub", Tier: iot.TierComfort, Enabled: true})
	ctrl.RegisterDevice(iot.Device{ID: "light.only", Name: "Only", DeviceType: "light", Adapter: "stub", Tier: iot.TierComfort, Enabled: true})
	caps := capability.NewCapabilitySet(capability.IoTDefaults())

	results, err := ctrl.RunRoutine(context.Background(), caps, "goodnight", "2468")
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	var ok, skipped int
	for _, r := range results {
		switch r.Status {
		case "ok":
			ok++
		case "skipped":
			skipped++
			if r.Details != "no matching devices" {
				t.Errorf("skip reason = %q", r.Details)
			}
		}
	}
	if ok != 1 || skipped != 2 {
		t.Errorf("ok=%d skipped=%d, want 1 light ok + lock/climate skipped", ok, skipped)
	}
}

// TestRoutineUnknown — unknown routine names error clearly.
func TestRoutineUnknown(t *testing.T) {
	ctrl, _, _ := routineFixture(t)
	_, err := ctrl.RunRoutine(context.Background(), capability.NewCapabilitySet(nil), "party_mode", "")
	if err == nil {
		t.Fatal("unknown routine must error")
	}
}

// TestRoutineNamesStable — the advertised names are the shippable set.
func TestRoutineNamesStable(t *testing.T) {
	names := iot.RoutineNames()
	want := []string{"good_morning", "goodnight", "leaving_home", "movie_time"}
	if len(names) != len(want) {
		t.Fatalf("routines = %v, want %v", names, want)
	}
	for i, n := range want {
		if names[i] != n {
			t.Errorf("names[%d] = %q, want %q", i, names[i], n)
		}
	}
}

// TestRoutineResultJSON — the tool output shape the model reads.
func TestRoutineResultJSON(t *testing.T) {
	ctrl, _, caps := routineFixture(t)
	results, err := ctrl.RunRoutine(context.Background(), caps, "movie_time", "")
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	data, _ := json.Marshal(results)
	var back []map[string]interface{}
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatalf("shape not JSON round-trippable: %v", err)
	}
	for _, step := range back {
		if _, ok := step["status"]; !ok {
			t.Errorf("step missing status field: %v", step)
		}
	}
}