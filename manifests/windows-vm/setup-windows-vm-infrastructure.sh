#!/bin/bash
#
# Setup Windows VM Infrastructure (Post-Deployment)
#
# This script is called automatically during cluster post-deployment when CNV addon is selected.
# It receives cluster information via environment variables:
#   - CLUSTER_ID: Cluster UUID
#   - CLUSTER_NAME: Cluster name
#   - INFRA_ID: OpenShift infrastructure ID
#   - REGION: AWS region
#   - KUBECONFIG: Path to kubeconfig file
#
# The script:
# 1. Creates IAM role/user with S3 read-only permissions (IRSA or Mint mode)
# 2. Configures trust policy for the cluster's OIDC provider
# 3. Detects pre-created regional EBS snapshot (fast path) or falls back to S3 (slow path)
# 4. Imports Windows image via DataVolume (from snapshot or S3)
# 5. Creates VM template for users
#
# Note: This script does NOT create regional EBS snapshots.
# Use create-regional-snapshot.sh for explicit regional snapshot creation.
#

set -e
set -o pipefail

# Setup detailed logging to file for debugging
LOG_DIR="/var/lib/ocpctl/clusters/${CLUSTER_ID:-unknown}"
mkdir -p "$LOG_DIR"
SCRIPT_LOG="$LOG_DIR/windows-vm-setup.log"

# Redirect all output (stdout and stderr) to both console and log file
exec > >(tee -a "$SCRIPT_LOG") 2>&1

# Log script start with timestamp
echo "================================================================================"
echo "Windows VM Infrastructure Setup - Started at $(date -u +"%Y-%m-%d %H:%M:%S UTC")"
echo "Cluster ID: ${CLUSTER_ID:-unknown}"
echo "Job ID: ${JOB_ID:-unknown}"
echo "Attempt: ${ATTEMPT:-1}"
echo "Log file: $SCRIPT_LOG"
echo "================================================================================"
echo ""

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

# Track script start time for duration calculation
SCRIPT_START_TIME=$(date +%s)

# Trap to log script completion/failure
cleanup_and_log_exit() {
    EXIT_CODE=$?
    SCRIPT_END_TIME=$(date +%s)
    DURATION=$((SCRIPT_END_TIME - SCRIPT_START_TIME))

    echo ""
    echo "================================================================================"
    if [ $EXIT_CODE -eq 0 ]; then
        echo "Windows VM Infrastructure Setup - COMPLETED SUCCESSFULLY"
    else
        echo "Windows VM Infrastructure Setup - FAILED with exit code $EXIT_CODE"
    fi
    echo "Ended at: $(date -u +"%Y-%m-%d %H:%M:%S UTC")"
    echo "Duration: ${DURATION} seconds"
    echo "Full logs saved to: $SCRIPT_LOG"
    echo "================================================================================"
}

trap cleanup_and_log_exit EXIT

# Verify required environment variables
if [ -z "$INFRA_ID" ] || [ -z "$REGION" ] || [ -z "$KUBECONFIG" ]; then
    log_error "Missing required environment variables"
    log_error "Required: INFRA_ID, REGION, KUBECONFIG"
    exit 1
fi

log_info "Starting Windows VM infrastructure setup"
log_info "Cluster: $CLUSTER_NAME (ID: $CLUSTER_ID)"
log_info "Infrastructure ID: $INFRA_ID"
log_info "Region: $REGION"

# Configuration
ROLE_NAME="ocpctl-win-s3-${CLUSTER_ID}"
SERVICE_ACCOUNT_NAME="windows-image-importer"
SERVICE_ACCOUNT_NAMESPACE="openshift-virtualization-os-images"

# Get AWS account ID
AWS_ACCOUNT_ID=$(aws sts get-caller-identity --query Account --output text)
log_info "AWS Account ID: $AWS_ACCOUNT_ID"

# Detect credentials mode (Manual vs Mint)
OIDC_PROVIDER="${INFRA_ID}-oidc.s3.${REGION}.amazonaws.com"
OIDC_PROVIDER_ARN="arn:aws:iam::${AWS_ACCOUNT_ID}:oidc-provider/${OIDC_PROVIDER}"

if aws iam get-open-id-connect-provider --open-id-connect-provider-arn "$OIDC_PROVIDER_ARN" &>/dev/null; then
    # Manual mode (OIDC provider exists)
    CREDENTIALS_MODE="manual"
    log_info "✓ OIDC provider detected: $OIDC_PROVIDER"
    log_info "Using Manual mode (IRSA with IAM role)"
