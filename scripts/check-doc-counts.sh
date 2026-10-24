#!/usr/bin/env bash
# Keep the README's "Key Numbers" honest. Runs in CI; fails when a count in
# the README no longer matches the tree.
set -euo pipefail

cd "$(dirname "$0")/.."

tests_raw=$(grep -rE '^func Test' --include='*_test.go' . | wc -l | tr -d ' ')
pkgs=$(go list ./... | wc -l | tr -d ' ')
tables=$(grep -h -i 'CREATE TABLE' internal/db/migrations/*.up.sql | wc -l | tr -d ' ')
migrations=$(ls internal/db/migrations/*.up.sql | wc -l | tr -d ' ')
deps=$(go list -m -f '{{if not .Indirect}}{{.Path}}{{end}}' all \
  | grep -v '^$' \
  | grep -v '^github.com/LumabyteCo/aibutler$' \
  | wc -l | tr -d ' ')
tests=$(python3 -c "print(format(${tests_raw}, ','))")

fail=0
check() {
  local label="$1" needle="$2"
  if ! grep -qF -- "$needle" README.md; then
    echo "::error::README is stale: expected ${label} — missing: ${needle}"
    fail=1
  fi
}

check "test count (hero)"       "${tests} passing tests"
check "test count (feature)"    "${tests} tests."
check "test count (table)"      "| Tests (passing, race-free) | **${tests}** |"
# Package discovery is platform-dependent (build constraints can hide a
# package on Linux even though it is part of the cross-platform project).
# Warn instead of failing CI; the authoritative counts are tests/schema/deps.
if ! grep -qF "| Go packages | **${pkgs}** |" README.md; then
  echo "::warning::README package count may be stale: this platform sees ${pkgs} packages"
fi
check "SQLite table count"      "| SQLite tables | **${tables}** |"
check "migration count"         "| Database migrations | **${migrations}** |"
check "direct dependency count" "| External Go dependencies | **${deps} direct** |"
check "schema prose"            "${tables} tables, ${migrations} migrations"

if [ "${fail}" -ne 0 ]; then
  echo ""
  echo "Update README.md (Key Numbers + hero/feature counts) and rerun:"
  echo "  tests=${tests_raw} packages=${pkgs} tables=${tables} migrations=${migrations} deps=${deps}"
  exit 1
fi

echo "README counts are accurate: tests=${tests_raw} packages=${pkgs} tables=${tables} migrations=${migrations} deps=${deps}"
