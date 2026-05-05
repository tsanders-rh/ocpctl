# OCPCTL - System Architecture & Debugging Guide

**Purpose**: This file provides Claude with persistent context about ocpctl's architecture, production environment, and common debugging patterns across sessions.

---

## Project Overview

**ocpctl** is a cluster lifecycle management platform for provisioning and managing OpenShift, EKS, GKE, and IKS clusters across AWS, GCP, and IBM Cloud.

**Tech Stack**:
- **Backend**: Go (API server + async worker service)
- **Frontend**: Next.js 14 (React, TypeScript, Tailwind CSS)
- **Database**: PostgreSQL (41 migrations)
- **Message Queue**: PostgreSQL-based job queue with distributed locking
- **Storage**: AWS S3 for artifacts and binaries
- **Deployment**: Systemd services, auto-scaling workers

---

## Production Environment

### Primary Server
- **Hostname**: ocpctl-production
- **IP**: 44.201.165.78
- **Instance Type**: t3.large (2 vCPU, 8GB RAM)
- **OS**: Ubuntu 22.04 LTS
- **SSH**: `ssh -i ~/.ssh/ocpctl-production-key ubuntu@44.201.165.78`

### Service Ports
- **API Server**: 8080 (internal, proxied via nginx)
- **Worker Health**: 8081 (internal)
- **Web UI**: 3000 (internal, proxied via nginx)
- **Nginx**: 80, 443 (public)

### Production Paths
```
/opt/ocpctl/
├── current/                    # Symlink to active version
│   ├── ocpctl-api             # API server binary
│   └── ocpctl-worker          # Worker binary
├── releases/                   # Versioned deployments
│   └── v0.YYYYMMDD.HASH/
├── profiles/                   # Cluster profile YAML files (30+)
├── addons/                     # CNV, MTA, MTC, OADP definitions
├── scripts/                    # ensure-installers.sh, etc.
└── manifests/                  # Kubernetes manifests

/etc/ocpctl/
├── api.env                     # API server config (DATABASE_URL, JWT_SECRET)
└── worker.env                  # Worker config (WORK_DIR, CONCURRENCY)

/var/lib/ocpctl/
└── clusters/                   # Worker cluster work directories
```

### Systemd Services
```bash
# Check service status
sudo systemctl status ocpctl-api
sudo systemctl status ocpctl-worker

# View logs
sudo journalctl -u ocpctl-api -f
sudo journalctl -u ocpctl-worker -f

# Restart services
sudo systemctl restart ocpctl-api
sudo systemctl restart ocpctl-worker
```

### S3 Buckets
- **ocpctl-binaries**: Worker binaries, bootstrap scripts, profiles, addons
  - `releases/v0.YYYYMMDD.HASH/` - Versioned binaries
  - `binaries/` - Current symlinked binaries
  - `scripts/` - Bootstrap and utility scripts
  - `LATEST` - Current version pointer
- **ocpctl-artifacts**: Cluster state, kubeconfigs, installer directories

---

## Code Structure

### Top-Level Directories
```
ocpctl/
├── cmd/                        # Entry points
│   ├── api/                   # API server (main.go → port 8080)
│   └── worker/                # Worker service (main.go → port 8081 health)
├── internal/                   # Core logic (not exported)
│   ├── api/                   # REST handlers (26 files)
│   ├── worker/                # Job handlers (create, destroy, hibernate, resume, post-config)
│   ├── store/                 # PostgreSQL data access (migrations/)
│   ├── profile/               # Cluster profile system
│   ├── addon/                 # Post-deployment addons
│   ├── installer/             # CLI wrappers (openshift-install, eksctl, etc.)
│   ├── auth/                  # JWT + IAM authentication
│   ├── aws/                   # AWS-specific implementations
│   ├── gcp/                   # GCP-specific implementations
│   └── ibmcloud/              # IBM Cloud implementations
├── pkg/types/                  # Shared types (cluster.go, job.go)
├── web/                        # Next.js 14 frontend
├── scripts/                    # deploy.sh, bootstrap-worker.sh, etc.
├── deploy/                     # Systemd, nginx configs
├── terraform/                  # Infrastructure as Code
└── docs/                       # Documentation
```