else
    # Mint mode (no OIDC provider - pre-release clusters)
    CREDENTIALS_MODE="mint"
    log_info "OIDC provider not found - using Mint mode"
    log_info "Using Mint mode (IAM user with access keys)"
fi

##############################################################################
# Manual Mode Setup (IAM Role + OIDC)
##############################################################################
if [ "$CREDENTIALS_MODE" = "manual" ]; then

# Create trust policy document
TRUST_POLICY=$(cat <<EOF
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Principal": {
        "Federated": "${OIDC_PROVIDER_ARN}"
      },
      "Action": "sts:AssumeRoleWithWebIdentity",
      "Condition": {
        "StringEquals": {
          "${OIDC_PROVIDER}:sub": "system:serviceaccount:${SERVICE_ACCOUNT_NAMESPACE}:${SERVICE_ACCOUNT_NAME}"
        }
      }
    }
  ]
}
EOF
)

# Create the IAM role
log_info "Creating IAM role: $ROLE_NAME"
if aws iam get-role --role-name "$ROLE_NAME" &>/dev/null; then
    log_warn "Role already exists, updating trust policy"
    echo "$TRUST_POLICY" > /tmp/trust-policy-${CLUSTER_ID}.json
    aws iam update-assume-role-policy \
        --role-name "$ROLE_NAME" \
        --policy-document file:///tmp/trust-policy-${CLUSTER_ID}.json
    rm /tmp/trust-policy-${CLUSTER_ID}.json
else
    echo "$TRUST_POLICY" > /tmp/trust-policy-${CLUSTER_ID}.json
    aws iam create-role \
        --role-name "$ROLE_NAME" \
        --assume-role-policy-document file:///tmp/trust-policy-${CLUSTER_ID}.json \
        --description "IRSA role for CDI to download Windows images from S3 (Cluster: $CLUSTER_NAME)" \
        --tags Key=ClusterID,Value=$CLUSTER_ID Key=ClusterName,Value=$CLUSTER_NAME Key=ManagedBy,Value=ocpctl
    rm /tmp/trust-policy-${CLUSTER_ID}.json
    log_info "✓ Role created"
fi

# Attach S3 read-only policy
log_info "Attaching S3 read-only policy"

# Get script directory to find iam-policy.json
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

aws iam put-role-policy \
    --role-name "$ROLE_NAME" \
    --policy-name S3WindowsImageReadOnly \
    --policy-document file://${SCRIPT_DIR}/iam-policy.json
log_info "✓ Policy attached"

# Get role ARN
ROLE_ARN=$(aws iam get-role --role-name "$ROLE_NAME" --query 'Role.Arn' --output text)
log_info "✓ IAM Role ARN: $ROLE_ARN"

# Create namespace
log_info "Creating namespace: $SERVICE_ACCOUNT_NAMESPACE"
oc --kubeconfig="$KUBECONFIG" create namespace "$SERVICE_ACCOUNT_NAMESPACE" --dry-run=client -o yaml | \
    oc --kubeconfig="$KUBECONFIG" apply -f -
log_info "✓ Namespace ready"

# Create ServiceAccount with IAM role annotation
log_info "Creating ServiceAccount: $SERVICE_ACCOUNT_NAME"
cat <<EOF | oc --kubeconfig="$KUBECONFIG" apply -f -
apiVersion: v1
kind: ServiceAccount
metadata:
  name: ${SERVICE_ACCOUNT_NAME}
  namespace: ${SERVICE_ACCOUNT_NAMESPACE}
  annotations:
    eks.amazonaws.com/role-arn: ${ROLE_ARN}
EOF
log_info "✓ ServiceAccount created"

# Note: CDI importer pods use the 'default' service account, not the custom one
# We need to annotate the default SA and update the trust policy
log_info "Annotating default ServiceAccount with IAM role..."
oc --kubeconfig="$KUBECONFIG" annotate sa default -n ${SERVICE_ACCOUNT_NAMESPACE} \
    eks.amazonaws.com/role-arn=${ROLE_ARN} --overwrite

# Update trust policy to allow both service accounts
log_info "Updating IAM trust policy to allow default service account..."
TRUST_POLICY_UPDATED=$(cat <<EOF
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Principal": {
        "Federated": "${OIDC_PROVIDER_ARN}"
      },
      "Action": "sts:AssumeRoleWithWebIdentity",
      "Condition": {
        "StringEquals": {
          "${OIDC_PROVIDER}:sub": [
            "system:serviceaccount:${SERVICE_ACCOUNT_NAMESPACE}:${SERVICE_ACCOUNT_NAME}",
            "system:serviceaccount:${SERVICE_ACCOUNT_NAMESPACE}:default"
          ]
        }
      }
    }
  ]
}
EOF
)
echo "$TRUST_POLICY_UPDATED" > /tmp/trust-policy-${CLUSTER_ID}-updated.json
aws iam update-assume-role-policy \
    --role-name "$ROLE_NAME" \
    --policy-document file:///tmp/trust-policy-${CLUSTER_ID}-updated.json
