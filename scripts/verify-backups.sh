#!/bin/bash
# Verify OCPCTL Disaster Recovery Configuration
# This script checks that backups are properly configured and current

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
BACKUP_MAX_AGE_HOURS="${BACKUP_MAX_AGE_HOURS:-25}"

echo -e "${BLUE}=== OCPCTL Backup Verification ===${NC}"
echo ""

ERRORS=0
WARNINGS=0

# =============================================================================
# Check RDS Backups
# =============================================================================
echo -e "${BLUE}Checking RDS Backups${NC}"

if aws rds describe-db-instances \
    --db-instance-identifier "$DB_INSTANCE_ID" \
    --region "$PRIMARY_REGION" \
    --query 'DBInstances[0].DBInstanceIdentifier' \
    --output text 2>/dev/null | grep -q "$DB_INSTANCE_ID"; then

    # Check backup retention
    RETENTION=$(aws rds describe-db-instances \
        --db-instance-identifier "$DB_INSTANCE_ID" \
        --region "$PRIMARY_REGION" \
        --query 'DBInstances[0].BackupRetentionPeriod' \
        --output text)

    if [ "$RETENTION" -ge 30 ]; then
        echo -e "  ${GREEN}✓${NC} Backup retention: $RETENTION days"
    elif [ "$RETENTION" -gt 0 ]; then
        echo -e "  ${YELLOW}⚠${NC}  Backup retention: $RETENTION days (recommended: 30)"
        WARNINGS=$((WARNINGS + 1))
    else
        echo -e "  ${RED}✗${NC} Automated backups are DISABLED"
        ERRORS=$((ERRORS + 1))
    fi

    # Check deletion protection
    DELETION_PROTECTION=$(aws rds describe-db-instances \
        --db-instance-identifier "$DB_INSTANCE_ID" \
        --region "$PRIMARY_REGION" \
        --query 'DBInstances[0].DeletionProtection' \
        --output text)

    if [ "$DELETION_PROTECTION" = "True" ]; then
        echo -e "  ${GREEN}✓${NC} Deletion protection: Enabled"
    else
        echo -e "  ${YELLOW}⚠${NC}  Deletion protection: Disabled (recommended: Enabled)"
        WARNINGS=$((WARNINGS + 1))
    fi

    # Check latest backup age
    LATEST_BACKUP=$(aws rds describe-db-snapshots \
        --db-instance-identifier "$DB_INSTANCE_ID" \
        --region "$PRIMARY_REGION" \
        --snapshot-type automated \
        --query 'DBSnapshots | sort_by(@, &SnapshotCreateTime) | [-1].SnapshotCreateTime' \
        --output text 2>/dev/null || echo "")

    if [ -n "$LATEST_BACKUP" ]; then
        BACKUP_AGE_SECONDS=$(( $(date +%s) - $(date -j -f "%Y-%m-%dT%H:%M:%S" "${LATEST_BACKUP%.*}" +%s 2>/dev/null || echo "0") ))
        BACKUP_AGE_HOURS=$(( BACKUP_AGE_SECONDS / 3600 ))

        if [ "$BACKUP_AGE_HOURS" -lt "$BACKUP_MAX_AGE_HOURS" ]; then
            echo -e "  ${GREEN}✓${NC} Latest backup: $BACKUP_AGE_HOURS hours old"
        else
            echo -e "  ${RED}✗${NC} Latest backup: $BACKUP_AGE_HOURS hours old (stale!)"
            ERRORS=$((ERRORS + 1))
        fi
    else
        echo -e "  ${YELLOW}⚠${NC}  No automated backups found yet"
        WARNINGS=$((WARNINGS + 1))
    fi

else
    echo -e "  ${RED}✗${NC} RDS instance not found: $DB_INSTANCE_ID"
    ERRORS=$((ERRORS + 1))
fi

echo ""

# =============================================================================
# Check S3 Versioning
# =============================================================================
echo -e "${BLUE}Checking S3 Versioning${NC}"

