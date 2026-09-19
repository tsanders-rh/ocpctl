#!/bin/bash
#
# handover-bundle.sh — push/pull the untracked files a maintainer needs.
#
# Everything ocpctl needs to deploy that is NOT in git lives in a handful of
# places on one maintainer's laptop. This script moves that set to a team-owned,
# KMS-encrypted S3 bucket so ownership survives any one person leaving.
#
# It deliberately does NOT use the binaries bucket: every worker instance role
# can read s3://ocpctl-binaries, and this bundle is broader than worker.env.
#
#   ./scripts/handover-bundle.sh push          # collect local files -> S3
#   ./scripts/handover-bundle.sh pull          # S3 -> local (new maintainer)
#   ./scripts/handover-bundle.sh list          # what's in the bucket
#   ./scripts/handover-bundle.sh verify        # check local set without uploading
#
# See docs/operations/OWNERSHIP_HANDOVER.md for what each file is and who owns it.

set -euo pipefail

BUCKET="${OCPCTL_HANDOVER_BUCKET:-s3://ocpctl-handover-346869059911}"
KMS_KEY="${OCPCTL_HANDOVER_KMS_KEY:-alias/ocpctl-handover}"
REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
SSH_DIR="${HOME}/.ssh"

# Files relative to the repo root.
REPO_FILES=(
  ".env"
  "CLAUDE.local.md"
  "config/api.env.dev"
  "config/api.env.production"
  "config/worker.env"
  "config/worker.env.dev"
  "config/worker.env.production"
  "config/web.env.dev" "config/web.env.production"
  "terraform/dev/terraform.tfvars"
  "terraform/worker-autoscaling/terraform.tfvars"
)

# SSH private keys, taken from ~/.ssh and stored under ssh/ in the bundle.
SSH_FILES=(
  "ocpctl-production-key"
  "ocpctl-dev-key"
)

log()  { printf '%s\n' "$*"; }
warn() { printf 'WARN: %s\n' "$*" >&2; }
die()  { printf 'ERROR: %s\n' "$*" >&2; exit 1; }

require_aws() {
  command -v aws >/dev/null || die "aws CLI not found"
  aws sts get-caller-identity >/dev/null 2>&1 || die "no valid AWS credentials"
}

# Terraform state is NOT bundled: it lives in the remote S3 backend
# (s3://ocpctl-tfstate-346869059911). Warn loudly if a stale local copy exists,
# because that means someone is running Terraform without the shared backend.
check_no_local_state() {
  local found=0
  for d in dev worker-autoscaling; do
    if [[ -f "$REPO_ROOT/terraform/$d/terraform.tfstate" ]]; then
      warn "local terraform/$d/terraform.tfstate exists — the shared backend is authoritative."
      warn "  run: terraform -chdir=terraform/$d init -migrate-state"
      found=1
    fi
  done
  return $found
}

cmd_verify() {
  local missing=0
  log "Repo files:"
  for f in "${REPO_FILES[@]}"; do
    if [[ -f "$REPO_ROOT/$f" ]]; then
      log "  ok      $f"
    else
      log "  MISSING $f"
      missing=$((missing + 1))
    fi
  done
  log "SSH keys:"
  for k in "${SSH_FILES[@]}"; do
    if [[ -f "$SSH_DIR/$k" ]]; then
      log "  ok      ~/.ssh/$k"
    else
      log "  MISSING ~/.ssh/$k"
      missing=$((missing + 1))
    fi
  done
  check_no_local_state || true
  [[ $missing -eq 0 ]] || warn "$missing file(s) missing — bundle would be incomplete."
  return 0
}

