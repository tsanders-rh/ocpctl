# Cluster Pools Feature

## Executive Summary

Cluster Pools provide pre-provisioned, instantly available clusters for CI/CD pipelines and rapid testing. Instead of waiting 30-60 minutes for cluster provisioning, users can lease a ready cluster in seconds.

**Key Benefits:**
- **Instant Access**: Lease clusters in seconds instead of waiting 30-60 minutes
- **CI/CD Integration**: Perfect for automated testing pipelines
- **Cost Optimization**: Pools automatically scale based on demand and work hours
- **Self-Service**: Users can browse and lease clusters via Web UI or API
- **Auto-Release**: Clusters automatically return to pool after lease expires

**Use Cases:**
- CI/CD pipeline testing (GitHub Actions, Jenkins, Tekton)
- Rapid development iterations
- Demo and presentation environments
- Automated integration testing
- Temporary cluster needs

---

## Architecture Overview

### Components

```
┌─────────────────────────────────────────────────────────────────┐
│                         Cluster Pools                            │
├─────────────────────────────────────────────────────────────────┤
│                                                                   │
│  ┌──────────────┐   ┌──────────────┐   ┌──────────────┐        │
│  │   READY      │   │   LEASED     │   │ PROVISIONING │        │
│  │  Cluster A   │   │  Cluster B   │   │  Cluster D   │        │
│  │  Cluster C   │   │              │   │              │        │
│  └──────────────┘   └──────────────┘   └──────────────┘        │
│                                                                   │
│  Pool Manager (Background Scheduler):                            │
│  - Maintains target_size READY clusters                          │
│  - Auto-releases expired leases                                  │
│  - Cleans and refreshes aged clusters                           │
│  - Scales down during off-hours (optional)                       │
│                                                                   │
└─────────────────────────────────────────────────────────────────┘

User Actions:
  1. Browse pools (/pools)
  2. Lease cluster (instant)
  3. Use cluster
  4. Release when done (or auto-release)
  5. Cluster returns to READY state
```

### Pool States

**Cluster Pool States:**
- **READY**: Available for immediate lease
- **LEASED**: Currently in use by a user/job
- **PROVISIONING**: Being created to replenish pool
- **CLEANING**: Being reset before returning to READY
- **EXPIRED**: Lease expired, awaiting cleanup

### Database Schema

**cluster_pools table:**
```sql
- id (UUID)
- name (unique identifier, e.g., "dev-pool")
- display_name (user-facing name)
- profile (cluster profile to use)
- target_size (desired READY clusters)
- min_size / max_size (bounds)
- max_lease_duration_hours (auto-release timeout)
- auto_release_enabled (boolean)
- max_cluster_age_days (refresh threshold)
- scheduled_mode (work hours only)
- schedule_timezone, schedule_start_hour, schedule_end_hour
- cluster_config (JSON overrides)
```

**clusters table additions:**
```sql
- pool_id (references cluster_pools)
- pool_state (READY, LEASED, PROVISIONING, etc.)
- leased_by (username/job ID)
- leased_at, lease_expires_at
- lease_metadata (JSON, stores context)
- pool_generation (incremented each cycle)
- last_cleaned_at
```

---

## API Endpoints

### Public Endpoints (All Authenticated Users)

#### List Enabled Pools
```
GET /api/v1/pools?enabled_only=true
```

**Response:**
```json
{
  "pools": [
    {
      "id": "uuid",
      "name": "dev-pool",
      "display_name": "Development Pool",
      "description": "Fast clusters for dev testing",
      "profile": "aws-sno-ga",
      "target_size": 3,
      "max_lease_duration_hours": 2,
      "enabled": true,
      "scheduled_mode": false
    }
  ]
}
```

#### Get Pool Statistics
```
GET /api/v1/pools/:pool_name/stats
```

**Response:**
```json
{
  "pool_id": "uuid",
  "pool_name": "dev-pool",
  "total_clusters": 5,
  "ready_clusters": 3,
  "leased_clusters": 1,
  "provisioning_clusters": 1,
  "cleaning_clusters": 0,
  "expired_clusters": 0
}
```

#### Lease Cluster
```
POST /api/v1/pools/:pool_name/lease
Content-Type: application/json

{
  "leased_by": "jenkins-job-123",
  "duration": 120,  // optional, minutes
  "metadata": {
    "job_id": "123",
    "build_url": "https://jenkins.example.com/job/123"
  }
}
```

**Response:**
```json
{
  "cluster_id": "uuid",
  "cluster_name": "dev-pool-abc123",
  "leased_by": "jenkins-job-123",
  "leased_at": "2026-05-22T10:00:00Z",
  "lease_expires_at": "2026-05-22T12:00:00Z",
  "api_url": "https://api.cluster.example.com:6443",
  "console_url": "https://console-openshift-console.apps.cluster.example.com",
  "kubeconfig_path": "s3://ocpctl-artifacts/clusters/{id}/kubeconfig"
}
```

