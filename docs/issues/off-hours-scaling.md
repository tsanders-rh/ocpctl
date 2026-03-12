# Feature: Off-Hours Worker Scaling for Cost Optimization

## Summary

Implement automatic scaling of worker nodes during off-hours to reduce costs while maintaining cluster availability. Unlike hibernation (which stops the entire cluster), off-hours scaling keeps the control plane running but scales down worker nodes to a minimum during non-business hours.

## Motivation

**Problem**: Work hours hibernation completely shuts down clusters during off-hours, making them inaccessible. Some users need 24/7 cluster access but can tolerate reduced capacity outside business hours.

**Use Cases**:
- Production clusters that need continuous monitoring/alerting
- Shared dev/test environments used across timezones
- Clusters running background jobs that can tolerate slower execution
- CI/CD pipelines that may trigger outside business hours
- Clusters with persistent services (databases, caches) that need to stay running

**Cost Savings**: 30-50% reduction (depending on worker count and instance types), less than hibernation (~70%) but with continuous availability.

## Feature Comparison

| Feature | Work Hours Hibernation | Off-Hours Scaling |
|---------|----------------------|-------------------|
| **Cluster State** | HIBERNATED (stopped) | READY (running) |
| **Control Plane** | Stopped | Running |
| **Worker Nodes** | Stopped | Scaled to minimum |
| **Accessibility** | None | Full (reduced capacity) |
| **Cost Savings** | ~70% | 30-50% |
| **Resume Time** | 5-10 minutes | Seconds (node startup) |
| **Use Case** | Dev/test clusters | Shared/production clusters |
| **Platform Support** | AWS only | AWS, IBM Cloud (MachineSet API) |

## Technical Design

### Architecture Overview

```
User Profile
    ↓
    work_hours_enabled = true
    work_hours_start = "09:00"
    work_hours_end = "17:00"
    work_days = 62 (Mon-Fri bitmask)
    ↓
Cluster
    ↓
    profile.features.off_hours_scaling = true
    profile.compute.workers.min_replicas = 1
    profile.compute.workers.max_replicas = 5
    ↓
Janitor (every 5 min)
    ↓
    Check work hours status
    ↓
    ┌─────────────────────────────────────┐
    │ Within work hours?                  │
    └─────────────────────────────────────┘
           │                    │
           YES                  NO
           │                    │
           ↓                    ↓
    Scale to max          Scale to min
    (max_replicas)        (min_replicas)
           │                    │
           ↓                    ↓
    Update MachineSet     Update MachineSet
```

### Database Schema Changes

No new tables needed - leverage existing work hours configuration:

```sql
-- Clusters already have work hours fields (from hibernation feature)
-- work_hours_enabled, work_hours_start, work_hours_end, work_days

-- Add tracking for last scaling action
ALTER TABLE clusters ADD COLUMN last_scaling_check TIMESTAMP WITH TIME ZONE;
ALTER TABLE clusters ADD COLUMN current_worker_count INT;

CREATE INDEX idx_clusters_off_hours_scaling
  ON clusters(status)
  WHERE status = 'READY';
```

### Profile Configuration

Profiles already have the necessary fields defined:

```yaml
# Profile: aws-standard.yaml
compute:
  workers:
    replicas: 5          # Initial/desired count
    minReplicas: 1       # Off-hours minimum
    maxReplicas: 5       # Business hours maximum
    instanceType: m6i.2xlarge
    autoscaling: false   # OpenShift cluster-autoscaler (separate feature)

features:
  offHoursScaling: true  # Enable off-hours scaling for this profile
```

**Key Constraints**:
- `minReplicas` must be >= 0 (0 = no workers during off-hours)
- `maxReplicas` must be >= `minReplicas`
- Off-hours scaling only applies to profiles where `features.offHoursScaling = true`
- SNO profiles (replicas=0) cannot use off-hours scaling

### Implementation Components

#### 1. Janitor Off-Hours Scaling Task

**File**: `internal/janitor/off_hours_scaling.go`

