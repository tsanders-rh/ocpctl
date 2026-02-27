# Implementation Guide

Quick reference for implementing the OpenShift Cluster Control Plane based on the resolved critical design items.

---

## Critical Design Documents (Read These First)

| Document | Purpose | Priority |
|----------|---------|----------|
| [design-specification.md](design-specification.md) | Complete product requirements | 🔴 MUST READ |
| [architecture.md](architecture.md) | System architecture overview | 🔴 MUST READ |
| [CRITICAL-ITEMS-RESOLVED.md](CRITICAL-ITEMS-RESOLVED.md) | Resolved blocking issues | 🔴 MUST READ |
| [worker-concurrency-safety.md](worker-concurrency-safety.md) | Worker locking strategy | 🟡 READ BEFORE WORKER |
| [api.yaml](api.yaml) | OpenAPI specification | 🟡 READ BEFORE API |

---

## Database Schema Reference

**Migration file**: `internal/store/migrations/00001_initial_schema.sql`

### Core Tables

| Table | Purpose | Key Columns |
|-------|---------|-------------|
| `clusters` | Cluster inventory | id, name, platform, status, destroy_at |
| `jobs` | Async job tracking | id, cluster_id, job_type, status, attempt |
| `job_locks` | Worker concurrency control | cluster_id, job_id, locked_by, expires_at |
| `cluster_outputs` | Access credentials | cluster_id, api_url, console_url, kubeconfig_s3_uri |
| `cluster_artifacts` | S3 state snapshots | cluster_id, artifact_type, s3_uri, checksum |
| `audit_events` | Immutable audit trail | actor, action, target_cluster_id, created_at |
| `idempotency_keys` | Duplicate request prevention | key, request_hash, response_body, expires_at |
| `rbac_mappings` | IAM to team/role mapping | iam_principal_arn, team, role, enabled |
| `usage_samples` | Cost tracking | cluster_id, sample_time, estimated_hourly_cost |

### Running Migrations

```bash
# Start PostgreSQL
make docker-up

# Run migrations
export DATABASE_URL="postgres://ocpctl:ocpctl-dev-password@localhost:5432/ocpctl?sslmode=disable"
make migrate-up

# Verify tables
psql $DATABASE_URL -c "\dt"
```

### Key Constraints to Remember

1. **Unique active cluster name**:
   ```sql
   CREATE UNIQUE INDEX idx_unique_active_cluster
   ON clusters(name, platform, base_domain)
   WHERE status NOT IN ('DESTROYED', 'FAILED');
   ```

2. **Job lock is cluster-scoped**:
   ```sql
   cluster_id VARCHAR(64) PRIMARY KEY
   ```

3. **Idempotency keys expire after 24 hours**

---

## Cluster Profile System

**Location**: `internal/profile/definitions/`

### Profile Files

- `aws-minimal-test.yaml` - 3 control plane, 0 workers, 72h max TTL
- `aws-standard.yaml` - 3 control plane, 3 workers, 168h max TTL
- `ibm-minimal-test.yaml` - IBM Cloud (disabled in Phase 1)
- `ibm-standard.yaml` - IBM Cloud (disabled in Phase 1)

### Profile Schema

See `internal/profile/definitions/SCHEMA.md` for complete schema documentation.

### Key Profile Fields

```yaml
name: aws-minimal-test
platform: aws
enabled: true

openshiftVersions:
  allowlist: ["4.20.3", "4.20.4"]
  default: "4.20.3"

compute:
  controlPlane:
    replicas: 3
    instanceType: m6i.xlarge
    schedulable: true
  workers:
    replicas: 0
    minReplicas: 0
    maxReplicas: 3

lifecycle:
  maxTTLHours: 72
  defaultTTLHours: 24
  allowCustomTTL: true

costControls:
  estimatedHourlyCost: 2.50
```

### Using Profiles in Code

