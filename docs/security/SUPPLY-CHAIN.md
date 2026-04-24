# Supply Chain Security

**Date:** 2026-04-05
**Scope:** Dependency management, build integrity, vulnerability scanning

## Overview

AI Butler follows supply chain security best practices to minimize risk
from third-party dependencies and build infrastructure compromise.

## Dependency Policy

### Minimal Dependencies

The project maintains a minimal dependency footprint:

- **No CGO** — eliminates C library supply chain risk
- **Go stdlib preferred** — cryptographic operations use `crypto/ecdsa`, `crypto/sha256`, etc.
- **Vendored dependencies** — all dependencies are committed to `go.sum` with cryptographic hashes
- **Direct dependencies reviewed** — each new dependency requires justification

### Current Direct Dependencies

| Dependency | Purpose | Risk Level |
|-----------|---------|------------|
| `filippo.io/age` | Encryption | Low (well-audited) |
| `github.com/ncruces/go-sqlite3` | Database (pure Go) | Low (no CGO) |
| `github.com/extism/go-sdk` | WASM plugin runtime | Medium (complex) |
| `golang.org/x/crypto` | Cryptographic primitives | Low (Go team) |
| `nhooyr.io/websocket` | WebSocket support | Low |
| `gopkg.in/yaml.v3` | YAML config parsing | Low |
| `mvdan.cc/sh/v3` | Shell parsing | Low |

## Automated Scanning

### GitHub Actions Workflow

The `.github/workflows/security.yml` workflow runs:

1. **`govulncheck`** — checks for known vulnerabilities in dependencies
2. **`go vet`** — static analysis for common issues
3. **Dependency review** — flags new dependencies in PRs
4. **Build verification** — ensures reproducible builds

### Running Locally

```bash
# Install govulncheck
go install golang.org/x/vuln/cmd/govulncheck@latest

# Scan for vulnerabilities
govulncheck ./...

# Verify dependency checksums
go mod verify

# Check for unused dependencies
go mod tidy -diff
```

## Build Integrity

### Reproducible Builds

- All builds use pinned Go versions (specified in `go.mod`)
- No build-time code generation that varies between environments
- CI builds match local builds

### Binary Verification

- Release binaries include version information via `-ldflags`
- SHA-256 checksums published alongside releases

## Incident Response

If a dependency vulnerability is discovered:

1. Check if the vulnerable code path is reachable (`govulncheck`)
2. If reachable, update the dependency immediately
3. If not reachable, schedule update for next release
4. Document the vulnerability and resolution in CHANGELOG

## SBOM

A Software Bill of Materials can be generated from `go.mod`:

```bash
go version -m ./aibutler
```

This lists all compiled-in dependencies with their exact versions.
