# CNV Nightly Operator Timeout - Root Cause Found

**Date**: 2026-07-15
**Status**: Root cause identified
**Affected**: cnv-nightly addon (all versions: stable-stage, nightly, windows variants)

---

## Executive Summary

The CNV nightly operator installation fails because **the code does not wait for custom CatalogSource resources to be ready** before attempting to install operators from them. The catalog source pod needs 5-15 minutes to pull the image and index the catalog, but operator installation starts immediately.

---

## The Bug

### Location
`internal/worker/handler_post_configure.go:2010-2018` (applyManifest function)

### Current Behavior
```go
// Apply manifest
if err := h.applyYAML(ctx, kubeconfigPath, string(content)); err != nil {
    return fmt.Errorf("apply manifest: %w", err)
}

_ = h.updateConfigTaskStatus(ctx, configID, types.ConfigStatusCompleted, nil)
return nil  // ← Returns immediately after applying YAML!
```

The code:
1. Waits for the CatalogSource CRD to exist (✓)
2. Applies the CatalogSource manifest (✓)
3. **Immediately marks task as complete** (✗)
4. Proceeds to install operator (✗ catalog not ready yet!)

### What Should Happen
After applying a CatalogSource manifest, the code should:
1. Wait for the catalog source pod to be created
2. Wait for the pod to be Running
3. Wait for status.connectionState.lastObservedState == "READY"
4. Then mark task complete and proceed

---

## Evidence

### GA CNV Works
```yaml
operators:
  - name: kubevirt-hyperconverged
    namespace: openshift-cnv
    source: redhat-operators  # ← Built-in catalog, always ready
    channel: stable
```

The `redhat-operators` catalog is pre-installed and always ready, so operator installation succeeds immediately.

### Nightly CNV Fails
```yaml
manifests:
  - name: cnv-nightly-catalogsource
    content: |
      apiVersion: operators.coreos.com/v1alpha1
      kind: CatalogSource
      metadata:
        name: cnv-nightly
        namespace: openshift-marketplace
      spec:
        sourceType: grpc
        image: quay.io/openshift-cnv/nightly-catalog:4.22  # ← Must pull and index

operators:
  - name: kubevirt-hyperconverged
    namespace: openshift-cnv
    source: cnv-nightly  # ← Custom catalog, needs time to index
    channel: nightly-4.22
    dependsOn:
      - cnv-nightly-catalogsource  # ← Only waits for manifest apply, not readiness!
```

The `dependsOn` mechanism only ensures the manifest is *applied*, not that the catalog is *ready*.

### Timeline from Logs (shadow-clone)
```
00:30:45 - Applying manifest: cnv-nightly-catalogsource
00:30:47 - CRD catalogsources.operators.coreos.com is ready
00:30:48 - Applied YAML successfully: catalogsource.operators.coreos.com/cnv-nightly created
00:30:48 - Task cnv-nightly-catalogsource completed successfully  ← Marked complete!
00:30:48 - Installing operator: kubevirt-hyperconverged  ← Starts immediately!
00:30:49 - Subscription created
00:30:49 - Waiting for operator to be ready (timeout: 10 minutes)
00:31:00 - Operator status:  (waiting...)
...
00:40:49 - Timeout (10 minutes elapsed)
```

Only **3 seconds** between catalog creation and operator installation! The catalog pod hasn't even started yet.

---

## CatalogSource Lifecycle

When a CatalogSource is created with a grpc image:

1. **t=0s**: Manifest applied, resource created
2. **t=5-30s**: Pod created in openshift-marketplace namespace
3. **t=30s-2m**: Image pull begins (can be slow for large nightly catalogs)
4. **t=2-10m**: Catalog indexing (building package database)
5. **t=10-15m**: Status updates to READY
6. **Only then**: OLM can resolve package queries and create InstallPlans/CSVs

The code currently jumps from step 1 to step 6, skipping the entire catalog initialization process.

---

## Fix Implementation

### Option 1: Add CatalogSource Readiness Check (Recommended)

Add a new function to wait for catalog sources to be ready:

```go
// waitForCatalogSourceReady waits for a CatalogSource to be indexed and ready
func (h *PostConfigureHandler) waitForCatalogSourceReady(ctx context.Context, kubeconfigPath, name, namespace string) error {
    log.Printf("Waiting for CatalogSource %s/%s to be ready...", namespace, name)

    timeout := time.After(15 * time.Minute)  // Nightly catalogs can take 10-15 min
    ticker := time.NewTicker(10 * time.Second)
    defer ticker.Stop()

    for {
        select {
        case <-ctx.Done():
            return ctx.Err()
        case <-timeout:
            return fmt.Errorf("timeout waiting for CatalogSource %s to be ready", name)
        case <-ticker.C:
            // Check catalog source status
            cmd := exec.CommandContext(ctx, "oc", "--kubeconfig", kubeconfigPath,
                "get", "catalogsource", name, "-n", namespace,
                "-o", "jsonpath={.status.connectionState.lastObservedState}")
            output, err := cmd.CombinedOutput()
            if err != nil {
                log.Printf("Checking CatalogSource status: %v (will retry)", err)
                continue
            }

            state := strings.TrimSpace(string(output))
            if state == "READY" {
                log.Printf("CatalogSource %s is ready", name)
                return nil
            }

            log.Printf("CatalogSource %s state: %s (waiting...)", name, state)
        }
    }
}
```

