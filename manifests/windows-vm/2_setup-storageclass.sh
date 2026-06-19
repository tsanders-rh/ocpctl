#!/bin/bash
#
# 2_setup-storageclass.sh - Prepare StorageClass and apply Windows DataVolume
#
# CDI (Containerized Data Importer) requires a StorageClass with Immediate volume
# binding mode so that both the main PVC and scratch PVC are provisioned before
# the importer pod is scheduled. Without this, the two PVCs can land in different
# AZs and the pod will never be schedulable.
#
# What this script does:
#   1. Finds worker nodes and the AZs they live in
#   2. Picks an AZ and looks for an existing Immediate-binding StorageClass pinned to it
#   3. Creates one if none exists (cloned from gp3-csi-immediate or gp3-csi)
#   4. Applies 2_windows-datavolume.yaml with the correct storageClassName injected
#
# Usage:
#   ./2_setup-storageclass.sh [--dry-run] [--zone <zone>] [--sc <name>]
#
#   --dry-run      Print the final DataVolume manifest but do not apply it
#   --zone <zone>  Force a specific AZ (e.g. us-west-2c)
#   --sc <name>    Use an existing StorageClass by name (skips auto-detection)
#   --watch        After applying, watch the DataVolume until it succeeds or fails
#
# Examples:
#   ./2_setup-storageclass.sh
#   ./2_setup-storageclass.sh --watch
#   ./2_setup-storageclass.sh --zone us-west-2a --dry-run
#   ./2_setup-storageclass.sh --sc my-custom-sc

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
DV_YAML="${SCRIPT_DIR}/2_windows-datavolume.yaml"
DV_NAMESPACE="openshift-virtualization-os-images"
DV_NAME="windows"
SC_BASE_PROVISIONER="ebs.csi.aws.com"

DRY_RUN=false
WATCH=false
FORCE_ZONE=""
FORCE_SC=""

# ---------------------------------------------------------------------------
# Argument parsing
# ---------------------------------------------------------------------------
while [[ $# -gt 0 ]]; do
  case "$1" in
    --dry-run)   DRY_RUN=true;  shift ;;
    --watch)     WATCH=true;    shift ;;
    --zone)      FORCE_ZONE="$2"; shift 2 ;;
    --sc)        FORCE_SC="$2";   shift 2 ;;
    *) echo "Unknown argument: $1"; exit 1 ;;
  esac
done

# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------
info()    { echo "[INFO]  $*"; }
success() { echo "[OK]    $*"; }
warn()    { echo "[WARN]  $*"; }
die()     { echo "[ERROR] $*" >&2; exit 1; }

require_cmd() { command -v "$1" &>/dev/null || die "'$1' is required but not found in PATH"; }
require_cmd oc
require_cmd yq

# ---------------------------------------------------------------------------
# Step 0: verify cluster access
# ---------------------------------------------------------------------------
info "Verifying cluster access..."
oc cluster-info --request-timeout=10s &>/dev/null || die "Cannot reach the cluster. Check your kubeconfig / oc login."
success "Cluster reachable."

# ---------------------------------------------------------------------------
# Step 1: short-circuit if --sc was provided
# ---------------------------------------------------------------------------
if [[ -n "$FORCE_SC" ]]; then
  CHOSEN_SC="$FORCE_SC"
  info "Using user-supplied StorageClass: ${CHOSEN_SC}"
  # Verify it exists
  oc get sc "$CHOSEN_SC" &>/dev/null || die "StorageClass '${CHOSEN_SC}' not found in cluster."
