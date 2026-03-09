#!/bin/bash
#
# Configure AWS EFS CSI Driver for OpenShift Cluster
# This script sets up shared file storage (RWX) capabilities
#
# Usage: ./configure-efs-storage.sh <cluster-name> <kubeconfig-path>
#

set -e

CLUSTER_NAME="${1}"
KUBECONFIG_PATH="${2}"

if [ -z "$CLUSTER_NAME" ] || [ -z "$KUBECONFIG_PATH" ]; then
    echo "Usage: $0 <cluster-name> <kubeconfig-path>"
    echo "Example: $0 my-cluster /path/to/kubeconfig"
    exit 1
fi

export KUBECONFIG="$KUBECONFIG_PATH"

echo "========================================="
echo "Configuring EFS Storage for: $CLUSTER_NAME"
echo "========================================="

# Get cluster VPC and subnets
echo "→ Getting cluster infrastructure details..."
VPC_ID=$(oc get infrastructure cluster -o jsonpath='{.status.platformStatus.aws.resourceTags[?(@.key=="kubernetes.io/cluster/'$CLUSTER_NAME'")].key}' | sed 's/kubernetes.io\/cluster\///')
REGION=$(oc get infrastructure cluster -o jsonpath='{.status.platformStatus.aws.region}')

echo "  VPC ID: (will be detected)"
echo "  Region: $REGION"

# Get private subnets
echo "→ Detecting private subnets..."
SUBNET_IDS=$(aws ec2 describe-subnets \
    --region $REGION \
    --filters "Name=tag:kubernetes.io/cluster/$CLUSTER_NAME,Values=owned" \
              "Name=tag:kubernetes.io/role/internal-elb,Values=1" \
    --query 'Subnets[*].SubnetId' \
    --output text)

if [ -z "$SUBNET_IDS" ]; then
    echo "ERROR: Could not find private subnets for cluster $CLUSTER_NAME"
    exit 1
fi

echo "  Subnets: $SUBNET_IDS"

# Get cluster security group
echo "→ Getting cluster security group..."
CLUSTER_SG=$(aws ec2 describe-security-groups \
    --region $REGION \
    --filters "Name=tag:kubernetes.io/cluster/$CLUSTER_NAME,Values=owned" \
    --query 'SecurityGroups[0].GroupId' \
    --output text)

echo "  Security Group: $CLUSTER_SG"

# Create EFS security group
echo "→ Creating EFS security group..."
EFS_SG_NAME="$CLUSTER_NAME-efs-sg"
EFS_SG=$(aws ec2 create-security-group \
    --region $REGION \
    --group-name "$EFS_SG_NAME" \
    --description "Security group for EFS access from $CLUSTER_NAME" \
    --vpc-id $(aws ec2 describe-subnets --subnet-ids $(echo $SUBNET_IDS | awk '{print $1}') --query 'Subnets[0].VpcId' --output text --region $REGION) \
    --output text 2>/dev/null || \
    aws ec2 describe-security-groups --region $REGION --filters "Name=group-name,Values=$EFS_SG_NAME" --query 'SecurityGroups[0].GroupId' --output text)

# Allow NFS traffic from cluster
echo "→ Configuring security group rules..."
aws ec2 authorize-security-group-ingress \
    --region $REGION \
    --group-id $EFS_SG \
    --protocol tcp \
    --port 2049 \
    --source-group $CLUSTER_SG \
    2>/dev/null || echo "  Rule already exists"

# Tag security group
aws ec2 create-tags \
    --region $REGION \
    --resources $EFS_SG \
    --tags "Key=Name,Value=$EFS_SG_NAME" \
           "Key=kubernetes.io/cluster/$CLUSTER_NAME,Value=owned" \
           "Key=ManagedBy,Value=ocpctl"

# Create EFS file system
echo "→ Creating EFS file system..."
EFS_ID=$(aws efs create-file-system \
    --region $REGION \
    --performance-mode generalPurpose \
    --throughput-mode bursting \
    --encrypted \
    --tags "Key=Name,Value=$CLUSTER_NAME-efs" \
           "Key=kubernetes.io/cluster/$CLUSTER_NAME,Value=owned" \
           "Key=ManagedBy,Value=ocpctl" \
    --query 'FileSystemId' \
    --output text 2>/dev/null || \
    aws efs describe-file-systems --region $REGION --query "FileSystems[?Tags[?Key=='Name' && Value=='$CLUSTER_NAME-efs']].FileSystemId" --output text)

