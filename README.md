<p align="center">
  <h1 align="center">AI Butler</h1>
  <p align="center"><strong>The open-source AI agent that connects everything.</strong></p>
  <p align="center">
    Multi-channel personal assistant with exceptional memory, any AI model,<br/>
    self-hosted. One Go binary. No dependencies. Works offline.
  </p>
  <p align="center">
    <a href="https://aibutler.dev">Website</a> ·
    <a href="https://docs.aibutler.dev">Docs</a> ·
    <a href="#quick-start">Quick Start</a> ·
    <a href="#whats-in-this-release">What's in this release</a> ·
    <a href="CONTRIBUTING.md">Contribute</a>
  </p>
</p>

<p align="center">
  <a href="https://github.com/LumabyteCo/aibutler/actions/workflows/ci.yml"><img alt="CI" src="https://github.com/LumabyteCo/aibutler/actions/workflows/ci.yml/badge.svg?branch=main"></a>
  <a href="https://github.com/LumabyteCo/aibutler/actions/workflows/security.yml"><img alt="Security" src="https://github.com/LumabyteCo/aibutler/actions/workflows/security.yml/badge.svg?branch=main"></a>
  <a href="LICENSE"><img alt="License" src="https://img.shields.io/badge/license-Apache%202.0-blue.svg"></a>
  <img alt="Go" src="https://img.shields.io/badge/go-1.26%2B-00ADD8">
  <img alt="CGO" src="https://img.shields.io/badge/CGO-not%20required-success">
  <img alt="Status" src="https://img.shields.io/badge/status-v0.1%20public%20beta-orange">
  <a href="https://github.com/LumabyteCo/aibutler/releases"><img alt="Release" src="https://img.shields.io/github/v/release/LumabyteCo/aibutler?include_prereleases&label=release"></a>
</p>

---

