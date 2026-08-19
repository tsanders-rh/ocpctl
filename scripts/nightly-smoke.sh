#!/usr/bin/env bash
#
# nightly-smoke.sh — provision an AWS OpenShift SNO on the DEV environment via the
# public API, wait for it to reach READY, then destroy it. Used by the nightly
# pipeline (see docs/development/NIGHTLY_PIPELINE.md).
#
# Usage:
#   ./scripts/nightly-smoke.sh              # create -> poll until READY (exit 0), else exit 1
#   ./scripts/nightly-smoke.sh --teardown   # destroy the cluster recorded by the create run
#
# Env:
#   API             dev API base URL (default https://dev.ocpctl.mg.dog8code.com)
#   DEV_API_TOKEN   API key / bearer token with cluster create/read/delete (required)
#
# NOTE: the create-request JSON keys below are illustrative — confirm them against
# CreateClusterRequest (pkg/types) / internal/api/handler_clusters.go before enabling.
#
set -euo pipefail

API="${API:-https://dev.ocpctl.mg.dog8code.com}"
TOKEN="${DEV_API_TOKEN:?DEV_API_TOKEN is required}"
AUTH=(-H "Authorization: Bearer ${TOKEN}" -H "Content-Type: application/json")

PROFILE="${PROFILE:-aws-sno-ga}"
PLATFORM="${PLATFORM:-aws}"
REGION="${REGION:-us-east-1}"
TTL_HOURS="${TTL_HOURS:-2}"          # short TTL: janitor reaps a leaked cluster
POLL_ATTEMPTS="${POLL_ATTEMPTS:-90}" # 90 * 50s ≈ 75 min
POLL_INTERVAL="${POLL_INTERVAL:-50}"
STATE_FILE="${STATE_FILE:-.nightly-cluster-id}"

destroy() {
    [ -f "$STATE_FILE" ] || { echo "No cluster id recorded; nothing to destroy."; return 0; }
    local id; id="$(cat "$STATE_FILE")"
    [ -n "$id" ] || { echo "Empty cluster id; nothing to destroy."; return 0; }
    echo "Destroying cluster ${id}..."
    curl -fsS -X DELETE "${AUTH[@]}" "${API}/api/v1/clusters/${id}" >/dev/null || \
        echo "WARNING: destroy call failed; the ${TTL_HOURS}h TTL janitor will reap it."
    rm -f "$STATE_FILE"
}

if [ "${1:-}" = "--teardown" ]; then
    destroy
    exit 0
fi

NAME="nightly-$(date -u +%Y%m%d)"
echo "Creating ${PROFILE} cluster '${NAME}' on ${PLATFORM}/${REGION} (ttl=${TTL_HOURS}h)..."
ID="$(curl -fsS -X POST "${AUTH[@]}" "${API}/api/v1/clusters" -d "{
  \"name\": \"${NAME}\",
  \"profile\": \"${PROFILE}\",
  \"platform\": \"${PLATFORM}\",
  \"region\": \"${REGION}\",
  \"ttlHours\": ${TTL_HOURS},
  \"skipPostDeployment\": true
}" | jq -r '.id')"

if [ -z "$ID" ] || [ "$ID" = "null" ]; then
    echo "✗ create did not return a cluster id"
    exit 1
fi
echo "$ID" > "$STATE_FILE"
echo "Created cluster ${ID} (${NAME})"

for i in $(seq 1 "$POLL_ATTEMPTS"); do
    STATUS="$(curl -fsS "${AUTH[@]}" "${API}/api/v1/clusters/${ID}" | jq -r '.status')"
    echo "[$i/${POLL_ATTEMPTS}] status=${STATUS}"
    case "$STATUS" in
        READY)
            echo "✓ ${PROFILE} reached READY"
            exit 0
            ;;
        FAILED)
            echo "✗ cluster create FAILED — last 100 log lines:"
            curl -fsS "${AUTH[@]}" "${API}/api/v1/clusters/${ID}/logs" | tail -100 || true
            exit 1
            ;;
    esac
    sleep "$POLL_INTERVAL"
done

echo "✗ timed out waiting for READY after $((POLL_ATTEMPTS * POLL_INTERVAL))s"
exit 1
