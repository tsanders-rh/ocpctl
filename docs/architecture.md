# Architecture Overview

## System Components

### 1. Web UI (React + TypeScript)
- Self-service cluster request form
- Cluster inventory and status dashboard
- Admin policy and profile management
- Cost and usage reports
- Authentication via AWS IAM Identity Center

### 2. API Service (Go)
- RESTful API implementing OpenAPI spec
- Request validation and policy enforcement
- Job queue management
- IAM-based authentication and authorization
- Idempotency key handling
- Audit event recording

### 3. Worker Service (Go)
- Asynchronous job execution
- OpenShift installer orchestration
- State artifact management (S3)
- Log streaming and persistence
- Retry logic with exponential backoff
- Horizontally scalable

### 4. Janitor Service (Go)
- TTL-based cluster destruction
- Orphan resource detection and cleanup
- Failed job retry coordination
- Scheduled off-hours scaling operations
- Cost optimization sweeps

### 5. CLI Tool (Go)
- `ocpctl` command-line interface
- Wraps API calls for common operations
- Status tracking and output formatting
- Local configuration management

## Data Stores

### PostgreSQL (RDS)
- Primary state database
- Tables:
  - `clusters`: cluster inventory and metadata
  - `cluster_outputs`: cluster access information
  - `cluster_artifacts`: S3 artifact references
  - `jobs`: async job status and history
  - `audit_events`: immutable audit trail
  - `usage_samples`: cost tracking data

### S3 + KMS
- Artifact storage for:
  - OpenShift installer state directories
  - Installation and destroy logs
  - Cluster metadata and outputs
  - Authentication artifacts
- SSE-KMS encryption
- Versioning enabled for disaster recovery
- Lifecycle policies for retention management

### AWS Secrets Manager
- Pull secret storage
- Service credentials
- Cluster kubeadmin passwords

### SQS
- Job queue for async operations
- DLQ for failed jobs
- FIFO queue for cluster-scoped operations

## Control Flow

### Cluster Creation

```
User → Web UI → API Service → PostgreSQL (create cluster record)
                             → SQS (enqueue CREATE job)
                             → Audit Log

Worker → SQS (dequeue job)
       → PostgreSQL (update status: CREATING)
       → Secrets Manager (fetch pull secret)
       → openshift-install create cluster
       → S3 (upload install dir snapshot)
       → S3 (upload logs)
       → PostgreSQL (update status: READY, store outputs)
       → Scheduler (register TTL destroy job)
```

### Cluster Destruction

```
User/Janitor → API Service → PostgreSQL (update status: DESTROYING)
                           → SQS (enqueue DESTROY job)
                           → Audit Log

Worker → SQS (dequeue job)
       → S3 (download install dir snapshot)
       → openshift-install destroy cluster
       → S3 (upload destroy logs)
       → Cloud API (verify no tagged resources remain)
       → PostgreSQL (update status: DESTROYED)
```

## Security Architecture

### Authentication Flow

```
User → AWS IAM Identity Center → SAML assertion
                               → Web UI (session cookie)

Web UI → API Service → IAM role verification
                     → Team/RBAC mapping
                     → Request processing
```

### Authorization Model

- **Requester**: Can create/destroy/view own team's clusters
- **Platform Admin**: Full access, policy management, break-glass operations
- **Auditor**: Read-only access to all data and reports

### Network Security

- API service in private VPC subnet
- VPC endpoints for S3, Secrets Manager, SQS
- Security groups restrict traffic to known sources
- TLS 1.3 for all communication
- No public internet access from worker nodes

## Observability

### Metrics (Prometheus)
- `ocpctl_jobs_total{type, status}`
- `ocpctl_job_duration_seconds{type, status}`
- `ocpctl_clusters_total{platform, status}`
- `ocpctl_destroy_failures_total{platform}`
- `ocpctl_orphan_resources{platform}`

### Logging
- Structured JSON logs
- Correlation IDs for request tracing
- Log levels: debug, info, warn, error
- Fields: timestamp, level, component, correlation_id, message, metadata

### Alerting
- Destroy failure rate > 5%
- Cluster past TTL not destroyed
- Orphan resources detected
- Job queue depth > threshold
- Worker service unavailable

## Scalability

### Horizontal Scaling
- API service: Multiple instances behind load balancer
- Worker service: Auto-scaling based on queue depth
- Database: RDS read replicas for reporting queries

### Performance Targets
- API response time: p95 < 200ms
- Job enqueue latency: p99 < 2s
- Worker job start: p90 < 60s
- Cluster create time: depends on cloud provider (typically 30-45 min)
- Cluster destroy time: depends on resources (typically 5-15 min)

## Disaster Recovery

### State Backup
- PostgreSQL automated backups (point-in-time recovery)
- S3 cross-region replication for artifacts
- Daily snapshot verification

### Recovery Procedures
- Database restore from backup
- Artifact restoration from replica region
- Worker job replay from queue DLQ
- Manual orphan cleanup using cloud provider tags

## Technology Stack

- **Language**: Go 1.22+
- **Web Framework**: Chi router, standard library
- **Database**: PostgreSQL 15+ with pgx driver
- **Queue**: AWS SQS
- **Object Storage**: AWS S3
- **Frontend**: React 18, TypeScript, Tailwind CSS
- **Infrastructure**: Terraform
- **CI/CD**: GitHub Actions
- **Observability**: Prometheus, Grafana, structured logging
