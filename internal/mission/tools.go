package mission

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// toolRegistry is the narrow interface for registering tools (avoids import cycles).
type toolRegistry interface {
	Register(name, description, schema, capability string, exec func(ctx context.Context, input string) (string, error))
}

// missionDetail bundles a mission with its steps and recent events for
// the mission.get response.
type missionDetail struct {
	Mission Mission `json:"mission"`
	Steps   []Step  `json:"steps,omitempty"`
	Events  []Event `json:"events,omitempty"`
}

// RegisterTools registers mission.create / mission.list / mission.get /
// mission.events. Capability: tool.mission (single shared resource for
// the whole namespace; future commits may split read vs. mutate).
func RegisterTools(registry toolRegistry, mgr *Manager, store Store) {
	// mission.create — start a new mission in the `created` state.
	registry.Register(
		"mission.create",
		"Create a new mission with a stated goal. Returns the mission ID, which is used by all "+
			"other mission.* tools. budget_usd caps the projected cost; supervisor_agent_id "+
			"identifies the agent that owns this mission's plan.",
		`{"type":"object","properties":{`+
			`"goal":{"type":"string","description":"What the mission should achieve"},`+
			`"budget_usd":{"type":"number","minimum":0,"description":"Soft cap on total spend"},`+
			`"supervisor_agent_id":{"type":"string","description":"Optional — agent that owns the plan"}`+
			`},"required":["goal"]}`,
		"tool.mission",
		func(ctx context.Context, input string) (string, error) {
			var args struct {
				Goal              string  `json:"goal"`
				BudgetUSD         float64 `json:"budget_usd"`
				SupervisorAgentID string  `json:"supervisor_agent_id"`
			}
			if err := json.Unmarshal([]byte(input), &args); err != nil {
				return "", fmt.Errorf("mission.create: invalid input: %w", err)
			}
			if strings.TrimSpace(args.Goal) == "" {
				return "", fmt.Errorf("mission.create: goal is required")
			}
			m, err := mgr.Create(ctx, args.Goal, args.SupervisorAgentID, args.BudgetUSD)
			if err != nil {
				return "", err
			}
			out, _ := json.Marshal(m)
			return string(out), nil
		},
	)

	// mission.list — list missions, newest first.
	registry.Register(
		"mission.list",
		"List recent missions, newest first. Filter by state or supervisor. By default excludes "+
			"terminal states (completed/failed/cancelled) — set include_done=true to include.",
		`{"type":"object","properties":{`+
			`"state":{"type":"string","enum":["created","planned","running","waiting_user","completed","failed","cancelled"]},`+
			`"supervisor":{"type":"string"},`+
			`"include_done":{"type":"boolean"},`+
			`"limit":{"type":"integer","minimum":1,"maximum":500}`+
			`},"additionalProperties":false}`,
		"tool.mission",
		func(ctx context.Context, input string) (string, error) {
			var args struct {
				State       string `json:"state"`
				Supervisor  string `json:"supervisor"`
				IncludeDone bool   `json:"include_done"`
				Limit       int    `json:"limit"`
			}
			input = strings.TrimSpace(input)
			if input != "" && input != "{}" {
				if err := json.Unmarshal([]byte(input), &args); err != nil {
					return "", fmt.Errorf("mission.list: invalid input: %w", err)
				}
			}
			out, err := store.ListMissions(ctx, ListFilter{
				State:       State(args.State),
				Supervisor:  args.Supervisor,
				Limit:       args.Limit,
				IncludeDone: args.IncludeDone,
			})
			if err != nil {
				return "", err
			}
			b, _ := json.Marshal(out)
			return string(b), nil
		},
	)

	// mission.get — full mission details with steps and recent events.
	registry.Register(
		"mission.get",
		"Get a single mission's full state — the mission record, all plan steps, and the most "+
			"recent events from the mission log. Default event limit 100, max 1000.",
		`{"type":"object","properties":{`+
			`"id":{"type":"string"},`+
			`"event_limit":{"type":"integer","minimum":1,"maximum":1000}`+
			`},"required":["id"]}`,
		"tool.mission",
		func(ctx context.Context, input string) (string, error) {
			var args struct {
				ID         string `json:"id"`
				EventLimit int    `json:"event_limit"`
			}
			if err := json.Unmarshal([]byte(input), &args); err != nil {
				return "", fmt.Errorf("mission.get: invalid input: %w", err)
			}
			if args.ID == "" {
				return "", fmt.Errorf("mission.get: id is required")
			}
			mi, err := store.GetMission(ctx, args.ID)
			if err != nil {
				return "", err
			}
			steps, err := store.GetSteps(ctx, args.ID)
			if err != nil {
				return "", err
			}
			events, err := store.GetEvents(ctx, args.ID, args.EventLimit)
			if err != nil {
				return "", err
			}
			out, _ := json.Marshal(missionDetail{Mission: mi, Steps: steps, Events: events})
			return string(out), nil
		},
	)

	// mission.interrupt — pause / resume / cancel a running mission. The
	// supervisor agent rechecks mission state between steps and exits
	// cleanly when it sees the mission has been externally moved to a
	// terminal or waiting state, so this is the user-facing way to stop
	// or pause a mission mid-flight.
	registry.Register(
		"mission.interrupt",
		"Pause, resume, or cancel an active mission. action='pause' moves it to waiting_user "+
			"(supervisor exits the run loop between steps; resume returns it to running). "+
			"action='cancel' marks the mission terminal — the supervisor exits with an error. "+
			"action='resume' transitions a waiting_user mission back to running. The mission "+
			"engine enforces state-machine transitions; invalid combinations return an error.",
		`{"type":"object","properties":{`+
			`"id":{"type":"string","description":"Mission ID"},`+
			`"action":{"type":"string","enum":["pause","resume","cancel"]},`+
			`"reason":{"type":"string","description":"Recorded in the mission event log"}`+
			`},"required":["id","action"]}`,
		"tool.mission",
		func(ctx context.Context, input string) (string, error) {
			var args struct {
				ID     string `json:"id"`
				Action string `json:"action"`
				Reason string `json:"reason"`
			}
			if err := json.Unmarshal([]byte(input), &args); err != nil {
				return "", fmt.Errorf("mission.interrupt: invalid input: %w", err)
			}
			if args.ID == "" {
				return "", fmt.Errorf("mission.interrupt: id is required")
			}
			var err error
			switch args.Action {
			case "pause":
				err = mgr.Pause(ctx, args.ID, args.Reason)
			case "resume":
				err = mgr.Resume(ctx, args.ID)
			case "cancel":
				err = mgr.Cancel(ctx, args.ID, args.Reason)
			default:
				return "", fmt.Errorf("mission.interrupt: unknown action %q (want pause | resume | cancel)", args.Action)
			}
			if err != nil {
				return "", err
			}
			mi, err := store.GetMission(ctx, args.ID)
			if err != nil {
				return "", err
			}
			out, _ := json.Marshal(map[string]interface{}{
				"id":     mi.ID,
				"state":  mi.State,
				"action": args.Action,
			})
			return string(out), nil
		},
	)

	// mission.events — just the event log for a mission.
	registry.Register(
		"mission.events",
		"Replay the event log for a mission, oldest first. Useful for understanding what "+
			"happened during a mission and why it transitioned between states.",
		`{"type":"object","properties":{`+
			`"id":{"type":"string"},`+
			`"limit":{"type":"integer","minimum":1,"maximum":1000}`+
			`},"required":["id"]}`,
		"tool.mission",
		func(ctx context.Context, input string) (string, error) {
			var args struct {
				ID    string `json:"id"`
				Limit int    `json:"limit"`
			}
			if err := json.Unmarshal([]byte(input), &args); err != nil {
				return "", fmt.Errorf("mission.events: invalid input: %w", err)
			}
			if args.ID == "" {
				return "", fmt.Errorf("mission.events: id is required")
			}
			events, err := store.GetEvents(ctx, args.ID, args.Limit)
			if err != nil {
				return "", err
			}
			out, _ := json.Marshal(events)
			return string(out), nil
		},
	)
}
