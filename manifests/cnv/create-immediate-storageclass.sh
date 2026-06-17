#!/bin/bash
# create-immediate-storageclass.sh
# Creates an Immediate-mode storage class for OpenShift Virtualization
# Addresses GitHub issue #50: ensure storage classes have usable defaults for OpenShift-Virt

set -euo pipefail

# Get cluster region from node labels
REGION=$(oc get nodes -o jsonpath='{.items[0].metadata.labels.topology\.kubernetes\.io/region}')
if [ -z "$REGION" ]; then
    echo "ERROR: Could not determine cluster region from node labels"
    exit 1
fi

# Get first availability zone from nodes
ZONE=$(oc get nodes -o jsonpath='{.items[0].metadata.labels.topology\.kubernetes\.io/zone}' | sed "s/${REGION}//")
if [ -z "$ZONE" ]; then
    echo "WARNING: Could not determine zone, defaulting to 'a'"
    ZONE="a"
fi

STORAGE_CLASS_NAME="gp3-csi-immediate-${REGION}${ZONE}"
FULL_ZONE="${REGION}${ZONE}"

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
    storageclass.kubernetes.io/is-default-class: "true"
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
echo "  - Zone-specific topology constraints"
echo "  - Volume expansion support for CDI operations"
echo "  - Default for both Kubernetes and KubeVirt workloads"
