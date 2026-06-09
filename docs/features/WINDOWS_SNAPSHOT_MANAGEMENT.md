# Windows Snapshot Management System

## Overview

The Windows Snapshot Management System provides centralized control and visibility for regional EBS snapshots used in Windows VM provisioning. This system reduces Windows VM deployment time from 30-50 minutes to 2-3 minutes by pre-creating validated snapshots in each AWS region.

## Architecture

### Components

1. **Database Layer** (`internal/store/windows_snapshots.go`)
   - Tracks snapshot creation status and metadata
   - Provides admin dashboard visibility
   - Audit logging for snapshot operations

2. **API Layer** (`internal/api/handler_windows_snapshots.go`)
   - Admin-only endpoints for snapshot management
   - Triggers snapshot creation jobs
   - Provides coverage statistics

3. **Worker Layer** (`internal/worker/handler_windows_snapshot.go`)
   - Processes `CREATE_WINDOWS_SNAPSHOT` jobs
   - **Current status**: Stub implementation
   - **TODO**: Full snapshot creation workflow

4. **Cluster Provisioning** (`manifests/windows-vm/auto-setup-irsa.sh`)
   - Discovers snapshots via SSM Parameter Store
   - Falls back to S3 import if no snapshot exists
   - Creates cluster-local VolumeSnapshot for fast VM provisioning

### Data Flow

```
┌──────────────────────────────────────────────────────────────┐
│ Admin Dashboard (UI)                                         │
│  - View snapshot coverage across regions                     │
│  - Trigger snapshot creation                                 │
│  - Monitor job progress                                      │
└────────────────┬─────────────────────────────────────────────┘
                 │
                 ▼
┌──────────────────────────────────────────────────────────────┐
│ API Server (Go)                                              │
│  - POST /api/v1/windows-snapshots                           │
│  - GET  /api/v1/windows-snapshots/coverage                  │
│  - Creates CREATE_WINDOWS_SNAPSHOT job                       │
└────────────────┬─────────────────────────────────────────────┘
                 │
                 ▼
┌──────────────────────────────────────────────────────────────┐
│ Worker (Go)                                                  │
│  - TODO: Create temporary cluster in target region          │
│  - TODO: Install OpenShift Virtualization                   │
│  - TODO: Import Windows image from S3                       │
│  - TODO: Create and validate EBS snapshot                   │
│  - TODO: Publish to SSM Parameter Store                     │
└────────────────┬─────────────────────────────────────────────┘
                 │
                 ▼
┌──────────────────────────────────────────────────────────────┐
│ SSM Parameter Store                                          │
│  /ocpctl/windows-snapshots/{version}/{region}                │
│  → snap-abc123def456                                         │
└──────────────────────────────────────────────────────────────┘
                 │
                 ▼
┌──────────────────────────────────────────────────────────────┐
│ Cluster Provisioning (Bash)                                 │
│  - Check SSM for snapshot                                    │
│  - If found: Fast path (2-3 min)                            │
│  - If not found: S3 fallback (30-50 min) + create snapshot │
└──────────────────────────────────────────────────────────────┘
```

## Database Schema

### `windows_snapshots` table

```sql
CREATE TABLE windows_snapshots (
    id UUID PRIMARY KEY,
    region VARCHAR(50) NOT NULL,
    version VARCHAR(20) NOT NULL,
    ebs_snapshot_id VARCHAR(50) NOT NULL,
    status VARCHAR(20) NOT NULL,  -- creating, validating, ready, failed, deleting
    ssm_parameter_path VARCHAR(255),
    s3_source_url TEXT,
    created_at TIMESTAMP NOT NULL,
    updated_at TIMESTAMP NOT NULL,
    validated_at TIMESTAMP,
    error_message TEXT,
    job_id UUID REFERENCES jobs(id),
    snapshot_size_gb INTEGER,
    validation_vm_booted BOOLEAN,
    UNIQUE(region, version)
);
```

### `windows_snapshot_audit` table

