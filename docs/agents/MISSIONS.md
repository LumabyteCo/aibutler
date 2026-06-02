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
failure, if a `Replanner` is configured the supervisor consults it for
a recovery sequence (see *Replanning on step failure* below); otherwise
the mission is marked `failed` and the supervisor stops.

### Worker

Receives tasks on the mission's dispatch topic. Runs each task through
a `TaskExecutor` callback (the LLM-backed default wraps the agent loop
with the configured model adapter, tool dispatcher, and capability
set), reports the result on the events topic, and continues.

**Concurrency.** By default a worker processes one task at a time —
fully backwards-compatible with v0.1/v0.2 semantics. Set
`Worker.MaxConcurrent` (or `missionruntime.Options.WorkerMaxConcurrent`)
to N > 1 to enable per-worker fan-out: each worker runs up to N tasks
concurrently in their own goroutines, bounded by an internal
semaphore. When at the cap the worker's receive loop blocks before
consuming the next dispatch, so the bus's competing-consumer routing
keeps pushing work to peer workers (or briefly queues until a slot
frees up). On context cancellation `Worker.Run` waits for every
in-flight handler to complete before returning, so shutdown is clean
and goroutines do not leak.

Useful when worker tasks are I/O-bound — e.g. LLM API calls — and the
pool size is smaller than the in-flight work the caller wants. With 3
workers × `MaxConcurrent=3`, up to 9 LLM calls can be in flight
simultaneously while the goroutine count and runtime overhead stay
small.

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

The webchat ships with a **Missions** panel in the sidebar that
consumes the read-only mission state APIs. Click 🎯 Missions in the
sidebar nav to open it. The panel surfaces:

- **Header stats** — active / completed / failed counts and total cost
  across all missions.
- **Recent missions list** — goal, state badge (with pulsing animation
  for `waiting_user`), step count, cost, age. Toggle to include
  completed / failed / cancelled.
- **Per-mission detail subview** — click a mission to drill in. Shows
  every step with its state (colour-coded left border), the worker's
  output or error inline, and a **live event tail** that polls every
  2 seconds while the panel is open. The tail shows `mission.*`,
  `supervisor.*`, and `worker.*` events including the new
  `mission.confirmation_required` and `mission.replanned` events.

The panel is fully theme-aware (light/dark) and mobile-responsive.
Polling stops automatically when the panel closes — no SSE or
WebSocket overhead.

All state changes (pause / resume / cancel, mission creation, plan
authoring) still happen via the agent-facing `mission.*` tools or the
mission engine itself — the dashboard is read-only by design.

Underlying APIs (also callable directly):

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

## Dispatch modes — sequential vs parallel

`mission.Manager` exposes two ways to set the plan:

- **`SetPlan(missionID, steps)`** — the historical default. The
  supervisor walks steps one at a time in plan order. Each step's
  `DependsOn` field is ignored (steps run sequentially regardless).
- **`SetPlanParallel(missionID, steps)`** — the supervisor walks
  `Step.DependsOn` as a DAG. Each step whose dependencies are all
  completed is dispatched as soon as the previous result is observed.
  Multiple steps can be in flight at once. Steps with an empty
  `DependsOn` list are ready immediately (no implicit chain).

Failure policy is the same in both modes: the first step that fails
terminates the mission. In parallel mode, peer steps that were
already in flight before the failure get to publish their result;
no new work is dispatched after the failure is observed.