```go
// Pseudo-code for profile loading
profile, err := profileLoader.Load("aws-minimal-test")
if err != nil {
    return err
}

// Validate request against profile
err = profile.Validate(request)
if err != nil {
    return ValidationError{Errors: err}
}

// Render install-config.yaml
installConfig, err := profile.RenderInstallConfig(request)
```

---

## API Implementation

**OpenAPI Spec**: `docs/api.yaml`

### Authentication Flow

```
1. User authenticates via AWS IAM Identity Center
2. Web UI receives session cookie
3. UI calls API with AWS Sig V4 signed request
4. API extracts IAM principal ARN from signature
5. API queries rbac_mappings table for team/role
6. API enforces RBAC on request
```

### Idempotency Implementation

```go
// Pseudo-code for idempotency middleware
func IdempotencyMiddleware(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        if r.Method != "POST" && r.Method != "PUT" && r.Method != "DELETE" {
            next.ServeHTTP(w, r)
            return
        }

        idempotencyKey := r.Header.Get("Idempotency-Key")
        if idempotencyKey == "" {
            http.Error(w, "Idempotency-Key required", 400)
            return
        }

        // Check if key exists in database
        cached, err := store.GetIdempotencyKey(idempotencyKey)
        if err == nil {
            // Key exists, return cached response
            w.WriteHeader(cached.StatusCode)
            w.Write(cached.ResponseBody)
            return
        }

        // New request, process and cache response
        recorder := &responseRecorder{ResponseWriter: w}
        next.ServeHTTP(recorder, r)

        // Store response in database with 24h expiry
        store.SaveIdempotencyKey(idempotencyKey, recorder.StatusCode, recorder.Body, 24*time.Hour)
    })
}
```

### Key API Endpoints

#### Create Cluster
```http
POST /api/v1/clusters
Idempotency-Key: unique-key-123
Content-Type: application/json

{
  "name": "team-a-test-01",
  "platform": "aws",
  "version": "4.20.3",
  "profile": "aws-minimal-test",
  "region": "us-east-1",
  "baseDomain": "labs.example.com",
  "owner": "user@example.com",
  "team": "team-a",
  "costCenter": "sandbox",
  "ttlHours": 24
}
```

#### Destroy Cluster
```http
POST /api/v1/clusters/{clusterId}/destroy
Idempotency-Key: unique-key-456
```

#### Get Cluster Outputs
```http
GET /api/v1/clusters/{clusterId}/outputs

Response:
{
  "clusterId": "clu_01J...",
  "apiUrl": "https://api.cluster.labs.example.com:6443",
  "consoleUrl": "https://console-openshift-console.apps.cluster.labs.example.com",
  "kubeconfigDownloadUrl": "https://s3.presigned-url...",
  "kubeadminPasswordRef": "arn:aws:secretsmanager:us-east-1:123456789012:secret:..."
}
```

---

## Worker Service Implementation

**Concurrency doc**: `docs/worker-concurrency-safety.md`

### Job Processing Algorithm

```go
func (w *Worker) ProcessJobs(ctx context.Context) {
    for {
        // 1. Receive message from SQS FIFO queue
        msg, err := w.sqsClient.ReceiveMessage(ctx, &sqs.ReceiveMessageInput{
            QueueUrl:            aws.String(w.queueURL),
            MaxNumberOfMessages: 1,
            WaitTimeSeconds:     20, // Long polling
            VisibilityTimeout:   600, // 10 minutes
        })

        job := parseJob(msg.Body)

        // 2. Acquire cluster lock
        locked, err := w.acquireClusterLock(ctx, job.ClusterID, job.ID)
        if !locked {
            // Another worker has lock, skip this message
            w.sqsClient.ChangeMessageVisibility(ctx, msg, 0)
            continue
        }

        // 3. Start heartbeat to maintain lock
        heartbeatCtx, cancel := context.WithCancel(ctx)
        go w.maintainLockHeartbeat(heartbeatCtx, job.ClusterID, job.ID)

        // 4. Execute job
        err = w.executeJob(ctx, job)

        // 5. Release lock
        cancel() // Stop heartbeat
        w.releaseClusterLock(ctx, job.ClusterID, job.ID)

        // 6. Delete message from queue
        w.sqsClient.DeleteMessage(ctx, msg)
    }
}
```

