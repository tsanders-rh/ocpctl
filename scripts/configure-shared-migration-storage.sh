#!/bin/bash
#
# Configure Shared Storage for Application Migration Testing
# Creates a shared EFS filesystem accessible from multiple clusters
#
# Usage: ./configure-shared-migration-storage.sh <source-cluster> <target-cluster> <region>
#

set -e

SOURCE_CLUSTER="${1}"
TARGET_CLUSTER="${2}"
REGION="${3:-us-east-1}"

if [ -z "$SOURCE_CLUSTER" ] || [ -z "$TARGET_CLUSTER" ]; then
    echo "Usage: $0 <source-cluster> <target-cluster> [region]"
    echo "Example: $0 source-ocp target-ocp us-east-1"
    exit 1
fi

echo "========================================="
echo "Configuring Shared Migration Storage"
echo "========================================="
echo "Source Cluster: $SOURCE_CLUSTER"
echo "Target Cluster: $TARGET_CLUSTER"
echo "Region: $REGION"
echo ""

# Get VPC IDs for both clusters
echo "→ Getting cluster VPC information..."

# Try tag-based detection first (for standard IPI deployments)
SOURCE_VPC=$(aws ec2 describe-vpcs \
    --region $REGION \
    --filters "Name=tag:kubernetes.io/cluster/$SOURCE_CLUSTER,Values=owned" \
    --query 'Vpcs[0].VpcId' \
    --output text)

# If tag-based detection fails (BYOVPC), query via cluster instances
if [ "$SOURCE_VPC" == "None" ] || [ -z "$SOURCE_VPC" ]; then
    echo "  Source cluster using BYOVPC, detecting VPC from instances..."
    SOURCE_VPC=$(aws ec2 describe-instances \
        --region $REGION \
        --filters "Name=tag:Name,Values=${SOURCE_CLUSTER}*" "Name=instance-state-name,Values=running,stopped" \
        --query 'Reservations[0].Instances[0].VpcId' \
        --output text)
fi

# Try tag-based detection first (for standard IPI deployments)
TARGET_VPC=$(aws ec2 describe-vpcs \
    --region $REGION \
    --filters "Name=tag:kubernetes.io/cluster/$TARGET_CLUSTER,Values=owned" \
    --query 'Vpcs[0].VpcId' \
    --output text)

# If tag-based detection fails (BYOVPC), query via cluster instances
if [ "$TARGET_VPC" == "None" ] || [ -z "$TARGET_VPC" ]; then
    echo "  Target cluster using BYOVPC, detecting VPC from instances..."
    TARGET_VPC=$(aws ec2 describe-instances \
        --region $REGION \
        --filters "Name=tag:Name,Values=${TARGET_CLUSTER}*" "Name=instance-state-name,Values=running,stopped" \
        --query 'Reservations[0].Instances[0].VpcId' \
        --output text)
fi

echo "  Source VPC: $SOURCE_VPC"
echo "  Target VPC: $TARGET_VPC"

# Check if clusters are in same VPC
if [ "$SOURCE_VPC" == "$TARGET_VPC" ]; then
    echo "  ✓ Clusters are in the same VPC"
    SHARED_VPC=$SOURCE_VPC
else
    echo "  ⚠ Clusters are in different VPCs - will require VPC peering"
    echo "  This script currently only supports same-VPC setups"
    exit 1
fi

# Get all private subnets from both clusters
echo "→ Getting subnet information..."

# Try tag-based detection first (for standard IPI deployments)
SOURCE_SUBNETS=$(aws ec2 describe-subnets \
    --region $REGION \
    --filters "Name=tag:kubernetes.io/cluster/$SOURCE_CLUSTER,Values=owned" \
              "Name=tag:kubernetes.io/role/internal-elb,Values=1" \
    --query 'Subnets[*].SubnetId' \
    --output text)

# If tag-based detection fails (BYOVPC), get subnets from VPC private subnets
if [ -z "$SOURCE_SUBNETS" ]; then
    echo "  Source cluster using BYOVPC, getting private subnets from VPC..."
    SOURCE_SUBNETS=$(aws ec2 describe-subnets \
        --region $REGION \
        --filters "Name=vpc-id,Values=$SOURCE_VPC" \
        --query 'Subnets[?MapPublicIpOnLaunch==`false`].SubnetId' \
        --output text)
fi

# Try tag-based detection first (for standard IPI deployments)
TARGET_SUBNETS=$(aws ec2 describe-subnets \
    --region $REGION \
    --filters "Name=tag:kubernetes.io/cluster/$TARGET_CLUSTER,Values=owned" \
              "Name=tag:kubernetes.io/role/internal-elb,Values=1" \
    --query 'Subnets[*].SubnetId' \
    --output text)