A plan with a dangling `DependsOn` reference (the named step
doesn't exist) is detected as a deadlock and fails the mission with
a clear error rather than hanging.

Wall-clock parallelism: each dispatched task lands on exactly ONE
worker in the pool (competing-consumer delivery) rather than being
broadcast to every worker. With N workers and N independent steps,
all N steps run concurrently. With more steps than workers, work
queues fairly as workers free up. Per-call shuffle in the bus
distributes load across the pool; busy workers fall through to peers
via the publish's SendTimeout.

## Replanning on step failure

`Supervisor.Replanner` is an optional `supervisor.Replanner` interface.
When set, a failing step does not immediately terminate the mission —
the Replanner is consulted for a replacement step sequence and the
supervisor continues from there.

The interface is one method:

```go
type Replanner interface {
    Replan(ctx context.Context, req ReplanRequest) ([]mission.Step, error)
}
```

`ReplanRequest` carries the goal, the completed steps (with their
outputs), the failed step (with its error), the remaining unstarted
steps, and how many replan attempts the mission has already used.

Returning `(steps, nil)` succeeds — the supervisor calls
`Manager.Replan` to persist the new steps, marks any unstarted original
steps that came after the failure as `cancelled` ("superseded by
replan"), and continues from the next non-terminal step. Returning
`(nil, ErrReplanRejected)` signals "this isn't recoverable" and the
supervisor takes the fail-fast path immediately. Any other non-nil
error is treated as a Replanner implementation failure — the mission
still fails, but the implementation error is surfaced in the
`mission.failed` reason for diagnostics.

`Supervisor.MaxReplans` caps how many attempts one mission may make
(default 3). After the cap, the next failure fails the mission
regardless of what the Replanner would have returned.

The runtime ships an LLM-backed implementation under
`missionruntime.NewLLMReplanner`. It calls a configured
`agent.ModelAdapter` directly (no tools, no nested agent loop) with a
strict JSON-output prompt, retries on malformed output up to
`LLMReplannerConfig.MaxRetries` (default 1), and translates an empty
`steps` array to `ErrReplanRejected`. The replan call has its own
timeout (`Timeout`, default 30 s).

To enable replanning at app startup, construct the LLM-backed replanner
once and pass it to the runtime:

```go
rp, err := missionruntime.NewLLMReplanner(missionruntime.LLMReplannerConfig{
    Model: modelAdapter, // existing Anthropic/Ollama adapter
})
// ...
rt := missionruntime.New(mgr, store, b, missionruntime.Options{
    Executor:   llmExec,
    Replanner:  rp,
    MaxReplans: 3,
})
```

Existing missions without a configured Replanner keep the previous
"fail on first failure" behaviour — no flag flips, no migrations.

Scope notes:

- Replanning is **sequential mode only** in this revision. Parallel
  dispatch (`SetPlanParallel`) still fails the whole mission on the
  first failure; in-flight peers complete naturally but no replan
  attempt is made.
- The Replanner sees prior completed step outputs but does NOT see
  the mission's running event log or per-step telemetry. The signal it
  gets is the same signal the supervisor has: goal + completed + failed
  + remaining. A richer audit-trail integration is a clearly-scoped
  follow-up.

## Mid-mission auto-pause on capability confirmation

When a worker's task triggers a capability that requires explicit user
confirmation (`Capability.RequiresConfirmation = true`), the mission
auto-pauses instead of failing or replanning.

The flow:

1. The tool dispatcher's capability check resolves to allowed AND
   `RequiresConfirmation`. It returns `capability.ConfirmationRequiredError`
   (struct, `errors.As`-detectable) instead of executing the tool. The
   error carries the capability resource ID and the engine reason.
2. The worker's `handle` notices the sentinel via `errors.As`, builds a
   `worker.Result` with `NeedsConfirmation=true` + `ConfirmationReason`
   (and `Success=false`), and publishes it on the mission events topic.
3. The supervisor's `runStep` branches on `NeedsConfirmation` BEFORE
   the success/failure split. It marks the step's `State=waiting_user`,
   stamps the reason as `Step.Error`, leaves `CompletedAt` unset
   (the step is paused, not finished), emits a
   `supervisor.step_paused` event with the full Result payload, and
   returns a `stepNeedsConfirmationError` sentinel.
4. `runSequential` catches that sentinel, emits a
   `mission.confirmation_required` event with `{step_id, reason}`,
   calls `Manager.Pause` to transition the mission to `waiting_user`,
   and exits with `ErrMissionPaused`.
5. The runtime's poll picks up the paused mission's eventual resume:
   `scan` now lists missions in `StateRunning` (not just `StatePlanned`)
   so that after `mission.interrupt action=resume` flips the mission
   back to running, the runtime spawns a fresh supervisor + worker
   pair. The supervisor's `runSequential` cursor treats step
   `state=waiting_user` as non-terminal and picks it up for a clean
   re-dispatch.

If the underlying capability still requires confirmation when the
mission resumes, the same pause path triggers again — by design.
Granting the confirmation is the user's call, handled separately via
the capability engine (grant the rule without `RequiresConfirmation`,
or via the prompter's interactive path).

Replanning and auto-pause are independent: a confirmation-required
result NEVER triggers the Replanner. Auto-pause is checked first in
the supervisor's outer loop, ahead of the replan branch.

## What's not in this revision

Several capabilities were considered for the initial release but
intentionally deferred:

- **Replanning in parallel mode.** Sequential mode replanning ships
  in this revision (above); the parallel-DAG path still fails the
  whole mission on the first step failure. Replanning under parallel
  dispatch is its own design problem (what to do with in-flight peers
  whose results are already partway done) and is a follow-up.
- **Manager tier (3-level hierarchy).** Workers report directly to the
  supervisor today. A manager layer between them, owning sub-domains,
  is part of the eventual hierarchy.
