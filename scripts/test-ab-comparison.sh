#!/bin/bash
set -euo pipefail

# A/B Test: Raw openshift-install vs ocpctl
# Purpose: Identify what difference causes NAT gateway route reconciliation failures

REGION="${REGION:-us-west-2}"
VERSION="${VERSION:-4.22.0-ec.4}"
BASE_DOMAIN="${BASE_DOMAIN:-mg.dog8code.com}"
CLUSTER_NAME_A="raw-ab-${VERSION//\./-}"
CLUSTER_NAME_B="ocpctl-ab-${VERSION//\./-}"

# Paths
RAW_DIR="/tmp/ab-test-raw-$(date +%s)"
OCPCTL_DIR="/tmp/ab-test-ocpctl-$(date +%s)"
COMPARISON_DIR="/tmp/ab-comparison-$(date +%s)"

# Pull secret (required)
PULL_SECRET="${OPENSHIFT_PULL_SECRET}"
if [ -z "$PULL_SECRET" ]; then
    echo "ERROR: OPENSHIFT_PULL_SECRET environment variable not set"
    exit 1
fi

# SSH key (optional)
SSH_KEY=$(cat ~/.ssh/id_rsa.pub 2>/dev/null || echo "")

echo "================================"
echo "A/B Comparison Test"
echo "================================"
echo "Version: $VERSION"
echo "Region: $REGION"
echo "Base Domain: $BASE_DOMAIN"
echo "Cluster A (raw): $CLUSTER_NAME_A"
echo "Cluster B (ocpctl): $CLUSTER_NAME_B"
echo ""
echo "Raw install dir: $RAW_DIR"
echo "Ocpctl install dir: $OCPCTL_DIR"
echo "Comparison output: $COMPARISON_DIR"
echo "================================"
echo ""

mkdir -p "$RAW_DIR" "$OCPCTL_DIR" "$COMPARISON_DIR"

# ============================================================================
# PATH A: Raw openshift-install
# ============================================================================

echo "[A] Creating install-config.yaml for raw openshift-install..."

cat > "$RAW_DIR/install-config.yaml" <<EOF
apiVersion: v1
baseDomain: $BASE_DOMAIN
credentialsMode: Manual
metadata:
  name: $CLUSTER_NAME_A
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
pullSecret: '$PULL_SECRET'
sshKey: '$SSH_KEY'
EOF

echo "[A] Backing up install-config.yaml..."
cp "$RAW_DIR/install-config.yaml" "$COMPARISON_DIR/install-config-raw.yaml"

echo "[A] Running: openshift-install-4.22 create manifests..."
/usr/local/bin/openshift-install-4.22 create manifests --dir "$RAW_DIR" --log-level=debug 2>&1 | tee "$COMPARISON_DIR/raw-create-manifests.log"

echo "[A] Saving manifests..."
cp -r "$RAW_DIR/manifests" "$COMPARISON_DIR/manifests-raw" 2>/dev/null || true
cp -r "$RAW_DIR/openshift" "$COMPARISON_DIR/openshift-raw" 2>/dev/null || true

echo "[A] Running: ccoctl aws create-all..."
INFRA_ID=$(jq -r '.infraID' "$RAW_DIR/.openshift_install_state.json")
echo "[A] InfraID: $INFRA_ID"

/usr/local/bin/ccoctl-4.22 aws create-all \
    --name="$INFRA_ID" \
    --region="$REGION" \
    --credentials-requests-dir="$RAW_DIR/manifests" \
    --output-dir="$RAW_DIR/cco-output" \
    2>&1 | tee "$COMPARISON_DIR/raw-ccoctl.log"

echo "[A] Copying CCO manifests..."
cp -r "$RAW_DIR/cco-output/manifests/"* "$RAW_DIR/manifests/" 2>/dev/null || true