# If tag-based detection fails (BYOVPC), get subnets from VPC private subnets
if [ -z "$TARGET_SUBNETS" ]; then
    echo "  Target cluster using BYOVPC, getting private subnets from VPC..."
    TARGET_SUBNETS=$(aws ec2 describe-subnets \
        --region $REGION \
        --filters "Name=vpc-id,Values=$TARGET_VPC" \
        --query 'Subnets[?MapPublicIpOnLaunch==`false`].SubnetId' \
        --output text)
fi

ALL_SUBNETS="$SOURCE_SUBNETS $TARGET_SUBNETS"
UNIQUE_SUBNETS=$(echo $ALL_SUBNETS | tr ' ' '\n' | sort -u | tr '\n' ' ')

echo "  Subnets: $UNIQUE_SUBNETS"

# Get security groups for both clusters
echo "→ Getting security groups..."

# Try tag-based detection first (for standard IPI deployments)
SOURCE_SG=$(aws ec2 describe-security-groups \
    --region $REGION \
    --filters "Name=tag:kubernetes.io/cluster/$SOURCE_CLUSTER,Values=owned" \
              "Name=tag-key,Values=Name" \
    --query 'SecurityGroups[?contains(GroupName, `worker`)].GroupId' \
    --output text | awk '{print $1}')

# If tag-based detection fails (BYOVPC), get security group from cluster instances
if [ -z "$SOURCE_SG" ]; then
    echo "  Source cluster using BYOVPC, getting security group from instances..."
    SOURCE_SG=$(aws ec2 describe-instances \
        --region $REGION \
        --filters "Name=tag:Name,Values=${SOURCE_CLUSTER}*" "Name=instance-state-name,Values=running,stopped" \
        --query 'Reservations[0].Instances[0].SecurityGroups[0].GroupId' \
        --output text)
fi

# Try tag-based detection first (for standard IPI deployments)
TARGET_SG=$(aws ec2 describe-security-groups \
    --region $REGION \
    --filters "Name=tag:kubernetes.io/cluster/$TARGET_CLUSTER,Values=owned" \
              "Name=tag-key,Values=Name" \
    --query 'SecurityGroups[?contains(GroupName, `worker`)].GroupId' \
    --output text | awk '{print $1}')

# If tag-based detection fails (BYOVPC), get security group from cluster instances
if [ -z "$TARGET_SG" ]; then
    echo "  Target cluster using BYOVPC, getting security group from instances..."
    TARGET_SG=$(aws ec2 describe-instances \
        --region $REGION \
        --filters "Name=tag:Name,Values=${TARGET_CLUSTER}*" "Name=instance-state-name,Values=running,stopped" \
        --query 'Reservations[0].Instances[0].SecurityGroups[0].GroupId' \
        --output text)
fi

echo "  Source SG: $SOURCE_SG"
echo "  Target SG: $TARGET_SG"

# Create shared EFS security group
echo "→ Creating shared EFS security group..."
SHARED_SG_NAME="migration-shared-efs-sg"
SHARED_SG=$(aws ec2 create-security-group \
    --region $REGION \
    --group-name "$SHARED_SG_NAME" \
    --description "Shared EFS security group for migration between $SOURCE_CLUSTER and $TARGET_CLUSTER" \
    --vpc-id $SHARED_VPC \
    --query 'GroupId' \
    --output text 2>/dev/null || \
    aws ec2 describe-security-groups --region $REGION --filters "Name=group-name,Values=$SHARED_SG_NAME" --query 'SecurityGroups[0].GroupId' --output text)

echo "  Shared SG: $SHARED_SG"

# Allow NFS access from both clusters
echo "→ Configuring NFS access rules..."
for SG in $SOURCE_SG $TARGET_SG; do
    echo "  Allowing access from $SG..."
    aws ec2 authorize-security-group-ingress \
        --region $REGION \
        --group-id $SHARED_SG \
        --protocol tcp \
        --port 2049 \
        --source-group $SG \
        2>/dev/null || echo "    Rule already exists"
done

# Tag security group
aws ec2 create-tags \
    --region $REGION \
    --resources $SHARED_SG \
    --tags "Key=Name,Value=$SHARED_SG_NAME" \
           "Key=Purpose,Value=migration-storage" \
           "Key=SourceCluster,Value=$SOURCE_CLUSTER" \
           "Key=TargetCluster,Value=$TARGET_CLUSTER" \
           "Key=ManagedBy,Value=ocpctl"