rm /tmp/trust-policy-${CLUSTER_ID}-updated.json
log_info "✓ Trust policy updated"

##############################################################################
# Mint Mode Setup (IAM User + Access Keys + Secret)
##############################################################################
else
    # Mint mode: Create IAM user with access keys instead of role
    IAM_USER_NAME="ocpctl-win-s3-${CLUSTER_ID}"

    log_info "Creating IAM user: $IAM_USER_NAME"
    if aws iam get-user --user-name "$IAM_USER_NAME" &>/dev/null; then
        log_warn "IAM user already exists: $IAM_USER_NAME"
    else
        aws iam create-user \
            --user-name "$IAM_USER_NAME" \
            --tags Key=ClusterID,Value=$CLUSTER_ID Key=ClusterName,Value=$CLUSTER_NAME Key=ManagedBy,Value=ocpctl
        log_info "✓ IAM user created"
    fi

    # Attach S3 read-only policy to user
    log_info "Attaching S3 read-only policy to IAM user"
    SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

    if aws iam get-user-policy --user-name "$IAM_USER_NAME" --policy-name S3WindowsImageReadOnly &>/dev/null; then
        log_info "✓ Policy already attached to IAM user"
    else
        if aws iam put-user-policy \
            --user-name "$IAM_USER_NAME" \
            --policy-name S3WindowsImageReadOnly \
            --policy-document file://${SCRIPT_DIR}/iam-policy.json 2>&1; then
            log_info "✓ Policy attached to IAM user"
        else
            if aws iam get-user-policy --user-name "$IAM_USER_NAME" --policy-name S3WindowsImageReadOnly &>/dev/null; then
                log_warn "Policy already exists (attached by external process)"
            else
                log_error "Failed to attach policy"
                exit 1
            fi
        fi
    fi

    # Create access keys
    log_info "Creating access keys for IAM user"
    EXISTING_KEYS=$(aws iam list-access-keys --user-name "$IAM_USER_NAME" --query 'AccessKeyMetadata[*].AccessKeyId' --output text)
    for key in $EXISTING_KEYS; do
        log_warn "Deleting existing access key: $key"
        aws iam delete-access-key --user-name "$IAM_USER_NAME" --access-key-id "$key"
    done

    ACCESS_KEY_OUTPUT=$(aws iam create-access-key --user-name "$IAM_USER_NAME" --output json)
    AWS_ACCESS_KEY_ID=$(echo "$ACCESS_KEY_OUTPUT" | jq -r '.AccessKey.AccessKeyId')
    AWS_SECRET_ACCESS_KEY=$(echo "$ACCESS_KEY_OUTPUT" | jq -r '.AccessKey.SecretAccessKey')
    log_info "✓ Access keys created"

    # Create namespace
    log_info "Creating namespace: $SERVICE_ACCOUNT_NAMESPACE"
    oc --kubeconfig="$KUBECONFIG" create namespace "$SERVICE_ACCOUNT_NAMESPACE" --dry-run=client -o yaml | \
        oc --kubeconfig="$KUBECONFIG" apply -f -
    log_info "✓ Namespace ready"

    # Create Secret with AWS credentials for CDI
    log_info "Creating Secret with AWS credentials for CDI"
    cat <<EOF | oc --kubeconfig="$KUBECONFIG" apply -f -
apiVersion: v1
kind: Secret
metadata:
  name: cdi-s3-import-creds
  namespace: ${SERVICE_ACCOUNT_NAMESPACE}
type: Opaque
stringData:
  accessKeyId: ${AWS_ACCESS_KEY_ID}
  secretKey: ${AWS_SECRET_ACCESS_KEY}
EOF
    log_info "✓ Secret created with AWS credentials"

    # Unset credentials
    unset AWS_ACCESS_KEY_ID
    unset AWS_SECRET_ACCESS_KEY
fi

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

    if [ $((ELAPSED % 30)) -eq 0 ]; then
        log_info "Still waiting for CDI API... (${ELAPSED}s elapsed)"
    fi

    sleep 5
    ELAPSED=$((ELAPSED + 5))
done

if [ $ELAPSED -ge $MAX_WAIT ]; then
    log_error "Timeout waiting for CDI API to be ready after ${MAX_WAIT}s"
    exit 1
fi