### Lock Acquisition

```go
func (w *Worker) acquireClusterLock(ctx context.Context, clusterID, jobID string) (bool, error) {
    tx, err := w.db.BeginTx(ctx, &sql.TxOptions{
        Isolation: sql.LevelSerializable,
    })
    if err != nil {
        return false, err
    }
    defer tx.Rollback()

    // Use SELECT FOR UPDATE to lock cluster row
    var currentStatus string
    err = tx.QueryRow(`
        SELECT status FROM clusters
        WHERE id = $1
        FOR UPDATE
    `, clusterID).Scan(&currentStatus)

    // Validate state transition is allowed
    if !isValidTransition(currentStatus, job.JobType) {
        return false, fmt.Errorf("invalid state transition")
    }

    // Try to insert lock (will fail if already locked)
    _, err = tx.Exec(`
        INSERT INTO job_locks (cluster_id, job_id, locked_by, expires_at)
        VALUES ($1, $2, $3, NOW() + INTERVAL '60 minutes')
        ON CONFLICT (cluster_id) DO NOTHING
    `, clusterID, jobID, w.instanceID)

    if err != nil {
        return false, err
    }

    // Check if our insert succeeded
    var lockedJobID string
    err = tx.QueryRow(`
        SELECT job_id FROM job_locks WHERE cluster_id = $1
    `, clusterID).Scan(&lockedJobID)

    if lockedJobID != jobID {
        // Another worker won the race
        return false, nil
    }

    // Update job status
    _, err = tx.Exec(`
        UPDATE jobs SET status = 'RUNNING', started_at = NOW()
        WHERE id = $1
    `, jobID)

    err = tx.Commit()
    return err == nil, err
}
```

### Job Handlers

#### CREATE Job

```go
func (w *Worker) handleCreateJob(ctx context.Context, job *Job) error {
    // 1. Update cluster status
    w.updateClusterStatus(ctx, job.ClusterID, "CREATING")

    // 2. Load profile
    profile, err := w.profileLoader.Load(job.Profile)
    if err != nil {
        return err
    }

    // 3. Fetch pull secret from Secrets Manager
    pullSecret, err := w.secretsClient.GetSecret(ctx, w.pullSecretARN)
    if err != nil {
        return err
    }

    // 4. Render install-config.yaml
    installConfig, err := profile.RenderInstallConfig(job.Request, pullSecret)
    if err != nil {
        return err
    }

    // 5. Create working directory
    workDir := fmt.Sprintf("/work/%s", job.ID)
    os.MkdirAll(workDir, 0755)
    defer os.RemoveAll(workDir)

    // 6. Write install-config.yaml
    ioutil.WriteFile(filepath.Join(workDir, "install-config.yaml"), installConfig, 0644)

    // 7. Run openshift-install
    cmd := exec.CommandContext(ctx, "openshift-install", "create", "cluster",
        "--dir", workDir,
        "--log-level=info")

    output, err := cmd.CombinedOutput()

    // 8. Upload logs to S3
    w.uploadLogs(ctx, job.ClusterID, job.ID, output)

    if err != nil {
        return fmt.Errorf("install failed: %w", err)
    }

    // 9. Parse outputs (API URL, console URL, etc.)
    outputs, err := w.parseOutputs(workDir)
    if err != nil {
        return err
    }

    // 10. Snapshot install directory and upload to S3
    snapshot, err := w.createInstallDirSnapshot(workDir)
    if err != nil {
        return err
    }

    s3Uri, err := w.uploadSnapshot(ctx, job.ClusterID, snapshot)
    if err != nil {
        return err
    }

    // 11. Store outputs and artifacts in database
    err = w.storeOutputs(ctx, job.ClusterID, outputs)
    if err != nil {
        return err
    }

    err = w.storeArtifact(ctx, job.ClusterID, "INSTALL_DIR_SNAPSHOT", s3Uri, snapshot.Checksum)
    if err != nil {
        return err
    }

    // 12. Update cluster status to READY
    w.updateClusterStatus(ctx, job.ClusterID, "READY")

    // 13. Schedule TTL destroy job
    destroyAt := time.Now().Add(time.Duration(job.TTLHours) * time.Hour)
    w.scheduleDestroy(ctx, job.ClusterID, destroyAt)

    return nil
}
```

