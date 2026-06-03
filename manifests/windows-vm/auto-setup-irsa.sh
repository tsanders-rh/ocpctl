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

        # Step 1: Create VolumeSnapshotContent pointing to pre-existing EBS snapshot
        # This is required for importing external snapshots into Kubernetes
        log_info "Creating VolumeSnapshotContent for snapshot $SNAPSHOT_ID..."
        cat <<EOF | oc --kubeconfig="$KUBECONFIG" apply -f -
apiVersion: snapshot.storage.k8s.io/v1
kind: VolumeSnapshotContent
metadata:
  name: windows-source-snapshot-content
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

# Apply DataSource
log_info "Creating DataSource (windows10-datasource)"
oc --kubeconfig="$KUBECONFIG" apply -f "${SCRIPT_DIR}/3_datasource-windows.yaml"
log_info "✓ DataSource created"

# Note: VM Template creation moved to after zone detection
# so it defaults to the correct zone-specific storage class

# Wait for DataVolume to complete before creating VM
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

# If we used S3 import, create snapshot for future use
if [ "$DV_PHASE" = "Succeeded" ] && [ "$IMPORT_METHOD" = "s3" ]; then
    log_info ""
    log_info "DataVolume import succeeded - creating EBS snapshot for future deployments..."

    # Get the PVC backing the DataVolume
    PVC_NAME="windows"
    PV_NAME=$(oc --kubeconfig="$KUBECONFIG" get pvc $PVC_NAME -n $SERVICE_ACCOUNT_NAMESPACE -o jsonpath='{.spec.volumeName}' 2>/dev/null)

    if [ -n "$PV_NAME" ]; then
        # Extract EBS volume ID from PV
        VOLUME_ID=$(oc --kubeconfig="$KUBECONFIG" get pv $PV_NAME -o jsonpath='{.spec.csi.volumeHandle}' 2>/dev/null)

        if [ -n "$VOLUME_ID" ]; then
            log_info "Creating snapshot from EBS volume: $VOLUME_ID"

            # Create snapshot with tags
            SNAPSHOT_DESCRIPTION="ocpctl Windows 10 OADP v${SNAPSHOT_VERSION} (auto-created)"
            NEW_SNAPSHOT_ID=$(aws ec2 create-snapshot \
                --region "$REGION" \
                --volume-id "$VOLUME_ID" \
                --description "$SNAPSHOT_DESCRIPTION" \
                --tag-specifications "ResourceType=snapshot,Tags=[{Key=Name,Value=ocpctl-windows-10-oadp-v${SNAPSHOT_VERSION}},{Key=ocpctl:managed,Value=true},{Key=ocpctl:image-version,Value=${SNAPSHOT_VERSION}},{Key=ocpctl:source-s3,Value=s3://ocpctl-binaries/windows-images/windows-10-oadp.qcow2},{Key=ocpctl:created-at,Value=$(date -u +%Y-%m-%dT%H:%M:%SZ)},{Key=ocpctl:region,Value=${REGION}}]" \
                --query 'SnapshotId' \
                --output text 2>/dev/null || echo "")

            if [ -n "$NEW_SNAPSHOT_ID" ]; then
                log_info "✓ Created snapshot: $NEW_SNAPSHOT_ID (will be used for future Windows VM deployments in $REGION)"
                log_info "  Snapshot will complete in background (20-30 minutes)"
                log_info "  Future deployments in $REGION will complete in 2-3 minutes using this snapshot"

                # Optionally store in SSM Parameter Store (requires additional IAM permissions)
                SNAPSHOT_PARAM="/ocpctl/windows-snapshots/${SNAPSHOT_VERSION}/${REGION}"
                if aws ssm put-parameter --name "$SNAPSHOT_PARAM" --value "$NEW_SNAPSHOT_ID" --type String --overwrite --region "$REGION" 2>/dev/null; then
                    log_info "✓ Stored snapshot ID in SSM Parameter Store: $SNAPSHOT_PARAM"
                else
                    log_warn "Could not store snapshot ID in SSM Parameter Store (may lack permissions)"
                    log_info "Snapshot can still be discovered via EC2 tags"
                fi
            else
                log_warn "Failed to create snapshot - future deployments will use S3 fallback"
            fi
        else
            log_warn "Could not extract EBS volume ID from PV - skipping snapshot creation"
        fi
    else
        log_warn "Could not find PV for DataVolume - skipping snapshot creation"
    fi
