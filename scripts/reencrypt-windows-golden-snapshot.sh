#!/bin/bash
#
# Replace an unencrypted Windows golden EBS snapshot with an encrypted copy.
#
# Why this exists
# ---------------
# Windows VM disks are CSI-restored straight from the golden snapshot into a
# StorageClass that sets `encrypted: "true"`. If the golden snapshot is
# unencrypted, EBS re-encrypts every block during the restore, which severs the
# incremental-snapshot lineage — so the FIRST CSI/Velero backup of every VM is a
# full ~70 GiB snapshot (~68 minutes) instead of an incremental one.
#   https://docs.aws.amazon.com/ebs/latest/userguide/how_snapshots_work.html
#
# The worker now passes --encrypted when it creates golden snapshots, but that
# is not retroactive. This script fixes snapshots created before that change.
#
# What it does
# ------------
#   1. Resolves the golden snapshot for a region from SSM.
#   2. Exits early if it is already encrypted (safe to re-run).
#   3. Makes a same-region `copy-snapshot --encrypted` (default EBS KMS key —
#      the same key the CSI StorageClass uses), or adopts one already in flight.
#   4. Waits for the copy, tags it like the original.
#   5. Re-points the SSM parameter at the new snapshot.
#
# Expect this to take roughly an hour. A same-region copy is normally a cheap
# metadata operation, but this one *changes encryption state*, so every block
# has to be read, re-encrypted and rewritten — the same full-copy physics that
# causes the bug being fixed. Observed ~14% after 11 minutes for 70 GiB.
#
# The wait is resumable: if the copy is still running when you interrupt it, or
# you re-run after a timeout, the script finds the in-flight copy by description
# and adopts it rather than starting a second one.
#
# The old snapshot is left in place. Delete it yourself once you are satisfied,
# and only after checking no live volume still depends on it.
#
# Usage:
#   scripts/reencrypt-windows-golden-snapshot.sh --region us-east-1 [--version 1.0] [--dry-run]
#

set -euo pipefail

REGION=""
SNAPSHOT_VERSION="1.0"
DRY_RUN=false

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

log_info()  { echo -e "${GREEN}[INFO]${NC} $1"; }
log_warn()  { echo -e "${YELLOW}[WARN]${NC} $1"; }
log_error() { echo -e "${RED}[ERROR]${NC} $1"; }

usage() {
    cat <<EOF
Usage: $0 --region <aws-region> [--version <version>] [--dry-run]

  --region    AWS region holding the golden snapshot (required)
  --version   Snapshot image version (default: 1.0)
  --dry-run   Show what would happen without creating or re-pointing anything
EOF
    exit 1
}

while [[ $# -gt 0 ]]; do
    case "$1" in
        --region)  REGION="${2:-}"; shift 2 ;;
        --version) SNAPSHOT_VERSION="${2:-}"; shift 2 ;;
        --dry-run) DRY_RUN=true; shift ;;
        -h|--help) usage ;;
        *) log_error "Unknown argument: $1"; usage ;;
    esac
done

[ -n "$REGION" ] || { log_error "--region is required"; usage; }

SSM_PARAM="/ocpctl/windows-snapshots/${SNAPSHOT_VERSION}/${REGION}"

log_info "═══════════════════════════════════════════════════════════════"
log_info "Re-encrypt Windows golden snapshot"
log_info "Region:  $REGION"
log_info "Version: $SNAPSHOT_VERSION"
log_info "SSM:     $SSM_PARAM"
[ "$DRY_RUN" = true ] && log_warn "DRY RUN - no changes will be made"
log_info "═══════════════════════════════════════════════════════════════"

# 1. Resolve the current golden snapshot
OLD_SNAPSHOT=$(aws ssm get-parameter --name "$SSM_PARAM" --region "$REGION" \
    --query 'Parameter.Value' --output text 2>/dev/null || echo "")

if [ -z "$OLD_SNAPSHOT" ] || [ "$OLD_SNAPSHOT" = "None" ]; then
    log_error "No golden snapshot registered at $SSM_PARAM"
    log_error "Nothing to remediate. Create one via the Windows Snapshot Management UI."
    exit 1