```go
// enforceOffHoursScaling scales worker nodes based on work hours
func (j *Janitor) enforceOffHoursScaling(ctx context.Context) error {
    // Get clusters that need off-hours scaling enforcement
    clusters, err := j.store.Clusters.GetClustersForOffHoursScaling(ctx)
    if err != nil {
        return err
    }

    for _, cluster := range clusters {
        // Get profile to check if off-hours scaling is enabled
        profile, err := j.profileRegistry.Get(cluster.Profile)
        if err != nil || !profile.Features.OffHoursScaling {
            continue
        }

        // Determine if within work hours
        isWithinWorkHours := j.calculateWithinWorkHours(cluster, user)

        // Determine target worker count
        targetWorkerCount := profile.Compute.Workers.MinReplicas
        if isWithinWorkHours {
            targetWorkerCount = profile.Compute.Workers.MaxReplicas
        }

        // Get current worker count from cluster
        currentCount, err := j.getWorkerCount(ctx, cluster)
        if err != nil {
            log.Printf("Failed to get worker count for cluster %s: %v", cluster.ID, err)
            continue
        }

        // Scale if needed
        if currentCount != targetWorkerCount {
            if err := j.scaleWorkers(ctx, cluster, targetWorkerCount); err != nil {
                log.Printf("Failed to scale workers for cluster %s: %v", cluster.ID, err)
                continue
            }

            // Update tracking
            j.store.Clusters.UpdatePartial(ctx, cluster.ID, map[string]interface{}{
                "current_worker_count": targetWorkerCount,
                "last_scaling_check":   time.Now(),
            })

            // Audit log
            j.logAudit(ctx, cluster.ID, "cluster.workers.scaled", map[string]interface{}{
                "from":          currentCount,
                "to":            targetWorkerCount,
                "reason":        "off_hours_scaling",
                "work_hours":    isWithinWorkHours,
            })
        } else {
            // No change needed, just update check time
            j.store.Clusters.UpdatePartial(ctx, cluster.ID, map[string]interface{}{
                "last_scaling_check": time.Now(),
            })
        }
    }

    return nil
}
```

#### 2. Worker Scaling Implementation

**File**: `internal/scaling/machineset.go`

OpenShift uses MachineSet resources for managing worker nodes:

```go
package scaling

import (
    "context"
    "fmt"
    "path/filepath"

    "k8s.io/client-go/kubernetes"
    "k8s.io/client-go/tools/clientcmd"
)

// MachineSetScaler handles scaling of OpenShift MachineSets
type MachineSetScaler struct {
    kubeconfigPath string
}

// NewMachineSetScaler creates a new scaler
func NewMachineSetScaler(clusterWorkDir string) *MachineSetScaler {
    return &MachineSetScaler{
        kubeconfigPath: filepath.Join(clusterWorkDir, "auth", "kubeconfig"),
    }
}

// GetWorkerCount returns current worker node count
func (s *MachineSetScaler) GetWorkerCount(ctx context.Context) (int, error) {
    // Load kubeconfig
    config, err := clientcmd.BuildConfigFromFlags("", s.kubeconfigPath)
    if err != nil {
        return 0, fmt.Errorf("failed to load kubeconfig: %w", err)
    }

    // Create clientset
    clientset, err := kubernetes.NewForConfig(config)
    if err != nil {
        return 0, fmt.Errorf("failed to create clientset: %w", err)
    }

    // Get nodes with role=worker
    nodes, err := clientset.CoreV1().Nodes().List(ctx, metav1.ListOptions{
        LabelSelector: "node-role.kubernetes.io/worker",
    })
    if err != nil {
        return 0, fmt.Errorf("failed to list worker nodes: %w", err)
    }

    return len(nodes.Items), nil
}

// ScaleWorkers sets the desired replica count on all worker MachineSets
func (s *MachineSetScaler) ScaleWorkers(ctx context.Context, targetCount int) error {
    // Use oc or kubectl to scale MachineSets
    // Example: oc scale machineset <name> --replicas=<count> -n openshift-machine-api

    // 1. Get all worker MachineSets
    // 2. For each MachineSet, update replicas to targetCount / len(machineSets)
    // 3. Handle rounding (distribute replicas evenly across AZs)

    return nil
}
```

