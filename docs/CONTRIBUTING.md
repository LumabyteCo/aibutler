# Contributing to AI Butler

Contributions are welcome. This guide covers setup, code style, and the pull request process.

## Prerequisites

- Go 1.22+
- make

## Clone and Build

```bash
git clone https://github.com/LumabyteCo/aibutler.git
cd aibutler
make build
```

This produces a single `aibutler` binary (`CGO_ENABLED=0`, no C compiler needed).

## Run Tests

```bash
make test             # All unit tests
make test-race        # Unit tests with race detector
make test-integration # Integration tests (requires `-tags=integration`)
make test-all         # Race + integration
make lint             # go vet
make check            # lint + test-race
```

All tests must pass with `-race` before submitting a PR.

## Project Structure

```
internal/
  agent/       Agent core loop, 6-state lifecycle, nesting, recovery
  capability/  Capability engine (permission model for agents)
  channel/     Channel abstraction and registry
  cli/         CLI app bootstrap, commands, version
  config/      Three Enriches config system (Settings/Configurations/Options)
  db/          SQLite database, schema, migrations, backup, integrity
  discord/     Discord channel adapter
  finance/     Finance tools
  i18n/        Internationalization (14 languages)
  iot/         IoT foundation (Home Assistant, stub adapter)
  mcp/         MCP client protocol
  media/       Media processing pipeline
  memory/      Living Memory (FTS + entities + graph + vectors + hybrid)
  prompt/      Prompt composer, cost tracker, token economy
  proxy/       Resource Access Proxy
  schedule/    Scheduled agents (cron-like)
  session/     Session and conversation management
  shell/       Shell execution (allowlist/denylist security)
  slack/       Slack channel adapter
  stopphrase/  Stop phrase detection
  telegram/    Telegram channel adapter
  tool/        Tool framework and registry
  vault/       Credential vault (keychain, age files, env vars)
  voice/       Voice pipeline (STT/TTS)
  webchat/     WebChat channel adapter
```

## Code Style

- Follow standard Go conventions.
- Run `go vet ./...` before committing.
- Keep functions short and testable.
- Exported types need doc comments.
- Error messages start lowercase, no trailing punctuation: `fmt.Errorf("open database: %w", err)`.

## Adding Tests

- Place test files next to the code they test: `foo.go` and `foo_test.go` in the same package.
- Use table-driven tests where applicable.
- Integration tests use the `integration` build tag: `//go:build integration`.
- Use `:memory:` databases for unit tests (`db.Config{Path: ":memory:"}`).

## Writing Documentation

- Docs live in `docs/`.
- Style: examples first, concise, copy-pasteable commands.
- No marketing language. Document what exists, not what is planned.

## Pull Request Process

1. Fork the repository and create a feature branch.
2. Make your changes with tests.
3. Run `make check` -- all tests and linting must pass.
4. Commit with a clear message describing the change.
5. Open a PR against `main` with:
   - What the change does
   - How to test it
   - Any config changes or migration notes
6. Address review feedback.

## Reporting Security Vulnerabilities

Report security issues privately -- do **not** open public issues for vulnerabilities. See [SECURITY.md](../SECURITY.md) for the project's security model and contact information.

## License

By contributing, you agree that your contributions will be licensed under the same terms as the project.
