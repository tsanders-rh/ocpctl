#!/bin/bash
# Test CNI health checks against a real cluster without triggering remediation
# Usage: ./test-cni-health-check.sh /path/to/kubeconfig

set -e

KUBECONFIG_PATH="${1:-$HOME/Downloads/kubeconfig-sandy-virt-ga-2.yaml}"

if [ ! -f "$KUBECONFIG_PATH" ]; then
    echo "Error: kubeconfig not found at $KUBECONFIG_PATH"
    echo "Usage: $0 /path/to/kubeconfig"
    exit 1
fi

echo "=== Testing CNI Health Checks ==="
echo "Kubeconfig: $KUBECONFIG_PATH"
echo ""

# Check multus pods
echo "1. Checking multus pods..."
kubectl --kubeconfig "$KUBECONFIG_PATH" get pods -n openshift-multus -l app=multus \
    -o jsonpath='{range .items[*]}{.metadata.name},{.status.phase},{.status.containerStatuses[*].ready}|{end}' || true
echo ""

# Parse and evaluate
MULTUS_OUTPUT=$(kubectl --kubeconfig "$KUBECONFIG_PATH" get pods -n openshift-multus -l app=multus \
    -o jsonpath='{range .items[*]}{.metadata.name},{.status.phase},{.status.containerStatuses[*].ready}|{end}' 2>/dev/null || echo "")

echo "Multus raw output: $MULTUS_OUTPUT"
echo ""

# Check OVN pods
echo "2. Checking OVN-Kubernetes pods..."
kubectl --kubeconfig "$KUBECONFIG_PATH" get pods -n openshift-ovn-kubernetes -l app=ovnkube-node \
    -o jsonpath='{range .items[*]}{.metadata.name},{.status.phase},{.status.containerStatuses[*].ready}|{end}' || true
echo ""

OVN_OUTPUT=$(kubectl --kubeconfig "$KUBECONFIG_PATH" get pods -n openshift-ovn-kubernetes -l app=ovnkube-node \
    -o jsonpath='{range .items[*]}{.metadata.name},{.status.phase},{.status.containerStatuses[*].ready}|{end}' 2>/dev/null || echo "")

echo "OVN raw output: $OVN_OUTPUT"
echo ""

# Analyze results
echo "=== Analysis ==="

# Count pods
MULTUS_COUNT=$(kubectl --kubeconfig "$KUBECONFIG_PATH" get pods -n openshift-multus -l app=multus --no-headers 2>/dev/null | wc -l)
OVN_COUNT=$(kubectl --kubeconfig "$KUBECONFIG_PATH" get pods -n openshift-ovn-kubernetes -l app=ovnkube-node --no-headers 2>/dev/null | wc -l)

echo "Found $MULTUS_COUNT multus pods"
echo "Found $OVN_COUNT OVN pods"
echo ""

# Check for issues
MULTUS_ISSUES=$(kubectl --kubeconfig "$KUBECONFIG_PATH" get pods -n openshift-multus -l app=multus \
    --field-selector=status.phase!=Running 2>/dev/null | grep -v NAME || echo "")
OVN_ISSUES=$(kubectl --kubeconfig "$KUBECONFIG_PATH" get pods -n openshift-ovn-kubernetes -l app=ovnkube-node \
    --field-selector=status.phase!=Running 2>/dev/null | grep -v NAME || echo "")

if [ -z "$MULTUS_ISSUES" ]; then
    echo "✓ All multus pods are Running"
else
    echo "⚠ Multus issues found:"
    echo "$MULTUS_ISSUES"
fi

if [ -z "$OVN_ISSUES" ]; then
    echo "✓ All OVN pods are Running"
else
    echo "⚠ OVN issues found:"
    echo "$OVN_ISSUES"
fi

echo ""
echo "=== Test Complete ==="
echo "This simulates what the CNI health check will see during resume."
echo "If all pods show 'Running' and 'true' for ready status, auto-recovery won't trigger."