fi

log_info "Current golden snapshot: $OLD_SNAPSHOT"

# 2. Check encryption state — this script is a no-op if it is already fixed
read -r ENCRYPTED STATE VOLUME_SIZE <<<"$(aws ec2 describe-snapshots \
    --snapshot-ids "$OLD_SNAPSHOT" --region "$REGION" \
    --query 'Snapshots[0].[Encrypted,State,VolumeSize]' --output text)"

log_info "  Encrypted: $ENCRYPTED | State: $STATE | Size: ${VOLUME_SIZE} GiB"

if [ "$STATE" != "completed" ]; then
    log_error "Snapshot is in state '$STATE'; refusing to copy from it"
    exit 1
fi

if [ "$ENCRYPTED" = "True" ] || [ "$ENCRYPTED" = "true" ]; then
    log_info "✓ Golden snapshot is already encrypted - nothing to do"
    exit 0
fi

log_warn "Golden snapshot is unencrypted - every VM restored from it pays a"
log_warn "full-snapshot penalty on its first CSI backup."

if [ "$DRY_RUN" = true ]; then
    log_info "[dry-run] Would run: aws ec2 copy-snapshot --encrypted \\"
    log_info "[dry-run]   --source-snapshot-id $OLD_SNAPSHOT --source-region $REGION \\"
    log_info "[dry-run]   --destination-region $REGION --region $REGION"
    log_info "[dry-run] Would wait ~1 hour (encryption change forces a full block copy)"
    log_info "[dry-run] Would re-point $SSM_PARAM at the new snapshot"
    log_info "[dry-run] Would leave $OLD_SNAPSHOT in place for manual deletion"
    exit 0
fi

# 3. Same-region encrypted copy. No --kms-key-id: that selects the region's
#    default EBS key, which is exactly what `encrypted: "true"` gives the
#    restored volume, so the lineage is preserved on restore.
COPY_DESCRIPTION="Windows 10 OADP v${SNAPSHOT_VERSION} (encrypted copy of ${OLD_SNAPSHOT})"

# Adopt an in-flight or finished copy from an earlier interrupted run rather
# than kicking off a second hour-long copy of the same data.
NEW_SNAPSHOT=$(aws ec2 describe-snapshots --owner-ids self --region "$REGION" \
    --filters "Name=description,Values=${COPY_DESCRIPTION}" \
              "Name=status,Values=pending,completed" \
    --query 'reverse(sort_by(Snapshots, &StartTime))[0].SnapshotId' \
    --output text 2>/dev/null || echo "None")

if [ -n "$NEW_SNAPSHOT" ] && [ "$NEW_SNAPSHOT" != "None" ]; then
    log_info "Adopting existing encrypted copy from an earlier run: $NEW_SNAPSHOT"
else
    log_info "Creating encrypted copy (default EBS KMS key)..."
    NEW_SNAPSHOT=$(aws ec2 copy-snapshot \
        --source-region "$REGION" \
        --source-snapshot-id "$OLD_SNAPSHOT" \
        --destination-region "$REGION" \
        --region "$REGION" \
        --encrypted \
        --description "$COPY_DESCRIPTION" \
        --query 'SnapshotId' --output text)

    log_info "✓ Copy started: $NEW_SNAPSHOT"
fi

# 4. Wait for completion. Changing encryption state forces a full block-by-block
#    copy, so budget ~an hour for 70 GiB rather than the minutes a same-encryption
#    same-region copy would take.
log_info "Waiting for copy to complete (expect ~1 hour for 70 GiB)..."
DEADLINE=$(( $(date +%s) + 7200 ))
while [ "$(date +%s)" -lt "$DEADLINE" ]; do
    read -r COPY_STATE COPY_PROGRESS <<<"$(aws ec2 describe-snapshots \
        --snapshot-ids "$NEW_SNAPSHOT" --region "$REGION" \
        --query 'Snapshots[0].[State,Progress]' --output text)"

    if [ "$COPY_STATE" = "completed" ]; then
        log_info "✓ Copy completed: $NEW_SNAPSHOT"
        break
    elif [ "$COPY_STATE" = "error" ]; then
        log_error "Copy failed (state: error)"
        exit 1
    fi

    log_info "  Progress: $COPY_PROGRESS (state: $COPY_STATE)"
    sleep 60
