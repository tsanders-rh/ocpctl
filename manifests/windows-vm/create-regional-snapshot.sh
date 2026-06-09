#!/bin/bash
#
# Create Regional Windows Snapshot
#
# This script creates a validated EBS snapshot of the Windows image in a specific region.
# It's designed to be run on a temporary OpenShift cluster with CNV already installed.
#
# Required environment variables:
#   - KUBECONFIG: Path to kubeconfig file
#   - REGION: AWS region for snapshot
#   - SNAPSHOT_VERSION: Snapshot version (e.g., "1.0")
#   - S3_SOURCE_URL: S3 URL of Windows QCOW2 image (optional, defaults to standard path)
#
# Outputs:
#   - EBS_SNAPSHOT_ID: EBS snapshot ID (written to stdout on success)
#   - SSM_PARAMETER_PATH: SSM parameter path where snapshot is published
#

set -e
set -o pipefail

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

log_info() {
    echo -e "${GREEN}[INFO]${NC} $1"
}

log_warn() {
    echo -e "${YELLOW}[WARN]${NC} $1"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

# Verify required environment variables
if [ -z "$KUBECONFIG" ] || [ -z "$REGION" ] || [ -z "$SNAPSHOT_VERSION" ]; then
    log_error "Missing required environment variables"
    log_error "Required: KUBECONFIG, REGION, SNAPSHOT_VERSION"
    exit 1
fi

# Default S3 source
if [ -z "$S3_SOURCE_URL" ]; then
    S3_SOURCE_URL="s3://ocpctl-binaries/windows-images/windows-10-oadp.qcow2"
fi

SERVICE_ACCOUNT_NAMESPACE="openshift-virtualization-os-images"

log_info "═══════════════════════════════════════════════════════════════"
log_info "Creating Regional Windows Snapshot"
log_info "Region: $REGION"
log_info "Version: $SNAPSHOT_VERSION"
log_info "S3 Source: $S3_SOURCE_URL"
log_info "═══════════════════════════════════════════════════════════════"

# Create namespace
log_info "Creating namespace: $SERVICE_ACCOUNT_NAMESPACE"
oc --kubeconfig="$KUBECONFIG" create namespace "$SERVICE_ACCOUNT_NAMESPACE" --dry-run=client -o yaml | \
    oc --kubeconfig="$KUBECONFIG" apply -f -

# Wait for CDI API to be ready
log_info "Waiting for CDI API to be ready..."
MAX_WAIT=300
ELAPSED=0
while [ $ELAPSED -lt $MAX_WAIT ]; do
    if oc --kubeconfig="$KUBECONFIG" get endpoints cdi-api -n openshift-cnv &>/dev/null; then
        ENDPOINTS=$(oc --kubeconfig="$KUBECONFIG" get endpoints cdi-api -n openshift-cnv -o jsonpath='{.subsets[*].addresses[*].ip}' 2>/dev/null || echo "")
        if [ -n "$ENDPOINTS" ]; then
            log_info "✓ CDI API is ready"
            break
        fi
    fi
    sleep 5
    ELAPSED=$((ELAPSED + 5))
done

if [ $ELAPSED -ge $MAX_WAIT ]; then
    log_error "Timeout waiting for CDI API"
    exit 1
fi

# Detect storage class and zone
log_info "Detecting storage class and worker zones..."
if oc --kubeconfig="$KUBECONFIG" get storageclass gp3-csi &>/dev/null; then
    WORKER_ZONES=$(oc --kubeconfig="$KUBECONFIG" get nodes -l node-role.kubernetes.io/worker \
        -o jsonpath='{range .items[*]}{.metadata.labels.topology\.kubernetes\.io/zone}{"\n"}{end}' | sort -u)

    if [ -z "$WORKER_ZONES" ]; then
        log_error "No worker nodes found"
        exit 1
    fi

    IMPORT_ZONE=$(echo "$WORKER_ZONES" | head -1)
    STORAGE_CLASS="gp3-csi-immediate-${IMPORT_ZONE}"

    # Create zone-constrained storage class
    if ! oc --kubeconfig="$KUBECONFIG" get storageclass "$STORAGE_CLASS" &>/dev/null; then
        log_info "Creating storage class: $STORAGE_CLASS"
        cat <<EOF | oc --kubeconfig="$KUBECONFIG" apply -f -
apiVersion: storage.k8s.io/v1
kind: StorageClass
metadata:
  name: ${STORAGE_CLASS}
allowVolumeExpansion: true
parameters:
  encrypted: "true"
  type: gp3
provisioner: ebs.csi.aws.com
reclaimPolicy: Delete
volumeBindingMode: Immediate
allowedTopologies:
- matchLabelExpressions:
  - key: topology.ebs.csi.aws.com/zone
    values:
    - ${IMPORT_ZONE}
EOF
    fi
else
    log_error "No supported storage class found"
    exit 1
fi

log_info "✓ Using storage class: $STORAGE_CLASS in zone: $IMPORT_ZONE"

# Import Windows image from S3
log_info ""
log_info "═══════════════════════════════════════════════════════════════"
log_info "Importing Windows image from S3..."
log_info "═══════════════════════════════════════════════════════════════"

# Generate presigned URL
PRESIGNED_URL=$(aws s3 presign "$S3_SOURCE_URL" --expires-in 86400 --region us-east-1)

# Create DataVolume
cat <<EOF | oc --kubeconfig="$KUBECONFIG" create -f -
apiVersion: cdi.kubevirt.io/v1beta1
kind: DataVolume
metadata:
  name: windows
  namespace: ${SERVICE_ACCOUNT_NAMESPACE}
  annotations:
    cdi.kubevirt.io/storage.usePopulator: "false"
spec:
  contentType: kubevirt
  source:
    http:
      url: "${PRESIGNED_URL}"
  storage:
    accessModes:
      - ReadWriteOnce
    resources:
      requests:
        storage: 70Gi
    storageClassName: ${STORAGE_CLASS}
EOF

log_info "✓ DataVolume created, waiting for import to complete..."

# Wait for import (40-50 minutes)
MAX_WAIT=3600  # 1 hour
WAIT_TIME=0
while [ $WAIT_TIME -lt $MAX_WAIT ]; do
    DV_PHASE=$(oc --kubeconfig="$KUBECONFIG" get datavolume windows -n $SERVICE_ACCOUNT_NAMESPACE -o jsonpath='{.status.phase}' 2>/dev/null || echo "NotFound")
    DV_PROGRESS=$(oc --kubeconfig="$KUBECONFIG" get datavolume windows -n $SERVICE_ACCOUNT_NAMESPACE -o jsonpath='{.status.progress}' 2>/dev/null || echo "N/A")

    if [ "$DV_PHASE" = "Succeeded" ]; then
        log_info "✓ Windows image import completed"
        break
    elif [ "$DV_PHASE" = "Failed" ]; then
        log_error "DataVolume import failed"
        exit 1
    fi

    # Log progress every 5 minutes
    if [ $((WAIT_TIME % 300)) -eq 0 ] && [ $WAIT_TIME -gt 0 ]; then
        ELAPSED_MIN=$((WAIT_TIME / 60))
        log_info "  Import status: $DV_PHASE | Progress: $DV_PROGRESS | Elapsed: ${ELAPSED_MIN}m"
    fi

    sleep 10
    WAIT_TIME=$((WAIT_TIME + 10))
done

if [ $WAIT_TIME -ge $MAX_WAIT ]; then
    log_error "Import timeout"
    exit 1
fi

# Create VolumeSnapshot from imported PVC
log_info ""
log_info "═══════════════════════════════════════════════════════════════"
log_info "Creating VolumeSnapshot from imported PVC..."
log_info "═══════════════════════════════════════════════════════════════"

# Discover VolumeSnapshotClass
SNAPSHOT_CLASS=$(oc --kubeconfig="$KUBECONFIG" get volumesnapshotclass -o jsonpath='{.items[?(@.driver=="ebs.csi.aws.com")].metadata.name}' 2>/dev/null | awk '{print $1}')

if [ -z "$SNAPSHOT_CLASS" ]; then
    log_error "Could not find VolumeSnapshotClass for ebs.csi.aws.com"
    exit 1
fi

log_info "✓ Using VolumeSnapshotClass: $SNAPSHOT_CLASS"

# Create VolumeSnapshot
SNAPSHOT_NAME="windows-golden-snapshot-temp"
cat <<EOF | oc --kubeconfig="$KUBECONFIG" apply -f -
apiVersion: snapshot.storage.k8s.io/v1
kind: VolumeSnapshot
metadata:
  name: ${SNAPSHOT_NAME}
  namespace: ${SERVICE_ACCOUNT_NAMESPACE}
  labels:
    ocpctl.mg.dog8code.com/managed: "true"
    ocpctl.mg.dog8code.com/image-version: "${SNAPSHOT_VERSION}"
spec:
  volumeSnapshotClassName: ${SNAPSHOT_CLASS}
  source:
    persistentVolumeClaimName: windows
EOF

log_info "✓ VolumeSnapshot created, waiting for readyToUse..."

# Wait for VolumeSnapshot to become ready (40-50 minutes)
MAX_WAIT=3600
WAIT_TIME=0
while [ $WAIT_TIME -lt $MAX_WAIT ]; do
    READY_TO_USE=$(oc --kubeconfig="$KUBECONFIG" get volumesnapshot $SNAPSHOT_NAME -n $SERVICE_ACCOUNT_NAMESPACE \
        -o jsonpath='{.status.readyToUse}' 2>/dev/null || echo "false")

    if [ "$READY_TO_USE" = "true" ]; then
        log_info "✓ VolumeSnapshot is ready"
        break
    fi

    # Check for errors
    SNAPSHOT_ERROR=$(oc --kubeconfig="$KUBECONFIG" get volumesnapshot $SNAPSHOT_NAME -n $SERVICE_ACCOUNT_NAMESPACE \
        -o jsonpath='{.status.error.message}' 2>/dev/null || echo "")

    if [ -n "$SNAPSHOT_ERROR" ]; then
        log_error "VolumeSnapshot failed: $SNAPSHOT_ERROR"
        exit 1
    fi

    # Log progress every 5 minutes
    if [ $((WAIT_TIME % 300)) -eq 0 ] && [ $WAIT_TIME -gt 0 ]; then
        ELAPSED_MIN=$((WAIT_TIME / 60))
        log_info "  Snapshot creation in progress... Elapsed: ${ELAPSED_MIN}m"
    fi

    sleep 10
    WAIT_TIME=$((WAIT_TIME + 10))
done

if [ $WAIT_TIME -ge $MAX_WAIT ]; then
    log_error "Snapshot creation timeout"
    exit 1
fi

# Extract EBS snapshot ID
log_info "Extracting EBS snapshot ID..."
VOLUME_SNAPSHOT_CONTENT=$(oc --kubeconfig="$KUBECONFIG" get volumesnapshot $SNAPSHOT_NAME -n $SERVICE_ACCOUNT_NAMESPACE \
    -o jsonpath='{.status.boundVolumeSnapshotContentName}' 2>/dev/null)

if [ -z "$VOLUME_SNAPSHOT_CONTENT" ]; then
    log_error "Could not get VolumeSnapshotContent"
    exit 1
fi

EBS_SNAPSHOT_ID=$(oc --kubeconfig="$KUBECONFIG" get volumesnapshotcontent "$VOLUME_SNAPSHOT_CONTENT" \
    -o jsonpath='{.status.snapshotHandle}' 2>/dev/null)

if [ -z "$EBS_SNAPSHOT_ID" ]; then
    log_error "Could not extract EBS snapshot ID"
    exit 1
fi

log_info "✓ EBS Snapshot ID: $EBS_SNAPSHOT_ID"

# Add annotation for K8s 1.30+
oc --kubeconfig="$KUBECONFIG" annotate volumesnapshotcontent "$VOLUME_SNAPSHOT_CONTENT" \
    snapshot.storage.kubernetes.io/allow-volume-mode-change=true --overwrite 2>/dev/null || true

# Validate snapshot by creating and booting a test VM
log_info ""
log_info "═══════════════════════════════════════════════════════════════"
log_info "Validating snapshot by booting test VM..."
log_info "═══════════════════════════════════════════════════════════════"

# Create validation PVC from snapshot
cat <<EOF | oc --kubeconfig="$KUBECONFIG" apply -f -
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: windows-validation-disk
  namespace: ${SERVICE_ACCOUNT_NAMESPACE}
spec:
  accessModes:
    - ReadWriteOnce
  volumeMode: Block
  storageClassName: ${STORAGE_CLASS}
  resources:
    requests:
      storage: 70Gi
  dataSource:
    apiGroup: snapshot.storage.k8s.io
    kind: VolumeSnapshot
    name: ${SNAPSHOT_NAME}
EOF

log_info "Waiting for validation PVC to bind..."
MAX_WAIT=300
WAIT_TIME=0
while [ $WAIT_TIME -lt $MAX_WAIT ]; do
    VAL_PVC_PHASE=$(oc --kubeconfig="$KUBECONFIG" get pvc windows-validation-disk -n $SERVICE_ACCOUNT_NAMESPACE -o jsonpath='{.status.phase}' 2>/dev/null || echo "NotFound")

    if [ "$VAL_PVC_PHASE" = "Bound" ]; then
        log_info "✓ Validation PVC bound"
        break
    elif [ "$VAL_PVC_PHASE" = "Failed" ] || [ "$VAL_PVC_PHASE" = "Lost" ]; then
        log_error "Validation PVC failed"
        exit 1
    fi

    sleep 5
    WAIT_TIME=$((WAIT_TIME + 5))
done

if [ $WAIT_TIME -ge $MAX_WAIT ]; then
    log_error "Validation PVC bind timeout"
    exit 1
fi

# Create validation VM
cat <<EOF | oc --kubeconfig="$KUBECONFIG" apply -f -
apiVersion: kubevirt.io/v1
kind: VirtualMachine
metadata:
  name: windows-validation-vm
  namespace: ${SERVICE_ACCOUNT_NAMESPACE}
spec:
  running: true
  template:
    metadata:
      labels:
        kubevirt.io/vm: windows-validation-vm
    spec:
      domain:
        cpu:
          cores: 2
        devices:
          disks:
          - disk:
              bus: sata
            name: rootdisk
          interfaces:
          - masquerade: {}
            name: default
            model: e1000
        machine:
          type: pc-q35-rhel9.2.0
        memory:
          guest: 4Gi
        resources:
          requests:
            memory: 4Gi
      networks:
      - name: default
        pod: {}
      affinity:
        nodeAffinity:
          requiredDuringSchedulingIgnoredDuringExecution:
            nodeSelectorTerms:
            - matchExpressions:
              - key: topology.kubernetes.io/zone
                operator: In
                values:
                - ${IMPORT_ZONE}
      volumes:
      - name: rootdisk
        persistentVolumeClaim:
          claimName: windows-validation-disk
EOF

log_info "Waiting for validation VM to boot..."
MAX_WAIT=600
WAIT_TIME=0
STABLE_COUNT=0

while [ $WAIT_TIME -lt $MAX_WAIT ]; do
    VM_STATUS=$(oc --kubeconfig="$KUBECONFIG" get vm windows-validation-vm -n $SERVICE_ACCOUNT_NAMESPACE -o jsonpath='{.status.printableStatus}' 2>/dev/null || echo "Unknown")

    if [ "$VM_STATUS" = "Running" ]; then
        STABLE_COUNT=$((STABLE_COUNT + 1))
        if [ $STABLE_COUNT -ge 3 ]; then
            log_info "✓ Validation VM is running and stable"
            break
        fi
    else
        STABLE_COUNT=0
        if [ "$VM_STATUS" = "Failed" ] || [ "$VM_STATUS" = "Error" ]; then
            log_error "Validation VM failed"
            exit 1
        fi
    fi

    if [ $((WAIT_TIME % 30)) -eq 0 ] && [ $WAIT_TIME -gt 0 ]; then
        log_info "  VM status: $VM_STATUS"
    fi

    sleep 10
    WAIT_TIME=$((WAIT_TIME + 10))
done

if [ $STABLE_COUNT -lt 3 ]; then
    log_error "Validation VM did not stabilize"
    exit 1
fi

log_info "✓ Snapshot validation successful!"

# Tag and publish to SSM
log_info ""
log_info "═══════════════════════════════════════════════════════════════"
log_info "Publishing snapshot to SSM Parameter Store..."
log_info "═══════════════════════════════════════════════════════════════"

# Tag EBS snapshot
aws ec2 create-tags --region "$REGION" --resources "$EBS_SNAPSHOT_ID" \
    --tags \
        "Key=Name,Value=ocpctl-windows-10-oadp-v${SNAPSHOT_VERSION}" \
        "Key=ocpctl:managed,Value=true" \
        "Key=ocpctl:image-version,Value=${SNAPSHOT_VERSION}" \
        "Key=ocpctl:validated,Value=true" \
        "Key=ocpctl:region,Value=${REGION}" \
    2>/dev/null || log_warn "Could not tag snapshot"

# Store in SSM
SSM_PARAMETER_PATH="/ocpctl/windows-snapshots/${SNAPSHOT_VERSION}/${REGION}"
aws ssm put-parameter --name "$SSM_PARAMETER_PATH" --value "$EBS_SNAPSHOT_ID" --type String --overwrite --region "$REGION" 2>/dev/null || {
    log_error "Failed to publish to SSM"
    exit 1
}

log_info "✓ Published to SSM: $SSM_PARAMETER_PATH → $EBS_SNAPSHOT_ID"

# Output results (for worker to parse)
log_info ""
log_info "═══════════════════════════════════════════════════════════════"
log_info "✅ Regional Snapshot Creation Complete!"
log_info "═══════════════════════════════════════════════════════════════"
log_info "EBS Snapshot ID: $EBS_SNAPSHOT_ID"
log_info "SSM Parameter: $SSM_PARAMETER_PATH"
log_info "Region: $REGION"
log_info "Version: $SNAPSHOT_VERSION"
log_info "═══════════════════════════════════════════════════════════════"

# Write machine-readable output
echo "EBS_SNAPSHOT_ID=$EBS_SNAPSHOT_ID"
echo "SSM_PARAMETER_PATH=$SSM_PARAMETER_PATH"

exit 0