##############################################################################
# Regional Snapshot Detection (Fast Path)
##############################################################################

log_info "Checking for pre-created regional Windows image EBS snapshot in $REGION..."

SNAPSHOT_VERSION="1.0"
SNAPSHOT_ID=""
IMPORT_METHOD="s3"  # Default to S3 fallback

# Try SSM Parameter Store first
if command -v aws &> /dev/null; then
    SNAPSHOT_PARAM="/ocpctl/windows-snapshots/${SNAPSHOT_VERSION}/${REGION}"

    SSM_ATTEMPTS=0
    SSM_MAX_ATTEMPTS=3
    while [ $SSM_ATTEMPTS -lt $SSM_MAX_ATTEMPTS ]; do
        SSM_ATTEMPTS=$((SSM_ATTEMPTS + 1))

        set +e
        SSM_OUTPUT=$(aws ssm get-parameter --name "$SNAPSHOT_PARAM" --region "$REGION" --query 'Parameter.Value' --output text 2>&1)
        SSM_EXIT_CODE=$?
        set -e

        if [ $SSM_EXIT_CODE -eq 0 ] && [ -n "$SSM_OUTPUT" ] && [ "$SSM_OUTPUT" != "None" ]; then
            SNAPSHOT_ID="$SSM_OUTPUT"
            log_info "✓ Found snapshot via SSM Parameter Store: $SNAPSHOT_ID"
            break
        fi

        if [[ "$SSM_OUTPUT" == *"ParameterNotFound"* ]]; then
            log_info "SSM parameter $SNAPSHOT_PARAM does not exist"
            break
        fi

        if [ $SSM_ATTEMPTS -lt $SSM_MAX_ATTEMPTS ]; then
            BACKOFF=$((2 ** SSM_ATTEMPTS))
            log_info "Retrying SSM lookup in ${BACKOFF}s..."
            sleep $BACKOFF
        fi
    done

    # Fallback: Query snapshots by tags
    if [ -z "$SNAPSHOT_ID" ] || [ "$SNAPSHOT_ID" = "None" ]; then
        log_info "SSM parameter not found, checking EBS snapshots by tags..."

        EC2_ATTEMPTS=0
        EC2_MAX_ATTEMPTS=3
        while [ $EC2_ATTEMPTS -lt $EC2_MAX_ATTEMPTS ]; do
            EC2_ATTEMPTS=$((EC2_ATTEMPTS + 1))

            set +e
            EC2_OUTPUT=$(aws ec2 describe-snapshots \
                --region "$REGION" \
                --filters "Name=tag:ocpctl:managed,Values=true" \
                          "Name=tag:ocpctl:image-version,Values=${SNAPSHOT_VERSION}" \
                          "Name=status,Values=completed" \
                --owner-ids self \
                --query 'reverse(sort_by(Snapshots, &StartTime))[0].SnapshotId' \
                --output text 2>&1)
            EC2_EXIT_CODE=$?
            set -e

            if [ $EC2_EXIT_CODE -eq 0 ] && [ -n "$EC2_OUTPUT" ] && [ "$EC2_OUTPUT" != "None" ]; then
                SNAPSHOT_ID="$EC2_OUTPUT"
                log_info "✓ Found snapshot via EC2 tags: $SNAPSHOT_ID"
                break
            fi

            if [ $EC2_ATTEMPTS -lt $EC2_MAX_ATTEMPTS ]; then
                BACKOFF=$((2 ** EC2_ATTEMPTS))
                log_info "Retrying EC2 snapshot query in ${BACKOFF}s..."
                sleep $BACKOFF
            fi
        done
    fi
fi

# Validate snapshot exists and is completed
if [ -n "$SNAPSHOT_ID" ] && [ "$SNAPSHOT_ID" != "None" ]; then
    SNAPSHOT_STATE=$(aws ec2 describe-snapshots --snapshot-ids "$SNAPSHOT_ID" --region "$REGION" --query 'Snapshots[0].State' --output text 2>&1 || echo "error")

    if [ "$SNAPSHOT_STATE" = "completed" ]; then
        IMPORT_METHOD="snapshot"
        log_info "✓ Validated snapshot is ready: $SNAPSHOT_ID (state: completed)"
        log_info "  Using fast snapshot-based import (expected: 2-3 minutes)"
    else
        log_warn "Snapshot $SNAPSHOT_ID exists but is not completed (state: $SNAPSHOT_STATE)"
        log_warn "Falling back to S3 import"
        SNAPSHOT_ID=""
    fi
fi

