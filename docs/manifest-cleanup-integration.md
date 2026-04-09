# Manifest-Driven Cleanup Integration Guide

## Overview

This guide shows where to add manifest recording calls during cluster creation to enable fast manifest-driven cleanup during destroy.

## Benefits

- **10-100x faster destroy**: Direct deletion by name/ARN instead of scanning entire AWS account
- **No timeouts**: Eliminates account-wide `ListRoles`, `ListOpenIDConnectProviders`, `ListHostedZones` calls
- **Deterministic cleanup**: Exact resources created are exactly what gets deleted

## Architecture

### Three-tiered destroy strategy:

1. **ccoctl** (fastest - uses infraID from metadata.json)
2. **Manifest-driven** (fast - uses exact names/ARNs from aws-cleanup-manifest.json)
3. **Discovery-based** (slow - scans AWS account for matching resources)

## Integration Points

### 1. After Infrastructure ID is Known

**Location**: After `openshift-install create manifests` or when metadata.json is created

```go
// Record infraID as soon as it's available
if err := RecordAWSInfraID(workDir, cluster.Name, infraID, cluster.Region); err != nil {
    log.Printf("Warning: failed to record infraID in cleanup manifest: %v", err)
}
```

### 2. After CCO IAM Role Creation

**Location**: After `ccoctl aws create-all` or when CCO roles are created

```go
// Option A: Parse ccoctl output to extract role names
ccoctlOutput, err := runCCOCTL(...)
if err == nil {
    roleNames := parseRoleNamesFromCCOCTLOutput(ccoctlOutput)
    for _, roleName := range roleNames {
        if err := RecordAWSIAMRole(workDir, roleName); err != nil {
            log.Printf("Warning: failed to record IAM role %s: %v", roleName, err)
        }
    }
}

// Option B: Use infraID to predict role names (if you know the naming convention)
// CCO roles typically follow: <infraID>-<component>-cloud-credentials
// Example: tsanders-virt-oadp5000-abc12-openshift-ingress-cloud-credentials
```

### 3. After OIDC Provider Creation

**Location**: After OIDC provider is created (usually by ccoctl)

```go
// Construct OIDC provider ARN
accountID := getAWSAccountID()
oidcProviderArn := fmt.Sprintf(
    "arn:aws:iam::%s:oidc-provider/%s-oidc.s3.%s.amazonaws.com",
    accountID, infraID, cluster.Region,
)

if err := RecordAWSOIDCProvider(workDir, oidcProviderArn); err != nil {
    log.Printf("Warning: failed to record OIDC provider: %v", err)
}
```

### 4. After Route53 Hosted Zone Creation

**Location**: After hosted zone is created or looked up

```go
// After creating or finding the hosted zone
zoneID := createdZone.Id // or from ListHostedZones API

if err := RecordAWSRoute53HostedZone(workDir, zoneID); err != nil {
    log.Printf("Warning: failed to record Route53 zone ID: %v", err)
}
```

### 5. After Windows IRSA Role Creation

**Location**: In Windows post-configuration handler

```go
// After creating Windows IRSA role for S3 access
windowsRoleName := fmt.Sprintf("ocpctl-win-s3-%s", cluster.ID)

// Create the role...
_, err := iamClient.CreateRole(...)
if err == nil {
    if err := RecordAWSWindowsIRSARole(workDir, windowsRoleName); err != nil {
        log.Printf("Warning: failed to record Windows IRSA role: %v", err)
    }
}
```

## Example: Parsing CCO Role Names from ccoctl Output

```go
func parseRoleNamesFromCCOCTLOutput(output string) []string {
    var roleNames []string

    // ccoctl output typically includes lines like:
    // Created role: tsanders-virt-oadp5000-abc12-openshift-ingress-cloud-credentials

    scanner := bufio.NewScanner(strings.NewReader(output))
    for scanner.Scan() {
        line := scanner.Text()
        if strings.Contains(line, "Created role:") || strings.Contains(line, "Role:") {
            parts := strings.Split(line, ":")
            if len(parts) >= 2 {
                roleName := strings.TrimSpace(parts[1])
                roleNames = append(roleNames, roleName)
            }
        }
    }

    return roleNames
}
```

## Example: Alternative - Direct AWS SDK Recording

If you don't trust ccoctl output parsing, you can query AWS directly after ccoctl completes:

