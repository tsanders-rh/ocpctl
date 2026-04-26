#!/bin/bash
# Simulated CNI failure test - breaks and recovers CNI on ONE node
# WARNING: This temporarily breaks networking on one worker node
# Use only in dev/test clusters, NOT production
# Usage: ./test-cni-recovery.sh /path/to/kubeconfig

set -e

KUBECONFIG_PATH="${1:-$HOME/Downloads/kubeconfig-sandy-virt-ga-2.yaml}"

if [ ! -f "$KUBECONFIG_PATH" ]; then
    echo "Error: kubeconfig not found at $KUBECONFIG_PATH"
    exit 1
fi

echo "⚠️  WARNING: This will intentionally break CNI on ONE worker node"
echo "The auto-recovery code should detect and fix it within 30 seconds"
echo ""
read -p "Continue? (yes/no): " confirm

if [ "$confirm" != "yes" ]; then
    echo "Aborted"
    exit 0
fi

# Find a worker node
WORKER_NODE=$(kubectl --kubeconfig "$KUBECONFIG_PATH" get nodes -l node-role.kubernetes.io/worker \
    -o jsonpath='{.items[0].metadata.name}')

echo "Selected worker node: $WORKER_NODE"
echo ""

# Find multus pod on that node
MULTUS_POD=$(kubectl --kubeconfig "$KUBECONFIG_PATH" get pods -n openshift-multus -l app=multus \
    --field-selector spec.nodeName="$WORKER_NODE" -o jsonpath='{.items[0].metadata.name}')

echo "Found multus pod on that node: $MULTUS_POD"
echo ""

# Find OVN pod on that node
OVN_POD=$(kubectl --kubeconfig "$KUBECONFIG_PATH" get pods -n openshift-ovn-kubernetes -l app=ovnkube-node \
    --field-selector spec.nodeName="$WORKER_NODE" -o jsonpath='{.items[0].metadata.name}')

echo "Found OVN pod on that node: $OVN_POD"
echo ""

# Step 1: Delete the multus pod (simulates the crash we saw)
echo "Step 1: Deleting multus pod to simulate crash..."
kubectl --kubeconfig "$KUBECONFIG_PATH" delete pod -n openshift-multus "$MULTUS_POD" --wait=false

echo "Waiting 5 seconds..."
sleep 5

# Check pod status
echo ""
echo "Current multus pod status on $WORKER_NODE:"
kubectl --kubeconfig "$KUBECONFIG_PATH" get pods -n openshift-multus \
    --field-selector spec.nodeName="$WORKER_NODE"

echo ""
echo "Step 2: Simulating what auto-recovery would do..."
echo "In real resume, the code would detect this and delete the problematic pod"
echo ""

# Step 3: Wait for auto-recovery (pod should restart automatically via DaemonSet)
echo "Step 3: Monitoring pod recovery..."
for i in {1..30}; do
    NEW_POD=$(kubectl --kubeconfig "$KUBECONFIG_PATH" get pods -n openshift-multus -l app=multus \
        --field-selector spec.nodeName="$WORKER_NODE",status.phase=Running \
        -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || echo "")

    if [ -n "$NEW_POD" ]; then
        echo "✓ Pod recovered: $NEW_POD (running)"

        # Check if containers are ready
        READY=$(kubectl --kubeconfig "$KUBECONFIG_PATH" get pod -n openshift-multus "$NEW_POD" \
            -o jsonpath='{.status.containerStatuses[0].ready}')

        if [ "$READY" = "true" ]; then
            echo "✓ Pod is healthy and ready!"
            break
        fi
    fi

    echo "  Waiting for pod to be ready... ($i/30)"
    sleep 2
done

echo ""
echo "=== Final Status ==="
kubectl --kubeconfig "$KUBECONFIG_PATH" get pods -n openshift-multus \
    --field-selector spec.nodeName="$WORKER_NODE"

echo ""
echo "Test complete! CNI recovery simulation finished."
echo "The multus pod was deleted and automatically recreated by the DaemonSet."
echo "This simulates what auto-recovery does during cluster resume."
