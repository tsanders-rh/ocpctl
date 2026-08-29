#!/bin/bash
# Refresh the registry.ci.openshift.org auth in the worker pull secret.
#
# WHY: OpenShift *nightly* builds (e.g. 4.23.0-0.nightly-2026-08-25-215343) live
# ONLY in registry.ci.openshift.org, which requires a personal CI token that
# EXPIRES ROUGHLY MONTHLY. When it lapses, nightly creates fail with an
# actionable error from the worker ("pull secret is missing
# registry.ci.openshift.org auth ...") and `oc adm release extract` in
# download-specific-version.sh starts returning auth errors. This script
# re-merges a freshly-minted registry.ci auth into the worker.env pull secret(s)
# so a subsequent deploy ships it to the static hosts and (via S3) to autoscale
# workers on next boot.
#
# It NEVER prints secret material.
#
# ── How to mint a fresh registry.ci token ────────────────────────────────────
#   1. Log in to the CI cluster (app.ci): open
#        https://console.redhat.com/openshift/  ->  the OpenShift CI cluster
#      or use the "Copy login command" from the CI OAuth page:
#        https://oauth-openshift.apps.ci.l2s4.p1.openshiftapps.com/oauth/token/request
#      then run the printed `oc login --token=... --server=https://api.ci...:6443`.
#   2. Write a docker-config containing the registry.ci auth:
#        oc registry login --to=/tmp/ci-auth.json
#   3. Run this script pointing at that file:
#        ./scripts/refresh-ci-pull-secret.sh /tmp/ci-auth.json
#   4. Deploy so workers pick it up:
#        ./scripts/deploy.sh            # prod (static hosts + S3 for the ASG)
#        ./scripts/deploy-env.sh dev    # dev
#      deploy.sh terminates running autoscale workers so the ASG relaunches them
#      with the refreshed secret.
#
# Then delete /tmp/ci-auth.json.

set -euo pipefail

REGISTRY_HOST="registry.ci.openshift.org"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"

# Env files that carry the worker pull secret. worker.env and
# worker.env.production are the source of truth deployed to prod hosts + S3;
# worker.env.dev feeds the dev environment.
ENV_FILES=(
    "${REPO_ROOT}/config/worker.env"
    "${REPO_ROOT}/config/worker.env.production"
    "${REPO_ROOT}/config/worker.env.dev"
)

CI_AUTH_FILE="${1:-}"

err()  { echo "ERROR: $*" >&2; }
info() { echo "[refresh-ci-pull-secret] $*"; }

if ! command -v jq >/dev/null 2>&1; then
    err "jq is required"
    exit 1
fi

if [ -z "$CI_AUTH_FILE" ]; then
    err "usage: $0 <ci-auth-file>"
    err "  <ci-auth-file> is a docker-config JSON containing ${REGISTRY_HOST} auth,"
    err "  e.g. produced by:  oc registry login --to=/tmp/ci-auth.json"
    err "  (see the header of this script for how to mint a fresh CI token)"
    exit 1
fi

if [ ! -f "$CI_AUTH_FILE" ]; then
    err "CI auth file not found: $CI_AUTH_FILE"
    exit 1
fi

# Validate the CI auth file actually contains a registry.ci entry (without
# printing it).
if ! jq -e --arg h "$REGISTRY_HOST" '.auths[$h].auth // .auths[$h].token' "$CI_AUTH_FILE" >/dev/null 2>&1; then
    err "$CI_AUTH_FILE has no auth entry for ${REGISTRY_HOST}"
    err "regenerate it with: oc registry login --to=$CI_AUTH_FILE (while logged in to app.ci)"
    exit 1
fi

info "Validated ${REGISTRY_HOST} auth is present in ${CI_AUTH_FILE##*/}"

# merge_into_env_file rewrites OPENSHIFT_PULL_SECRET in $1, adding/replacing the
# registry.ci auth from the CI auth file. Never echoes the secret.
merge_into_env_file() {
    local env_file=$1
    if [ ! -f "$env_file" ]; then
        info "skip (not present): ${env_file}"
        return 0
    fi
    if ! grep -q "^OPENSHIFT_PULL_SECRET=" "$env_file"; then
        info "skip (no OPENSHIFT_PULL_SECRET): ${env_file}"
        return 0
    fi

    local current new tmp_line
    # Strip the KEY=, then surrounding single quotes.
    current=$(grep -m1 "^OPENSHIFT_PULL_SECRET=" "$env_file" | sed "s/^OPENSHIFT_PULL_SECRET=//; s/^'//; s/'\$//")

    if ! printf '%s' "$current" | jq -e '.auths' >/dev/null 2>&1; then
        err "existing OPENSHIFT_PULL_SECRET in ${env_file} is not valid JSON with .auths — leaving unchanged"
        return 1
    fi

    # Merge the single registry.ci auth object into the existing auths map.
    new=$(printf '%s' "$current" | jq -c \
        --slurpfile ci "$CI_AUTH_FILE" \
        --arg h "$REGISTRY_HOST" \
        '.auths[$h] = ($ci[0].auths[$h])')

    # Sanity check the merge landed.
    if ! printf '%s' "$new" | jq -e --arg h "$REGISTRY_HOST" '.auths[$h].auth // .auths[$h].token' >/dev/null 2>&1; then
        err "merge failed to add ${REGISTRY_HOST} to ${env_file} — leaving unchanged"
        return 1
    fi

    tmp_line=$(mktemp)
    # Preserve the exact single-quoted one-line format used by the env files.
    printf "OPENSHIFT_PULL_SECRET='%s'\n" "$new" > "$tmp_line"

    # Replace the line in place, reading the (secret) replacement from a file so
    # it never appears on a command line.
    awk -v repl_file="$tmp_line" '
        /^OPENSHIFT_PULL_SECRET=/ { while ((getline line < repl_file) > 0) print line; next }
        { print }
    ' "$env_file" > "${env_file}.new"

    mv "${env_file}.new" "$env_file"
    rm -f "$tmp_line"
    info "updated ${REGISTRY_HOST} auth in ${env_file##*/}"
}

status=0
for f in "${ENV_FILES[@]}"; do
    merge_into_env_file "$f" || status=1
done

echo ""
if [ "$status" -ne 0 ]; then
    err "one or more env files could not be updated — review the messages above"
    exit 1
fi

info "Done. registry.ci auth refreshed in worker env files."
echo ""
echo "Next steps:"
echo "  1. Review the (secret) diff locally — do NOT commit worker.env* (they're gitignored)."
echo "  2. Deploy so workers pick it up:"
echo "       ./scripts/deploy.sh          # prod hosts + S3 (ASG self-heals on next boot)"
echo "       ./scripts/deploy-env.sh dev  # dev"
echo "  3. rm -f ${CI_AUTH_FILE}"