echo "[A] Saving cluster-api manifests..."
cp -r "$RAW_DIR/openshift/99_openshift-cluster-api_*" "$COMPARISON_DIR/cluster-api-raw/" 2>/dev/null || true

echo "[A] Running: openshift-install-4.22 create cluster..."
echo "[A] NOTE: This will take 30-45 minutes. Watch for NAT gateway errors around 2-3 minutes in."
echo "[A] Press Ctrl+C to cancel if you just want to compare manifests."
echo ""
read -p "[A] Continue with cluster creation? (y/n) " -n 1 -r
echo
if [[ ! $REPLY =~ ^[Yy]$ ]]; then
    echo "[A] Skipping cluster creation. Manifests saved to $COMPARISON_DIR"
else
    /usr/local/bin/openshift-install-4.22 create cluster --dir "$RAW_DIR" --log-level=debug 2>&1 | tee "$COMPARISON_DIR/raw-create-cluster.log" || true
fi

# Save state
cp "$RAW_DIR/.openshift_install_state.json" "$COMPARISON_DIR/state-raw.json" 2>/dev/null || true
cp "$RAW_DIR/.openshift_install.log" "$COMPARISON_DIR/install-raw.log" 2>/dev/null || true

# ============================================================================
# PATH B: ocpctl
# ============================================================================

echo ""
echo "[B] Creating cluster via ocpctl..."
echo "[B] This will use the same version, region, and instance types as Path A"
echo ""

# Create cluster via ocpctl API (simulating what happens)
# We'll do this manually to capture the same artifacts

echo "[B] Generating install-config.yaml via ocpctl renderer..."

# This would normally be done by ocpctl's profile renderer
# For now, create it manually with ocpctl's typical tags

cat > "$OCPCTL_DIR/install-config.yaml" <<EOF
apiVersion: v1
baseDomain: $BASE_DOMAIN
credentialsMode: Manual
metadata:
  name: $CLUSTER_NAME_B
publish: External
platform:
  aws:
    region: $REGION
    userTags:
      Owner: admin@example.com
      Team: Migration Feature Team
      CostCenter: "733"
      ManagedBy: ocpctl
      Platform: aws
      Profile: aws-sno-test
      Purpose: testing
      Environment: test
      ClusterName: $CLUSTER_NAME_B
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
pullSecret: '$PULL_SECRET'
sshKey: '$SSH_KEY'
EOF

echo "[B] Backing up install-config.yaml..."
cp "$OCPCTL_DIR/install-config.yaml" "$COMPARISON_DIR/install-config-ocpctl.yaml"

echo "[B] Running: openshift-install-4.22 create manifests (with OPENSHIFT_INSTALL_INVOKER=ocpctl)..."
OPENSHIFT_INSTALL_INVOKER=ocpctl /usr/local/bin/openshift-install-4.22 create manifests --dir "$OCPCTL_DIR" --log-level=debug 2>&1 | tee "$COMPARISON_DIR/ocpctl-create-manifests.log"

echo "[B] Saving manifests..."
cp -r "$OCPCTL_DIR/manifests" "$COMPARISON_DIR/manifests-ocpctl" 2>/dev/null || true
cp -r "$OCPCTL_DIR/openshift" "$COMPARISON_DIR/openshift-ocpctl" 2>/dev/null || true

echo "[B] Running: ccoctl aws create-all..."
INFRA_ID_B=$(jq -r '.infraID' "$OCPCTL_DIR/.openshift_install_state.json")
echo "[B] InfraID: $INFRA_ID_B"

/usr/local/bin/ccoctl-4.22 aws create-all \
    --name="$INFRA_ID_B" \
    --region="$REGION" \
    --credentials-requests-dir="$OCPCTL_DIR/manifests" \
    --output-dir="$OCPCTL_DIR/cco-output" \
    2>&1 | tee "$COMPARISON_DIR/ocpctl-ccoctl.log"

echo "[B] Copying CCO manifests..."
cp -r "$OCPCTL_DIR/cco-output/manifests/"* "$OCPCTL_DIR/manifests/" 2>/dev/null || true