#### DESTROY Job

```go
func (w *Worker) handleDestroyJob(ctx context.Context, job *Job) error {
    // 1. Update cluster status
    w.updateClusterStatus(ctx, job.ClusterID, "DESTROYING")

    // 2. Download install directory snapshot from S3
    artifact, err := w.getLatestSnapshot(ctx, job.ClusterID)
    if err != nil {
        // Fallback to tag-based cleanup
        return w.destroyByTags(ctx, job.ClusterID)
    }

    // 3. Extract snapshot to working directory
    workDir := fmt.Sprintf("/work/%s", job.ID)
    err = w.extractSnapshot(artifact.S3URI, workDir)
    if err != nil {
        return err
    }
    defer os.RemoveAll(workDir)

    // 4. Run openshift-install destroy
    cmd := exec.CommandContext(ctx, "openshift-install", "destroy", "cluster",
        "--dir", workDir,
        "--log-level=debug")

    output, err := cmd.CombinedOutput()

    // 5. Upload destroy logs
    w.uploadDestroyLogs(ctx, job.ClusterID, job.ID, output)

    if err != nil {
        return fmt.Errorf("destroy failed: %w", err)
    }

    // 6. Verify no tagged resources remain
    orphans, err := w.checkForOrphans(ctx, job.ClusterID)
    if err != nil {
        return err
    }

    if len(orphans) > 0 {
        w.logOrphans(ctx, job.ClusterID, orphans)
        return fmt.Errorf("orphaned resources detected: %v", orphans)
    }

    // 7. Update cluster status to DESTROYED
    w.updateClusterStatus(ctx, job.ClusterID, "DESTROYED")

    return nil
}
```

---

## SQS Queue Configuration

### FIFO Queue Setup

```bash
# Create FIFO queue
aws sqs create-queue \
  --queue-name ocpctl-jobs.fifo \
  --attributes '{
    "FifoQueue": "true",
    "ContentBasedDeduplication": "true",
    "VisibilityTimeout": "600",
    "ReceiveMessageWaitTimeSeconds": "20",
    "MessageRetentionPeriod": "1209600"
  }'

# Create DLQ
aws sqs create-queue \
  --queue-name ocpctl-jobs-dlq.fifo \
  --attributes '{
    "FifoQueue": "true",
    "MessageRetentionPeriod": "1209600"
  }'

# Configure DLQ redrive policy
aws sqs set-queue-attributes \
  --queue-url https://sqs.us-east-1.amazonaws.com/123456789012/ocpctl-jobs.fifo \
  --attributes '{
    "RedrivePolicy": "{\"deadLetterTargetArn\":\"arn:aws:sqs:us-east-1:123456789012:ocpctl-jobs-dlq.fifo\",\"maxReceiveCount\":\"3\"}"
  }'
```

### Message Format

```json
{
  "jobId": "job_01J...",
  "clusterId": "clu_01J...",
  "jobType": "CREATE",
  "request": {
    "name": "team-a-test-01",
    "platform": "aws",
    "version": "4.20.3",
    "profile": "aws-minimal-test",
    ...
  }
}
```

### Sending Messages

