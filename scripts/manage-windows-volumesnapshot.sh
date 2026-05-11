#!/bin/bash
#
# 5_create-golden-snapshot.sh - Create a VolumeSnapshot of the Windows golden image
#
# Takes a VolumeSnapshot of the 'windows' PVC in openshift-virtualization-os-images.
# Subsequent VM launches can restore from this snapshot (~2-5 min on EBS) instead of
# cloning from the DataSource (~30 min per VM).
#
# Prerequisites:
#   - 'windows' DataVolume must be in Succeeded phase (run 2_setup-storageclass.sh first)
#   - A VolumeSnapshotClass must exist for the cluster's CSI driver
#
# Usage:
#   ./5_create-golden-snapshot.sh [--name <name>] [--snapshot-class <class>] [--dry-run]
#
# Examples:
#   ./5_create-golden-snapshot.sh
#   ./5_create-golden-snapshot.sh --name my-snap
#   ./5_create-golden-snapshot.sh --dry-run

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ENV_FILE="${SCRIPT_DIR}/.golden-snapshot.env"

PVC_NAME="windows"
PVC_NAMESPACE="openshift-virtualization-os-images"
SNAP_NAME="windows-golden-snap"
SNAP_CLASS="csi-aws-vsc"
DRY_RUN=false

die() { echo "[ERROR] $*" >&2; exit 1; }