### Key Entry Points

**API Server** (`cmd/api/main.go`):
- Initializes database connection pool (pgx)
- Loads profiles from `PROFILES_DIR`
- Loads addons from `ADDONS_DIR`
- Starts Echo HTTP server on port 8080
- Endpoints: `/api/v1/*` (clusters, profiles, auth, costs, storage)

**Worker Service** (`cmd/worker/main.go`):
- Polls PostgreSQL job queue every 10 seconds
- Acquires distributed locks (90-minute TTL)
- Processes jobs concurrently (default: 3 max)
- Streams logs to database
- Graceful shutdown: 1 hour timeout

---

## Architecture Components

### 1. API Server (Port 8080)
**Purpose**: RESTful API for cluster lifecycle management

**Key Handlers** (`internal/api/`):
- `handler_clusters.go` - Create/list/update/destroy clusters
- `handler_auth.go` - JWT login, IAM auth
- `handler_profiles.go` - Profile metadata
- `handler_addons.go` - Addon management
- `handler_costs.go` - Cost tracking
- `handler_orphaned_resources.go` - AWS/GCP cleanup

**Authentication**:
- JWT tokens (stored in HTTP-only cookies)
- Optional AWS IAM SigV4 authentication
- RBAC roles: Admin, User, Viewer

### 2. Worker Service (Port 8081 Health)
**Purpose**: Asynchronous job processing

**Job Types** (`internal/worker/`):
| Job Type | Handler | Duration |
|----------|---------|----------|
| CREATE | handler_create.go | 30-60 min |
| DESTROY | handler_destroy.go | 10-20 min |
| HIBERNATE | handler_hibernate.go | 5-10 min |
| RESUME | handler_resume.go | 5-10 min |
| POST_CONFIGURE | handler_post_configure.go | 10-30 min |
| JANITOR_DESTROY | handler_destroy.go | 10-20 min |

**Handler Pattern**:
```go
func (h *CreateHandler) Handle(ctx context.Context, job *types.Job) error {
    // 1. Load cluster from database
    // 2. Switch on cluster.ClusterType (openshift, eks, gke, iks)
    // 3. Call platform-specific handler (handleOpenShiftCreate, etc.)
    // 4. Update cluster status (CREATING → READY or FAILED)
    // 5. Upload artifacts to S3
    // 6. Trigger post-deployment if configured
}
```

### 3. Database (PostgreSQL)
**Core Tables**:
- `clusters` - Cluster inventory (name, status, platform, profile, owner, ttl)
- `cluster_outputs` - Access info (api_url, console_url, kubeconfig_s3_uri)
- `jobs` - Async job queue (type, status, attempt, error_message)
- `job_locks` - Distributed locks (cluster_id, locked_by, expires_at)
- `audit_events` - Immutable audit trail
- `users` - User accounts and RBAC
- `rbac_mappings` - IAM to team/role mappings
- `post_config_addons` - Available addons

**Migrations**: `internal/store/migrations/` (00001-00041)

### 4. Profile System
**Location**: `internal/profile/definitions/` (30+ YAML files)

**Supported Profiles**:
- AWS OpenShift: sno-ga, standard, minimal, virtualization, prerelease
- AWS EKS: eks-standard
- GCP: gke-standard, openshift-standard
- IBM Cloud: iks-standard

**Profile Structure**:
```yaml
name: aws-sno-ga
platform: aws
clusterType: openshift  # openshift|eks|gke|iks
track: ga               # ga|prerelease|kube
openshiftVersions:
  allowlist: [4.18, 4.19, 4.20, 4.21, 4.22]
  default: 4.20
regions:
  allowlist: [us-east-1, us-west-2, ...]
compute:
  workers:
    replicas: 1
    instanceType: m6i.2xlarge
lifecycle:
  ttlHours: 72
  offhoursBehavior: hibernate
```

### 5. Addon System
**Location**: `internal/addon/definitions/`

