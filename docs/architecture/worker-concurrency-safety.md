# Worker Concurrency Safety and Locking Strategy

## Overview

The worker service must guarantee that **only one job operates on a cluster at any given time**. This is critical because:

1. **State corruption**: Multiple workers modifying the same install directory simultaneously
2. **Cloud API conflicts**: Parallel creates/destroys causing resource conflicts
3. **Database inconsistency**: Race conditions updating cluster status

This document defines the locking strategy and safety guarantees for worker operations.

---

## Threat Model

### Scenarios to Prevent

1. **Concurrent Destroys**
   - User initiates destroy while janitor TTL destroy is running
   - Two workers both download install-dir snapshot and run destroy concurrently
   - Result: Cloud API errors, partial cleanup

2. **Create During Destroy**
   - User recreates cluster (same name) while destroy is still in progress
   - New create job starts before old resources are cleaned up
   - Result: Name conflicts, orphaned resources

3. **Worker Crash Recovery**
   - Worker crashes mid-operation, lock remains held
   - Other workers cannot process cluster jobs
   - Result: Deadlock, cluster stuck in CREATING state

4. **Clock Skew**
   - Different workers have different system clocks
   - Lock expiry checks produce inconsistent results
   - Result: Multiple workers claim lock

---

## Locking Strategy

### 1. Database-Level Job Locks

**Primary mechanism**: `job_locks` table with cluster-scoped advisory locks

```sql
CREATE TABLE job_locks (
    cluster_id VARCHAR(64) PRIMARY KEY REFERENCES clusters(id) ON DELETE CASCADE,
    job_id VARCHAR(64) NOT NULL REFERENCES jobs(id) ON DELETE CASCADE,
    locked_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    locked_by VARCHAR(255) NOT NULL, -- worker instance ID
    expires_at TIMESTAMP WITH TIME ZONE NOT NULL
);
```

**Lock acquisition algorithm**:

```
1. BEGIN TRANSACTION
2. SELECT FOR UPDATE on clusters WHERE id = $cluster_id
3. Check cluster status is valid for job type:
   - CREATE: status must be PENDING
   - DESTROY: status must be READY or FAILED
   - SCALE_WORKERS: status must be READY
4. INSERT into job_locks (cluster_id, job_id, locked_by, expires_at)
   - ON CONFLICT (cluster_id) DO NOTHING
5. If INSERT succeeded:
   - Update job status to RUNNING
   - Update cluster status to appropriate IN_PROGRESS state
   - COMMIT
   - Return SUCCESS
6. If INSERT failed (lock held by another worker):
   - ROLLBACK
   - Return LOCK_HELD_BY_OTHER_WORKER
```

**Lock release**:

```
1. DELETE FROM job_locks WHERE cluster_id = $cluster_id AND job_id = $job_id
2. Update cluster status to final state (READY, DESTROYED, FAILED)
3. Update job status to final state (SUCCEEDED, FAILED)
```

### 2. Lock Expiry and Staleness

**Problem**: Worker crashes while holding lock

**Solution**: Time-based lock expiry with heartbeat

- Lock TTL: 60 minutes (longer than any expected operation)
- Worker heartbeat interval: 5 minutes
- Heartbeat updates `expires_at` to NOW() + 60 minutes

**Stale lock cleanup** (janitor service):

```sql
DELETE FROM job_locks
WHERE expires_at < NOW() - INTERVAL '5 minutes'
RETURNING cluster_id, job_id, locked_by;

-- For each returned lock:
-- 1. Mark job as FAILED with error_code = 'WORKER_TIMEOUT'
-- 2. Log incident for investigation
-- 3. Optionally trigger retry with new job
```

### 3. PostgreSQL Advisory Locks

**Supplementary mechanism** for extra safety during critical sections:

```sql
-- Acquire advisory lock (blocks until available)
SELECT pg_advisory_lock(hashtext($cluster_id));

-- Perform critical operation
-- ...

-- Release advisory lock
SELECT pg_advisory_unlock(hashtext($cluster_id));
```

**When to use**:
- Before downloading/uploading S3 artifacts
- Before running openshift-install
- During cluster status transitions

**Benefit**: Kernel-level lock, automatically released on connection close

---

## Worker Job Processing Flow

### Job Dequeue with Lock Acquisition

