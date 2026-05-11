#!/bin/bash
#
# manage-windows-volumesnapshot.sh - Manage a VolumeSnapshot of the Windows golden image
#
# Takes a VolumeSnapshot of the 'windows' PVC in openshift-virtualization-os-images.
# Subsequent VM launches can restore from this snapshot (~2-5 min on EBS) instead of
# cloning from the DataSource (~30 min per VM).
#
# Prerequisites:
#   - 'windows' DataVolume must be in Succeeded phase
#   - A VolumeSnapshotClass must exist for the cluster's CSI driver
#
# Usage:
#   ./manage-windows-volumesnapshot.sh [--name <name>] [--snapshot-class <class>] [--dry-run]
#   ./manage-windows-volumesnapshot.sh --status [--wait] [--name <name>]
#
# Examples:
#   ./manage-windows-volumesnapshot.sh
#   ./manage-windows-volumesnapshot.sh --name my-snap
#   ./manage-windows-volumesnapshot.sh --dry-run
#   ./manage-windows-volumesnapshot.sh --status
#   ./manage-windows-volumesnapshot.sh --status --wait

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ENV_FILE="${SCRIPT_DIR}/.golden-snapshot.env"

PVC_NAME="windows"
PVC_NAMESPACE="openshift-virtualization-os-images"
SNAP_NAME="windows-golden-snap"
SNAP_CLASS="csi-aws-vsc"
DRY_RUN=false
STATUS_ONLY=false
STATUS_WAIT=false

# ---------------------------------------------------------------------------
# Argument parsing
# ---------------------------------------------------------------------------
while [[ $# -gt 0 ]]; do
  case "$1" in
    --name)
      [[ $# -ge 2 ]] || { echo "[ERROR] --name requires a value" >&2; exit 1; }
      SNAP_NAME="$2"; shift 2 ;;
    --snapshot-class)
      [[ $# -ge 2 ]] || { echo "[ERROR] --snapshot-class requires a value" >&2; exit 1; }
      SNAP_CLASS="$2"; shift 2 ;;
    --dry-run)  DRY_RUN=true;    shift ;;
    --status)   STATUS_ONLY=true; shift ;;
    --wait)     STATUS_WAIT=true; shift ;;
    *) echo "Unknown argument: $1"; exit 1 ;;
  esac
done

if [[ ! "$SNAP_NAME" =~ ^[a-z0-9]([a-z0-9.-]*[a-z0-9])?$ ]]; then
  echo "[ERROR] Invalid snapshot name '${SNAP_NAME}'." >&2; exit 1
fi

# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------
die()     { echo "[ERROR] $*" >&2; exit 1; }
info()    { echo "[INFO]  $*"; }
success() { echo "[OK]    $*"; }
warn()    { echo "[WARN]  $*"; }

require_cmd() { command -v "$1" &>/dev/null || die "'$1' is required but not found in PATH"; }
require_cmd oc

snap_field() {
  oc get volumesnapshot "$SNAP_NAME" -n "$PVC_NAMESPACE" -o jsonpath="$1" 2>/dev/null || echo "${2:-unknown}"
}

detect_az() {
  local pv="$1" az=""
  az=$(oc get pv "$pv" -o jsonpath='{.metadata.labels.topology\.kubernetes\.io/zone}' 2>/dev/null || true)
  if [[ -z "$az" ]]; then
    az=$(oc get pv "$pv" -o jsonpath='{.spec.nodeAffinity.required.nodeSelectorTerms[0].matchExpressions[?(@.key=="topology.kubernetes.io/zone")].values[0]}' 2>/dev/null || true)
  fi
  echo "$az"
}

region_from_az() {
  [[ -n "$1" ]] && echo "${1%[a-f]}" || true
}

# Resolve VolumeSnapshot -> VolumeSnapshotContent -> EBS snapshot handle
resolve_ebs_handle() {
  local content handle=""
  content=$(snap_field '{.status.boundVolumeSnapshotContentName}' "")
  if [[ -n "$content" ]]; then
    handle=$(oc get volumesnapshotcontent "$content" -o jsonpath='{.status.snapshotHandle}' 2>/dev/null || true)
  fi
  echo "$handle"
}

# Query EBS snapshot state and progress; sets EBS_STATE and EBS_PROGRESS
query_ebs() {
  EBS_STATE="" EBS_PROGRESS=""
  [[ -n "$AWS_REGION" ]] && command -v aws &>/dev/null || return 0
  local handle
  handle=$(resolve_ebs_handle)
  [[ -n "$handle" ]] || return 0
  EBS_HANDLE="$handle"
  EBS_PROGRESS=$(aws ec2 describe-snapshots --snapshot-ids "$handle" \
    --query 'Snapshots[0].Progress' --output text --region "$AWS_REGION" 2>/dev/null || true)
  EBS_STATE=$(aws ec2 describe-snapshots --snapshot-ids "$handle" \
    --query 'Snapshots[0].State' --output text --region "$AWS_REGION" 2>/dev/null || true)
}

