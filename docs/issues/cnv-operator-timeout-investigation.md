# CNV Operator Installation Timeout - Investigation

**Date**: 2026-07-15
**Issue**: POST_CONFIGURE jobs failing with timeout waiting for CNV operator to be ready
**Affected Clusters**: shadow-clone (prod), tsanders-pre-virt-2 (dev)

---

## Summary

Both production and dev environments experienced identical failures when installing the CNV nightly operator via POST_CONFIGURE jobs. The operator subscription was created successfully, but the worker timed out after 30 minutes (3 attempts × 10 minutes) waiting for the operator CSV to become ready.

## Timeline

### Production (shadow-clone)
- **00:30:38** - Cluster READY, POST_CONFIGURE job created
- **00:30:42** - First attempt: catalog source created, subscription created
- **00:40:49** - Timeout, attempt 2
- **00:51:05** - Timeout, attempt 3
- **01:01:23** - Final timeout, job FAILED
- **02:11:32** - Cluster destroyed

### Dev (tsanders-pre-virt-2)
- **19:52:25** - Cluster READY, POST_CONFIGURE job created
- **19:52:32** - First attempt started
- **20:02:37** - Timeout, attempt 2
- **20:12:42** - Timeout, attempt 3
- **20:22:47** - Final timeout, job FAILED

**Error Message**: `timeout waiting for operator kubevirt-hyperconverged to be ready`

---

## Root Cause Analysis

### The Bug

The `waitForOperatorReady()` function in `internal/worker/handler_post_configure.go:1705-1736` uses an **incorrect CSV lookup method**:

```go
// Line 1720-1721
cmd := exec.CommandContext(ctx, "oc", "--kubeconfig", kubeconfigPath,
    "get", "csv", "-n", op.Namespace, "-o",
    "jsonpath={.items[?(@.spec.displayName contains '"+op.Name+"')].status.phase}")
```

**Problem**: This searches for a CSV where `spec.displayName` contains the operator name (`kubevirt-hyperconverged`), but:

1. **Operator name** (from subscription): `kubevirt-hyperconverged`
2. **CSV displayName**: "OpenShift Virtualization" (or similar)

These do NOT match! The displayName "OpenShift Virtualization" does not contain "kubevirt-hyperconverged", so the query returns empty results every time.

### Why This Happened

The subscription creates successfully:
```yaml
apiVersion: operators.coreos.com/v1alpha1
kind: Subscription
metadata:
  name: kubevirt-hyperconverged  # ← operator name
  namespace: openshift-cnv
spec:
  channel: nightly-4.22
  name: kubevirt-hyperconverged  # ← package name
  source: cnv-nightly
```

OLM processes this and creates a CSV with:
```yaml
apiVersion: operators.coreos.com/v1alpha1
kind: ClusterServiceVersion
metadata:
  name: kubevirt-hyperconverged-operator.v4.22.0-xxx  # ← generated name
  namespace: openshift-cnv
spec:
  displayName: "OpenShift Virtualization"  # ← human-readable name
  ...
```

The code tries to find this CSV by displayName containing "kubevirt-hyperconverged", which fails because "OpenShift Virtualization" doesn't contain that string.

### Log Evidence

From both clusters, the logs show:
```
Operator kubevirt-hyperconverged status:  (waiting...)
```

The empty status indicates the JSONPath query returned nothing because no CSV matched the displayName filter.

---

## The Correct Approach

There are **three proper ways** to find the CSV created by a subscription:

### Option 1: Check Subscription Status (Recommended)
```bash
oc get subscription kubevirt-hyperconverged -n openshift-cnv \
  -o jsonpath='{.status.currentCSV}'
```

This returns the exact CSV name that OLM created (e.g., `kubevirt-hyperconverged-operator.v4.22.0-123`), then check that CSV's status:

```bash
CSV_NAME=$(oc get subscription kubevirt-hyperconverged -n openshift-cnv -o jsonpath='{.status.currentCSV}')
oc get csv "$CSV_NAME" -n openshift-cnv -o jsonpath='{.status.phase}'
```

### Option 2: Match by Package Name
The subscription's `spec.name` field is the package name, which should match the CSV's metadata:

```bash
oc get csv -n openshift-cnv -o json | \
  jq -r '.items[] | select(.metadata.name | startswith("kubevirt-hyperconverged")) | .status.phase'
```

### Option 3: List All CSVs (Fallback)
Simply get all CSVs in the namespace and check if any are in Succeeded state:

```bash
oc get csv -n openshift-cnv -o jsonpath='{.items[*].status.phase}'
```

---

## Fix Implementation

### Code Change Required

File: `internal/worker/handler_post_configure.go`