**Available Addons**:
- `cnv-nightly.yaml` - OpenShift Virtualization (4.22 stable-stage, 4.99 nightly, Windows VM support)
- `mta.yaml` - Migration Toolkit for Applications
- `mtc.yaml` - Migration Toolkit for Containers
- `oadp.yaml` - OpenShift API for Data Protection

**Addon Execution**: POST_CONFIGURE job after cluster reaches READY status

---

## Deployment Process

### Version Format
```
v0.YYYYMMDD.COMMITHASH
Example: v0.20260505.d70856c
```

### Deployment Script (`scripts/deploy.sh`)

**What it does**:
1. **Build binaries** with version metadata embedded via `-ldflags`
2. **Upload to S3**:
   - Versioned: `s3://ocpctl-binaries/releases/v0.YYYYMMDD.HASH/`
   - Stable: `s3://ocpctl-binaries/binaries/ocpctl-worker`
   - Update `LATEST` pointer
3. **Sync artifacts**: profiles, addons, manifests, scripts
4. **Terminate autoscale workers** (ASG launches replacements with new version)
5. **Deploy to API server**:
   - Copy binary to `/opt/ocpctl/releases/VERSION/`
   - Update symlink: `/opt/ocpctl/current → releases/VERSION`
   - Restart: `systemctl restart ocpctl-api`
6. **Deploy to worker server**:
   - Same process as API
   - Requeue RUNNING jobs to PENDING (prevents orphaned jobs)
   - Clear stale locks
   - Restart: `systemctl restart ocpctl-worker`
7. **Verify**: Check `/version` endpoints for API and worker

**Run deployment**:
```bash
./scripts/deploy.sh
# or with specific version:
./scripts/deploy.sh v0.20260505.abc1234
```

### Rollback
```bash
# List available versions
ssh -i ~/.ssh/ocpctl-production-key ubuntu@44.201.165.78 'sudo ls -d /opt/ocpctl/releases/*'

# Deploy specific version
./scripts/deploy.sh v0.20260413.1346b69
```

---

## Common Patterns & Debugging

### Cluster Types
```go
// pkg/types/cluster.go
const (
    ClusterTypeOpenShift ClusterType = "openshift"  // IPI, openshift-install
    ClusterTypeEKS       ClusterType = "eks"        // eksctl
    ClusterTypeIKS       ClusterType = "iks"        // ibmcloud cli
    ClusterTypeGKE       ClusterType = "gke"        // gke cli
)
```

### Installer Wrappers
**Location**: `internal/installer/`

**Supported CLIs**:
- `openshift-install` - OpenShift 4.x provisioning
- `eksctl` - AWS EKS clusters
- `gcloud` / `gke` - Google Cloud GKE
- `ibmcloud` - IBM Cloud IKS
- `ccoctl` - Cloud Credential Operator (OpenShift)

**Binary Location**: `/usr/local/bin/` (installed by `ensure-installers.sh`)

### Job Processing Flow
```
1. User creates cluster via API
2. API validates request, creates cluster record (status: PENDING)
3. API creates CREATE job (status: PENDING)
4. Worker polls database, finds job
5. Worker acquires lock on cluster
6. Worker executes handler (handleOpenShiftCreate, etc.)
7. Handler updates status: CREATING → READY or FAILED
8. Worker uploads artifacts to S3
9. Worker releases lock, marks job SUCCEEDED or FAILED
10. If SUCCEEDED and !skipPostDeployment, create POST_CONFIGURE job
```

### Distributed Locking
**Purpose**: Prevent multiple workers from processing same cluster simultaneously

**Implementation**:
- PostgreSQL `job_locks` table
- Lock TTL: 90 minutes (longer than longest operation)
- Heartbeat goroutine maintains lock during long operations
- Auto-release on job completion or failure

### Hibernation vs Stop
| Cluster Type | Hibernation Method | Cost When Hibernated |
|--------------|-------------------|---------------------|
| OpenShift IPI | Stop all EC2 instances | ~10% (storage only) |
| EKS | Scale node groups to 0 | $0.10/hr (control plane) |
| GKE | Stop node pools | $0.10/hr (control plane) |