done

if [ "$COPY_STATE" != "completed" ]; then
    log_error "Timed out waiting for copy to complete"
    log_error "Snapshot $NEW_SNAPSHOT may still be in progress; SSM was NOT re-pointed"
    exit 1
fi

# Confirm the copy actually came out encrypted before we advertise it
NEW_ENCRYPTED=$(aws ec2 describe-snapshots --snapshot-ids "$NEW_SNAPSHOT" --region "$REGION" \
    --query 'Snapshots[0].Encrypted' --output text)

if [ "$NEW_ENCRYPTED" != "True" ] && [ "$NEW_ENCRYPTED" != "true" ]; then
    log_error "Copy $NEW_SNAPSHOT is not encrypted; refusing to re-point SSM"
    exit 1
fi

log_info "✓ Verified new snapshot is encrypted"

# 5. Tag it the same way the worker tags golden snapshots, so the EC2-tag
#    discovery fallback finds it too.
aws ec2 create-tags --region "$REGION" --resources "$NEW_SNAPSHOT" --tags \
    "Key=Name,Value=ocpctl-windows-10-oadp-v${SNAPSHOT_VERSION}" \
    "Key=ocpctl:managed,Value=true" \
    "Key=ocpctl:image-version,Value=${SNAPSHOT_VERSION}" \
    "Key=ocpctl:region,Value=${REGION}" \
    "Key=ocpctl:validated,Value=true" \
    "Key=ocpctl:persistent,Value=true" \
    "Key=ocpctl:source-snapshot,Value=${OLD_SNAPSHOT}" \
    "Key=ocpctl:creation-method,Value=encrypted-recopy" \
    || log_warn "Could not tag $NEW_SNAPSHOT"

log_info "✓ Tagged $NEW_SNAPSHOT"

# 6. Re-point SSM
aws ssm put-parameter --name "$SSM_PARAM" --value "$NEW_SNAPSHOT" \
    --type String --overwrite --region "$REGION" >/dev/null

log_info "✓ Re-pointed $SSM_PARAM → $NEW_SNAPSHOT"

# The old snapshot keeps the stale ocpctl:managed tag, which would let the
# EC2-tag discovery fallback pick it back up. Strip the discovery tags so only
# the encrypted one is findable.
aws ec2 delete-tags --region "$REGION" --resources "$OLD_SNAPSHOT" \
    --tags "Key=ocpctl:managed" "Key=ocpctl:image-version" \
    || log_warn "Could not remove discovery tags from $OLD_SNAPSHOT"

aws ec2 create-tags --region "$REGION" --resources "$OLD_SNAPSHOT" \
    --tags "Key=ocpctl:superseded-by,Value=${NEW_SNAPSHOT}" \
    || log_warn "Could not tag $OLD_SNAPSHOT as superseded"

log_info "✓ Removed discovery tags from superseded snapshot $OLD_SNAPSHOT"

log_info ""
log_info "═══════════════════════════════════════════════════════════════"
log_info "✅ Remediation complete"
log_info "═══════════════════════════════════════════════════════════════"
log_info "  Old (unencrypted): $OLD_SNAPSHOT  [left in place]"
log_info "  New (encrypted):   $NEW_SNAPSHOT"
log_info "  SSM:               $SSM_PARAM"
log_info ""
log_info "Clusters provisioned from now on will restore from the encrypted"
log_info "snapshot and their first CSI backup will be incremental."
log_info ""
log_warn "Windows VMs on EXISTING clusters still sit on volumes restored from"
log_warn "the old snapshot; they keep the full-first-backup penalty until they"
log_warn "are recreated."
log_info ""
log_info "Once you have confirmed nothing depends on it, delete the old snapshot:"
log_info "  aws ec2 delete-snapshot --snapshot-id $OLD_SNAPSHOT --region $REGION"