```go
func (w *Worker) DequeueAndLock() (*Job, error) {
    // 1. Receive message from SQS (with visibility timeout = 10 minutes)
    sqsMsg, err := w.sqs.ReceiveMessage(ctx)
    if err != nil {
        return nil, err
    }

    job := parseJob(sqsMsg.Body)

    // 2. Attempt to acquire cluster lock
    tx, err := w.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
    if err != nil {
        w.sqs.ChangeVisibility(sqsMsg, 0) // Make visible again immediately
        return nil, err
    }

    // 3. Acquire database lock
    locked, err := w.acquireLock(tx, job.ClusterID, job.ID)
    if err != nil || !locked {
        tx.Rollback()
        w.sqs.ChangeVisibility(sqsMsg, 0)
        return nil, ErrLockHeld
    }

    // 4. Update job and cluster status
    err = w.updateJobStatus(tx, job.ID, StatusRunning)
    if err != nil {
        tx.Rollback()
        w.sqs.ChangeVisibility(sqsMsg, 0)
        return nil, err
    }

    err = tx.Commit()
    if err != nil {
        w.sqs.ChangeVisibility(sqsMsg, 0)
        return nil, err
    }

    // 5. Job locked successfully, delete from SQS
    w.sqs.DeleteMessage(sqsMsg)

    // 6. Start heartbeat goroutine
    go w.maintainLock(job.ClusterID, job.ID)

    return job, nil
}
```

### Lock Heartbeat

```go
func (w *Worker) maintainLock(clusterID, jobID string) {
    ticker := time.NewTicker(5 * time.Minute)
    defer ticker.Stop()

    for {
        select {
        case <-ticker.C:
            err := w.updateLockExpiry(clusterID, jobID)
            if err != nil {
                log.Error("Failed to update lock heartbeat",
                    "cluster_id", clusterID,
                    "job_id", jobID,
                    "error", err)
            }
        case <-w.jobCompleteChan:
            return
        }
    }
}

func (w *Worker) updateLockExpiry(clusterID, jobID string) error {
    _, err := w.db.Exec(`
        UPDATE job_locks
        SET expires_at = NOW() + INTERVAL '60 minutes'
        WHERE cluster_id = $1 AND job_id = $2
    `, clusterID, jobID)
    return err
}
```

---

## SQS Queue Configuration

### FIFO Queue for Cluster-Scoped Ordering

**Queue name**: `ocpctl-jobs.fifo`

**Configuration**:
- Message group ID: `cluster-{clusterID}`
- Content-based deduplication: Enabled
- Visibility timeout: 10 minutes
- Default message delay: 0 seconds
- Receive message wait time: 20 seconds (long polling)

**Rationale**:
- FIFO queue ensures jobs for the same cluster are processed in order
- Message group ID = cluster ID ensures no parallel processing of same cluster
- Visibility timeout gives worker time to acquire lock before message becomes visible again

### Dead Letter Queue

**DLQ name**: `ocpctl-jobs-dlq.fifo`

**Configuration**:
- Max receive count: 3 (after 3 failed lock acquisitions, move to DLQ)
- DLQ retention: 14 days

**DLQ processing**:
- Janitor service monitors DLQ
- Failed jobs are analyzed and optionally retried
- Human operator alert for jobs stuck in DLQ > 1 hour

---

## State Transition Safety

### Valid State Transitions

```
CREATE job:
  PENDING -> CREATING -> READY
  PENDING -> CREATING -> FAILED

DESTROY job:
  READY -> DESTROYING -> DESTROYED
  FAILED -> DESTROYING -> DESTROYED
  READY -> DESTROYING -> FAILED

SCALE_WORKERS job:
  READY -> SCALING -> READY
  READY -> SCALING -> FAILED

JANITOR_DESTROY job:
  READY -> DESTROYING -> DESTROYED
  (Same as manual destroy)
```

### Enforced Constraints

1. **Cannot create if status != PENDING**
   - Prevents recreate during destroy

2. **Cannot destroy if status = CREATING or DESTROYING**
   - Prevents concurrent destroys

3. **Cannot scale if status != READY**
   - Prevents scaling during create/destroy

4. **Name uniqueness constraint**
   ```sql
   CREATE UNIQUE INDEX idx_unique_active_cluster
   ON clusters(name, platform, base_domain)
   WHERE status NOT IN ('DESTROYED', 'FAILED');
   ```

---

## Idempotency and Retry Safety

### Idempotent Job Execution

All job handlers must be idempotent:

1. **CREATE job**:
   - Check if cluster already exists with matching name
   - If exists and READY: return success (already created)
   - If exists and CREATING: check if our job_id matches, continue if yes

2. **DESTROY job**:
   - Check if cluster status = DESTROYED
   - If yes: verify cloud resources are gone, return success
   - If resources exist: proceed with destroy

3. **SCALE_WORKERS job**:
   - Check current worker count
   - If already at target: return success
   - Otherwise: proceed with scaling

### Retry Strategy

**Transient failures** (retry automatically):
- Cloud API throttling (429)
- Network timeouts
- S3 access errors

**Permanent failures** (do not retry):
- Invalid install-config.yaml
- Cloud quota exceeded
- Invalid credentials

