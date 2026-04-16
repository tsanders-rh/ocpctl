#!/bin/bash
set -euo pipefail

# A/B Manifest Comparison Test
# Run this on the worker server where openshift-install binaries are installed

REGION="${REGION:-us-west-2}"
VERSION="4.22"
BASE_DOMAIN="mg.dog8code.com"
CLUSTER_NAME_A="raw-ab-test"
CLUSTER_NAME_B="ocpctl-ab-test"

RAW_DIR="/tmp/ab-raw-$(date +%s)"
OCPCTL_DIR="/tmp/ab-ocpctl-$(date +%s)"
COMP_DIR="/tmp/ab-comparison-$(date +%s)"

# Dummy pull secret (only for manifest generation)
PULL_SECRET='{"auths":{"fake.example.com":{"auth":"dXNlcjpwYXNz"}}}'

echo "================================"
echo "A/B Manifest Comparison"
echo "================================"
echo "Version: 4.22.0-ec.4"
echo "Region: $REGION"
echo "Raw dir: $RAW_DIR"
echo "Ocpctl dir: $OCPCTL_DIR"
echo "Output: $COMP_DIR"
echo "================================"
echo ""

mkdir -p "$RAW_DIR" "$OCPCTL_DIR" "$COMP_DIR"

# ============================================================================
# PATH A: Raw install-config (NO TAGS, NO INVOKER)
# ============================================================================

echo "[A] Creating RAW install-config.yaml (no tags, no invoker)..."
cat > "$RAW_DIR/install-config.yaml" <<'EOF'
apiVersion: v1
baseDomain: mg.dog8code.com
credentialsMode: Manual
metadata:
  name: raw-ab-test
publish: External
platform:
  aws:
    region: us-west-2
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
pullSecret: '{"auths":{"fake.example.com":{"auth":"dXNlcjpwYXNz"}}}'
EOF

cp "$RAW_DIR/install-config.yaml" "$COMP_DIR/install-config-raw.yaml"

echo "[A] Running: openshift-install create manifests (NO INVOKER ENV VAR)..."
/usr/local/bin/openshift-install-$VERSION create manifests --dir "$RAW_DIR" --log-level=info 2>&1 | tee "$COMP_DIR/raw-manifests.log"

echo "[A] Saving generated manifests..."
cp -r "$RAW_DIR/manifests" "$COMP_DIR/manifests-raw/" 2>/dev/null || true
cp -r "$RAW_DIR/openshift" "$COMP_DIR/openshift-raw/" 2>/dev/null || true
mkdir -p "$COMP_DIR/cluster-api-raw"
find "$RAW_DIR/openshift" -name "*cluster-api*" -exec cp {} "$COMP_DIR/cluster-api-raw/" \; 2>/dev/null || true

INFRA_A=$(jq -r '.infraID' "$RAW_DIR/.openshift_install_state.json" 2>/dev/null || echo "unknown")
echo "[A] InfraID: $INFRA_A"

# ============================================================================
# PATH B: Ocpctl-style install-config (WITH TAGS, WITH INVOKER)
# ============================================================================

echo ""
echo "[B] Creating OCPCTL install-config.yaml (WITH tags, WITH invoker)..."
cat > "$OCPCTL_DIR/install-config.yaml" <<'EOF'
apiVersion: v1
baseDomain: mg.dog8code.com
credentialsMode: Manual
metadata:
  name: ocpctl-ab-test
publish: External
platform:
  aws:
    region: us-west-2
    userTags:
      Owner: admin@example.com
      Team: Migration Feature Team
      CostCenter: "733"
      ManagedBy: ocpctl
      Platform: aws
      Profile: aws-sno-test
      Purpose: testing
      Environment: test
      ClusterName: ocpctl-ab-test
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
pullSecret: '{"auths":{"fake.example.com":{"auth":"dXNlcjpwYXNz"}}}'
EOF

cp "$OCPCTL_DIR/install-config.yaml" "$COMP_DIR/install-config-ocpctl.yaml"

echo "[B] Running: openshift-install create manifests (WITH OPENSHIFT_INSTALL_INVOKER=ocpctl)..."
OPENSHIFT_INSTALL_INVOKER=ocpctl /usr/local/bin/openshift-install-$VERSION create manifests --dir "$OCPCTL_DIR" --log-level=info 2>&1 | tee "$COMP_DIR/ocpctl-manifests.log"

