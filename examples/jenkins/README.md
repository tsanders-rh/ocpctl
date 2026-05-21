# Jenkins Pipeline for OCPCTL Automated Testing

This directory contains a complete Jenkins pipeline implementation for provisioning OpenShift clusters via the ocpctl API and running automated tests against them.

## Overview

The pipeline:
1. **Provisions** OpenShift clusters on AWS using ocpctl REST API
2. **Waits** for cluster to reach READY status (30-60 minutes typically)
3. **Retrieves** kubeconfig and cluster credentials
4. **Executes** custom test suites against the cluster
5. **Cleans up** by deleting the cluster (unless tests/provisioning failed and preservation is enabled)
6. **Archives** kubeconfig and test results as Jenkins artifacts

## Features

- **Dual Authentication**: Supports both API keys (portable) and EC2 IAM roles (for Jenkins on AWS)
- **Smart Cleanup**: Preserves clusters on provisioning or test failures for debugging
- **Comprehensive Testing**: Includes example test suite with cluster validation
- **Artifact Collection**: Automatically archives kubeconfig and test results
- **Error Handling**: Automatic retry logic for transient API errors with exponential backoff
- **Progress Tracking**: Clear status updates during long-running operations

## Prerequisites

### Required
- **Jenkins 2.x** with Pipeline plugin
- **Network access** to ocpctl API (https://ocpctl.mg.dog8code.com)
- **OpenShift CLI** (`oc`) installed on Jenkins agent (for running tests)
- **One of the following** for authentication:
  - API key from ocpctl (recommended for any environment)
  - EC2 instance role with appropriate permissions (for Jenkins on AWS)

### Optional
- `jq` for JSON parsing (used by pipeline)
- `curl` with AWS SigV4 support (for IAM authentication)

## Setup Instructions

### Option 1: API Key Authentication (Recommended)

This method works in any Jenkins environment (on-prem, cloud, EC2, or outside AWS).

#### Step 1: Create API Key in ocpctl

1. Login to ocpctl web UI: https://ocpctl.mg.dog8code.com
2. Navigate to: **User menu → API Keys**
3. Click **"Create API Key"**
4. Fill in the form:
   - **Name**: `Jenkins CI/CD`
   - **Scope**: `Full Access`
   - **Expiration Date**: (Optional) Select a date, e.g., 1 year from now, or leave empty for no expiration
5. Click **"Create API Key"**
6. **IMPORTANT**: Copy the API key immediately (starts with `ocpctl_`)
   - This is the only time you'll see it!
   - Example: `ocpctl_K3xZ9mQ2pL8vN4cD6jH5bG7wF1sT9xYuM3qR6nP2...`

#### Step 2: Add API Key to Jenkins Credentials

1. Go to: **Jenkins → Credentials → System → Global credentials**
2. Click **"Add Credentials"**
3. Configure:
   - **Kind**: `Secret text`
   - **Scope**: `Global`
   - **Secret**: `[paste your API key]`
   - **ID**: `ocpctl-api-key` (exact ID required!)
   - **Description**: `ocpctl API Key for cluster provisioning`
4. Click **"Create"**

#### Step 3: Verify API Key Works

```bash
# Test from command line
curl -X GET https://ocpctl.mg.dog8code.com/api/v1/profiles \
  -H "Authorization: Bearer ocpctl_YOUR_KEY"

# Expected: 200 OK with JSON list of profiles
```

### Option 2: EC2 IAM Authentication (For Jenkins on AWS)

This method is ideal if your Jenkins runs on EC2 - it uses the instance role automatically without storing credentials.

#### Step 1: Verify IAM Authentication is Enabled

Check with your ocpctl administrator that:
- `ENABLE_IAM_AUTH=true` is set in ocpctl API server configuration
- Your IAM user/role is in the allowed group (if group enforcement is enabled)

#### Step 2: Grant EC2 Instance Role Permissions

Add this policy to your Jenkins EC2 instance role:

```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Action": [
        "sts:GetCallerIdentity"
      ],
      "Resource": "*"
    }
  ]
}
```

#### Step 3: Add to IAM Group (if required)

If ocpctl has `IAM_ALLOWED_GROUP` configured:

```bash
# Add your instance role or IAM user to the allowed group
aws iam add-user-to-group \
  --user-name jenkins-user \
  --group-name ocpctl-users
```

#### Step 4: Verify IAM Auth Works

```bash
# Test from EC2 instance (no credentials needed!)
curl -X GET https://ocpctl.mg.dog8code.com/api/v1/profiles \
  --aws-sigv4 "aws:amz:us-east-1:execute-api"

# Expected: 200 OK with JSON list of profiles
```

## Creating the Jenkins Pipeline Job

### Step 1: Create Pipeline Job

1. Go to Jenkins dashboard
2. Click **"New Item"**
3. Enter name: `openshift-cluster-test`
4. Select **"Pipeline"**
5. Click **"OK"**

### Step 2: Configure Pipeline

1. In the **Pipeline** section:
   - **Definition**: `Pipeline script from SCM`
   - **SCM**: `Git`
   - **Repository URL**: `https://github.com/tsanders-rh/ocpctl.git` (or your fork)
   - **Branch**: `main`
   - **Script Path**: `examples/jenkins/Jenkinsfile`
2. Click **"Save"**

### Alternative: Inline Pipeline

If you prefer to paste the Jenkinsfile content directly:
1. **Definition**: `Pipeline script`
2. Copy the entire contents of `Jenkinsfile` into the **Script** text box
3. Click **"Save"**

## Usage

### Running the Pipeline

1. Go to your pipeline job
2. Click **"Build with Parameters"**
3. Configure parameters:

#### Required Parameters

| Parameter | Description | Example |
|-----------|-------------|---------|
| **AUTH_METHOD** | Authentication method | `API_KEY` or `EC2_IAM` |
| **CLUSTER_NAME_PREFIX** | Cluster name prefix | `jenkins-test` |
| **PROFILE** | Cluster profile | `aws-sno-ga` |
| **OPENSHIFT_VERSION** | OpenShift version | `4.22.0` |
| **REGION** | AWS region | `us-east-1` |
| **BASE_DOMAIN** | DNS base domain | `mg.dog8code.com` |
| **OWNER_EMAIL** | Owner email | `your-email@example.com` |
| **TEAM** | Team name | `platform-engineering` |
| **COST_CENTER** | Cost center code | `eng-001` |
| **TTL_HOURS** | Time-to-live (hours) | `24` |
| **PRESERVE_ON_FAILURE** | Preserve on failure | `true` (recommended) |
| **TEST_SCRIPT_PATH** | Path to test script | `./examples/jenkins/run-tests.sh` |

4. Click **"Build"**
5. Monitor progress in **Console Output**

### Understanding Pipeline Stages

The pipeline executes in 5 stages:

```
1. Validate (1 min)
   └─ Verify test script exists and credentials are configured

2. Create Cluster (1 min)
   └─ POST /api/v1/clusters to create cluster

3. Wait for Ready (30-60 min)
   └─ Poll cluster status every 30s until READY or FAILED

4. Get Credentials (1 min)
   └─ GET /api/v1/clusters/:id/outputs to retrieve kubeconfig

5. Run Tests (varies)
   └─ Execute custom test script with cluster credentials

Post: Cleanup
   └─ Archive artifacts and conditionally delete cluster
```

### Expected Timeline

- **Single Node OpenShift (SNO)**: 30-45 minutes total
- **3-Node HA OpenShift**: 45-60 minutes total
- **Test Execution**: Depends on your test suite (example: 5-10 minutes)

## Writing Custom Test Scripts

The example `run-tests.sh` demonstrates best practices. Your test script will receive these environment variables:

| Variable | Description | Example |
|----------|-------------|---------|
| `KUBECONFIG` | Path to kubeconfig | `/var/jenkins/workspace/job/kubeconfig-jenkins-test-123.yaml` |
| `CLUSTER_NAME` | Cluster name | `jenkins-test-123` |
| `CLUSTER_ID` | Cluster UUID | `550e8400-e29b-41d4-a716-446655440000` |
| `CLUSTER_API_URL` | Kubernetes API URL | `https://api.cluster.example.com:6443` |
| `CLUSTER_CONSOLE_URL` | OpenShift console | `https://console-openshift-console.apps.cluster.example.com` |
| `KUBEADMIN_PASSWORD` | Admin password | `AbCdEfGh1234567890` |

### Minimal Test Script

```bash
#!/bin/bash
set -e  # Exit on error

echo "Testing cluster: ${CLUSTER_NAME}"

# Verify cluster is accessible
oc get nodes

# Run your tests
oc get clusteroperators
oc get pods -A

echo "Tests passed!"
exit 0
```

### Test Script Best Practices

1. **Exit codes**: Exit `0` for success, non-zero for failure
2. **Error handling**: Use `set -e` to fail fast on errors
3. **Output**: Print clear messages for debugging
4. **Artifacts**: Save results to `test-results/` directory (auto-archived)
5. **Cleanup**: Delete test resources before script exits
6. **Timeouts**: Use `--timeout` flags to avoid hanging

## Troubleshooting

### Problem: "API key credential not found"

**Solution**:
- Verify credential exists: **Jenkins → Credentials → Global credentials**
- Credential ID must be exactly `ocpctl-api-key`
- Credential type must be "Secret text"
- If using EC2_IAM, select that option instead of API_KEY

### Problem: "Failed to create cluster. HTTP 401: Unauthorized"

**Cause**: Invalid or expired API key

**Solution**:
- Test API key: `curl -H "Authorization: Bearer $KEY" https://ocpctl.mg.dog8code.com/api/v1/profiles`
- If expired/invalid, create new key and update Jenkins credential

### Problem: "Cluster stuck in CREATING status"

**Cause**: Cluster provisioning is taking longer than expected or has failed

**Solution**:
- Check ocpctl worker logs on production server
- Verify cluster in ocpctl web UI
- Pipeline will timeout after 120 minutes and preserve cluster for investigation
- Check `/var/lib/ocpctl/clusters/<cluster-name>/.openshift_install.log` on worker

### Problem: "Tests failed but cluster was deleted"

**Cause**: `PRESERVE_ON_FAILURE` parameter was set to `false`

**Solution**:
- Set `PRESERVE_ON_FAILURE=true` to keep clusters on failure
- Cluster will remain accessible for debugging
- Delete manually when done: `curl -X DELETE https://ocpctl.mg.dog8code.com/api/v1/clusters/:id`

### Problem: "API returns 429 Too Many Requests"

**Cause**: Rate limiting (300 requests/minute global limit)

**Solution**:
- Pipeline includes automatic retry with exponential backoff
- If persistent, contact ocpctl admin to increase rate limits
- Avoid running many parallel builds

### Problem: "EC2 IAM authentication not working"

**Cause**: Missing permissions or IAM group membership

**Solution**:
1. Verify `sts:GetCallerIdentity` permission on instance role
2. Check IAM group membership if group enforcement enabled
3. Test: `curl --aws-sigv4 "aws:amz:us-east-1:execute-api" https://ocpctl.mg.dog8code.com/api/v1/profiles`
4. If 403 Forbidden, add instance role to allowed IAM group

### Problem: "oc command not found"

**Cause**: OpenShift CLI not installed on Jenkins agent

**Solution**:
```bash
# Install oc on Jenkins agent
wget https://mirror.openshift.com/pub/openshift-v4/clients/ocp/latest/openshift-client-linux.tar.gz
tar xzf openshift-client-linux.tar.gz
sudo mv oc /usr/local/bin/
oc version --client
```

## Advanced Usage

### Running Multiple Parallel Tests

To test multiple profiles or versions in parallel:

1. Create separate pipeline jobs for each configuration
2. Trigger them in parallel using a multi-branch pipeline or Jenkins job DSL
3. **Note**: Be mindful of rate limits (300 req/min)

### Integrating with GitHub Pull Requests

Use Jenkins GitHub plugin to trigger tests on PRs:

```groovy
// In your main repo's Jenkinsfile
stage('Test on OpenShift') {
    steps {
        build job: 'openshift-cluster-test', parameters: [
            string(name: 'CLUSTER_NAME_PREFIX', value: "pr-${env.CHANGE_ID}"),
            string(name: 'TEST_SCRIPT_PATH', value: './ci/integration-tests.sh')
        ]
    }
}
```

### Customizing Cleanup Behavior

Edit the Jenkinsfile `shouldDeleteCluster()` function to customize logic:

```groovy
def shouldDeleteCluster() {
    // Example: Always delete on weekends
    def dayOfWeek = new Date().format('E')
    if (dayOfWeek in ['Sat', 'Sun']) {
        return true
    }

    // Original logic
    if (params.PRESERVE_ON_FAILURE == false) return true
    if (env.PROVISIONING_FAILED == 'true') return false
    if (env.TESTS_FAILED == 'true') return false
    return true
}
```

### Using Different Profiles

The pipeline supports all AWS OpenShift profiles:

| Profile | Description | Nodes | Use Case |
|---------|-------------|-------|----------|
| `aws-sno-ga` | Single Node OpenShift | 1 | Development, testing |
| `aws-standard-ga` | 3-node HA cluster | 1 control + 2 workers | Production-like |
| `aws-minimal-ga` | Minimal 3-node HA | 3 combined | Cost-optimized |
| `aws-virt-windows-minimal-ga` | With OpenShift Virtualization | 3 nodes | Windows VM testing |

## Files in This Directory

| File | Description |
|------|-------------|
| `Jenkinsfile` | Main pipeline implementation (declarative pipeline) |
| `run-tests.sh` | Example test script with comprehensive cluster validation |
| `README.md` | This file - setup and usage documentation |

## Additional Resources

- **OCPCTL Documentation**: `/Users/tsanders/Workspace2/ocpctl/docs/`
- **API Reference**: https://ocpctl.mg.dog8code.com/swagger/index.html
- **OCPCTL Architecture**: `/Users/tsanders/Workspace2/ocpctl/CLAUDE.md`
- **Jenkins Pipeline Syntax**: https://www.jenkins.io/doc/book/pipeline/syntax/

## Support

For issues or questions:
1. Check ocpctl logs: `sudo journalctl -u ocpctl-api -f`
2. Review worker logs: `sudo journalctl -u ocpctl-worker -f`
3. Contact ocpctl administrator
4. File issue: https://github.com/tsanders-rh/ocpctl/issues

## License

This example is part of the ocpctl project and follows the same license.