#### Release Cluster
```
POST /api/v1/pools/clusters/:cluster_id/release
```

**Response:** 204 No Content

### Admin Endpoints

#### Create Pool
```
POST /api/v1/admin/pools
Content-Type: application/json

{
  "name": "dev-pool",
  "display_name": "Development Pool",
  "description": "Fast SNO clusters for development",
  "profile": "aws-sno-ga",
  "target_size": 3,
  "min_size": 1,
  "max_size": 5,
  "max_lease_duration_hours": 2,
  "auto_release_enabled": true,
  "max_cluster_age_days": 7,
  "scheduled_mode": false,
  "cluster_config": {
    "region": "us-east-1",
    "tags": {
      "pool": "dev-pool"
    }
  }
}
```

#### List All Pools (Admin)
```
GET /api/v1/admin/pools
```

#### Get Pool Details
```
GET /api/v1/admin/pools/:name
```

#### Update Pool
```
PATCH /api/v1/admin/pools/:name
Content-Type: application/json

{
  "target_size": 5,
  "max_lease_duration_hours": 4
}
```

#### Delete Pool
```
DELETE /api/v1/admin/pools/:name
```

**Note:** Deletes pool but orphans existing clusters (sets pool_id to NULL). Clusters continue running.

---

## Pool Manager (Background Scheduler)

The pool manager runs as a background goroutine and performs:

### 1. Pool Replenishment
- **Trigger**: ready_clusters < target_size
- **Action**: Create new clusters with pool_state=PROVISIONING
- **Frequency**: Every 30 seconds
- **Bounds**: Respects min_size and max_size

### 2. Lease Expiration
- **Trigger**: lease_expires_at < NOW()
- **Action**: Update pool_state to EXPIRED, queue for release
- **Frequency**: Every 60 seconds

### 3. Cluster Aging/Refresh
- **Trigger**: cluster age > max_cluster_age_days
- **Action**: Destroy and replenish with fresh cluster
- **Frequency**: Daily check
- **Purpose**: Prevent stale clusters, ensure latest profiles

### 4. Work Hours Enforcement (Optional)
- **Trigger**: scheduled_mode=true and outside work hours
- **Action**: Scale pool to 0 (destroy READY clusters, block new provisions)
- **Frequency**: Hourly check
- **Example**: 8am-6pm EST, Monday-Friday

### 5. Pool Scaling
- **Dynamic**: Adjusts target_size based on demand patterns
- **Cost-aware**: Scales down idle pools
- **Manual**: Admin can adjust target_size via API/UI

---

## CI/CD Integration Examples

### GitHub Actions

```yaml
name: Integration Tests
on: [push, pull_request]

jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v3

      - name: Lease Cluster
        id: lease
        run: |
          RESPONSE=$(curl -X POST \
            -H "Authorization: Bearer ${{ secrets.OCPCTL_TOKEN }}" \
            -H "Content-Type: application/json" \
            -d '{"leased_by": "github-${{ github.run_id }}", "metadata": {"repo": "${{ github.repository }}", "run_id": "${{ github.run_id }}"}}' \
            https://ocpctl.mg.dog8code.com/api/v1/pools/ci-pool/lease)

          CLUSTER_ID=$(echo $RESPONSE | jq -r '.cluster_id')
          KUBECONFIG_PATH=$(echo $RESPONSE | jq -r '.kubeconfig_path')

          echo "cluster_id=$CLUSTER_ID" >> $GITHUB_OUTPUT
          echo "kubeconfig_path=$KUBECONFIG_PATH" >> $GITHUB_OUTPUT

      - name: Download Kubeconfig
        run: |
          aws s3 cp ${{ steps.lease.outputs.kubeconfig_path }} ./kubeconfig
          export KUBECONFIG=./kubeconfig

      - name: Run Tests
        run: |
          kubectl get nodes
          # Your tests here

      - name: Release Cluster
        if: always()
        run: |
          curl -X POST \
            -H "Authorization: Bearer ${{ secrets.OCPCTL_TOKEN }}" \
            https://ocpctl.mg.dog8code.com/api/v1/pools/clusters/${{ steps.lease.outputs.cluster_id }}/release
```

### Jenkins Pipeline

