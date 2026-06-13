#!/bin/bash
#
# Automated IRSA Setup for Windows Image Access (Post-Deployment)
#
# This script is called automatically during cluster post-deployment.
# It receives cluster information via environment variables:
#   - CLUSTER_ID: Cluster UUID
#   - CLUSTER_NAME: Cluster name
#   - INFRA_ID: OpenShift infrastructure ID
#   - REGION: AWS region
#   - KUBECONFIG: Path to kubeconfig file
#
# The script:
# 1. Creates an IAM role with S3 read-only permissions
# 2. Configures trust policy for the cluster's OIDC provider
# 3. Applies ServiceAccount with IAM role annotation
# 4. Applies DataVolume to download Windows image from S3
# 5. Applies DataSource and VM template
#

set -e
set -o pipefail

# Setup detailed logging to file for debugging
# Log file is stored in cluster work directory and survives job retries
LOG_DIR="/var/lib/ocpctl/clusters/${CLUSTER_ID:-unknown}"
mkdir -p "$LOG_DIR"
SCRIPT_LOG="$LOG_DIR/windows-vm-setup.log"

# Redirect all output (stdout and stderr) to both console and log file
exec > >(tee -a "$SCRIPT_LOG") 2>&1

# Log script start with timestamp
echo "================================================================================"
echo "Windows VM Setup Script - Started at $(date -u +"%Y-%m-%d %H:%M:%S UTC")"
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
        echo "Windows VM Setup Script - COMPLETED SUCCESSFULLY"
    else
        echo "Windows VM Setup Script - FAILED with exit code $EXIT_CODE"
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

log_info "Starting automated IRSA setup for Windows VM"
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

    # Attach S3 read-only policy to user (check if already attached first)
    log_info "Attaching S3 read-only policy to IAM user"
    SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

    # Check if policy is already attached
    if aws iam get-user-policy --user-name "$IAM_USER_NAME" --policy-name S3WindowsImageReadOnly &>/dev/null; then
        log_info "✓ Policy already attached to IAM user"
    else
        # Policy doesn't exist, try to attach it
        if aws iam put-user-policy \
            --user-name "$IAM_USER_NAME" \
            --policy-name S3WindowsImageReadOnly \
            --policy-document file://${SCRIPT_DIR}/iam-policy.json 2>&1; then
            log_info "✓ Policy attached to IAM user"
        else
            # If we don't have permission to attach, check if it was already attached by someone else
            if aws iam get-user-policy --user-name "$IAM_USER_NAME" --policy-name S3WindowsImageReadOnly &>/dev/null; then
                log_warn "Policy already exists (attached by external process)"
            else
                log_error "Failed to attach policy and policy doesn't exist - manual intervention required"
                exit 1
            fi
        fi
    fi

    # Create access keys (delete old ones first if they exist)
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

    # Unset credentials so they don't interfere with snapshot detection
    # (SSM/EC2 API calls should use worker's EC2 instance role, not S3 user credentials)
    unset AWS_ACCESS_KEY_ID
    unset AWS_SECRET_ACCESS_KEY
fi

# Wait for CDI API to be ready
log_info "Waiting for CDI API to be ready..."
MAX_WAIT=300  # 5 minutes
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
    log_error "The CDI operator may still be deploying. Try retrying this configuration in a few minutes."
    exit 1
fi

##############################################################################
# Snapshot-Based Import (Fast Path - 2-3 minutes)
##############################################################################

log_info "Checking for pre-created Windows image EBS snapshot in region $REGION..."
log_info "[DEBUG] Step 1: Starting snapshot detection"

# Detect cluster region from infrastructure if not already set
if [ -z "$REGION" ]; then
    REGION=$(oc --kubeconfig="$KUBECONFIG" get infrastructure cluster -o jsonpath='{.status.platformStatus.aws.region}' 2>/dev/null || echo "us-east-1")
    log_info "Auto-detected region: $REGION"
fi
log_info "[DEBUG] Step 2: Region set to: $REGION"

SNAPSHOT_VERSION="1.0"  # Could be parameterized in future
SNAPSHOT_ID=""
IMPORT_METHOD="s3"  # Default to S3 fallback
log_info "[DEBUG] Step 3: Variables initialized (SNAPSHOT_VERSION=$SNAPSHOT_VERSION)"

# Try SSM Parameter Store first with retry logic
log_info "[DEBUG] Step 4: Checking if AWS CLI is available"
if command -v aws &> /dev/null; then
    log_info "[DEBUG] Step 5: AWS CLI found, proceeding with SSM lookup"
    SNAPSHOT_PARAM="/ocpctl/windows-snapshots/${SNAPSHOT_VERSION}/${REGION}"

    # Retry SSM lookup up to 3 times with exponential backoff
    SSM_ATTEMPTS=0
    SSM_MAX_ATTEMPTS=3
    log_info "[DEBUG] Step 5a: Starting SSM retry loop (max attempts: $SSM_MAX_ATTEMPTS)"
    while [ $SSM_ATTEMPTS -lt $SSM_MAX_ATTEMPTS ]; do
        SSM_ATTEMPTS=$((SSM_ATTEMPTS + 1))
        log_info "[DEBUG] Step 5b: SSM attempt $SSM_ATTEMPTS of $SSM_MAX_ATTEMPTS"

        # Capture both stdout and stderr
        log_info "[DEBUG] Step 5c: Running AWS SSM command"
        log_info "[DEBUG] Step 5c1: PATH=$PATH"
        AWS_LOCATION=$(which aws 2>&1 || echo 'aws not found in PATH')
        log_info "[DEBUG] Step 5c2: AWS binary location: $AWS_LOCATION"
        log_info "[DEBUG] Step 5c3: Command: aws ssm get-parameter --name $SNAPSHOT_PARAM --region $REGION"

        set +e  # Temporarily disable exit on error to capture AWS CLI exit code
        SSM_OUTPUT=$(aws ssm get-parameter --name "$SNAPSHOT_PARAM" --region "$REGION" --query 'Parameter.Value' --output text 2>&1)
        SSM_EXIT_CODE=$?
        set -e  # Re-enable exit on error

        log_info "[DEBUG] Step 5d: AWS SSM command completed (exit code: $SSM_EXIT_CODE)"
        log_info "[DEBUG] Step 5d1: Output length: ${#SSM_OUTPUT} chars"

        if [ $SSM_EXIT_CODE -eq 0 ] && [ -n "$SSM_OUTPUT" ] && [ "$SSM_OUTPUT" != "None" ]; then
            SNAPSHOT_ID="$SSM_OUTPUT"
            log_info "✓ Found snapshot via SSM Parameter Store: $SNAPSHOT_ID (attempt $SSM_ATTEMPTS)"
            break
        fi

        # Log the error if not a simple "parameter not found"
        if [[ "$SSM_OUTPUT" != *"ParameterNotFound"* ]]; then
            log_warn "SSM lookup attempt $SSM_ATTEMPTS failed: $SSM_OUTPUT"
        fi

        # Don't retry if parameter genuinely doesn't exist
        if [[ "$SSM_OUTPUT" == *"ParameterNotFound"* ]]; then
            log_info "SSM parameter $SNAPSHOT_PARAM does not exist (not an error)"
            break
        fi

        # Exponential backoff before retry
        if [ $SSM_ATTEMPTS -lt $SSM_MAX_ATTEMPTS ]; then
            BACKOFF=$((2 ** SSM_ATTEMPTS))
            log_info "Retrying SSM lookup in ${BACKOFF}s..."
            sleep $BACKOFF
        fi
    done

    # Fallback: Query snapshots by tags (with sorting by StartTime DESC to get newest)
    if [ -z "$SNAPSHOT_ID" ] || [ "$SNAPSHOT_ID" = "None" ]; then
        log_info "SSM parameter not found, checking EBS snapshots by tags..."

        EC2_ATTEMPTS=0
        EC2_MAX_ATTEMPTS=3
        while [ $EC2_ATTEMPTS -lt $EC2_MAX_ATTEMPTS ]; do
            EC2_ATTEMPTS=$((EC2_ATTEMPTS + 1))

            # Query with proper sorting to get NEWEST snapshot (not random)
            set +e  # Temporarily disable exit on error
            EC2_OUTPUT=$(aws ec2 describe-snapshots \
                --region "$REGION" \
                --filters "Name=tag:ocpctl:managed,Values=true" \
                          "Name=tag:ocpctl:image-version,Values=${SNAPSHOT_VERSION}" \
                          "Name=status,Values=completed" \
                --owner-ids self \
                --query 'reverse(sort_by(Snapshots, &StartTime))[0].SnapshotId' \
                --output text 2>&1)
            EC2_EXIT_CODE=$?
            set -e  # Re-enable exit on error

            if [ $EC2_EXIT_CODE -eq 0 ] && [ -n "$EC2_OUTPUT" ] && [ "$EC2_OUTPUT" != "None" ]; then
                SNAPSHOT_ID="$EC2_OUTPUT"
                log_info "✓ Found snapshot via EC2 tags (newest): $SNAPSHOT_ID (attempt $EC2_ATTEMPTS)"
                break
            fi

            # Log the error
            if [ $EC2_EXIT_CODE -ne 0 ]; then
                log_warn "EC2 snapshot query attempt $EC2_ATTEMPTS failed: $EC2_OUTPUT"
            fi

            # Exponential backoff before retry
            if [ $EC2_ATTEMPTS -lt $EC2_MAX_ATTEMPTS ]; then
                BACKOFF=$((2 ** EC2_ATTEMPTS))
                log_info "Retrying EC2 snapshot query in ${BACKOFF}s..."
                sleep $BACKOFF
            fi
        done
    fi