if [ -z "$SNAPSHOT_ID" ] || [ "$SNAPSHOT_ID" = "None" ]; then
    log_info "⚠ No usable regional snapshot found in $REGION"
    log_info "  Using S3 download fallback (expected: 30-50 minutes)"
    log_info "  To create a regional snapshot for faster future deployments:"
    log_info "  Use the Windows Snapshot Management UI or run create-regional-snapshot.sh"
fi

##############################################################################
# Storage Class Detection
##############################################################################

log_info "Detecting available storage class..."
if oc --kubeconfig="$KUBECONFIG" get storageclass ocs-storagecluster-ceph-rbd-virtualization &>/dev/null; then
    STORAGE_CLASS="ocs-storagecluster-ceph-rbd-virtualization"
    ACCESS_MODE="ReadWriteMany"
    log_info "✓ Using ODF storage: $STORAGE_CLASS (supports live migration)"
elif oc --kubeconfig="$KUBECONFIG" get storageclass gp3-csi &>/dev/null; then
    # Detect worker zones
    log_info "Detecting worker node zones..."
    WORKER_ZONES=$(oc --kubeconfig="$KUBECONFIG" get nodes -l node-role.kubernetes.io/worker \
        -o jsonpath='{range .items[*]}{.metadata.labels.topology\.kubernetes\.io/zone}{"\n"}{end}' | sort -u)

    if [ -z "$WORKER_ZONES" ]; then
        log_error "No worker nodes found in cluster"
        exit 1
    fi

    IMPORT_ZONE=$(echo "$WORKER_ZONES" | head -1)
    log_info "✓ Detected worker zones: $(echo $WORKER_ZONES | tr '\n' ', ')"
    log_info "✓ Using zone $IMPORT_ZONE for DataVolume import"

    # Create zone-constrained storage class
    IMPORT_STORAGE_CLASS="gp3-csi-immediate-${IMPORT_ZONE}"
    if ! oc --kubeconfig="$KUBECONFIG" get storageclass "$IMPORT_STORAGE_CLASS" &>/dev/null; then
        log_info "Creating zone-constrained storage class: $IMPORT_STORAGE_CLASS"
        cat <<EOF | oc --kubeconfig="$KUBECONFIG" apply -f -
apiVersion: storage.k8s.io/v1
kind: StorageClass
metadata:
  name: ${IMPORT_STORAGE_CLASS}
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

    STORAGE_CLASS="$IMPORT_STORAGE_CLASS"
    ACCESS_MODE="ReadWriteOnce"
    log_info "✓ Using zone-constrained storage: $STORAGE_CLASS"
elif oc --kubeconfig="$KUBECONFIG" get storageclass gp2-csi &>/dev/null; then
    if ! oc --kubeconfig="$KUBECONFIG" get storageclass gp2-csi-immediate &>/dev/null; then
        log_info "Creating gp2-csi-immediate storage class..."
        cat <<EOF | oc --kubeconfig="$KUBECONFIG" apply -f -
apiVersion: storage.k8s.io/v1
kind: StorageClass
metadata:
  name: gp2-csi-immediate
allowVolumeExpansion: true
parameters:
  encrypted: "true"
  type: gp2
provisioner: ebs.csi.aws.com
reclaimPolicy: Delete
volumeBindingMode: Immediate
EOF
    fi
    STORAGE_CLASS="gp2-csi-immediate"
    ACCESS_MODE="ReadWriteOnce"
    log_info "✓ Using AWS EBS storage: $STORAGE_CLASS"
else
    STORAGE_CLASS=$(oc --kubeconfig="$KUBECONFIG" get storageclass -o jsonpath='{.items[?(@.metadata.annotations.storageclass\.kubernetes\.io/is-default-class=="true")].metadata.name}')
    if [ -z "$STORAGE_CLASS" ]; then
        log_error "No suitable storage class found"
        exit 1
    fi
    ACCESS_MODE="ReadWriteOnce"
    log_info "✓ Using default storage class: $STORAGE_CLASS"
fi

##############################################################################
# Import Windows Image
##############################################################################

# Check if DataVolume already exists
EXISTING_DV_PHASE=$(oc --kubeconfig="$KUBECONFIG" get datavolume windows -n $SERVICE_ACCOUNT_NAMESPACE -o jsonpath='{.status.phase}' 2>/dev/null || echo "NotFound")

if [ "$EXISTING_DV_PHASE" = "Succeeded" ]; then
    log_info "DataVolume 'windows' already exists and has succeeded - skipping creation"
elif [ "$EXISTING_DV_PHASE" != "NotFound" ]; then
    log_warn "DataVolume 'windows' exists with phase: $EXISTING_DV_PHASE"
    log_warn "Deleting and recreating..."
    oc --kubeconfig="$KUBECONFIG" delete datavolume windows -n $SERVICE_ACCOUNT_NAMESPACE --wait=true
    log_info "✓ Old DataVolume deleted"
