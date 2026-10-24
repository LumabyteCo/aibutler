# External Security Audit — Readiness Package

**Status: readiness package complete; external audit not yet commissioned.**
This document is the hand-off for an independent firm. It records scope,
architecture, controls, evidence, and known gaps so the audit can start
without a discovery phase.

Target: complete the external audit before **v1.0**.

---

## 1. Scope

| Area | Included |
|---|---|
| Go source (single static binary, zero CGO) | `cmd`, `internal/*` |
| Agent loop, tool dispatcher, capability engine | yes |
| Local HTTP surface (webchat, dashboard, auth, uploads) | yes |
| Channels (Telegram, Slack, Discord, WhatsApp, email, webchat) | yes |
| Vault, credential lifecycle, keyring/file backends | yes |
| Memory, transcripts, checkpoints, backups | yes |
| Smart-home adapter (Home Assistant) + three-tier safety | yes |
| Plugin sandbox (Extism/WASM), shell sandbox | yes |
| MCP client/server, A2A v2 peer protocol | yes |
| Build/release pipeline, SBOM, dependency policy | yes |
| Out of scope | third-party services, hosted models, OS keyring internals |

## 2. Architecture and trust boundaries

- **Single-user, self-hosted** by default; webchat binds to localhost.
- **Untrusted inputs:** chat messages, attachments, tool arguments, web
  content, MCP/A2A peers, Home Assistant state, model output.
- **Trusted:** local config/vault, the Go binary, the OS user account.
- **Boundary crossings:** model provider HTTP, channel APIs, local shells,
  native scripting (AppleScript/D-Bus/PowerShell), file system, Home
  Assistant REST, plugin WASM.

See [SECURITY-MODEL.md](SECURITY-MODEL.md) for the control inventory
(capability grants, PIN gates, SSRF guard, allowlists, audit trail,
redaction) and [SUPPLY-CHAIN.md](SUPPLY-CHAIN.md) for dependencies.

## 3. Controls inventory (summary)

| Control | Where |
|---|---|
| Per-tool capability grants, deny-by-default | `internal/capability/` |
| Tier-3 safety PIN (bcrypt, vault) + confirmation | `internal/iot/pin.go` |
| Shell allowlist + parser (no `;`, `&&`, `|`, substitution) | `internal/shell/` |
| SSRF guard / offline mode | `internal/security/`, `internal/offline/` |
| Secret redaction in prompts, logs, audit | `internal/audit/redactor.go` |
| Tamper-evident audit trail | `internal/audit/`, `actions` |
| Vault: OS keyring or encrypted file | `internal/vault/` |
| WASM plugin sandbox | `internal/plugin/sandbox/` |
| Workspace path boundary checks | `internal/file/`, `internal/checkpoint/` |
| Archive zip-slip + zip-bomb checks | `internal/media/archive/` |
| Dependency scanning (govulncheck) | CI `security.yml` |

## 4. Evidence snapshot — 2026-10-24

| Check | Result |
|---|---|
| `govulncheck ./...` (Go 1.27.3) | **0 vulnerabilities** |
| `go vet ./...` | clean |
| `go test ./... -race` | green (2,262 tests, 149 packages) |
| gosec (v2, `./...`) | 210 findings: 29 high, ~120 medium, ~60 low |
| gosec high-severity review | all 29 traced to validated paths or benign conversions — see triage below |
| CI | build matrix (darwin/linux/windows), race tests, integration, Tier 3/4 live Linux, govulncheck |
| Release artifacts | per-archive CycloneDX 1.7 SBOMs + checksums; goreleaser v2 |
| Dependency policy | 12 direct dependencies; Dependabot + weekly govulncheck (failure opens an issue) |

### gosec high-severity triage

| Rule | Count | Assessment |
|---|---|---|
| G115 (int conversions) | 12 | Audio WAV (`int16↔uint16`) and QR matrix arithmetic. Bounded inputs; no security impact. |
| G703 (path traversal) | 11 | File tools call `CheckPathBoundary` before every read/write; config/checkpoint paths are operator-controlled. gosec cannot see the validator. |
| G404 (math/rand) | 3 | Retry jitter and A2A backoff only — no tokens, keys, or IDs. |
| G118 (context.Background in goroutine) | 3 | Deliberate detached work (upload handler, scheduler init, background tool) that must outlive the request. |
| G122 (filepath.Walk race) | 1 | Workspace walk; symlink traversal is constrained by the boundary check. Auditor should confirm. |

Medium/low findings are dominated by G104 (unchecked errors), G204
(subprocess arguments — inherent to the product), G304 (file paths from
config), G301/G306 (file modes for user-edited files), and G705/G120
(SSE/upload handlers with escaping and `MaxBytesReader` that gosec does
not trace). Reproduce with:

```bash
go install github.com/securego/gosec/v2/cmd/gosec@latest
gosec -fmt=json -out=gosec.json ./...
```

## 5. Known gaps (declared before the audit)

- External audit itself (this document is the preparation).
- Windows Tier 3/4 live validation (Linux automated; macOS manual).
- Home Assistant long-running soak/reconnect validation beyond unit +
  short live tests.
- Voice microphone hardware validation on a machine with an input device
  (the test host reports no audio input; loop/channel are covered by
  fake-source tests).
- Swarm/custom modes are newly selectable; their orchestration paths need
  adversarial testing.
- No bug bounty program yet.

## 6. Suggested audit workstreams

1. **Threat-model review** — validate trust boundaries above; focus on
   prompt injection → tool call escalation, channel identity spoofing,
   and MCP/A2A peer trust.
2. **Code review** — capability engine, tool dispatcher, shell parser,
   vault, webchat auth/upload, plugin sandbox, Home Assistant PIN path.
3. **Dynamic testing** — local webchat and API, OAuth/OIDC, file and
   archive handling, SSRF, model-output-driven tool use.
4. **Supply chain** — go.mod, SBOM correctness, release provenance,
   dependency review.
5. **Retest** — verify fixes; publish a summary report.

## 7. Access and logistics

- Repository: https://github.com/LumabyteCo/aibutler (Apache 2.0).
- Build: Go 1.27.3, `go build ./...`; no CGO.
- Test: `go test ./... -race`.
- Contact: `security@lumabyte.co`.
- Disclosure policy: [SECURITY.md](../../SECURITY.md).

Auditors should receive a private test instance with synthetic data —
never a production vault or real smart-home credentials.
