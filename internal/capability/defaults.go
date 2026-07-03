package capability

import "time"

// TerminalDefaults returns the default capability set for terminal mode.
func TerminalDefaults() []Capability {
	return []Capability{
		{Resource: "tool.file.read", Paths: []string{"./"}, AuditLevel: AuditSummary},
		{Resource: "tool.file.edit", Paths: []string{"./"}, AuditLevel: AuditSummary},
		{Resource: "tool.lsp.*", AuditLevel: AuditNone},
		{Resource: "tool.web.search", AuditLevel: AuditSummary},
		{Resource: "memory.read", AuditLevel: AuditNone},
		{Resource: "memory.write", AuditLevel: AuditNone},
		// Deletion is irreversible, so it always leaves a full audit trail.
		// The terminal prompter additionally confirms it like other write tools.
		{Resource: "memory.forget", AuditLevel: AuditFull},
		{Resource: "tool.shell.exec", AuditLevel: AuditFull},
		{Resource: "agent.delegate", AuditLevel: AuditSummary},
	}
}

// MessagingDefaults returns the default capability set for messaging mode.
func MessagingDefaults() []Capability {
	return []Capability{
		{Resource: "channel.read", Channels: []string{"*"}, AuditLevel: AuditSummary},
		{Resource: "channel.write", Channels: []string{"*"}, AuditLevel: AuditSummary},
		{Resource: "channel.send", Channels: []string{"*"}, AuditLevel: AuditFull},
		{Resource: "channel.relay", Channels: []string{"*"}, AuditLevel: AuditFull},
		{Resource: "mcp.call", AuditLevel: AuditFull},
		{Resource: "tool.web.search", AuditLevel: AuditSummary},
		{Resource: "tool.web.fetch", RateLimit: &RateLimit{MaxCalls: 10, Window: time.Minute}, AuditLevel: AuditFull},
		{Resource: "memory.read", AuditLevel: AuditNone},
		{Resource: "memory.write", AuditLevel: AuditSummary},
		// Deletion over a messaging channel requires an explicit yes from the
		// user in-channel before it executes, and is always fully audited —
		// it is irreversible and cascades to derived data.
		{Resource: "memory.forget", RequiresConfirmation: true, AuditLevel: AuditFull},
		{Resource: "schedule.manage", AuditLevel: AuditFull},
		{Resource: "tool.shell.exec", AuditLevel: AuditFull},
		{Resource: "data.tasks.read", AuditLevel: AuditNone},
		{Resource: "data.tasks.write", AuditLevel: AuditSummary},
		{Resource: "data.finance.read", AuditLevel: AuditSummary},
		{Resource: "data.finance.write", AuditLevel: AuditSummary},
		{Resource: "data.contacts.read", AuditLevel: AuditNone},
		{Resource: "data.contacts.write", AuditLevel: AuditSummary},
		{Resource: "data.journal.read", AuditLevel: AuditNone},
		{Resource: "data.journal.write", AuditLevel: AuditSummary},
		{Resource: "data.health.read", AuditLevel: AuditSummary},
		{Resource: "data.health.write", AuditLevel: AuditSummary},
		{Resource: "tool.file.read", AuditLevel: AuditSummary},
		{Resource: "tool.file.write", AuditLevel: AuditFull},
		{Resource: "tool.file.edit", AuditLevel: AuditFull},
		// Git read is granted by default. Write (commit/push/branch switches)
		// is intentionally NOT in the default set — every destructive git op
		// must be explicitly granted via config.
		{Resource: "tool.git.read", AuditLevel: AuditSummary},
		{Resource: "data.reminders.read", AuditLevel: AuditNone},
		{Resource: "data.reminders.write", AuditLevel: AuditSummary},
		{Resource: "data.habits.read", AuditLevel: AuditNone},
		{Resource: "data.habits.write", AuditLevel: AuditSummary},
		{Resource: "data.places.read", AuditLevel: AuditNone},
		{Resource: "data.places.write", AuditLevel: AuditSummary},
		{Resource: "voice.transcribe", AuditLevel: AuditSummary},
		{Resource: "voice.speak", AuditLevel: AuditSummary},
		{Resource: "instruction.read", AuditLevel: AuditNone},
		{Resource: "instruction.write", AuditLevel: AuditSummary},
		{Resource: "agent.delegate", AuditLevel: AuditSummary},
		{Resource: "plugin.*.call", AuditLevel: AuditFull},
		{Resource: "mcp.server.call", AuditLevel: AuditFull},
		{Resource: "a2a.delegate", AuditLevel: AuditFull},
	}
}

// IoTDefaults returns the IoT overlay capabilities.
//
// SECURITY MODEL — three tiers:
//
//   - Tier 1 (sensor)  — sensor.read, device.list, device.discover
//     Read-only; always safe. Granted by default with rate limiting.
//   - Tier 2 (comfort) — device.control (lights, thermostat, fans, speakers)
//     Reversible and low-risk. Granted by default but rate-limited and fully
//     audited. Users can revoke via config if they want stricter control.
//   - Tier 3 (safety)  — safety.control (locks, garage doors, gas/water valves)
//     Destructive or access-granting. NOT granted by default. Must be
//     explicitly configured AND require PIN verification at call time.
//
// This matches the UNIX model: read and low-risk actions are default-on,
// destructive actions require explicit elevation.
func IoTDefaults() []Capability {
	return []Capability{
		// Tier 1 — read-only, always safe
		{Resource: "iot.sensor.read", Devices: []string{"*"}, RateLimit: &RateLimit{MaxCalls: 120, Window: time.Hour}, AuditLevel: AuditSummary},
		{Resource: "iot.device.list", AuditLevel: AuditSummary},
		{Resource: "iot.device.discover", AuditLevel: AuditSummary},

		// Tier 2 — comfort device control (lights, thermostat, fans)
		// Reversible, rate-limited, fully audited. Users can override via config.
		{Resource: "iot.device.control", Devices: []string{"*"}, RateLimit: &RateLimit{MaxCalls: 60, Window: time.Hour}, AuditLevel: AuditFull},

		// Tier 3 — safety.control is INTENTIONALLY ABSENT.
		// Must be explicitly granted AND the tool itself requires PIN verification.
	}
}
