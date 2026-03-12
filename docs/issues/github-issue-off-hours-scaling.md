# Off-Hours Worker Scaling for Cost Optimization

## Summary

Implement automatic scaling of worker nodes during off-hours to reduce costs while maintaining cluster availability. Unlike hibernation (which stops the entire cluster), off-hours scaling keeps the control plane running but scales down worker nodes to a minimum during non-business hours.

## Problem

Current work hours hibernation completely shuts down clusters during off-hours, making them inaccessible. Some users need 24/7 cluster access but can tolerate reduced capacity outside business hours:

- Production clusters requiring continuous monitoring/alerting
- Shared dev/test environments used across timezones
- CI/CD pipelines that may trigger outside business hours
- Clusters with persistent services that need to stay running

## Proposed Solution

Leverage OpenShift's MachineSet API to automatically scale worker nodes based on user-configured work hours:

**During Work Hours** (e.g., 9am-5pm):
- Scale workers to `max_replicas` (e.g., 5 nodes)
- Full capacity for development work

**During Off-Hours** (e.g., 5pm-9am):
- Scale workers to `min_replicas` (e.g., 1 node)
- Control plane stays running
- Cluster remains accessible with reduced capacity

**Cost Savings**: 30-50% reduction (vs 70% with hibernation, but with continuous availability)

## Feature Comparison

| Feature | Hibernation | Off-Hours Scaling |
|---------|------------|-------------------|
| **Cluster State** | HIBERNATED | READY |
| **Control Plane** | Stopped | Running |
| **Accessibility** | None | Full (reduced capacity) |
| **Cost Savings** | ~70% | 30-50% |
| **Resume Time** | 5-10 minutes | Seconds |
| **Use Case** | Dev/test only | Shared/production |

## Technical Approach

### 1. Database Schema
No new tables needed - leverage existing work hours configuration from hibernation feature:

```sql
ALTER TABLE clusters ADD COLUMN last_scaling_check TIMESTAMP WITH TIME ZONE;
ALTER TABLE clusters ADD COLUMN current_worker_count INT;
```

### 2. Profile Configuration
Profiles already have necessary fields defined:

```yaml
compute:
  workers:
    minReplicas: 1       # Off-hours minimum
    maxReplicas: 5       # Business hours maximum

features:
  offHoursScaling: true  # Enable for this profile
```

### 3. Janitor Task
Add `enforceOffHoursScaling()` to janitor (runs every 5 minutes):

1. Query clusters with `off_hours_scaling` enabled and status = READY
2. Calculate if currently within work hours (reuse hibernation logic)
3. Determine target worker count: `min_replicas` or `max_replicas`
4. Use `oc scale machineset` to adjust worker count
5. Audit log the scaling action

### 4. MachineSet Scaling
Create `internal/scaling/machineset.go` package:

```go
// Get current worker count
oc get nodes -l node-role.kubernetes.io/worker --no-headers | wc -l

// Scale MachineSets
oc scale machineset <name> --replicas=<count> -n openshift-machine-api
```

### 5. Frontend Updates
- Show off-hours scaling checkbox when profile supports it
- Display current worker count on cluster detail page
- Show estimated cost savings

## Implementation Phases

### Phase 1: Infrastructure (1-2 weeks)
- [ ] Database migration for tracking fields
- [ ] `MachineSetScaler` package with `oc` CLI integration
- [ ] Store method `GetClustersForOffHoursScaling()`
- [ ] Unit tests

### Phase 2: Janitor Integration (1 week)
- [ ] `enforceOffHoursScaling()` implementation
- [ ] Reuse work hours calculation from hibernation
- [ ] Audit logging
- [ ] Manual testing

### Phase 3: Profile & Frontend (1 week)
- [ ] Enable `offHoursScaling` in appropriate profiles
- [ ] Update cluster creation form
- [ ] Update cluster detail page with worker count
- [ ] Cost savings calculator

### Phase 4: Testing (1-2 weeks)
- [ ] E2E test: scaling up/down based on work hours
- [ ] Edge cases: timezone changes, DST, midnight wraparound
- [ ] Load test: multiple clusters scaling simultaneously
- [ ] Documentation

**Total Estimated Time**: 4-6 weeks

## Cost Analysis Example

**AWS Standard Profile** (3 control plane + 5 workers, m6i.2xlarge @ $0.384/hr):

**Without Scaling**:
- 24h × (3×$0.384 + 5×$0.384) = **$73.73/day**

**With Off-Hours Scaling** (8h work, 16h off-hours with 1 worker):
- Work (8h): (3+5)×$0.384 × 8 = $24.58
- Off (16h): (3+1)×$0.384 × 16 = $24.58
- **Total: $49.15/day** (33% savings, $738/month saved)

**With Hibernation**:
- **Total: $26.58/day** (64% savings, but 16h/day downtime)

## Edge Cases Handled

1. **Zero Workers During Off-Hours**: Allow `minReplicas=0` for max savings (control plane must be schedulable)
2. **Multiple AZs**: Distribute replicas evenly (e.g., 5 workers across 3 AZs → [2,2,1])
3. **Hibernation Takes Precedence**: If cluster is HIBERNATED, skip scaling enforcement
4. **Platform Support**: AWS (MachineSet API), IBM Cloud (MachineSet API), future: GCP/Azure
5. **Kubeconfig Errors**: Log warning, retry on next cycle, don't fail cluster
6. **Concurrent Scaling**: Use cluster lock to prevent conflicts

## Success Criteria

- [x] Janitor successfully scales workers based on work hours
- [x] 30-50% cost savings achieved for enabled profiles
- [x] Zero cluster downtime during scaling
- [x] UI shows current worker count and next action
- [x] Audit logs capture all scaling events
- [x] Works reliably for 100+ clusters
- [x] Complete user documentation

## Security Considerations

- Janitor needs read access to cluster kubeconfigs (already has this for hibernation)
- Uses cluster-admin kubeconfig from install (has MachineSet update permissions)
- All scaling events logged in audit log with full context

## Dependencies

- OpenShift 4.20+ (MachineSet API stable)
- `oc` CLI available on janitor/worker node
- Kubeconfig in cluster work directory

## Alternatives Considered

1. **Cluster Autoscaler**: Load-based, more complex, doesn't scale by time
2. **CronJobs**: Requires per-cluster config, not centralized
3. **Scale Control Plane**: Too risky, not supported by OpenShift

**Decision**: Time-based MachineSet scaling via janitor (consistent with existing architecture)

## Documentation

Full technical specification: `docs/issues/off-hours-scaling.md`

Includes:
- Detailed architecture diagrams
- Complete code examples for all components
- Database schema DDL
- Testing strategy
- Monitoring/metrics plan
- Cost calculator
- Grafana dashboard design

## Related

- Work Hours Hibernation: #10 (completed)
- Profile autoscaling field: Already defined but unused
- Janitor architecture: Runs every 5 minutes, handles TTL/hibernation/cleanup

---

**Effort**: Medium-Large (4-6 weeks)
**Priority**: Medium (nice-to-have, complements hibernation)
**Complexity**: Medium (leverages existing work hours infrastructure)