> **🚀 v0.1 — Public Beta.** AI Butler is an ambitious project — **116 packages, 1,681 passing tests, 59 internal security audit passes.** The core (memory, webchat, scheduler, agent loop, MCP integration) is production-ready. Several advanced features are in beta and labeled clearly below. We built this in the open and we'd love your help finishing it. [See what's ready →](#whats-in-this-release)

---

## Why AI Butler

Your AI should work wherever you are — your phone, your terminal, your smart home, your team chat. It should remember what you told it last week. It should coordinate complex tasks across multiple agents. And it should be **yours** — self-hosted, private, extensible, auditable.

**That's AI Butler.**

|   | What makes it different |
|---|---|
| 💬 **Works everywhere you chat** | 12 channels — Telegram, WhatsApp, Slack, Discord, Teams, Google Chat, LINE, IRC, web chat, terminal, custom webhook, Nostr (web chat + terminal are production-ready; the other 10 are in beta — see the release table below). One agent, shared memory, all platforms. |
| 🧠 **Memory that actually works** | Knowledge graph + FTS5 full-text search + vector embeddings, fused with reciprocal rank fusion. Ask about something you mentioned weeks ago — it remembers. A core strength of AI Butler: your memory stays local, is yours, and is never LLM-summarized. |
| 🤖 **Any AI model** | Claude, GPT, Gemini, Grok, or fully local via Ollama, LM Studio, vLLM, Groq, DeepSeek. Bring your own key, swap models per-task, or run entirely offline. |
| 🔗 **Agent ecosystem hub** | [Google A2A v2](https://github.com/google/A2A) (beta — handler works, conformance suite pending). Built-in MCP client + MCP server. Subprocess bridges for wrapping any CLI tool. |
| 🛡️ **Security-first** | 59-pass internal security audit. RBAC, FIDO2/WebAuthn, OIDC SSO, capability-gated tools, WASM plugin sandbox, SSRF protection, shell allowlisting. |
| 🖥️ **Clean built-in web UI** | 6-panel sidebar dashboard — chat, home, memories browser, connected apps, spending, settings. No framework, no build step, embedded in the binary. |
| 💾 **Self-hosted & private** | Single Go binary. Your data stays on your machine. Runs from Raspberry Pi to cloud. Zero telemetry unless you explicitly opt in. |
| 🌍 **Open source** | Apache 2.0 licensed (patent grant included). 1,681 tests. 116 packages. Zero CVEs (`govulncheck` verified). |

---

## What's in this release

**We're shipping honest labels instead of marketing.** Here's exactly what's production-ready, what's beta (works but needs community testing), and what's coming in v0.2:

### ✅ Production-ready (v0.1)

| Feature | Status |
|---|---|
| **Agent loop** with streaming, tool calls, capability gating | `ready` |
| **Memory system** — FTS5 + knowledge graph + vector embeddings + hybrid search | `ready` |
| **Claude integration** (Anthropic API) | `ready` |
| **Ollama integration** (local models + embeddings, auto-detected) | `ready` |
| **Ollama Cloud** (OpenAI-compatible, `base_url: https://ollama.com`, validated end-to-end with `glm-5.1` on a 50-test QA suite) | `ready` |
| **Web chat interface** — 6-panel sidebar, streaming, file upload, voice upload, responsive, dark mode | `ready` |
| **Terminal REPL** — interactive, streaming, slash commands, session resume | `ready` |
| **Scheduler** — natural language → cron, persistent, reliable | `ready` |
| **Cost tracking** — per-model, per-session, live dashboard | `ready` |
| **MCP client** — connects to external MCP servers on boot | `ready` |
| **Vault** — credential storage with OS keyring integration | `ready` |
| **Capability engine** — per-tool permissions + audit trail | `ready` |
| **SQLite database** — 67 tables, 19 migrations, WAL mode | `ready` |
| **Config system** — Three-tier (Settings / Configurations / Options) + BUTLER.md project files | `ready` |
| **File + shell + git tools** — capability-gated, sandboxed | `ready` |
| **Single-binary distribution** — zero CGO, cross-compiles to any Go platform | `ready` |

### 🟡 Beta — code complete, needs real-world validation (v0.1)

We wrote the code and it compiles + passes unit tests, but we haven't put these through end-to-end production use with real third-party APIs. **We'd love your help testing these.** Open an issue if you hit anything.

| Feature | Status | Help us test |
|---|---|---|
| **Telegram channel** (Bot API) | `beta` | [#test-telegram](../../issues/new?labels=beta&title=Telegram+test+report) |
| **Slack channel** (Bolt SDK) | `beta` | [#test-slack](../../issues/new?labels=beta&title=Slack+test+report) |
| **Discord channel** (Gateway) | `beta` | [#test-discord](../../issues/new?labels=beta&title=Discord+test+report) |
| **WhatsApp channel** (Meta Cloud API) | `beta` | [#test-whatsapp](../../issues/new?labels=beta&title=WhatsApp+test+report) |
| **Microsoft Teams, Google Chat, LINE, IRC, Nostr, custom webhook** | `beta` | [#test-channels](../../issues/new?labels=beta&title=Other+channel+test+report) |
| **OpenAI GPT + Azure OpenAI** | `beta` | — |
| **Google Gemini** | `beta` | — |
| **xAI Grok** | `beta` | — |
| **LM Studio / vLLM / Groq / DeepSeek** (OpenAI-compatible) | `beta` | — |
| **Multi-agent swarm orchestration** — engine runs, first cookbook example coming | `beta` | [#swarm-feedback](../../issues/new?labels=beta&title=Swarm+feedback) |
| **A2A v2 protocol compliance** — handler works, conformance suite pending | `beta` | [#a2a-interop](../../issues/new?labels=beta&title=A2A+interop+report) |
| **MCP server mode** (exposing Butler's tools to other MCP clients) | `beta` | [#mcp-server-test](../../issues/new?labels=beta&title=MCP+server+test) |
| **WASM plugin sandbox** (Extism runtime) — runtime ready, sample plugins coming | `beta` | [#plugin-feedback](../../issues/new?labels=beta&title=Plugin+feedback) |
| **OIDC SSO** (Auth0, Okta, Keycloak, Authelia, ZITADEL) | `beta` | [#sso-test](../../issues/new?labels=beta&title=OIDC+test) |
| **FIDO2 / WebAuthn** (hardware security keys) | `beta` | — |
| **TOTP 2FA** | `beta` | — |
| **RBAC** (admin/user/viewer/agent roles) | `beta` | — |
| **LAN mode + mDNS discovery** | `beta` | — |
| **Subprocess bridges** (ffmpeg, Aider, Continue, any CLI) | `beta` | — |
| **Smart home via Home Assistant** — tool surface + PIN safety gating ready, HA adapter in final wiring | `beta → v0.2` | [#ha-adapter](../../issues/new?labels=beta&title=Home+Assistant+adapter) |
| **Whisper STT** (local + cloud) | `beta` | — |
| **Piper TTS** (local, CPU-only) | `beta` | — |

### ✨ New in v0.2

The v0.2 release theme: **AI Butler can act on your computer**. Native
OS scripting on every major platform, vision input on every major
adapter, and a mission engine for goals that take more than one turn.

| Feature | Status | What it does |
|---|---|---|
| **Native OS scripting** — `shell.applescript` (macOS), `shell.dbus` (Linux), `shell.shortcuts` (Apple Shortcuts), plus the existing `shell.powershell` (Windows) | `ready` | Drive Mail, Calendar, Music, Notification Center, MPRIS players, etc. without vision-driven UI clicking. |
| **Cross-OS dispatcher** — `shell.script` | `ready` | Agent provides per-OS payloads; dispatcher routes by `runtime.GOOS`. |
| **Vision input on Ollama vision / GPT-4o / Claude** — new `agent.Image` field with base64 + URL sources | `ready` | Verified end-to-end against `qwen3-vl:8b` and Claude image API. |
| **Mission engine** — persistent state machine, supervisor + worker agents, reliable-bus orchestration, `aibutler mode mission` runtime, dashboard backend | `ready` | Long-running goals with audit trail, pause/resume/cancel, LLM-backed task execution. See [docs/agents/MISSIONS.md](docs/agents/MISSIONS.md). |
| **Clipboard tool** — `clipboard.read` / `clipboard.write`, cross-platform | `ready` | Privacy: read records byte-count only, never content. |
| **`wait.until` primitive** — file_exists / process_running / port_open / http_ready / duration | `ready` | Block until real-world readiness instead of racing UIs. |
| **`cost.forecast` tool** | `ready` | Pre-action token + USD estimate for a planned model call. |
| **macOS permission wizard** — `permissions.check` | `ready` | Probes Automation + Screen Recording, deep-links to Settings panels. |
| **Just-in-time credential broker** — `vault.request` | `ready` | Default-deny credential issuance with audit trail. |
| **Action recording** — fine-grained `actions` audit log with credential redaction | `ready` | Every native-script call logged with target, payload (redacted), duration, result. |
| **AppleScript target-app allowlist** — `tell:Mail`, `tell:Music*`, `tell:*` | `ready` | Finer-grained safe defaults than bare-verb allowlisting. |

### 🔜 On the roadmap (v0.3+)

| Feature | Target |
|---|---|
| **Hosted demo** (`demo.aibutler.dev`) | v0.2.x |
| **LLM-driven replanning** when a mission step fails | v0.2.x |
| **Mid-mission user-confirmation prompts** (today: manual `mission.interrupt action=pause`) | v0.2.x |
| **Parallel step dispatch** in supervisor (sequential today) | v0.2.x |
| **Manager tier** — 3-level supervisor → manager → worker hierarchy (2-level today) | v0.3 |
| **Tier 3 accessibility tree** (AX, UIAutomation, AT-SPI) — one level finer than vision-driven UI | v0.3 |
| **Tier 4 vision + input** — screen capture + mouse/keyboard for the long tail | v0.3 |
| **Smart home full integration** — working Home Assistant adapter out of the box | v0.3 |
| **Plugin marketplace** with sample plugins | v0.3 |
| **Voice TUI mode** — terminal mic capture + playback | v0.3 |
| **Image generation** — Flux, Stable Diffusion, DALL-E via official APIs | v0.3 |
| **Advanced TTS** — ElevenLabs adapter | v0.3 |
| **Video generation and advanced creative tools** | Later |
| **Internet mode** with autocert + password + TOTP | v0.3 |
| **PWA** (installable web app) | v0.3 |
| **Homebrew formula** | v0.3 |
| **SLSA Level 3 provenance + cosign binary signing** | v0.3 |
| **14-language i18n** (full Arabic RTL, CJK, etc.) | v0.3 |
| **External security audit** + bug bounty program | v1.0 |

**Want to help push a feature from beta → ready, or from roadmap → beta?** [Open an issue](../../issues/new) or [start a discussion](../../discussions). This project thrives on contribution.

---

## Quick Start

### Install

```bash
# Clone + build from source (Go 1.26+)
git clone https://github.com/LumabyteCo/aibutler.git
cd aibutler
CGO_ENABLED=0 go build -o aibutler .
sudo mv aibutler /usr/local/bin/

# Or with Docker
docker pull ghcr.io/lumabyteco/aibutler:latest
```

### Configure

```bash
# Add your AI model API key (choose one)
aibutler vault set anthropic_api_key sk-ant-...
# Or: aibutler vault set openai_api_key sk-...
# Or: aibutler vault set gemini_api_key AIza...
# Or use Ollama (no key needed): just install and run Ollama

# Run the setup wizard
aibutler setup
```

### Run

```bash
# Start all configured channels + scheduler
aibutler run

# Or use the interactive terminal REPL
aibutler repl

# Web interface at http://localhost:3377
```

### Connect a messaging channel (optional — beta)

```bash
# Telegram
aibutler vault set telegram_bot_token YOUR_BOT_TOKEN

# Slack
aibutler vault set slack_bot_token xoxb-...
aibutler vault set slack_app_token xapp-...

# Discord
aibutler vault set discord_bot_token YOUR_TOKEN

# WhatsApp
aibutler vault set whatsapp_access_token YOUR_TOKEN
aibutler vault set whatsapp_phone_number_id YOUR_PHONE_ID
```

**All channel adapters are beta.** Tell us how they work — it's the fastest way to push them to `ready`.

---

## Architecture

```
                              User
                                │
     ┌──────────────────────────┼──────────────────────────┐
     │                          │                          │
   Web UI                  Terminal REPL              Messaging
  (built-in)                                     (Telegram, Slack,
                                                  Discord, WhatsApp,
                                                   Teams, LINE …)
     │                          │                          │
     └──────────────────────────┼──────────────────────────┘
                                │
                       ┌────────┴─────────┐
                       │    AI Butler     │
                       │                  │
                       │   Agent loop     │ ←── Capability engine
                       │   Memory         │     + Audit trail
                       │   Scheduler      │
                       │   Swarm (beta)   │
                       │   Plugins (WASM) │
                       │                  │
                       └────────┬─────────┘
                                │
       ┌────────────────────────┼────────────────────────┐
       │                        │                        │
    A2A v2               MCP (client + server)     Subprocess
    (agent-to-agent)     (external tool servers)   bridges
```

### Key Numbers

| Metric | Value |
|--------|-------|
| Tests (passing, race-free) | **1,681** |
| Go packages | **116** |
| SQLite tables | **67** |
| Database migrations | **19** |
| Internal security audit passes | **59** |
| CVEs (`govulncheck` verified) | **0** |
| External Go dependencies | **~10 direct** |
| CGO required | **No** |
| Channels wired | **12** (2 ready: web chat + terminal; 10 beta) |
| AI providers wired | **6+** (Claude + Ollama ready, others beta) |

---

## Security

AI Butler is built with a security-first architecture:

- **59-pass internal security audit** with 74 findings found and 70+ fixed
- **Capability engine** with per-tool granular permissions and audit logging
- **RBAC** (`admin`, `user`, `viewer`, `agent` roles) with OIDC SSO and FIDO2/WebAuthn (beta)
- **SSRF protection** blocking private/internal IP ranges
- **WASM plugin sandbox** (Extism) — plugins can't access filesystem or network without declared grants
- **Shell command allowlisting** with namespace isolation (Linux `unshare`, macOS `sandbox-exec`)
- **Webhook signature verification** (Telegram secret token, Slack HMAC-SHA256, Discord Ed25519)
- **Rate limiting** on all external-facing endpoints
- **Zero CVEs** — `govulncheck` verified clean
- **Responsible disclosure** — see [SECURITY.md](SECURITY.md)

**Note:** The audit is internal. An external third-party audit + bug bounty program is planned for v1.0. [Help us prepare.](../../discussions)

---

## Deployment

### Docker

```bash
docker compose up -d
# With Ollama (fully local AI, no API key needed):
docker compose -f docker-compose.ollama.yml up -d
# Full stack (with Home Assistant placeholder):
docker compose -f docker-compose.full.yml up -d
```

### Kubernetes

```bash
helm install aibutler deploy/helm/aibutler/
```

### systemd

```bash
sudo deploy/systemd/install.sh
```

### Raspberry Pi / ARM / RISC-V

```bash
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -o aibutler .
# Transfer to Pi and run
```

---

## Configuration

AI Butler uses a **three-tier configuration system** so regular users don't drown in knobs but power users can tune everything:

| Tier | Audience | Example |
|------|----------|---------|
| **Settings** | Everyone | Language, active channels, AI model, monthly budget |
| **Configurations** | Power users | OAuth providers, swarm settings, bridge config, hooks |
| **Options** | Developers | Token limits, retry counts, timeout tuning |

```yaml
# ~/.aibutler/config.yaml
settings:
  language: en
  active_channels: [webchat, telegram]
  model: claude-sonnet-4-6
  cost:
    strategy: balanced
    monthly_budget: 25  # USD
```

Per-project overrides via `BUTLER.md` files (like `.cursorrules` but for AI Butler). See [docs/config/reference](https://docs.aibutler.dev/config/reference/).

---

## Contributing

**We're actively looking for community help to push features from beta → ready.** See [CONTRIBUTING.md](CONTRIBUTING.md) for the full guide.

**Highest-impact contributions right now:**

- 🧪 **Test a beta channel end-to-end** with your own credentials and file a report
- 📝 **Write a sample WASM plugin** for the marketplace
- 🤝 **Connect a real A2A v2 peer** and verify interop
- 🏠 **Wire up the Home Assistant adapter** — the tool interface is ready
- 🌐 **Translate the web UI** to your language
- 🧑‍💻 **Build a VS Code or JetBrains extension** against the dashboard API
- 🪄 **Build the multi-agent swarm cookbook** — show off what swarm can do

```bash
# Run tests
go test ./... -race

# Run security scan
govulncheck ./...

# Build
CGO_ENABLED=0 go build -o aibutler .

# Serve the docs site locally
cd website && npm run dev
```

---

## License

Apache 2.0 — see [LICENSE](LICENSE). Includes explicit patent grant and retaliation clause, appropriate for AI agents that may interact with patented APIs and services.

---

## Links

- **Website:** [aibutler.dev](https://aibutler.dev)
- **Documentation:** [docs.aibutler.dev](https://docs.aibutler.dev)
- **FAQ:** [docs.aibutler.dev/faq](https://docs.aibutler.dev/faq/)
- **Discussions:** [GitHub Discussions](../../discussions)
- **Issues:** [GitHub Issues](../../issues)
- **Security:** [SECURITY.md](SECURITY.md)
- **Roadmap:** see [CHANGELOG.md](CHANGELOG.md)

---

<p align="center">
  Built with care by <a href="https://lumabyte.co">LumaByte Co</a> and contributors.<br/>
  <sub>AI Butler is an independent open-source project. Not affiliated with Anthropic, OpenAI, Google, or any AI model provider.</sub>
</p>