print_status() {
  local ready restore_size creation_time snap_class
  ready=$(snap_field '{.status.readyToUse}' "unknown")
  restore_size=$(snap_field '{.status.restoreSize}' "unknown")
  creation_time=$(snap_field '{.status.creationTime}' "unknown")
  snap_class=$(snap_field '{.spec.volumeSnapshotClassName}' "unknown")
  LAST_READY="$ready"

  echo ""
  echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
  echo "  VolumeSnapshot Status"
  echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
  echo "  Snapshot:       ${SNAP_NAME}"
  echo "  Namespace:      ${PVC_NAMESPACE}"
  echo "  ReadyToUse:     ${ready}"
  echo "  RestoreSize:    ${restore_size}"
  echo "  CreationTime:   ${creation_time}"
  echo "  SnapshotClass:  ${snap_class}"
  echo "  StorageClass:   ${SOURCE_SC:-unknown}"
  echo "  AZ:             ${SOURCE_AZ:-unknown}"

  EBS_HANDLE=""
  query_ebs
  if [[ -n "$EBS_STATE" ]]; then
    echo ""
    echo "  EBS Snapshot:   ${EBS_HANDLE}"
    echo "  EBS State:      ${EBS_STATE}"
    echo "  EBS Progress:   ${EBS_PROGRESS:-unknown}"
  fi

  echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
}

golden_snapshot_summary() {
  echo ""
  echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
  echo "  Golden snapshot ready"
  echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
  echo "  Snapshot:       ${SNAP_NAME}"
  echo "  Namespace:      ${PVC_NAMESPACE}"
  echo "  StorageClass:   ${SOURCE_SC}"
  echo "  AZ:             ${SOURCE_AZ:-unknown}"
  echo ""
  echo "  Launch VMs:"
  echo "    ./6_launch_vm.sh --count 5"
  echo ""
  echo "  For Go tests:"
  echo "    export KVP_WINDOWS_SNAPSHOT_NAME=${SNAP_NAME}"
  echo "    export KVP_WINDOWS_SNAPSHOT_NS=${PVC_NAMESPACE}"
  echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
}

write_env_file() {
  info "Writing ${ENV_FILE}..."
  cat > "$ENV_FILE" <<ENVEOF
GOLDEN_SNAPSHOT_NAME=${SNAP_NAME}
GOLDEN_SNAPSHOT_NS=${PVC_NAMESPACE}
GOLDEN_SC=${SOURCE_SC}
ENVEOF
  success "Env file written: ${ENV_FILE}"
}

# Wait for VolumeSnapshot readyToUse with progress display
wait_for_snapshot() {
  local timeout=$1 interval=$2
  info "Waiting for VolumeSnapshot to become readyToUse..."
  local elapsed=0
  while [[ $elapsed -lt $timeout ]]; do
    local ready
    ready=$(snap_field '{.status.readyToUse}' "false")
    [[ "$ready" == "true" ]] && { echo ""; return 0; }

    EBS_HANDLE=""
    query_ebs
    if [[ -n "$EBS_PROGRESS" ]]; then
      printf "\r  Waiting... (%ds / %ds) | EBS snapshot progress: %s" "$elapsed" "$timeout" "$EBS_PROGRESS"
    else
      printf "\r  Waiting... (%ds / %ds)" "$elapsed" "$timeout"
    fi

    sleep "$interval"
    elapsed=$((elapsed + interval))
  done
  echo ""
  return 1
}

# ---------------------------------------------------------------------------
# Preflight: verify cluster access and source PVC
# ---------------------------------------------------------------------------
info "Verifying cluster access..."
oc cluster-info --request-timeout=10s &>/dev/null || die "Cannot reach the cluster. Check your kubeconfig / oc login."
success "Cluster reachable."

info "Checking DataVolume '${PVC_NAME}' in namespace '${PVC_NAMESPACE}'..."
DV_PHASE=$(oc get datavolume "$PVC_NAME" -n "$PVC_NAMESPACE" -o jsonpath='{.status.phase}' 2>/dev/null || echo "NotFound")
[[ "$DV_PHASE" == "Succeeded" ]] || die "DataVolume '${PVC_NAME}' is in phase '${DV_PHASE}' (expected 'Succeeded')."
success "DataVolume '${PVC_NAME}' is Succeeded."

oc get pvc "$PVC_NAME" -n "$PVC_NAMESPACE" &>/dev/null || die "PVC '${PVC_NAME}' not found in namespace '${PVC_NAMESPACE}'."