**Alternative Approach**: Use `oc` CLI instead of k8s client-go:

```go
func (s *MachineSetScaler) ScaleWorkers(ctx context.Context, targetCount int) error {
    // Get MachineSets
    cmd := exec.CommandContext(ctx, "oc", "--kubeconfig", s.kubeconfigPath,
        "get", "machineset", "-n", "openshift-machine-api",
        "-o", "jsonpath={.items[?(@.spec.template.metadata.labels.machine\\.openshift\\.io/cluster-api-machine-role==\"worker\")].metadata.name}")

    output, err := cmd.CombinedOutput()
    if err != nil {
        return fmt.Errorf("failed to get machinesets: %w", err)
    }

    machineSets := strings.Fields(string(output))
    if len(machineSets) == 0 {
        return fmt.Errorf("no worker machinesets found")
    }

    // Calculate replicas per MachineSet (distribute evenly)
    replicasPerMS := targetCount / len(machineSets)
    remainder := targetCount % len(machineSets)

    // Scale each MachineSet
    for i, ms := range machineSets {
        replicas := replicasPerMS
        if i < remainder {
            replicas++ // Distribute remainder across first N MachineSets
        }

        cmd := exec.CommandContext(ctx, "oc", "--kubeconfig", s.kubeconfigPath,
            "scale", "machineset", ms, "-n", "openshift-machine-api",
            fmt.Sprintf("--replicas=%d", replicas))

        if err := cmd.Run(); err != nil {
            return fmt.Errorf("failed to scale machineset %s: %w", ms, err)
        }
    }

    return nil
}
```

#### 3. Store Query for Scaling Candidates

**File**: `internal/store/clusters.go`

```go
// GetClustersForOffHoursScaling returns clusters that need scaling enforcement
func (s *ClusterStore) GetClustersForOffHoursScaling(ctx context.Context) ([]*types.Cluster, error) {
    query := `
        SELECT c.*, u.timezone, u.work_hours_enabled, u.work_hours_start, u.work_hours_end, u.work_days
        FROM clusters c
        JOIN users u ON c.user_id = u.id
        WHERE c.status = 'READY'
          AND (
            -- Cluster has work hours override enabled
            (c.work_hours_enabled = true)
            OR
            -- Cluster inherits user's work hours (NULL = inherit)
            (c.work_hours_enabled IS NULL AND u.work_hours_enabled = true)
          )
          -- Only check every 5 minutes (align with janitor interval)
          AND (c.last_scaling_check IS NULL OR c.last_scaling_check < NOW() - INTERVAL '4 minutes')
        ORDER BY c.last_scaling_check ASC NULLS FIRST
    `

    rows, err := s.db.Query(ctx, query)
    if err != nil {
        return nil, err
    }
    defer rows.Close()

    var clusters []*types.Cluster
    for rows.Next() {
        // Scan cluster and user work hours
        // ...
    }

    return clusters, nil
}
```

### Integration with Janitor

**File**: `internal/janitor/janitor.go`

```go
func (j *Janitor) run() {
    // ... existing tasks ...

    // Off-hours scaling enforcement
    if err := j.enforceOffHoursScaling(j.ctx); err != nil {
        log.Printf("Error enforcing off-hours scaling: %v", err)
    }
}
```

### Frontend Updates

**1. Cluster Creation Form**

Show off-hours scaling option when profile supports it:

