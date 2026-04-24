# Contributing to AI Butler

**AI Butler is a community-driven project. We need your help.**

We've built an ambitious core — 116 Go packages, 1,681 tests, 59 internal security audit passes, and an architecture that spans memory, scheduling, 12 messaging channels, an A2A protocol implementation, MCP client + server, a WASM plugin sandbox, and a capability-gated agent loop. **The core works.** What we need now is **real-world validation** and **the last-mile wiring** that turns beta features into production-ready ones.

This document covers how to help. If you're looking for the deep technical reference (build system, project layout, code style, PR process), see [`docs/CONTRIBUTING.md`](docs/CONTRIBUTING.md).

---

## Where we need the most help

### 🧪 Test a beta feature with real credentials

The fastest way to push a feature from `beta` → `ready` is to try it with your own API credentials and file a report.

**Messaging channels** — we have code for 12 channels, but only `webchat` and the `terminal` REPL are confirmed in daily use. The other 10 (Telegram, Slack, Discord, WhatsApp, Teams, Google Chat, LINE, IRC, custom webhook, Nostr) are all code-complete and unit-tested but pending real-world validation. If you have credentials for any of them, please try the corresponding adapter and open an issue with your results (good or bad). Use the `beta-report` label.

**AI providers** — Claude is the only provider we've tested end-to-end. The OpenAI, Gemini, Grok, and local (LM Studio, vLLM, Groq, DeepSeek) adapters all have code and unit tests, but real-API validation is pending. Point the config at your provider and tell us what happens.

**Auth** — OIDC SSO, FIDO2/WebAuthn, and TOTP all have implementations. We'd love real-world validation against Keycloak, Auth0, Authelia, ZITADEL, hardware keys (YubiKey, SoloKey), and authenticator apps.

### 📝 Write a sample WASM plugin

The plugin runtime works, but there are no sample plugins in the repo yet. **The first external plugin will set the pattern for the marketplace.** Pick something useful:

- A weather plugin calling OpenWeather/Meteo
- A news summarizer calling Hacker News / Reddit
- A calendar integration (Google Calendar, Fastmail, ProtonCal)
- A journal / diary plugin that writes to markdown files
- A custom LLM wrapper for an obscure provider

The plugin runtime lives in `internal/plugin/` — see the README on each subpackage (manifest, sandbox, scanner, store, defense, host) for the runtime API, capability declaration, and build flow. We'll feature the first 5 community plugins in the README.

### 🏠 Wire up the Home Assistant adapter

AI Butler's IoT tool surface is complete — `iot.sensor.read`, `iot.device.control`, `iot.safety.control`, `iot.device.list`, `iot.device.discover`. They talk to a stub adapter today. **We need someone to implement the same interface against the Home Assistant REST API** so the tools work with real devices.

This is probably our highest-impact single contribution. Reference: `internal/iot/stub.go`. Target: `internal/iot/homeassistant.go`.

### 🤝 Verify A2A v2 protocol interop

We believe our A2A v2 handler is spec-compliant with [Google's A2A protocol](https://github.com/google/A2A). If you're building another A2A-compliant agent, we'd love to try running them against each other. Interop reports are gold.

### 🌐 Translate the web UI

The web UI already has an i18n framework with English and Arabic (RTL) baked in. We want to add 12 more languages. Translation is ~40 strings total — shouldn't take more than an hour. See `internal/webchat/static/chat.js` for the `i18nStrings` object.

Languages we want first: Spanish, French, German, Portuguese, Italian, Japanese, Korean, Chinese (Simplified + Traditional), Russian, Hindi, Turkish.

### 🧑‍💻 Build editor extensions

The dashboard API (`/api/dashboard/*`) exposes everything an editor extension needs — memory search, tool execution, session management, cost tracking. If you use VS Code, JetBrains, Zed, Emacs, Vim/Neovim, Sublime — we'd love to see an extension that talks to a local AI Butler instance.

### 📹 Make a demo video

Show off what AI Butler can do in 2–3 minutes. Good candidates:

- "I told my AI Butler about my sister's birthday last week — watch it remember today"
- "One command schedules a daily briefing across Telegram, the terminal, and the web"
- "Switching models mid-conversation with zero friction"
- "Connecting Claude Desktop to AI Butler via MCP"

We'll link it from the README and the landing page.

### 🐛 Fix a good-first-issue

We tag beginner-friendly issues with `good-first-issue`. Check [the issues list](https://github.com/LumabyteCo/aibutler/issues?q=is%3Aissue+is%3Aopen+label%3Agood-first-issue).

---

## Getting set up (5 minutes)

```bash
git clone https://github.com/LumabyteCo/aibutler.git
cd aibutler
CGO_ENABLED=0 go build -o aibutler .
./aibutler vault set anthropic_api_key sk-ant-...   # or ollama (no key)
./aibutler run
```

Run the tests:

```bash
go test ./... -race
```

Run the security scan:

```bash
go install golang.org/x/vuln/cmd/govulncheck@latest
govulncheck ./...
```

Serve the docs site locally:

```bash
cd website && npm install && npm run dev
```

For the full technical reference (project layout, code style, PR process, integration tests), see [`docs/CONTRIBUTING.md`](docs/CONTRIBUTING.md).

---

## Our values

- **Honesty over marketing.** Every feature is labeled `ready`, `beta`, or `v0.2+`. We don't claim capabilities we haven't verified.
- **Single-binary discipline.** No CGO. Zero required external services. The binary has to run offline on a Raspberry Pi.
- **Memory is the #1 feature.** If you're adding something that could improve memory (indexing, recall, ranking, compression, pruning), that's our highest priority.
- **User privacy by default.** No telemetry unless opted in. No phoning home. No hidden network calls.
- **Security-first.** Every new tool needs a capability, every capability needs an audit entry, every external call needs rate limiting and allowlisting.
- **Consumer-friendly UI.** The web UI is for everyone, not just developers. No jargon ("backend", "entities", "swarm") in user-facing strings.

---

## Pull request process

1. **Open an issue first** for anything non-trivial — it saves everyone time
2. **Fork and branch** — use descriptive names: `beta/telegram-reliability`, `feat/home-assistant-adapter`, `fix/memory-race`
3. **Write tests** — every new feature needs unit tests, every bug fix needs a regression test
4. **Run `go test ./... -race` and `govulncheck ./...`** — both must pass
5. **Update docs** in `website/src/content/docs/` if your change is user-facing
6. **One change per PR** — easier to review, easier to revert
7. **Clear commit messages** — describe the *why*, not just the *what*
8. **CLA-free** — no contributor license agreement. You keep your copyright; you just grant the same Apache 2.0 license as the rest of the project.

---

## Code of conduct

Be kind. Assume good faith. No harassment, no slurs, no personal attacks. Debate ideas, not people. We reserve the right to remove comments or ban contributors that violate this.

---

## Getting help

- **General questions / ideas / show-and-tell:** [GitHub Discussions](https://github.com/LumabyteCo/aibutler/discussions)
- **Bug reports and feature requests:** [GitHub Issues](https://github.com/LumabyteCo/aibutler/issues)
- **Security vulnerabilities:** see [SECURITY.md](SECURITY.md) — please report privately
- **Documentation:** [docs.aibutler.dev](https://docs.aibutler.dev)

---

Thank you for helping build AI Butler. This project only works if the community pushes it forward — and you're the community.
