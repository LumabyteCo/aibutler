# Missions

A **mission** is a long-running goal that one or more agents collaborate to
achieve. Where the v0.1 agent loop handles one user turn at a time,
missions persist across turns, survive process restart, run autonomously
in the background, and produce a structured audit trail.

## When to use missions

Reach for the mission engine when you have a multi-step goal that:

- Has more than 2-3 steps and benefits from explicit planning.
- Should keep running while you do other things — e.g. "research X, then
  draft a summary, then save it to my notes."
- Needs a clear pause / resume / cancel surface.
- Should leave an inspectable trail of what happened, in what order, with
  what cost.

For one-shot questions ("what's the weather"), the regular single-agent
loop is still the right tool — missions add overhead that pays off only
when there's real orchestration work to do.

## Roles

### Supervisor

Owns one mission end-to-end. Reads the plan, dispatches each step to a
worker via the reliable bus, waits for the result event, persists step
state, and transitions the mission through its state machine. On step
failure the supervisor marks the mission `failed` and stops; replanning
is reserved for a future revision.

### Worker

Receives one task at a time on the mission's dispatch topic. Runs the
task through a `TaskExecutor` callback (the LLM-backed default wraps the
agent loop with the configured model adapter, tool dispatcher, and
capability set), reports the result on the events topic, and waits for
the next task.

## Lifecycle

```
created → planned → running ⇄ waiting_user → completed
                       │
                       ├──→ failed
                       │
                       └──→ cancelled
```

| State | Meaning | Terminal? |
|-------|---------|-----------|
| `created` | Goal received; no plan yet. | No |
| `planned` | Plan exists; no workers running. | No |
| `running` | Supervisor is dispatching steps. | No |
| `waiting_user` | Mission paused via `mission.interrupt`. | No |
| `completed` | Goal achieved. | Yes |
| `failed` | Step failed or supervisor errored. | Yes |
| `cancelled` | User stopped the mission via `mission.interrupt`. | Yes |

State transitions are enforced by the mission engine — invalid moves
return an error rather than silently corrupting state. Every transition
emits one `mission_events` row so reviewers can replay exactly what
happened.

## Persistence

Three SQLite tables added by migration 021:

- **`missions`** — one row per mission. Goal, plan JSON, state, budget,
  cost-so-far, supervisor agent ID, created / started / completed
  timestamps.
- **`mission_steps`** — plan steps. Step ID, task description,
  dependencies (JSON array of step IDs), assigned worker ID, state,
  output, error, timestamps.
- **`mission_events`** — append-only event log. Mission events
  (`mission.created` / `planned` / `started` / `paused` / `resumed` /
  `completed` / `failed` / `cancelled`) plus arbitrary worker events
  (`worker.started`, `worker.completed`, etc.).

Missions persist across process restart. Picking them back up after
restart is supported by the state-machine path (the supervisor's `Run`
accepts both `planned` and `running` start states and skips
already-completed steps), but full automatic resumption-on-boot is
follow-up work.

## Bus protocol

Two reliable topics per mission:

- **`mission.{id}.dispatch`** — supervisor → worker. Each message is a
  `Task {step_id, mission_id, task, input}`. The worker acks on receipt
  (delivery confirmation) and runs the task; the supervisor waits on
  the events topic for completion.
- **`mission.{id}.events`** — worker → supervisor. Each message is a
  `Result {step_id, mission_id, worker_id, output, error, success}`.
  The supervisor matches the `step_id` to the dispatched step, persists
  output/error, and either continues or fails the mission.

Both topics use the bus's at-least-once delivery (`PublishReliable` /
`SubscribeReliable`) — neither dispatch nor result can be silently
dropped on a slow consumer. Stable message IDs across retries let
non-idempotent handlers detect duplicates.

## Mode and runtime

Switch the agent into mission mode:

```bash
aibutler mode mission
# Agent mode switched to: mission
#   Mission runtime will start on next `aibutler run`.
#   Create missions via the mission.create tool; the runtime drives them.

aibutler run
# Mission mode active: runtime started (LLM-backed task executor).
#   Create missions via the mission.create tool.
#   Inspect via mission.list / mission.get / mission.events.
#   Pause / resume / cancel via mission.interrupt.
```

The runtime polls the mission store every 2s for missions in `planned`
state and spawns a supervisor + worker pair for each, up to the
`MaxConcurrent` cap (default 4). Workers use the LLM-backed task
executor by default; if no model adapter is configured (no API key,
offline mode), the runtime falls back to an echo executor so the
orchestration mechanics still work for testing.

## Tools

All under capability `tool.mission`:

| Tool | Purpose |
|------|---------|
| `mission.create` | Create a new mission with a stated goal and optional budget. Returns the mission ID. |
| `mission.list` | List recent missions. Filter by state, supervisor, or include terminal states. |
| `mission.get` | Get a single mission's full state — record, steps, recent events. |
| `mission.events` | Replay a mission's event log, oldest first. |
| `mission.interrupt` | Pause, resume, or cancel an active mission. The supervisor rechecks state between steps and exits cleanly when externally interrupted. |

## Dashboard

The webchat dashboard exposes read-only mission state under
`/api/dashboard/missions`:

- `GET /api/dashboard/missions` — list with filters
- `GET /api/dashboard/missions/stats` — quick counts and cost totals
- `GET /api/dashboard/missions/{id}` — full detail (mission + steps + events)
- `GET /api/dashboard/missions/{id}/events` — event stream for live tailing

## Costs and budgets

Each mission carries a `budget_usd` field; each step run by the
LLM-backed executor inherits a per-step budget cap (default $0.50)
that the existing agent core enforces. Mission-wide budget enforcement
(comparing accumulated cost across all steps to `budget_usd`) is
follow-up work; today the per-step cap prevents any single step from
blowing the budget by an order of magnitude.

The `cost.forecast` tool returns a pre-action token + USD estimate for
a planned model call — useful in the agent's plan-the-plan turn before
calling `mission.create`.

## What's not in this revision

Several capabilities were considered for the initial release but
intentionally deferred:

- **Replanning on step failure.** Today a failed step fails the
  whole mission; the supervisor doesn't try the same step with adjusted
  inputs or substitute a different worker. The architecture supports
  it (events are recorded, steps have `state` columns) — the policy
  layer is the next addition.
- **Mid-mission user-confirmation prompts.** When a worker hits a
  capability that requires confirmation, the existing capability engine
  surfaces a prompt — but the mission engine doesn't yet auto-pause
  the mission while waiting. `mission.interrupt action=pause` is the
  manual equivalent today.
- **Manager tier (3-level hierarchy).** Workers report directly to the
  supervisor today. A manager layer between them, owning sub-domains,
  is part of the eventual hierarchy.
- **Parallel step dispatch.** The supervisor walks steps sequentially
  even when their `depends_on` graph would allow parallelism. Both bus
  and store already support concurrent dispatch — this is policy work.
