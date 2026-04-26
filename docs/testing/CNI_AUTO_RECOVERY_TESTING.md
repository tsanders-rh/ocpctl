# CNI Auto-Recovery Testing Guide

This guide covers testing the automatic CNI recovery feature added to the cluster resume workflow.

## Feature Overview

The resume handler now includes:
- **CNI Health Checks**: Detects multus and OVN-Kubernetes pod issues after cluster resume
- **Automatic Remediation**: Deletes problematic CNI pods to trigger automatic recovery
- **Smart Retry Logic**: Attempts remediation once, then monitors for up to 10 minutes

## Test Results

### ✅ Unit Tests (PASSING)
```bash
go test ./internal/worker -v

# Results:
# TestCheckPodHealthParsing - validates pod status parsing
# TestRemediateCNIPodsLogic - validates remediation logic
# TestWaitForCNIPodsIntegration - integration test (requires TEST_KUBECONFIG)
```

**All unit tests passing** - Logic validated for:
- Pod health state detection (Running, CrashLoopBackOff, Error, Pending)
- Container readiness parsing
- Issue identification and pod name extraction
- Edge cases (empty output, no issues, multiple issues)

### ✅ Safe Integration Test (PASSING)
```bash
./scripts/test-cni-health-check.sh ~/Downloads/kubeconfig-sandy-virt-ga-2.yaml

# Results: All 5 multus and 5 OVN pods healthy
# This confirms the health check queries work against real cluster
```

## Recommended Testing Sequence

### Phase 1: Verify Current State (COMPLETED)
- [x] Unit tests pass
- [x] Integration test confirms health checks work
- [x] Code compiles without errors

### Phase 2: Real-World Hibernate/Resume Test (RECOMMENDED NEXT)

This is the **definitive test** that validates end-to-end functionality.

**Prerequisites**:
- Access to ocpctl API or CLI
- Cluster in READY state
- Ability to monitor job logs

**Test Steps**:

1. **Hibernate the cluster**:
   ```bash
   # Via API
   curl -X POST https://<ocpctl-api>/api/v1/clusters/{cluster-id}/hibernate

   # Via CLI (if available)
   ocpctl hibernate sandy-virt-ga-2
   ```

2. **Wait for hibernation to complete** (~5 minutes):
   - All EC2 instances should be stopped
   - Cluster status should be HIBERNATED

3. **Resume the cluster**:
   ```bash
   # Via API
   curl -X POST https://<ocpctl-api>/api/v1/clusters/{cluster-id}/resume

   # Via CLI
   ocpctl resume sandy-virt-ga-2
   ```

4. **Monitor the resume job logs** for these indicators:

   **Expected log output (healthy case)**:
   ```
   Waiting for API server to be accessible...
   API server is accessible
   Waiting for cluster operators to be ready...
   All critical cluster operators are ready
   Waiting for router pods to be running...
   All 2 router pods are ready
   Waiting for CNI networking pods to be healthy...
   All CNI networking pods are healthy
   Verifying load balancer health check configuration...
   Load balancer has healthy instances
   Cluster sandy-virt-ga-2 is now healthy and ready
   ```

   **Expected log output (with CNI issues - auto-recovery)**:
   ```
   Waiting for CNI networking pods to be healthy...
   CNI networking pods not yet healthy (attempt 6/60)...
     Multus issues: [multus-abc (phase: CrashLoopBackOff)]
     OVN issues: [ovnkube-node-xyz (containers not ready)]
   Detected CNI pod issues, attempting automatic remediation...
   Deleting problematic multus pod: multus-abc
   Deleting problematic OVN pod: ovnkube-node-xyz
   Deleted 2 problematic CNI pods for automatic recovery
   Automatic remediation completed, waiting for pods to restart...
   [30 second pause]
   All CNI networking pods are healthy
   Cluster sandy-virt-ga-2 is now healthy and ready
   ```

5. **Verify cluster health** after resume:
   ```bash
   kubectl --kubeconfig ~/Downloads/kubeconfig-sandy-virt-ga-2.yaml get co
   kubectl --kubeconfig ~/Downloads/kubeconfig-sandy-virt-ga-2.yaml get nodes
   ./scripts/test-cni-health-check.sh ~/Downloads/kubeconfig-sandy-virt-ga-2.yaml
   ```