info "Detecting source PVC availability zone and StorageClass..."
SOURCE_SC=$(oc get pvc "$PVC_NAME" -n "$PVC_NAMESPACE" -o jsonpath='{.spec.storageClassName}')
PV_NAME=$(oc get pvc "$PVC_NAME" -n "$PVC_NAMESPACE" -o jsonpath='{.spec.volumeName}')
SOURCE_AZ=""
[[ -n "$PV_NAME" ]] && SOURCE_AZ=$(detect_az "$PV_NAME")
AWS_REGION=$(region_from_az "$SOURCE_AZ")

info "Source StorageClass: ${SOURCE_SC:-unknown}"
info "Source AZ: ${SOURCE_AZ:-unknown}"

# ---------------------------------------------------------------------------
# --status mode
# ---------------------------------------------------------------------------
if $STATUS_ONLY; then
  if ! oc get volumesnapshot "$SNAP_NAME" -n "$PVC_NAMESPACE" &>/dev/null; then
    echo ""
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    echo "  Snapshot:       ${SNAP_NAME} (not found)"
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    exit 0
  fi

  LAST_READY="unknown"

  if ! $STATUS_WAIT; then
    print_status
    exit 0
  fi

  info "Polling snapshot status (Ctrl-C to stop)..."
  TIMEOUT=3600
  ELAPSED=0
  while [[ $ELAPSED -lt $TIMEOUT ]]; do
    clear 2>/dev/null || printf "\033c"
    print_status

    if [[ "$LAST_READY" == "true" ]]; then
      echo ""
      success "VolumeSnapshot '${SNAP_NAME}' is ready."
      break
    fi

    echo ""
    printf "  Polling... (%ds elapsed, refreshing in 10s)\n" "$ELAPSED"
    sleep 10
    ELAPSED=$((ELAPSED + 10))
  done

  [[ "$LAST_READY" == "true" ]] || die "Timed out after ${TIMEOUT}s waiting for snapshot to become ready."
  exit 0
fi

# ---------------------------------------------------------------------------
# Create flow: validate snapshot class
# ---------------------------------------------------------------------------
oc get volumesnapshotclass "$SNAP_CLASS" &>/dev/null || die "VolumeSnapshotClass '${SNAP_CLASS}' not found. Create it or pass --snapshot-class."
info "Using VolumeSnapshotClass: ${SNAP_CLASS} (deletionPolicy: $(oc get volumesnapshotclass "$SNAP_CLASS" -o jsonpath='{.deletionPolicy}'))"

# ---------------------------------------------------------------------------
# Create flow: check for existing snapshot
# ---------------------------------------------------------------------------
if oc get volumesnapshot "$SNAP_NAME" -n "$PVC_NAMESPACE" &>/dev/null; then
  EXISTING_READY=$(snap_field '{.status.readyToUse}' "false")
  if [[ "$EXISTING_READY" == "true" ]]; then
    warn "VolumeSnapshot '${SNAP_NAME}' already exists and is ready."
    warn "Skipping creation. Delete it first if you want to recreate:"
    warn "  oc delete volumesnapshot ${SNAP_NAME} -n ${PVC_NAMESPACE}"
    $DRY_RUN || write_env_file
    golden_snapshot_summary
    exit 0
  else
    warn "VolumeSnapshot '${SNAP_NAME}' exists but is not ready. Deleting and recreating..."
    if $DRY_RUN; then
      warn "Dry-run: not deleting existing VolumeSnapshot; manifest below is what would be applied."
    else
      oc delete volumesnapshot "$SNAP_NAME" -n "$PVC_NAMESPACE"
      sleep 2
    fi
  fi
fi

# ---------------------------------------------------------------------------
# Create flow: apply the VolumeSnapshot
# ---------------------------------------------------------------------------
info "Creating VolumeSnapshot '${SNAP_NAME}' from PVC '${PVC_NAME}'..."

SNAP_MANIFEST=$(cat <<EOF
apiVersion: snapshot.storage.k8s.io/v1
kind: VolumeSnapshot
metadata:
  name: ${SNAP_NAME}
  namespace: ${PVC_NAMESPACE}
  labels:
    app.kubernetes.io/managed-by: oadp-kubevirt-windows-test
    app.kubernetes.io/created-by: manage-windows-volumesnapshot.sh
spec:
  volumeSnapshotClassName: ${SNAP_CLASS}
  source:
    persistentVolumeClaimName: ${PVC_NAME}
EOF
)

if $DRY_RUN; then
  echo ""
  echo "=== DRY RUN ==="
  echo "$SNAP_MANIFEST"
  echo "================"
  exit 0
fi

echo "$SNAP_MANIFEST" | oc apply -f -

# ---------------------------------------------------------------------------
# Create flow: wait for snapshot
# ---------------------------------------------------------------------------
wait_for_snapshot 1800 5 || die "VolumeSnapshot '${SNAP_NAME}' did not become ready within 1800s."
success "VolumeSnapshot '${SNAP_NAME}' is ready."

SNAP_SIZE=$(snap_field '{.status.restoreSize}' "unknown")
info "Snapshot restore size: ${SNAP_SIZE}"

write_env_file
golden_snapshot_summary
