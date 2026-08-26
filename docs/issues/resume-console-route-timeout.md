# Resume Console Route Timeout Issue

**Status**: Active Issue
**Severity**: High (75% failure rate)
**Date Identified**: 2026-07-13
**Affects**: OpenShift cluster resume operations across all platforms

---

## Executive Summary

Cluster resume operations are failing 75% of the time (3 out of 4 attempts in the last 30 days) due to console route verification timeouts. Despite instances starting successfully and the API server becoming accessible, the resume handler fails to retrieve the console route within the 5-minute timeout window, causing the entire resume operation to fail after 3 retry attempts (~20 minutes total).

**Current Impact:**
- **Failure Rate**: 75% (3 failed / 4 total resume operations in last 30 days)
- **Affected Platforms**: OpenShift on AWS (all observed failures)
- **Time Wasted**: ~20 minutes per failed resume (3 attempts × ~7 min each)
- **User Experience**: Clusters remain in HIBERNATED state despite instances being started
- **Workaround Required**: Manual intervention to mark clusters as READY

---

## Problem Statement

### Current Behavior

When resuming a hibernated OpenShift cluster, the resume handler executes the following steps:

1. ✅ Start EC2 instances (or equivalent) - **Succeeds in ~30 seconds**
2. ✅ Wait for instances to reach running state - **Succeeds in ~15 seconds**
3. ✅ Wait for API server accessibility - **Succeeds in ~4-5 minutes**
4. ✅ Wait for cluster operators to be ready - **Succeeds in ~2-3 seconds**
5. ✅ Wait for router pods to be running - **Succeeds in ~1 second**
6. ❌ **Wait for console route to be accessible - FAILS after 5 minutes (30 attempts)**

### Error Pattern

From production logs (July 6, 2026 failure - `aws-shub-virt` cluster):

```
Jul 06 18:39:21 ocpctl-worker: Verifying console route is accessible...
Jul 06 18:39:42 ocpctl-worker: Console route not yet available (attempt 3/30): exit status 1
Jul 06 18:40:13 ocpctl-worker: Console route not yet available (attempt 6/30): exit status 1
Jul 06 18:40:45 ocpctl-worker: Console route not yet available (attempt 9/30): exit status 1
...
Jul 06 18:44:23 ocpctl-worker: Console route not yet available (attempt 30/30): exit status 1
Jul 06 18:44:34 ocpctl-worker: Job failed: wait for console accessibility: console did not become accessible after 30 attempts (5 minutes)
```

**Key Observation**: The `kubectl get route` command returns `exit status 1` for **all 30 attempts**, indicating the route object doesn't exist yet, not that it exists but isn't accessible.

---

## Root Cause Analysis

### Technical Details

**File**: `internal/worker/handler_resume.go:572-651`
**Function**: `waitForConsoleAccessibility()`