```sql
CREATE TABLE windows_snapshot_audit (
    id UUID PRIMARY KEY,
    snapshot_id UUID NOT NULL REFERENCES windows_snapshots(id),
    action VARCHAR(50) NOT NULL,
    user_id UUID REFERENCES users(id),
    details JSONB,
    created_at TIMESTAMP NOT NULL
);
```

## API Endpoints

### List Snapshots
```
GET /api/v1/admin/windows-snapshots
Query params:
  - region: Filter by AWS region
  - status: Filter by status (creating, validating, ready, failed)
Response: [WindowsSnapshot]
```

### Get Snapshot
```
GET /api/v1/admin/windows-snapshots/:id
Response: WindowsSnapshot
```

### Get Coverage
```
GET /api/v1/admin/windows-snapshots/coverage
Query params:
  - version: Latest version (default: 1.0)
Response: {
  total_regions: 15,
  covered_regions: 8,
  coverage_percent: 53.3,
  latest_version: "1.0",
  missing_regions: ["us-west-1", ...],
  outdated_regions: ["eu-west-1", ...],
  snapshots_by_region: {...}
}
```

### Create Snapshot
```
POST /api/v1/admin/windows-snapshots
Body: {
  region: "us-east-1",
  version: "1.0",
  s3_source_url: "s3://ocpctl-binaries/windows-images/windows-10-oadp.qcow2"  # optional
}
Response: {
  snapshot_id: "uuid",
  job_id: "uuid",
  status: "creating",
  message: "Snapshot creation job started"
}
```

### Delete Snapshot
```
DELETE /api/v1/admin/windows-snapshots/:id
Response: {
  message: "Snapshot deleted successfully"
}
```

## Worker Implementation Plan

The `CREATE_WINDOWS_SNAPSHOT` job handler needs to be fully implemented. Two possible approaches:

### Approach 1: Temporary Cluster (Current Plan)

1. Create a minimal OpenShift cluster in the target region
   - Use SNO profile (single node)
   - Minimum compute resources
   - 2-hour TTL
2. Install OpenShift Virtualization
3. Run S3 import workflow (30-50 min)
4. Create VolumeSnapshot from imported PVC
5. Extract EBS snapshot ID
6. Validate by booting a test VM
7. Tag and publish to SSM
8. Destroy temporary cluster

**Pros**: Uses existing infrastructure, well-tested import process
**Cons**: Expensive (30-40 min cluster creation + 30-50 min import), slow

### Approach 2: Dedicated Snapshot Factory Cluster (Recommended)

1. Maintain a single "snapshot factory" cluster in us-east-1
2. Has OpenShift Virtualization pre-installed
3. For each snapshot request:
   - Download Windows QCOW2 from S3
   - Use AWS EC2 API to create EBS volume in target region
   - Use CDI to import QCOW2 to that volume
   - Snapshot the volume via AWS API
   - Validate by creating a temporary test cluster
   - Tag and publish

**Pros**: Faster (no cluster creation per snapshot), cost-effective
**Cons**: More complex implementation, requires EC2 API access

### Approach 3: Direct AWS API (Most Efficient)

1. Use AWS EC2 APIs directly to create volumes and snapshots
2. No OpenShift cluster required for snapshot creation
3. Validation uses a temporary small cluster

**Pros**: Fastest, cheapest
**Cons**: Most complex, requires reimplementing CDI import logic

## UI Components

### Snapshot Dashboard Page

**Location**: `web/components/admin/WindowsSnapshots/`

**Components**:

1. **Coverage Widget** (`CoverageWidget.tsx`)
   - Shows percentage of regions with snapshots
   - Visual progress bar
   - List of missing/outdated regions

2. **Snapshot Table** (`SnapshotTable.tsx`)
   - Columns: Region, Version, Status, Created, Actions
   - Sortable and filterable
   - Real-time status updates via polling

3. **Create Snapshot Modal** (`CreateSnapshotModal.tsx`)
   - Region dropdown (all AWS regions)
   - Version input
   - S3 source URL (optional)
   - Validation

4. **Job Progress Viewer** (`JobProgressViewer.tsx`)
   - Real-time log streaming
   - Progress indicators
   - Cancel button (if applicable)

