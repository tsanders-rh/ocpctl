# OCPCTL Disaster Recovery Guide

## Overview

This guide provides procedures for recovering from various disaster scenarios affecting the OCPCTL platform.

**Recovery Time Objectives (RTO/RPO):**

| Scenario | RPO | RTO | Priority |
|----------|-----|-----|----------|
| Database corruption | < 5 minutes | < 30 minutes | P0 |
| Accidental data deletion | 0 | < 15 minutes | P1 |
| Region failure | < 1 hour | < 2 hours | P1 |
| Complete disaster | < 24 hours | < 4 hours | P2 |

---

## Quick Start

### Setup Disaster Recovery (First Time)

```bash
# Configure RDS backups and S3 replication
cd /Users/tsanders/Workspace2/ocpctl
./scripts/setup-disaster-recovery.sh

# Verify configuration
./scripts/verify-backups.sh
```

### Verify Backups (Monthly)

```bash
# Run verification script
./scripts/verify-backups.sh

# Should output: "✓ All checks passed!"
```

---

## Recovery Procedures

### Scenario 1: Database Corruption or Data Loss

**When to use:** Database has corrupted data, accidental DELETE, or needs rollback to earlier state.

#### Option A: Restore from Latest Automated Backup

```bash
# 1. List available automated backups
aws rds describe-db-snapshots \
  --db-instance-identifier ocpctl-production \
  --region us-east-1 \
  --snapshot-type automated \
  --query 'DBSnapshots | sort_by(@, &SnapshotCreateTime) | [-5:] | [].[DBSnapshotIdentifier, SnapshotCreateTime]' \
  --output table

# 2. Restore from specific snapshot (creates new instance)
aws rds restore-db-instance-from-db-snapshot \
  --db-instance-identifier ocpctl-production-restored-$(date +%Y%m%d) \
  --db-snapshot-identifier rds:ocpctl-production-YYYY-MM-DD-HH-MM \
  --db-instance-class db.t3.medium \
  --region us-east-1

# 3. Wait for restore to complete (5-10 minutes)
aws rds wait db-instance-available \
  --db-instance-identifier ocpctl-production-restored-$(date +%Y%m%d) \
  --region us-east-1

# 4. Get new endpoint
NEW_ENDPOINT=$(aws rds describe-db-instances \
  --db-instance-identifier ocpctl-production-restored-$(date +%Y%m%d) \
  --region us-east-1 \
  --query 'DBInstances[0].Endpoint.Address' \
  --output text)

echo "New database endpoint: $NEW_ENDPOINT"

# 5. Update DATABASE_URL in deployment
# Update environment variable or secrets manager with new endpoint
# Format: postgres://username:password@$NEW_ENDPOINT:5432/ocpctl?sslmode=require

# 6. Restart API and worker services to use new database
# (deployment-specific commands)

# 7. Verify application is working
curl http://ocpctl.mg.dog8code.com/health

# 8. Once verified, optionally rename instances:
# - Rename old: ocpctl-production → ocpctl-production-old
# - Rename new: ocpctl-production-restored-DATE → ocpctl-production
```

#### Option B: Point-in-Time Recovery (PITR)

**Use when:** Need to recover to a specific timestamp (e.g., "5 minutes before the bad DELETE")

```bash
# 1. Identify exact recovery time (UTC)
RECOVERY_TIME="2026-03-26T14:30:00Z"  # Replace with actual time

# 2. Restore to point in time
aws rds restore-db-instance-to-point-in-time \
  --source-db-instance-identifier ocpctl-production \
  --target-db-instance-identifier ocpctl-production-pitr-$(date +%Y%m%d-%H%M) \
  --restore-time "$RECOVERY_TIME" \
  --db-instance-class db.t3.medium \
  --region us-east-1

# 3. Wait for restore (5-10 minutes)
aws rds wait db-instance-available \
  --db-instance-identifier ocpctl-production-pitr-$(date +%Y%m%d-%H%M) \
  --region us-east-1

# 4. Follow steps 4-8 from Option A above
```

