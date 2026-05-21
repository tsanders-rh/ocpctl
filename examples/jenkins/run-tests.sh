#!/bin/bash

# Example test script for OpenShift clusters provisioned via ocpctl
# This script demonstrates how to write custom tests that run against your cluster
#
# Environment variables provided by Jenkins pipeline:
#   KUBECONFIG           - Path to kubeconfig file
#   CLUSTER_NAME         - Cluster name
#   CLUSTER_ID           - Cluster UUID
#   CLUSTER_API_URL      - Kubernetes API URL
#   CLUSTER_CONSOLE_URL  - OpenShift web console URL
#   KUBEADMIN_PASSWORD   - Kubeadmin password

set -e  # Exit on first error
set -o pipefail  # Catch errors in pipes

echo "=========================================="
echo "  OCPCTL Custom Test Suite"
echo "=========================================="
echo ""
echo "Cluster Name:    ${CLUSTER_NAME}"
echo "Cluster ID:      ${CLUSTER_ID}"
echo "API URL:         ${CLUSTER_API_URL}"
echo "Console URL:     ${CLUSTER_CONSOLE_URL}"
echo "Kubeconfig:      ${KUBECONFIG}"
echo ""

# Verify oc client is available
if ! command -v oc &> /dev/null; then
    echo "ERROR: 'oc' command not found. Please install OpenShift CLI."
    exit 1
fi

# Verify kubeconfig is valid
if [ ! -f "${KUBECONFIG}" ]; then
    echo "ERROR: Kubeconfig not found at ${KUBECONFIG}"
    exit 1
fi

echo "✓ Prerequisites validated"
echo ""

# Test 1: Cluster version
echo "[Test 1/6] Verifying cluster version..."
CLUSTER_VERSION=$(oc get clusterversion version -o jsonpath='{.status.desired.version}')
echo "  Cluster version: ${CLUSTER_VERSION}"
echo "✓ Test 1 passed"
echo ""

# Test 2: Wait for cluster operators to be ready
echo "[Test 2/6] Waiting for critical cluster operators..."
CRITICAL_OPERATORS=(
    "authentication"
    "console"
    "ingress"
    "kube-apiserver"
    "openshift-apiserver"
)

for op in "${CRITICAL_OPERATORS[@]}"; do
    echo "  Waiting for operator: ${op}"
    if ! oc wait --for=condition=Available --timeout=10m clusteroperator/${op}; then
        echo "ERROR: Operator ${op} did not become available"
        oc get clusteroperator/${op} -o yaml
        exit 1
    fi
done
echo "✓ Test 2 passed - All critical operators available"
echo ""

# Test 3: Verify nodes are ready
echo "[Test 3/6] Verifying cluster nodes..."
NODE_COUNT=$(oc get nodes --no-headers | wc -l)
READY_COUNT=$(oc get nodes --no-headers | grep -c " Ready " || true)
echo "  Total nodes: ${NODE_COUNT}"
echo "  Ready nodes: ${READY_COUNT}"

if [ "${NODE_COUNT}" -ne "${READY_COUNT}" ]; then
    echo "ERROR: Not all nodes are ready"
    oc get nodes
    exit 1
fi
echo "✓ Test 3 passed - All nodes ready"
echo ""

# Test 4: Deploy test workload
echo "[Test 4/6] Deploying test workload..."
TEST_NAMESPACE="ocpctl-test-${RANDOM}"
oc new-project ${TEST_NAMESPACE}

# Deploy a simple nginx deployment
oc create deployment nginx-test --image=nginx:latest -n ${TEST_NAMESPACE}
oc scale deployment/nginx-test --replicas=3 -n ${TEST_NAMESPACE}

echo "  Waiting for deployment to be ready..."
if ! oc wait --for=condition=Available --timeout=5m deployment/nginx-test -n ${TEST_NAMESPACE}; then
    echo "ERROR: Deployment did not become available"
    oc get deployment/nginx-test -n ${TEST_NAMESPACE} -o yaml
    oc get pods -n ${TEST_NAMESPACE}
    exit 1
fi
echo "✓ Test 4 passed - Test workload deployed"
echo ""

# Test 5: Verify pod scheduling
echo "[Test 5/6] Verifying pod scheduling..."
RUNNING_PODS=$(oc get pods -n ${TEST_NAMESPACE} --field-selector=status.phase=Running --no-headers | wc -l)
echo "  Running pods: ${RUNNING_PODS}"

if [ "${RUNNING_PODS}" -ne 3 ]; then
    echo "ERROR: Expected 3 running pods, found ${RUNNING_PODS}"
    oc get pods -n ${TEST_NAMESPACE}
    oc describe pods -n ${TEST_NAMESPACE}
    exit 1
fi
echo "✓ Test 5 passed - All pods running"
echo ""

# Test 6: Service connectivity
echo "[Test 6/6] Testing service connectivity..."
oc expose deployment/nginx-test --port=80 -n ${TEST_NAMESPACE}

# Wait for service
sleep 5

SERVICE_IP=$(oc get service/nginx-test -n ${TEST_NAMESPACE} -o jsonpath='{.spec.clusterIP}')
echo "  Service IP: ${SERVICE_IP}"

# Test connectivity from within a pod
TEST_POD=$(oc get pods -n ${TEST_NAMESPACE} -l app=nginx-test -o jsonpath='{.items[0].metadata.name}')
if oc exec ${TEST_POD} -n ${TEST_NAMESPACE} -- curl -s --max-time 5 ${SERVICE_IP}:80 > /dev/null; then
    echo "✓ Test 6 passed - Service connectivity verified"
else
    echo "ERROR: Service connectivity test failed"
    oc get service -n ${TEST_NAMESPACE}
    exit 1
fi
echo ""

# Save test results
echo "Saving test results..."
mkdir -p test-results

cat > test-results/summary.txt <<EOF
OCPCTL Test Suite Results
=========================
Date:            $(date)
Cluster Name:    ${CLUSTER_NAME}
Cluster ID:      ${CLUSTER_ID}
Cluster Version: ${CLUSTER_VERSION}
Node Count:      ${NODE_COUNT}

Test Results:
  ✓ Cluster version verified
  ✓ All critical operators available
  ✓ All nodes ready
  ✓ Test workload deployed
  ✓ Pod scheduling verified
  ✓ Service connectivity verified

Status: PASSED
EOF

# Save cluster state
oc get nodes -o wide > test-results/nodes.txt
oc get clusteroperators > test-results/operators.txt
oc get all -n ${TEST_NAMESPACE} > test-results/workload.txt
oc get events -n ${TEST_NAMESPACE} --sort-by='.lastTimestamp' > test-results/events.txt

# Cleanup test resources
echo "Cleaning up test resources..."
oc delete project ${TEST_NAMESPACE} --wait=false

echo ""
echo "=========================================="
echo "  All Tests Passed! ✓"
echo "=========================================="
echo ""
echo "Test results saved to: test-results/"
echo ""

exit 0
