#!/bin/bash
#
# Deploy OCPCTL Worker Auto-Scaling Infrastructure
#
# This script:
# 1. Builds the worker binary for Linux
# 2. Uploads the binary to S3
# 3. Creates necessary IAM roles and policies
# 4. Creates worker security group
# 5. Deploys Terraform auto-scaling infrastructure
#
# Usage: ./scripts/deploy-worker-autoscaling.sh [options]
#
# Options:
#   --region REGION        AWS region (default: us-east-1)
#   --vpc-name NAME        VPC name tag (required)
#   --s3-bucket BUCKET     S3 bucket for worker binary (required)
#   --database-url URL     PostgreSQL connection URL (required)
#   --ami-id AMI           Amazon Linux 2023 AMI ID (optional, will auto-detect)
#   --skip-build           Skip building worker binary
#   --skip-upload          Skip uploading binary to S3
#   --skip-iam             Skip IAM role creation
#   --skip-sg              Skip security group creation
#   --apply                Auto-approve Terraform apply (default: plan only)
#

set -e

# Default values
AWS_REGION="us-east-1"
PROJECT_NAME="ocpctl"
SKIP_BUILD=""
SKIP_UPLOAD=""
SKIP_IAM=""
SKIP_SG=""
AUTO_APPLY=""

# Parse arguments
while [[ $# -gt 0 ]]; do
    case $1 in
        --region)
            AWS_REGION="$2"
            shift 2
            ;;
        --vpc-name)
            VPC_NAME="$2"
            shift 2
            ;;
        --s3-bucket)
            S3_BUCKET="$2"
            shift 2
            ;;
        --database-url)
            DATABASE_URL="$2"
            shift 2
            ;;
        --ami-id)
            AMI_ID="$2"
            shift 2
            ;;
        --skip-build)
            SKIP_BUILD="true"
            shift
            ;;
        --skip-upload)
            SKIP_UPLOAD="true"
            shift
            ;;
        --skip-iam)
            SKIP_IAM="true"
            shift
            ;;
        --skip-sg)
            SKIP_SG="true"
            shift
            ;;
        --apply)
            AUTO_APPLY="true"
            shift
            ;;
        *)
            echo "Unknown option: $1"
            exit 1
            ;;
    esac
done

# Validate required parameters
if [ -z "$VPC_NAME" ]; then
    echo "Error: --vpc-name is required"
    exit 1
fi

if [ -z "$S3_BUCKET" ]; then
    echo "Error: --s3-bucket is required"
    exit 1
fi

if [ -z "$DATABASE_URL" ]; then
    echo "Error: --database-url is required"
    exit 1
fi

echo "=== OCPCTL Worker Auto-Scaling Deployment ==="
echo "Region: $AWS_REGION"
echo "VPC: $VPC_NAME"
echo "S3 Bucket: $S3_BUCKET"
echo ""

# Step 1: Build worker binary
if [ -z "$SKIP_BUILD" ]; then
    echo "[1/6] Building worker binary for Linux..."
    make build-linux
    echo "✓ Binary built: bin/ocpctl-worker-linux"
else
    echo "[1/6] Skipping binary build"
fi

# Step 2: Upload worker binary to S3
WORKER_BINARY_URL="s3://${S3_BUCKET}/binaries/ocpctl-worker"
if [ -z "$SKIP_UPLOAD" ]; then
    echo ""
    echo "[2/6] Uploading worker binary to S3..."
    aws s3 cp bin/ocpctl-worker-linux "$WORKER_BINARY_URL" \
        --region "$AWS_REGION"
    echo "✓ Binary uploaded to $WORKER_BINARY_URL"
else
    echo ""
    echo "[2/6] Skipping binary upload"
fi

# Step 3: Get or detect AMI ID
if [ -z "$AMI_ID" ]; then
    echo ""
    echo "[3/6] Auto-detecting Amazon Linux 2023 AMI..."
    AMI_ID=$(aws ec2 describe-images \
        --region "$AWS_REGION" \
        --owners amazon \
        --filters "Name=name,Values=al2023-ami-2023.*-x86_64" \
        "Name=state,Values=available" \
        --query 'Images | sort_by(@, &CreationDate) | [-1].ImageId' \
        --output text)
    echo "✓ Found AMI: $AMI_ID"
else
    echo ""
    echo "[3/6] Using provided AMI: $AMI_ID"
fi