else
    log_warn "[DEBUG] AWS CLI not found - skipping snapshot lookup"
fi
log_info "[DEBUG] Step 6: Finished snapshot lookup attempt (SNAPSHOT_ID='$SNAPSHOT_ID')"

# Validate snapshot exists and is completed before using it
if [ -n "$SNAPSHOT_ID" ] && [ "$SNAPSHOT_ID" != "None" ]; then
    # Verify snapshot is actually available
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
    log_info "⚠ No usable snapshot found in $REGION"
    log_info "  Using S3 download fallback (expected: 30-50 minutes)"
    log_info "  Note: First VM deployment will create snapshot for future use"
fi

# Auto-detect best storage class for Windows VMs
log_info "Detecting available storage class..."
if oc --kubeconfig="$KUBECONFIG" get storageclass ocs-storagecluster-ceph-rbd-virtualization &>/dev/null; then
    STORAGE_CLASS="ocs-storagecluster-ceph-rbd-virtualization"
    ACCESS_MODE="ReadWriteMany"
    log_info "✓ Using ODF storage: $STORAGE_CLASS (supports live migration)"
elif oc --kubeconfig="$KUBECONFIG" get storageclass gp3-csi &>/dev/null; then
    # CRITICAL: Detect worker zones FIRST to avoid zone mismatch failures
    # If we create an Immediate storage class without zone constraints, AWS may provision
    # the volume in a zone with no workers, causing the importer pod to be unschedulable
    log_info "Detecting worker node zones to ensure DataVolume can be imported..."
    WORKER_ZONES=$(oc --kubeconfig="$KUBECONFIG" get nodes -l node-role.kubernetes.io/worker \
        -o jsonpath='{range .items[*]}{.metadata.labels.topology\.kubernetes\.io/zone}{"\n"}{end}' | sort -u)

    if [ -z "$WORKER_ZONES" ]; then
        log_error "No worker nodes found in cluster"
        exit 1
    fi

    # Pick the first worker zone for DataVolume import
    IMPORT_ZONE=$(echo "$WORKER_ZONES" | head -1)
    log_info "✓ Detected worker zones: $(echo $WORKER_ZONES | tr '\n' ', ')"
    log_info "✓ Using zone $IMPORT_ZONE for DataVolume import (has workers)"

    # Create zone-constrained Immediate storage class for CDI imports
    # This ensures the DataVolume PVC is created in a zone where workers exist
    IMPORT_STORAGE_CLASS="gp3-csi-immediate-${IMPORT_ZONE}"
    if ! oc --kubeconfig="$KUBECONFIG" get storageclass "$IMPORT_STORAGE_CLASS" &>/dev/null; then
        log_info "Creating zone-constrained storage class for CDI imports: $IMPORT_STORAGE_CLASS..."
        cat <<EOF | oc --kubeconfig="$KUBECONFIG" apply -f -
apiVersion: storage.k8s.io/v1
kind: StorageClass
metadata:
  name: ${IMPORT_STORAGE_CLASS}
  annotations:
    storageclass.kubernetes.io/description: "AWS EBS gp3 with immediate binding in zone ${IMPORT_ZONE} for CDI imports"
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

    # Create gp3-csi-wfc for VM disks (WaitForFirstConsumer prevents AZ mismatch)
    if ! oc --kubeconfig="$KUBECONFIG" get storageclass gp3-csi-wfc &>/dev/null; then
        log_info "Creating gp3-csi-wfc storage class for VM disks..."
        cat <<EOF | oc --kubeconfig="$KUBECONFIG" apply -f -
apiVersion: storage.k8s.io/v1
kind: StorageClass
metadata:
  name: gp3-csi-wfc
  annotations:
    storageclass.kubernetes.io/description: "AWS EBS gp3 with WaitForFirstConsumer - prevents AZ mismatch for VM clones"
allowVolumeExpansion: true
parameters:
  encrypted: "true"
  type: gp3
provisioner: ebs.csi.aws.com
reclaimPolicy: Delete
volumeBindingMode: WaitForFirstConsumer
EOF
        log_info "✓ Created gp3-csi-wfc storage class for VM disks (prevents AZ mismatch)"
    fi

    STORAGE_CLASS="$IMPORT_STORAGE_CLASS"
    ACCESS_MODE="ReadWriteOnce"
    log_info "✓ Using zone-constrained storage: $STORAGE_CLASS for image import"
    log_info "✓ VM template will use zone-specific Immediate class for clones"
elif oc --kubeconfig="$KUBECONFIG" get storageclass gp2-csi &>/dev/null; then
    # Create gp2-csi-immediate for CDI imports
    if ! oc --kubeconfig="$KUBECONFIG" get storageclass gp2-csi-immediate &>/dev/null; then
        log_info "Creating gp2-csi-immediate storage class for CDI imports..."
        cat <<EOF | oc --kubeconfig="$KUBECONFIG" apply -f -
apiVersion: storage.k8s.io/v1
kind: StorageClass
metadata:
  name: gp2-csi-immediate
  annotations:
    storageclass.kubernetes.io/description: "AWS EBS gp2 with immediate binding for CDI imports"
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
    log_info "✓ Using AWS EBS storage: $STORAGE_CLASS (immediate binding for CDI)"
else
    # Fallback to default storage class
    STORAGE_CLASS=$(oc --kubeconfig="$KUBECONFIG" get storageclass -o jsonpath='{.items[?(@.metadata.annotations.storageclass\.kubernetes\.io/is-default-class=="true")].metadata.name}')
    if [ -z "$STORAGE_CLASS" ]; then
        log_error "No suitable storage class found"
        exit 1
    fi
    ACCESS_MODE="ReadWriteOnce"
    log_info "✓ Using default storage class: $STORAGE_CLASS"