fi

if [ "$EXISTING_DV_PHASE" != "Succeeded" ]; then
    log_info "Creating DataVolume for Windows image (method: $IMPORT_METHOD, storage: $STORAGE_CLASS)"

    if [ "$IMPORT_METHOD" = "snapshot" ]; then
        # Fast path: Use EBS snapshot
        log_info "Creating DataVolume from EBS snapshot $SNAPSHOT_ID..."

        # Clean up any existing snapshot resources
        oc --kubeconfig="$KUBECONFIG" delete volumesnapshot windows-source-snapshot -n $SERVICE_ACCOUNT_NAMESPACE --ignore-not-found=true 2>/dev/null || true
        oc --kubeconfig="$KUBECONFIG" delete volumesnapshotcontent windows-source-snapshot-content --ignore-not-found=true 2>/dev/null || true

        # Create VolumeSnapshotContent
        cat <<EOF | oc --kubeconfig="$KUBECONFIG" apply -f -
apiVersion: snapshot.storage.k8s.io/v1
kind: VolumeSnapshotContent
metadata:
  name: windows-source-snapshot-content
  annotations:
    snapshot.storage.kubernetes.io/allow-volume-mode-change: "true"
spec:
  deletionPolicy: Retain
  driver: ebs.csi.aws.com
  source:
    snapshotHandle: ${SNAPSHOT_ID}
  sourceVolumeMode: Block
  volumeSnapshotClassName: csi-aws-vsc
  volumeSnapshotRef:
    name: windows-source-snapshot
    namespace: ${SERVICE_ACCOUNT_NAMESPACE}
EOF

        # Create VolumeSnapshot
        cat <<EOF | oc --kubeconfig="$KUBECONFIG" apply -f -
apiVersion: snapshot.storage.k8s.io/v1
kind: VolumeSnapshot
metadata:
  name: windows-source-snapshot
  namespace: ${SERVICE_ACCOUNT_NAMESPACE}
spec:
  volumeSnapshotClassName: csi-aws-vsc
  source:
    volumeSnapshotContentName: windows-source-snapshot-content
EOF

        # Create PVC from snapshot
        EXISTING_PVC=$(oc --kubeconfig="$KUBECONFIG" get pvc windows -n $SERVICE_ACCOUNT_NAMESPACE -o jsonpath='{.status.phase}' 2>/dev/null || echo "NotFound")

        if [ "$EXISTING_PVC" = "Bound" ]; then
            log_info "PVC 'windows' already exists and is Bound"
        elif [ "$EXISTING_PVC" != "NotFound" ]; then
            log_warn "PVC 'windows' exists with phase: $EXISTING_PVC - deleting and recreating"
            oc --kubeconfig="$KUBECONFIG" delete pvc windows -n $SERVICE_ACCOUNT_NAMESPACE --wait=true
            EXISTING_PVC="NotFound"
        fi

        if [ "$EXISTING_PVC" != "Bound" ]; then
            cat <<EOF | oc --kubeconfig="$KUBECONFIG" create -f -
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: windows
  namespace: ${SERVICE_ACCOUNT_NAMESPACE}
  annotations:
    ocpctl.io/import-method: "snapshot"
    ocpctl.io/snapshot-id: "${SNAPSHOT_ID}"
spec:
  accessModes:
    - ${ACCESS_MODE}
  volumeMode: Block
  resources:
    requests:
      storage: 70Gi
  storageClassName: ${STORAGE_CLASS}
  dataSourceRef:
    apiGroup: snapshot.storage.k8s.io
    kind: VolumeSnapshot
    name: windows-source-snapshot