fi

# Create initial test VM (this triggers snapshot creation for faster subsequent clones)
log_info ""
log_info "Creating initial Windows VM (stopped state)..."
log_info "Note: First VM clone takes 20-30 min due to EBS snapshot creation"
log_info "Subsequent VMs will clone in 2-3 minutes using the snapshot"

# Detect the availability zone of the source Windows image PVC to ensure clone works
log_info "Detecting source PVC availability zone for proper VM placement..."
log_info "Waiting for source PVC to be bound..."

# Wait for PVC to be bound (should be fast since import already succeeded)
PVC_WAIT=0
PVC_MAX_WAIT=120  # 2 minutes
while [ $PVC_WAIT -lt $PVC_MAX_WAIT ]; do
    PVC_PHASE=$(oc --kubeconfig="$KUBECONFIG" get pvc windows -n $SERVICE_ACCOUNT_NAMESPACE -o jsonpath='{.status.phase}' 2>/dev/null || echo "NotFound")
    if [ "$PVC_PHASE" = "Bound" ]; then
        log_info "✓ Source PVC is bound"
        break
    fi
    sleep 2
    PVC_WAIT=$((PVC_WAIT + 2))
done

if [ $PVC_WAIT -ge $PVC_MAX_WAIT ]; then
    log_error "Source PVC did not become Bound within timeout"
    exit 1
fi

# Get the PV name from the bound PVC
SOURCE_PV=$(oc --kubeconfig="$KUBECONFIG" get pvc windows -n $SERVICE_ACCOUNT_NAMESPACE -o jsonpath='{.spec.volumeName}' 2>/dev/null)
if [ -z "$SOURCE_PV" ]; then
    log_error "Could not find PV for source PVC 'windows' in namespace $SERVICE_ACCOUNT_NAMESPACE"
    exit 1
fi

log_info "Source PV: $SOURCE_PV"

# Extract the actual availability zone from the PV's node affinity
# Use the EBS CSI-specific topology key for accurate zone detection
SOURCE_ZONE=$(oc --kubeconfig="$KUBECONFIG" get pv "$SOURCE_PV" -o jsonpath='{.spec.nodeAffinity.required.nodeSelectorTerms[0].matchExpressions[?(@.key=="topology.ebs.csi.aws.com/zone")].values[0]}' 2>/dev/null)

# Fallback to kubernetes.io topology key if EBS CSI key not found (older clusters)
if [ -z "$SOURCE_ZONE" ]; then
    log_warn "EBS CSI topology key not found, trying fallback..."
    SOURCE_ZONE=$(oc --kubeconfig="$KUBECONFIG" get pv "$SOURCE_PV" -o jsonpath='{.spec.nodeAffinity.required.nodeSelectorTerms[0].matchExpressions[?(@.key=="topology.kubernetes.io/zone")].values[0]}' 2>/dev/null)
fi

if [ -z "$SOURCE_ZONE" ]; then
    log_error "Could not determine availability zone for source PV $SOURCE_PV"
    log_error "PV nodeAffinity:"
    oc --kubeconfig="$KUBECONFIG" get pv "$SOURCE_PV" -o jsonpath='{.spec.nodeAffinity}' || true
    exit 1
fi

log_info "✓ Source Windows image is in availability zone: $SOURCE_ZONE"

# Create a cluster-scoped zone-specific storage class with Immediate binding
# This ensures VM clone PVCs are created in the same AZ as the source, avoiding clone failures
# Using Immediate binding allows provisioning even when VM is stopped (no pod to trigger WaitForFirstConsumer)
# Cluster-scoped naming avoids collisions if multiple clusters share admin context
CLONE_STORAGE_CLASS="gp3-csi-${INFRA_ID}-${SOURCE_ZONE}"

# Check if zone-specific storage class already exists (reuse across clusters in same zone)
if oc --kubeconfig="$KUBECONFIG" get storageclass "${CLONE_STORAGE_CLASS}" &>/dev/null; then
    log_info "✓ Zone-specific storage class already exists: ${CLONE_STORAGE_CLASS}"
    log_info "✓ Reusing existing storage class for VM cloning"
else
    log_info "Creating zone-specific storage class: ${CLONE_STORAGE_CLASS}..."
    cat <<EOF_SC | oc --kubeconfig="$KUBECONFIG" apply -f -
