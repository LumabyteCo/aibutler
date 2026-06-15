package shortcuts

// Test-only export. Compiled solely into the test binary. Lets the
// external _test package exercise the allowlist matcher directly on any
// OS — Run() short-circuits with a macOS-only error on non-darwin before
// the allowlist check, which would otherwise hide allowlist regressions
// on Linux CI.

// InAllowlist exposes the unexported allowlist matcher for tests.
func (r *Runner) InAllowlist(name string) bool { return r.inAllowlist(name) }
