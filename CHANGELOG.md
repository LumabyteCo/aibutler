# Changelog

All notable changes to AI Butler will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [0.1.0] — 2026-04-10

### First public beta

AI Butler is a self-hosted personal AI agent with exceptional memory, any AI
model, a built-in web UI, and a 12-channel messaging interface. This is the
first public beta — the core is production-ready, several advanced features
are in beta and labeled clearly.

**Core stats:**

- **1,586 tests** passing (race-free, `go test -race -count=1`)
- **114 Go packages**
- **67 SQLite tables**, 19 migrations
- **59 internal security audit passes**, 74 findings fixed
- **0 CVEs** (`govulncheck` verified on every CI run)
- **~10 direct dependencies**, zero CGO
- **Apache 2.0** licensed with explicit patent grant

### Production-ready features

Everything in this list is tested end-to-end and ready for daily use.

**Core runtime**
- Agent loop with streaming responses, tool-call orchestration, and
  capability-gated execution
- Memory system combining FTS5 full-text search, an entity knowledge graph,
  vector embeddings, and hybrid ranking via Reciprocal Rank Fusion
- Per-project `BUTLER.md` instruction files (like `.cursorrules`)
- Three-tier config system (Settings / Configurations / Options) with the
  Butler Three Enriches pattern
- Single Go binary, zero CGO, cross-compiles to Linux/macOS/Windows/FreeBSD
  on amd64/arm64/arm/riscv64

**AI providers**
- **Anthropic Claude** — direct API integration, streaming, tool use
- **Ollama** — auto-detects running instance, uses it for both chat and
  memory embeddings; fully local option with zero cloud calls

**User interfaces**
- **Web chat** — 6-panel sidebar dashboard (Chat · Home · Memories ·
  Connected Apps · Spending · Settings). Welcome screen with 6 starter
  prompts. Live budget pill. Connection status. Mobile-responsive drawer.
  Auto/Light/Dark theme. File upload + voice upload. English + Arabic RTL.
- **Terminal REPL** — interactive streaming, slash commands, session resume
- **Dashboard API** — 16+ JSON endpoints (`/api/dashboard/*`) for stats,
  memory, agents, costs, swarm topology, capabilities, IoT, plugins,
  transactions, AI providers, audit log, registry, agent-card

**Developer tools**
- `file.read`, `file.write`, `file.edit`, `file.list`, `file.search` —
  workspace-bounded with path enforcement
- `shell.exec` — allowlisted commands, sandboxed via `unshare` (Linux)
  or `sandbox-exec` (macOS)
- `git.status`, `git.diff`, `git.commit`, `git.log`, `git.branch`,
  `git.pr_create` — wrapping the real git CLI

**Operations**
- **Scheduler** — natural language → cron, delivered to any channel,
  persistent across restarts
- **Cost tracking** — per-model, per-session, live dashboard, alerts
- **MCP client** — auto-connects to external MCP servers on boot
- **Vault** — credential storage in OS keyring (Keychain, GNOME Keyring,
  Windows Credential Manager) with age-encryption fallback
- **Capability engine** — per-tool grants + tamper-evident audit trail
- **Integrity checker** — `aibutler integrity` runs SQLite `PRAGMA
  integrity_check` + migration validation + orphan row detection

**Deployment**
- Docker + Docker Compose (standalone, `+ ollama`, `+ full stack`)
- Kubernetes via Helm chart (`deploy/helm/`)
- systemd service (`deploy/systemd/`)
- Raspberry Pi (ARM64) — first-class target

### Beta features — code complete, needs real-world validation

All of these compile, pass unit tests, and work in isolation. We're
asking the community to help validate them end-to-end with real
credentials, real third-party services, and real usage.

**Messaging channels (10 beta)**
- Telegram (Bot API with long polling + webhook mode)
- Slack (Bolt SDK with Socket Mode)
- Discord (Gateway)
- WhatsApp (Meta Cloud API)
- Microsoft Teams (Bot Framework)
- Google Chat (Cards v2)
- LINE (Messaging API with Flex Messages)
- IRC
- Custom webhook (HTTP)
- Nostr (NIP-04 direct messages)