```tsx
{selectedProfile?.features?.off_hours_scaling && (
  <div className="space-y-2">
    <div className="flex items-center space-x-2">
      <Checkbox
        id="offhours_opt_in"
        checked={watchedValues.offhours_opt_in}
        onCheckedChange={(checked) => setValue("offhours_opt_in", checked as boolean)}
      />
      <Label htmlFor="offhours_opt_in">
        Enable off-hours scaling
      </Label>
    </div>
    <p className="text-sm text-muted-foreground ml-6">
      Automatically scale workers from {selectedProfile.compute.workers.min_replicas}
      (off-hours) to {selectedProfile.compute.workers.max_replicas} (work hours)
      to reduce costs while maintaining cluster availability
    </p>
    {selectedProfile.cost_controls?.estimated_hourly_cost && (
      <p className="text-sm text-green-600 ml-6">
        Estimated savings: ${((selectedProfile.cost_controls.estimated_hourly_cost * 0.4) * 16).toFixed(2)}/day
      </p>
    )}
  </div>
)}
```

**2. Cluster Detail Page**

Add worker scaling status to Work Hours Schedule card:

```tsx
{cluster.profile.features?.off_hours_scaling && cluster.offhours_opt_in && (
  <div className="mt-4 pt-4 border-t">
    <div className="text-sm font-medium text-muted-foreground mb-2">Worker Scaling</div>
    <div className="space-y-2">
      <div className="flex justify-between">
        <span className="text-sm">Current Workers:</span>
        <span className="text-sm font-medium">{cluster.current_worker_count || 'Unknown'}</span>
      </div>
      <div className="flex justify-between">
        <span className="text-sm">Off-Hours Minimum:</span>
        <span className="text-sm font-medium">{cluster.profile.compute.workers.min_replicas}</span>
      </div>
      <div className="flex justify-between">
        <span className="text-sm">Work Hours Maximum:</span>
        <span className="text-sm font-medium">{cluster.profile.compute.workers.max_replicas}</span>
      </div>
      <div className="flex justify-between">
        <span className="text-sm">Next Check:</span>
        <span className="text-sm text-muted-foreground">
          {cluster.last_scaling_check
            ? `${Math.ceil((Date.now() - new Date(cluster.last_scaling_check).getTime()) / 60000)} min ago`
            : 'Pending'}
        </span>
      </div>
    </div>
  </div>
)}
```

## Implementation Phases

### Phase 1: Infrastructure (Week 1-2)
- [ ] Add database fields (`last_scaling_check`, `current_worker_count`)
- [ ] Implement `MachineSetScaler` package with `oc` CLI integration
- [ ] Add store method `GetClustersForOffHoursScaling()`
- [ ] Write unit tests for scaling logic

### Phase 2: Janitor Integration (Week 2-3)
- [ ] Implement `enforceOffHoursScaling()` in janitor
- [ ] Reuse existing work hours calculation logic from hibernation
- [ ] Add audit logging for scaling events
- [ ] Test with manual scaling on test cluster

### Phase 3: Profile Configuration (Week 3)
- [ ] Update profile definitions to enable off-hours scaling
- [ ] Set min/max replicas for each profile
- [ ] Document profile configuration in README
- [ ] Validate profiles with new feature enabled

### Phase 4: Frontend (Week 3-4)
- [ ] Update cluster creation form to show off-hours scaling option
- [ ] Add worker scaling status to cluster detail page
- [ ] Update cluster list to show current worker count
- [ ] Add tooltips explaining cost savings

### Phase 5: Testing & Refinement (Week 4-5)
- [ ] E2E test: Create cluster with off-hours scaling enabled
- [ ] Verify scaling up at work hours start
- [ ] Verify scaling down at work hours end
- [ ] Test edge cases (timezone changes, DST, midnight wraparound)
- [ ] Load testing with multiple clusters scaling simultaneously
- [ ] Monitor AWS API rate limits

### Phase 6: Documentation (Week 5)
- [ ] User guide for off-hours scaling
- [ ] Comparison table: Hibernation vs Scaling vs Always-On
- [ ] Cost calculator tool
- [ ] Admin runbook for troubleshooting scaling issues

## Testing Strategy