# Step 4: Create IAM role and policies
if [ -z "$SKIP_IAM" ]; then
    echo ""
    echo "[4/6] Creating IAM role and policies..."

    # Check if role already exists
    if aws iam get-role --role-name "${PROJECT_NAME}-worker-role" --region "$AWS_REGION" >/dev/null 2>&1; then
        echo "  IAM role ${PROJECT_NAME}-worker-role already exists"
    else
        echo "  Creating IAM role..."

        # Create trust policy
        cat > /tmp/worker-trust-policy.json <<EOF
{
  "Version": "2012-10-17",
  "Statement": [{
    "Effect": "Allow",
    "Principal": {
      "Service": "ec2.amazonaws.com"
    },
    "Action": "sts:AssumeRole"
  }]
}
EOF

        aws iam create-role \
            --role-name "${PROJECT_NAME}-worker-role" \
            --assume-role-policy-document file:///tmp/worker-trust-policy.json \
            --region "$AWS_REGION"

        # Create and attach CloudWatch policy
        cat > /tmp/worker-cloudwatch-policy.json <<EOF
{
  "Version": "2012-10-17",
  "Statement": [{
    "Effect": "Allow",
    "Action": [
      "cloudwatch:PutMetricData"
    ],
    "Resource": "*"
  }]
}
EOF

        aws iam put-role-policy \
            --role-name "${PROJECT_NAME}-worker-role" \
            --policy-name "${PROJECT_NAME}-worker-cloudwatch" \
            --policy-document file:///tmp/worker-cloudwatch-policy.json \
            --region "$AWS_REGION"

        # Create and attach S3 policy for worker binary download
        cat > /tmp/worker-s3-policy.json <<EOF
{
  "Version": "2012-10-17",
  "Statement": [{
    "Effect": "Allow",
    "Action": [
      "s3:GetObject"
    ],
    "Resource": "arn:aws:s3:::${S3_BUCKET}/binaries/*"
  }]
}
EOF

        aws iam put-role-policy \
            --role-name "${PROJECT_NAME}-worker-role" \
            --policy-name "${PROJECT_NAME}-worker-s3" \
            --policy-document file:///tmp/worker-s3-policy.json \
            --region "$AWS_REGION"

        # Cleanup temp files
        rm -f /tmp/worker-trust-policy.json /tmp/worker-cloudwatch-policy.json /tmp/worker-s3-policy.json

        echo "✓ IAM role created: ${PROJECT_NAME}-worker-role"
    fi
else
    echo ""
    echo "[4/6] Skipping IAM role creation"
fi

# Step 5: Create security group
if [ -z "$SKIP_SG" ]; then
    echo ""
    echo "[5/6] Creating worker security group..."

    # Get VPC ID
    VPC_ID=$(aws ec2 describe-vpcs \
        --region "$AWS_REGION" \
        --filters "Name=tag:Name,Values=$VPC_NAME" \
        --query 'Vpcs[0].VpcId' \
        --output text)

    if [ "$VPC_ID" == "None" ] || [ -z "$VPC_ID" ]; then
        echo "Error: VPC '$VPC_NAME' not found"
        exit 1
    fi

    echo "  Found VPC: $VPC_ID"

    # Check if security group already exists
    SG_ID=$(aws ec2 describe-security-groups \
        --region "$AWS_REGION" \
        --filters "Name=vpc-id,Values=$VPC_ID" "Name=tag:Name,Values=${PROJECT_NAME}-worker-sg" \
        --query 'SecurityGroups[0].GroupId' \
        --output text)

    if [ "$SG_ID" != "None" ] && [ -n "$SG_ID" ]; then
        echo "  Security group ${PROJECT_NAME}-worker-sg already exists: $SG_ID"
    else
        echo "  Creating security group..."
        SG_ID=$(aws ec2 create-security-group \
            --region "$AWS_REGION" \
            --group-name "${PROJECT_NAME}-worker-sg" \
            --description "Security group for OCPCTL worker instances" \
            --vpc-id "$VPC_ID" \
            --tag-specifications "ResourceType=security-group,Tags=[{Key=Name,Value=${PROJECT_NAME}-worker-sg}]" \
            --query 'GroupId' \
            --output text)

        echo "✓ Security group created: $SG_ID"
        echo "  NOTE: Configure database access rules separately"
    fi
else
    echo ""
    echo "[5/6] Skipping security group creation"
fi

# Step 6: Deploy Terraform infrastructure
echo ""
echo "[6/6] Deploying Terraform auto-scaling infrastructure..."

cd terraform/worker-autoscaling

# Create terraform.tfvars
cat > terraform.tfvars <<EOF
aws_region         = "$AWS_REGION"
project_name       = "$PROJECT_NAME"
vpc_name           = "$VPC_NAME"
ami_id             = "$AMI_ID"
database_url       = "$DATABASE_URL"
worker_binary_url  = "$WORKER_BINARY_URL"

# Auto-scaling configuration
asg_min_size              = 1
asg_max_size              = 10
asg_desired_capacity      = 1
pending_jobs_per_worker   = 2

# Worker configuration
instance_type         = "t3.small"
work_dir              = "/var/lib/ocpctl"
worker_poll_interval  = 10
worker_max_concurrent = 3

# Tags
tags = {
  ManagedBy = "Terraform"
  Project   = "OCPCTL"
}
EOF

echo "  Initializing Terraform..."
terraform init

if [ -n "$AUTO_APPLY" ]; then
    echo "  Applying Terraform configuration..."
    terraform apply -auto-approve
    echo ""
    echo "✓ Auto-scaling infrastructure deployed successfully!"
else
    echo "  Planning Terraform changes..."
    terraform plan
    echo ""
    echo "=== Deployment Plan Ready ==="
    echo "Review the plan above and run with --apply to deploy:"
    echo "  ./scripts/deploy-worker-autoscaling.sh --apply [other options]"
fi

cd ../..

echo ""
echo "=== Deployment Complete ==="
echo ""
echo "Worker binary:  $WORKER_BINARY_URL"
echo "IAM role:       ${PROJECT_NAME}-worker-role"
echo "Security group: ${PROJECT_NAME}-worker-sg"
echo ""
echo "Next steps:"
echo "1. Configure database security group to allow access from worker SG"
echo "2. Monitor CloudWatch metrics: OCPCTL/PendingJobs, OCPCTL/WorkerActive"
echo "3. Check Auto Scaling Group: ${PROJECT_NAME}-worker-asg"
echo "4. View worker logs: aws logs tail /aws/ec2/instance-id --follow"