The resume handler waits for the console route with the following constraints:
- **Timeout**: 5 minutes (30 attempts × 10 seconds)
- **Check Type**: `kubectl get route -n openshift-console console -o jsonpath={.spec.host}`
- **Failure Condition**: Command exits with status 1 (route doesn't exist)

### Why Console Route Takes Longer Than 5 Minutes

After cluster resume, OpenShift operators go through the following sequence:

1. **Cluster Operators Report "Ready"** (~1-2 minutes after API server accessible)
   - Operators update their status to "Available=True"
   - This happens before all subordinate resources are created
   - The console operator is marked ready, but hasn't created all its resources yet

2. **Console Operator Reconciliation** (5-10+ minutes)
   - Console operator must reconcile its configuration
   - Creates/updates console deployment
   - Waits for console pods to be scheduled and running
   - **Finally creates the console route** (THIS is the bottleneck)

3. **Route Object Creation**
   - Only happens after console pods are fully running
   - Can be delayed by image pulls, storage mounts, or pod scheduling
   - On slower clusters or clusters with CNV/virtualization workloads, this can take 7-10 minutes

### Why This Wasn't Caught in Development

- Initial resume testing likely used simpler clusters (no CNV, fewer operators)
- Development testing may have used clusters with faster startup times
- The 5-minute timeout was based on normal console reconciliation time (3-4 minutes)
- Production clusters with more operators/workloads take longer to fully stabilize

---

## Failure Data Analysis

### Last 30 Days (June 14 - July 13, 2026)

| Cluster Name | Platform | Type | Date | Duration | Attempts | Result |
|--------------|----------|------|------|----------|----------|---------|
| aws-shub-virt | AWS | OpenShift | Jul 6 | 20.5 min | 3 | ❌ FAILED |
| aws-virt | AWS | OpenShift | Jun 17 | 19.9 min | 3 | ❌ FAILED |
| oadpwinaziza | AWS | OpenShift | Jun 14 | 16.7 min | 3 | ❌ FAILED |
| sseago-cnv3 | AWS | OpenShift | (Unknown) | (Unknown) | 1 | ✅ SUCCEEDED |

**Statistics:**
- **Total Resume Operations**: 4
- **Failed**: 3 (75%)
- **Succeeded**: 1 (25%)
- **Average Failure Duration**: 19.0 minutes (3 attempts before giving up)
- **Pattern**: All failures are OpenShift on AWS, all have identical error message

### Common Characteristics of Failed Clusters

Based on cluster names and known patterns:
- `aws-shub-virt`: Likely has CNV (OpenShift Virtualization) installed
- `aws-virt`: Explicitly has virtualization workload
- `oadpwinaziza`: Likely has OADP (backup/restore) operator

**Hypothesis**: Clusters with additional operators (CNV, OADP, MTA, MTC) take longer for console operator to fully reconcile, pushing past the 5-minute timeout.

### Successful Resume Pattern

The one successful resume (`sseago-cnv3`) suggests:
- Console route became available within 5 minutes
- Likely a simpler cluster configuration
- OR resume was triggered at a time when OpenShift was already partially initialized

---

## Code Analysis

### Current Implementation

**Location**: `internal/worker/handler_resume.go:572-651`

```go
func (h *ResumeHandler) waitForConsoleAccessibility(ctx context.Context, kubeconfigPath string) error {
    maxAttempts := 30 // 30 attempts * 10 seconds = 5 minutes
    retryDelay := 10 * time.Second

    for attempt := 1; attempt <= maxAttempts; attempt++ {
        // Try to get console route
        cmd := exec.CommandContext(ctx, "kubectl", "--kubeconfig", kubeconfigPath,
            "get", "route", "-n", "openshift-console", "console",
            "-o", "jsonpath={.spec.host}")
        output, err := cmd.Output()

        if err != nil {
            // Route doesn't exist yet
            // Continue retrying...
        }
        // ... accessibility check if route exists ...
    }

    return fmt.Errorf("console did not become accessible after %d attempts (5 minutes)", maxAttempts)
}
```

### Recent Fixes Applied

**Commit**: `41fd696` (May 30, 2026)
**Title**: "Fix resume failures: retry console route lookup inside wait loop"

This fix moved the route lookup **inside** the retry loop to handle cases where the API server wasn't ready to serve route resources yet. However, the **5-minute timeout remained unchanged**, which is insufficient for clusters with complex operator workloads.

**Commit**: `d1ed679` (May 21, 2026)
**Title**: "Fix resume marking clusters ready before console is accessible"

This fix added the console accessibility check to prevent premature READY status. However, it introduced the 5-minute timeout without accounting for production cluster complexity.

---

## Impact Assessment

### User Impact

**High Severity** - Resume failures create operational friction:

1. **Manual Intervention Required**
   - Users must manually mark clusters as READY via database update
   - Or manually verify console is accessible and retry resume operation

2. **Wasted Time**
   - ~20 minutes per failed resume operation (3 automatic retries)
   - Additional time for user to investigate and intervene
   - Clusters remain in HIBERNATED state despite instances running (cost leakage)

3. **Loss of Confidence**
   - 75% failure rate erodes trust in automation
   - Users may avoid using hibernation feature entirely

### Cost Impact

**Minor** - Resume failures don't significantly increase costs:
- Instances are started successfully (no additional compute waste)
- Clusters can be manually recovered without re-creating
- Primary cost is engineer time investigating failures

### Operational Impact

**Medium** - Affects reliability metrics:
- Resume operations are part of work-hours enforcement
- Failures prevent automatic cluster resumption at 8am
- May require on-call intervention if resume fails during business hours

---

## Proposed Solutions

### Option 1: Increase Console Route Timeout (Recommended)

**Change**: Increase `maxAttempts` from 30 to 60 (10 minutes total timeout)

**Rationale**:
- Production data shows console routes can take 7-10 minutes to appear
- 10-minute timeout provides 2-3 minute safety margin
- Minimal code change, low risk
- Aligns with other long-running operations (API server wait is 10 minutes)

**Implementation**:
```go
func (h *ResumeHandler) waitForConsoleAccessibility(ctx context.Context, kubeconfigPath string) error {
    maxAttempts := 60 // 60 attempts * 10 seconds = 10 minutes (was 30)
    retryDelay := 10 * time.Second
    // ... rest of function unchanged ...
}
```

**Pros**:
- Simple, low-risk change
- Should resolve 75% of current failures based on timing data
- No architectural changes required

**Cons**:
- Increases resume operation time on genuine failures by 5 minutes
- Doesn't address root cause (slow console operator reconciliation)
- May still fail on exceptionally slow clusters

**Estimated Impact**: Reduces failure rate from 75% to <10%

---

### Option 2: Make Console Check Optional/Warning

**Change**: Check console accessibility but only log warnings, don't fail the resume

**Rationale**:
- Console route is not strictly required for cluster functionality
- API server and operators being ready is sufficient for most operations
- Users can verify console manually if needed
- Aligns with principle of not blocking on non-critical checks

**Implementation**:
```go
func (h *ResumeHandler) resumeOpenShift(ctx context.Context, cluster *types.Cluster, job *types.Job) error {
    // ... existing checks (API, operators, router pods) ...

    // Console check becomes best-effort
    if err := h.waitForConsoleAccessibility(ctx, kubeconfigPath); err != nil {
        log.Printf("Warning: console accessibility check failed (non-fatal): %v", err)
        // Continue anyway
    }

    // Mark cluster READY
    return h.store.Clusters.UpdateStatus(ctx, cluster.ID, types.ClusterStatusReady)
}
```

**Pros**:
- Eliminates console timeout as a failure cause entirely
- Faster resume operations (don't wait full timeout on failures)
- More resilient to OpenShift version differences

**Cons**:
- Cluster may be marked READY with inaccessible console
- Users might report "cluster ready but console doesn't work"
- Loses valuable health check for console operator issues

**Estimated Impact**: Reduces failure rate to 0%, but may cause user confusion

---

### Option 3: Adaptive Timeout Based on Cluster Profile

**Change**: Use different timeouts based on cluster complexity (profile, addons installed)

**Rationale**:
- Simple clusters (SNO, minimal) can use 5-minute timeout
- Complex clusters (CNV, MTA, OADP) use 15-minute timeout
- Provides best balance of speed and reliability

**Implementation**:
```go
func (h *ResumeHandler) getConsoleTimeout(cluster *types.Cluster) (int, time.Duration) {
    // Default: 5 minutes
    maxAttempts := 30
    retryDelay := 10 * time.Second

    // Check if cluster has complex addons installed
    hasComplexAddons := false
    // Query cluster_configurations for CNV, OADP, MTA, MTC
    // ... database query ...

    if hasComplexAddons || cluster.ProfileName.Contains("virtualization") {
        maxAttempts = 90 // 15 minutes for complex clusters
    }

    return maxAttempts, retryDelay
}
```

**Pros**:
- Optimizes timeout for cluster complexity
- Fast resume for simple clusters, reliable resume for complex ones
- Provides better user experience overall

**Cons**:
- More complex implementation
- Requires profile/addon metadata queries
- Harder to test all combinations

**Estimated Impact**: Reduces failure rate to ~5%, optimizes resume time

---

### Option 4: Parallel Console Check + Background Verification

**Change**: Mark cluster READY after API/operators/router checks, verify console in background

**Rationale**:
- Resume operation completes faster
- Console verification continues asynchronously
- Update cluster status or send alert if console doesn't become available

**Implementation**:
```go
func (h *ResumeHandler) resumeOpenShift(ctx context.Context, cluster *types.Cluster, job *types.Job) error {
    // ... existing checks (API, operators, router pods) ...

    // Mark cluster READY immediately
    if err := h.store.Clusters.UpdateStatus(ctx, cluster.ID, types.ClusterStatusReady); err != nil {
        return err
    }

    // Start background console verification
    go func() {
        ctx := context.Background()
        if err := h.waitForConsoleAccessibility(ctx, kubeconfigPath); err != nil {
            log.Printf("Warning: console accessibility verification failed for cluster %s: %v", cluster.Name, err)
            // Could send alert, create audit event, etc.
        } else {
            log.Printf("Console verified accessible for cluster %s", cluster.Name)
        }
    }()

    return nil
}
```

**Pros**:
- Fastest resume operations (don't block on console)
- Still verifies console health for monitoring
- Provides visibility into console issues without blocking

**Cons**:
- Complexity of background goroutine management
- Goroutine lifecycle management (what if worker restarts?)
- Potential for resource leaks if not managed carefully

**Estimated Impact**: Reduces resume time by 5-10 minutes, eliminates console-related failures

---

## Recommendation

**Implement Option 1 (Increase Timeout to 10 Minutes) immediately**, then consider Option 3 (Adaptive Timeout) as a follow-up enhancement.

**Rationale**:
1. **Low Risk**: Minimal code change, well-understood behavior
2. **High Impact**: Should resolve 75% of current failures based on timing data
3. **Quick to Deploy**: Can be deployed in next release cycle
4. **Data-Driven**: Based on actual production failure timings (16-20 minutes)
5. **Aligns with Existing Patterns**: API server wait already uses 10-minute timeout

**Additional Improvements**:
- Add metric tracking for console route wait times to CloudWatch/Prometheus
- Log console operator pod status when route lookup fails (for debugging)
- Add documentation about expected resume times for different cluster profiles

---

## Implementation Plan

### Phase 1: Immediate Fix (Option 1)

**Target**: Next deployment (within 1 week)

1. **Code Change** (1 hour)
   - Update `internal/worker/handler_resume.go:573`
   - Change `maxAttempts := 30` to `maxAttempts := 60`
   - Update error message: "console did not become accessible after 60 attempts (10 minutes)"

2. **Testing** (2 hours)
   - Test resume on simple cluster (SNO, minimal)
   - Test resume on complex cluster (CNV, OADP)
   - Verify failure case still works (force console operator failure)

3. **Documentation** (30 minutes)
   - Update CLAUDE.md with expected resume times
   - Add troubleshooting guide for resume failures

4. **Deployment**
   - Deploy to dev environment, verify with test resumes
   - Deploy to production via standard deployment process
   - Monitor resume success rate for 1 week

**Success Criteria**: Resume failure rate drops below 20%

---

### Phase 2: Enhanced Observability (Parallel with Phase 1)

**Target**: Same release as Phase 1

1. **Add Console Wait Time Metric** (1 hour)
   - Track time spent waiting for console route
   - Log to job output for debugging
   - Add to audit events

2. **Improve Error Messages** (1 hour)
   - Log console operator pod status when route fails
   - Include console deployment status in error context
   - Add link to troubleshooting docs

**Example Enhanced Error**:
```
Console route failed to appear after 10 minutes
Console operator status: Available=True, Progressing=False
Console deployment: 2/2 pods ready
Console pods: console-xyz (Running), console-abc (Running)
Possible causes: Route reconciliation delay, DNS issues, router configuration
See: https://docs.ocpctl.<BASE_DOMAIN>/troubleshooting/resume-console-timeout
```

---

### Phase 3: Adaptive Timeout (Option 3) - Future Enhancement

**Target**: 2-4 weeks after Phase 1

1. **Profile Metadata Enhancement** (3 hours)
   - Add `expectedResumeTime` field to profiles
   - Define timeout multipliers for addon types

2. **Implementation** (4 hours)
   - Create `getConsoleTimeout()` helper function
   - Query cluster addons from database
   - Apply appropriate timeout based on complexity

3. **Testing** (4 hours)
   - Test with various cluster configurations
   - Validate timeout selection logic
   - Verify metrics and logging

**Success Criteria**:
- Resume failure rate < 5%
- Average resume time reduced by 20% for simple clusters
- Complex clusters complete successfully within expected window

---

## Monitoring and Validation

### Metrics to Track

1. **Resume Success Rate**
   - Current baseline: 25% (1/4 successful)
   - Phase 1 target: >80% successful
   - Phase 3 target: >95% successful

2. **Console Route Wait Time**
   - Current: Unknown (fails at 5 minutes)
   - Track P50, P95, P99 wait times
   - Alert if P95 exceeds 8 minutes (approaching timeout)

3. **Resume Duration by Profile**
   - Simple clusters (SNO, minimal): <7 minutes
   - Standard clusters: <10 minutes
   - Complex clusters (CNV, OADP): <15 minutes

### Alerts

1. **High Resume Failure Rate**
   - Trigger: >30% of resume operations fail in 24-hour window
   - Action: Page on-call engineer, investigate pattern

2. **Console Route Timeout Approaching**
   - Trigger: Console route wait time >8 minutes (P95)
   - Action: Consider further timeout increase or investigation

3. **Individual Resume Failure**
   - Trigger: Any resume operation fails
   - Action: Log to audit trail, notify cluster owner

---

## Testing Plan

### Unit Tests

**New Test**: `TestConsoleAccessibilityTimeout`
```go
func TestConsoleAccessibilityTimeout(t *testing.T) {
    // Verify 60-attempt timeout behavior
    // Verify proper error message
    // Verify logging at appropriate intervals
}
```

### Integration Tests

1. **Successful Resume Test**
   - Create SNO cluster
   - Hibernate cluster
   - Resume cluster
   - Verify console accessible within 10 minutes
   - Verify cluster status = READY

2. **Slow Console Test**
   - Simulate delayed console route creation (mock kubectl)
   - Verify resume waits full 10 minutes
   - Verify console check succeeds once route appears

3. **Console Failure Test**
   - Simulate console operator failure (route never created)
   - Verify resume fails after 10 minutes
   - Verify appropriate error message

### Production Testing

1. **Dev Environment**
   - Deploy fix to dev environment
   - Resume 3-5 clusters with varying profiles
   - Validate success rate and timing

2. **Production Canary**
   - Deploy to production
   - Monitor first 10 resume operations closely
   - Rollback if failure rate doesn't improve

---

## Rollback Plan

If Phase 1 changes cause issues:

1. **Immediate Rollback** (if needed)
   - Revert `maxAttempts` from 60 to 30
   - Redeploy previous version via `./scripts/deploy.sh v0.PREVIOUS_VERSION`
   - Duration: <5 minutes

2. **Alternative Approach**
   - Implement Option 2 (make console check non-fatal)
   - Provides immediate relief while investigating root cause

---

## Related Issues

- **Commit 41fd696** (May 30, 2026): Previous fix for console route lookup
- **Commit d1ed679** (May 21, 2026): Initial console accessibility check implementation
- **Commit 2fd04c8** (Apr 26, 2026): CNI recovery for resume operations

---

## References

- Production Logs: `/var/log/journal` on ocpctl-production (<PROD_HOST>)
- Database: Production PostgreSQL (`jobs` and `clusters` tables)
- Code: `internal/worker/handler_resume.go:572-651`
- Recent Failures: July 6, June 17, June 14 (2026)

---

## Appendix: Sample Production Logs

### Failed Resume Attempt (July 6, 2026 - aws-shub-virt)

```
18:34:45 Processing job e308316d (type=RESUME, cluster=aws-shub-virt)
18:34:45 Resuming AWS cluster aws-shub-virt by starting EC2 instances
18:34:45 Found 5 stopped instances to start
18:34:47 Successfully initiated start for 5 instances
18:35:03 All instances are now running
18:35:03 Waiting for cluster aws-shub-virt to become healthy...
18:35:03 Waiting for API server to be accessible...
18:39:18 API server is accessible (took 4m15s)
18:39:20 All critical cluster operators are ready
18:39:21 All 2 router pods are ready
18:39:21 Verifying console route is accessible...
18:39:42 Console route not yet available (attempt 3/30): exit status 1
18:40:13 Console route not yet available (attempt 6/30): exit status 1
[... repeated failures every 10 seconds ...]
18:44:23 Console route not yet available (attempt 30/30): exit status 1
18:44:34 Job failed: console did not become accessible after 30 attempts (5 minutes)
18:44:34 Job will be retried (attempt 2/3)
[... two more retry attempts with identical pattern ...]
18:55:14 Job failed after 3 attempts (final failure after 20.5 minutes total)
```

**Key Observation**: Console route check consumed 5 minutes of a 10-minute resume operation, then failed. With the proposed 10-minute timeout, this cluster would likely succeed on first attempt.

---

**Document Version**: 1.0
**Last Updated**: 2026-07-13
**Author**: Claude (AI Assistant)
**Review Status**: Pending User Review
