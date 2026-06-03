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

    # Attach S3 read-only policy to user
    log_info "Attaching S3 read-only policy to IAM user"
    SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
    aws iam put-user-policy \
        --user-name "$IAM_USER_NAME" \
        --policy-name S3WindowsImageReadOnly \
        --policy-document file://${SCRIPT_DIR}/iam-policy.json
    log_info "✓ Policy attached to IAM user"

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
    log_info "Creating DataVolume for Windows image download (using $STORAGE_CLASS)"

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

    log_info "✓ DataVolume created (import starting - this will take 5-10 minutes)"
fi

# Apply DataSource
log_info "Creating DataSource (windows10-datasource)"
oc --kubeconfig="$KUBECONFIG" apply -f "${SCRIPT_DIR}/3_datasource-windows.yaml"
log_info "✓ DataSource created"

# Note: VM Template creation moved to after zone detection
# so it defaults to the correct zone-specific storage class

# Wait for DataVolume to complete before creating VM
log_info ""
log_info "Waiting for Windows image download to complete before creating test VM..."
log_info "(This ensures the snapshot is created during deployment, making future VM creation faster)"
log_info "Note: Image import can take 2-4 hours depending on download speed and conversion time"

# Wait up to 4 hours for DataVolume to succeed (handles downloads, conversions, and restarts)
WAIT_TIME=0
MAX_WAIT=14400  # 4 hours (enough for full import + cluster restart scenarios)
PROGRESS_LOG_INTERVAL=300  # Log progress every 5 minutes
while [ $WAIT_TIME -lt $MAX_WAIT ]; do
    DV_PHASE=$(oc --kubeconfig="$KUBECONFIG" get datavolume windows -n $SERVICE_ACCOUNT_NAMESPACE -o jsonpath='{.status.phase}' 2>/dev/null || echo "NotFound")
    DV_PROGRESS=$(oc --kubeconfig="$KUBECONFIG" get datavolume windows -n $SERVICE_ACCOUNT_NAMESPACE -o jsonpath='{.status.progress}' 2>/dev/null || echo "N/A")

    if [ "$DV_PHASE" = "Succeeded" ]; then
        log_info "✓ Windows image download completed"
        break
    elif [ "$DV_PHASE" = "Failed" ]; then
        log_error "DataVolume import failed"
        # Get more details about failure
        oc --kubeconfig="$KUBECONFIG" get datavolume windows -n $SERVICE_ACCOUNT_NAMESPACE -o yaml | grep -A10 "conditions:" || true
        exit 1
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
    log_error "DataVolume import timed out after ${MAX_WAIT}s (4 hours)"
    log_error "This likely indicates a problem with the S3 presigned URL or network connectivity"
    log_error "Current DataVolume status:"
    oc --kubeconfig="$KUBECONFIG" get datavolume windows -n $SERVICE_ACCOUNT_NAMESPACE -o yaml | grep -A20 "status:" || true
    exit 1
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