### Unit Tests
- `MachineSetScaler.GetWorkerCount()` - mock kubeconfig access
- `MachineSetScaler.ScaleWorkers()` - mock `oc` commands
- `calculateWithinWorkHours()` - edge cases (midnight, DST, timezones)
- Profile validation with off-hours scaling enabled

### Integration Tests
- Janitor scaling enforcement loop
- Database queries for scaling candidates
- Audit log creation for scaling events
- Work hours inheritance (cluster override vs user default)

### E2E Tests
1. **Basic Scaling**
   - Create cluster with off-hours scaling enabled
   - Wait for off-hours (or manually set time)
   - Verify workers scale down to min replicas
   - Enter work hours
   - Verify workers scale up to max replicas

2. **Edge Cases**
   - Cluster created during off-hours → should start with min workers
   - User changes work hours mid-day → scaling adjusts on next janitor cycle
   - Cluster destroyed during scaling → graceful cancellation
   - Kubeconfig inaccessible → log error, don't crash janitor

3. **Concurrency**
   - 10 clusters all scaling simultaneously
   - Verify no race conditions
   - Check AWS API rate limits aren't exceeded

### Manual Verification
- Monitor AWS EC2 console during scaling events
- Verify instances terminate/launch correctly
- Check MachineSet status in OpenShift console
- Validate cost reduction in AWS Cost Explorer

## Edge Cases and Error Handling

### Edge Cases

1. **Zero Workers During Off-Hours**
   - Allow `minReplicas = 0` for maximum savings
   - Control plane remains schedulable if configured
   - Workloads may fail if control plane can't handle load

2. **Scaling During Active Workloads**
   - Node eviction follows standard Kubernetes graceful termination
   - PodDisruptionBudgets respected
   - If eviction fails, node remains until next cycle

3. **Platform Differences**
   - AWS: MachineSet API works reliably
   - IBM Cloud: MachineSet API availability TBD
   - GCP/Azure: Future support (MachineSet API universal)

4. **Cluster Hibernation Takes Precedence**
   - If cluster is HIBERNATED, off-hours scaling doesn't apply
   - Scaling only applies to READY clusters
   - Feature flag in profile determines which feature to use

5. **Multiple Availability Zones**
   - Distribute replicas evenly across AZs
   - Example: 5 workers, 3 AZs → [2, 2, 1] distribution
   - Maintain HA during both work hours and off-hours

### Error Handling

1. **Kubeconfig Missing/Invalid**
   - Log warning: "Cannot scale cluster X: kubeconfig not found"
   - Don't mark cluster as failed
   - Retry on next janitor cycle

2. **MachineSet Not Found**
   - Possible if cluster is SNO (no workers)
   - Log info: "No worker machinesets found, skipping scaling"
   - Don't create error event

3. **Scaling Timeout**
   - If nodes don't come up within 10 minutes
   - Log error but don't fail
   - Next janitor cycle will retry

4. **AWS API Rate Limits**
   - Janitor runs every 5 minutes (12 times/hour)
   - Each cluster check = 2-3 API calls
   - Rate limit: ~100 clusters before throttling
   - Mitigation: Exponential backoff on errors

5. **Concurrent Scaling Operations**
   - Use cluster lock mechanism (existing infrastructure)
   - Prevent manual scaling while janitor is scaling
   - Prevent multiple janitor instances from conflicting

## Monitoring and Metrics

### Prometheus Metrics
- `ocpctl_worker_scaling_total{cluster_id, from_count, to_count}` - Counter
- `ocpctl_worker_scaling_errors_total{cluster_id, error_type}` - Counter
- `ocpctl_worker_scaling_duration_seconds{cluster_id}` - Histogram
- `ocpctl_current_worker_count{cluster_id}` - Gauge

### Audit Events
- `cluster.workers.scaled` - Worker count changed
- `cluster.workers.scaling_failed` - Scaling operation failed
- Include metadata: from_count, to_count, reason, work_hours status

### Grafana Dashboard
- Current worker count per cluster (gauge)
- Scaling events timeline (graph)
- Error rate (graph)
- Estimated cost savings (calculated metric)