**Success Criteria**:
- ✅ Resume job completes successfully
- ✅ All cluster operators show AVAILABLE=True, DEGRADED=False
- ✅ All nodes are Ready
- ✅ All multus and OVN pods are Running and ready
- ✅ No manual intervention required

### Phase 3: Simulated Failure Test (OPTIONAL - Advanced)

Use this to test auto-recovery **without** doing a full hibernate/resume cycle.

**⚠️ WARNING**: This temporarily breaks CNI on one worker node. Use only on dev/test clusters.

```bash
./scripts/test-cni-recovery.sh ~/Downloads/kubeconfig-sandy-virt-ga-2.yaml
```

**What it does**:
1. Selects one worker node
2. Deletes the multus pod on that node (simulates crash)
3. Monitors automatic DaemonSet recovery
4. Confirms pod is recreated and healthy

**Why this is useful**:
- Tests CNI recovery without risking full cluster hibernation
- Faster iteration (30 seconds vs 10+ minutes)
- Safe to run multiple times
- Validates Kubernetes DaemonSet auto-healing works

**Expected output**:
```
Selected worker node: ip-10-0-74-237.us-west-2.compute.internal
Found multus pod on that node: multus-v26pq
Step 1: Deleting multus pod to simulate crash...
Step 3: Monitoring pod recovery...
  Waiting for pod to be ready... (1/30)
  Waiting for pod to be ready... (2/30)
✓ Pod recovered: multus-xyz123 (running)
✓ Pod is healthy and ready!
Test complete!
```

### Phase 4: Chaos Engineering (OPTIONAL - Production Validation)

For production validation, consider:

1. **Scheduled Hibernation Test**:
   - Set up work hours for automatic hibernation
   - Monitor resume job metrics over multiple cycles
   - Track auto-recovery trigger rate

2. **Metrics & Alerting**:
   - Add CloudWatch metric for CNI remediation events
   - Alert if remediation fails after max retries
   - Dashboard showing resume success rate

3. **Runbook Documentation**:
   - Document what to do if auto-recovery fails
   - Create manual recovery procedure
   - Define escalation path

## Troubleshooting

### Auto-Recovery Didn't Trigger
**Symptoms**: Cluster resumed but CNI issues persisted
**Diagnosis**:
- Check resume job logs for "Waiting for CNI networking pods"
- Verify issue occurred within 10-minute timeout window
- Check if pods were in states that trigger remediation (CrashLoopBackOff, Error, Failed)

**Manual Recovery**:
```bash
kubectl delete pod -n openshift-multus <pod-name>
kubectl delete pod -n openshift-ovn-kubernetes <pod-name>
```

### Auto-Recovery Failed
**Symptoms**: Remediation attempted but cluster still unhealthy
**Diagnosis**:
- Check if new pods are also failing
- Review underlying node issues (disk, memory, etc.)
- Check OVN database connectivity

**Manual Investigation**:
```bash
kubectl describe pod -n openshift-multus <pod-name>
kubectl logs -n openshift-multus <pod-name>
kubectl logs -n openshift-ovn-kubernetes <pod-name> -c ovnkube-controller
```

## Success Metrics

Track these metrics to measure auto-recovery effectiveness:

- **Resume Success Rate**: % of resumes that reach READY status
- **Auto-Recovery Trigger Rate**: % of resumes that trigger CNI remediation
- **Mean Time to Recovery (MTTR)**: Time from resume start to READY status
- **Manual Intervention Rate**: % of resumes requiring human action

## Next Steps

After successful testing:

1. **Monitor Production**:
   - Track resume job metrics
   - Set up alerting for failures
   - Review logs weekly

2. **Iterate & Improve**:
   - Add metrics for recovery events
   - Expand to other post-resume issues
   - Consider periodic health checks during normal operation

3. **Documentation**:
   - Update user-facing docs with auto-recovery feature
   - Create troubleshooting runbooks
   - Share lessons learned

## References

- Implementation: `internal/worker/handler_resume.go:545-700`
- Unit Tests: `internal/worker/handler_resume_test.go`
- Test Scripts: `scripts/test-cni-*.sh`
- Original Issue: Post-hibernation CNI networking failure (multus crash, OVN database timeout)