```go
func recordAllInfraRoles(ctx context.Context, workDir, infraID, region string) error {
    cfg, _ := config.LoadDefaultConfig(ctx, config.WithRegion(region))
    iamClient := iam.NewFromConfig(cfg)

    // List all roles matching infraID prefix
    paginator := iam.NewListRolesPaginator(iamClient, &iam.ListRolesInput{})

    prefix := infraID + "-"
    for paginator.HasMorePages() {
        page, err := paginator.NextPage(ctx)
        if err != nil {
            return err
        }

        for _, role := range page.Roles {
            roleName := aws.ToString(role.RoleName)
            if strings.HasPrefix(roleName, prefix) {
                if err := RecordAWSIAMRole(workDir, roleName); err != nil {
                    log.Printf("Warning: failed to record role %s: %v", roleName, err)
                }
            }
        }
    }

    return nil
}
```

## Manifest Lifecycle

### Create Flow

1. Cluster creation starts → Create empty manifest
2. Each resource created → Append to manifest
3. Upload artifacts to S3 → Include `aws-cleanup-manifest.json`

### Destroy Flow

1. Download artifacts from S3 → Get `aws-cleanup-manifest.json`
2. Try ccoctl delete (fast path)
3. If ccoctl fails → Try manifest-driven cleanup (fast fallback)
4. If manifest missing/empty → Try discovery-based cleanup (slow fallback)

## Manifest File Location

- **Path**: `<workDir>/aws-cleanup-manifest.json`
- **Upload to S3**: Include in artifact upload (same as metadata.json, kubeconfig, etc.)
- **Download from S3**: Download during destroy (same as other artifacts)

## Manifest Example

```json
{
  "clusterName": "tsanders-virt-oadp5000",
  "infraID": "tsanders-virt-oadp5000-fbfvv",
  "region": "us-east-1",
  "iamRoles": [
    "tsanders-virt-oadp5000-fbfvv-openshift-ingress-cloud-credentials",
    "tsanders-virt-oadp5000-fbfvv-openshift-image-registry-cloud-credentials",
    "tsanders-virt-oadp5000-fbfvv-openshift-machine-api-aws-cloud-credentials"
  ],
  "instanceProfiles": [],
  "oidcProviderArn": "arn:aws:iam::346869059911:oidc-provider/tsanders-virt-oadp5000-fbfvv-oidc.s3.us-east-1.amazonaws.com",
  "windowsIRSARole": "ocpctl-win-s3-12345-67890-abcde",
  "route53HostedZoneId": "Z0123456789ABCDEF"
}
```

## Testing the Integration

### 1. Create a test cluster

The manifest should be created automatically during installation.

### 2. Verify manifest exists

```bash
cat <workDir>/aws-cleanup-manifest.json
```

### 3. Check manifest has expected resources

Should contain:
- infraID
- At least 3-5 IAM roles (CCO creates multiple)
- OIDC provider ARN
- Route53 hosted zone ID
- Windows IRSA role (if Windows nodes were configured)

### 4. Destroy the cluster

Watch logs for:
- "Attempting manifest-driven cleanup for cluster..."
- "Found cleanup manifest with X IAM roles..."
- "Manifest-driven cleanup: deleted X resources..."

### 5. Verify performance improvement

Before manifest: 60-90 seconds of IAM scanning
After manifest: <5 seconds direct deletion

## Migration for Existing Clusters

Existing clusters won't have manifests. The destroy handler automatically falls back to discovery-based cleanup for these clusters.

No action needed - new clusters will use manifest-driven cleanup automatically once you integrate the recording calls.

## Best Practices

1. **Record as soon as resources are created**: Don't wait until end of installation
2. **Record even on partial success**: If some roles created but installation fails, manifest ensures cleanup
3. **Log but don't fail on recording errors**: Manifest is an optimization, not required for destroy
4. **Upload manifest to S3**: Ensure it survives worker restarts

## Performance Comparison

| Method | API Calls | Time (500+ IAM roles) | Timeout Risk |
|--------|-----------|----------------------|--------------|
| ccoctl | O(infraID resources) | <5s | None |
| Manifest-driven | O(recorded resources) | <5s | None |
| Discovery-based | O(all account resources) | 60-90s | High |

## Summary

The manifest approach eliminates the fundamental problem of discovery-based cleanup: scanning resources you don't own.

By recording exact names/ARNs during create, destroy becomes deterministic, fast, and timeout-free.