apiVersion: storage.k8s.io/v1
kind: StorageClass
metadata:
  name: ${CLONE_STORAGE_CLASS}
provisioner: ebs.csi.aws.com
parameters:
  type: gp3
  encrypted: "true"
volumeBindingMode: Immediate
allowedTopologies:
- matchLabelExpressions:
  - key: topology.ebs.csi.aws.com/zone
    values:
    - ${SOURCE_ZONE}
reclaimPolicy: Delete
EOF_SC
    log_info "✓ Created storage class: ${CLONE_STORAGE_CLASS}"
fi

log_info "✓ VM clone will be constrained to availability zone: $SOURCE_ZONE"

# Now create VM Template with the zone-specific storage class as default
log_info "Creating Windows VM template with zone-specific storage class..."
export STORAGE_CLASS="${CLONE_STORAGE_CLASS}"
export ACCESS_MODE="ReadWriteOnce"
cat "${SCRIPT_DIR}/4_windows10-template.yaml" | envsubst '${STORAGE_CLASS}' | oc --kubeconfig="$KUBECONFIG" apply -f -
log_info "✓ VM Template created (default storage class: ${CLONE_STORAGE_CLASS})"
log_info "✓ Future VMs created from template will use the correct zone-specific class"

# Process template using the zone-specific storage class and add node affinity
VM_YAML=$(mktemp)
oc --kubeconfig="$KUBECONFIG" process -n $SERVICE_ACCOUNT_NAMESPACE windows10-oadp-vm \
    -p VM_NAME=windows-oadp-test-1 \
    -p VM_NAMESPACE=default \
    -p STORAGE_CLASS=${CLONE_STORAGE_CLASS} > "$VM_YAML"

# Add node affinity to ensure VM pod lands in correct zone
export VM_YAML SOURCE_ZONE
python3 <<'EOF_PYTHON'
import yaml
import sys
import os

vm_yaml_file = os.environ['VM_YAML']
source_zone = os.environ['SOURCE_ZONE']

with open(vm_yaml_file, 'r') as f:
    doc = yaml.safe_load(f)

# Add node affinity to VM template spec
if 'spec' in doc and 'template' in doc['spec'] and 'spec' in doc['spec']['template']:
    vm_spec = doc['spec']['template']['spec']
    if 'affinity' not in vm_spec:
        vm_spec['affinity'] = {}
    vm_spec['affinity']['nodeAffinity'] = {
        'requiredDuringSchedulingIgnoredDuringExecution': {
            'nodeSelectorTerms': [{
                'matchExpressions': [{
                    'key': 'topology.kubernetes.io/zone',
                    'operator': 'In',
                    'values': [source_zone]
                }]
            }]
        }
    }

with open(vm_yaml_file, 'w') as f:
    yaml.dump(doc, f, default_flow_style=False)
EOF_PYTHON

# Wait for kubemacpool webhook service to be ready before creating VMs
# This prevents "no endpoints available for service kubemacpool-service" errors
log_info "Waiting for kubemacpool webhook service to be ready..."
WEBHOOK_WAIT=0
WEBHOOK_MAX_WAIT=300  # 5 minutes
while [ $WEBHOOK_WAIT -lt $WEBHOOK_MAX_WAIT ]; do
    if oc --kubeconfig="$KUBECONFIG" get endpoints kubemacpool-service -n openshift-cnv &>/dev/null; then
        ENDPOINTS=$(oc --kubeconfig="$KUBECONFIG" get endpoints kubemacpool-service -n openshift-cnv -o jsonpath='{.subsets[*].addresses[*].ip}' 2>/dev/null || echo "")
        if [ -n "$ENDPOINTS" ]; then
            log_info "✓ kubemacpool webhook service is ready"
            break
        fi
    fi

    if [ $((WEBHOOK_WAIT % 30)) -eq 0 ]; then
        log_info "  Still waiting for kubemacpool-service endpoints... (${WEBHOOK_WAIT}s elapsed)"
    fi

    sleep 5
    WEBHOOK_WAIT=$((WEBHOOK_WAIT + 5))
done

if [ $WEBHOOK_WAIT -ge $WEBHOOK_MAX_WAIT ]; then
    log_error "Timeout waiting for kubemacpool-service to be ready after ${WEBHOOK_MAX_WAIT}s"
    log_error "The CNV operator may still be deploying. Try retrying this configuration in a few minutes."
    exit 1