**Note**: ROSA (when implemented) cannot stop control plane, only scale workers to 0 ($0.03/hr)

### Debug Cluster Creation Failures

**Check job logs**:
```sql
-- Connect to database
psql $DATABASE_URL

-- Find failed jobs
SELECT id, cluster_id, job_type, status, error_message, ended_at
FROM jobs
WHERE status = 'FAILED'
ORDER BY ended_at DESC
LIMIT 10;

-- Get detailed logs
SELECT log FROM job_logs WHERE job_id = 'job-uuid';
```

**Check worker logs**:
```bash
sudo journalctl -u ocpctl-worker -f
```

**Check cluster work directory**:
```bash
# On worker server
cd /var/lib/ocpctl/clusters/<cluster-name>
ls -la
cat .openshift_install.log  # OpenShift installer logs
```

**Check S3 artifacts**:
```bash
aws s3 ls s3://ocpctl-artifacts/clusters/<cluster-id>/
```

### Debug API Issues

**Check API logs**:
```bash
sudo journalctl -u ocpctl-api -f
```

**Test API directly**:
```bash
# Health check
curl http://localhost:8080/health

# Version
curl http://localhost:8080/version

# List clusters (requires auth)
curl -H "Authorization: Bearer $JWT_TOKEN" http://localhost:8080/api/v1/clusters
```

**Check profile loading**:
```bash
# API logs should show on startup:
# "Loaded N profiles from /opt/ocpctl/profiles"
# "Profile registry initialized with X enabled profiles"

# Verify profiles directory
ls -la /opt/ocpctl/profiles/
```

---

## Quick Commands Reference

### Production Server Access
```bash
# SSH to production
ssh -i ~/.ssh/ocpctl-production-key ubuntu@44.201.165.78

# Check service status
sudo systemctl status ocpctl-api ocpctl-worker

# View live logs
sudo journalctl -u ocpctl-api -f
sudo journalctl -u ocpctl-worker -f

# Restart services
sudo systemctl restart ocpctl-api
sudo systemctl restart ocpctl-worker

# Check database connectivity
psql $DATABASE_URL -c "SELECT version();"
```

### Development
```bash
# Run locally
make run-api     # Start API on :8080
make run-worker  # Start worker

# Build binaries
make build

# Run tests
make test

# Database migrations
make migrate
```

### S3 Operations
```bash
# List worker binaries
aws s3 ls s3://ocpctl-binaries/releases/

# Check current version
aws s3 cp s3://ocpctl-binaries/LATEST -

# List cluster artifacts
aws s3 ls s3://ocpctl-artifacts/clusters/
```

---

## Important Files for Common Tasks

### Adding New Cluster Type (e.g., ROSA)
1. `pkg/types/cluster.go` - Add ClusterType constant
2. `internal/store/migrations/XXX_add_rosa.sql` - Database migration
3. `internal/installer/rosa.go` - Create installer wrapper
4. `internal/worker/handler_create.go` - Add case for handleROSACreate
5. `internal/worker/handler_destroy.go` - Add case for handleROSADestroy
6. `internal/worker/handler_hibernate.go` - Add hibernation logic
7. `internal/worker/handler_resume.go` - Add resume logic
8. `internal/profile/definitions/aws-rosa-*.yaml` - Create profiles
9. `internal/api/handler_clusters.go` - Update validation

### Adding New Profile
1. Create YAML in `internal/profile/definitions/`
2. Follow existing pattern (aws-sno-ga.yaml as template)
3. Validate required fields: name, platform, clusterType, regions, compute
4. Deploy to S3: `aws s3 cp internal/profile/definitions/new-profile.yaml s3://ocpctl-binaries/profiles/`
5. Restart API to reload: `sudo systemctl restart ocpctl-api`

### Adding New Addon
1. Create YAML in `internal/addon/definitions/`
2. Define versions, scripts/manifests, metadata
3. Deploy to S3: `aws s3 cp internal/addon/definitions/new-addon.yaml s3://ocpctl-binaries/addons/`
4. Restart API to reload: `sudo systemctl restart ocpctl-api`