**Retry configuration**:
```go
maxAttempts := 3
backoff := exponential.New(
    initialInterval: 5 * time.Minute,
    multiplier: 2.0,
    maxInterval: 30 * time.Minute,
)
```

---

## Disaster Recovery

### Lost Lock State

**Scenario**: Database crash, job_locks table data loss

**Detection**:
- Workers query job_locks on startup
- Find jobs with status = RUNNING but no corresponding lock
- Log discrepancy

**Recovery**:
1. Janitor identifies RUNNING jobs without locks
2. Mark jobs as FAILED with error_code = 'LOCK_STATE_LOST'
3. Set cluster status to FAILED
4. Alert operator for manual investigation

### Multiple Workers Claim Lock (Byzantine Failure)

**Scenario**: Database replication lag causes two workers to think they have lock

**Prevention**:
- Use PostgreSQL serializable isolation level for lock acquisition
- Single primary database (no multi-master)
- Read-after-write consistency required

**Detection**:
- Workers log all lock acquisitions with correlation IDs
- Audit log monitoring detects duplicate lock_acquired events for same cluster

**Mitigation**:
- If detected: kill both worker processes
- Mark job FAILED
- Operator investigates and triggers manual retry

---

## Monitoring and Alerting

### Critical Metrics

```
# Lock acquisition failures
ocpctl_lock_acquisition_failures_total{reason}

# Lock held duration
ocpctl_lock_held_duration_seconds{job_type}

# Stale locks cleaned up
ocpctl_stale_locks_cleaned_total

# Jobs stuck in RUNNING state
ocpctl_jobs_running_duration_seconds{job_type}
```

### Alerts

1. **Stale Lock Detected**
   - Trigger: Lock expires_at < NOW() - 5 minutes
   - Severity: Warning
   - Action: Janitor cleans up, marks job failed

2. **Job Running Too Long**
   - Trigger: Job in RUNNING state > 90 minutes
   - Severity: Critical
   - Action: Operator investigates worker health

3. **High Lock Contention**
   - Trigger: lock_acquisition_failures > 10/min
   - Severity: Warning
   - Action: Check worker scaling, investigate queue depth

---

## Testing Strategy

### Chaos Engineering Tests

1. **Worker Crash During Create**
   - Start create job
   - Kill worker at random point
   - Verify lock expires and janitor cleans up
   - Verify retry succeeds

2. **Concurrent Destroy Attempts**
   - Enqueue 3 destroy jobs for same cluster
   - Verify only one acquires lock
   - Verify other 2 fail gracefully

3. **Database Connection Loss**
   - Worker holds lock
   - Disconnect database
   - Verify advisory lock is released
   - Verify heartbeat fails and lock expires

### Integration Tests

```go
func TestLockAcquisition_Concurrent(t *testing.T) {
    // Setup: Create cluster in READY state
    cluster := createTestCluster(t, StatusReady)

    // Enqueue 2 destroy jobs
    job1 := enqueueJob(t, cluster.ID, JobTypeDestroy)
    job2 := enqueueJob(t, cluster.ID, JobTypeDestroy)

    // Start 2 workers
    worker1 := NewWorker(config)
    worker2 := NewWorker(config)

    var wg sync.WaitGroup
    wg.Add(2)

    var acquiredCount int32

    go func() {
        defer wg.Done()
        if worker1.DequeueAndLock() != nil {
            atomic.AddInt32(&acquiredCount, 1)
        }
    }()

    go func() {
        defer wg.Done()
        if worker2.DequeueAndLock() != nil {
            atomic.AddInt32(&acquiredCount, 1)
        }
    }()

    wg.Wait()

    // Assert: Only 1 worker acquired lock
    assert.Equal(t, int32(1), atomic.LoadInt32(&acquiredCount))
}
```

---

## Summary

**Lock Hierarchy** (from strongest to weakest):
1. PostgreSQL advisory locks (pg_advisory_lock)
2. Database row locks (job_locks table with SELECT FOR UPDATE)
3. SQS FIFO message groups (prevents parallel queue processing)
4. Optimistic locking with status checks

**Safety Guarantees**:
- ✅ Only one worker operates on a cluster at a time
- ✅ Crashed workers release locks via expiry
- ✅ Concurrent job submissions are serialized
- ✅ State transitions are validated and atomic
- ✅ Retries are idempotent and safe

**Trade-offs**:
- **Throughput**: Lower than fully parallel (acceptable for cluster operations)
- **Complexity**: More moving parts to monitor
- **Failure modes**: Requires careful testing and monitoring

This locking strategy prioritizes **correctness and safety over performance**, which is appropriate for cluster lifecycle operations where mistakes are expensive.