fi

# Create DataVolume
# Note: CDI importer doesn't properly support IRSA for S3, so we use HTTP with presigned URL

# Check if DataVolume already exists
EXISTING_DV_PHASE=$(oc --kubeconfig="$KUBECONFIG" get datavolume windows -n $SERVICE_ACCOUNT_NAMESPACE -o jsonpath='{.status.phase}' 2>/dev/null || echo "NotFound")

if [ "$EXISTING_DV_PHASE" = "Succeeded" ]; then
    log_info "DataVolume 'windows' already exists and has succeeded - skipping creation"
elif [ "$EXISTING_DV_PHASE" != "NotFound" ]; then
    log_warn "DataVolume 'windows' exists with phase: $EXISTING_DV_PHASE"
    log_warn "DataVolume specs are immutable - deleting and recreating with fresh presigned URL..."
    oc --kubeconfig="$KUBECONFIG" delete datavolume windows -n $SERVICE_ACCOUNT_NAMESPACE --wait=true
    log_info "✓ Old DataVolume deleted"
fi

if [ "$EXISTING_DV_PHASE" != "Succeeded" ]; then
    log_info "Creating DataVolume for Windows image (method: $IMPORT_METHOD, storage: $STORAGE_CLASS)"

    if [ "$IMPORT_METHOD" = "snapshot" ]; then
        # Fast path: Use EBS snapshot via VolumeSnapshot
        log_info "Creating DataVolume from EBS snapshot $SNAPSHOT_ID..."

        # Clean up any existing snapshot resources from previous failed attempts
        log_info "Cleaning up any existing snapshot import resources..."
        oc --kubeconfig="$KUBECONFIG" delete volumesnapshot windows-source-snapshot -n $SERVICE_ACCOUNT_NAMESPACE --ignore-not-found=true 2>/dev/null || true
        oc --kubeconfig="$KUBECONFIG" delete volumesnapshotcontent windows-source-snapshot-content --ignore-not-found=true 2>/dev/null || true
        log_info "✓ Cleanup complete"

        # Step 1: Create VolumeSnapshotContent pointing to pre-existing EBS snapshot
        # This is required for importing external snapshots into Kubernetes
        log_info "Creating VolumeSnapshotContent for snapshot $SNAPSHOT_ID..."
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

        # Step 2: Create VolumeSnapshot that references the VolumeSnapshotContent
        log_info "Creating VolumeSnapshot bound to VolumeSnapshotContent..."
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

        # Check if PVC already exists (for retry scenarios)
        EXISTING_PVC=$(oc --kubeconfig="$KUBECONFIG" get pvc windows -n $SERVICE_ACCOUNT_NAMESPACE -o jsonpath='{.status.phase}' 2>/dev/null || echo "NotFound")

        if [ "$EXISTING_PVC" = "Bound" ]; then
            log_info "PVC 'windows' already exists and is Bound - skipping creation"
        elif [ "$EXISTING_PVC" != "NotFound" ]; then
            log_warn "PVC 'windows' exists with phase: $EXISTING_PVC - deleting and recreating"
            oc --kubeconfig="$KUBECONFIG" delete pvc windows -n $SERVICE_ACCOUNT_NAMESPACE --wait=true
            log_info "✓ Old PVC deleted"
            # Reset variable after successful deletion
            EXISTING_PVC="NotFound"
        fi

        if [ "$EXISTING_PVC" != "Bound" ]; then
            # Create PVC directly with dataSourceRef pointing to VolumeSnapshot
            # This uses native CSI snapshot restore (fast) instead of CDI's DataVolume (slow)
            # CDI will automatically create a DataVolume wrapper for this PVC
            cat <<EOF | oc --kubeconfig="$KUBECONFIG" create -f -
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: windows
  namespace: ${SERVICE_ACCOUNT_NAMESPACE}
  annotations:
    ocpctl.io/import-method: "snapshot"
    ocpctl.io/snapshot-id: "${SNAPSHOT_ID}"
    ocpctl.io/snapshot-version: "${SNAPSHOT_VERSION}"
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
            # Manual mode: Use HTTP with presigned URL (IRSA doesn't work with CDI S3 imports)
            log_info "Generating presigned URL for Windows image (valid for 24 hours)..."
            # IMPORTANT: Use bucket region (us-east-1), not cluster region, to avoid 301 redirects
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
    ocpctl.io/create-snapshot: "true"
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
            # Mint mode: Use S3 source with credentials from Secret
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
    ocpctl.io/create-snapshot: "true"
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
        log_info "  Snapshot will be created automatically for future deployments"
    fi
fi

# Note: DataSource removed - using explicit VolumeSnapshot → PVC restore path for all VMs
# This ensures consistent, fast CSI snapshot restore behavior

# Wait for pristine PVC to complete before creating cluster-local snapshot
log_info ""
log_info "Waiting for Windows image import to complete before creating test VM..."
log_info "(This ensures the snapshot is created during deployment, making future VM creation faster)"

# Adjust timeout and progress logging based on import method
if [ "$IMPORT_METHOD" = "snapshot" ]; then
    MAX_WAIT=900  # 15 minutes for snapshot restore (expected 2-3 min)
    PROGRESS_LOG_INTERVAL=30  # Log every 30 seconds
    log_info "Note: Snapshot-based import typically completes in 2-3 minutes"
else
    MAX_WAIT=14400  # 4 hours for S3 download
    PROGRESS_LOG_INTERVAL=300  # Log every 5 minutes
    log_info "Note: S3 import can take 30-50 minutes depending on download speed and conversion time"
fi

# Wait for import to succeed
WAIT_TIME=0
while [ $WAIT_TIME -lt $MAX_WAIT ]; do
    if [ "$IMPORT_METHOD" = "snapshot" ]; then
        # For snapshot import, check PVC status (we create PVC directly, not DataVolume)
        PVC_PHASE=$(oc --kubeconfig="$KUBECONFIG" get pvc windows -n $SERVICE_ACCOUNT_NAMESPACE -o jsonpath='{.status.phase}' 2>/dev/null || echo "NotFound")

        if [ "$PVC_PHASE" = "Bound" ]; then
            log_info "✓ Windows image restored from snapshot"
            break
        elif [ "$PVC_PHASE" = "Failed" ] || [ "$PVC_PHASE" = "Lost" ]; then
            DV_PHASE="Failed"  # Trigger fallback logic below
        else
            DV_PHASE="$PVC_PHASE"
        fi
        DV_PROGRESS="N/A"
    else
        # For S3 import, check DataVolume status
        DV_PHASE=$(oc --kubeconfig="$KUBECONFIG" get datavolume windows -n $SERVICE_ACCOUNT_NAMESPACE -o jsonpath='{.status.phase}' 2>/dev/null || echo "NotFound")
        DV_PROGRESS=$(oc --kubeconfig="$KUBECONFIG" get datavolume windows -n $SERVICE_ACCOUNT_NAMESPACE -o jsonpath='{.status.progress}' 2>/dev/null || echo "N/A")
    fi

    if [ "$DV_PHASE" = "Succeeded" ]; then
        log_info "✓ Windows image import completed"
        break
    elif [ "$DV_PHASE" = "Failed" ]; then
        # If snapshot import failed, fall back to S3
        if [ "$IMPORT_METHOD" = "snapshot" ]; then
            log_warn "⚠ Snapshot-based import failed, falling back to S3..."
            log_warn "Error details: $(oc --kubeconfig="$KUBECONFIG" get datavolume windows -n $SERVICE_ACCOUNT_NAMESPACE -o jsonpath='{.status.conditions[?(@.type=="Running")].message}' 2>/dev/null || echo 'N/A')"

            # Clean up failed resources
            log_info "Cleaning up failed snapshot import..."
            oc --kubeconfig="$KUBECONFIG" delete pvc windows -n $SERVICE_ACCOUNT_NAMESPACE --wait=true 2>/dev/null || true
            oc --kubeconfig="$KUBECONFIG" delete datavolume windows -n $SERVICE_ACCOUNT_NAMESPACE --wait=true 2>/dev/null || true
            oc --kubeconfig="$KUBECONFIG" delete volumesnapshot windows-source-snapshot -n $SERVICE_ACCOUNT_NAMESPACE --ignore-not-found=true
            oc --kubeconfig="$KUBECONFIG" delete volumesnapshotcontent windows-source-snapshot-content --ignore-not-found=true

            # Retry with S3 method
            IMPORT_METHOD="s3"
            log_info "Retrying with S3 download method..."

            # Re-create DataVolume with S3 method
            if [ "$CREDENTIALS_MODE" = "manual" ]; then
                log_info "Generating presigned URL for Windows image (valid for 24 hours)..."
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
    ocpctl.io/create-snapshot: "true"
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
                cat <<EOF | oc --kubeconfig="$KUBECONFIG" create -f -
