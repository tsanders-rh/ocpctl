#!/bin/bash
# create-immediate-storageclass.sh
# Creates an Immediate-mode storage class for OpenShift Virtualization
# Addresses GitHub issue #50: ensure storage classes have usable defaults for OpenShift-Virt

set -euo pipefail

# Get cluster region and zone from node labels
FULL_ZONE=$(oc get nodes -o jsonpath='{.items[0].metadata.labels.topology\.kubernetes\.io/zone}')
REGION=$(oc get nodes -o jsonpath='{.items[0].metadata.labels.topology\.kubernetes\.io/region}')

if [ -z "$REGION" ] || [ -z "$FULL_ZONE" ]; then
    echo "ERROR: Could not determine region or zone from node labels"
    exit 1
fi

# Extract zone suffix (e.g., "a" from "us-east-1a")
ZONE="${FULL_ZONE#$REGION}"

STORAGE_CLASS_NAME="gp3-csi-immediate-${FULL_ZONE}"

echo "Creating virtualization-optimized storage class: ${STORAGE_CLASS_NAME}"
echo "  Region: ${REGION}"
echo "  Zone: ${FULL_ZONE}"
echo "  Volume binding mode: Immediate (faster VM provisioning)"
echo "  Volume expansion: enabled (required for CDI)"

# Create storage class manifest
cat <<EOF | oc apply -f -
apiVersion: storage.k8s.io/v1
kind: StorageClass
metadata:
  name: ${STORAGE_CLASS_NAME}
  annotations:
    storageclass.kubevirt.io/is-default-class: "true"
provisioner: ebs.csi.aws.com
parameters:
  type: gp3
  encrypted: "true"
reclaimPolicy: Delete
volumeBindingMode: Immediate
allowVolumeExpansion: true
allowedTopologies:
- matchLabelExpressions:
  - key: topology.ebs.csi.aws.com/zone
    values:
    - ${FULL_ZONE}
EOF

echo "✓ Storage class ${STORAGE_CLASS_NAME} created successfully"
echo ""
echo "This storage class provides:"
echo "  - Immediate volume binding (no WaitForFirstConsumer delay)"
echo "  - Zone-specific topology constraints (${FULL_ZONE})"
echo "  - Volume expansion support for CDI operations"
echo "  - Default storage class for KubeVirt workloads"
echo ""
echo "NOTE: VMs using this storage class must schedule in ${FULL_ZONE}"
