#!/bin/bash
#
# Configure AWS EFS CSI Driver for OpenShift Cluster
# This script sets up shared file storage (RWX) capabilities
#
# Usage: ./configure-efs-storage.sh <cluster-name> <kubeconfig-path>
#
# NOTE: For STS-enabled clusters, the IAM role and credentials for the EFS CSI operator
# are automatically created during cluster provisioning via ccoctl. The credentials secret
# (aws-efs-cloud-credentials) should already exist in the openshift-cluster-csi-drivers namespace.
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

# Get cluster infrastructure details
echo "→ Getting cluster infrastructure details..."
INFRA_NAME=$(oc get infrastructure cluster -o jsonpath='{.status.infrastructureName}')
REGION=$(oc get infrastructure cluster -o jsonpath='{.status.platformStatus.aws.region}')

echo "  Cluster Name: $CLUSTER_NAME"
echo "  Infrastructure Name: $INFRA_NAME"
echo "  Region: $REGION"

# Get private subnets using infrastructure name (one per AZ)
echo "→ Detecting private subnets (one per AZ)..."
SUBNET_IDS=$(aws ec2 describe-subnets \
    --region $REGION \
    --filters "Name=tag:kubernetes.io/cluster/$INFRA_NAME,Values=owned" \
              "Name=tag:kubernetes.io/role/internal-elb,Values=1" \
    --query 'Subnets | sort_by(@, &AvailabilityZone)[].{id:SubnetId, az:AvailabilityZone}' \
    --output json | jq -r 'unique_by(.az) | .[].id')

if [ -z "$SUBNET_IDS" ]; then
    echo "ERROR: Could not find private subnets for infrastructure $INFRA_NAME"
    exit 1
fi

echo "  Subnets: $SUBNET_IDS"

# Get cluster worker node security group
echo "→ Getting cluster worker node security group..."
CLUSTER_SG=$(aws ec2 describe-security-groups \
    --region $REGION \
    --filters "Name=tag:kubernetes.io/cluster/$INFRA_NAME,Values=owned" \
              "Name=tag:Name,Values=$INFRA_NAME-node" \
    --query 'SecurityGroups[0].GroupId' \
    --output text)

# Fallback to any cluster-owned SG if worker-node SG not found
if [ -z "$CLUSTER_SG" ] || [ "$CLUSTER_SG" = "None" ]; then
    echo "  Worker node SG not found, using first cluster-owned SG"
    CLUSTER_SG=$(aws ec2 describe-security-groups \
        --region $REGION \
        --filters "Name=tag:kubernetes.io/cluster/$INFRA_NAME,Values=owned" \
        --query 'SecurityGroups[0].GroupId' \
        --output text)
fi

echo "  Security Group: $CLUSTER_SG"

# Create EFS security group
echo "→ Creating EFS security group..."
EFS_SG_NAME="$CLUSTER_NAME-efs-sg"
EFS_SG=$(aws ec2 create-security-group \
    --region $REGION \
    --group-name "$EFS_SG_NAME" \
    --description "Security group for EFS access from $CLUSTER_NAME" \
    --vpc-id $(aws ec2 describe-subnets --subnet-ids $(echo $SUBNET_IDS | awk '{print $1}') --query 'Subnets[0].VpcId' --output text --region $REGION) \
    --query 'GroupId' \
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
           "Key=kubernetes.io/cluster/$INFRA_NAME,Value=owned" \
           "Key=ClusterName,Value=$CLUSTER_NAME" \
           "Key=ManagedBy,Value=ocpctl"

# Create EFS file system (idempotent with creation-token)
echo "→ Creating EFS file system..."
CREATION_TOKEN="${INFRA_NAME}-efs"

# Use creation-token for idempotency - AWS will return existing FS if token matches
EFS_ID=$(aws efs create-file-system \
    --region $REGION \
    --creation-token "$CREATION_TOKEN" \
    --performance-mode generalPurpose \
    --throughput-mode bursting \
    --encrypted \
    --tags "Key=Name,Value=$CLUSTER_NAME-efs" \
           "Key=kubernetes.io/cluster/$INFRA_NAME,Value=owned" \
           "Key=ClusterName,Value=$CLUSTER_NAME" \
           "Key=ManagedBy,Value=ocpctl" \
    --query 'FileSystemId' \
    --output text 2>&1)

# Check if creation failed
if [ $? -ne 0 ]; then
    # Check if it failed due to duplicate token (filesystem already exists)
    if echo "$EFS_ID" | grep -q "FileSystemAlreadyExists"; then
        # Extract filesystem ID from error or query by creation token
        EFS_ID=$(aws efs describe-file-systems \
            --region $REGION \
            --creation-token "$CREATION_TOKEN" \
            --query 'FileSystems[0].FileSystemId' \
            --output text)
        echo "  Found existing EFS: $EFS_ID"
    else
        echo "ERROR: Failed to create EFS filesystem: $EFS_ID"
        exit 1
    fi
else
    echo "  Created EFS: $EFS_ID"
fi

# Wait for filesystem to become available (manual poll since aws efs wait doesn't exist)
echo "→ Waiting for EFS to become available..."
for i in {1..60}; do
    STATE=$(aws efs describe-file-systems \
        --region $REGION \
        --file-system-id $EFS_ID \
        --query 'FileSystems[0].LifeCycleState' \
        --output text 2>/dev/null)

    if [ "$STATE" = "available" ]; then
        echo "  EFS is available"
        break
    fi

    if [ $i -eq 1 ]; then
        echo -n "  Current state: $STATE, waiting"
    else
        echo -n "."
    fi
    sleep 5
done
echo ""

if [ "$STATE" != "available" ]; then
    echo "ERROR: EFS did not become available within 5 minutes (state: $STATE)"
    exit 1
fi

echo "  EFS ID: $EFS_ID"

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
spec: {}
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
# Wait for deployment to be created by OLM (can take 1-2 minutes)
for i in {1..60}; do
    if oc get deployment aws-efs-csi-driver-operator -n openshift-cluster-csi-drivers &>/dev/null; then
        echo "  Operator deployment created"
        break
    fi
    [ $i -eq 1 ] && echo -n "  Waiting for operator deployment to be created"
    echo -n "."
    sleep 5
done
echo ""

# Now wait for it to become available
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

# Wait for CSI driver to be ready (per Red Hat docs)
echo "→ Waiting for EFS CSI Driver to be ready..."
echo "  Waiting for AWSEFSDriverNodeServiceControllerAvailable..."
oc wait clustercsidriver/efs.csi.aws.com \
    --for=jsonpath='{.status.conditions[?(@.type=="AWSEFSDriverNodeServiceControllerAvailable")].status}'=True \
    --timeout=300s

echo "  Waiting for AWSEFSDriverControllerServiceControllerAvailable..."
oc wait clustercsidriver/efs.csi.aws.com \
    --for=jsonpath='{.status.conditions[?(@.type=="AWSEFSDriverControllerServiceControllerAvailable")].status}'=True \
    --timeout=300s

echo "  EFS CSI Driver is ready"

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