apiVersion: cdi.kubevirt.io/v1beta1
kind: DataVolume
metadata:
  name: windows
  namespace: ${SERVICE_ACCOUNT_NAMESPACE}
  annotations:
    cdi.kubevirt.io/storage.usePopulator: "false"
    ocpctl.io/import-method: "s3"
    ocpctl.io/create-snapshot: "true"
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

            log_info "✓ DataVolume recreated with S3 method"

            # Update timeout for S3 download
            MAX_WAIT=14400
            PROGRESS_LOG_INTERVAL=300
            WAIT_TIME=0
            log_info "Resetting wait timer for S3 import (up to 4 hours)"
            continue
        else
            log_error "DataVolume import failed"
            # Get more details about failure
            oc --kubeconfig="$KUBECONFIG" get datavolume windows -n $SERVICE_ACCOUNT_NAMESPACE -o yaml | grep -A10 "conditions:" || true
            exit 1
        fi
    fi

    # Log progress at intervals
    if [ $((WAIT_TIME % PROGRESS_LOG_INTERVAL)) -eq 0 ]; then
        ELAPSED_MIN=$((WAIT_TIME / 60))
        REMAINING_MIN=$(((MAX_WAIT - WAIT_TIME) / 60))
        log_info "  DataVolume status: $DV_PHASE | Progress: $DV_PROGRESS | Elapsed: ${ELAPSED_MIN}m | Timeout in: ${REMAINING_MIN}m"
    fi

    sleep 10
    WAIT_TIME=$((WAIT_TIME + 10))
done

if [ $WAIT_TIME -ge $MAX_WAIT ]; then
    log_error "DataVolume import timed out after ${MAX_WAIT}s"
    log_error "This likely indicates a problem with the import source or network connectivity"
    log_error "Current DataVolume status:"
    oc --kubeconfig="$KUBECONFIG" get datavolume windows -n $SERVICE_ACCOUNT_NAMESPACE -o yaml | grep -A20 "status:" || true
    exit 1
fi

# Step 5: Validate source PVC before snapshotting (if S3 import and no snapshot exists)
if [ "$DV_PHASE" = "Succeeded" ] && [ "$IMPORT_METHOD" = "s3" ] && [ -z "$SNAPSHOT_ID" ]; then
    log_info ""
    log_info "═══════════════════════════════════════════════════════════════"
    log_info "Step 5: Validating imported Windows image before snapshotting..."
    log_info "This ensures S3 import/QCOW2 conversion worked correctly"
    log_info "═══════════════════════════════════════════════════════════════"

    # Get source PVC configuration
    SOURCE_PV=$(oc --kubeconfig="$KUBECONFIG" get pvc windows -n $SERVICE_ACCOUNT_NAMESPACE -o jsonpath='{.spec.volumeName}' 2>/dev/null)
    SOURCE_VOLUME_MODE=$(oc --kubeconfig="$KUBECONFIG" get pvc windows -n $SERVICE_ACCOUNT_NAMESPACE -o jsonpath='{.spec.volumeMode}' 2>/dev/null || echo "Block")
    SOURCE_ZONE=$(oc --kubeconfig="$KUBECONFIG" get pv "$SOURCE_PV" -o jsonpath='{.spec.nodeAffinity.required.nodeSelectorTerms[0].matchExpressions[?(@.key=="topology.ebs.csi.aws.com/zone")].values[0]}' 2>/dev/null)
    if [ -z "$SOURCE_ZONE" ]; then
        SOURCE_ZONE=$(oc --kubeconfig="$KUBECONFIG" get pv "$SOURCE_PV" -o jsonpath='{.spec.nodeAffinity.required.nodeSelectorTerms[0].matchExpressions[?(@.key=="topology.kubernetes.io/zone")].values[0]}' 2>/dev/null)
    fi

    log_info "Source PVC: windows (namespace: $SERVICE_ACCOUNT_NAMESPACE)"
    log_info "  Volume mode: $SOURCE_VOLUME_MODE"
    log_info "  Zone: $SOURCE_ZONE"

    # Create source validation VM directly from imported PVC
    log_info "Creating validation VM from source PVC..."
    cat <<EOF_SOURCE_VM | oc --kubeconfig="$KUBECONFIG" apply -f -
apiVersion: kubevirt.io/v1
kind: VirtualMachine
metadata:
  name: windows-source-validation
  namespace: $SERVICE_ACCOUNT_NAMESPACE
spec:
  running: false
  template:
    metadata:
      labels:
        kubevirt.io/vm: windows-source-validation
    spec:
      domain:
        cpu:
          cores: 4
        devices:
          disks:
          - disk:
              bus: sata
            name: rootdisk
          - disk:
              bus: sata
            name: cloudinitdisk
          interfaces:
          - masquerade: {}
            name: default
            model: e1000
        machine:
          type: pc-q35-rhel9.2.0
        resources:
          requests:
            memory: 8Gi
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
                - ${SOURCE_ZONE}
      volumes:
      - name: rootdisk
        persistentVolumeClaim:
          claimName: windows
      - cloudInitNoCloud:
          userData: "#cloud-config"
        name: cloudinitdisk
