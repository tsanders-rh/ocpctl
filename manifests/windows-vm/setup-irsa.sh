#!/bin/bash
#
# Setup IRSA (IAM Roles for Service Accounts) for Windows Image Access
#
# This script creates an IAM role that can be assumed by a Kubernetes ServiceAccount
# in the cluster, eliminating the need for static AWS credentials.
#
# Prerequisites:
# - Cluster must be created with ocpctl (which uses STS mode with OIDC)
# - You need the cluster's infraID and region
#
# Usage:
#   ./setup-irsa.sh <infraID> <region>
#
# Example:
#   ./setup-irsa.sh sandersvirt6-abc123 us-east-1
#

set -euo pipefail

if [ $# -lt 2 ] || [ $# -gt 3 ]; then
  echo "Usage: $0 <infraID> <region> [oidcIssuerURL]"
  echo "Example: $0 sandersvirt6-abc123 us-east-1"
  echo "Example: $0 sandersvirt6-abc123 us-east-1 https://rh-oidc.s3.us-east-1.amazonaws.com/abc123"
  exit 1
fi

INFRA_ID="$1"
REGION="$2"
OIDC_ISSUER_URL="${3:-}"
ROLE_NAME="ocpctl-windows-image-s3-reader"
SERVICE_ACCOUNT_NAME="windows-image-importer"
SERVICE_ACCOUNT_NAMESPACE="openshift-virtualization-os-images"

# Pre-flight: verify AWS credentials are valid
echo "Verifying AWS credentials..."
if ! AWS_ACCOUNT_ID=$(aws sts get-caller-identity --query Account --output text 2>&1); then
  echo ""
  echo "ERROR: AWS credentials are invalid or expired."
  echo ""
  echo "  Error details: $AWS_ACCOUNT_ID"
  echo ""
  echo "To fix this, refresh your AWS credentials. Common methods:"
  echo "  - AWS SSO:        aws sso login --profile <profile>"
  echo "  - saml2aws:       saml2aws login"
  echo "  - Environment:    export AWS_ACCESS_KEY_ID=... AWS_SECRET_ACCESS_KEY=... AWS_SESSION_TOKEN=..."
  echo "  - AWS profile:    export AWS_PROFILE=<profile-name>"
  echo ""
  echo "After refreshing, verify with:  aws sts get-caller-identity"
  exit 1
fi
echo "AWS Account ID: $AWS_ACCOUNT_ID"

# Determine OIDC provider URL
# If not passed as argument, try to get it from the cluster, then fall back to the standard pattern
if [ -z "$OIDC_ISSUER_URL" ]; then
  if command -v oc &>/dev/null; then
    OIDC_ISSUER_URL=$(oc get authentication.config.openshift.io cluster \
      -o jsonpath='{.spec.serviceAccountIssuer}' 2>/dev/null || true)
  fi
fi

if [ -n "$OIDC_ISSUER_URL" ]; then
  # Strip the https:// prefix to get the provider host path
  OIDC_PROVIDER="${OIDC_ISSUER_URL#https://}"
  echo "OIDC Issuer (from cluster): $OIDC_ISSUER_URL"
else
  # Fall back to the standard IPI STS pattern
  OIDC_PROVIDER="${INFRA_ID}-oidc.s3.${REGION}.amazonaws.com"
  echo "OIDC Issuer: not found in cluster, using standard IPI STS pattern"
fi

OIDC_PROVIDER_ARN="arn:aws:iam::${AWS_ACCOUNT_ID}:oidc-provider/${OIDC_PROVIDER}"

echo "OIDC Provider: $OIDC_PROVIDER"
echo "OIDC Provider ARN: $OIDC_PROVIDER_ARN"

# Verify OIDC provider exists
if ! aws iam get-open-id-connect-provider --open-id-connect-provider-arn "$OIDC_PROVIDER_ARN" &>/dev/null; then
  echo ""
  echo "⚠ OIDC provider not found in IAM: $OIDC_PROVIDER_ARN"
  echo ""
  echo "This cluster was not created with STS/OIDC mode, so IRSA cannot be"
  echo "used. The fallback is to store static AWS credentials as a Kubernetes"
  echo "secret instead."
  echo ""

  SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
  if [ ! -f "$SCRIPT_DIR/create-s3-secret.sh" ]; then
    echo "ERROR: create-s3-secret.sh not found at $SCRIPT_DIR"
    echo "Cannot continue without either IRSA or static credentials."
    exit 1
  fi

  read -rp "Would you like to create the static credentials secret now? [y/N] " REPLY
  echo ""
  if [[ "$REPLY" =~ ^[Yy]$ ]]; then
    exec "$SCRIPT_DIR/create-s3-secret.sh"
  else
    echo "Skipped. To set up credentials manually, run:"
    echo "  ./create-s3-secret.sh"
    exit 1
  fi
fi
echo "✓ OIDC provider verified"

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
echo ""
echo "Creating IAM role: $ROLE_NAME"
if aws iam get-role --role-name "$ROLE_NAME" &>/dev/null; then
  echo "⚠ Role already exists, updating trust policy..."
  echo "$TRUST_POLICY" > /tmp/trust-policy.json
  aws iam update-assume-role-policy \
    --role-name "$ROLE_NAME" \
    --policy-document file:///tmp/trust-policy.json
  rm /tmp/trust-policy.json
else
  echo "$TRUST_POLICY" > /tmp/trust-policy.json
  aws iam create-role \
    --role-name "$ROLE_NAME" \
    --assume-role-policy-document file:///tmp/trust-policy.json \
    --description "IRSA role for CDI to download Windows images from S3"
  rm /tmp/trust-policy.json
  echo "✓ Role created"
fi

# Attach S3 read-only policy
echo ""
echo "Attaching S3 read-only policy..."
aws iam put-role-policy \
  --role-name "$ROLE_NAME" \
  --policy-name S3WindowsImageReadOnly \
  --policy-document file://iam-policy.json
echo "✓ Policy attached"

# Get role ARN
ROLE_ARN=$(aws iam get-role --role-name "$ROLE_NAME" --query 'Role.Arn' --output text)
echo ""
echo "✓ IAM Role ARN: $ROLE_ARN"

# Generate ServiceAccount manifest
echo ""
echo "Generating ServiceAccount manifest..."
cat > 1a_windows-image-serviceaccount.yaml <<EOF
---
# ServiceAccount with IAM Role for Windows Image Access (IRSA)
#
# This ServiceAccount uses IRSA (IAM Roles for Service Accounts) to access S3
# without storing static AWS credentials in the cluster.
#
# How it works:
# 1. ServiceAccount is annotated with IAM role ARN
# 2. CDI importer pod uses this ServiceAccount
# 3. Pod gets temporary AWS credentials via OIDC federation
# 4. Credentials auto-rotate and are never stored in etcd
#
# Setup:
#   oc create namespace openshift-virtualization-os-images
#   oc apply -f 1a_windows-image-serviceaccount.yaml
#
apiVersion: v1
kind: ServiceAccount
metadata:
  name: ${SERVICE_ACCOUNT_NAME}
  namespace: ${SERVICE_ACCOUNT_NAMESPACE}
  annotations:
    eks.amazonaws.com/role-arn: ${ROLE_ARN}
EOF

echo "✓ Created: 1a_windows-image-serviceaccount.yaml"

# Generate updated DataVolume manifest (IRSA version)
echo ""
echo "Generating IRSA-enabled DataVolume manifest..."
cat > 2_windows-datavolume-irsa.yaml <<EOF
---
# Windows 10 DataVolume - Downloads from S3 using IRSA
#
# This DataVolume uses IRSA (IAM Roles for Service Accounts) instead of
# static credentials for better security.
#
# Process:
# 1. CDI creates an importer pod with the ServiceAccount
# 2. Pod gets temporary AWS credentials via OIDC
# 3. Importer downloads from S3 using those credentials
# 4. Image is converted and stored in a PVC (70Gi)
# 5. DataSource references this PVC for VM cloning
#
# Expected Duration: 5-10 minutes (depends on S3 transfer speed)
#
# Monitoring:
#   oc get datavolume windows -n openshift-virtualization-os-images -w
#   oc get pods -n openshift-virtualization-os-images
#
apiVersion: cdi.kubevirt.io/v1beta1
kind: DataVolume
metadata:
  name: windows
  namespace: openshift-virtualization-os-images
spec:
  contentType: kubevirt
  source:
    s3:
      # S3 URL format: s3://bucket-name/path/to/object
      url: "s3://ocpctl-binaries/windows-images/windows-10-oadp.qcow2"
      # Use IRSA instead of secretRef - credentials come from ServiceAccount
      # No secretRef needed!
  storage:
    resources:
      requests:
        # Must be at least as large as the QCOW2 image (23GB actual, 70GB allocated)
        storage: 70Gi
    # Use ODF/OCS storage class optimized for virtualization workloads
    storageClassName: ocs-storagecluster-ceph-rbd-virtualization
  # IMPORTANT: Use the ServiceAccount with IAM role annotation
  # This tells CDI to use IRSA for S3 access
  pvc:
    serviceAccountName: ${SERVICE_ACCOUNT_NAME}
EOF

echo "✓ Created: 2_windows-datavolume-irsa.yaml"

echo ""
echo "═══════════════════════════════════════════════════════════════"
echo "✅ IRSA Setup Complete!"
echo "═══════════════════════════════════════════════════════════════"
echo ""
echo "IAM Role ARN: $ROLE_ARN"
echo "ServiceAccount: $SERVICE_ACCOUNT_NAMESPACE/$SERVICE_ACCOUNT_NAME"
echo ""
echo "Next steps:"
echo "1. Apply the ServiceAccount to your cluster:"
echo "   oc create namespace openshift-virtualization-os-images"
echo "   oc apply -f 1a_windows-image-serviceaccount.yaml"
echo ""
echo "2. Use the IRSA-enabled DataVolume instead of the credential-based one:"
echo "   oc apply -f 2_windows-datavolume-irsa.yaml"
echo ""
echo "3. Continue with DataSource and Template:"
echo "   oc apply -f 3_datasource-windows.yaml"
echo "   oc apply -f 4_windows10-template.yaml"
echo ""
echo "Security benefits:"
echo "✓ No static credentials stored in cluster"
echo "✓ Temporary credentials that auto-rotate"
echo "✓ Credentials never stored in etcd"
echo "✓ Fine-grained IAM permissions"
echo "═══════════════════════════════════════════════════════════════"