echo "[B] Saving cluster-api manifests..."
mkdir -p "$COMPARISON_DIR/cluster-api-ocpctl"
cp -r "$OCPCTL_DIR/openshift/99_openshift-cluster-api_"* "$COMPARISON_DIR/cluster-api-ocpctl/" 2>/dev/null || true

echo "[B] Running: openshift-install-4.22 create cluster (with OPENSHIFT_INSTALL_INVOKER=ocpctl)..."
echo "[B] NOTE: This will take 30-45 minutes. Watch for NAT gateway errors around 2-3 minutes in."
echo "[B] Press Ctrl+C to cancel if you just want to compare manifests."
echo ""
read -p "[B] Continue with cluster creation? (y/n) " -n 1 -r
echo
if [[ ! $REPLY =~ ^[Yy]$ ]]; then
    echo "[B] Skipping cluster creation. Manifests saved to $COMPARISON_DIR"
else
    OPENSHIFT_INSTALL_INVOKER=ocpctl /usr/local/bin/openshift-install-4.22 create cluster --dir "$OCPCTL_DIR" --log-level=debug 2>&1 | tee "$COMPARISON_DIR/ocpctl-create-cluster.log" || true
fi

# Save state
cp "$OCPCTL_DIR/.openshift_install_state.json" "$COMPARISON_DIR/state-ocpctl.json" 2>/dev/null || true
cp "$OCPCTL_DIR/.openshift_install.log" "$COMPARISON_DIR/install-ocpctl.log" 2>/dev/null || true

# ============================================================================
# COMPARISON
# ============================================================================

echo ""
echo "================================"
echo "Generating Comparison"
echo "================================"

# Diff install-config.yaml
echo "Diffing install-config.yaml..."
diff -u "$COMPARISON_DIR/install-config-raw.yaml" "$COMPARISON_DIR/install-config-ocpctl.yaml" > "$COMPARISON_DIR/diff-install-config.txt" || true

# Diff manifests
echo "Diffing manifests/..."
diff -ruN "$COMPARISON_DIR/manifests-raw" "$COMPARISON_DIR/manifests-ocpctl" > "$COMPARISON_DIR/diff-manifests.txt" || true

# Diff openshift
echo "Diffing openshift/..."
diff -ruN "$COMPARISON_DIR/openshift-raw" "$COMPARISON_DIR/openshift-ocpctl" > "$COMPARISON_DIR/diff-openshift.txt" || true

# Diff cluster-api
echo "Diffing cluster-api manifests..."
diff -ruN "$COMPARISON_DIR/cluster-api-raw" "$COMPARISON_DIR/cluster-api-ocpctl" > "$COMPARISON_DIR/diff-cluster-api.txt" || true

echo ""
echo "================================"
echo "Comparison Complete"
echo "================================"
echo ""
echo "Results saved to: $COMPARISON_DIR"
echo ""
echo "Key files to review:"
echo "  - diff-install-config.txt    (install-config.yaml differences)"
echo "  - diff-cluster-api.txt       (Cluster API manifest differences)"
echo "  - diff-manifests.txt         (All manifest differences)"
echo "  - diff-openshift.txt         (OpenShift manifest differences)"
echo ""
echo "If clusters were created, check AWS state:"
echo "  aws ec2 describe-nat-gateways --region $REGION --filters Name=tag:kubernetes.io/cluster/$INFRA_ID,Values=owned"
echo "  aws ec2 describe-nat-gateways --region $REGION --filters Name=tag:kubernetes.io/cluster/$INFRA_ID_B,Values=owned"
echo ""
echo "To view summaries:"
echo "  cat $COMPARISON_DIR/diff-install-config.txt"
echo "  cat $COMPARISON_DIR/diff-cluster-api.txt | grep -A5 -B5 'subnet\|nat\|route'"
echo ""
