# Resource Tagging Operations Guide

This guide provides operational procedures for managing AWS resource tagging in ocpctl production environments.

## Table of Contents

- [Overview](#overview)
- [Deployment Checklist](#deployment-checklist)
- [Monitoring](#monitoring)
- [Troubleshooting](#troubleshooting)
- [Maintenance](#maintenance)
- [Runbooks](#runbooks)

## Overview

### What Is Resource Tagging?

Resource tagging is a Phase 2 feature that automatically applies metadata tags to ALL AWS resources created by OpenShift clusters:

- **Purpose**: Enable accurate orphan detection, cost attribution, and automated cleanup
- **Timing**: Tags applied post-cluster-creation (~30-45 min after cluster ready)
- **Scope**: EC2, ELB, Route53, S3, IAM resources
- **Tag Key**: `ManagedBy=ocpctl` (primary identifier)

### Architecture

```
┌──────────────┐
│    Worker    │
│   Service    │
└──────┬───────┘
       │
       │ After cluster creation completes
       │
       ▼
┌─────────────────────────────────────────────┐
│      TagAWSResources() - Parallel Exec      │
├─────────────────────────────────────────────┤
│                                             │
│  ┌─────────┐  ┌─────┐  ┌────────┐  ┌────┐ │
│  │   EC2   │  │ ELB │  │Route53 │  │ S3 │ │
│  └─────────┘  └─────┘  └────────┘  └────┘ │
│       │         │          │          │    │
│       └─────────┴──────────┴──────────┘    │
│                    │                        │
│                    ▼                        │
│           ┌──────────────┐                 │
│           │ Aggregate    │                 │
│           │ Errors       │                 │
│           └──────────────┘                 │
└─────────────────────────────────────────────┘
```

**Total execution time**: ~5 seconds (parallel)
**Failure mode**: Non-blocking (cluster creation succeeds even if tagging fails)

## Deployment Checklist

### Pre-Deployment

- [ ] **Verify IAM permissions** - Ensure tagging permissions are applied
- [ ] **Test in dev environment** - Create test cluster and verify tags
- [ ] **Review existing clusters** - Identify clusters needing retroactive tagging
- [ ] **Plan retroactive tagging** - Schedule bulk tagging operation
- [ ] **Update runbooks** - Ensure ops team has tagging procedures

### IAM Permission Verification

```bash
# On worker instance, verify permissions
aws ec2 describe-vpcs --max-results 1
aws elbv2 describe-load-balancers --max-results 1
aws route53 list-hosted-zones --max-items 1
aws s3api list-buckets
aws iam list-roles --max-items 1

# All should return successfully (not AccessDenied)
```

If any fail, apply IAM policy:

```bash
aws iam attach-role-policy \
  --role-name ocpctl-worker-role \
  --policy-arn arn:aws:iam::<account-id>:policy/OCPCTLTaggingPolicy
```

### Post-Deployment Verification

**1. Create test cluster:**

```bash
curl -X POST https://ocpctl.example.com/api/v1/clusters \
  -H "Authorization: Bearer $TOKEN" \
  -d '{
    "name":"tagging-test-001",
    "profile":"aws-sno-test",
    "version":"4.20.3"
  }'
```

**2. Wait for cluster creation (~30 min)**

**3. Verify tags in AWS console:**

Navigate to EC2 → VPCs → Select cluster VPC → Tags

**Expected tags:**
```
ManagedBy: ocpctl
ClusterName: tagging-test-001
Profile: aws-sno-test
InfraID: tagging-test-001-xxxxx
CreatedAt: 2024-03-17T10:30:00Z
OcpctlVersion: v0.20260317.bca1feb
kubernetes.io/cluster/tagging-test-001-xxxxx: owned
```

**4. Check worker logs:**

```bash
journalctl -u ocpctl-worker -n 200 | grep -i tag

# Look for:
# "Tagging all AWS resources for cluster tagging-test-001..."
# "[TagAWSResources] ✓ EC2 resources tagged"
# "[TagAWSResources] ✓ Load balancers tagged"
# "[TagAWSResources] ✓ Route53 zones tagged"
# "[TagAWSResources] ✓ S3 buckets tagged"
# "[TagAWSResources] ✓ IAM/OIDC resources tagged"
# "[TagAWSResources] ✓ All AWS resources tagged successfully"
```

**5. Destroy test cluster:**

```bash
# Via API
curl -X DELETE https://ocpctl.example.com/api/v1/clusters/<cluster-id> \
  -H "Authorization: Bearer $TOKEN"

# Verify cleanup in AWS console after 15-20 min
```

## Monitoring

### Health Metrics

**Key metrics to monitor:**

1. **Tagging success rate**: Should be >99%
2. **Tagging duration**: Should be <10 seconds
3. **IAM permission errors**: Should be 0
4. **Orphan detection accuracy**: False positive rate should be 0%

### Log Monitoring

**Grep patterns for alerts:**

```bash
# Failed tagging (investigate)
journalctl -u ocpctl-worker -f | grep "failed to tag AWS resources"

# Permission errors (urgent - fix IAM)
journalctl -u ocpctl-worker -f | grep "AccessDenied"

# Orphan detection (informational)
journalctl -u ocpctl-worker -f | grep "Found.*orphaned AWS resources"
```

### Worker Service Health

```bash
# Check worker is running
systemctl status ocpctl-worker

# Check health endpoint
curl http://localhost:8081/health

# Check readiness (DB connectivity)
curl http://localhost:8081/ready
```

### Database Queries

**Count orphaned resources:**

```sql
SELECT resource_type, COUNT(*)
FROM orphaned_resources
WHERE status = 'active'
GROUP BY resource_type;
```

**List recently tagged clusters:**

```sql
SELECT
  c.name,
  c.created_at,
  c.status,
  j.completed_at AS tagging_completed
FROM clusters c
LEFT JOIN jobs j ON j.cluster_id = c.id AND j.job_type = 'create'
WHERE c.created_at > NOW() - INTERVAL '24 hours'
ORDER BY c.created_at DESC;
```

**Find clusters without tags (need retroactive tagging):**

```sql
-- Query orphaned_resources for VPCs without ManagedBy tag
SELECT DISTINCT cluster_name
FROM orphaned_resources
WHERE resource_type = 'VPC'
  AND tags->>'ManagedBy' IS NULL
  AND status = 'active';
```

## Troubleshooting

### Common Issues

#### 1. Tags Not Applied

**Symptoms:**
- Cluster created successfully
- AWS resources exist but don't have `ManagedBy=ocpctl` tag
- Worker logs show tagging warnings

**Root Causes:**
- IAM permissions not configured
- AWS API rate limiting
- Network connectivity issues

**Resolution:**

```bash
# Check worker logs for specific error
journalctl -u ocpctl-worker -n 500 | grep -A 10 "failed to tag"

# If AccessDenied, fix IAM permissions
aws iam attach-role-policy \
  --role-name ocpctl-worker-role \
  --policy-arn arn:aws:iam::<account-id>:policy/OCPCTLTaggingPolicy

# Restart worker service
systemctl restart ocpctl-worker

# Apply tags retroactively
./bin/tag-aws-resources -name <cluster-name>
```

#### 2. Orphan Detection False Positives

**Symptoms:**
- Resources flagged as orphaned that belong to active clusters
- Resources from non-ocpctl clusters flagged as orphaned

**Root Causes:**
- Database cluster record missing
- Cluster marked as destroyed but resources still exist
- Non-ocpctl resources have `ManagedBy=ocpctl` tag (user error)

**Resolution:**

```bash
# Check if cluster exists in database
psql -d ocpctl -c "SELECT name, status FROM clusters WHERE name='<cluster-name>';"

# If cluster should be active but shows destroyed:
psql -d ocpctl -c "UPDATE clusters SET status='ready' WHERE name='<cluster-name>';"

# If resource shouldn't have ManagedBy tag, remove it:
aws ec2 delete-tags \
  --resources <resource-id> \
  --tags Key=ManagedBy,Value=ocpctl
```

#### 3. Retroactive Tagging Fails

**Symptoms:**
- Tool errors with "No VPC found with kubernetes.io/cluster/<infraID> tag"
- Tool can't find cluster in database

**Root Causes:**
- Cluster deleted from AWS but still in database
- Wrong region specified
- Cluster name typo

**Resolution:**

```bash
# Verify cluster exists in AWS
aws ec2 describe-vpcs \
  --filters "Name=tag:Name,Values=*<cluster-name>*" \
  --region us-east-1

# Specify correct region
./bin/tag-aws-resources -name <cluster-name> -region us-west-2

# Use cluster ID instead of name
./bin/tag-aws-resources -id <cluster-uuid>

# If cluster truly gone, mark as destroyed in database:
psql -d ocpctl -c "UPDATE clusters SET status='destroyed' WHERE name='<cluster-name>';"
```

#### 4. High Orphan Count

**Symptoms:**
- Sudden spike in orphaned resources reported
- Orphan count growing over time

**Root Causes:**
- Cluster destruction failures (most common)
- Database cleanup issues
- Manual resource deletion

**Resolution:**

```bash
# List orphaned resources by cluster
psql -d ocpctl -c "
SELECT cluster_name, resource_type, COUNT(*)
FROM orphaned_resources
WHERE status='active'
GROUP BY cluster_name, resource_type
ORDER BY COUNT(*) DESC;
"

# Investigate specific cluster
psql -d ocpctl -c "
SELECT * FROM clusters WHERE name='<cluster-name>';
"

# If cluster destroy failed, retry via API:
curl -X DELETE https://ocpctl.example.com/api/v1/clusters/<cluster-id> \
  -H "Authorization: Bearer $TOKEN"

# Or manually clean up AWS resources:
cd /tmp/ocpctl/<cluster-id>
openshift-install destroy cluster --dir .
```

### Emergency Procedures

#### Stop Orphan Detection

If orphan detection is causing issues (false positives, performance):

```bash
# Temporarily disable janitor by stopping worker
systemctl stop ocpctl-worker

# Edit worker config to disable orphan detection
# (No config flag exists yet - requires code change)

# Alternative: Delete all orphaned resource records
psql -d ocpctl -c "DELETE FROM orphaned_resources WHERE status='active';"

# Restart worker
systemctl start ocpctl-worker
```

#### Bulk Tag All Clusters

If many clusters need retroactive tagging:

```bash
#!/bin/bash
# bulk-tag-clusters.sh

# Get all active cluster names
psql -d ocpctl -t -A -c "SELECT name FROM clusters WHERE status='ready';" | while read cluster_name; do
  echo "========================================="
  echo "Tagging cluster: $cluster_name"
  echo "========================================="

  # Tag cluster (with error handling)
  if ./bin/tag-aws-resources -name "$cluster_name"; then
    echo "✓ Successfully tagged $cluster_name"
  else
    echo "✗ Failed to tag $cluster_name - see errors above"
  fi

  echo ""

  # Rate limiting - wait 2 seconds between clusters
  sleep 2
done

echo "Bulk tagging complete"
```

Run in screen/tmux session:

```bash
screen -S bulk-tagging
./bulk-tag-clusters.sh 2>&1 | tee bulk-tag-$(date +%Y%m%d).log
# Ctrl+A, D to detach
```

#### Force Cleanup Orphaned Resources

**⚠️ DANGER: This permanently deletes AWS resources**

Only use if:
- Resource is confirmed orphaned (cluster truly deleted)
- No data loss will occur
- You have management approval

```bash
# Manual cleanup of specific orphaned VPC
aws ec2 delete-vpc --vpc-id vpc-0123456789abcdef0

# Automated cleanup script (use with extreme caution)
#!/bin/bash
# cleanup-orphaned-vpcs.sh

psql -d ocpctl -t -A -c "
SELECT resource_id FROM orphaned_resources
WHERE resource_type='VPC'
  AND status='active'
  AND first_detected_at < NOW() - INTERVAL '48 hours';
" | while read vpc_id; do
  echo "Deleting orphaned VPC: $vpc_id"

  # Delete VPC dependencies first
  # ... (complex logic - see AWS docs)

  # Delete VPC
  aws ec2 delete-vpc --vpc-id "$vpc_id"

  # Mark as resolved in database
  psql -d ocpctl -c "
  UPDATE orphaned_resources
  SET status='resolved',
      resolved_by='automated-cleanup',
      resolved_at=NOW(),
      notes='Automatically deleted after 48h orphan period'
  WHERE resource_id='$vpc_id';
  "
done
```

## Maintenance

### Weekly Tasks

**Monday Morning:**

```bash
# Review orphaned resources
psql -d ocpctl -c "
SELECT resource_type, COUNT(*), MIN(first_detected_at) AS oldest
FROM orphaned_resources
WHERE status='active'
GROUP BY resource_type;
"

# Check tagging success rate (last 7 days)
psql -d ocpctl -c "
SELECT
  COUNT(*) FILTER (WHERE status='completed') AS successful,
  COUNT(*) FILTER (WHERE status='failed') AS failed,
  ROUND(100.0 * COUNT(*) FILTER (WHERE status='completed') / COUNT(*), 2) AS success_rate
FROM jobs
WHERE job_type='create'
  AND created_at > NOW() - INTERVAL '7 days';
"
```

**Action items:**
- Investigate orphaned resources with >7 day age
- Review tagging failures (success rate should be >99%)
- Clean up resolved orphaned resources from database

### Monthly Tasks

**First Monday of Month:**

```bash
# Generate cost report by cluster
# (Use AWS Cost Explorer with ClusterName tag filter)

# Review tag compliance
# (All resources should have ManagedBy=ocpctl tag)

# Audit orphan cleanup
# (Review all manually cleaned up resources)
```

**Last Friday of Month:**

```bash
# Archive old orphaned resource records
psql -d ocpctl -c "
DELETE FROM orphaned_resources
WHERE status='resolved'
  AND resolved_at < NOW() - INTERVAL '90 days';
"

# Review resource tagging policy
# (Check if any new AWS services need tagging support)
```

### Quarterly Tasks

- Review IAM permissions (principle of least privilege)
- Performance test bulk tagging on 100+ clusters
- Update documentation with lessons learned
- Validate disaster recovery procedures

## Runbooks

### Runbook: New Cluster Not Tagged

**Trigger:** Cluster created successfully but tags missing from AWS resources

**Steps:**

1. **Verify cluster status:**
   ```bash
   curl https://ocpctl.example.com/api/v1/clusters/<cluster-id> \
     -H "Authorization: Bearer $TOKEN"

   # Should show status: "ready"
   ```

2. **Check worker logs:**
   ```bash
   journalctl -u ocpctl-worker -n 500 | grep -i "<cluster-name>"

   # Look for tagging errors
   ```

3. **Verify IAM permissions:**
   ```bash
   # On worker instance
   aws ec2 describe-vpcs --max-results 1
   ```

4. **Apply tags retroactively:**
   ```bash
   ./bin/tag-aws-resources -name <cluster-name>
   ```

5. **Verify tags applied:**
   ```bash
   # Check VPC in AWS console
   # Verify ManagedBy=ocpctl tag exists
   ```

6. **Document incident:**
   - Create GitHub issue if IAM permissions were missing
   - Update deployment checklist if needed

### Runbook: Orphan Detection False Positive

**Trigger:** Resource flagged as orphaned but belongs to active cluster

**Steps:**

1. **Verify cluster exists and is active:**
   ```bash
   psql -d ocpctl -c "
   SELECT name, status, created_at
   FROM clusters
   WHERE name='<cluster-name>';
   "
   ```

2. **Check resource tags in AWS:**
   ```bash
   aws ec2 describe-vpcs \
     --vpc-ids <vpc-id> \
     --query 'Vpcs[0].Tags'
   ```

3. **If cluster is active:**
   - Mark orphan as resolved:
     ```bash
     psql -d ocpctl -c "
     UPDATE orphaned_resources
     SET status='resolved',
         resolved_by='ops-team',
         notes='False positive - cluster is active'
     WHERE resource_id='<resource-id>';
     "
     ```

4. **If cluster should be destroyed:**
   - Trigger cluster destruction:
     ```bash
     curl -X DELETE https://ocpctl.example.com/api/v1/clusters/<cluster-id> \
       -H "Authorization: Bearer $TOKEN"
     ```

5. **Root cause analysis:**
   - Why was it flagged? (Missing tag, DB inconsistency?)
   - How to prevent? (Better validation, improved tagging?)

### Runbook: Bulk Retroactive Tagging

**Trigger:** Multiple clusters need retroactive tagging (e.g., after ocpctl upgrade)

**Steps:**

1. **Identify clusters needing tagging:**
   ```bash
   psql -d ocpctl -t -A -c "
   SELECT name FROM clusters
   WHERE status IN ('ready', 'hibernating')
     AND created_at < '<date-of-phase2-deployment>';
   " > clusters-to-tag.txt
   ```

2. **Dry run test:**
   ```bash
   head -5 clusters-to-tag.txt | while read cluster_name; do
     ./bin/tag-aws-resources -name "$cluster_name" -dry-run
   done
   ```

3. **Start bulk tagging:**
   ```bash
   screen -S bulk-tagging
   cat clusters-to-tag.txt | while read cluster_name; do
     echo "Tagging: $cluster_name"
     if ./bin/tag-aws-resources -name "$cluster_name"; then
       echo "✓ $cluster_name" >> success.log
     else
       echo "✗ $cluster_name" >> failed.log
     fi
     sleep 2  # Rate limiting
   done
   # Ctrl+A, D to detach
   ```

4. **Monitor progress:**
   ```bash
   screen -r bulk-tagging

   # In another terminal:
   watch -n 30 "echo 'Success:' $(wc -l < success.log); echo 'Failed:' $(wc -l < failed.log)"
   ```

5. **Review failures:**
   ```bash
   cat failed.log

   # Investigate each failure
   # Retry with manual intervention if needed
   ```

6. **Verify completion:**
   ```bash
   # Count: should match initial cluster list
   total=$(wc -l < clusters-to-tag.txt)
   success=$(wc -l < success.log)
   failed=$(wc -l < failed.log)

   echo "Total: $total, Success: $success, Failed: $failed"
   ```

## Reference

### AWS API Rate Limits

| Service | Rate Limit | Tagging Limit | Notes |
|---------|-----------|---------------|-------|
| EC2 | 2,000 req/sec | 50 CreateTags/sec | Batch up to 1000 resources |
| ELB | 20 req/sec | 10 AddTags/sec | Batch up to 20 LBs |
| Route53 | 5 req/sec | 1 req/sec per zone | No batch support |
| S3 | 3,500 PUT/sec | 100 req/sec per bucket | Per-bucket tagging |
| IAM | 20 req/sec | 10 TagRole/sec | Global service |

### Support Contacts

- **AWS Support**: [AWS Premium Support](https://console.aws.amazon.com/support/)
- **Internal Team**: ocpctl-team@example.com
- **Oncall**: PagerDuty escalation policy
- **Documentation**: [GitHub Wiki](https://github.com/tsanders-rh/ocpctl/wiki)

### Related Documentation

- [AWS Resource Management Guide](../user-guide/aws-resource-management.md)
- [IAM Setup Guide](../../deploy/IAM-SETUP.md)
- [Retroactive Tagging Tool](../../cmd/tag-aws-resources/README.md)
- [Orphaned Resource Admin Guide](../user-guide/orphaned-resources.md)

---

**Last Updated:** March 17, 2024
**Maintained By:** OCPCTL Operations Team
**Review Cycle:** Quarterly
