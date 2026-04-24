# Security Model

## Quick Example

```
Agent wants to run: shell "npm test"
  1. Find grant      -> tool.shell.exec found
  2. Check scope     -> Commands: ["npm test"] -- matches
  3. Check rate limit -> 10/hour, 3 used -- OK
  4. Check TTL       -> Granted 2h ago, TTL 4h -- OK
  5. Check max calls -> 50 total, 12 used -- OK
  6. Audit log       -> AuditFull: write entry to resource_access_log
  7. Allow           -> Execute command
```

## Capability Engine

Every access goes through `capability.Engine.Check()`. A grant specifies:

- **Resource**: What (`tool.shell.exec`, `iot.sensor.read`) -- wildcards supported (`tool.lsp.*`)
- **Scope**: Paths (glob), commands (exact), domains (suffix `*.github.com`), channels, devices
- **Limits**: Rate limit (sliding window), TTL (auto-expiry), MaxCalls (lifetime cap)
- **Safety**: `RequiresConfirmation`, `RequiresPIN`, `SafetyBounds`
- **Audit**: `None`, `Summary` (denials only), `Full` (every check)

## 7-Step Resolution

1. **Find grant** -- Match resource (exact or wildcard)
2. **Check scope** -- Path (glob, rejects `..`), command, domain, channel, device
3. **Rate limit** -- Sliding window with timestamp tracking
4. **TTL** -- `GrantedAt + TTL` vs now
5. **MaxCalls** -- Lifetime counter
6. **Audit** -- Full logs all, Summary logs denials
7. **Allow** -- Return result with safety flags

Denials: `no_capability`, `scope_denied`, `rate_limited`, `ttl_expired`, `max_calls_exceeded`.

## Credential Vault

Three backends, auto-selected by priority:

| Backend    | When used                          | Encryption          |
|------------|------------------------------------|---------------------|
| Keychain   | Default (macOS/Linux keyring)      | OS-managed          |
| File       | Passphrase provided or ForceFile   | age (scrypt)        |
| Env        | ForceEnv flag or last resort       | None (env vars)     |

10 credential types supported. Operations: `Store`, `Get`, `Delete`, `List`, `Has`, `HealthCheck`. Secrets zeroed after use via `vault.ZeroBytes()`.

## Resource Access Proxy

The `proxy.Proxy` wraps every external HTTP call in a 5-step pipeline:

1. **Capability check** -- `tool.web.fetch` with domain scope
2. **Credential resolve** -- Look up service config + credential by domain
3. **Token refresh** -- If OAuth2 token expires within 5 minutes, refresh automatically
4. **Execute** -- HTTP request with injected auth headers
5. **Audit** -- Log success/failure to resource_access_log

## Shell Security

Default mode: `allowlist` (empty -- nothing allowed until configured). Commands match exactly (no prefix). Path scope rejects `..`.

## Source Files

- `internal/capability/engine.go` -- Engine, CapabilitySet, Check()
- `internal/capability/capability.go` -- Capability struct, AuditLevel, RateLimit
- `internal/capability/scope.go` -- matchPath, matchCommand, matchDomain, matchChannel, matchDevice
- `internal/vault/vault.go` -- Vault interface, backend selection
- `internal/proxy/proxy.go` -- Resource Access Proxy pipeline