### Debugging Worker Stuck Jobs
```sql
-- Find stuck jobs (RUNNING for > 2 hours)
SELECT id, cluster_id, job_type, status, started_at,
       NOW() - started_at as duration
FROM jobs
WHERE status = 'RUNNING'
  AND started_at < NOW() - INTERVAL '2 hours';

-- Check locks
SELECT * FROM job_locks WHERE expires_at > NOW();

-- Manually fail stuck job (use carefully!)
UPDATE jobs SET status = 'FAILED', error_message = 'Manual intervention: stuck job'
WHERE id = 'job-uuid';

-- Clear stuck lock
DELETE FROM job_locks WHERE cluster_id = 'cluster-uuid';
```

---

## Cost Tracking

### Hibernation Cost Savings
| Cluster Type | Normal Cost | Hibernated Cost | Savings |
|--------------|-------------|-----------------|---------|
| AWS OpenShift SNO | $0.384/hr | $0.038/hr | 90% |
| AWS OpenShift 3-node | $1.152/hr | $0.115/hr | 90% |
| EKS | $0.394/hr | $0.10/hr | 75% |
| GKE | $0.320/hr | $0.10/hr | 69% |

### Work Hours Enforcement
- Default: 8am-6pm EST, Monday-Friday
- Auto-hibernate outside hours (if profile.offhoursBehavior = "hibernate")
- Override: Set cluster.ignore_work_hours = true

---

## Troubleshooting

### "Cluster stuck in CREATING"
1. Check job status: `SELECT * FROM jobs WHERE cluster_id = 'xxx' AND status = 'RUNNING'`
2. Check worker logs: `sudo journalctl -u ocpctl-worker -f`
3. Check work directory: `ls /var/lib/ocpctl/clusters/<cluster-name>/`
4. Check installer logs: `cat /var/lib/ocpctl/clusters/<cluster-name>/.openshift_install.log`

### "Worker not picking up jobs"
1. Check worker service: `sudo systemctl status ocpctl-worker`
2. Check database connectivity: `psql $DATABASE_URL -c "SELECT 1;"`
3. Check job queue: `SELECT * FROM jobs WHERE status = 'PENDING' ORDER BY created_at`
4. Check locks: `SELECT * FROM job_locks WHERE expires_at > NOW()`

### "Profile not showing in UI"
1. Check profile file exists: `ls /opt/ocpctl/profiles/`
2. Check API logs for loading errors: `sudo journalctl -u ocpctl-api | grep profile`
3. Verify YAML syntax: `yq eval /opt/ocpctl/profiles/profile.yaml`
4. Restart API: `sudo systemctl restart ocpctl-api`

### "Deployment failed"
1. Check S3 upload: `aws s3 ls s3://ocpctl-binaries/releases/`
2. Check binary permissions: `ls -la /opt/ocpctl/current/`
3. Check systemd service: `sudo systemctl status ocpctl-api ocpctl-worker`
4. Check version: `curl http://localhost:8080/version`
5. Rollback if needed: `./scripts/deploy.sh v0.PREVIOUS_VERSION`

---

## Recent Changes

**2026-05-05**: Added CNV 4.22 stable-stage support with Windows VM option
- Catalog: `quay.io/openshift-cnv/nightly-catalog:4.22`
- Channel: `nightly-4.22`
- Windows support includes automated IRSA setup and S3 image import
- 4 versions now available: 4.22 (base + Windows), 4.99 (base + Windows)

---

## Resources

- **Production URL**: https://ocpctl.mg.dog8code.com
- **GitHub**: https://github.com/tsanders-rh/ocpctl
- **Docs**: `/Users/tsanders/Workspace2/ocpctl/docs/`
- **Architecture Decisions**: `docs/features/` (ROSA_SUPPORT_PLAN.md, etc.)
- **Swagger API**: https://ocpctl.mg.dog8code.com/swagger/index.html

---

**Last Updated**: 2026-05-05 (CNV 4.22 deployment)