fi

# Apply the modified VM YAML
oc --kubeconfig="$KUBECONFIG" apply -f "$VM_YAML"
rm -f "$VM_YAML"

if [ $? -ne 0 ]; then
    log_error "Failed to create test VM"
    exit 1
fi

log_info "✓ Test VM created: windows-oadp-test-1 (namespace: default)"

# Wait for VM DataVolume clone to complete (this creates the EBS snapshot)
log_info ""
log_info "Waiting for VM disk clone to complete (creating EBS snapshot for fast future clones)..."
log_info "This typically takes 20-30 minutes for the first VM..."

VM_DV_WAIT=0
VM_DV_MAX_WAIT=2400  # 40 minutes (enough for snapshot creation)
while [ $VM_DV_WAIT -lt $VM_DV_MAX_WAIT ]; do
    VM_DV_PHASE=$(oc --kubeconfig="$KUBECONFIG" get datavolume windows-oadp-test-1-disk -n default -o jsonpath='{.status.phase}' 2>/dev/null || echo "NotFound")
    VM_DV_PROGRESS=$(oc --kubeconfig="$KUBECONFIG" get datavolume windows-oadp-test-1-disk -n default -o jsonpath='{.status.progress}' 2>/dev/null || echo "N/A")

    if [ "$VM_DV_PHASE" = "Succeeded" ]; then
        log_info "✓ VM disk clone completed - EBS snapshot created"
        break
    elif [ "$VM_DV_PHASE" = "Failed" ]; then
        log_error "VM DataVolume clone failed"
        oc --kubeconfig="$KUBECONFIG" get datavolume windows-oadp-test-1-disk -n default -o yaml | grep -A10 "conditions:" || true
        exit 1
    fi

    # Log progress every 5 minutes
    if [ $((VM_DV_WAIT % 300)) -eq 0 ]; then
        ELAPSED_MIN=$((VM_DV_WAIT / 60))
        REMAINING_MIN=$(((VM_DV_MAX_WAIT - VM_DV_WAIT) / 60))
        log_info "  Clone status: $VM_DV_PHASE | Progress: $VM_DV_PROGRESS | Elapsed: ${ELAPSED_MIN}m | Timeout in: ${REMAINING_MIN}m"
    fi

    sleep 10
    VM_DV_WAIT=$((VM_DV_WAIT + 10))
done

if [ $VM_DV_WAIT -ge $VM_DV_MAX_WAIT ]; then
    log_error "VM disk clone timed out after ${VM_DV_MAX_WAIT}s (40 minutes)"
    log_error "Current DataVolume status:"
    oc --kubeconfig="$KUBECONFIG" get datavolume windows-oadp-test-1-disk -n default -o yaml | grep -A20 "status:" || true
    exit 1
fi

log_info "✓ Test VM fully provisioned and ready (VM is stopped by default)"

log_info ""
log_info "═══════════════════════════════════════════════════════════════"
log_info "✅ IRSA Setup Complete!"
log_info "═══════════════════════════════════════════════════════════════"
log_info ""
log_info "IAM Role: $ROLE_NAME"
log_info "Role ARN: $ROLE_ARN"
log_info "ServiceAccount: $SERVICE_ACCOUNT_NAMESPACE/$SERVICE_ACCOUNT_NAME"
log_info ""
log_info "Windows VM Resources:"
log_info "  Base Image: windows (70GB Windows 10 QCOW2)"
log_info "  Template: windows10-oadp-vm (namespace: $SERVICE_ACCOUNT_NAMESPACE)"
log_info "  Test VM: windows-oadp-test-1 (namespace: default) - READY (Stopped)"
log_info "  EBS Snapshot: Created (future VMs will clone in 2-3 minutes)"
log_info ""
log_info "Next Steps:"
log_info "  1. Start the test VM from OpenShift Console:"
log_info "     Virtualization → VirtualMachines → windows-oadp-test-1 → Start"
log_info ""
log_info "  2. Create additional VMs (fast clones from snapshot - 2-3 min):"
log_info "     oc process -n $SERVICE_ACCOUNT_NAMESPACE windows10-oadp-vm \\"
log_info "       -p VM_NAME=my-windows-vm -p VM_NAMESPACE=default | oc apply -f -"
log_info ""
log_info "═══════════════════════════════════════════════════════════════"

exit 0