EOF_SOURCE_VM

    log_info "✓ Source validation VM created"
    log_info "  Starting VM to validate imported image..."

    # Start the validation VM
    if ! oc --kubeconfig="$KUBECONFIG" patch virtualmachine windows-source-validation -n $SERVICE_ACCOUNT_NAMESPACE --type merge -p '{"spec":{"running":true}}' 2>/dev/null; then
        log_error "Failed to start source validation VM"
        exit 1
    fi

    # Wait for VM to reach Running and remain stable
    SOURCE_VM_WAIT=0
    SOURCE_VM_MAX_WAIT=600  # 10 minutes (boot timeout)
    VM_RUNNING=false
    RUNNING_START_TIME=0

    log_info "  Waiting for VM to boot..."

    while [ $SOURCE_VM_WAIT -lt $SOURCE_VM_MAX_WAIT ]; do
        VM_STATUS=$(oc --kubeconfig="$KUBECONFIG" get virtualmachine windows-source-validation -n $SERVICE_ACCOUNT_NAMESPACE \
            -o jsonpath='{.status.printableStatus}' 2>/dev/null || echo "Unknown")

        # Check if VM failed
        if [ "$VM_STATUS" = "CrashLoopBackOff" ] || [ "$VM_STATUS" = "Failed" ] || [ "$VM_STATUS" = "ErrorUnschedulable" ]; then
            log_error "Source VM failed - status: $VM_STATUS"
            log_error "Imported disk failed basic boot validation"
            exit 1
        fi

        # Once Running, start stability timer
        if [ "$VM_STATUS" = "Running" ]; then
            if [ "$VM_RUNNING" = "false" ]; then
                log_info "  VM reached Running state, monitoring stability..."
                RUNNING_START_TIME=$SOURCE_VM_WAIT
                VM_RUNNING=true
            fi

            # Check if been stable for 5 minutes
            STABLE_DURATION=$((SOURCE_VM_WAIT - RUNNING_START_TIME))
            if [ $STABLE_DURATION -ge 300 ]; then
                log_info "✓ Source VM reached Running and remained stable for 5 minutes"
                log_info "  Imported disk passed basic boot validation"
                break
            fi

            # Log stability progress every 60 seconds
            if [ $((STABLE_DURATION % 60)) -eq 0 ] && [ $STABLE_DURATION -gt 0 ]; then
                STABLE_MIN=$((STABLE_DURATION / 60))
                log_info "  VM stable for ${STABLE_MIN}m (waiting 5m total)"
            fi
        else
            # If was Running but changed status, fail
            if [ "$VM_RUNNING" = "true" ]; then
                log_error "VM became unstable - status changed from Running to $VM_STATUS"
                log_error "Imported disk failed stability validation"
                exit 1
            fi

            # Still waiting to reach Running
            if [ $((SOURCE_VM_WAIT % 30)) -eq 0 ] && [ $SOURCE_VM_WAIT -gt 0 ]; then
                ELAPSED_MIN=$((SOURCE_VM_WAIT / 60))
                log_info "  VM status: $VM_STATUS (${ELAPSED_MIN}m elapsed)"
            fi
        fi

        sleep 5
        SOURCE_VM_WAIT=$((SOURCE_VM_WAIT + 5))
    done

    # Check if we timed out
    STABLE_DURATION=$((SOURCE_VM_WAIT - RUNNING_START_TIME))
    if [ $STABLE_DURATION -lt 300 ]; then
        log_error "Source VM did not remain stable for 5 minutes"
        log_error "Imported disk failed stability validation"
        exit 1
    fi

    # Stop and delete validation VM to ensure PVC is idle before snapshotting
    log_info "  Stopping validation VM (ensuring PVC is idle)..."
    oc --kubeconfig="$KUBECONFIG" patch virtualmachine windows-source-validation -n $SERVICE_ACCOUNT_NAMESPACE --type merge -p '{"spec":{"running":false}}' 2>/dev/null || true

    # Wait for VM to fully stop
    sleep 10

    log_info "  Deleting validation VM..."
    oc --kubeconfig="$KUBECONFIG" delete virtualmachine windows-source-validation -n $SERVICE_ACCOUNT_NAMESPACE --ignore-not-found=true 2>/dev/null || true

    # Wait a bit to ensure PVC is completely released
    sleep 5
    log_info "✓ Source PVC validated and idle"

fi

# ═══════════════════════════════════════════════════════════════
# REFACTORED WORKFLOW: Explicit VolumeSnapshot → PVC Fast Path
# ═══════════════════════════════════════════════════════════════
#
# This workflow creates a cluster-local VolumeSnapshot from the pristine PVC
# and validates it by restoring a PVC FROM that VolumeSnapshot (not cloning
# from pristine PVC). This ensures all VMs use the fast CSI snapshot restore
# path instead of slow CDI PVC cloning.
#
# Architecture:
# 1. Pristine PVC (from S3 or EBS snapshot import)
# 2. Create cluster-local VolumeSnapshot from pristine PVC
# 3. Restore validation PVC FROM VolumeSnapshot (tests fast path!)
# 4. Boot validation VM from restored PVC
# 5. Mark VolumeSnapshot as active only after boot validation passes
# 6. Future VMs restore FROM VolumeSnapshot (fast CSI restore ~1 min)

# Initialize cluster-local snapshot name variable (used by template later)
CLUSTER_SNAPSHOT_NAME=""

# Step 6: Create and validate cluster-local VolumeSnapshot (S3 import path only)
if [ "$DV_PHASE" = "Succeeded" ] && [ "$IMPORT_METHOD" = "s3" ]; then
    log_info ""
    log_info "═══════════════════════════════════════════════════════════════"
    log_info "Creating cluster-local golden VolumeSnapshot..."
    log_info "This snapshot will be used for all future VM disk restores"
    log_info "═══════════════════════════════════════════════════════════════"

    # Discover VolumeSnapshotClass (don't hardcode)
    log_info "Discovering VolumeSnapshotClass for EBS CSI driver..."
    SNAPSHOT_CLASS=$(oc --kubeconfig="$KUBECONFIG" get volumesnapshotclass -o jsonpath='{.items[?(@.driver=="ebs.csi.aws.com")].metadata.name}' 2>/dev/null | awk '{print $1}')

    if [ -z "$SNAPSHOT_CLASS" ]; then
        log_error "Could not find VolumeSnapshotClass for ebs.csi.aws.com driver"
        exit 1
    fi
    log_info "✓ Using VolumeSnapshotClass: $SNAPSHOT_CLASS"

    # Get source PVC details dynamically
    log_info "Detecting source PVC configuration..."
    SOURCE_PV=$(oc --kubeconfig="$KUBECONFIG" get pvc windows -n $SERVICE_ACCOUNT_NAMESPACE -o jsonpath='{.spec.volumeName}' 2>/dev/null)
    SOURCE_SIZE=$(oc --kubeconfig="$KUBECONFIG" get pvc windows -n $SERVICE_ACCOUNT_NAMESPACE -o jsonpath='{.spec.resources.requests.storage}' 2>/dev/null)
    SOURCE_STORAGE_CLASS=$(oc --kubeconfig="$KUBECONFIG" get pvc windows -n $SERVICE_ACCOUNT_NAMESPACE -o jsonpath='{.spec.storageClassName}' 2>/dev/null)
    SOURCE_ZONE=$(oc --kubeconfig="$KUBECONFIG" get pv "$SOURCE_PV" -o jsonpath='{.spec.nodeAffinity.required.nodeSelectorTerms[0].matchExpressions[?(@.key=="topology.ebs.csi.aws.com/zone")].values[0]}' 2>/dev/null)
    if [ -z "$SOURCE_ZONE" ]; then
        SOURCE_ZONE=$(oc --kubeconfig="$KUBECONFIG" get pv "$SOURCE_PV" -o jsonpath='{.spec.nodeAffinity.required.nodeSelectorTerms[0].matchExpressions[?(@.key=="topology.kubernetes.io/zone")].values[0]}' 2>/dev/null)
    fi

    if [ -z "$SOURCE_ZONE" ]; then
        log_error "Could not detect source PVC zone"
        exit 1
    fi

    log_info "✓ Source PVC size: $SOURCE_SIZE"
    log_info "✓ Source storage class: $SOURCE_STORAGE_CLASS"
    log_info "✓ Source zone: $SOURCE_ZONE"

    # Create versioned snapshot name (date-stamped to avoid breaking existing VMs)
    SNAPSHOT_DATE=$(date -u +%Y%m%d)
    CLUSTER_SNAPSHOT_NAME="windows-golden-snapshot-v${SNAPSHOT_VERSION}-${SNAPSHOT_DATE}"

    log_info "Creating cluster-local VolumeSnapshot: $CLUSTER_SNAPSHOT_NAME"

    # Create cluster-local VolumeSnapshot from pristine PVC
    cat <<EOF_GOLDEN_SNAPSHOT | oc --kubeconfig="$KUBECONFIG" apply -f -