if aws s3api head-bucket --bucket "$PRIMARY_BUCKET" --region "$PRIMARY_REGION" 2>/dev/null; then
    VERSIONING_STATUS=$(aws s3api get-bucket-versioning \
        --bucket "$PRIMARY_BUCKET" \
        --region "$PRIMARY_REGION" \
        --query 'Status' \
        --output text 2>/dev/null || echo "Disabled")

    if [ "$VERSIONING_STATUS" = "Enabled" ]; then
        echo -e "  ${GREEN}✓${NC} Versioning enabled on $PRIMARY_BUCKET"
    else
        echo -e "  ${RED}✗${NC} Versioning DISABLED on $PRIMARY_BUCKET"
        ERRORS=$((ERRORS + 1))
    fi
else
    echo -e "  ${YELLOW}⚠${NC}  Bucket not found: $PRIMARY_BUCKET"
    WARNINGS=$((WARNINGS + 1))
fi

echo ""

# =============================================================================
# Check S3 Replication
# =============================================================================
echo -e "${BLUE}Checking S3 Replication${NC}"

if aws s3api head-bucket --bucket "$PRIMARY_BUCKET" --region "$PRIMARY_REGION" 2>/dev/null; then
    REPLICATION_STATUS=$(aws s3api get-bucket-replication \
        --bucket "$PRIMARY_BUCKET" \
        --region "$PRIMARY_REGION" \
        --query 'ReplicationConfiguration.Rules[0].Status' \
        --output text 2>/dev/null || echo "NotConfigured")

    if [ "$REPLICATION_STATUS" = "Enabled" ]; then
        echo -e "  ${GREEN}✓${NC} Replication enabled: $PRIMARY_BUCKET → $REPLICA_BUCKET"

        # Check if replica bucket exists
        if aws s3api head-bucket --bucket "$REPLICA_BUCKET" --region "$REPLICA_REGION" 2>/dev/null; then
            echo -e "  ${GREEN}✓${NC} Replica bucket exists: $REPLICA_BUCKET"

            # Check replica bucket versioning
            REPLICA_VERSIONING=$(aws s3api get-bucket-versioning \
                --bucket "$REPLICA_BUCKET" \
                --region "$REPLICA_REGION" \
                --query 'Status' \
                --output text 2>/dev/null || echo "Disabled")

            if [ "$REPLICA_VERSIONING" = "Enabled" ]; then
                echo -e "  ${GREEN}✓${NC} Replica bucket versioning enabled"
            else
                echo -e "  ${RED}✗${NC} Replica bucket versioning DISABLED"
                ERRORS=$((ERRORS + 1))
            fi
        else
            echo -e "  ${RED}✗${NC} Replica bucket not found: $REPLICA_BUCKET"
            ERRORS=$((ERRORS + 1))
        fi
    else
        echo -e "  ${YELLOW}⚠${NC}  Replication not configured (recommended for production)"
        WARNINGS=$((WARNINGS + 1))
    fi
else
    echo -e "  ${YELLOW}⚠${NC}  Primary bucket not found: $PRIMARY_BUCKET"
    WARNINGS=$((WARNINGS + 1))
fi

echo ""

# =============================================================================
# Summary
# =============================================================================
echo -e "${BLUE}=== Verification Summary ===${NC}"
echo ""

if [ $ERRORS -eq 0 ] && [ $WARNINGS -eq 0 ]; then
    echo -e "${GREEN}✓ All checks passed!${NC}"
    echo ""
    echo "Your disaster recovery is properly configured."
    exit 0
elif [ $ERRORS -eq 0 ]; then
    echo -e "${YELLOW}⚠ $WARNINGS warning(s) found${NC}"
    echo ""
    echo "Your backups are working but some recommended settings are not enabled."
    echo "Run: ./scripts/setup-disaster-recovery.sh"
    exit 0
else
    echo -e "${RED}✗ $ERRORS error(s) and $WARNINGS warning(s) found${NC}"
    echo ""
    echo "Your disaster recovery is NOT properly configured!"
    echo "Run: ./scripts/setup-disaster-recovery.sh"
    exit 1
fi
