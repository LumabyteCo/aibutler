package applescript

// Test-only exports. Compiled solely into the test binary, so they don't
// widen the package's public API. They let the external _test package
// exercise the allowlist matcher directly on any OS — Execute() short-
// circuits with a macOS-only error on non-darwin before the allowlist
// check runs, which would otherwise hide allowlist regressions on Linux
// CI.

// InAllowlist exposes the unexported allowlist matcher for tests.
func (e *Executor) InAllowlist(script string) bool { return e.inAllowlist(script) }