apiVersion: snapshot.storage.k8s.io/v1
kind: VolumeSnapshot
metadata:
  name: ${CLUSTER_SNAPSHOT_NAME}
  namespace: ${SERVICE_ACCOUNT_NAMESPACE}
  labels:
    ocpctl.mg.dog8code.com/managed: "true"
    ocpctl.mg.dog8code.com/image-version: "${SNAPSHOT_VERSION}"
    ocpctl.mg.dog8code.com/current: "candidate"  # Will change to "active" after validation
    ocpctl.mg.dog8code.com/snapshot-type: "cluster-local-golden"
  annotations:
    ocpctl.mg.dog8code.com/description: "Golden snapshot for fast Windows VM provisioning via CSI snapshot restore"
spec:
  volumeSnapshotClassName: ${SNAPSHOT_CLASS}
  source:
    persistentVolumeClaimName: windows
EOF_GOLDEN_SNAPSHOT

    log_info "✓ VolumeSnapshot created: $CLUSTER_SNAPSHOT_NAME"

    # Wait for cluster-local VolumeSnapshot to become ready
    log_info "Waiting for cluster-local VolumeSnapshot to become ready..."
    SNAPSHOT_WAIT=0
    SNAPSHOT_MAX_WAIT=7200  # 120 minutes
    CLUSTER_SNAPSHOT_READY=false

    while [ $SNAPSHOT_WAIT -lt $SNAPSHOT_MAX_WAIT ]; do
        READY_TO_USE=$(oc --kubeconfig="$KUBECONFIG" get volumesnapshot $CLUSTER_SNAPSHOT_NAME -n $SERVICE_ACCOUNT_NAMESPACE \
            -o jsonpath='{.status.readyToUse}' 2>/dev/null || echo "false")

        if [ "$READY_TO_USE" = "true" ]; then
            log_info "✓ Cluster-local VolumeSnapshot is ready to use (readyToUse=true)"
            CLUSTER_SNAPSHOT_READY=true

            # Get the VolumeSnapshotContent and add K8s 1.30+ annotation
            VOLUME_SNAPSHOT_CONTENT=$(oc --kubeconfig="$KUBECONFIG" get volumesnapshot $CLUSTER_SNAPSHOT_NAME -n $SERVICE_ACCOUNT_NAMESPACE \
                -o jsonpath='{.status.boundVolumeSnapshotContentName}' 2>/dev/null)

            if [ -n "$VOLUME_SNAPSHOT_CONTENT" ]; then
                log_info "Adding volume-mode-change annotation to VolumeSnapshotContent..."
                if oc --kubeconfig="$KUBECONFIG" annotate volumesnapshotcontent "$VOLUME_SNAPSHOT_CONTENT" \
                    snapshot.storage.kubernetes.io/allow-volume-mode-change=true --overwrite 2>/dev/null; then
                    log_info "✓ Annotation added (enables PVC restore from snapshot)"
                fi

                # Extract EBS snapshot ID for SSM registration
                GOLDEN_SNAPSHOT_ID=$(oc --kubeconfig="$KUBECONFIG" get volumesnapshotcontent "$VOLUME_SNAPSHOT_CONTENT" \
                    -o jsonpath='{.status.snapshotHandle}' 2>/dev/null)
                if [ -n "$GOLDEN_SNAPSHOT_ID" ]; then
                    log_info "✓ Extracted EBS snapshot ID: $GOLDEN_SNAPSHOT_ID"
                fi
            fi
            break
        fi

        # Check for errors
        SNAPSHOT_ERROR=$(oc --kubeconfig="$KUBECONFIG" get volumesnapshot $CLUSTER_SNAPSHOT_NAME -n $SERVICE_ACCOUNT_NAMESPACE \
            -o jsonpath='{.status.error.message}' 2>/dev/null || echo "")

        if [ -n "$SNAPSHOT_ERROR" ]; then
            log_error "VolumeSnapshot failed: $SNAPSHOT_ERROR"
            exit 1
        fi

        # Log progress every 5 minutes
        if [ $((SNAPSHOT_WAIT % 300)) -eq 0 ] && [ $SNAPSHOT_WAIT -gt 0 ]; then
            ELAPSED_MIN=$((SNAPSHOT_WAIT / 60))
            REMAINING_MIN=$(((SNAPSHOT_MAX_WAIT - SNAPSHOT_WAIT) / 60))
            log_info "  Snapshot status: Creating | Elapsed: ${ELAPSED_MIN}m | Timeout in: ${REMAINING_MIN}m"
        fi

        sleep 10
        SNAPSHOT_WAIT=$((SNAPSHOT_WAIT + 10))
    done

    if [ "$CLUSTER_SNAPSHOT_READY" != "true" ]; then
        log_error "Cluster-local VolumeSnapshot did not become ready within timeout"
        exit 1
    fi

    log_info ""
    log_info "═══════════════════════════════════════════════════════════════"
    log_info "Validating VolumeSnapshot → PVC restore path..."
    log_info "This tests the EXACT path all future VMs will use"
    log_info "═══════════════════════════════════════════════════════════════"

    # Clean up any existing validation resources
    log_info "Cleaning up any existing validation resources..."
    oc --kubeconfig="$KUBECONFIG" delete vm windows-validation-vm -n $SERVICE_ACCOUNT_NAMESPACE --ignore-not-found=true 2>/dev/null || true
    oc --kubeconfig="$KUBECONFIG" delete pvc windows-validation-disk -n $SERVICE_ACCOUNT_NAMESPACE --ignore-not-found=true 2>/dev/null || true

    # Wait for PVC deletion to complete
    PVC_DELETE_WAIT=0
    PVC_DELETE_MAX_WAIT=120
    while oc --kubeconfig="$KUBECONFIG" get pvc windows-validation-disk -n $SERVICE_ACCOUNT_NAMESPACE &>/dev/null; do
        if [ $PVC_DELETE_WAIT -ge $PVC_DELETE_MAX_WAIT ]; then
            log_warn "PVC deletion timeout - proceeding anyway"
            break
        fi
        sleep 2
        PVC_DELETE_WAIT=$((PVC_DELETE_WAIT + 2))
    done
    log_info "✓ Cleanup complete"

    # Create validation PVC from cluster-local VolumeSnapshot (testing the fast path!)
    log_info "Creating validation PVC from VolumeSnapshot $CLUSTER_SNAPSHOT_NAME..."
    cat <<EOF_VAL_PVC | oc --kubeconfig="$KUBECONFIG" apply -f -
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: windows-validation-disk
  namespace: ${SERVICE_ACCOUNT_NAMESPACE}
  labels:
    ocpctl.mg.dog8code.com/validation: "true"
spec:
  accessModes:
    - ReadWriteOnce
  volumeMode: Block
  storageClassName: ${SOURCE_STORAGE_CLASS}
  resources:
    requests:
      storage: ${SOURCE_SIZE}
  dataSource:
    apiGroup: snapshot.storage.k8s.io
    kind: VolumeSnapshot
    name: ${CLUSTER_SNAPSHOT_NAME}