# ---------------------------------------------------------------------------
# Argument parsing
# ---------------------------------------------------------------------------
while [[ $# -gt 0 ]]; do
  case "$1" in
    --name)
      [[ $# -ge 2 ]] || die "--name requires a value"
      SNAP_NAME="$2"; shift 2 ;;
    --snapshot-class)
      [[ $# -ge 2 ]] || die "--snapshot-class requires a value"
      SNAP_CLASS="$2"; shift 2 ;;
    --dry-run)         DRY_RUN=true; shift ;;
    *) echo "Unknown argument: $1"; exit 1 ;;
  esac
done

if [[ ! "$SNAP_NAME" =~ ^[a-z0-9]([a-z0-9.-]*[a-z0-9])?$ ]]; then
  die "Invalid snapshot name '${SNAP_NAME}'. Must be a valid Kubernetes resource name (lowercase alphanumeric, hyphens, dots)."
fi

# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------
info()    { echo "[INFO]  $*"; }
success() { echo "[OK]    $*"; }
warn()    { echo "[WARN]  $*"; }

require_cmd() { command -v "$1" &>/dev/null || die "'$1' is required but not found in PATH"; }
require_cmd oc

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

# ---------------------------------------------------------------------------
# Step 0: verify cluster access
# ---------------------------------------------------------------------------
info "Verifying cluster access..."
oc cluster-info --request-timeout=10s &>/dev/null || die "Cannot reach the cluster. Check your kubeconfig / oc login."
success "Cluster reachable."

# ---------------------------------------------------------------------------
# Step 1: verify the windows DataVolume is Succeeded
# ---------------------------------------------------------------------------
info "Checking DataVolume '${PVC_NAME}' in namespace '${PVC_NAMESPACE}'..."
DV_PHASE=$(oc get datavolume "$PVC_NAME" -n "$PVC_NAMESPACE" -o jsonpath='{.status.phase}' 2>/dev/null || echo "NotFound")

if [[ "$DV_PHASE" != "Succeeded" ]]; then
  die "DataVolume '${PVC_NAME}' is in phase '${DV_PHASE}' (expected 'Succeeded'). Run 2_setup-storageclass.sh first and wait for the import to complete."
fi
success "DataVolume '${PVC_NAME}' is Succeeded."

# ---------------------------------------------------------------------------
# Step 2: verify the PVC exists
# ---------------------------------------------------------------------------
if ! oc get pvc "$PVC_NAME" -n "$PVC_NAMESPACE" &>/dev/null; then
  die "PVC '${PVC_NAME}' not found in namespace '${PVC_NAMESPACE}'."
fi

# ---------------------------------------------------------------------------
# Step 3: detect the source PVC's AZ and StorageClass
# ---------------------------------------------------------------------------
info "Detecting source PVC availability zone and StorageClass..."

SOURCE_SC=$(oc get pvc "$PVC_NAME" -n "$PVC_NAMESPACE" -o jsonpath='{.spec.storageClassName}')
PV_NAME=$(oc get pvc "$PVC_NAME" -n "$PVC_NAMESPACE" -o jsonpath='{.spec.volumeName}')
SOURCE_AZ=""
if [[ -n "$PV_NAME" ]]; then
  SOURCE_AZ=$(oc get pv "$PV_NAME" -o jsonpath='{.metadata.labels.topology\.kubernetes\.io/zone}' 2>/dev/null || true)
fi

info "Source StorageClass: ${SOURCE_SC:-unknown}"
info "Source AZ: ${SOURCE_AZ:-unknown}"

# ---------------------------------------------------------------------------
# Step 4: find or validate VolumeSnapshotClass
# ---------------------------------------------------------------------------
if ! oc get volumesnapshotclass "$SNAP_CLASS" &>/dev/null; then
  die "VolumeSnapshotClass '${SNAP_CLASS}' not found. Create it or pass --snapshot-class."
fi
info "Using VolumeSnapshotClass: ${SNAP_CLASS} (deletionPolicy: $(oc get volumesnapshotclass "$SNAP_CLASS" -o jsonpath='{.deletionPolicy}'))"

# ---------------------------------------------------------------------------
# Step 5: check for existing snapshot
# ---------------------------------------------------------------------------
if oc get volumesnapshot "$SNAP_NAME" -n "$PVC_NAMESPACE" &>/dev/null; then
  EXISTING_READY=$(oc get volumesnapshot "$SNAP_NAME" -n "$PVC_NAMESPACE" -o jsonpath='{.status.readyToUse}' 2>/dev/null || echo "false")
  if [[ "$EXISTING_READY" == "true" ]]; then
    warn "VolumeSnapshot '${SNAP_NAME}' already exists and is ready."
    warn "Skipping creation. Delete it first if you want to recreate:"
    warn "  oc delete volumesnapshot ${SNAP_NAME} -n ${PVC_NAMESPACE}"

    if ! $DRY_RUN; then
      info "Writing ${ENV_FILE}..."
      cat > "$ENV_FILE" <<ENVEOF
GOLDEN_SNAPSHOT_NAME=${SNAP_NAME}
GOLDEN_SNAPSHOT_NS=${PVC_NAMESPACE}
GOLDEN_SC=${SOURCE_SC}
ENVEOF
      success "Env file written: ${ENV_FILE}"
    else
      info "Dry-run: skipping ${ENV_FILE}"
    fi
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
# Step 6: create the VolumeSnapshot
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
    app.kubernetes.io/created-by: 5_create-golden-snapshot.sh
spec:
  volumeSnapshotClassName: ${SNAP_CLASS}
  source:
    persistentVolumeClaimName: ${PVC_NAME}
EOF
)

if $DRY_RUN; then
  echo ""
  echo "=== DRY RUN — VolumeSnapshot manifest that would be applied ==="
  echo "$SNAP_MANIFEST"
  echo "================================================================"
  exit 0
fi

echo "$SNAP_MANIFEST" | oc apply -f -

# ---------------------------------------------------------------------------
# Step 7: wait for snapshot to become ready
# ---------------------------------------------------------------------------
info "Waiting for VolumeSnapshot to become readyToUse..."

AWS_REGION=""
if [[ -n "${SOURCE_AZ}" ]]; then
  AWS_REGION="${SOURCE_AZ%[a-f]}"
fi

READY=false
TIMEOUT=1800
ELAPSED=0
while [[ $ELAPSED -lt $TIMEOUT ]]; do
  READY=$(oc get volumesnapshot "$SNAP_NAME" -n "$PVC_NAMESPACE" -o jsonpath='{.status.readyToUse}' 2>/dev/null || echo "false")
  if [[ "$READY" == "true" ]]; then
    break
  fi

  AWS_PROGRESS=""
  if [[ -n "$AWS_REGION" ]] && command -v aws &>/dev/null; then
    SNAP_CONTENT=$(oc get volumesnapshot "$SNAP_NAME" -n "$PVC_NAMESPACE" -o jsonpath='{.status.boundVolumeSnapshotContentName}' 2>/dev/null || true)
    if [[ -n "$SNAP_CONTENT" ]]; then
      SNAP_HANDLE=$(oc get volumesnapshotcontent "$SNAP_CONTENT" -o jsonpath='{.status.snapshotHandle}' 2>/dev/null || true)
      if [[ -n "$SNAP_HANDLE" ]]; then
        AWS_PROGRESS=$(aws ec2 describe-snapshots --snapshot-ids "$SNAP_HANDLE" \
          --query 'Snapshots[0].Progress' --output text --region "$AWS_REGION" 2>/dev/null || true)
      fi
    fi
  fi

  if [[ -n "$AWS_PROGRESS" ]]; then
    printf "\r  Waiting... (%ds / %ds) — EBS snapshot progress: %s" "$ELAPSED" "$TIMEOUT" "$AWS_PROGRESS"
  else
    printf "\r  Waiting... (%ds / %ds)" "$ELAPSED" "$TIMEOUT"
  fi
  sleep 5
  ELAPSED=$((ELAPSED + 5))
done

if [[ "$READY" != "true" ]]; then
  echo ""
  die "VolumeSnapshot '${SNAP_NAME}' did not become ready within ${TIMEOUT}s."
fi

echo ""
success "VolumeSnapshot '${SNAP_NAME}' is ready."

SNAP_SIZE=$(oc get volumesnapshot "$SNAP_NAME" -n "$PVC_NAMESPACE" -o jsonpath='{.status.restoreSize}' 2>/dev/null || echo "unknown")
info "Snapshot restore size: ${SNAP_SIZE}"

# ---------------------------------------------------------------------------
# Step 8: write env file
# ---------------------------------------------------------------------------
info "Writing ${ENV_FILE}..."
cat > "$ENV_FILE" <<ENVEOF
GOLDEN_SNAPSHOT_NAME=${SNAP_NAME}
GOLDEN_SNAPSHOT_NS=${PVC_NAMESPACE}
GOLDEN_SC=${SOURCE_SC}
ENVEOF
success "Env file written: ${ENV_FILE}"

golden_snapshot_summary