EOF
            log_info "✓ PVC created from snapshot (CSI restore starting - 2-3 minutes)"
        fi

    else
        # Slow path: S3 download
        if [ "$CREDENTIALS_MODE" = "manual" ]; then
            log_info "Generating presigned URL for Windows image..."
            PRESIGNED_URL=$(aws s3 presign s3://ocpctl-binaries/windows-images/windows-10-oadp.qcow2 --expires-in 86400 --region us-east-1)

            cat <<EOF | oc --kubeconfig="$KUBECONFIG" create -f -
apiVersion: cdi.kubevirt.io/v1beta1
kind: DataVolume
metadata:
  name: windows
  namespace: ${SERVICE_ACCOUNT_NAMESPACE}
  annotations:
    cdi.kubevirt.io/storage.usePopulator: "false"
    ocpctl.io/import-method: "s3"
spec:
  contentType: kubevirt
  source:
    http:
      url: "${PRESIGNED_URL}"
  storage:
    accessModes:
      - ${ACCESS_MODE}
    resources:
      requests:
        storage: 70Gi
    storageClassName: ${STORAGE_CLASS}
EOF
        else
            log_info "Using S3 source with credentials from Secret"
            cat <<EOF | oc --kubeconfig="$KUBECONFIG" create -f -
apiVersion: cdi.kubevirt.io/v1beta1
kind: DataVolume
metadata:
  name: windows
  namespace: ${SERVICE_ACCOUNT_NAMESPACE}
  annotations:
    cdi.kubevirt.io/storage.usePopulator: "false"
    ocpctl.io/import-method: "s3"
spec:
  contentType: kubevirt
  source:
    s3:
      url: "https://s3.us-east-1.amazonaws.com/ocpctl-binaries/windows-images/windows-10-oadp.qcow2"
      secretRef: "cdi-s3-import-creds"
  storage:
    accessModes:
      - ${ACCESS_MODE}
    resources:
      requests:
        storage: 70Gi
    storageClassName: ${STORAGE_CLASS}
EOF
        fi

        log_info "✓ DataVolume created (S3 import starting - 30-50 minutes)"
    fi
fi

##############################################################################
# Wait for Import Completion
##############################################################################

log_info ""
log_info "Waiting for Windows image import to complete..."

if [ "$IMPORT_METHOD" = "snapshot" ]; then
    MAX_WAIT=900
    PROGRESS_LOG_INTERVAL=30
    log_info "Note: Snapshot-based import typically completes in 2-3 minutes"
else
    MAX_WAIT=14400
    PROGRESS_LOG_INTERVAL=300
    log_info "Note: S3 import can take 30-50 minutes"
fi

WAIT_TIME=0
while [ $WAIT_TIME -lt $MAX_WAIT ]; do
    if [ "$IMPORT_METHOD" = "snapshot" ]; then
        PVC_PHASE=$(oc --kubeconfig="$KUBECONFIG" get pvc windows -n $SERVICE_ACCOUNT_NAMESPACE -o jsonpath='{.status.phase}' 2>/dev/null || echo "NotFound")

        if [ "$PVC_PHASE" = "Bound" ]; then
            log_info "✓ Windows image restored from snapshot"
            break
        elif [ "$PVC_PHASE" = "Failed" ] || [ "$PVC_PHASE" = "Lost" ]; then
            log_error "PVC restore from snapshot failed"
            exit 1
        fi
        DV_PHASE="$PVC_PHASE"
        DV_PROGRESS="N/A"
    else
        DV_PHASE=$(oc --kubeconfig="$KUBECONFIG" get datavolume windows -n $SERVICE_ACCOUNT_NAMESPACE -o jsonpath='{.status.phase}' 2>/dev/null || echo "NotFound")
        DV_PROGRESS=$(oc --kubeconfig="$KUBECONFIG" get datavolume windows -n $SERVICE_ACCOUNT_NAMESPACE -o jsonpath='{.status.progress}' 2>/dev/null || echo "N/A")

        if [ "$DV_PHASE" = "Succeeded" ]; then
            log_info "✓ Windows image import completed"
            break
        elif [ "$DV_PHASE" = "Failed" ]; then
            log_error "DataVolume import failed"
            exit 1
        fi
    fi

    if [ $((WAIT_TIME % PROGRESS_LOG_INTERVAL)) -eq 0 ]; then
        ELAPSED_MIN=$((WAIT_TIME / 60))
        REMAINING_MIN=$(((MAX_WAIT - WAIT_TIME) / 60))
        log_info "  Status: $DV_PHASE | Progress: $DV_PROGRESS | Elapsed: ${ELAPSED_MIN}m | Timeout: ${REMAINING_MIN}m"
    fi

    sleep 10
    WAIT_TIME=$((WAIT_TIME + 10))
done

if [ $WAIT_TIME -ge $MAX_WAIT ]; then
    log_error "DataVolume import timed out after ${MAX_WAIT}s"
    exit 1
fi

##############################################################################
# Wait for CNV Webhook Services
##############################################################################

log_info ""
log_info "Waiting for CNV webhook services to be ready..."

# Wait for kubemacpool-service to have endpoints
# This prevents "no endpoints available for service kubemacpool-service" errors
# when creating VMs immediately after CNV installation
MAX_WAIT=300
ELAPSED=0
while [ $ELAPSED -lt $MAX_WAIT ]; do
    ENDPOINTS=$(oc --kubeconfig="$KUBECONFIG" get endpoints kubemacpool-service -n openshift-cnv -o jsonpath='{.subsets[*].addresses[*].ip}' 2>/dev/null || echo "")
    if [ -n "$ENDPOINTS" ]; then
        log_info "✓ CNV webhook services ready"
        break
    fi

    if [ $((ELAPSED % 30)) -eq 0 ]; then
        log_info "Still waiting for CNV webhooks... (${ELAPSED}s elapsed)"
    fi

    sleep 5
    ELAPSED=$((ELAPSED + 5))
done

if [ $ELAPSED -ge $MAX_WAIT ]; then
    log_error "Timeout waiting for CNV webhook services after ${MAX_WAIT}s"
    exit 1
fi

##############################################################################
# Create VM Template
##############################################################################

log_info ""
log_info "Creating Windows VM template..."

# Get source PVC details
SOURCE_STORAGE_CLASS=$(oc --kubeconfig="$KUBECONFIG" get pvc windows -n $SERVICE_ACCOUNT_NAMESPACE -o jsonpath='{.spec.storageClassName}' 2>/dev/null)
CLONE_STORAGE_CLASS="${SOURCE_STORAGE_CLASS}"

log_info "  Storage class: ${CLONE_STORAGE_CLASS}"
log_info "  VMs will clone from pristine PVC 'windows'"

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
export STORAGE_CLASS="${CLONE_STORAGE_CLASS}"
export ACCESS_MODE="ReadWriteOnce"

# Note: Template creates VMs from windows-source-snapshot (VolumeSnapshot restore)
# windows-source-snapshot is populated from EBS snapshot (fast) or S3 import (slow)
# For faster provisioning in new regions, create regional snapshots via Snapshot Management UI
cat "${SCRIPT_DIR}/4_windows10-template.yaml" | envsubst '${STORAGE_CLASS}' | oc --kubeconfig="$KUBECONFIG" apply -f -
log_info "✓ VM Template created: windows10-oadp-vm"

# Create default Windows VM
log_info ""
log_info "Creating default Windows VM from template..."
oc --kubeconfig="$KUBECONFIG" process -n $SERVICE_ACCOUNT_NAMESPACE windows10-oadp-vm \
    -p VM_NAME=windows-vm \
    -p VM_NAMESPACE=$SERVICE_ACCOUNT_NAMESPACE \
    -p STORAGE_CLASS=${CLONE_STORAGE_CLASS} | oc --kubeconfig="$KUBECONFIG" apply -f -

log_info "✓ Windows VM created: windows-vm (namespace: $SERVICE_ACCOUNT_NAMESPACE)"
log_info "  VM is created but not started - start via OpenShift Console or CLI"

log_info ""
log_info "═══════════════════════════════════════════════════════════════"
log_info "✅ Windows VM Infrastructure Setup Complete!"
log_info "═══════════════════════════════════════════════════════════════"
log_info ""
if [ -n "$ROLE_ARN" ]; then
    log_info "IAM Role: $ROLE_NAME"
    log_info "Role ARN: $ROLE_ARN"
else
    log_info "Credentials Mode: Mint"
fi
log_info "ServiceAccount: $SERVICE_ACCOUNT_NAMESPACE/$SERVICE_ACCOUNT_NAME"
log_info ""
log_info "Windows VM Resources:"
log_info "  Base Image: windows (pristine PVC, 70GB Windows 10)"
log_info "  Default VM: windows-vm (namespace: $SERVICE_ACCOUNT_NAMESPACE)"
log_info "  Template: windows10-oadp-vm"
log_info "  Storage Class: ${CLONE_STORAGE_CLASS}"
log_info "  Import Method: ${IMPORT_METHOD}"
log_info ""
log_info "Next Steps:"
log_info "  1. Start the Windows VM:"
log_info "     oc patch vm windows-vm -n $SERVICE_ACCOUNT_NAMESPACE --type merge -p '{\"spec\":{\"running\":true}}'"
log_info ""
log_info "  2. Create additional VMs using the template:"
log_info "     oc process -n $SERVICE_ACCOUNT_NAMESPACE windows10-oadp-vm \\"
log_info "       -p VM_NAME=my-windows-vm -p VM_NAMESPACE=default | oc apply -f -"
log_info ""
if [ "$IMPORT_METHOD" = "s3" ]; then
    log_info "Performance Note:"
    log_info "  This region used S3 import (slow path: 30-50 min)."
    log_info "  To enable fast path (2-3 min) for future deployments:"
    log_info "  Use Windows Snapshot Management UI to create a regional snapshot."
    log_info ""
fi
log_info "═══════════════════════════════════════════════════════════════"

exit 0