EOF_VAL_PVC

    log_info "✓ Validation PVC created from VolumeSnapshot"

    # Wait for validation PVC to bind
    log_info "Waiting for validation PVC to bind (CSI snapshot restore)..."
    VAL_PVC_WAIT=0
    VAL_PVC_MAX_WAIT=300  # 5 minutes
    VAL_PVC_BOUND=false

    while [ $VAL_PVC_WAIT -lt $VAL_PVC_MAX_WAIT ]; do
        VAL_PVC_PHASE=$(oc --kubeconfig="$KUBECONFIG" get pvc windows-validation-disk -n $SERVICE_ACCOUNT_NAMESPACE -o jsonpath='{.status.phase}' 2>/dev/null || echo "NotFound")

        if [ "$VAL_PVC_PHASE" = "Bound" ]; then
            log_info "✓ Validation PVC bound successfully (~1 min CSI snapshot restore)"
            VAL_PVC_BOUND=true
            break
        elif [ "$VAL_PVC_PHASE" = "Failed" ] || [ "$VAL_PVC_PHASE" = "Lost" ]; then
            log_error "Validation PVC failed to bind"
            exit 1
        fi

        sleep 5
        VAL_PVC_WAIT=$((VAL_PVC_WAIT + 5))
    done

    if [ "$VAL_PVC_BOUND" != "true" ]; then
        log_error "Validation PVC did not bind within timeout"
        exit 1
    fi

    # Create validation VM using the PVC restored from VolumeSnapshot
    log_info "Creating validation VM from snapshot-restored PVC..."
    cat <<EOF_VAL_VM | oc --kubeconfig="$KUBECONFIG" apply -f -
apiVersion: kubevirt.io/v1
kind: VirtualMachine
metadata:
  name: windows-validation-vm
  namespace: ${SERVICE_ACCOUNT_NAMESPACE}
spec:
  running: false
  template:
    metadata:
      labels:
        kubevirt.io/vm: windows-validation-vm
    spec:
      domain:
        cpu:
          cores: 4
          sockets: 1
          threads: 1
        devices:
          disks:
          - disk:
              bus: sata
            name: rootdisk
          interfaces:
          - masquerade: {}
            name: default
            model: e1000
          networkInterfaceMultiqueue: true
          rng: {}
        features:
          acpi: {}
          apic: {}
          hyperv:
            frequencies: {}
            ipi: {}
            reenlightenment: {}
            relaxed: {}
            reset: {}
            runtime: {}
            spinlocks:
              spinlocks: 8191
            synic: {}
            synictimer:
              direct: {}
            tlbflush: {}
            vapic: {}
            vpindex: {}
        clock:
          timer:
            hyperv: {}
            pit:
              tickPolicy: delay
            rtc:
              tickPolicy: catchup
            hpet:
              present: false
        firmware:
          bootloader:
            efi:
              secureBoot: false
        machine:
          type: pc-q35-rhel9.2.0
        memory:
          guest: 8Gi
        resources:
          requests:
            memory: 8Gi
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
                - ${SOURCE_ZONE}
      volumes:
      - name: rootdisk
        persistentVolumeClaim:
          claimName: windows-validation-disk
EOF_VAL_VM

    log_info "✓ Validation VM created"
    log_info "  Starting validation VM to test Windows boot..."

    # Patch VM to start it
    oc --kubeconfig="$KUBECONFIG" patch virtualmachine windows-validation-vm -n $SERVICE_ACCOUNT_NAMESPACE --type merge -p '{"spec":{"running":true}}' 2>/dev/null

    # Wait for VM to reach Running state with stability checks
    VM_WAIT=0
    VM_MAX_WAIT=600  # 10 minutes
    VM_VALIDATED=false
    STABLE_COUNT=0
    STABLE_THRESHOLD=2  # 2 consecutive checks (10 seconds apart = 20s stability)

    while [ $VM_WAIT -lt $VM_MAX_WAIT ]; do
        VM_STATUS=$(oc --kubeconfig="$KUBECONFIG" get vm windows-validation-vm -n $SERVICE_ACCOUNT_NAMESPACE -o jsonpath='{.status.printableStatus}' 2>/dev/null || echo "Unknown")

        if [ "$VM_STATUS" = "Running" ]; then
            STABLE_COUNT=$((STABLE_COUNT + 1))
            if [ $STABLE_COUNT -ge $STABLE_THRESHOLD ]; then
                # Additional wait for Windows boot
                log_info "✓ VM stable in Running state - allowing 30s for Windows to complete boot..."
                sleep 30

                # Re-check still Running
                VM_STATUS=$(oc --kubeconfig="$KUBECONFIG" get vm windows-validation-vm -n $SERVICE_ACCOUNT_NAMESPACE -o jsonpath='{.status.printableStatus}' 2>/dev/null || echo "Unknown")
                if [ "$VM_STATUS" = "Running" ]; then
                    log_info "✓ Validation VM reached Running and remained stable"
                    VM_VALIDATED=true
                    break
                else
                    log_error "VM became unstable after initial stability check"
                    exit 1
                fi
            fi
        else
            # Reset stability counter if status changes
            STABLE_COUNT=0

            if [ "$VM_STATUS" = "Failed" ] || [ "$VM_STATUS" = "Error" ] || [ "$VM_STATUS" = "CrashLoopBackOff" ]; then
                log_error "Validation VM failed to start"
                exit 1
            fi
        fi

        if [ $((VM_WAIT % 30)) -eq 0 ] && [ $VM_WAIT -gt 0 ]; then
            log_info "  VM status: $VM_STATUS (${VM_WAIT}s elapsed)"
        fi

        sleep 10
        VM_WAIT=$((VM_WAIT + 10))
    done

    if [ "$VM_VALIDATED" != "true" ]; then
        log_error "Validation VM did not reach Running state within timeout"
        exit 1
    fi

    log_info "✓ Validation successful: VolumeSnapshot → PVC → VM boot path confirmed"
    log_info ""

    # Mark VolumeSnapshot as active (only after validation passes)
    log_info "Marking VolumeSnapshot as active..."
    oc --kubeconfig="$KUBECONFIG" label volumesnapshot "$CLUSTER_SNAPSHOT_NAME" -n $SERVICE_ACCOUNT_NAMESPACE \
        ocpctl.mg.dog8code.com/current=active --overwrite
    log_info "✓ Cluster-local VolumeSnapshot '$CLUSTER_SNAPSHOT_NAME' is now validated and ready"

    # Clean up validation resources
    log_info "Cleaning up validation resources..."
    oc --kubeconfig="$KUBECONFIG" patch virtualmachine windows-validation-vm -n $SERVICE_ACCOUNT_NAMESPACE --type merge -p '{"spec":{"running":false}}' 2>/dev/null || true
    sleep 5
    oc --kubeconfig="$KUBECONFIG" delete vm windows-validation-vm -n $SERVICE_ACCOUNT_NAMESPACE --ignore-not-found=true 2>/dev/null || true
    oc --kubeconfig="$KUBECONFIG" delete pvc windows-validation-disk -n $SERVICE_ACCOUNT_NAMESPACE --ignore-not-found=true 2>/dev/null || true
    log_info "✓ Validation resources cleaned up"

    # Publish EBS snapshot to SSM for regional discovery
    if [ -n "$GOLDEN_SNAPSHOT_ID" ]; then
        log_info ""
        log_info "Publishing validated EBS snapshot to SSM Parameter Store..."

        # Tag EBS snapshot
        aws ec2 create-tags --region "$REGION" --resources "$GOLDEN_SNAPSHOT_ID" \
            --tags \
                "Key=Name,Value=ocpctl-windows-10-oadp-v${SNAPSHOT_VERSION}" \
                "Key=ocpctl:managed,Value=true" \
                "Key=ocpctl:image-version,Value=${SNAPSHOT_VERSION}" \
                "Key=ocpctl:validated,Value=true" \
            2>/dev/null || true

        # Store in SSM for regional discovery
        SNAPSHOT_PARAM="/ocpctl/windows-snapshots/${SNAPSHOT_VERSION}/${REGION}"
        if aws ssm put-parameter --name "$SNAPSHOT_PARAM" --value "$GOLDEN_SNAPSHOT_ID" --type String --overwrite --region "$REGION" 2>/dev/null; then
            log_info "✓ Published to SSM: $SNAPSHOT_PARAM → $GOLDEN_SNAPSHOT_ID"
            log_info "  Future deployments in $REGION will use this snapshot"
        fi
    fi