# Create shared EFS file system
echo "→ Creating shared EFS file system..."
SHARED_EFS_NAME="migration-shared-storage"
SHARED_EFS_ID=$(aws efs create-file-system \
    --region $REGION \
    --performance-mode generalPurpose \
    --throughput-mode bursting \
    --encrypted \
    --tags "Key=Name,Value=$SHARED_EFS_NAME" \
           "Key=Purpose,Value=migration-storage" \
           "Key=SourceCluster,Value=$SOURCE_CLUSTER" \
           "Key=TargetCluster,Value=$TARGET_CLUSTER" \
           "Key=ManagedBy,Value=ocpctl" \
    --query 'FileSystemId' \
    --output text 2>/dev/null || \
    aws efs describe-file-systems --region $REGION --query "FileSystems[?Tags[?Key=='Name' && Value=='$SHARED_EFS_NAME']].FileSystemId" --output text)

echo "  Shared EFS ID: $SHARED_EFS_ID"

# Wait for EFS to be available
echo "→ Waiting for EFS to become available..."
# Polling loop instead of 'aws efs wait' (not available in AWS CLI v1)
for i in {1..60}; do
    EFS_STATE=$(aws efs describe-file-systems \
        --region $REGION \
        --file-system-id $SHARED_EFS_ID \
        --query 'FileSystems[0].LifeCycleState' \
        --output text)

    if [ "$EFS_STATE" == "available" ]; then
        echo "  EFS is available"
        break
    fi

    if [ $i -eq 60 ]; then
        echo "  ERROR: EFS did not become available within 5 minutes"
        exit 1
    fi

    sleep 5
done

# Create mount targets
echo "→ Creating mount targets in all subnets..."
for SUBNET in $UNIQUE_SUBNETS; do
    echo "  Creating mount target in subnet: $SUBNET"
    aws efs create-mount-target \
        --region $REGION \
        --file-system-id $SHARED_EFS_ID \
        --subnet-id $SUBNET \
        --security-groups $SHARED_SG \
        2>/dev/null || echo "    Mount target already exists"
done

# Create access point for migration data
echo "→ Creating EFS access point for migration data..."
MIGRATION_AP=$(aws efs create-access-point \
    --region $REGION \
    --file-system-id $SHARED_EFS_ID \
    --posix-user Uid=1000,Gid=1000 \
    --root-directory "Path=/migration-data,CreationInfo={OwnerUid=1000,OwnerGid=1000,Permissions=755}" \
    --tags "Key=Name,Value=migration-data-ap" \
    --query 'AccessPointId' \
    --output text 2>/dev/null || echo "already exists")

echo "  Access Point: $MIGRATION_AP"

# Create S3 bucket for object storage backups
echo "→ Creating S3 bucket for migration backups..."
BUCKET_NAME="ocpctl-migration-${SOURCE_CLUSTER}-${TARGET_CLUSTER}-$(date +%s)"
aws s3 mb "s3://$BUCKET_NAME" --region $REGION 2>/dev/null || echo "  Bucket already exists"

# Enable versioning and encryption
aws s3api put-bucket-versioning \
    --bucket $BUCKET_NAME \
    --versioning-configuration Status=Enabled

aws s3api put-bucket-encryption \
    --bucket $BUCKET_NAME \
    --server-side-encryption-configuration '{
        "Rules": [{
            "ApplyServerSideEncryptionByDefault": {
                "SSEAlgorithm": "AES256"
            }
        }]
    }'

# Tag bucket
aws s3api put-bucket-tagging \
    --bucket $BUCKET_NAME \
    --tagging "TagSet=[
        {Key=Name,Value=migration-backup-storage},
        {Key=SourceCluster,Value=$SOURCE_CLUSTER},
        {Key=TargetCluster,Value=$TARGET_CLUSTER},
        {Key=ManagedBy,Value=ocpctl}
    ]"

echo "  S3 Bucket: s3://$BUCKET_NAME"

echo ""
echo "========================================="
echo "✅ Shared Migration Storage Complete!"
echo "========================================="
echo ""
echo "Shared EFS:"
echo "  File System ID: $SHARED_EFS_ID"
echo "  Access Point: $MIGRATION_AP"
echo "  Security Group: $SHARED_SG"
echo ""
echo "S3 Backup Storage:"
echo "  Bucket: s3://$BUCKET_NAME"
echo ""

# Output JSON for programmatic parsing (OCPCTL_OUTPUT marker)
echo "OCPCTL_OUTPUT_START"
cat <<JSON_OUTPUT
{
  "efs_id": "$SHARED_EFS_ID",
  "efs_access_point_id": "$MIGRATION_AP",
  "efs_security_group_id": "$SHARED_SG",
  "s3_bucket": "$BUCKET_NAME",
  "region": "$REGION",
  "source_cluster": "$SOURCE_CLUSTER",
  "target_cluster": "$TARGET_CLUSTER"
}
JSON_OUTPUT
echo "OCPCTL_OUTPUT_END"
