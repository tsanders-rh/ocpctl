# Lock Deadlock Recovery Alerting

OCPCTL includes automatic detection and alerting for stuck job locks that may indicate deadlocked jobs or crashed workers.

## Overview

Job locks are used to ensure only one worker processes a cluster at a time. Under normal circumstances:
1. Worker acquires lock (TTL: 90 minutes)
2. Job completes in 20-60 minutes
3. Lock is released

**Stuck locks** occur when:
- Worker crashes mid-job and fails to release lock
- Job hangs indefinitely
- Network partition prevents lock release

## Detection

The janitor runs stuck lock detection every 5 minutes:

```
Stuck Threshold: 72 minutes (80% of 90-minute TTL)
```

When a lock has been held for ≥72 minutes, it's flagged as stuck and:
1. **Detailed logs** are written with cluster, job, and worker info
2. **CloudWatch metrics** are published for alerting
3. **Recovery steps** are logged for operations teams

## CloudWatch Metrics

### StuckLockDetected

**Type:** Count
**Dimensions:** LockWorker, Platform, Profile, JobType
**Description:** Incremented by 1 for each stuck lock detected

**Example alarm:**
```json
{
  "AlarmName": "StuckLocksDetected",
  "MetricName": "StuckLockDetected",
  "Namespace": "OCPCTL",
  "Statistic": "Sum",
  "Period": 300,
  "EvaluationPeriods": 1,
  "Threshold": 1,
  "ComparisonOperator": "GreaterThanOrEqualToThreshold",
  "TreatMissingData": "notBreaching"
}
```

### LockAgeSeconds

**Type:** Value
**Dimensions:** LockWorker, Platform, Profile, JobType
**Description:** Age of stuck lock in seconds

**Example alarm (locks held > 80 minutes):**
```json
{
  "AlarmName": "LocksHeldTooLong",
  "MetricName": "LockAgeSeconds",
  "Namespace": "OCPCTL",
  "Statistic": "Maximum",
  "Period": 300,
  "EvaluationPeriods": 1,
  "Threshold": 4800,
  "ComparisonOperator": "GreaterThanThreshold"
}
```

### TotalStuckLocks

**Type:** Count
**Dimensions:** None
**Description:** Total number of stuck locks across all workers

**Example alarm (more than 5 stuck locks):**
```json
{
  "AlarmName": "ManyStuckLocks",
  "MetricName": "TotalStuckLocks",
  "Namespace": "OCPCTL",
  "Statistic": "Sum",
  "Period": 300,
  "EvaluationPeriods": 1,
  "Threshold": 5,
  "ComparisonOperator": "GreaterThanThreshold"
}
```

### ExpiredLocksCleanedUp

**Type:** Count
**Dimensions:** None
**Description:** Number of expired locks automatically cleaned up

**Example alarm (frequent lock expiration may indicate worker health issues):**
```json
{
  "AlarmName": "FrequentLockExpiration",
  "MetricName": "ExpiredLocksCleanedUp",
  "Namespace": "OCPCTL",
  "Statistic": "Sum",
  "Period": 300,
  "EvaluationPeriods": 3,
  "Threshold": 10,
  "ComparisonOperator": "GreaterThanThreshold"
}
```

## Log Output

When a stuck lock is detected, logs include:

```
WARNING: Detected 1 stuck locks (held > 72m0s)
STUCK LOCK DETECTED: cluster=abc-123, job=def-456, worker=worker-01, age=75m12s, expires_in=14m48s
  Cluster: name=my-cluster, status=CREATING, platform=aws, profile=small-dev
  Job: type=CREATE, status=RUNNING, attempt=1/3, started=2024-01-15 10:30:00
  RECOVERY: Lock will auto-expire in 14m48s. If job is genuinely stuck, consider:
    1. Check worker logs for worker_id=worker-01
    2. Verify job def4 is actually running (check worker health endpoint)
    3. If worker crashed, lock will auto-expire and job will retry
    4. Manual intervention: DELETE FROM job_locks WHERE cluster_id = 'abc-123'
```

## Alert Response Runbook

### Step 1: Verify the Alert

1. **Check CloudWatch Alarms:**
   ```bash
   aws cloudwatch describe-alarms --alarm-names StuckLocksDetected
   ```

2. **Review janitor logs:**
   ```bash
   kubectl logs -l app=ocpctl-worker -n ocpctl | grep "STUCK LOCK DETECTED"
   ```

3. **Identify affected cluster:**
   - Note the `cluster_id` and `job_id` from logs

### Step 2: Check Worker Health

1. **Verify worker is running:**
   ```bash
   # Check worker health endpoint
   curl http://worker:8081/health

   # Check worker status
   curl http://worker:8081/status
   ```

2. **Check if job is actively processing:**
   ```bash
   # Look for the job in worker status
   curl http://worker:8081/status | jq '.jobs[] | select(.jobId == "def-456")'
   ```

3. **Check worker logs for errors:**
   ```bash
   kubectl logs -l app=ocpctl-worker --tail=1000 | grep "def-456"
   ```

### Step 3: Determine Root Cause

**Common scenarios:**

#### Scenario A: Worker Crashed
**Symptoms:**
- Worker pod restarted recently
- No active job in /status endpoint
- Lock exists but worker_id doesn't match current worker

**Action:** Wait for lock to auto-expire (locks auto-expire after 90 minutes)

#### Scenario B: Job Genuinely Running (Slow)
**Symptoms:**
- Job appears in /status endpoint
- Worker logs show progress
- No errors in logs

