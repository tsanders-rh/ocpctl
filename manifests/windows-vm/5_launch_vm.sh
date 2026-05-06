#!/bin/bash
#
# Launch a Windows 10 VM from the windows10-oadp-vm template
#
# Usage:
#   ./5_launch_vm.sh [VM_NAME] [VM_NAMESPACE] [STORAGE_CLASS]
#
# Examples:
#   ./5_launch_vm.sh                              # auto-generated name, windows-oadp-test namespace
#   ./5_launch_vm.sh windows-vm1                  # named VM, windows-oadp-test namespace
#   ./5_launch_vm.sh windows-vm1 my-namespace     # named VM, custom namespace
#

set -euo pipefail

TEMPLATE_NAME="windows10-oadp-vm"
TEMPLATE_NAMESPACE="openshift-virtualization-os-images"

VM_NAME="${1:-}"
VM_NAMESPACE="${2:-windows-oadp-test}"
STORAGE_CLASS="${3:-gp3-csi-immediate-us-west-2c}"

# Pre-flight: must be logged into the cluster
if ! oc whoami &>/dev/null; then
  echo "ERROR: Not logged into an OpenShift cluster."
  echo "Run: oc login <cluster-url>"
  exit 1
fi

# Pre-flight: verify the template exists
if ! oc get template "$TEMPLATE_NAME" -n "$TEMPLATE_NAMESPACE" &>/dev/null; then
  echo "ERROR: Template '$TEMPLATE_NAME' not found in namespace '$TEMPLATE_NAMESPACE'."
  echo "Apply it first: oc apply -f 4_windows10-template.yaml"
  exit 1
fi

echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "  Launching Windows 10 VM"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "  Template:      $TEMPLATE_NAME"
echo "  Namespace:     $VM_NAMESPACE"
echo "  Storage Class: $STORAGE_CLASS"
if [ -n "$VM_NAME" ]; then
  echo "  VM Name:       $VM_NAME"
else
  echo "  VM Name:       (auto-generated)"
fi
echo ""

# Build the oc process command
PROCESS_ARGS=(
  "$TEMPLATE_NAME"
  -n "$TEMPLATE_NAMESPACE"
  -p "VM_NAMESPACE=$VM_NAMESPACE"
  -p "STORAGE_CLASS=$STORAGE_CLASS"
)
if [ -n "$VM_NAME" ]; then
  PROCESS_ARGS+=(-p "VM_NAME=$VM_NAME")
fi

# Process the template and apply
oc process "${PROCESS_ARGS[@]}" | oc apply -f -

# Get the actual VM name (in case it was auto-generated)
ACTUAL_VM=$(oc get vm -n "$VM_NAMESPACE" \
  -l vm.kubevirt.io/template="$TEMPLATE_NAME" \
  --sort-by=.metadata.creationTimestamp \
  -o jsonpath='{.items[-1].metadata.name}' 2>/dev/null || true)

echo ""
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "✅ VM created: $ACTUAL_VM"
echo "   Namespace:  $VM_NAMESPACE"
echo ""
echo "Monitor startup:"
echo "  oc get vm $ACTUAL_VM -n $VM_NAMESPACE -w"
echo ""
echo "Start the VM:"
echo "  virtctl start $ACTUAL_VM -n $VM_NAMESPACE"
echo ""
echo "Open a console:"
echo "  virtctl console $ACTUAL_VM -n $VM_NAMESPACE"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
