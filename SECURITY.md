# Security Policy

AI Butler takes security seriously. This document explains how to report a vulnerability, our current audit posture, and the security architecture you can rely on.

## Supported Versions

| Version | Supported |
|---------|-----------|
| v0.1.x  | ✅ |
| Older development builds | ❌ |

## Reporting a Vulnerability

**Please do not open a public GitHub issue for security-sensitive reports.**

Instead, email `security@lumabyte.co` with:

1. **Description** of the vulnerability
2. **Steps to reproduce** (or a working proof-of-concept)
3. **Impact** — who is affected and what they can exploit
4. **Suggested fix** (if any)
5. **Your name / handle** (so we can credit you in the advisory, unless you prefer to remain anonymous)

### Response timeline

| Severity | Acknowledgement | Initial triage | Fix target |
|---|---|---|---|
| **Critical** (RCE, auth bypass, data exfiltration) | Within 24h | Within 48h | Within 7 days |
| **High** (privilege escalation, injection) | Within 48h | Within 5 days | Within 14 days |
| **Medium** (XSS, CSRF, info disclosure) | Within 5 days | Within 10 days | Next release |
| **Low** (hardening, defense-in-depth) | Within 10 days | — | Best effort |

We coordinate disclosure timelines with reporters. Credit in the release notes is standard unless you request otherwise.

### Safe harbor

We will not pursue legal action against researchers who:

- Act in good faith and avoid privacy violations, data destruction, or service disruption
- Give us reasonable time to fix the issue before public disclosure
- Do not attempt to exploit the vulnerability beyond what is necessary to demonstrate it
- Do not access or modify data belonging to other users

## Security Architecture

AI Butler is built with a **security-first** architecture. Every tool call is gated, every external call is audited, every user input is validated.

### Capability engine

Every tool (`file.edit`, `shell.exec`, `iot.safety.control`, etc.) requires a granted capability at call time. Capabilities flow through context and cannot be forged. The capability engine logs every grant, denial, and tool invocation to a tamper-evident audit trail stored in SQLite.

### Role-based access control (RBAC)

Four built-in roles with different permission sets: `admin`, `user`, `viewer`, `agent`. Admins can create/manage users; regular users can execute tools; viewers get read-only access; agents (used for A2A peers) can only execute tools they've been explicitly granted.

### Authentication (beta in v0.1)

- **Password** — bcrypt with per-user salt
- **TOTP 2FA** — RFC 6238, compatible with any standard authenticator
- **OIDC SSO** — Auth0, Okta, Keycloak, Authelia, ZITADEL, Google Workspace
- **FIDO2 / WebAuthn** — hardware security keys (YubiKey, SoloKey, Apple Touch ID, etc.)

All auth methods are implemented and pass unit tests. Real-world validation in production deployments is the `beta` classification reason.

### Sandboxing

- **Shell commands** — allowlisted by command name, executed inside Linux `unshare` namespaces or macOS `sandbox-exec`. Denied commands get a clear error instead of a silent workaround.
- **File operations** — workspace boundary enforced on every path; symlink traversal blocked; absolute paths outside the workspace refused.
- **WASM plugins** — run in an Extism sandbox with zero filesystem, network, or syscall access by default. Every capability must be declared in the plugin manifest and granted by the user.
- **Subprocess bridges** — inherit the same shell sandbox as `shell.exec`.

### Network defense

- **SSRF protection** — HTTP proxy and browser tools block requests to private IP ranges (`10.0.0.0/8`, `172.16.0.0/12`, `192.168.0.0/16`, `127.0.0.0/8`, `169.254.0.0/16`, `fc00::/7`, and link-local IPv6)
- **Rate limiting** — per-IP on all external-facing endpoints (auth, A2A, webhooks)
- **Webhook signature verification** — Telegram secret token, Slack HMAC-SHA256, Discord Ed25519, GitHub HMAC-SHA256
- **TLS** — LAN mode uses self-signed certs with PIN pairing; internet mode uses Let's Encrypt `autocert` (or user-provided certs)

### Secrets management

- **Vault** — credentials stored in OS keyring (macOS Keychain, GNOME Keyring, Windows Credential Manager) with age encryption fallback
- **No secrets in config files** — all API keys flow through the vault
- **No default credentials** — first-run setup always requires explicit configuration

### Data at rest

- SQLite database stored with 0600 permissions in the user's data directory
- Backup manager supports age-encrypted snapshots
- Remote backup destinations (S3, HTTP PUT) support server-side encryption

### Input validation

- All webhook handlers enforce a 1 MB body limit
- File paths validated against workspace boundaries before any I/O
- Tool schemas enforced via JSON schema validation before execution
- Markdown rendering uses a safe HTML allow-list to prevent XSS

## Audit Posture

### Current (v0.1)

- **59 internal security audit passes** across the v0.1 development cycle. 74 findings were discovered across these passes; 70+ have been fixed. The remaining items are tracked internally and will be resolved before v1.0.
- **`govulncheck` clean** — zero known CVEs in dependencies as of the latest build. CI runs `govulncheck` on every commit.
- **`go vet ./...` clean** — zero vet warnings.
- **Integration tests** cover the full agent loop, capability enforcement, channel adapters, and auth flows.

### Planned (before v1.0)

- **External third-party security audit** — we'll publish the full report when complete
- **Bug bounty program** — scope, rewards, and rules published alongside the audit
- **SLSA Level 3 provenance** on all release binaries
- **cosign-signed binaries** with transparency log
- **CVE monitoring integration** for all dependencies

**Important:** The 59 audit passes are all internal. They are deep and thorough, but they are not a substitute for an external review. If you're deploying AI Butler in a production environment with sensitive data, please treat it as beta software until the external audit is complete.

## Threat Model

AI Butler is designed for these primary threats:

1. **Malicious tool inputs** — model-generated inputs are untrusted; every tool validates schemas and sanitizes outputs
2. **Compromised MCP servers or A2A peers** — external services can only access capabilities explicitly granted; all responses are validated
3. **Hostile web content** — browser tools run in a sandboxed context with SSRF protection
4. **Plugin supply chain attacks** — plugins are scanned on install for dangerous imports; sandbox prevents escape
5. **Credential theft** — secrets live in the OS keyring; never logged, never serialized to disk in plaintext
6. **Multi-tenant privilege escalation** — RBAC enforced at the tool-call level; no shared session state

### Out of scope (for now)

- **Physical access** to the host machine
- **Kernel or hypervisor compromise**
- **Model weights exfiltration** (use local models if this is a concern)
- **Side-channel attacks** (timing, cache, speculative execution)
- **Nation-state-level adversaries**

## Security Disclosures

Past advisories will be listed here. The repo is new; no advisories yet.

## References

- [`docs/security/`](docs/security/) — detailed security documentation
- [Security overview on docs.aibutler.dev](https://docs.aibutler.dev/security/model/)
