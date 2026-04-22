# Pull Secret Mismatch - Diagnosis and Fix

## Root Cause

OpenShift 4.22.0-ec.5 deployments fail because ocpctl is using a different pull secret than the successful manual tests.

### Pull Secret Sources

| Test Type | Pull Secret Source | Status |
|-----------|-------------------|--------|
| **Manual Test** | `$OPENSHIFT_PULL_SECRET` environment variable | ✅ Works |
| **ocpctl Deployment** | AWS Secrets Manager | ❌ Fails at bootstrap |

## Diagnosis

### 1. Check what pull secret ocpctl is using

SSH to the worker instance and check:

```bash
# Check the worker environment configuration
cat /etc/ocpctl/worker.env | grep PULL_SECRET

# If using file-based:
sudo cat /etc/ocpctl/pull-secret.json | jq

# If using Secrets Manager:
# Get the secret name from worker environment
SECRET_NAME=$(grep OPENSHIFT_PULL_SECRET_NAME /etc/ocpctl/worker.env | cut -d= -f2)
aws secretsmanager get-secret-value --secret-id "$SECRET_NAME" --query SecretString --output text | jq
```

### 2. Compare with the working pull secret

```bash
# Show the pull secret used in successful manual test
echo $OPENSHIFT_PULL_SECRET | jq
```

### 3. Check for differences

Key things to verify:
- Are the pull secrets identical?
- Does the Secrets Manager pull secret have access to `quay.io/openshift-release-dev`?
- Is the pull secret expired? (check `.auths."cloud.openshift.com".auth` expiration)

## Fix

### Option 1: Update AWS Secrets Manager (Recommended)

Replace the invalid pull secret in AWS Secrets Manager with the valid one:

```bash
# Get the valid pull secret from your environment
VALID_PULL_SECRET="$OPENSHIFT_PULL_SECRET"

# Update AWS Secrets Manager
# Find the secret name/ARN first:
aws secretsmanager list-secrets | jq '.SecretList[] | select(.Name | contains("pull-secret"))'

# Update the secret (replace SECRET_NAME with actual name)
aws secretsmanager put-secret-value \
  --secret-id "SECRET_NAME" \
  --secret-string "$VALID_PULL_SECRET"

# Restart worker to pick up new secret
sudo systemctl restart ocpctl-worker

# Verify worker loaded new secret successfully
sudo journalctl -u ocpctl-worker -n 50 | grep "pull secret"
```

### Option 2: Use File-Based Pull Secret (Autoscaling Workers)

If using autoscaling workers deployed via Terraform, update the `openshift_pull_secret` variable:

```hcl
# terraform/worker-autoscaling/terraform.tfvars
openshift_pull_secret = <<-EOT
{
  "auths": {
    "cloud.openshift.com": {
      "auth": "...",
      "email": "..."
    },
    "quay.io": {
      "auth": "...",
      "email": "..."
    },
    # ... rest of your valid pull secret
  }
}
EOT
```

Then redeploy:

```bash
cd terraform/worker-autoscaling
terraform apply
```

## Verification

After updating the pull secret, create a test cluster:

```bash
# Through UI or API
curl -X POST http://your-api-server/api/v1/clusters \
  -H "Content-Type: application/json" \
  -d '{
    "name": "test-ec5-pullsecret",
    "platform": "aws",
    "clusterType": "openshift",
    "version": "4.22.0-ec.5",
    "profile": "aws-sno-test",
    "region": "us-west-2"
  }'
```

Watch the logs for successful image pulls:

```bash
# On worker instance
sudo tail -f /tmp/ocpctl/{cluster-id}/.openshift_install.log | grep -i "pull\|bootstrap"
```

Success indicators:
- Bootstrap VM boots successfully
- No "pull secret" or "unauthorized" errors
- API becomes available within 20 minutes

## Related Files

- `cmd/worker/main.go:202-274` - Pull secret loading logic
- `internal/worker/handler_create.go:98-101` - Pull secret validation
- `deploy/systemd/ocpctl-worker.service:10` - Environment file location
- `terraform/worker-autoscaling/user-data.sh:51-56` - Pull secret file creation

## Notes

- Pull secrets from Red Hat Cloud Console work for both stable AND dev-preview versions
- No special pull secret is needed for early candidate (ec) builds
- The pull secret must have access to `quay.io/openshift-release-dev` registry
- If your pull secret is more than 60 days old, it may have expired - get a fresh one from https://console.redhat.com/openshift/install/pull-secret
