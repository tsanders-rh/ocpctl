#!/bin/bash
# Setup Disaster Recovery for OCPCTL
# This script configures AWS RDS automated backups and S3 protection

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Configuration
DB_INSTANCE_ID="${DB_INSTANCE_ID:-ocpctl-production}"
PRIMARY_BUCKET="${PRIMARY_BUCKET:-ocpctl-binaries}"
REPLICA_BUCKET="${REPLICA_BUCKET:-ocpctl-binaries-replica}"
PRIMARY_REGION="${PRIMARY_REGION:-us-east-1}"
REPLICA_REGION="${REPLICA_REGION:-us-west-2}"
BACKUP_RETENTION_DAYS="${BACKUP_RETENTION_DAYS:-30}"
BACKUP_WINDOW="${BACKUP_WINDOW:-02:00-03:00}"

echo -e "${BLUE}=== OCPCTL Disaster Recovery Setup ===${NC}"
echo ""
echo "Configuration:"
echo "  RDS Instance: $DB_INSTANCE_ID"
echo "  Primary Bucket: $PRIMARY_BUCKET ($PRIMARY_REGION)"
echo "  Replica Bucket: $REPLICA_BUCKET ($REPLICA_REGION)"
echo "  Backup Retention: $BACKUP_RETENTION_DAYS days"
echo ""

# Check if running in dry-run mode
DRY_RUN="${DRY_RUN:-false}"
if [ "$DRY_RUN" = "true" ]; then
    echo -e "${YELLOW}Running in DRY-RUN mode - no changes will be made${NC}"
    echo ""
fi

# Function to run command or print in dry-run mode
run_cmd() {
    if [ "$DRY_RUN" = "true" ]; then
        echo -e "${YELLOW}[DRY-RUN]${NC} $*"
    else
        "$@"
    fi
}

# =============================================================================
# Step 1: Configure RDS Automated Backups
# =============================================================================
echo -e "${BLUE}Step 1: Configuring RDS Automated Backups${NC}"

echo "  - Checking if RDS instance exists..."
if aws rds describe-db-instances \
    --db-instance-identifier "$DB_INSTANCE_ID" \
    --region "$PRIMARY_REGION" \
    --query 'DBInstances[0].DBInstanceIdentifier' \
    --output text 2>/dev/null | grep -q "$DB_INSTANCE_ID"; then

    echo -e "  ${GREEN}✓${NC} RDS instance found: $DB_INSTANCE_ID"

    # Get current backup retention
    CURRENT_RETENTION=$(aws rds describe-db-instances \
        --db-instance-identifier "$DB_INSTANCE_ID" \
        --region "$PRIMARY_REGION" \
        --query 'DBInstances[0].BackupRetentionPeriod' \
        --output text)

    echo "  - Current backup retention: $CURRENT_RETENTION days"

    if [ "$CURRENT_RETENTION" -lt "$BACKUP_RETENTION_DAYS" ]; then
        echo "  - Enabling automated backups with $BACKUP_RETENTION_DAYS-day retention..."
        run_cmd aws rds modify-db-instance \
            --db-instance-identifier "$DB_INSTANCE_ID" \
            --region "$PRIMARY_REGION" \
            --backup-retention-period "$BACKUP_RETENTION_DAYS" \
            --preferred-backup-window "$BACKUP_WINDOW" \
            --deletion-protection \
            --apply-immediately \
            --no-cli-pager

        echo -e "  ${GREEN}✓${NC} RDS automated backups configured"
    else
        echo -e "  ${GREEN}✓${NC} RDS backups already configured ($CURRENT_RETENTION days)"
    fi

    # Check deletion protection
    DELETION_PROTECTION=$(aws rds describe-db-instances \
        --db-instance-identifier "$DB_INSTANCE_ID" \
        --region "$PRIMARY_REGION" \
        --query 'DBInstances[0].DeletionProtection' \
        --output text)

    if [ "$DELETION_PROTECTION" = "False" ]; then
        echo "  - Enabling deletion protection..."
        run_cmd aws rds modify-db-instance \
            --db-instance-identifier "$DB_INSTANCE_ID" \
            --region "$PRIMARY_REGION" \
            --deletion-protection \
            --apply-immediately \
            --no-cli-pager
        echo -e "  ${GREEN}✓${NC} Deletion protection enabled"
    else
        echo -e "  ${GREEN}✓${NC} Deletion protection already enabled"
    fi
