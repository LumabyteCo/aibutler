package agent

import "testing"

func TestEffectiveMode(t *testing.T) {
	tests := []struct {
		input Mode
		want  Mode
	}{
		{ModeAuto, ModeMulti},   // auto resolves to multi (delegation enabled)
		{ModeSingle, ModeSingle},
		{ModeMulti, ModeMulti},
		{ModeCustom, ModeCustom},
		{ModeSwarm, ModeMulti}, // swarm resolves to multi execution with router overlay
	}
	for _, tt := range tests {
		got := effectiveMode(tt.input)
		if got != tt.want {
			t.Errorf("effectiveMode(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestValidMode(t *testing.T) {
	valid := []string{"auto", "single", "multi", "custom", "swarm"}
	for _, m := range valid {
		if !ValidMode(m) {
			t.Errorf("ValidMode(%q) = false, want true", m)
		}
	}

	invalid := []string{"", "parallel", "unknown"}
	for _, m := range invalid {
		if ValidMode(m) {
			t.Errorf("ValidMode(%q) = true, want false", m)
		}
	}
}

func TestParseModeOverride(t *testing.T) {
	tests := []struct {
		input    string
		wantMode Mode
		wantTask string
	}{
		// Valid overrides.
		{"[mode:multi] do something", ModeMulti, "do something"},
		{"[mode:single] analyze this", ModeSingle, "analyze this"},
		{"[mode:custom] route to expert", ModeCustom, "route to expert"},
		{"[mode:auto] figure it out", ModeAuto, "figure it out"},
		// Leading whitespace.
		{"  [mode:multi] hello", ModeMulti, "hello"},
		// No override — return original.
		{"just a normal task", "", "just a normal task"},
		{"mode:multi no brackets", "", "mode:multi no brackets"},
		// Swarm mode override.
		{"[mode:swarm] do swarm stuff", ModeSwarm, "do swarm stuff"},
		// No closing bracket.
		{"[mode:multi oops", "", "[mode:multi oops"},
		// Empty task after prefix.
		{"[mode:multi]", ModeMulti, ""},
		{"[mode:multi]  ", ModeMulti, ""},
	}
	for _, tt := range tests {
		gotMode, gotTask := ParseModeOverride(tt.input)
		if gotMode != tt.wantMode {
			t.Errorf("ParseModeOverride(%q) mode = %q, want %q", tt.input, gotMode, tt.wantMode)
		}
		if gotTask != tt.wantTask {
			t.Errorf("ParseModeOverride(%q) task = %q, want %q", tt.input, gotTask, tt.wantTask)
		}
	}
}

func TestParseModeOverrideBlocksEscalation(t *testing.T) {
	tests := []struct {
		input       string
		currentMode Mode
		wantMode    Mode
		wantTask    string
	}{
		// Blocked escalations.
		{"[mode:multi] escalate", ModeSingle, "", "[mode:multi] escalate"},
		{"[mode:swarm] escalate", ModeSingle, "", "[mode:swarm] escalate"},
		{"[mode:swarm] escalate", ModeMulti, "", "[mode:swarm] escalate"},
		// Allowed downgrades.
		{"[mode:single] downgrade", ModeMulti, ModeSingle, "downgrade"},
		{"[mode:single] downgrade", ModeSwarm, ModeSingle, "downgrade"},
		{"[mode:multi] downgrade", ModeSwarm, ModeMulti, "downgrade"},
		// Same mode is allowed.
		{"[mode:multi] same", ModeMulti, ModeMulti, "same"},
		{"[mode:single] same", ModeSingle, ModeSingle, "same"},
	}
	for _, tt := range tests {
		gotMode, gotTask := ParseModeOverride(tt.input, tt.currentMode)
		if gotMode != tt.wantMode {
			t.Errorf("ParseModeOverride(%q, %q) mode = %q, want %q", tt.input, tt.currentMode, gotMode, tt.wantMode)
		}
		if gotTask != tt.wantTask {
			t.Errorf("ParseModeOverride(%q, %q) task = %q, want %q", tt.input, tt.currentMode, gotTask, tt.wantTask)
		}
	}
}
