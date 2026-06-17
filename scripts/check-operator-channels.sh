#!/bin/bash
# Check available channels for an operator in Red Hat Operators catalog

OPERATOR_NAME="${1:-}"
KUBECONFIG_PATH="${2:-$KUBECONFIG}"

if [ -z "$OPERATOR_NAME" ]; then
    echo "Usage: $0 <operator-name> [kubeconfig-path]"
    echo "Example: $0 redhat-oadp-operator ~/Downloads/kubeconfig.yaml"
    exit 1
fi

if [ -n "$KUBECONFIG_PATH" ]; then
    export KUBECONFIG="$KUBECONFIG_PATH"
fi

echo "=== Operator: $OPERATOR_NAME ==="
echo ""

# Check if packagemanifest exists
if ! oc get packagemanifest "$OPERATOR_NAME" -n openshift-marketplace &>/dev/null; then
    echo "ERROR: Operator '$OPERATOR_NAME' not found in catalog"
    echo ""
    echo "Available operators (first 10):"
    oc get packagemanifest -n openshift-marketplace -o jsonpath='{.items[*].metadata.name}' | tr ' ' '\n' | head -10
    exit 1
fi

echo "Available Channels:"
echo "-------------------"
oc get packagemanifest "$OPERATOR_NAME" -n openshift-marketplace -o json | \
    jq -r '.status.channels[] | "  - \(.name)\n    Version: \(.currentCSVDesc.version)\n    CSV: \(.currentCSV)\n"'

echo ""
echo "Default Channel:"
echo "----------------"
oc get packagemanifest "$OPERATOR_NAME" -n openshift-marketplace -o jsonpath='{.status.defaultChannel}'
echo ""
echo ""

echo "Full YAML (for reference):"
echo "--------------------------"
oc get packagemanifest "$OPERATOR_NAME" -n openshift-marketplace -o yaml | grep -A 50 "channels:"