echo "  EFS ID: $EFS_ID"

# Wait for EFS to be available
echo "→ Waiting for EFS to become available..."
aws efs wait file-system-available --region $REGION --file-system-id $EFS_ID
echo "  EFS is available"

# Create mount targets in each subnet
echo "→ Creating EFS mount targets..."
for SUBNET in $SUBNET_IDS; do
    echo "  Creating mount target in subnet: $SUBNET"
    aws efs create-mount-target \
        --region $REGION \
        --file-system-id $EFS_ID \
        --subnet-id $SUBNET \
        --security-groups $EFS_SG \
        2>/dev/null || echo "    Mount target already exists"
done

# Install EFS CSI Driver Operator
echo "→ Installing AWS EFS CSI Driver Operator..."
oc apply -f - <<EOF
apiVersion: v1
kind: Namespace
metadata:
  name: openshift-cluster-csi-drivers
  labels:
    openshift.io/cluster-monitoring: "true"
---
apiVersion: operators.coreos.com/v1
kind: OperatorGroup
metadata:
  name: openshift-cluster-csi-drivers
  namespace: openshift-cluster-csi-drivers
spec:
  targetNamespaces:
  - openshift-cluster-csi-drivers
---
apiVersion: operators.coreos.com/v1alpha1
kind: Subscription
metadata:
  name: aws-efs-csi-driver-operator
  namespace: openshift-cluster-csi-drivers
spec:
  channel: stable
  name: aws-efs-csi-driver-operator
  source: redhat-operators
  sourceNamespace: openshift-marketplace
  installPlanApproval: Automatic
EOF

echo "→ Waiting for EFS CSI Driver Operator to be ready..."
sleep 30
oc wait --for=condition=Available --timeout=300s \
    -n openshift-cluster-csi-drivers \
    deployment/aws-efs-csi-driver-operator

# Create ClusterCSIDriver
echo "→ Creating ClusterCSIDriver instance..."
oc apply -f - <<EOF
apiVersion: operator.openshift.io/v1
kind: ClusterCSIDriver
metadata:
  name: efs.csi.aws.com
spec:
  managementState: Managed
  logLevel: Normal
  operatorLogLevel: Normal
EOF

# Wait for CSI driver to be ready
echo "→ Waiting for EFS CSI Driver to be ready..."
sleep 30
oc wait --for=condition=Available --timeout=300s \
    csidriver efs.csi.aws.com

# Create StorageClass
echo "→ Creating EFS StorageClass..."
oc apply -f - <<EOF
apiVersion: storage.k8s.io/v1
kind: StorageClass
metadata:
  name: efs-sc
  annotations:
    storageclass.kubernetes.io/description: "AWS EFS shared file storage (RWX)"
provisioner: efs.csi.aws.com
parameters:
  provisioningMode: efs-ap
  fileSystemId: ${EFS_ID}
  directoryPerms: "700"
  gidRangeStart: "1000"
  gidRangeEnd: "2000"
  basePath: "/dynamic"
reclaimPolicy: Delete
volumeBindingMode: Immediate
allowVolumeExpansion: true
EOF

echo ""
echo "========================================="
echo "✅ EFS Storage Configuration Complete!"
echo "========================================="
echo ""
echo "EFS File System ID: $EFS_ID"
echo "EFS Security Group: $EFS_SG"
echo "StorageClass: efs-sc"
echo ""

# Output JSON for programmatic parsing (OCPCTL_OUTPUT marker)
echo "OCPCTL_OUTPUT_START"
cat <<JSON_OUTPUT
{
  "efs_id": "$EFS_ID",
  "efs_security_group_id": "$EFS_SG",
  "region": "$REGION",
  "storage_class": "efs-sc"
}
JSON_OUTPUT
echo "OCPCTL_OUTPUT_END"
