# OpenShift STS InvalidIdentityToken Root Cause and Fix

## Executive Summary

**Problem:** 25+ cluster installations failed with `InvalidIdentityToken: Couldn't retrieve verification key from your identity provider`

**Root Cause:** Missing `tls/` directory from ccoctl output prevents proper STS token projection. Pods use default service account tokens with `aud=["openshift"]` instead of projected tokens with `aud=["sts.amazonaws.com"]`.

**Fix:** Copy the `tls/` directory (containing `bound-service-account-signing-key.key`) from ccoctl output to the installation directory.

---

## Detailed Root Cause Analysis

### What We Initially Thought (WRONG)

- ❌ infraID mismatch between ccoctl and cluster
- ❌ ccoctl using wrong audience (`openshift` vs `sts.amazonaws.com`)
- ❌ AWS STS unable to fetch OIDC endpoints

### What's Actually Happening (CORRECT)

1. **IAM OIDC Provider Configuration is Correct**
   - ccoctl creates the IAM OIDC provider with BOTH allowed client IDs:
     - `"openshift"`
     - `"sts.amazonaws.com"`
   - Source: [cloud-credential-operator/create_identity_provider.go](https://github.com/openshift/cloud-credential-operator/blob/master/pkg/aws/actuator/create_identity_provider.go)

2. **The Problem: Wrong Token Type**
   - Pods are using **default service account tokens** (long-lived, aud=["openshift"])
   - Should be using **projected service account tokens** (short-lived, aud=["sts.amazonaws.com"])
   - AWS STS rejects tokens without `aud=["sts.amazonaws.com"]` for AssumeRoleWithWebIdentity

3. **Why Projected Tokens Aren't Working**
   - The cluster needs the **private signing key** to mint projected tokens
   - ccoctl generates this key in: `<ccoctl-output>/tls/bound-service-account-signing-key.key`
   - **Our code was NOT copying this directory to the installation directory**
   - Without the key, the cluster can't sign tokens, so pods fall back to default tokens

### Evidence

#### From sanders13 investigation:
```bash
# Service account token decoded:
{
  "iss": "https://sanders13-2r5cl-oidc.s3.us-east-1.amazonaws.com",
  "aud": ["openshift"],  # ← WRONG! Should be ["sts.amazonaws.com"]
  "sub": "system:serviceaccount:openshift-image-registry:cluster-image-registry-operator"
}
```

#### IAM OIDC Provider (correctly configured):
```json
{
  "Url": "sanders13-2r5cl-oidc.s3.us-east-1.amazonaws.com",
  "ClientIDList": ["openshift", "sts.amazonaws.com"],  # ← Both accepted
  "ThumbprintList": ["946e24da38a41bd708c5384de40f235c256c0722"]
}
```

#### Code Bug (internal/installer/installer.go:380-383):
```go
for _, file := range files {
    if file.IsDir() {
        continue  # ← SKIPS DIRECTORIES! tls/ directory never copied
    }
    // ...
}
```

---

## The Fix

### File: `internal/installer/installer.go`

**Function to modify:** `copyManifests(ccoOutputDir, workDir string)`

**Current behavior:**
- Copies files from `<ccoctl-output>/manifests/*.yaml` to `<install-dir>/manifests/`
- **Skips all directories** (line 381-383)

**Required behavior:**
- Copy files from `<ccoctl-output>/manifests/*.yaml` to `<install-dir>/manifests/`
- **Copy entire `<ccoctl-output>/tls/` directory to `<install-dir>/tls/`**

### Code Changes

#### 1. Add recursive directory copy helper function

Add after `getClusterInfo()` function (around line 252):

```go
// copyDir recursively copies a directory tree, preserving file modes
func copyDir(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)

		info, err := d.Info()
		if err != nil {
			return err
		}

		if d.IsDir() {
			return os.MkdirAll(target, info.Mode())
		}

		// Read and write file, preserving mode
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			return err
		}
		if err := os.WriteFile(target, data, info.Mode()); err != nil {
			return err
		}
		log.Printf("Copied: %s (mode %o)", rel, info.Mode())
		return nil
	})
}
```

#### 2. Update copyManifests() function

Add after line 398 (after copying manifests):

```go
	// CRITICAL: Copy tls/ directory containing bound-service-account-signing-key.key
	// These TLS assets are required for the installer to configure STS/bound-token behavior
	// See: https://docs.openshift.com/container-platform/4.9/authentication/managing_cloud_provider_credentials/cco-mode-sts.html
	srcTLSDir := filepath.Join(ccoOutputDir, "tls")
	dstTLSDir := filepath.Join(workDir, "tls")

	if st, err := os.Stat(srcTLSDir); err == nil && st.IsDir() {
		log.Printf("Copying tls/ directory to install directory...")
		if err := copyDir(srcTLSDir, dstTLSDir); err != nil {
			return fmt.Errorf("copy tls dir: %w", err)
		}
		log.Printf("✓ Copied tls/ directory")

		// Validate that the critical signing key exists
		// This turns a 90-minute cluster failure into a 2-second error
		keyPath := filepath.Join(dstTLSDir, "bound-service-account-signing-key.key")
		if _, err := os.Stat(keyPath); err != nil {
			return fmt.Errorf("missing required STS signing key %s: %w", keyPath, err)
		}
		log.Printf("✓ Validated STS signing key exists: %s", keyPath)
	} else {
		log.Printf("Warning: tls/ directory not found in ccoctl output (STS may fail)")
	}
```

#### 3. Add import for fs package

At the top of the file, ensure `io/fs` is imported:

```go
import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io/fs"  // ADD THIS
	"log"
	// ... other imports
)
```

### Why This Works

1. **ccoctl creates TLS assets:** `ccoctl aws create-all` generates `tls/bound-service-account-signing-key.key` and related configuration
2. **Installer copies TLS directory:** Our updated code recursively copies `<ccoctl-output>/tls/` to `<install-dir>/tls/`
3. **openshift-install applies STS configuration:** `openshift-install create cluster` uses the TLS assets to configure the cluster for STS/bound-token behavior
4. **Cluster enables projected tokens:** API server and controller-manager are configured to mint service account tokens with custom audiences
5. **Operators use projected tokens:** CCO-generated manifests configure pods to mount projected tokens with `aud=["sts.amazonaws.com"]`
6. **AWS STS accepts tokens:** Tokens have correct audience matching IAM OIDC provider's client ID list

**Corrected causal chain:**
- Missing `tls/` assets → installer doesn't apply the intended STS/bound-token configuration → operator pods use tokens with default audience → STS rejects

---

## Testing Plan

1. **Apply the fix** to `internal/installer/installer.go`
2. **Rebuild worker binary:**
   ```bash
   GOOS=linux GOARCH=amd64 go build -o bin/ocpctl-worker ./cmd/worker
   ```
3. **Deploy to EC2:**
   ```bash
   scp bin/ocpctl-worker ec2-user@54.205.91.62:/tmp/
   ssh ec2-user@54.205.91.62 "sudo mv /tmp/ocpctl-worker /opt/ocpctl/bin/ocpctl-worker && sudo systemctl restart ocpctl-worker"
   ```
4. **Create test cluster (sanders14):**
   ```bash
   # Via API or CLI
   curl -X POST http://54.205.91.62:8080/api/clusters \
     -H "Content-Type: application/json" \
     -d '{"name":"sanders14","platform":"aws","version":"4.20.3","profile":"aws-sno-test"}'
   ```
5. **Monitor for success:**
   - Watch for `Copied TLS file: bound-service-account-signing-key.key` in logs
   - Verify no InvalidIdentityToken errors after ~20 minutes
   - Check cluster operators reach Available=True

### Verification Commands

After sanders14 boots:

```bash
# 1. Verify credentials secret is in STS format
oc get secret -n openshift-image-registry installer-cloud-credentials -o json \
  | jq -r .data.credentials | base64 -d

# Should contain:
# role_arn = arn:aws:iam::346869059911:role/sanders14-xxxxx-openshift-image-registry-installer-cloud-credentials
# web_identity_token_file = /var/run/secrets/openshift/serviceaccount/token

# 2. Verify image-registry operator projected token has correct audience
POD=$(oc get pods -n openshift-image-registry -l name=cluster-image-registry-operator -o jsonpath='{.items[0].metadata.name}')
# Use the exact path from web_identity_token_file above
TOKEN=$(oc exec -n openshift-image-registry $POD -- cat /var/run/secrets/openshift/serviceaccount/token)
echo $TOKEN | cut -d. -f2 | base64 -d | jq .aud

# Should output: ["sts.amazonaws.com"]  (or contain it)

# 3. CRITICAL: Verify Cloud Credential Operator itself has correct audience
# This is the definitive test - if CCO has the right token, everything else will work
POD=$(oc -n openshift-cloud-credential-operator get pod -l app=cloud-credential-operator -o jsonpath='{.items[0].metadata.name}')
TOKEN=$(oc -n openshift-cloud-credential-operator exec "$POD" -- cat /var/run/secrets/openshift/serviceaccount/token)
echo "$TOKEN" | cut -d. -f2 | base64 -d | jq .aud

# Should output: ["sts.amazonaws.com"]  (or contain it)
# If this flips from ["openshift"] to ["sts.amazonaws.com"], the fix is confirmed!

# 4. Alternative: Check Kubernetes default mount path if above paths don't exist
# TOKEN=$(oc exec -n <namespace> $POD -- cat /var/run/secrets/kubernetes.io/serviceaccount/token)
```

---

## References

- [OpenShift STS Manual Mode Documentation](https://docs.openshift.com/container-platform/4.9/authentication/managing_cloud_provider_credentials/cco-mode-sts.html)
- [Cloud Credential Operator STS Docs](https://github.com/openshift/cloud-credential-operator/blob/master/docs/sts.md)
- [AWS STS AssumeRoleWithWebIdentity](https://docs.aws.amazon.com/STS/latest/APIReference/API_AssumeRoleWithWebIdentity.html)
- [Bound Service Account Tokens](https://docs.openshift.com/container-platform/4.11/authentication/bound-service-account-tokens.html)

---

## Lessons Learned

1. **Always check the official workflow:** We should have verified we're following the exact ccoctl procedure from Red Hat docs
2. **Directory copying matters:** Skipping directories in file copy operations can have non-obvious consequences
3. **Token types matter:** Default vs projected service account tokens have different properties (audience, expiration)
4. **Audience claim is critical for AWS STS:** The `aud` field in JWT tokens must match what the IAM OIDC provider client ID list expects
5. **Private keys enable token signing:** Without the signing key, the cluster can't mint custom tokens

---

## Timeline

- **Initial failures:** 25+ clusters failed with InvalidIdentityToken
- **Investigation 1:** Suspected infraID mismatch → proved wrong with sanders13 (consistent infraID still failed)
- **Investigation 2:** Suspected audience incompatibility → proved wrong (IAM provider accepts both audiences)
- **Investigation 3:** Checked token content → found wrong audience (`openshift` instead of `sts.amazonaws.com`)
- **Root cause identified:** Missing `tls/` directory prevents projected token minting
- **Fix implemented:** Updated `copyManifests()` to include `tls/` directory