Then modify `applyManifest` to detect CatalogSource resources:

```go
// Apply manifest
if err := h.applyYAML(ctx, kubeconfigPath, string(content)); err != nil {
    errMsg := err.Error()
    _ = h.updateConfigTaskStatus(ctx, configID, types.ConfigStatusFailed, &errMsg)
    return fmt.Errorf("apply manifest: %w", err)
}

// If this is a CatalogSource, wait for it to be ready before marking complete
if isCatalogSource(string(content)) {
    catalogName, catalogNS := extractCatalogSourceNameAndNamespace(string(content))
    if err := h.waitForCatalogSourceReady(ctx, kubeconfigPath, catalogName, catalogNS); err != nil {
        errMsg := fmt.Sprintf("CatalogSource not ready: %v", err)
        _ = h.updateConfigTaskStatus(ctx, configID, types.ConfigStatusFailed, &errMsg)
        return fmt.Errorf("wait for CatalogSource %s: %w", catalogName, err)
    }
}

_ = h.updateConfigTaskStatus(ctx, configID, types.ConfigStatusCompleted, nil)
return nil
```

### Helper Functions Needed

```go
// isCatalogSource checks if manifest content is a CatalogSource
func isCatalogSource(content string) bool {
    return strings.Contains(content, "kind: CatalogSource")
}

// extractCatalogSourceNameAndNamespace parses catalog source name and namespace from YAML
func extractCatalogSourceNameAndNamespace(content string) (string, string) {
    // Simple YAML parsing - production should use yaml.Unmarshal
    lines := strings.Split(content, "\n")
    var name, namespace string

    for _, line := range lines {
        if strings.Contains(line, "name:") && !strings.Contains(line, "metadata") && name == "" {
            name = strings.TrimSpace(strings.Split(line, ":")[1])
        }
        if strings.Contains(line, "namespace:") && !strings.Contains(line, "targetNamespaces") {
            namespace = strings.TrimSpace(strings.Split(line, ":")[1])
        }
    }

    if namespace == "" {
        namespace = "openshift-marketplace"  // Default for catalog sources
    }

    return name, namespace
}
```

### Option 2: Add Explicit Wait in Operator Install

Before creating the subscription, check if the catalog source is ready:

```go
func (h *PostConfigureHandler) installOperator(...) error {
    ...

    // If using a custom catalog source, wait for it to be ready
    if op.Source != "" && op.Source != "redhat-operators" {
        if err := h.waitForCatalogSourceReady(ctx, kubeconfigPath, op.Source, "openshift-marketplace"); err != nil {
            return fmt.Errorf("catalog source %s not ready: %w", op.Source, err)
        }
    }

    // Create Subscription
    if err := h.createSubscription(ctx, kubeconfigPath, op); err != nil {
        ...
    }
    ...
}
```

---

## Recommendation

**Implement Option 1** - Add catalog source readiness checks at the manifest application level. This ensures:
- All catalog sources are ready before dependent resources are created
- Clear logging of catalog indexing progress
- Works for any future addons using custom catalogs
- Follows OLM best practices

---

## Testing Plan

1. Deploy CNV nightly on test cluster with fix
2. Verify logs show catalog source reaching READY state
3. Verify operator installs successfully after catalog is ready
4. Test with all nightly variants (stable-stage, nightly, windows)
5. Regression test with GA CNV (should still work, skips wait)

---

## Impact

**Affected Addons**:
- cnv-nightly (all 4 versions)
- Any future addons using custom catalog sources

**Not Affected**:
- cnv (GA) - uses redhat-operators
- mta, mtc, oadp - use redhat-operators

---

## Files to Modify

1. `internal/worker/handler_post_configure.go`
   - Add `waitForCatalogSourceReady()` function
   - Add `isCatalogSource()` helper
   - Add `extractCatalogSourceNameAndNamespace()` helper
   - Modify `applyManifest()` to wait for catalog sources

2. `internal/worker/constants.go`
   - Add `CatalogSourceWaitTimeout = 15 * time.Minute`

---

## Related Issues

Previous investigation: `docs/issues/cnv-operator-timeout-investigation.md` (initial theory about CSV lookup - incorrect)
