#!/usr/bin/env bash
#
# Per-package coverage floors for security-critical packages (issue #102).
#
# We deliberately do NOT gate the repo-wide coverage number. The bulk of the
# codebase (internal/worker, internal/api, internal/store, internal/installer)
# is integration-shaped — it shells out to openshift-install/eksctl/gcloud,
# talks to a real database, and drives long-running cloud provisioning — so it
# is not meaningfully unit-testable. A repo-wide floor would therefore be
# pinned at a misleadingly low value and would punish adding un-unit-testable
# code. Instead we enforce a floor on each package where a bug has real blast
# radius (input validation, secret handling, resource teardown, orphan
# reaping) and ratchet the floors upward as coverage improves.
#
# To add a package: append a "path:floor" entry to FLOORS. To raise a floor:
# bump the number once the package clears it. Never lower a floor to make CI
# pass — fix the tests instead.
#
# Usage: scripts/check-coverage.sh   (run from repo root or anywhere)

set -euo pipefail

cd "$(dirname "$0")/.."

# pkg:floor (minimum statement coverage %, integer).
# PR2 will add: internal/aws/cleanup:70  internal/janitor:60
# PR3 will add: internal/api/middleware, internal/poolscheduler, internal/auth (full)
FLOORS=(
  "internal/validation:90"
  "internal/secrets:65"
)

profile="$(mktemp)"
trap 'rm -f "$profile"' EXIT

fail=0
for entry in "${FLOORS[@]}"; do
  pkg="${entry%%:*}"
  floor="${entry##*:}"

  if ! out=$(go test -covermode=atomic -coverprofile="$profile" "./${pkg}/" 2>&1); then
    echo "FAIL ${pkg}: tests did not pass"
    echo "${out}"
    fail=1
    continue
  fi

  pct=$(go tool cover -func="$profile" | awk '/^total:/ {gsub(/%/,"",$NF); print $NF}')
  if [ -z "${pct}" ]; then
    echo "FAIL ${pkg}: no coverage reported (no statements or no tests?)"
    fail=1
    continue
  fi

  # Float-safe comparison via awk (bash can't compare decimals).
  if awk "BEGIN{exit !(${pct} < ${floor})}"; then
    echo "FAIL ${pkg}: coverage ${pct}% is below floor ${floor}%"
    fail=1
  else
    echo "ok   ${pkg}: coverage ${pct}% (floor ${floor}%)"
  fi
done

if [ "${fail}" -ne 0 ]; then
  echo ""
  echo "Coverage floor(s) not met. Add tests to raise coverage, or see"
  echo "scripts/check-coverage.sh for the rationale behind these gates."
  exit 1
fi

echo ""
echo "All per-package coverage floors met."