**AI providers (5 beta)**
- OpenAI GPT + Azure OpenAI
- Google Gemini
- xAI Grok
- LM Studio / vLLM / Groq / DeepSeek (OpenAI-compatible)

**Advanced**
- Multi-agent **swarm orchestration** — engine works, first cookbook
  example coming in v0.2
- Full **Google A2A v2 protocol** — handler is spec-compliant, formal
  conformance suite pending
- **MCP server mode** — expose Butler's tools to other MCP clients
  (Claude Desktop, etc.)
- **WASM plugin sandbox** via [Extism](https://extism.org) — runtime
  ready, sample plugins coming in v0.2
- **OIDC SSO** (Auth0, Okta, Keycloak, Authelia, ZITADEL)
- **FIDO2 / WebAuthn** hardware security keys
- **TOTP 2FA** (RFC 6238, any authenticator app)
- **RBAC** with admin/user/viewer/agent roles
- **LAN mode** with mDNS discovery + PIN pairing
- **Subprocess bridges** for wrapping CLI tools as first-class Butler tools
- **Smart home via Home Assistant** — tool surface + PIN safety gating
  ready, HA adapter in final wiring
- **Whisper STT** (local binary or cloud API)
- **Piper TTS** (local, CPU-only)

### Roadmap for v0.2

- Hosted playground at `demo.aibutler.dev` (rate-limited, Haiku-only,
  read-only sandbox)
- Smart home: working Home Assistant adapter out of the box
- Plugin marketplace with sample plugins
- Multi-agent swarm cookbook with real-world delegation examples
- Voice TUI mode (terminal mic capture + playback)
- Image generation adapters (Flux, Stable Diffusion, DALL-E)
- ElevenLabs TTS adapter
- Internet access mode with autocert TLS + password + TOTP
- PWA (installable web app)
- Homebrew formula
- SLSA Level 3 provenance + cosign binary signing
- 14-language i18n
- External third-party security audit

### Installation

```bash
# Build from source (recommended)
git clone https://github.com/LumabyteCo/aibutler.git
cd aibutler && CGO_ENABLED=0 go build -o aibutler .

# Or download the release binary for your platform
curl -sSL https://github.com/LumabyteCo/aibutler/releases/download/v0.1.0/aibutler_0.1.0_Linux_x86_64.tar.gz | tar xz
sudo mv aibutler /usr/local/bin/

# Or Docker
docker pull ghcr.io/lumabyteco/aibutler:v0.1.0
```

### Quick start

```bash
./aibutler vault set anthropic_api_key sk-ant-...
./aibutler run
# Open http://localhost:3377
```

Full walkthrough with screenshots: [docs.aibutler.dev/getting-started/quick-start/](https://docs.aibutler.dev/getting-started/quick-start/)

### Breaking changes

None — this is the initial release.

### Acknowledgements

AI Butler is built on the shoulders of:

- [Go 1.26+](https://go.dev)
- [ncruces/go-sqlite3](https://github.com/ncruces/go-sqlite3) — pure Go
  SQLite with FTS5 support (no CGO)
- [Extism](https://extism.org) — WASM plugin runtime
- [nhooyr.io/websocket](https://github.com/coder/websocket) — production
  WebSocket library
- [Starlight](https://starlight.astro.build) + [Astro](https://astro.build)
  for the documentation site
- Every MCP server in the ecosystem
- Every A2A-compatible agent project

### Contributors

This release was built by [LumaByte Co](https://lumabyte.co). Community
contributions welcome — see [CONTRIBUTING.md](CONTRIBUTING.md) for the full
guide on how to push beta features to `ready`, write sample plugins, test
channels, and help shape the roadmap.

---

## [0.2.1] — 2026-05-06

Patch release. No product behaviour changes — CI / test infrastructure
fixes plus one dependency bump caught while shipping v0.2.0.

### Fixed

- **Test infrastructure: pinned in-memory SQLite to a single connection**
  (`db.SetMaxOpenConns(1)`) in mission and supervisor tests. The
  `database/sql` connection pool was handing fresh empty `:memory:`
  databases to runtime poll goroutines under `-race` + heavy parallel
  CI load, causing "no such table" flakes that masquerade as test
  timeouts. Locally the Go scheduler hid the bug; on Linux CI it
  surfaced reliably. (74c3610, 1ca121b)

### Dependencies

- Bumped `github.com/pelletier/go-toml/v2` from 2.3.0 to 2.3.1
  (Dependabot, PR #2).

### Other

- Enabled Dependency Graph + vulnerability alerts on the repo. Future
  Dependabot PRs no longer fail the `Dependency Review` Action.

---

## [0.2.0] — 2026-05-05

### Native OS scripting (Tier 2)

The biggest theme of this release. Agents can now drive macOS apps via
AppleScript, Linux desktops via D-Bus, and continue to drive Windows
via PowerShell — all through one cross-OS dispatcher.

- **AppleScript executor** (`shell.applescript`) — `osascript -e` wrapper
  with allowlist + timeout + output cap. Allowlist supports both
  bare verb (`tell`) and target-app patterns (`tell:Mail`,
  `tell:Music*`, `tell:*`).
- **macOS Shortcuts runner** (`shell.shortcuts`) — invokes named
  Apple Shortcuts with optional stdin input.
- **D-Bus client** (`shell.dbus`) — Linux method-call client with
  4-part `service:path:interface:method` allowlist patterns and
  wildcards. Pure Go via `github.com/godbus/dbus/v5`, no CGO.
- **Cross-OS dispatcher** (`shell.script`) — agent provides per-OS
  payloads (`{"darwin": {...}, "linux": {...}, "windows": {...}}`);
  the dispatcher routes by `runtime.GOOS`.
- **Out-of-box default allowlists** — opt-in via
  `Configurations.Security.Shell.UseDefaultAllowlist` for system
  notifications, MPRIS media controls, common AppleScript verbs.

### Mission engine

A new orchestration runtime for long-running goals. Persistent,
inspectable, controllable.

- **Mission state machine** — `created → planned → running ⇄ waiting_user
  → completed/failed/cancelled` with strict transition enforcement.
- **Persistence** — three new SQLite tables (`missions`,
  `mission_steps`, `mission_events`) added by migration 021. Missions
  survive process restart.
- **Manager API + tools** — `mission.create`, `mission.list`,
  `mission.get`, `mission.events`, `mission.interrupt`
  (pause/resume/cancel).
- **Reliable bus delivery** — new `PublishReliable` /
  `SubscribeReliable` alongside the existing best-effort pair.
  Bounded retry, ack-on-receipt, sentinel error types
  (`ErrNoSubscribers` / `ErrAckTimeout` / `ErrAllNacked`).
- **Supervisor + Worker agents** — Supervisor drives a planned mission
  to completion by dispatching plan steps to Workers via the reliable
  bus and aggregating results. External-state recheck between steps
  picks up pause/cancel from `mission.interrupt`.
- **`aibutler mode mission`** — runtime polls for planned missions
  and spawns supervisor + worker pairs (concurrent missions capped
  at 4 by default).
- **LLM-backed task executor** — wraps the existing agent loop. Each
  step runs through the configured model adapter with the tool
  dispatcher and a per-step budget cap (default $0.50).
- **Mission dashboard backend** — read-only HTTP endpoints under
  `/api/dashboard/missions`: list, detail (with steps + events),
  events stream, stats summary.

See [docs/agents/MISSIONS.md](docs/agents/MISSIONS.md) for the full
lifecycle, bus protocol, and tool reference.

### Vision and multimodal

- **Vision-capable model adapters** — new `agent.Image` type plumbed
  through the OpenAI-compatible adapter (Ollama vision, GPT-4o,
  LM Studio, vLLM) AND the Anthropic Claude adapter. Supports both
  base64 and URL sources. Validated end-to-end against `qwen3-vl:8b`
  on Ollama (synthetic 400×400 PNG → correct colour + shape
  identification in 7.7s).

### Computer-use primitives

Building blocks for the on-your-computer assistant story.

- **Clipboard tool** — `clipboard.read` / `clipboard.write`.
  Cross-platform via `pbcopy`/`pbpaste` (macOS), `wl-clipboard` or
  `xclip` (Linux), `clip.exe` + `Get-Clipboard` (Windows). Privacy:
  read records byte-count only, never content.
- **`wait.until` primitive** — block until one of `file_exists`,
  `process_running`, `port_open`, `http_ready`, or `duration` is
  satisfied. Per-attempt timeout, bounded poll interval, ctx
  cancellation honoured.
- **`cost.forecast` tool** — pre-action USD/token estimate for a
  planned model call. Pluggable pricing table with starter Anthropic
  prices and free-local prefix detection (ollama, lmstudio, vllm,
  llamacpp).
- **macOS permission wizard** — `permissions.check` probes Automation
  (System Events, Finder) and Screen Recording, returns deep-links to
  the relevant System Settings panel for any denied entries.
- **Just-in-time credential broker** — `vault.request` issues stored
  credentials to agents on demand with audit trail. Default-deny
  posture; explicit auto-approval list per credential
  (`Configurations.Vault.Request.AutoApprovedKeys`).
- **Action recording** — fine-grained per-call audit log (new
  `actions` SQLite table from migration 020) for AppleScript, D-Bus,
  Shortcuts, PowerShell, clipboard. Credential patterns
  auto-redacted in both payload and result fields before storage.

### Other

- Worker dispatch ack now happens on receipt rather than after
  executor completion — supervisor's reliable-publish budget no
  longer needs to cover the worst-case worker runtime.
- AppleScript and D-Bus executors emit one Action row per call when a
  recorder is attached; payload auto-redacted via the audit pipeline.
- `godbus/dbus/v5` promoted from indirect to direct dependency.

### Schema migrations

- **020** — `actions` table (action recording)
- **021** — `missions`, `mission_steps`, `mission_events` tables

Upgrade path verified: 19 → 20 → 21 with clean rollback at each step.

### Scoped follow-ups (intentionally not in v0.2)

The mission engine architecture supports several capabilities that are
not yet wired into the v0.2 supervisor:

- **Replanning on step failure** — today a failed step fails the
  whole mission; substituting a different worker or adjusting inputs
  is a follow-up.
- **Mid-mission user-confirmation prompts** — `mission.interrupt
  action=pause` is the manual equivalent today.
- **Parallel step dispatch** — the supervisor walks steps sequentially
  even when `depends_on` would allow concurrency.
- **Manager tier** — the 3-level supervisor → manager → worker
  hierarchy ships as 2-level (supervisor → worker) in v0.2.

The 5-tier automation framework (Tier 0 native API, Tier 1 MCP,
Tier 2 native scripting, Tier 3 accessibility, Tier 4 vision + input)
ships Tiers 0-2 in v0.2; Tier 3 (accessibility tree) and Tier 4
(vision-driven mouse/keyboard) are v0.3+ work.

---

## [0.4.1] — 2026-06-15

### Added — Replanning under parallel dispatch

LLM-driven replanning (shipped in v0.3.0 for sequential missions) now
works in parallel mode too. A failing step in a `SetPlanParallel`
mission no longer terminates the whole mission when a `Replanner` is
configured — instead the supervisor recovers.

- **Parallel replan in the supervisor.** When a step fails in
  `runParallel`, the supervisor lets any in-flight peer steps drain
  (records their results) and then, at that settled point, consults
  the configured `Replanner` exactly as the sequential path does. On
  success it persists the replacement via `Manager.Replan`, rebuilds
  the parallel dispatch state from the refreshed plan, and continues
  the DAG loop. `ErrReplanRejected` or an exhausted `MaxReplans` cap
  fails the mission as before. The cap is per-mission across both
  dispatch modes.
- **`Manager.Replan` supersede generalized.** Replanning now
  supersedes *every* still-`created` step (marking it `cancelled`,
  "superseded by replan"), regardless of its position relative to the
  failed step. In sequential mode this is unchanged (all unstarted
  steps already follow the failure). In parallel mode it correctly
  catches steps that were blocked on a dependency and sit *earlier* in
  created-order than the failure — which would otherwise orphan the
  post-replan DAG into a false deadlock.

### Changed

- **`runParallel` skips terminal steps by authoritative `State`**
  rather than by tracking a separate `failed` map (now removed as
  dead state). The DAG-completion check treats completed, cancelled,
  and superseded-failed steps all as "settled," so a post-replan plan
  with an audit-relic failed step completes cleanly instead of
  tripping the deadlock guard. New `buildParallelState` helper
  re-derives the dispatch-tracking maps at entry and after each
  replan.

### Fixed

- **Toolchain bumped to go1.26.4** (carried from the v0.4.0 release
  commit) — resolves stdlib CVEs GO-2026-5039 (net/textproto) and
  GO-2026-5037 (crypto/x509). `govulncheck ./...` reports clean.

### Notes

- Parallel replan waits for the in-flight drain so the Replanner sees
  a consistent snapshot (no steps mid-execution). The replacement
  steps replace the entire unstarted remainder of the plan.
- An LLM `Replanner` emits leaf steps; if a future Replanner emits
  grouped steps (with `SubSteps`), the supervisor re-hydrates them
  from the rewritten `PlanJSON` after the replan, so the manager tier
  composes with replanning.

---

## [0.4.0] — 2026-06-02

### Added — Manager tier (3-level hierarchy)

The mission engine grows a middle tier: supervisor → manager → worker.
Previously every plan step dispatched directly to a worker; now a step
can carry a list of sub-steps and be delegated to a manager that
decomposes and aggregates them.

- **`mission.Step.SubSteps []Step`.** A step carrying a non-empty
  `SubSteps` list is a "grouped step." It rides in the mission's
  `PlanJSON` (no schema migration — the `mission_steps` table is
  unchanged; sub-steps live only in the plan blob and are dispatched
  at run time). `omitempty` keeps existing plans parsing identically.
- **`internal/agent/manager` package.** A `Manager` subscribes to the
  new `mission.{id}.manager_dispatch` topic via competing-consumer.
  On receiving a grouped step it decomposes the sub-step list,
  dispatches each sub-step to the same worker pool the supervisor
  uses, awaits each result, and aggregates the outputs (one
  `sub_id: output` line each) into a single parent-level `Result`
  published on the events topic. A single failing sub-step terminates
  the group with a parent-level error.
- **Supervisor routing.** `runStep` (sequential) and the parallel
  dispatch loop now route grouped steps to `manager_dispatch` with the
  sub-step list JSON-encoded in `Task.Input`; leaf steps still go
  straight to a worker. A new `hydrateSubSteps` helper merges the
  `SubSteps` structure from `PlanJSON` onto the runtime steps loaded
  from the store (matched by ID). The supervisor's result-wait loop is
  unchanged — it matches the parent step ID whether the result came
  from a worker or a manager.
- **`missionruntime` spawns a Manager per mission** alongside the
  supervisor + worker. It parks idle on the `manager_dispatch`
  subscription for plans with no grouped steps (one goroutine), so the
  spawn path stays uniform and leaf-only missions behave exactly as in
  v0.3.x.

### Fixed

- **`mission.Manager.setPlan` now allocates step IDs before marshaling
  `PlanJSON`.** Previously IDs were allocated *after* the plan blob was
  serialised, so a caller passing steps with empty IDs got a `PlanJSON`
  with blank IDs that didn't match the allocated `mission_steps` row
  IDs. Harmless for leaf-only plans, but it would have silently broken
  the manager tier's `hydrateSubSteps` (which matches by ID). Sub-steps
  now get IDs allocated too, since the manager dispatches and matches
  them by ID.

### Notes

- **Scope:** one level of delegation (supervisor → manager → worker).
  Sub-steps are leaf-level — a sub-step cannot itself carry `SubSteps`
  (no recursive manager nesting). Sub-step execution within a group is
  sequential. Both are follow-ups.
- Fully backwards compatible: plans with no grouped steps dispatch
  every step directly to a worker, exactly as before. No config flag,
  no migration.

---

## [0.3.1] — 2026-06-02

### Added — Mission dashboard panel in webchat

- **New "Missions" panel in the sidebar nav.** Consumes the existing
  `/api/dashboard/missions/*` endpoints (shipped in v0.1.0) and
  renders them as a first-class UI surface alongside Chat, Home,
  Memories, Connected Apps, and Spending.
- **Header stats** — active / completed / failed counts and total
  cost across all missions, sourced from `/api/dashboard/missions/stats`.
- **Recent missions list** — goal, state badge with `waiting_user`
  pulse animation, step count, cost, age. Toggle to include
  terminal-state missions.
- **Per-mission detail subview** — click a mission to drill in. Every
  step is rendered with its state (colour-coded left border:
  green=completed, blue=running, red=failed, orange=waiting_user,
  grey=cancelled), the worker's output or error inline, and a **live
  event tail** showing the most recent 50 events newest-first
  including the v0.3.x `mission.confirmation_required` and
  `mission.replanned` events.
- **2-second polling** while the panel is open; stops automatically on
  panel switch. No SSE / WebSocket — append-only event log + low poll
  rate keeps the implementation simple and reconnect-free.
- **Theme-aware** (light/dark) and **mobile-responsive** — matches
  the existing webchat panel conventions.

All state changes (pause / resume / cancel, mission creation, plan
authoring) still happen via the agent-facing `mission.*` tools — the
dashboard panel is read-only by design.

### Added — Mid-mission auto-pause on capability confirmation

- **`capability.ConfirmationRequiredError` + `capability.ErrConfirmationRequired`.**
  New structured error type returned by the tool dispatcher when a
  capability check resolves to allowed AND `RequiresConfirmation`.
  Carries the capability resource ID and engine reason; sentinel
  detection via `errors.Is(err, capability.ErrConfirmationRequired)`
  or structured recovery via `errors.As`. Previously the
  `RequiresConfirmation` flag was plumbed through the engine but
  silently ignored at the tool dispatch layer — this commit wires it
  end-to-end.
- **`worker.Result.NeedsConfirmation` + `Result.ConfirmationReason`.**
  Distinguishes "this step did not run because the underlying
  capability requires explicit approval" from a real failure. The
  worker detects `capability.ConfirmationRequiredError` via
  `errors.As`, sets the fields, and publishes the Result with
  `Success=false` + `NeedsConfirmation=true`.
- **Supervisor auto-pause path.** `runStep` branches on
  `NeedsConfirmation` BEFORE the success/failure split. It marks the
  step `State=waiting_user` (NOT failed), stamps the reason in
  `Step.Error`, leaves `CompletedAt` unset, emits a
  `supervisor.step_paused` event, and returns a `stepNeedsConfirmationError`.
  `runSequential` catches it, emits a `mission.confirmation_required`
  event with `{step_id, reason}` payload, calls `Manager.Pause` to
  transition the mission to `waiting_user`, and exits with
  `ErrMissionPaused`. Replanning is bypassed entirely on the auto-
  pause path.
- **Runtime auto-resume.** `Runtime.scan` now lists missions in
  `StateRunning` (not just `StatePlanned`) on every poll tick. After
  the user calls `mission.interrupt action=resume`, the mission
  transitions back to running and the runtime spawns a fresh
  supervisor + worker pair on the next scan. The supervisor's cursor
  treats step `state=waiting_user` as non-terminal and re-dispatches
  cleanly.

### Added — Per-worker concurrent handling

- **`Worker.MaxConcurrent` field.** Caps how many tasks a single
  worker may process at once. Default 1 (preserves the historical
  one-task-at-a-time semantics — fully backwards compatible). Set
  to N > 1 to fan tasks out into goroutines bounded by an internal
  semaphore: when the worker is at the cap its receive loop blocks
  before consuming the next dispatch, so the bus's competing-
  consumer routing keeps pushing work to peer workers (or queues
  briefly until a slot frees up).
- **Clean shutdown.** `Worker.Run` now waits for every in-flight
  handler to complete before returning on context cancellation. With
  `MaxConcurrent=1` (the default) this is a single goroutine wait;
  with `MaxConcurrent=N` it's all N. No leaked goroutines and no
  silent result-publish races during shutdown.
- **`missionruntime.Options.WorkerMaxConcurrent`.** Runtime-level
  configuration plumbed through to every spawned `Worker`. Same
  zero-value-is-1 default. Useful when wiring the runtime to an
  LLM-backed executor: a single worker with `MaxConcurrent=5` can
  drive five concurrent LLM API calls rather than serialising them.

### Performance

- **9× wall-clock parallelism upper bound.** With 3 workers ×
  `MaxConcurrent=3`, up to 9 LLM API calls can be in flight
  simultaneously while the goroutine count and runtime overhead stay
  small. Verified by test: 3 tasks × 200 ms each on one worker with
  `MaxConcurrent=3` complete in ~205 ms wall-clock vs ~600 ms
  sequential.

### Notes

- Per-worker concurrency multiplies with cross-worker parallelism
  (the competing-consumer bus shipped in v0.2.2). With M workers ×
  `MaxConcurrent=N`, the effective in-flight cap is M×N tasks
  pool-wide. Existing single-task-per-worker deployments see no
  behaviour change.

---

## [0.3.0] — 2026-06-01

### Added — LLM-driven replanning on step failure

- **`supervisor.Replanner` interface + `Supervisor.Replanner` /
  `Supervisor.MaxReplans` fields.** When a step fails in sequential
  mode, the supervisor consults the configured `Replanner` for a
  replacement step sequence and continues from there instead of
  failing the mission. Each mission may be replanned up to
  `MaxReplans` times (default 3). The Replanner can return
  `ErrReplanRejected` to opt out of recovery for a specific failure;
  any other non-nil error is treated as an implementation failure
  and surfaced in the `mission.failed` reason. Existing missions
  without a configured Replanner keep the v0.2.x fail-fast
  behaviour — purely additive.
- **`supervisor.NoopReplanner`** — a Replanner that always returns
  `ErrReplanRejected`. Same semantics as leaving `Replanner` nil;
  useful in tests and as documentation.
- **`mission.Manager.Replan(missionID, fromStepID, newSteps, reason)`**
  — persistence-layer replanning. Appends replacement steps after the
  failed one, marks every still-unstarted original step that came
  after as `cancelled` (error: "superseded by replan"), rewrites
  `Mission.PlanJSON` to the union, and emits one
  `mission.replanned` event with payload
  `{from_step_id, new_step_count, superseded_step_ids, reason}`.
  The failed step itself stays in `state=failed` for the audit log;
  replanning never rewrites history.
- **`missionruntime.NewLLMReplanner(LLMReplannerConfig)`** — an
  LLM-backed `supervisor.Replanner`. Calls a configured
  `agent.ModelAdapter` directly with a strict JSON-output prompt
  (no tools, no nested agent loop, predictable cost), retries on
  malformed output up to `MaxRetries` (default 1), and translates
  an empty `steps` array to `ErrReplanRejected`. Per-call timeout
  is `Timeout` (default 30 s). Strips common LLM noise (markdown
  fences, leading/trailing prose) before parsing.
- **`missionruntime.Options.Replanner` /
  `Options.MaxReplans` / `Runtime.SetReplanner(...)`**. The runtime
  passes the configured Replanner to every spawned Supervisor.
  `SetReplanner` mirrors `SetExecutor` for hot-swap at app startup
  once the model adapter is resolved.

### Notes

- Replanning is **sequential mode only** in this revision. Parallel
  dispatch (`SetPlanParallel`) still fails the whole mission on the
  first failure; in-flight peers complete naturally but no replan
  attempt is made. Parallel-mode replanning has its own design
  problem (what to do with in-flight peers whose results are partway
  done) and is a separate follow-up.

---

## [0.2.3] — 2026-05-31

### Fixed

- **Data race in bus Unsubscribe vs concurrent in-flight publish**
  (supersedes v0.2.2). `UnsubscribeReliable` and `UnsubscribeCompeting`
  previously closed the subscriber channel. `tryPublishReliable` /
  `tryPublishCompeting` snapshot the subscriber slice under read-lock
  and then send on the chan after releasing the lock — closing the
  chan during that send window racing under `-race` and could panic
  ("send on closed channel") under the same goroutine timing in
  production. The race had been latent on the broadcast path since
  v0.2.0; v0.2.2's competing-consumer dispatch + worker's
  fire-and-forget `publishResult` goroutine made it reproducible in
  CI on `internal/agent/missionruntime`. Fix removes the close in
  both Unsubscribe paths — subscribers exit on their own context
  (worker and supervisor already do), and the chan is reclaimed by
  GC once the bus and the subscriber goroutine drop their references.
  Empty topic entries are now also pruned from the bus's internal
  map so the `*TopicCount` helpers report the actual count.
  `TestUnsubscribeReliable_RemovesAndCloses` renamed to
  `TestUnsubscribeReliable_RemovesFromTopic` and the assertion
  changed from "channel closes" to "topic disappears + subsequent
  publish returns `ErrNoSubscribers`."

## [0.2.2] — 2026-05-31

### Added — Parallel mission execution

- **`mission.Manager.SetPlanParallel(missionID, steps)`** — new sibling
  to `SetPlan`. Marks the plan for DAG dispatch in the supervisor:
  the supervisor walks `Step.DependsOn` as an authoritative dependency
  graph, dispatches every step whose dependencies are completed, and
  blocks on the next worker result rather than serialising at each
  step. Failure terminates the mission (already-in-flight peers
  publish their results, no new dispatches issued). Dangling
  `DependsOn` references are detected as deadlocks and surface a clear
  error.
- **`mission.Plan` struct + `mission.PlanFromJSON` helper** — the plan
  serialised in `Mission.PlanJSON` now carries a `Parallel bool` flag.
  `omitempty` keeps existing PlanJSON blobs parsing as
  `Parallel=false` — no migration needed.
- **Competing-consumer bus mode** — new `bus.SubscribeCompeting` /
  `bus.PublishCompeting` / `bus.UnsubscribeCompeting`. Each
  `PublishCompeting` message lands on EXACTLY ONE subscriber in the
  competing group; per-publish shuffle gives fair load distribution;
  busy peers fall through to ready peers via `SendTimeout`.
  Subscriber channels are unbuffered, so a send only completes when
  a subscriber is actively waiting in its receive select — clean
  "busy peer skipped" semantics with no queueing behind an already-
  busy worker. Independent of the existing broadcast
  `PublishReliable` / `SubscribeReliable` — same topic can have both
  kinds of subscribers, and a competing publish does NOT reach
  broadcast subscribers (and vice versa).
- **Worker + supervisor switched to competing-consumer for dispatch.**
  Worker uses `bus.SubscribeCompeting` for `mission.{id}.dispatch`;
  supervisor uses `bus.PublishCompeting`. Mission `.events` topic
  stays on broadcast so the dashboard and other observers can tail
  the event stream alongside the supervisor.

### Performance

- **Real wall-clock parallel execution.** With N workers competing for
  dispatched tasks, independent steps run concurrently. Verified by
  test: 3 independent steps × 200 ms each with 3 workers complete in
  ~220–370 ms wall-clock (best vs worst shuffle luck) vs ~600 ms
  sequential. CI-safe assertion bound is < 500 ms.

### Notes

- A single worker still handles one task at a time inside its own
  goroutine. Long-tail tasks could be fanned out into goroutines
  within a worker as a follow-up; today, scaling parallelism means
  running more worker instances.

[0.1.0]: https://github.com/LumabyteCo/aibutler/releases/tag/v0.1.0
[0.2.0]: https://github.com/LumabyteCo/aibutler/releases/tag/v0.2.0
[0.2.1]: https://github.com/LumabyteCo/aibutler/releases/tag/v0.2.1
[0.2.2]: https://github.com/LumabyteCo/aibutler/releases/tag/v0.2.2
[0.2.3]: https://github.com/LumabyteCo/aibutler/releases/tag/v0.2.3
[0.3.0]: https://github.com/LumabyteCo/aibutler/releases/tag/v0.3.0
[0.3.1]: https://github.com/LumabyteCo/aibutler/releases/tag/v0.3.1
[0.4.0]: https://github.com/LumabyteCo/aibutler/releases/tag/v0.4.0
[0.4.1]: https://github.com/LumabyteCo/aibutler/releases/tag/v0.4.1