echo "[B] Saving generated manifests..."
cp -r "$OCPCTL_DIR/manifests" "$COMP_DIR/manifests-ocpctl/" 2>/dev/null || true
cp -r "$OCPCTL_DIR/openshift" "$COMP_DIR/openshift-ocpctl/" 2>/dev/null || true
mkdir -p "$COMP_DIR/cluster-api-ocpctl"
find "$OCPCTL_DIR/openshift" -name "*cluster-api*" -exec cp {} "$COMP_DIR/cluster-api-ocpctl/" \; 2>/dev/null || true

INFRA_B=$(jq -r '.infraID' "$OCPCTL_DIR/.openshift_install_state.json" 2>/dev/null || echo "unknown")
echo "[B] InfraID: $INFRA_B"

# ============================================================================
# COMPARISON
# ============================================================================

echo ""
echo "================================"
echo "Generating Diffs"
echo "================================"

diff -u "$COMP_DIR/install-config-raw.yaml" "$COMP_DIR/install-config-ocpctl.yaml" > "$COMP_DIR/diff-install-config.txt" || true
diff -ruN "$COMP_DIR/manifests-raw" "$COMP_DIR/manifests-ocpctl" > "$COMP_DIR/diff-manifests.txt" || true
diff -ruN "$COMP_DIR/openshift-raw" "$COMP_DIR/openshift-ocpctl" > "$COMP_DIR/diff-openshift.txt" || true
diff -ruN "$COMP_DIR/cluster-api-raw" "$COMP_DIR/cluster-api-ocpctl" > "$COMP_DIR/diff-cluster-api.txt" || true

echo ""
echo "================================"
echo "RESULTS"
echo "================================"
echo ""

echo "--- 1. install-config.yaml Diff ---"
if [ -s "$COMP_DIR/diff-install-config.txt" ]; then
    echo "DIFFERENCES FOUND:"
    head -80 "$COMP_DIR/diff-install-config.txt"
else
    echo "NO DIFFERENCES (unexpected!)"
fi

echo ""
echo "--- 2. Cluster API Manifests Diff ---"
if [ -s "$COMP_DIR/diff-cluster-api.txt" ]; then
    echo "DIFFERENCES FOUND - Checking for networking-related changes:"
    grep -A8 -B3 -E 'subnet|Subnet|nat|NAT|route|Route|failureDomain|FailureDomain|availabilityZone|AvailabilityZone' "$COMP_DIR/diff-cluster-api.txt" | head -100 || echo "No networking differences found in diff"
else
    echo "NO DIFFERENCES in cluster-api manifests"
fi

echo ""
echo "--- 3. All Manifests Summary ---"
if [ -s "$COMP_DIR/diff-manifests.txt" ]; then
    DIFF_LINES=$(wc -l < "$COMP_DIR/diff-manifests.txt")
    echo "Total lines changed: $DIFF_LINES"
    echo ""
    echo "Files that differ:"
    grep -E '^(\+\+\+|---)' "$COMP_DIR/diff-manifests.txt" | head -20
else
    echo "NO DIFFERENCES in manifests/"
fi

echo ""
echo "--- 4. OpenShift Manifests Summary ---"
if [ -s "$COMP_DIR/diff-openshift.txt" ]; then
    DIFF_LINES=$(wc -l < "$COMP_DIR/diff-openshift.txt")
    echo "Total lines changed: $DIFF_LINES"
else
    echo "NO DIFFERENCES in openshift/"
fi

echo ""
echo "================================"
echo "Full Diff Files"
echo "================================"
echo "  $COMP_DIR/diff-install-config.txt"
echo "  $COMP_DIR/diff-cluster-api.txt"
echo "  $COMP_DIR/diff-manifests.txt"
echo "  $COMP_DIR/diff-openshift.txt"
echo ""
echo "Manifest directories:"
echo "  $COMP_DIR/manifests-raw/"
echo "  $COMP_DIR/manifests-ocpctl/"
echo "  $COMP_DIR/cluster-api-raw/"
echo "  $COMP_DIR/cluster-api-ocpctl/"
echo ""

echo "================================"
echo "Analysis"
echo "================================"
echo ""
echo "If cluster-api manifests differ in subnet/AZ/route configuration:"
echo "  → ocpctl is causing the NAT gateway issue"
echo ""
echo "If cluster-api manifests are identical:"
echo "  → Problem is elsewhere (upstream bug, timing, AWS region)"
echo ""
echo "To view full cluster-api diff:"
echo "  cat $COMP_DIR/diff-cluster-api.txt | less"
echo ""
