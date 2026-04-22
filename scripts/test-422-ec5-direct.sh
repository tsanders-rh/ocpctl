#!/bin/bash
# Test 4.22.0-ec.5 RHEL9 FIPS installer directly (no ocpctl)
# Uses Manual mode (OIDC/STS) - required when using EC2 instance profile credentials

set -euo pipefail

# Configuration
CLUSTER_NAME="${1:-test-422-ec5-direct}"
REGION="${REGION:-us-west-2}"
BASE_DOMAIN="${BASE_DOMAIN:-mg.dog8code.com}"
TEST_DIR="/tmp/test-${CLUSTER_NAME}-$(date +%s)"

# Required environment variables
if [ -z "${OPENSHIFT_PULL_SECRET:-}" ]; then
    echo "ERROR: OPENSHIFT_PULL_SECRET environment variable not set"
    echo "Export it first: export OPENSHIFT_PULL_SECRET='<your-pull-secret-json>'"
    exit 1
fi

# SSH key (optional)
SSH_KEY=$(cat ~/.ssh/id_rsa.pub 2>/dev/null || echo "")

echo "============================================"
echo "4.22.0-ec.5 RHEL9 FIPS Direct Test"
echo "============================================"
echo "Cluster Name: $CLUSTER_NAME"
echo "Region: $REGION"
echo "Base Domain: $BASE_DOMAIN"
echo "Test Directory: $TEST_DIR"
echo "Mode: Manual (OIDC/STS - required for 4.22.0-ec.5 with EC2 instance profile)"
echo "============================================"
echo ""

# Create test directory
mkdir -p "$TEST_DIR"
cd "$TEST_DIR"

# Create install-config.yaml (Manual mode required for 4.22.0-ec.5 with EC2 instance profile)
echo "Creating install-config.yaml..."
cat > install-config.yaml <<EOF
apiVersion: v1
baseDomain: $BASE_DOMAIN
credentialsMode: Manual
metadata:
  name: $CLUSTER_NAME
publish: External
platform:
  aws:
    region: $REGION
controlPlane:
  name: master
  replicas: 1
  platform:
    aws:
      type: m6i.2xlarge
compute:
- name: worker
  replicas: 0
  platform:
    aws:
      type: m6i.2xlarge
networking:
  networkType: OVNKubernetes
  clusterNetwork:
  - cidr: 10.128.0.0/14
    hostPrefix: 23
  serviceNetwork:
  - 172.30.0.0/16
  machineNetwork:
  - cidr: 10.0.0.0/16
pullSecret: '$OPENSHIFT_PULL_SECRET'
sshKey: '$SSH_KEY'
EOF

echo "✓ Created install-config.yaml"
echo ""

# Backup install-config
cp install-config.yaml install-config.yaml.bak

# Show binary version
echo "Installer binary:"
/usr/local/bin/openshift-install-4.22 version
echo ""

# Create manifests first (required for Manual mode)
echo "Creating manifests for Manual mode..."
/usr/local/bin/openshift-install-4.22 create manifests --dir "$TEST_DIR"
echo ""

# Extract infrastructure name from manifests
INFRA_NAME=$(grep -oP 'infrastructureName: \K.*' "$TEST_DIR/manifests/cluster-infrastructure-02-config.yml" || echo "$CLUSTER_NAME-$(openssl rand -hex 2)")
echo "Infrastructure name: $INFRA_NAME"
echo ""

# Extract CredentialsRequests from release image
echo "Extracting CredentialsRequests from release image..."
RELEASE_IMAGE="quay.io/openshift-release-dev/ocp-release@sha256:354270425f0cb661d5723910eb9d5ab7bd9510cdff43919c32695849bf0599f4"
CREDS_DIR="$TEST_DIR/credentialsrequests"
mkdir -p "$CREDS_DIR"

# Create temporary pull secret file for oc
PULL_SECRET_FILE="$TEST_DIR/.pull-secret.json"
echo "$OPENSHIFT_PULL_SECRET" > "$PULL_SECRET_FILE"

oc adm release extract \
  --credentials-requests \
  --cloud=aws \
  --to="$CREDS_DIR" \
  --from="$RELEASE_IMAGE" \
  --registry-config="$PULL_SECRET_FILE"

rm -f "$PULL_SECRET_FILE"
echo "Extracted $(ls -1 $CREDS_DIR | wc -l) CredentialsRequest manifests"
echo ""

# Show what credential requests were extracted
echo "CredentialsRequest files:"
ls -la "$CREDS_DIR"
echo ""

# Create OIDC provider and IAM resources using ccoctl (RHEL9 version)
echo "Creating OIDC provider and IAM roles with ccoctl (RHEL9)..."
OIDC_BUCKET="${CLUSTER_NAME}-oidc"
/usr/local/bin/ccoctl-4.22-rhel9 aws create-all \
  --name="$INFRA_NAME" \
  --region="$REGION" \
  --credentials-requests-dir="$CREDS_DIR" \
  --output-dir="$TEST_DIR"
echo ""

# Run openshift-install create cluster directly
echo "Starting cluster creation..."
echo "This will take 30-45 minutes."
echo ""
echo "Monitor progress in another terminal:"
echo "  tail -f $TEST_DIR/.openshift_install.log"
echo ""
read -p "Proceed with cluster creation? (y/n) " -n 1 -r
echo
if [[ ! $REPLY =~ ^[Yy]$ ]]; then
    echo "Aborted. Install config saved at: $TEST_DIR/install-config.yaml.bak"
    exit 0
fi

echo ""
echo "Running: /usr/local/bin/openshift-install-4.22 create cluster --dir $TEST_DIR --log-level=debug"
echo ""

/usr/local/bin/openshift-install-4.22 create cluster --dir "$TEST_DIR" --log-level=debug

# Check results
if [ $? -eq 0 ]; then
    echo ""
    echo "============================================"
    echo "✓ CLUSTER CREATION SUCCEEDED!"
    echo "============================================"
    echo ""
    echo "Cluster: $CLUSTER_NAME"
    echo "API: https://api.$CLUSTER_NAME.$BASE_DOMAIN:6443"
    echo "Console: https://console-openshift-console.apps.$CLUSTER_NAME.$BASE_DOMAIN"
    echo ""
    echo "Kubeconfig: $TEST_DIR/auth/kubeconfig"
    echo "Kubeadmin password: $TEST_DIR/auth/kubeadmin-password"
    echo ""
    echo "Test cluster with:"
    echo "  export KUBECONFIG=$TEST_DIR/auth/kubeconfig"
    echo "  oc whoami"
    echo ""
    echo "Destroy cluster when done:"
    echo "  cd $TEST_DIR"
    echo "  /usr/local/bin/openshift-install-4.22 destroy cluster --dir ."
    echo ""
else
    echo ""
    echo "============================================"
    echo "✗ CLUSTER CREATION FAILED"
    echo "============================================"
    echo ""
    echo "Logs: $TEST_DIR/.openshift_install.log"
    echo ""
    echo "To destroy and clean up:"
    echo "  cd $TEST_DIR"
    echo "  /usr/local/bin/openshift-install-4.22 destroy cluster --dir ."
    echo ""
    exit 1
fi
