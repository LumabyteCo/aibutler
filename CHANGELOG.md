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

## [Unreleased]

Changes in `main` that haven't been released yet will be tracked here.

[0.1.0]: https://github.com/LumabyteCo/aibutler/releases/tag/v0.1.0
[0.2.0]: https://github.com/LumabyteCo/aibutler/releases/tag/v0.2.0
[Unreleased]: https://github.com/LumabyteCo/aibutler/compare/v0.2.0...HEAD