elif [ "$DV_PHASE" = "Succeeded" ] && [ "$IMPORT_METHOD" = "snapshot" ]; then
    # Fast path: cluster already has regional EBS snapshot
    # VMs will clone from windows-source-snapshot (created during import)
    log_info ""
    log_info "Fast path deployment - using existing regional EBS snapshot"
    log_info "VMs will clone from windows-source-snapshot (fast CSI restore)"
fi

# Step 8: Create VM template for users
log_info ""
log_info "═══════════════════════════════════════════════════════════════"
log_info "Step 8: Creating Windows VM template for users..."
log_info "═══════════════════════════════════════════════════════════════"

# Get source PVC storage class for template
SOURCE_STORAGE_CLASS=$(oc --kubeconfig="$KUBECONFIG" get pvc windows -n $SERVICE_ACCOUNT_NAMESPACE -o jsonpath='{.spec.storageClassName}' 2>/dev/null)
SOURCE_PV=$(oc --kubeconfig="$KUBECONFIG" get pvc windows -n $SERVICE_ACCOUNT_NAMESPACE -o jsonpath='{.spec.volumeName}' 2>/dev/null)
SOURCE_ZONE=$(oc --kubeconfig="$KUBECONFIG" get pv "$SOURCE_PV" -o jsonpath='{.spec.nodeAffinity.required.nodeSelectorTerms[0].matchExpressions[?(@.key=="topology.ebs.csi.aws.com/zone")].values[0]}' 2>/dev/null)
if [ -z "$SOURCE_ZONE" ]; then
    SOURCE_ZONE=$(oc --kubeconfig="$KUBECONFIG" get pv "$SOURCE_PV" -o jsonpath='{.spec.nodeAffinity.required.nodeSelectorTerms[0].matchExpressions[?(@.key=="topology.kubernetes.io/zone")].values[0]}' 2>/dev/null)
fi

# Use the same storage class as source PVC
CLONE_STORAGE_CLASS="${SOURCE_STORAGE_CLASS}"

# Set default snapshot name if not already set (for fast path deployments)
if [ -z "$CLUSTER_SNAPSHOT_NAME" ]; then
    CLUSTER_SNAPSHOT_NAME="windows-golden-snapshot-v${SNAPSHOT_VERSION}"
fi

log_info "Creating Windows VM template..."
log_info "  Storage class: ${CLONE_STORAGE_CLASS}"
log_info "  Snapshot: ${CLUSTER_SNAPSHOT_NAME} (VolumeSnapshot → PVC fast restore)"
export STORAGE_CLASS="${CLONE_STORAGE_CLASS}"
export SNAPSHOT_NAME="${CLUSTER_SNAPSHOT_NAME}"
export ACCESS_MODE="ReadWriteOnce"
cat "${SCRIPT_DIR}/4_windows10-template.yaml" | envsubst '${STORAGE_CLASS},${SNAPSHOT_NAME}' | oc --kubeconfig="$KUBECONFIG" apply -f -
log_info "✓ VM Template created: windows10-oadp-vm"
log_info "  VMs created from this template will restore PVC from VolumeSnapshot (fast CSI restore)"

# Wait for CNV webhook services to be ready
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

# Step 9: Create default Windows VM from template
log_info ""
log_info "═══════════════════════════════════════════════════════════════"
log_info "Step 9: Creating default Windows VM from template..."
log_info "═══════════════════════════════════════════════════════════════"

log_info "Creating default Windows VM: windows-vm..."
log_info "  Using VolumeSnapshot: ${CLUSTER_SNAPSHOT_NAME}"
oc --kubeconfig="$KUBECONFIG" process -n $SERVICE_ACCOUNT_NAMESPACE windows10-oadp-vm \
    -p VM_NAME=windows-vm \
    -p VM_NAMESPACE=$SERVICE_ACCOUNT_NAMESPACE \
    -p STORAGE_CLASS=${CLONE_STORAGE_CLASS} \
    -p SNAPSHOT_NAME=${CLUSTER_SNAPSHOT_NAME} | oc --kubeconfig="$KUBECONFIG" apply -f -

log_info "✓ Windows VM created: windows-vm (namespace: $SERVICE_ACCOUNT_NAMESPACE)"
log_info "  VM disk will restore from VolumeSnapshot (fast CSI restore ~1 min)"
log_info "  VM is created but not started - start via OpenShift Console or CLI"

log_info "✓ Setup complete - VM resources ready for use"

log_info ""
log_info "═══════════════════════════════════════════════════════════════"
log_info "✅ IRSA Setup Complete!"
log_info "═══════════════════════════════════════════════════════════════"
log_info ""
if [ -n "$ROLE_ARN" ]; then
    log_info "IAM Role: $ROLE_NAME"
    log_info "Role ARN: $ROLE_ARN"
else
    log_info "Credentials Mode: Mint (using cluster IAM credentials)"
fi
log_info "ServiceAccount: $SERVICE_ACCOUNT_NAMESPACE/$SERVICE_ACCOUNT_NAME"
log_info ""
log_info "Windows VM Resources:"
log_info "  Base Image: windows (pristine PVC, 70GB Windows 10)"
log_info "  Cluster Snapshot: ${CLUSTER_SNAPSHOT_NAME} (validated, VolumeSnapshot → PVC fast restore)"
log_info "  Default VM: windows-vm (namespace: $SERVICE_ACCOUNT_NAMESPACE, 4 cores, 8GB RAM)"
log_info "  Template: windows10-oadp-vm (namespace: $SERVICE_ACCOUNT_NAMESPACE)"
log_info "  Storage Class: ${CLONE_STORAGE_CLASS} (zone: ${SOURCE_ZONE})"
if [ -n "$GOLDEN_SNAPSHOT_ID" ]; then
    log_info "  Regional EBS Snapshot: ${GOLDEN_SNAPSHOT_ID} (published to SSM for future deployments)"
fi
log_info ""
log_info "Architecture:"
log_info "  All VMs use explicit VolumeSnapshot → PVC restore (fast CSI snapshot path)"
log_info "  Expected VM disk creation time: ~1 minute (CSI snapshot restore)"
log_info "  Avoids slow CDI PVC cloning (20-25 minutes)"
log_info ""
log_info "Next Steps:"
log_info "  1. Start the default Windows VM:"
log_info "     oc patch vm windows-vm -n $SERVICE_ACCOUNT_NAMESPACE --type merge -p '{\"spec\":{\"running\":true}}'"
log_info "     OR via OpenShift Console: Virtualization → VirtualMachines → windows-vm → Start"
log_info ""
log_info "  2. Create additional VMs using the template (~1 min disk creation):"
log_info "     oc process -n $SERVICE_ACCOUNT_NAMESPACE windows10-oadp-vm \\"
log_info "       -p VM_NAME=my-windows-vm -p VM_NAMESPACE=default | oc apply -f -"
log_info ""
log_info "  Note: All VMs restore disk from VolumeSnapshot ${CLUSTER_SNAPSHOT_NAME} (fast path)"
log_info ""
log_info "═══════════════════════════════════════════════════════════════"

exit 0
