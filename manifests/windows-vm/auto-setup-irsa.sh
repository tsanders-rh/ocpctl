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

# Construct OIDC provider URL
OIDC_PROVIDER="${INFRA_ID}-oidc.s3.${REGION}.amazonaws.com"
OIDC_PROVIDER_ARN="arn:aws:iam::${AWS_ACCOUNT_ID}:oidc-provider/${OIDC_PROVIDER}"

log_info "OIDC Provider: $OIDC_PROVIDER"

# Verify OIDC provider exists
if ! aws iam get-open-id-connect-provider --open-id-connect-provider-arn "$OIDC_PROVIDER_ARN" &>/dev/null; then
    log_error "OIDC provider not found: $OIDC_PROVIDER_ARN"
    log_error "Cluster may not be using STS mode"
    exit 1
fi
log_info "✓ OIDC provider verified"

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

# Create DataVolume (IRSA version)
log_info "Creating DataVolume for Windows image download"
cat <<EOF | oc --kubeconfig="$KUBECONFIG" apply -f -
apiVersion: cdi.kubevirt.io/v1beta1
kind: DataVolume
metadata:
  name: windows
  namespace: ${SERVICE_ACCOUNT_NAMESPACE}
  annotations:
    cdi.kubevirt.io/storage.pod.serviceAccount: ${SERVICE_ACCOUNT_NAME}
spec:
  contentType: kubevirt
  source:
    s3:
      url: "https://s3.${REGION}.amazonaws.com/ocpctl-binaries/windows-images/windows-10-oadp.qcow2"
  storage:
    resources:
      requests:
        storage: 70Gi
    storageClassName: ocs-storagecluster-ceph-rbd-virtualization
EOF
log_info "✓ DataVolume created (import starting)"

# Apply DataSource
log_info "Creating DataSource (windows10-datasource)"
oc --kubeconfig="$KUBECONFIG" apply -f "${SCRIPT_DIR}/3_datasource-windows.yaml"
log_info "✓ DataSource created"

# Apply VM Template (to openshift namespace)
log_info "Creating Windows VM template in openshift namespace"
oc --kubeconfig="$KUBECONFIG" apply -f "${SCRIPT_DIR}/4_windows10-template.yaml"
log_info "✓ VM Template created"

log_info ""
log_info "═══════════════════════════════════════════════════════════════"
log_info "✅ IRSA Setup Complete!"
log_info "═══════════════════════════════════════════════════════════════"
log_info ""
log_info "IAM Role: $ROLE_NAME"
log_info "Role ARN: $ROLE_ARN"
log_info "ServiceAccount: $SERVICE_ACCOUNT_NAMESPACE/$SERVICE_ACCOUNT_NAME"
log_info ""
log_info "Windows image download started (5-10 minutes)"
log_info "Monitor progress:"
log_info "  oc get datavolume windows -n $SERVICE_ACCOUNT_NAMESPACE -w"
log_info ""
log_info "Once complete, VMs will appear in OpenShift Console:"
log_info "  Virtualization → Templates → Search for 'Windows 10 VM (OADP)'"
log_info ""
log_info "Or create from CLI:"
log_info "  oc process -n openshift windows10-oadp-vm -p VM_NAME=my-vm -p VM_NAMESPACE=default | oc apply -f -"
log_info ""
log_info "═══════════════════════════════════════════════════════════════"

exit 0