## Cost Analysis

### Example: AWS Standard Profile
- **Control Plane**: 3 × m6i.2xlarge = $1.152/hr (always running)
- **Workers**: 5 × m6i.2xlarge = $1.920/hr (work hours only)
- **Off-Hours Workers**: 1 × m6i.2xlarge = $0.384/hr

**Without Off-Hours Scaling**:
- 24 hours × ($1.152 + $1.920) = **$73.73/day**

**With Off-Hours Scaling** (16 hours off, 8 hours on):
- Work hours (8h): $1.152 + $1.920 = $3.072 × 8 = $24.58
- Off-hours (16h): $1.152 + $0.384 = $1.536 × 16 = $24.58
- **Total**: **$49.15/day** (~33% savings)

**With Hibernation**:
- Work hours (8h): $3.072 × 8 = $24.58
- Off-hours (16h): EBS storage only = ~$2/day
- **Total**: **$26.58/day** (~64% savings)

## Security Considerations

1. **Kubeconfig Access**
   - Janitor needs read access to cluster kubeconfigs
   - Stored in `/opt/ocpctl/clusters/{id}/auth/kubeconfig`
   - Already protected by filesystem permissions

2. **RBAC Permissions**
   - Janitor kubeconfig needs `update` on `machinesets`
   - Use cluster-admin from install (already has permissions)
   - No additional RBAC configuration needed

3. **Audit Logging**
   - All scaling events logged with actor (janitor system account)
   - Include rationale (work_hours enforcement)
   - Visible in admin audit log dashboard

## Alternatives Considered

### 1. Use Cluster Autoscaler Instead
- **Pros**: Native OpenShift feature, load-based scaling
- **Cons**: Doesn't scale based on time, requires metrics, more complex
- **Decision**: Off-hours scaling is simpler and more predictable

### 2. Use CronJobs to Scale
- **Pros**: Built into Kubernetes, no custom code
- **Cons**: Requires per-cluster configuration, not centralized, harder to manage
- **Decision**: Janitor-based approach is more consistent with existing architecture

### 3. Scale Control Plane Too
- **Pros**: Maximum cost savings
- **Cons**: Complex, risky (affects cluster stability), not supported by OpenShift
- **Decision**: Only scale workers (safe, well-supported)

## Dependencies

- **oc CLI**: Must be available in PATH on janitor/worker node
- **Kubeconfig**: Must exist in cluster work directory
- **OpenShift 4.20+**: MachineSet API stable
- **Kubernetes client-go** (optional): If we use Go client instead of `oc` CLI

## Success Criteria

1. ✅ Janitor successfully scales workers based on work hours
2. ✅ Cost savings of 30-50% achieved for profiles with off-hours scaling
3. ✅ No cluster downtime during scaling operations
4. ✅ UI clearly shows current worker count and next scaling action
5. ✅ Audit logs capture all scaling events with full context
6. ✅ Feature works reliably for 100+ clusters
7. ✅ Documentation enables users to configure and understand feature

## Future Enhancements

- **Per-Cluster Schedule Override**: Allow users to set custom work hours per cluster (already supported in DB schema)
- **Load-Based Scaling**: Combine time-based with load-based scaling (integrate with Cluster Autoscaler)
- **Cost Dashboard**: Real-time cost tracking showing actual savings from off-hours scaling
- **Predictive Scaling**: ML-based prediction of when to scale up/down based on usage patterns
- **Multi-Region Support**: Coordinate scaling across clusters in different regions/timezones
- **Notification Before Scaling**: Warn users 5 minutes before scaling down workers

## References

- OpenShift MachineSet Documentation: https://docs.openshift.com/container-platform/4.20/machine_management/index.html
- Work Hours Hibernation Implementation: See GitHub issue #10
- Janitor Architecture: `docs/architecture/janitor.md`
- AWS EC2 Pricing: https://aws.amazon.com/ec2/pricing/on-demand/