```groovy
pipeline {
    agent any
    environment {
        OCPCTL_TOKEN = credentials('ocpctl-api-token')
    }
    stages {
        stage('Lease Cluster') {
            steps {
                script {
                    def response = sh(
                        script: """
                            curl -X POST -H "Authorization: Bearer ${OCPCTL_TOKEN}" \
                            -H "Content-Type: application/json" \
                            -d '{"leased_by": "jenkins-${BUILD_ID}"}' \
                            https://ocpctl.mg.dog8code.com/api/v1/pools/ci-pool/lease
                        """,
                        returnStdout: true
                    ).trim()

                    def json = readJSON text: response
                    env.CLUSTER_ID = json.cluster_id
                    env.KUBECONFIG_PATH = json.kubeconfig_path
                }
            }
        }

        stage('Run Tests') {
            steps {
                sh """
                    aws s3 cp ${KUBECONFIG_PATH} ./kubeconfig
                    export KUBECONFIG=./kubeconfig
                    kubectl get nodes
                    # Your tests here
                """
            }
        }
    }
    post {
        always {
            sh """
                curl -X POST -H "Authorization: Bearer ${OCPCTL_TOKEN}" \
                https://ocpctl.mg.dog8code.com/api/v1/pools/clusters/${CLUSTER_ID}/release
            """
        }
    }
}
```

### Tekton Task

```yaml
apiVersion: tekton.dev/v1beta1
kind: Task
metadata:
  name: ocpctl-lease-cluster
spec:
  params:
    - name: pool-name
      type: string
      default: "ci-pool"
  results:
    - name: cluster-id
    - name: kubeconfig-path
  steps:
    - name: lease
      image: curlimages/curl:latest
      script: |
        #!/bin/sh
        RESPONSE=$(curl -X POST \
          -H "Authorization: Bearer $(cat /secrets/token)" \
          -H "Content-Type: application/json" \
          -d "{\"leased_by\": \"tekton-$(context.pipelineRun.name)\"}" \
          https://ocpctl.mg.dog8code.com/api/v1/pools/$(params.pool-name)/lease)

        echo $RESPONSE | jq -r '.cluster_id' > $(results.cluster-id.path)
        echo $RESPONSE | jq -r '.kubeconfig_path' > $(results.kubeconfig-path.path)
```

---

## Cost Analysis

### Without Pools (Traditional Provisioning)
- **Wait Time**: 30-60 minutes per cluster
- **CI/CD Impact**: Slow feedback loops
- **Cluster Utilization**: Low (created on-demand, destroyed after)

### With Pools
- **Wait Time**: < 5 seconds (instant lease)
- **Pool Cost**: 3 SNO clusters × $0.38/hr = $1.14/hr (24/7)
- **Pool Cost with Work Hours**: $1.14/hr × 10 hours/day × 5 days/week = $57/week
- **Savings**: Faster development cycles, improved developer productivity

**Example ROI:**
- Developer hourly cost: $100/hr
- Time saved per cluster: 45 minutes
- Clusters created per day: 10
- **Time saved**: 7.5 hours/day = $750/day
- **Pool cost**: $1.14/hr × 10 hours = $11.40/day
- **Net savings**: $738.60/day

---

## Security Considerations

### Lease Isolation
- Each lease is tied to a specific user/job identity
- Lease metadata captures context (job ID, build URL, etc.)
- Audit trail: all lease/release events logged

### Auto-Release
- Prevents cluster hoarding
- Enforces max_lease_duration_hours
- Background job auto-releases expired leases

### Pool Access Control
- Pools can be enabled/disabled by admins
- Only enabled pools visible to users
- Admin-only pool management endpoints

### Cluster Cleaning
- Future: Reset cluster state between leases
- Options: namespace cleanup, RBAC reset, network policy refresh

---

## Limitations & Future Enhancements

### Current Limitations
1. **No Cluster Cleaning**: Clusters return to pool without reset (future: automated cleanup)
2. **Single Profile per Pool**: Each pool uses one cluster profile
3. **No Priority Queues**: First-come-first-served leasing
4. **No Reservation System**: Cannot reserve clusters for future use

### Planned Enhancements
1. **Cluster Cleaning Job**: Automated reset between leases
2. **Multi-Profile Pools**: Support multiple profiles in one pool
3. **Lease Queuing**: Reserve clusters when pool empty
4. **Pool Analytics**: Usage metrics, cost tracking per pool
5. **Custom Lease Durations**: User-specified lease periods (within max)
6. **Lease Extensions**: Extend lease before expiration

---

## Monitoring & Metrics

### Key Metrics
- Pool health: ready_clusters / target_size ratio
- Lease utilization: leased_clusters / total_clusters
- Average lease duration
- Pool replenishment time
- Cost per lease

### Alerts
- Pool empty for > 10 minutes
- Provisioning failures
- Expired leases not releasing
- Pool costs exceeding budget

---

## References

- **User Guide**: [docs/user-guide/cluster-pools.md](../user-guide/cluster-pools.md)
- **API Documentation**: https://ocpctl.mg.dog8code.com/swagger/index.html
- **Cluster Management**: [docs/user-guide/cluster-management.md](../user-guide/cluster-management.md)
- **Architecture**: [docs/architecture/architecture.md](../architecture/architecture.md)