cmd_push() {
  require_aws
  cmd_verify
  local stamp prefix
  stamp="$(date -u +%Y%m%dT%H%M%SZ)"
  prefix="$BUCKET/bundles/$stamp"

  for f in "${REPO_FILES[@]}"; do
    [[ -f "$REPO_ROOT/$f" ]] || continue
    aws s3 cp "$REPO_ROOT/$f" "$prefix/repo/$f" \
      --sse aws:kms --sse-kms-key-id "$KMS_KEY" --only-show-errors
    log "pushed $f"
  done
  for k in "${SSH_FILES[@]}"; do
    [[ -f "$SSH_DIR/$k" ]] || continue
    aws s3 cp "$SSH_DIR/$k" "$prefix/ssh/$k" \
      --sse aws:kms --sse-kms-key-id "$KMS_KEY" --only-show-errors
    log "pushed ~/.ssh/$k"
  done

  # Manifest records who pushed what, so the next owner can tell staleness.
  local manifest
  manifest="$(mktemp)"
  {
    echo "bundle:    $stamp"
    echo "pushed_by: $(aws sts get-caller-identity --query Arn --output text)"
    echo "host:      $(hostname)"
    echo "git_head:  $(git -C "$REPO_ROOT" rev-parse --short HEAD 2>/dev/null || echo unknown)"
    echo "files:"
    for f in "${REPO_FILES[@]}"; do
      [[ -f "$REPO_ROOT/$f" ]] && echo "  - repo/$f"
    done
    for k in "${SSH_FILES[@]}"; do
      [[ -f "$SSH_DIR/$k" ]] && echo "  - ssh/$k"
    done
  } > "$manifest"
  aws s3 cp "$manifest" "$prefix/MANIFEST.txt" \
    --sse aws:kms --sse-kms-key-id "$KMS_KEY" --only-show-errors
  rm -f "$manifest"

  # LATEST pointer mirrors the deploy-artifact convention used for binaries.
  printf '%s\n' "$stamp" | aws s3 cp - "$BUCKET/LATEST" \
    --sse aws:kms --sse-kms-key-id "$KMS_KEY" --only-show-errors

  log ""
  log "Bundle pushed: $prefix"
  log "Pull with: ./scripts/handover-bundle.sh pull"
}

cmd_pull() {
  require_aws
  local stamp="${1:-}"
  if [[ -z "$stamp" ]]; then
    stamp="$(aws s3 cp "$BUCKET/LATEST" - 2>/dev/null | tr -d '[:space:]')"
    [[ -n "$stamp" ]] || die "no LATEST pointer in $BUCKET — pass a bundle id explicitly"
  fi
  local prefix="$BUCKET/bundles/$stamp"
  log "Pulling bundle $stamp"

  aws s3 cp "$prefix/MANIFEST.txt" - 2>/dev/null || die "bundle $stamp not found"

  for f in "${REPO_FILES[@]}"; do
    if aws s3 ls "$prefix/repo/$f" >/dev/null 2>&1; then
      mkdir -p "$(dirname "$REPO_ROOT/$f")"
      aws s3 cp "$prefix/repo/$f" "$REPO_ROOT/$f" --only-show-errors
      chmod 600 "$REPO_ROOT/$f"
      log "pulled $f"
    fi
  done
  mkdir -p "$SSH_DIR"
  for k in "${SSH_FILES[@]}"; do
    if aws s3 ls "$prefix/ssh/$k" >/dev/null 2>&1; then
      aws s3 cp "$prefix/ssh/$k" "$SSH_DIR/$k" --only-show-errors
      chmod 600 "$SSH_DIR/$k"
      log "pulled ~/.ssh/$k"
    fi
  done

  log ""
  log "Done. Terraform state is NOT in this bundle — it is in the remote backend:"
  log "  terraform -chdir=terraform/dev init"
  log "  terraform -chdir=terraform/worker-autoscaling init"
}

cmd_list() {
  require_aws
  log "LATEST: $(aws s3 cp "$BUCKET/LATEST" - 2>/dev/null | tr -d '[:space:]' || echo none)"
  aws s3 ls "$BUCKET/bundles/"
}

case "${1:-}" in
  push)   cmd_push ;;
  pull)   shift; cmd_pull "${1:-}" ;;
  list)   cmd_list ;;
  verify) cmd_verify ;;
  *)      die "usage: $0 {push|pull [bundle-id]|list|verify}" ;;
esac