else
    echo -e "  ${RED}✗${NC} RDS instance not found: $DB_INSTANCE_ID"
    echo "     Please create the RDS instance first or set DB_INSTANCE_ID environment variable"
    exit 1
fi

echo ""

# =============================================================================
# Step 2: Enable S3 Versioning
# =============================================================================
echo -e "${BLUE}Step 2: Enabling S3 Versioning${NC}"

echo "  - Checking if bucket exists: $PRIMARY_BUCKET..."
if aws s3api head-bucket --bucket "$PRIMARY_BUCKET" --region "$PRIMARY_REGION" 2>/dev/null; then
    echo -e "  ${GREEN}✓${NC} Bucket found: $PRIMARY_BUCKET"

    # Check versioning status
    VERSIONING_STATUS=$(aws s3api get-bucket-versioning \
        --bucket "$PRIMARY_BUCKET" \
        --region "$PRIMARY_REGION" \
        --query 'Status' \
        --output text 2>/dev/null || echo "Disabled")

    echo "  - Current versioning status: $VERSIONING_STATUS"

    if [ "$VERSIONING_STATUS" != "Enabled" ]; then
        echo "  - Enabling versioning on $PRIMARY_BUCKET..."
        run_cmd aws s3api put-bucket-versioning \
            --bucket "$PRIMARY_BUCKET" \
            --region "$PRIMARY_REGION" \
            --versioning-configuration Status=Enabled
        echo -e "  ${GREEN}✓${NC} S3 versioning enabled"
    else
        echo -e "  ${GREEN}✓${NC} S3 versioning already enabled"
    fi
else
    echo -e "  ${YELLOW}⚠${NC}  Bucket not found: $PRIMARY_BUCKET"
    echo "     Skipping S3 versioning (bucket will be created during deployment)"
fi

echo ""

# =============================================================================
# Step 3: Configure S3 Cross-Region Replication
# =============================================================================
echo -e "${BLUE}Step 3: Configuring S3 Cross-Region Replication${NC}"

