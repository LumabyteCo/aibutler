<!--
Thanks for contributing to AI Butler!

Before you submit, please:
1. Run `go test ./... -race -count=1` locally — all tests must pass
2. Run `go vet ./...` — no warnings
3. If you added a new package, make sure it's instantiated in
   `internal/cli/app.go` and wired into `internal/cli/cmd_run.go`
-->

## What this changes

<!-- One short paragraph. "Why" matters more than "what" — the diff shows the what. -->

## Type of change

<!-- Delete the lines that don't apply. -->

- [ ] Bug fix — existing behavior was wrong
- [ ] New feature — capability the project didn't have before
- [ ] Refactor — same behavior, cleaner code
- [ ] Docs — README / docs / comments only
- [ ] Test — added or improved tests only
- [ ] CI / tooling — build, lint, dependencies
- [ ] Breaking change — existing users / configs / APIs need to change

## Linked issues

<!-- `Closes #123`, `Fixes #456`, `Refs #789` -->

## How you tested this

<!--
Not just "tests pass". Describe what behavior you exercised.
For integration-shaped changes, include the commands you ran and what
you observed. For bug fixes, describe the reproduction and confirm
the fix.
-->

## Checklist

- [ ] `go test ./... -race -count=1` passes locally
- [ ] `go vet ./...` has no warnings
- [ ] New code is wired into the app bootstrap (`cli/app.go`) if it's a new package
- [ ] Tests added or updated (and they actually cover the change, not just happy path)
- [ ] Docs updated — README, CHANGELOG, or relevant feature docs as applicable
- [ ] Honest beta labels preserved — nothing was silently promoted from `[beta]` to `[ready]` without real-world validation
- [ ] No secrets in the diff (API keys, tokens, personal paths)
- [ ] Commit message explains the *why*

## Anything else reviewers should know

<!--
Tricky design decisions, alternatives you considered and rejected,
follow-up work you deferred, etc.
-->