**Recovery Time:** 10-15 minutes
**Data Loss:** < 5 minutes (based on WAL archiving frequency)

---

### Scenario 2: Accidental File Deletion from S3

**When to use:** Accidentally deleted binary, manifest, or configuration file from S3.

#### Restore Deleted File

```bash
# 1. List all versions of the deleted file
aws s3api list-object-versions \
  --bucket ocpctl-binaries \
  --prefix releases/v0.20260325.f4193e1/ocpctl-worker \
  --region us-east-1 \
  --query 'Versions | sort_by(@, &LastModified) | [-5:] | [].[VersionId, LastModified, IsLatest]' \
  --output table

# 2. Identify the correct version (look for IsLatest=false before deletion)
VERSION_ID="<version-id-from-above>"

# 3. Restore by copying the old version to current
aws s3api copy-object \
  --bucket ocpctl-binaries \
  --copy-source "ocpctl-binaries/releases/v0.20260325.f4193e1/ocpctl-worker?versionId=$VERSION_ID" \
  --key releases/v0.20260325.f4193e1/ocpctl-worker \
  --region us-east-1

# 4. Verify file is restored
aws s3 ls s3://ocpctl-binaries/releases/v0.20260325.f4193e1/
```

**Recovery Time:** < 5 minutes
**Data Loss:** 0 (versioning captures all versions)

---

### Scenario 3: Region Failure (us-east-1 unavailable)

**When to use:** AWS region outage, can't access primary resources.

#### Switch to Replica Region

```bash
# 1. Verify replica bucket is available
aws s3 ls s3://ocpctl-binaries-replica/ --region us-west-2

# 2. Update deployment configuration
# Set these environment variables in your deployment:
export ARTIFACTS_BUCKET=ocpctl-binaries-replica
export ARTIFACTS_REGION=us-west-2
export AWS_REGION=us-west-2

# 3. For database: Promote read replica (if configured)
aws rds promote-read-replica \
  --db-instance-identifier ocpctl-production-replica \
  --region us-west-2

# Wait for promotion to complete
aws rds wait db-instance-available \
  --db-instance-identifier ocpctl-production-replica \
  --region us-west-2

# 4. Get promoted database endpoint
REPLICA_ENDPOINT=$(aws rds describe-db-instances \
  --db-instance-identifier ocpctl-production-replica \
  --region us-west-2 \
  --query 'DBInstances[0].Endpoint.Address' \
  --output text)

# 5. Update DATABASE_URL to use promoted replica
export DATABASE_URL="postgres://username:password@$REPLICA_ENDPOINT:5432/ocpctl?sslmode=require"

# 6. Redeploy services in us-west-2
# (deployment-specific commands)
```

**Recovery Time:** 1-2 hours (depending on read replica promotion)
**Data Loss:** < 1 hour (replication lag)

---

### Scenario 4: Complete Disaster Recovery

**When to use:** Need to rebuild entire system from scratch.

#### Full System Rebuild

```bash
# 1. Create new RDS instance in target region
aws rds restore-db-instance-from-db-snapshot \
  --db-instance-identifier ocpctl-production-new \
  --db-snapshot-identifier <latest-snapshot-id> \
  --db-instance-class db.t3.medium \
  --region us-east-1

# 2. Wait for database to be available
aws rds wait db-instance-available \
  --db-instance-identifier ocpctl-production-new \
  --region us-east-1

# 3. If S3 bucket lost, restore from replica
aws s3 sync \
  s3://ocpctl-binaries-replica \
  s3://ocpctl-binaries \
  --source-region us-west-2 \
  --region us-east-1

# 4. Recreate infrastructure
# - VPC, subnets, security groups
# - EC2 instances for API/worker
# - Auto Scaling Groups
# - Load balancers

# 5. Deploy latest release
./scripts/deploy.sh

# 6. Verify all services
curl http://ocpctl.mg.dog8code.com/health
curl http://ocpctl.mg.dog8code.com/version

# 7. Run verification
./scripts/verify-backups.sh
```