**Action:** No intervention needed - some jobs (especially large clusters) take 60+ minutes

#### Scenario C: Job Hung/Deadlocked
**Symptoms:**
- Job in /status endpoint but no recent log output
- Same log line repeated for > 30 minutes
- Worker is healthy but job isn't progressing

**Action:** Manual intervention required (see Step 4)

#### Scenario D: Network Partition
**Symptoms:**
- Worker can't reach database
- Connection timeouts in logs
- Multiple stuck locks across different workers

**Action:** Investigate network connectivity, check RDS security groups

### Step 4: Manual Recovery (If Needed)

**Only perform manual recovery if:**
- Job is confirmed hung/deadlocked
- Worker has crashed and lock won't auto-expire soon
- Urgent cluster creation is needed

#### Option 1: Force Lock Release (Safest)

```sql
-- Connect to database
psql $DATABASE_URL

-- View stuck lock details
SELECT
  cluster_id,
  job_id,
  locked_by,
  locked_at,
  expires_at,
  NOW() - locked_at AS lock_age,
  expires_at - NOW() AS time_until_expiry
FROM job_locks
WHERE locked_at < NOW() - interval '72 minutes';

-- Release specific lock (replace cluster_id)
DELETE FROM job_locks WHERE cluster_id = 'abc-123';
```

**Result:** Job will be picked up by another worker and retried

#### Option 2: Mark Job as Failed

If the job is genuinely stuck and should not retry:

```sql
-- Mark job as failed
UPDATE jobs
SET status = 'FAILED',
    error_code = 'MANUAL_INTERVENTION',
    error_message = 'Job deadlocked, manually failed by operations',
    ended_at = NOW()
WHERE id = 'def-456';

-- Release the lock
DELETE FROM job_locks WHERE job_id = 'def-456';
```

#### Option 3: Restart Worker Pod

If multiple jobs are stuck on the same worker:

```bash
# Identify the problematic worker
kubectl get pods -l app=ocpctl-worker -n ocpctl

# Restart the worker (locks will auto-release, jobs will retry)
kubectl delete pod <worker-pod-name> -n ocpctl
```

**Warning:** Active jobs on this worker will be interrupted and retried

### Step 5: Post-Incident Review

After resolving the stuck lock:

1. **Document root cause** in incident tracker
2. **Review worker logs** for patterns
3. **Check for infrastructure issues** (CPU throttling, OOM, network latency)
4. **Update monitoring thresholds** if needed
5. **Create Jira ticket** if code changes required

## Prevention

### Worker Health Monitoring

Set up proactive monitoring:

1. **Worker health checks:**
   ```bash
   # Kubernetes liveness probe
   livenessProbe:
     httpGet:
       path: /health
       port: 8081
     initialDelaySeconds: 30
     periodSeconds: 10

   # Readiness probe
   readinessProbe:
     httpGet:
       path: /ready
       port: 8081
     initialDelaySeconds: 10
     periodSeconds: 5
   ```

2. **Resource limits:**
   ```yaml
   resources:
     limits:
       memory: "4Gi"
       cpu: "2000m"
     requests:
       memory: "2Gi"
       cpu: "1000m"
   ```

3. **OOM detection:**
   ```bash
   kubectl describe pod <worker-pod> | grep -i oom
   ```

### Database Connection Pooling

Ensure worker has adequate database connections:

```go
// In worker config
MaxConnections: 25
MinConnections: 5
MaxConnLifetime: 30m
```

### Lock Timeout Tuning

If jobs frequently time out, consider increasing lock timeout:

```go
// In worker/worker.go
LockTimeout: 120 * time.Minute  // Increase from 90 to 120 minutes
```

**Note:** Also update stuck lock threshold (80% of new timeout):
```go
// In janitor/janitor.go
stuckThreshold := 96 * time.Minute  // 80% of 120 minutes
```

## Automatic Recovery

The system has built-in automatic recovery:

1. **Stuck job detection** (janitor, every 5 minutes)
   - Detects jobs in RUNNING status for > 90 minutes
   - Marks as FAILED or RETRYING (depending on attempt count)
   - Releases lock

2. **Expired lock cleanup** (janitor, every 5 minutes)
   - Removes locks past their expiry time
   - Publishes metrics

3. **Lock heartbeat** (worker, every 30 seconds)
   - Worker extends lock expiry while job is active
   - If worker crashes, heartbeat stops and lock expires

## Integration with PagerDuty

To send stuck lock alerts to PagerDuty:

1. **Create CloudWatch alarm** (see metrics section above)

2. **Configure SNS topic:**
   ```bash
   aws sns create-topic --name ocpctl-stuck-locks
   aws sns subscribe \
     --topic-arn arn:aws:sns:us-east-1:123456789:ocpctl-stuck-locks \
     --protocol https \
     --notification-endpoint https://events.pagerduty.com/integration/<key>/enqueue
   ```

3. **Link alarm to SNS:**
   ```bash
   aws cloudwatch put-metric-alarm \
     --alarm-name StuckLocksDetected \
     --alarm-actions arn:aws:sns:us-east-1:123456789:ocpctl-stuck-locks \
     ...
   ```

## References

- [Worker Lock Mechanism](../../internal/worker/worker.go#L516)
- [Janitor Stuck Job Detection](../../internal/janitor/janitor.go#L265)
- [Lock Database Schema](../../internal/store/locks.go)
- [CloudWatch Metrics](./METRICS.md)
- [Troubleshooting Runbooks](./TROUBLESHOOTING.md)
