# OIDC Issuer Fix Plan

## Problem Summary

After 25+ failed cluster installation attempts, we've identified the root cause:

**InvalidIdentityToken errors from AWS STS** - Operators cannot authenticate to AWS because of an issuer mismatch between:
- The issuer claim in service account JWT tokens issued by the cluster
- The OIDC provider configuration in IAM

## Root Cause (To Be Verified)

Cluster operators are failing with **InvalidIdentityToken** errors from AWS STS. This means:
- Service account tokens being presented to AWS don't match what IAM expects
- Could be: wrong issuer, wrong audience, wrong subject, or mismatched trust policy

**What we need to verify:**
1. What issuer is the cluster ACTUALLY minting in service account tokens?
2. Does that match the OIDC discovery document issuer?
3. Does the IAM OIDC provider correspond to that issuer?
4. Do role trust policies match the token's aud and sub claims?

**Note on AWS OIDC Provider Display:**
The IAM `get-open-id-connect-provider` command shows `Url` without `https://` - this is normal AWS behavior and NOT a bug. The provider is created WITH `https://`, AWS just displays the host/path only. The ARN format is always `arn:aws:iam::account:oidc-provider/host/path` (no scheme).

## The Fix

### Step 1: Cleanup Current Cluster

**Action:** Destroy cluster c3bd49e0-f1b5-4461-89c8-7f83f9ccb5b9
- Kill running openshift-install process
- Run openshift-install destroy (will likely panic on Route53, that's OK)
- Manually clean up remaining resources:
  - IAM OIDC provider
  - IAM roles (6 roles created by ccoctl)
  - S3 bucket for OIDC discovery
  - Route53 DNS records
  - EC2 instances, VPC, load balancers

### Step 2: Add OIDC Validation After ccoctl

**File:** `/Users/tsanders/Workspace2/ocpctl/internal/installer/installer.go`

**IMPORTANT:** The code currently uses `clusterName` (filepath.Base(workDir)) which is a UUID, not the human-readable cluster name. This happens to be the infraID. However, for clarity, we should:
1. Extract infraID explicitly from the state file after manifests are created
2. Use infraID consistently in fixOIDCThumbprint and validateOIDCConfiguration

**Add validation function after ccoctl runs to verify OIDC configuration is correct:**

```go
// validateOIDCConfiguration verifies OIDC provider and discovery documents are correctly configured
// This catches configuration mismatches before cluster installation starts
func (i *Installer) validateOIDCConfiguration(ctx context.Context, infraID, region string) error {
    log.Printf("Validating OIDC configuration for infraID=%s, region=%s", infraID, region)

    // Define canonical issuer URL (using infraID, not human cluster name)
    issuerHost := fmt.Sprintf("%s-oidc.s3.%s.amazonaws.com", infraID, region)
    canonicalIssuer := fmt.Sprintf("https://%s", issuerHost)

    // Get AWS account ID
    accountID, err := i.getAWSAccountID(ctx)
    if err != nil {
        return fmt.Errorf("get AWS account ID: %w", err)
    }

    // Check 1: Verify discovery document issuer
    s3Bucket := fmt.Sprintf("%s-oidc", infraID)
    s3Key := ".well-known/openid-configuration"

    getDiscoveryCmd := exec.CommandContext(ctx, "aws", "s3", "cp",
        fmt.Sprintf("s3://%s/%s", s3Bucket, s3Key),
        "-",
        "--region", region)

    var discoveryOut, discoveryErr bytes.Buffer
    getDiscoveryCmd.Stdout = &discoveryOut
    getDiscoveryCmd.Stderr = &discoveryErr
    if err := getDiscoveryCmd.Run(); err != nil {
        return fmt.Errorf("fetch discovery document: %w\nStderr: %s", err, discoveryErr.String())
    }

    var discoveryDoc struct {
        Issuer   string `json:"issuer"`
        JwksURI  string `json:"jwks_uri"`
    }
    if err := json.Unmarshal(discoveryOut.Bytes(), &discoveryDoc); err != nil {
        return fmt.Errorf("parse discovery document: %w\nContent: %s", err, discoveryOut.String())
    }

    if discoveryDoc.Issuer != canonicalIssuer {
        return fmt.Errorf("discovery doc issuer mismatch: expected %s, got %s",
            canonicalIssuer, discoveryDoc.Issuer)
    }
    log.Printf("✓ Discovery document issuer correct: %s", discoveryDoc.Issuer)

    // Verify jwks_uri host matches issuer host (optional sanity check)
    expectedJwksURI := fmt.Sprintf("https://%s/keys.json", issuerHost)
    if discoveryDoc.JwksURI != expectedJwksURI {
        log.Printf("Warning: jwks_uri mismatch (expected %s, got %s)", expectedJwksURI, discoveryDoc.JwksURI)
    }

    // Check 2: Verify IAM OIDC provider exists and corresponds to canonical issuer
    providerARN := fmt.Sprintf("arn:aws:iam::%s:oidc-provider/%s", accountID, issuerHost)

    getProviderCmd := exec.CommandContext(ctx, "aws", "iam", "get-open-id-connect-provider",
        "--open-id-connect-provider-arn", providerARN,
        "--output", "json")

    var providerOut, providerErr bytes.Buffer
    getProviderCmd.Stdout = &providerOut
    getProviderCmd.Stderr = &providerErr
    if err := getProviderCmd.Run(); err != nil {
        return fmt.Errorf("get OIDC provider %s: %w\nStderr: %s", providerARN, err, providerErr.String())
    }

    var providerInfo struct {
        Url            string   `json:"Url"`
        ClientIDList   []string `json:"ClientIDList"`
        ThumbprintList []string `json:"ThumbprintList"`
    }
    if err := json.Unmarshal(providerOut.Bytes(), &providerInfo); err != nil {
        return fmt.Errorf("parse provider info: %w", err)
    }

    // AWS displays Url without scheme - verify host/path matches
    if providerInfo.Url != issuerHost {
        return fmt.Errorf("OIDC provider URL mismatch: expected %s, got %s",
            issuerHost, providerInfo.Url)
    }
    log.Printf("✓ IAM OIDC provider URL correct: %s", providerInfo.Url)

    // Check 3: Verify client IDs include sts.amazonaws.com (required for AWS STS)
    hasSTSAudience := false
    for _, clientID := range providerInfo.ClientIDList {
        if clientID == "sts.amazonaws.com" {
            hasSTSAudience = true
            break
        }
    }
    if !hasSTSAudience {
        return fmt.Errorf("OIDC provider missing required client ID: sts.amazonaws.com (has: %v)",
            providerInfo.ClientIDList)
    }
    log.Printf("✓ OIDC provider has sts.amazonaws.com in client IDs: %v", providerInfo.ClientIDList)

    // Check 4: Verify thumbprint is non-empty
    // Note: Don't recompute thumbprint against wrong host - trust what fixOIDCThumbprint set
    if len(providerInfo.ThumbprintList) == 0 {
        return fmt.Errorf("OIDC provider has empty thumbprint list")
    }
    log.Printf("✓ OIDC provider thumbprint: %s", providerInfo.ThumbprintList[0])

    // Check 5: Verify at least one IAM role trust policy is correctly configured
    // Pick a deterministic role name that ccoctl always creates
    testRoleName := fmt.Sprintf("%s-openshift-cloud-credential-", infraID)

    getRoleCmd := exec.CommandContext(ctx, "aws", "iam", "get-role",
        "--role-name", testRoleName,
        "--output", "json")

    var roleOut, roleErr bytes.Buffer
    getRoleCmd.Stdout = &roleOut
    getRoleCmd.Stderr = &roleErr
    if err := getRoleCmd.Run(); err != nil {
        log.Printf("Warning: could not verify role trust policy for %s: %v", testRoleName, err)
    } else {
        var roleInfo struct {
            Role struct {
                AssumeRolePolicyDocument struct {
                    Statement []struct {
                        Principal struct {
                            Federated string `json:"Federated"`
                        } `json:"Principal"`
                        Condition struct {
                            StringEquals map[string]string `json:"StringEquals"`
                        } `json:"Condition"`
                    } `json:"Statement"`
                } `json:"AssumeRolePolicyDocument"`
            } `json:"Role"`
        }
        if err := json.Unmarshal(roleOut.Bytes(), &roleInfo); err != nil {
            log.Printf("Warning: could not parse role trust policy: %v", err)
        } else if len(roleInfo.Role.AssumeRolePolicyDocument.Statement) > 0 {
            stmt := roleInfo.Role.AssumeRolePolicyDocument.Statement[0]

            // Verify Principal.Federated matches provider ARN
            if stmt.Principal.Federated != providerARN {
                return fmt.Errorf("role %s trust policy references wrong provider: expected %s, got %s",
                    testRoleName, providerARN, stmt.Principal.Federated)
            }

            // Verify condition includes aud check
            audKey := fmt.Sprintf("%s:aud", issuerHost)
            if audValue, ok := stmt.Condition.StringEquals[audKey]; !ok {
                log.Printf("Warning: role trust policy missing %s condition", audKey)
            } else if audValue != "sts.amazonaws.com" {
                log.Printf("Warning: role trust policy %s=%s (expected sts.amazonaws.com)", audKey, audValue)
            } else {
                log.Printf("✓ Role trust policy correctly configured for %s", testRoleName)
            }
        }
    }

    log.Printf("✓ OIDC configuration validation passed")
    return nil
}
```

**Update the runCCOCtl function to add validation:**
```go
// After line 208 (after fixOIDCThumbprint completes)
// Add validation step:

// Get infraID from state file for validation
infraID, err := i.getInfraID(workDir)
if err != nil {
    return fmt.Errorf("get infraID: %w", err)
}

// Validate OIDC configuration before proceeding
log.Printf("Validating OIDC configuration...")
if err := i.validateOIDCConfiguration(ctx, infraID, region); err != nil {
    return fmt.Errorf("OIDC validation failed: %w", err)
}

log.Printf("Successfully created IAM resources and credential manifests")
return nil
```

**Add helper to get infraID:**
```go
// getInfraID extracts the infrastructure ID from the openshift install state file
func (i *Installer) getInfraID(workDir string) (string, error) {
    stateFile := filepath.Join(workDir, ".openshift_install_state.json")
    data, err := os.ReadFile(stateFile)
    if err != nil {
        return "", fmt.Errorf("read state file: %w", err)
    }

    var state map[string]json.RawMessage
    if err := json.Unmarshal(data, &state); err != nil {
        return "", fmt.Errorf("parse state file: %w", err)
    }

    clusterIDRaw, ok := state["*installconfig.ClusterID"]
    if !ok {
        return "", fmt.Errorf("ClusterID not found in state file")
    }

    var clusterID struct {
        InfraID string `json:"InfraID"`
    }
    if err := json.Unmarshal(clusterIDRaw, &clusterID); err != nil {
        return "", fmt.Errorf("parse ClusterID: %w", err)
    }

    if clusterID.InfraID == "" {
        return "", fmt.Errorf("InfraID is empty in state file")
    }

    return clusterID.InfraID, nil
}
```

### Step 3: Rebuild and Deploy Worker

```bash
cd /Users/tsanders/Workspace2/ocpctl
GOOS=linux GOARCH=amd64 go build -o bin/ocpctl-worker ./cmd/worker
scp bin/ocpctl-worker ec2-user@54.205.91.62:/tmp/
ssh ec2-user@54.205.91.62 'sudo mv /tmp/ocpctl-worker /opt/ocpctl/bin/ocpctl-worker && sudo systemctl restart ocpctl-worker'
```

### Step 4: Create Fresh Test Cluster

Create a new cluster with a different name to avoid DNS conflicts:
- Cluster name: `sno-verified-v2` or similar
- Monitor for InvalidIdentityToken errors
- Should see operators successfully authenticate to AWS within 10-15 minutes

### Step 5: Validation Checklist

After cluster creation starts, verify:

```bash
# 1. OIDC validation passed during install (check worker logs)
sudo journalctl -u ocpctl-worker -f | grep -i "oidc\|validation"
# Expected: "✓ OIDC configuration validation passed"

# 2. No InvalidIdentityToken errors in operator logs
export KUBECONFIG=/tmp/ocpctl/<cluster-id>/auth/kubeconfig
oc -n openshift-ingress-operator logs deploy/ingress-operator --tail=50
oc -n openshift-cloud-credential-operator logs deploy/cloud-credential-operator --tail=50
# Expected: Successful AWS API calls, no authentication errors

# 3. Operators becoming available
oc get co
# Expected: ingress, authentication, image-registry showing AVAILABLE=True within 20-30 min
```

### Step 6: If Still Failing - Decode Actual Token

If operators still fail with InvalidIdentityToken after the validation passes, we need to see what the cluster is ACTUALLY minting:

```bash
# Get a failing pod's service account token
POD=$(oc -n openshift-cloud-credential-operator get pod -l app=cloud-credential-operator -o jsonpath='{.items[0].metadata.name}')

# Try standard Kubernetes SA token path first
oc -n openshift-cloud-credential-operator exec -it "$POD" -- cat /var/run/secrets/kubernetes.io/serviceaccount/token > /tmp/sa.jwt 2>/dev/null

# If that fails, try OpenShift-specific path
if [ ! -s /tmp/sa.jwt ]; then
  oc -n openshift-cloud-credential-operator exec -it "$POD" -- cat /var/run/secrets/openshift/serviceaccount/token > /tmp/sa.jwt
fi

# Decode the JWT payload
python3 - <<'PY'
import base64, json
t=open("/tmp/sa.jwt").read().strip().split(".")[1]
t += "=" * (-len(t) % 4)
payload = json.loads(base64.urlsafe_b64decode(t))
print(json.dumps(payload, indent=2))
print("\nKey claims:")
print(f"  iss: {payload.get('iss')}")
print(f"  aud: {payload.get('aud')}")
print(f"  sub: {payload.get('sub')}")
PY

# Expected output:
#   iss: https://<cluster-id>-oidc.s3.us-east-1.amazonaws.com
#   aud: ["sts.amazonaws.com"] (or similar, must include sts.amazonaws.com)
#   sub: system:serviceaccount:openshift-cloud-credential-operator:cloud-credential-operator

# If iss is NOT your S3 issuer (e.g., https://kubernetes.default.svc), that's the smoking gun
# The cluster is minting tokens with the wrong issuer
```

**Compare token to trust policy:**
```bash
# Get the role trust policy
aws iam get-role --role-name <cluster-id>-openshift-cloud-credential- --query Role.AssumeRolePolicyDocument

# Verify:
# 1. Principal.Federated matches your OIDC provider ARN
# 2. Condition StringEquals key matches provider host (without https://)
# 3. Condition value matches token's sub claim exactly
```

## Key Invariants to Enforce

The code must enforce these invariants to prevent OIDC authentication failures:

**Invariant A — Single source of truth for issuer**
- Define one canonical issuer string: `https://<cluster-id>-oidc.s3.<region>.amazonaws.com`
- This is what goes in:
  - Discovery document `issuer` field
  - The `--url` parameter when creating IAM OIDC provider
  - Any OpenShift serviceAccountIssuer configuration (if set)

**Invariant B — Canonicalize to https:// for issuer**
- Always use `https://` prefix in the issuer URL
- AWS IAM `create-open-id-connect-provider --url` requires https://
- When comparing, normalize by stripping scheme (AWS displays URL without scheme)

**Invariant C — Validate before cluster creation**
After ccoctl/OIDC setup completes but BEFORE starting `openshift-install create cluster`:
1. Fetch discovery doc from S3 and verify `issuer` equals canonical issuer
2. Verify IAM OIDC provider exists with matching host/path
3. Verify client IDs include `sts.amazonaws.com` (required for AWS STS AssumeRoleWithWebIdentity)
4. Verify thumbprint matches S3 certificate
5. If ANY validation fails, fail fast with clear error - don't proceed with installation

**Invariant D — Token validation (post-install debugging)**
If operators fail after passing validation:
- Decode actual service account token from failing pod
- Verify `iss` claim matches expected issuer
- Verify `aud` claim includes `sts.amazonaws.com`
- Verify `sub` claim matches role trust policy condition

## Cleanup Commands (for current broken cluster)

```bash
# Kill install process
sudo pkill -f "openshift-install.*c3bd49e0"

# Delete OIDC provider
aws iam delete-open-id-connect-provider \
  --open-id-connect-provider-arn arn:aws:iam::346869059911:oidc-provider/c3bd49e0-f1b5-4461-89c8-7f83f9ccb5b9-oidc.s3.us-east-1.amazonaws.com

# Delete IAM roles
for role in $(aws iam list-roles --query 'Roles[?contains(RoleName, `c3bd49e0`)].RoleName' --output text); do
  # Detach policies first
  aws iam list-attached-role-policies --role-name $role --query 'AttachedPolicies[].PolicyArn' --output text | \
    xargs -n1 aws iam detach-role-policy --role-name $role --policy-arn

  # Delete inline policies
  aws iam list-role-policies --role-name $role --query 'PolicyNames[]' --output text | \
    xargs -n1 aws iam delete-role-policy --role-name $role --policy-name

  # Delete role
  aws iam delete-role --role-name $role
done

# Delete S3 bucket
aws s3 rb s3://c3bd49e0-f1b5-4461-89c8-7f83f9ccb5b9-oidc --force

# Delete Route53 records
aws route53 list-resource-record-sets \
  --hosted-zone-id Z2GE8CSGW2ZA8W \
  --query "ResourceRecordSets[?contains(Name, 'sno-final-test')].Name" \
  --output text | xargs -n1 echo "Delete manually"

# Terminate EC2 instances (replace <infraID> with actual infraID, e.g., sno-final-test-wmnbx)
INFRA_ID="<infraID>"  # Get from metadata.json or state file
aws ec2 terminate-instances \
  --instance-ids $(aws ec2 describe-instances \
    --filters "Name=tag:kubernetes.io/cluster/${INFRA_ID},Values=owned" \
    --query "Reservations[].Instances[].InstanceId" --output text)
```

## Timeline Estimate

- Step 1 (Cleanup): 10 minutes
- Step 2 (Code fix): 15 minutes
- Step 3 (Build/deploy): 5 minutes
- Step 4 (Create cluster): 30-40 minutes
- **Total: ~60-70 minutes**

## Success Criteria

Cluster installation completes successfully with:
- All cluster operators AVAILABLE=True
- No InvalidIdentityToken errors
- Operators successfully authenticating to AWS via STS
- Web console accessible
- oc commands work without timeout errors