**Recovery Time:** 2-4 hours
**Data Loss:** < 24 hours (latest daily backup)

---

## Backup Verification

### Monthly DR Drill Checklist

Perform this drill on the **first Monday of each month**:

```bash
# 1. Verify backups are current
./scripts/verify-backups.sh

# 2. Test RDS snapshot restore (to test instance)
aws rds restore-db-instance-from-db-snapshot \
  --db-instance-identifier ocpctl-dr-drill-$(date +%Y%m%d) \
  --db-snapshot-identifier <latest-snapshot> \
  --db-instance-class db.t3.micro \
  --region us-east-1 \
  --no-publicly-accessible

# 3. Verify restore completed successfully
aws rds describe-db-instances \
  --db-instance-identifier ocpctl-dr-drill-$(date +%Y%m%d) \
  --query 'DBInstances[0].DBInstanceStatus'

# 4. Clean up test instance
aws rds delete-db-instance \
  --db-instance-identifier ocpctl-dr-drill-$(date +%Y%m%d) \
  --skip-final-snapshot

# 5. Document drill results
echo "DR Drill $(date +%Y-%m-%d): SUCCESS" >> docs/dr-drill-log.txt
```

### Automated Monitoring

Set up CloudWatch alarms:

```bash
# Alarm 1: RDS backup age
aws cloudwatch put-metric-alarm \
  --alarm-name ocpctl-rds-backup-age \
  --alarm-description "Alert if RDS backup is older than 25 hours" \
  --metric-name OldestBackup \
  --namespace AWS/RDS \
  --statistic Maximum \
  --period 3600 \
  --evaluation-periods 1 \
  --threshold 90000 \
  --comparison-operator GreaterThanThreshold

# Alarm 2: S3 replication lag
aws cloudwatch put-metric-alarm \
  --alarm-name ocpctl-s3-replication-lag \
  --alarm-description "Alert if S3 replication lag exceeds 1 hour" \
  --metric-name ReplicationLatency \
  --namespace AWS/S3 \
  --statistic Maximum \
  --period 900 \
  --evaluation-periods 4 \
  --threshold 3600000 \
  --comparison-operator GreaterThanThreshold
```

---

## Cost Estimates

### Monthly Backup Costs

| Component | Size | Cost/Month |
|-----------|------|------------|
| RDS automated backups (30 days) | 10 GB | ~$1.00 |
| RDS snapshot storage | 10 GB | ~$0.95 |
| S3 versioning overhead | ~20 GB | ~$0.46 |
| S3 cross-region replication | 100 GB | ~$2.00 |
| S3 replica storage | 100 GB | ~$2.30 |
| **Total** | | **~$6.71** |

**Note:** Costs scale linearly with data volume.

---

## Emergency Contacts

| Role | Contact | Availability |
|------|---------|--------------|
| Primary On-Call | (TBD) | 24/7 |
| Database Admin | (TBD) | Business hours |
| AWS Support | https://console.aws.amazon.com/support | 24/7 (with support plan) |

---

## Appendix: Configuration Details

### RDS Configuration

```bash
# Current settings (check with):
aws rds describe-db-instances \
  --db-instance-identifier ocpctl-production \
  --query 'DBInstances[0].{
    BackupRetention: BackupRetentionPeriod,
    BackupWindow: PreferredBackupWindow,
    DeletionProtection: DeletionProtection,
    Encrypted: StorageEncrypted,
    MultiAZ: MultiAZ
  }'
```

### S3 Bucket Configuration

```bash
# Check versioning:
aws s3api get-bucket-versioning --bucket ocpctl-binaries

# Check replication:
aws s3api get-bucket-replication --bucket ocpctl-binaries

# List recent versions:
aws s3api list-object-versions \
  --bucket ocpctl-binaries \
  --prefix releases/ \
  --max-items 10
```

---

## Change Log

| Date | Change | Author |
|------|--------|--------|
| 2026-03-26 | Initial disaster recovery documentation | System |