# Check if primary bucket exists
if aws s3api head-bucket --bucket "$PRIMARY_BUCKET" --region "$PRIMARY_REGION" 2>/dev/null; then

    # Create replica bucket if it doesn't exist
    echo "  - Checking if replica bucket exists: $REPLICA_BUCKET..."
    if ! aws s3api head-bucket --bucket "$REPLICA_BUCKET" --region "$REPLICA_REGION" 2>/dev/null; then
        echo "  - Creating replica bucket in $REPLICA_REGION..."
        if [ "$REPLICA_REGION" = "us-east-1" ]; then
            run_cmd aws s3api create-bucket \
                --bucket "$REPLICA_BUCKET" \
                --region "$REPLICA_REGION"
        else
            run_cmd aws s3api create-bucket \
                --bucket "$REPLICA_BUCKET" \
                --region "$REPLICA_REGION" \
                --create-bucket-configuration LocationConstraint="$REPLICA_REGION"
        fi
        echo -e "  ${GREEN}✓${NC} Replica bucket created"
    else
        echo -e "  ${GREEN}✓${NC} Replica bucket already exists"
    fi

    # Enable versioning on replica bucket
    echo "  - Enabling versioning on replica bucket..."
    run_cmd aws s3api put-bucket-versioning \
        --bucket "$REPLICA_BUCKET" \
        --region "$REPLICA_REGION" \
        --versioning-configuration Status=Enabled
    echo -e "  ${GREEN}✓${NC} Replica bucket versioning enabled"

    # Get AWS account ID
    ACCOUNT_ID=$(aws sts get-caller-identity --query 'Account' --output text)

    # Create replication role if it doesn't exist
    ROLE_NAME="ocpctl-s3-replication-role"
    echo "  - Checking if replication IAM role exists..."
    if ! aws iam get-role --role-name "$ROLE_NAME" 2>/dev/null >/dev/null; then
        echo "  - Creating replication IAM role..."

        # Create trust policy
        cat > /tmp/trust-policy.json <<EOF
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Principal": {
        "Service": "s3.amazonaws.com"
      },
      "Action": "sts:AssumeRole"
    }
  ]
}
EOF

        run_cmd aws iam create-role \
            --role-name "$ROLE_NAME" \
            --assume-role-policy-document file:///tmp/trust-policy.json

        # Create replication policy
        cat > /tmp/replication-policy.json <<EOF
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Action": [
        "s3:GetReplicationConfiguration",
        "s3:ListBucket"
      ],
      "Resource": "arn:aws:s3:::${PRIMARY_BUCKET}"
    },
    {
      "Effect": "Allow",
      "Action": [
        "s3:GetObjectVersionForReplication",
        "s3:GetObjectVersionAcl"
      ],
      "Resource": "arn:aws:s3:::${PRIMARY_BUCKET}/*"
    },
    {
      "Effect": "Allow",
      "Action": [
        "s3:ReplicateObject",
        "s3:ReplicateDelete"
      ],
      "Resource": "arn:aws:s3:::${REPLICA_BUCKET}/*"
    }
  ]
}
EOF

        run_cmd aws iam put-role-policy \
            --role-name "$ROLE_NAME" \
            --policy-name "S3ReplicationPolicy" \
            --policy-document file:///tmp/replication-policy.json

        echo -e "  ${GREEN}✓${NC} Replication IAM role created"

        # Wait for role to propagate
        if [ "$DRY_RUN" != "true" ]; then
            echo "  - Waiting for IAM role to propagate..."
            sleep 10
        fi
    else
        echo -e "  ${GREEN}✓${NC} Replication IAM role already exists"
    fi

    # Configure replication
    echo "  - Configuring replication rule..."
    cat > /tmp/replication-config.json <<EOF
{
  "Role": "arn:aws:iam::${ACCOUNT_ID}:role/${ROLE_NAME}",
  "Rules": [
    {
      "ID": "ReplicateAll",
      "Priority": 1,
      "Filter": {},
      "Status": "Enabled",
      "Destination": {
        "Bucket": "arn:aws:s3:::${REPLICA_BUCKET}",
        "ReplicationTime": {
          "Status": "Enabled",
          "Time": {
            "Minutes": 15
          }
        },
        "Metrics": {
          "Status": "Enabled",
          "EventThreshold": {
            "Minutes": 15
          }
        }
      },
      "DeleteMarkerReplication": {
        "Status": "Enabled"
      }
    }
  ]
}
EOF

    run_cmd aws s3api put-bucket-replication \
        --bucket "$PRIMARY_BUCKET" \
        --region "$PRIMARY_REGION" \
        --replication-configuration file:///tmp/replication-config.json

    echo -e "  ${GREEN}✓${NC} S3 replication configured"

    # Clean up temp files
    rm -f /tmp/trust-policy.json /tmp/replication-policy.json /tmp/replication-config.json
else
    echo -e "  ${YELLOW}⚠${NC}  Primary bucket not found, skipping replication setup"
fi

echo ""

# =============================================================================
# Summary
# =============================================================================
echo -e "${GREEN}=== Disaster Recovery Setup Complete ===${NC}"
echo ""
echo "Configuration Summary:"
echo "  • RDS automated backups: $BACKUP_RETENTION_DAYS days retention"
echo "  • RDS backup window: $BACKUP_WINDOW UTC"
echo "  • RDS deletion protection: Enabled"
echo "  • S3 versioning: Enabled on $PRIMARY_BUCKET"
echo "  • S3 replication: $PRIMARY_BUCKET → $REPLICA_BUCKET"
echo "  • Replication lag target: < 15 minutes"
echo ""
echo "Next Steps:"
echo "  1. Verify backups: ./scripts/verify-backups.sh"
echo "  2. Test recovery: docs/disaster-recovery/RECOVERY_RUNBOOK.md"
echo "  3. Schedule monthly DR drills"
echo ""
echo "Recovery Costs (estimated):"
echo "  • RDS backup storage: ~\$1/month (10GB database)"
echo "  • S3 replication: ~\$5/month (100GB artifacts)"
echo "  • Total: ~\$6/month"
echo ""