else

  # -------------------------------------------------------------------------
  # Step 2: find worker nodes and their AZs
  # -------------------------------------------------------------------------
  info "Discovering worker nodes and their availability zones..."

  # Worker nodes = nodes without the master taint
  WORKER_ZONES=()
  while IFS= read -r line; do
    WORKER_ZONES+=("$line")
  done < <(
    oc get nodes -o json | \
    python3 -c "
import json, sys
data = json.load(sys.stdin)
for n in data['items']:
    taints = n.get('spec', {}).get('taints', [])
    is_master = any(t.get('key') == 'node-role.kubernetes.io/master' for t in taints)
    if not is_master:
        zone = n['metadata'].get('labels', {}).get('topology.kubernetes.io/zone', '')
        if zone:
            print(zone)
" | sort -u
  )

  if [[ ${#WORKER_ZONES[@]} -eq 0 ]]; then
    die "No schedulable worker nodes with a topology zone label found."
  fi

  info "Worker AZs found: ${WORKER_ZONES[*]}"

  # Prefer user-specified zone if it has a worker
  if [[ -n "$FORCE_ZONE" ]]; then
    if printf '%s\n' "${WORKER_ZONES[@]}" | grep -qx "$FORCE_ZONE"; then
      TARGET_ZONE="$FORCE_ZONE"
    else
      die "Zone '${FORCE_ZONE}' was specified but has no schedulable worker nodes. Available: ${WORKER_ZONES[*]}"
    fi
  else
    TARGET_ZONE="${WORKER_ZONES[0]}"
  fi

  info "Target AZ: ${TARGET_ZONE}"

  # -------------------------------------------------------------------------
  # Step 3: find or create a suitable StorageClass
  # -------------------------------------------------------------------------
  # Suitable = Immediate binding + provisioner matches ebs.csi.aws.com + zone matches
  info "Looking for an existing Immediate-binding StorageClass in zone ${TARGET_ZONE}..."

  CHOSEN_SC=""
  while IFS= read -r sc_name; do
    binding_mode=$(oc get sc "$sc_name" -o jsonpath='{.volumeBindingMode}')
    provisioner=$(oc get sc "$sc_name" -o jsonpath='{.provisioner}')
    if [[ "$binding_mode" != "Immediate" ]]; then
      continue
    fi
    # Check for zone topology restriction
    allowed_zones=$(oc get sc "$sc_name" -o json | \
      python3 -c "
import json, sys
sc = json.load(sys.stdin)
zones = []
for top in sc.get('allowedTopologies', []):
    for expr in top.get('matchLabelExpressions', []):
        if expr.get('key') == 'topology.kubernetes.io/zone':
            zones.extend(expr.get('values', []))
print(' '.join(zones))
")
    if [[ -z "$allowed_zones" ]]; then
      # No zone restriction — Immediate but not pinned. Record as fallback.
      FALLBACK_SC="${sc_name}"
      continue
    fi
    if echo "$allowed_zones" | tr ' ' '\n' | grep -qx "$TARGET_ZONE"; then
      CHOSEN_SC="$sc_name"
      break
    fi
  done < <(oc get sc -o jsonpath='{range .items[*]}{.metadata.name}{"\n"}{end}')

  if [[ -n "$CHOSEN_SC" ]]; then
    success "Found existing zone-pinned Immediate StorageClass: ${CHOSEN_SC}"
  else
    # -----------------------------------------------------------------------
    # Step 4: create a zone-pinned StorageClass
    # -----------------------------------------------------------------------
    NEW_SC_NAME="gp3-csi-immediate-${TARGET_ZONE}"
    info "No suitable StorageClass found. Creating '${NEW_SC_NAME}'..."

    # Clone parameters from the best available Immediate SC (or gp3-csi)
    SOURCE_SC="${FALLBACK_SC:-gp3-csi-immediate}"
    if ! oc get sc "$SOURCE_SC" &>/dev/null; then
      SOURCE_SC=$(oc get sc -o jsonpath='{range .items[?(@.volumeBindingMode=="Immediate")]}{.metadata.name}{"\n"}{end}' | head -1)
      [[ -z "$SOURCE_SC" ]] && SOURCE_SC="gp3-csi"
    fi

    info "Cloning parameters from StorageClass: ${SOURCE_SC}"

    SC_PROVISIONER=$(oc get sc "$SOURCE_SC" -o jsonpath='{.provisioner}')
    SC_PARAMS=$(oc get sc "$SOURCE_SC" -o json | \
      python3 -c "
import json, sys
sc = json.load(sys.stdin)
params = sc.get('parameters', {})
print(json.dumps(params))
")

    if $DRY_RUN; then
      info "[dry-run] Would create StorageClass '${NEW_SC_NAME}' (zone=${TARGET_ZONE}, provisioner=${SC_PROVISIONER})"
      CHOSEN_SC="$NEW_SC_NAME"
    else
    cat <<EOF | oc apply -f -
allowVolumeExpansion: true
apiVersion: storage.k8s.io/v1
kind: StorageClass
metadata:
  name: ${NEW_SC_NAME}
  annotations:
    ocpctl.io/created-by: 2_setup-storageclass.sh
    ocpctl.io/cloned-from: ${SOURCE_SC}
parameters: $(python3 -c "
import json, sys
params = json.loads('${SC_PARAMS}')
if params:
    lines = []
    for k, v in params.items():
        lines.append('  ' + k + ': ' + json.dumps(v))
    print()
    print('\n'.join(lines))
else:
    print(' {}')
")
provisioner: ${SC_PROVISIONER}
reclaimPolicy: Delete
volumeBindingMode: Immediate
allowedTopologies:
- matchLabelExpressions:
  - key: topology.kubernetes.io/zone
    values:
    - ${TARGET_ZONE}
EOF

    success "StorageClass '${NEW_SC_NAME}' created."
    CHOSEN_SC="$NEW_SC_NAME"
    fi  # end dry-run guard
  fi
fi

# ---------------------------------------------------------------------------
# Step 5: build the final DataVolume manifest with the chosen StorageClass
# ---------------------------------------------------------------------------
info "Injecting storageClassName '${CHOSEN_SC}' into DataVolume manifest..."

FINAL_MANIFEST=$(yq e ".spec.storage.storageClassName = \"${CHOSEN_SC}\"" "$DV_YAML")

if $DRY_RUN; then
  echo ""
  echo "=== DRY RUN — DataVolume manifest that would be applied ==="
  echo "$FINAL_MANIFEST"
  echo "==========================================================="
  exit 0
fi

# ---------------------------------------------------------------------------
# Step 6: delete existing DataVolume if present (to force a clean import)
# ---------------------------------------------------------------------------
if oc get datavolume "$DV_NAME" -n "$DV_NAMESPACE" &>/dev/null; then
  warn "DataVolume '${DV_NAME}' already exists in namespace '${DV_NAMESPACE}'."
  read -r -p "        Delete and re-create it? [y/N] " confirm
  if [[ "$confirm" =~ ^[Yy]$ ]]; then
    info "Deleting existing DataVolume..."
    oc delete datavolume "$DV_NAME" -n "$DV_NAMESPACE"
    info "Waiting for PVCs to clean up..."
    sleep 5
  else
    info "Skipping deletion. Patching storageClassName on existing DataVolume..."
    echo "$FINAL_MANIFEST" | oc apply -f -
    SKIP_WATCH=true
  fi
fi

# ---------------------------------------------------------------------------
# Step 7: apply the DataVolume
# ---------------------------------------------------------------------------
if [[ "${SKIP_WATCH:-false}" != "true" ]]; then
  info "Applying DataVolume '${DV_NAME}' in namespace '${DV_NAMESPACE}'..."
  echo "$FINAL_MANIFEST" | oc create -f -
fi

success "DataVolume applied with StorageClass: ${CHOSEN_SC}"
echo ""
echo "  Monitor progress:"
echo "    oc get datavolume ${DV_NAME} -n ${DV_NAMESPACE} -w"
echo "    oc get pods -n ${DV_NAMESPACE}"
echo "    oc logs -f -n ${DV_NAMESPACE} -l app=containerized-data-importer"
echo ""

# ---------------------------------------------------------------------------
# Step 8: optional watch loop
# ---------------------------------------------------------------------------
if $WATCH; then
  info "Watching DataVolume import (Ctrl-C to stop)..."
  echo ""
  while true; do
    PHASE=$(oc get datavolume "$DV_NAME" -n "$DV_NAMESPACE" -o jsonpath='{.status.phase}' 2>/dev/null || echo "Unknown")
    PROGRESS=$(oc get datavolume "$DV_NAME" -n "$DV_NAMESPACE" -o jsonpath='{.status.progress}' 2>/dev/null || echo "N/A")
    POD=$(oc get pods -n "$DV_NAMESPACE" --no-headers 2>/dev/null | grep importer | awk '{print $1" "$3}' || echo "none")
    printf "\r  Phase: %-20s  Progress: %-10s  Pod: %s          " "$PHASE" "$PROGRESS" "$POD"
    case "$PHASE" in
      Succeeded)
        echo ""
        success "Import complete!"
        break ;;
      Failed|ImportFailed)
        echo ""
        die "Import failed. Check logs: oc logs -n ${DV_NAMESPACE} -l app=containerized-data-importer"
        ;;
    esac
    sleep 5
  done
fi