```go
_, err := sqsClient.SendMessage(ctx, &sqs.SendMessageInput{
    QueueUrl: aws.String(queueURL),
    MessageGroupId: aws.String(fmt.Sprintf("cluster-%s", clusterID)),
    MessageBody: aws.String(messageJSON),
})
```

---

## Testing Strategy

### Unit Tests

```bash
# Test store package
go test ./internal/store/...

# Test profile loader
go test ./internal/profile/...

# Test policy engine
go test ./internal/policy/...
```

### Integration Tests

```bash
# Requires PostgreSQL and LocalStack
make docker-up

# Run integration tests
go test -tags=integration ./...
```

### Key Test Cases

1. **Lock Acquisition**:
   - Two workers try to lock same cluster simultaneously
   - Only one succeeds

2. **Idempotency**:
   - Same create request sent twice with same key
   - Second request returns cached response

3. **State Transitions**:
   - CREATE job requires PENDING status
   - DESTROY job requires READY or FAILED status
   - Invalid transitions are rejected

4. **Worker Crash Recovery**:
   - Worker acquires lock and crashes
   - Lock expires after 60 minutes
   - Janitor cleans up stale lock

---

## Monitoring and Observability

### Critical Metrics

```go
// Prometheus metrics to implement
var (
    jobsTotal = prometheus.NewCounterVec(
        prometheus.CounterOpts{
            Name: "ocpctl_jobs_total",
            Help: "Total number of jobs by type and status",
        },
        []string{"type", "status"},
    )

    jobDuration = prometheus.NewHistogramVec(
        prometheus.HistogramOpts{
            Name: "ocpctl_job_duration_seconds",
            Help: "Job execution duration",
            Buckets: []float64{60, 300, 600, 1800, 3600},
        },
        []string{"type", "status"},
    )

    clustersTotal = prometheus.NewGaugeVec(
        prometheus.GaugeOpts{
            Name: "ocpctl_clusters_total",
            Help: "Total clusters by platform and status",
        },
        []string{"platform", "status"},
    )

    lockAcquisitionFailures = prometheus.NewCounterVec(
        prometheus.CounterOpts{
            Name: "ocpctl_lock_acquisition_failures_total",
            Help: "Lock acquisition failures by reason",
        },
        []string{"reason"},
    )
)
```

### Structured Logging

```go
logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

logger.Info("job started",
    "job_id", job.ID,
    "cluster_id", job.ClusterID,
    "job_type", job.Type,
    "correlation_id", correlationID,
)
```

---

## Quick Reference Commands

```bash
# Start local development environment
make docker-up

# Run database migrations
make migrate-up

# Start API server
make run-api

# Start worker service
make run-worker

# Run tests
make test

# Build all services
make build

# Clean build artifacts
make clean

# Format code
make fmt

# Run linters
make lint
```

---

## Common Pitfalls to Avoid

1. ❌ **Don't skip idempotency key validation** - Leads to duplicate clusters
2. ❌ **Don't allow concurrent jobs on same cluster** - State corruption
3. ❌ **Don't store kubeadmin passwords in database** - Use Secrets Manager
4. ❌ **Don't delete install-dir snapshots** - Needed for destroy
5. ❌ **Don't bypass profile validation** - Allows policy violations
6. ❌ **Don't update cluster status without transaction** - Race conditions
7. ❌ **Don't skip RBAC checks** - Unauthorized access
8. ❌ **Don't log sensitive data** - Secrets in logs

---

## Support and Resources

- **Design Spec**: [design-specification.md](design-specification.md)
- **Architecture**: [architecture.md](architecture.md)
- **API Spec**: [api.yaml](api.yaml)
- **Worker Safety**: [worker-concurrency-safety.md](worker-concurrency-safety.md)
- **Critical Items**: [CRITICAL-ITEMS-RESOLVED.md](CRITICAL-ITEMS-RESOLVED.md)

For questions, refer to the design documents or consult the platform engineering team.