**Current Code** (lines 1718-1733):
```go
case <-ticker.C:
    // Check if CSV is ready
    cmd := exec.CommandContext(ctx, "oc", "--kubeconfig", kubeconfigPath,
        "get", "csv", "-n", op.Namespace, "-o",
        "jsonpath={.items[?(@.spec.displayName contains '"+op.Name+"')].status.phase}")
    output, err := cmd.CombinedOutput()
    if err != nil {
        log.Printf("Checking operator status: %v (will retry)", err)
        continue
    }

    if strings.Contains(string(output), "Succeeded") {
        log.Printf("Operator %s is ready", op.Name)
        return nil
    }

    log.Printf("Operator %s status: %s (waiting...)", op.Name, strings.TrimSpace(string(output)))
```

**Fixed Code** (use subscription status approach):
```go
case <-ticker.C:
    // First, get the CSV name from the subscription status
    getCSVCmd := exec.CommandContext(ctx, "oc", "--kubeconfig", kubeconfigPath,
        "get", "subscription", op.Name, "-n", op.Namespace,
        "-o", "jsonpath={.status.currentCSV}")
    csvNameBytes, err := getCSVCmd.CombinedOutput()
    if err != nil {
        log.Printf("Checking subscription status: %v (will retry)", err)
        continue
    }

    csvName := strings.TrimSpace(string(csvNameBytes))
    if csvName == "" {
        log.Printf("Operator %s: subscription has no currentCSV yet (waiting...)", op.Name)
        continue
    }

    // Now check the CSV status
    checkCSVCmd := exec.CommandContext(ctx, "oc", "--kubeconfig", kubeconfigPath,
        "get", "csv", csvName, "-n", op.Namespace,
        "-o", "jsonpath={.status.phase}")
    statusBytes, err := checkCSVCmd.CombinedOutput()
    if err != nil {
        log.Printf("Checking CSV %s status: %v (will retry)", csvName, err)
        continue
    }

    status := strings.TrimSpace(string(statusBytes))
    if status == "Succeeded" {
        log.Printf("Operator %s is ready (CSV: %s)", op.Name, csvName)
        return nil
    }

    log.Printf("Operator %s status: %s (CSV: %s, waiting...)", op.Name, status, csvName)
```

### Alternative: Increase Timeout

If catalog image pulling is slow, we could also increase the timeout from 10 minutes to 15-20 minutes for nightly builds. However, **this is a band-aid** - the real issue is the broken CSV lookup.

Update `internal/worker/constants.go`:
```go
// Current
PostConfigWaitTimeout = 10 * time.Minute

// Proposed for nightly builds
PostConfigWaitTimeout = 20 * time.Minute  // Allow more time for catalog indexing
```

---

## Testing Plan

1. **Unit Test**: Create test case for `waitForOperatorReady()` with mock oc commands
2. **Integration Test**: Deploy CNV on test cluster and verify CSV detection works
3. **Regression Test**: Test with other operators (MTA, MTC, OADP) to ensure fix works universally

---

## Impact Assessment

### Affected Operators
- **CNV (OpenShift Virtualization)** - Confirmed broken
- **MTA (Migration Toolkit for Applications)** - Likely broken
- **MTC (Migration Toolkit for Containers)** - Likely broken
- **OADP (OpenShift API for Data Protection)** - Likely broken

All operators using custom displayNames different from their package names are affected.

### Workaround
None available. Clusters requiring CNV must have the operator installed manually after cluster creation.

---

## Recommendations

1. **Immediate**: Implement the subscription-based CSV lookup fix
2. **Short-term**: Add debug logging showing actual CSV names found
3. **Long-term**: Consider adding catalog source readiness checks before operator installation
4. **Monitoring**: Add alerting for POST_CONFIGURE job failures

---

## Related Files

- `internal/worker/handler_post_configure.go:1705-1736` - Buggy waitForOperatorReady()
- `internal/worker/constants.go:22` - PostConfigWaitTimeout constant
- `internal/addon/definitions/cnv-nightly.yaml` - CNV addon definition
- `internal/addon/definitions/mta.yaml` - MTA addon (likely affected)
- `internal/addon/definitions/mtc.yaml` - MTC addon (likely affected)
- `internal/addon/definitions/oadp.yaml` - OADP addon (likely affected)

---

## Next Steps

1. ✅ Root cause identified
2. ⬜ Implement fix in handler_post_configure.go
3. ⬜ Test fix with CNV deployment
4. ⬜ Verify other addons work correctly
5. ⬜ Deploy to dev environment
6. ⬜ Deploy to production
7. ⬜ Update CLAUDE.md with learnings
