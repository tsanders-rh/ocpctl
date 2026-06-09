#!/bin/bash
#
# Get cluster info and configure S3 access for Windows image import
#
# Detects whether the cluster supports IRSA (STS mode with OIDC) and falls
# back to static credentials from ~/.aws/credentials when it does not.
#
# Usage:
#   ./get-cluster-info.sh
#
# Prerequisites:
# - oc CLI must be installed and logged into the cluster
# - For static credentials fallback: ~/.aws/credentials must exist
#

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SERVICE_ACCOUNT_NAME="windows-image-importer"
SERVICE_ACCOUNT_NAMESPACE="openshift-virtualization-os-images"

echo "Getting cluster information..."
echo ""

# Get infraID from cluster
INFRA_ID=$(oc get infrastructure cluster -o jsonpath='{.status.infrastructureName}')
if [ -z "$INFRA_ID" ]; then
  echo "ERROR: Could not get infraID from cluster"
  echo "Make sure you're logged in: oc login <cluster-url>"
  exit 1
fi

# Get region from cluster
REGION=$(oc get infrastructure cluster -o jsonpath='{.status.platformStatus.aws.region}')
if [ -z "$REGION" ]; then
  echo "ERROR: Could not get region from cluster"
  echo "Is this an AWS cluster?"
  exit 1
fi

# Get cluster name
CLUSTER_NAME=$(oc get infrastructure cluster -o jsonpath='{.status.apiServerURL}' | sed 's|https://api\.||; s|:.*||; s|\..*||')

# Get OIDC provider info
OIDC_ISSUER=$(oc get authentication.config.openshift.io cluster -o jsonpath='{.spec.serviceAccountIssuer}')

# Detect availability zone from default storage class
DEFAULT_SC=$(oc get storageclass -o json | jq -r '.items[] | select(.metadata.annotations."storageclass.kubernetes.io/is-default-class" == "true") | .metadata.name' | head -1)
DETECTED_AZ=""
if [[ -n "$DEFAULT_SC" ]]; then
  # Try to extract AZ from storage class name (common pattern: *-us-east-1b)
  if [[ "$DEFAULT_SC" =~ -([a-z]+-[a-z]+-[0-9]+[a-z])$ ]]; then
    DETECTED_AZ="${BASH_REMATCH[1]}"
  fi
fi

echo "Cluster Information:"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "Cluster Name: $CLUSTER_NAME"
echo "Infrastructure ID: $INFRA_ID"
echo "Region: $REGION"
echo "Availability Zone: ${DETECTED_AZ:-<will be detected from PVC>}"
echo "Default StorageClass: ${DEFAULT_SC:-<none>}"
echo "OIDC Issuer: ${OIDC_ISSUER:-<not configured>}"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo ""

if [ -n "$OIDC_ISSUER" ]; then
  echo "✓ Cluster has STS mode enabled (OIDC issuer found)"
  echo ""
  echo "To setup IRSA, run:"
  echo "  ./setup-irsa.sh $INFRA_ID $REGION"
  echo ""
  exit 0
fi

# --- Static credentials fallback ---

echo "⚠ Cluster does NOT have STS mode enabled (no OIDC issuer)."
echo "  IRSA is not available. Falling back to static AWS credentials."
echo ""

AWS_CREDS_FILE="${AWS_SHARED_CREDENTIALS_FILE:-$HOME/.aws/credentials}"

if [ ! -f "$AWS_CREDS_FILE" ]; then
  echo "ERROR: AWS credentials file not found at $AWS_CREDS_FILE"
  echo ""
  echo "Create it with 'aws configure' or manually:"
  echo "  mkdir -p ~/.aws"
  echo "  cat > ~/.aws/credentials <<EOF"
  echo "  [default]"
  echo "  aws_access_key_id = YOUR_KEY"
  echo "  aws_secret_access_key = YOUR_SECRET"
  echo "  EOF"
  exit 1
fi

AWS_PROFILE="${AWS_PROFILE:-default}"

# Parse credentials from the AWS credentials file
ACCESS_KEY=$(aws configure get aws_access_key_id --profile "$AWS_PROFILE" 2>/dev/null || true)
SECRET_KEY=$(aws configure get aws_secret_access_key --profile "$AWS_PROFILE" 2>/dev/null || true)

if [ -z "$ACCESS_KEY" ] || [ -z "$SECRET_KEY" ]; then
  echo "ERROR: Could not read credentials from profile '$AWS_PROFILE' in $AWS_CREDS_FILE"
  exit 1
fi

echo "✓ Found AWS credentials (profile: $AWS_PROFILE)"
echo "  Access Key: ${ACCESS_KEY:0:4}...${ACCESS_KEY: -4}"
echo ""

# Ensure namespace exists
oc create namespace "$SERVICE_ACCOUNT_NAMESPACE" --dry-run=client -o yaml | oc apply -f - 2>/dev/null
echo "✓ Namespace: $SERVICE_ACCOUNT_NAMESPACE"

# Create the secret directly in the cluster — never written to disk
oc create secret generic s3-credentials \
  --namespace="$SERVICE_ACCOUNT_NAMESPACE" \
  --from-literal=accessKeyId="$ACCESS_KEY" \
  --from-literal=secretKey="$SECRET_KEY" \
  --dry-run=client -o yaml | oc apply -f -
echo "✓ Secret created: s3-credentials (in-cluster only, not written to disk)"

# CDI DataVolumes are immutable — delete first if one already exists
if oc get datavolume windows -n "$SERVICE_ACCOUNT_NAMESPACE" &>/dev/null; then
  echo "  Existing DataVolume found, deleting (CDI does not allow spec updates)..."
  oc delete datavolume windows -n "$SERVICE_ACCOUNT_NAMESPACE" --wait=true
fi
oc apply -f "${SCRIPT_DIR}/2_windows-datavolume.yaml"
echo "✓ Applied: 2_windows-datavolume.yaml"

# Apply remaining manifests
oc apply -f "${SCRIPT_DIR}/3_datasource-windows.yaml"
echo "✓ Applied: 3_datasource-windows.yaml"

oc apply -f "${SCRIPT_DIR}/4_windows10-template.yaml"
echo "✓ Applied: 4_windows10-template.yaml"

echo ""
echo "═══════════════════════════════════════════════════════════════"
echo "  Static Credentials Setup Complete"
echo "═══════════════════════════════════════════════════════════════"
echo ""
echo "The s3-credentials Secret was created directly in the cluster"
echo "from ~/.aws/credentials. No secrets were written to disk."
echo ""
echo "Monitor the import:"
echo "  oc get datavolume windows -n ${SERVICE_ACCOUNT_NAMESPACE} -w"
echo "═══════════════════════════════════════════════════════════════"