### Mockup

```
┌─────────────────────────────────────────────────────────────┐
│ Windows Snapshots                                   [Create]│
├─────────────────────────────────────────────────────────────┤
│                                                              │
│ ┌──────────────────────────────────────────────────────┐   │
│ │ Coverage: 8/15 regions (53%)                         │   │
│ │ ████████████████░░░░░░░░░░░░░░░░░░                  │   │
│ │ Missing: us-west-1, eu-central-1, ap-south-1         │   │
│ └──────────────────────────────────────────────────────┘   │
│                                                              │
│ ┌──────────────────────────────────────────────────────┐   │
│ │ Region       Version  Status    Created    Actions   │   │
│ ├──────────────────────────────────────────────────────┤   │
│ │ us-east-1    1.0      ✓ ready   2d ago     [Delete] │   │
│ │ us-west-2    1.0      ✓ ready   2d ago     [Delete] │   │
│ │ eu-west-1    0.9      ⚠ outdated 14d ago    [Update] │   │
│ │ us-west-1    1.0      ⏳ creating 5m ago    [Logs]   │   │
│ └──────────────────────────────────────────────────────┘   │
└─────────────────────────────────────────────────────────────┘
```

## Cluster Provisioning Integration

The cluster provisioning workflow (`auto-setup-irsa.sh`) remains unchanged. It:

1. Checks SSM Parameter Store for snapshot
2. Falls back to EC2 tag discovery
3. Falls back to S3 import if no snapshot found
4. On S3 fallback, creates snapshot for future use

**Key insight**: Database is for admin tracking only. Cluster provisioning uses SSM/EC2 for snapshot discovery to maintain decoupling and resilience.

## Migration Guide

### Database Migration

Run migration 00064:
```bash
./ocpctl-api migrate
```

### API Deployment

Deploy updated API and worker binaries:
```bash
./scripts/deploy.sh
```

### UI Deployment

Deploy updated Next.js frontend:
```bash
cd web && npm run build && npm run deploy
```

## Security Considerations

1. **Admin-only access**: All snapshot management endpoints require admin role
2. **Audit logging**: All snapshot operations logged to `windows_snapshot_audit`
3. **IAM permissions**: Worker needs EC2 snapshot and SSM permissions
4. **Validation required**: Snapshots must boot successfully before marked as "ready"

## Cost Analysis

### Current State (No Pre-created Snapshots)
- First VM deployment in region: 30-50 minutes (S3 import)
- Creates snapshot automatically: +40-50 minutes (one-time)
- Future VMs in region: 2-3 minutes (snapshot restore)

### With Admin-Created Snapshots
- Admin creates snapshot once per region: 40-80 minutes (depending on approach)
- All VM deployments: 2-3 minutes (snapshot restore)
- **ROI**: After 2 Windows VMs in a region, admin snapshot creation pays off

### Snapshot Storage Cost
- ~35GB EBS snapshot per region
- ~$1.75/month per region
- 15 regions = ~$26/month total
- **Compared to**: Saving 30-47 minutes × $0.50/hour = $0.25-$0.39 per VM deployment

## Future Enhancements

1. **Automated snapshot updates**: Trigger snapshot creation when new Windows image uploaded to S3
2. **Multi-version support**: Maintain multiple Windows image versions simultaneously
3. **Snapshot expiration**: Auto-delete outdated snapshots after new version validated
4. **Regional quotas**: Automatically request snapshot quota increases
5. **Snapshot replication**: Copy snapshots across regions for DR
6. **Performance metrics**: Track VM boot times to validate snapshot health

## References

- Database Migration: `internal/store/migrations/00064_windows_snapshots.sql`
- API Handler: `internal/api/handler_windows_snapshots.go`
- Store Methods: `internal/store/windows_snapshots.go`
- Worker Handler: `internal/worker/handler_windows_snapshot.go`
- Type Definitions: `pkg/types/windows_snapshot.go`
- Cluster Provisioning: `manifests/windows-vm/auto-setup-irsa.sh`
