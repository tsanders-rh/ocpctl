# Cluster Pools User Guide

Get instant access to pre-provisioned OpenShift clusters for testing and development.

## What are Cluster Pools?

Cluster Pools are collections of pre-provisioned, ready-to-use clusters that you can lease instantly instead of waiting 30-60 minutes for provisioning. Perfect for CI/CD pipelines, rapid testing, and development.

**Benefits:**
- ⚡ **Instant access** - Get a cluster in seconds, not minutes
- 🔄 **Auto-release** - Clusters automatically return to pool when done
- 💰 **Cost-efficient** - Shared pool resources reduce overall costs
- 🎯 **Self-service** - Lease and release via Web UI or API

---

## Quick Start: Web UI

### Browse Available Pools

1. **Navigate to Pools**
   - Click **Cluster Pools** in the sidebar (second item)
   - Or visit: https://ocpctl.mg.dog8code.com/pools

2. **View Pool Cards**
   - See pool name, description, and profile
   - Check target pool size and max lease duration
   - View work hours schedule (if configured)

### Lease a Cluster

1. **Select a Pool**
   - Click **View Pool Details** on any pool card
   - Or visit: `/pools/{pool-name}`

2. **Check Real-Time Stats**
   - **Ready Clusters**: Available for immediate lease
   - **Leased Clusters**: Currently in use
   - **Provisioning Clusters**: Being created
   - **Total Clusters**: In the pool

3. **Lease Cluster**
   - Click the **Lease Cluster** button
   - Wait a few seconds for confirmation
   - You'll be automatically redirected to the cluster details page

4. **Access Your Cluster**
   - View cluster API and console URLs
   - Download kubeconfig from the cluster details page
   - Start using immediately!

### Release a Cluster

When you're done with a cluster:

1. **Navigate to cluster details** (`/clusters/{cluster-id}`)
2. Click **Release Cluster** button
3. Cluster returns to pool and becomes available for others

**Auto-Release**: If you forget to release, the cluster will automatically return to the pool after the lease period expires.

---

## API Usage

### Authentication

All API requests require authentication. Use a JWT token:

```bash
# Get token via login
curl -X POST https://ocpctl.mg.dog8code.com/api/v1/auth/login \
  -H "Content-Type: application/json" \
  -d '{"username": "your-username", "password": "your-password"}'

# Save token
export TOKEN="your-jwt-token"
```

### List Available Pools

```bash
curl -H "Authorization: Bearer $TOKEN" \
  https://ocpctl.mg.dog8code.com/api/v1/pools?enabled_only=true
```

**Response:**
```json
{
  "pools": [
    {
      "name": "dev-pool",
      "display_name": "Development Pool",
      "description": "Fast SNO clusters for development",
      "profile": "aws-sno-ga",
      "target_size": 3,
      "max_lease_duration_hours": 2
    }
  ]
}
```

### Get Pool Statistics

```bash
curl -H "Authorization: Bearer $TOKEN" \
  https://ocpctl.mg.dog8code.com/api/v1/pools/dev-pool/stats
```

**Response:**
```json
{
  "pool_name": "dev-pool",
  "total_clusters": 5,
  "ready_clusters": 3,
  "leased_clusters": 1,
  "provisioning_clusters": 1
}
```

### Lease a Cluster

```bash
curl -X POST \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "leased_by": "my-test-job",
    "metadata": {
      "purpose": "integration testing",
      "ticket": "JIRA-123"
    }
  }' \
  https://ocpctl.mg.dog8code.com/api/v1/pools/dev-pool/lease
```

**Response:**
```json
{
  "cluster_id": "abc-123-def",
  "cluster_name": "dev-pool-xyz789",
  "leased_by": "my-test-job",
  "leased_at": "2026-05-22T10:00:00Z",
  "lease_expires_at": "2026-05-22T12:00:00Z",
  "api_url": "https://api.cluster.example.com:6443",
  "console_url": "https://console-openshift-console.apps.cluster.example.com",
  "kubeconfig_path": "s3://ocpctl-artifacts/clusters/abc-123-def/kubeconfig"
}
```

### Download Kubeconfig

```bash
# Extract kubeconfig path from lease response
KUBECONFIG_PATH="s3://ocpctl-artifacts/clusters/abc-123-def/kubeconfig"

# Download using AWS CLI
aws s3 cp $KUBECONFIG_PATH ./kubeconfig

# Use the cluster
export KUBECONFIG=./kubeconfig
kubectl get nodes
oc get clusterversion
```

### Release a Cluster

```bash
curl -X POST \
  -H "Authorization: Bearer $TOKEN" \
  https://ocpctl.mg.dog8code.com/api/v1/pools/clusters/abc-123-def/release
```

**Response:** 204 No Content (success)

---

## CI/CD Integration

### GitHub Actions Example

Create `.github/workflows/test.yml`:

```yaml
name: Integration Tests
on: [push, pull_request]

jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v3

      - name: Lease Cluster from Pool
        id: lease
        run: |
          RESPONSE=$(curl -X POST \
            -H "Authorization: Bearer ${{ secrets.OCPCTL_TOKEN }}" \
            -H "Content-Type: application/json" \
            -d '{
              "leased_by": "github-run-${{ github.run_id }}",
              "metadata": {
                "repo": "${{ github.repository }}",
                "workflow": "${{ github.workflow }}",
                "run_id": "${{ github.run_id }}"
              }
            }' \
            https://ocpctl.mg.dog8code.com/api/v1/pools/ci-pool/lease)

          echo "Response: $RESPONSE"

          CLUSTER_ID=$(echo $RESPONSE | jq -r '.cluster_id')
          API_URL=$(echo $RESPONSE | jq -r '.api_url')
          KUBECONFIG_PATH=$(echo $RESPONSE | jq -r '.kubeconfig_path')

          echo "cluster_id=$CLUSTER_ID" >> $GITHUB_OUTPUT
          echo "api_url=$API_URL" >> $GITHUB_OUTPUT
          echo "kubeconfig_path=$KUBECONFIG_PATH" >> $GITHUB_OUTPUT

      - name: Configure AWS Credentials
        uses: aws-actions/configure-aws-credentials@v2
        with:
          aws-access-key-id: ${{ secrets.AWS_ACCESS_KEY_ID }}
          aws-secret-access-key: ${{ secrets.AWS_SECRET_ACCESS_KEY }}
          aws-region: us-east-1

      - name: Download Kubeconfig
        run: |
          aws s3 cp ${{ steps.lease.outputs.kubeconfig_path }} ./kubeconfig
          chmod 600 ./kubeconfig

      - name: Run Tests
        env:
          KUBECONFIG: ./kubeconfig
        run: |
          kubectl get nodes
          kubectl get pods -A
          # Your integration tests here
          ./run-tests.sh

      - name: Release Cluster
        if: always()
        run: |
          curl -X POST \
            -H "Authorization: Bearer ${{ secrets.OCPCTL_TOKEN }}" \
            https://ocpctl.mg.dog8code.com/api/v1/pools/clusters/${{ steps.lease.outputs.cluster_id }}/release
```

**Setup:**
1. Add `OCPCTL_TOKEN` to GitHub repository secrets
2. Add AWS credentials if downloading kubeconfigs from S3
3. Update `ci-pool` to your pool name

### Bash Script Example

```bash
#!/bin/bash
set -e

# Configuration
POOL_NAME="dev-pool"
OCPCTL_URL="https://ocpctl.mg.dog8code.com"
TOKEN="${OCPCTL_TOKEN}"  # Set via environment

# Function to lease cluster
lease_cluster() {
    local response=$(curl -s -X POST \
        -H "Authorization: Bearer $TOKEN" \
        -H "Content-Type: application/json" \
        -d "{
            \"leased_by\": \"script-$(date +%s)\",
            \"metadata\": {
                \"script\": \"$0\",
                \"user\": \"$(whoami)\",
                \"hostname\": \"$(hostname)\"
            }
        }" \
        "$OCPCTL_URL/api/v1/pools/$POOL_NAME/lease")

    echo "$response" | jq -r '.cluster_id'
}

# Function to release cluster
release_cluster() {
    local cluster_id=$1
    curl -s -X POST \
        -H "Authorization: Bearer $TOKEN" \
        "$OCPCTL_URL/api/v1/pools/clusters/$cluster_id/release"
}

# Main script
echo "Leasing cluster from pool: $POOL_NAME"
CLUSTER_ID=$(lease_cluster)

if [ -z "$CLUSTER_ID" ] || [ "$CLUSTER_ID" = "null" ]; then
    echo "Error: Failed to lease cluster"
    exit 1
fi

echo "Leased cluster: $CLUSTER_ID"

# Ensure cleanup on exit
trap "echo 'Releasing cluster...'; release_cluster $CLUSTER_ID" EXIT

# Your work here
echo "Running tests on cluster..."
# ... your test commands ...

echo "Tests complete!"
```

### Python Example

```python
#!/usr/bin/env python3
import requests
import os
import time
from datetime import datetime

class OcpctlPool:
    def __init__(self, base_url, token):
        self.base_url = base_url
        self.headers = {
            "Authorization": f"Bearer {token}",
            "Content-Type": "application/json"
        }

    def lease_cluster(self, pool_name, leased_by, metadata=None):
        """Lease a cluster from the specified pool"""
        url = f"{self.base_url}/api/v1/pools/{pool_name}/lease"
        payload = {
            "leased_by": leased_by,
            "metadata": metadata or {}
        }

        response = requests.post(url, json=payload, headers=self.headers)
        response.raise_for_status()
        return response.json()

    def release_cluster(self, cluster_id):
        """Release a leased cluster back to the pool"""
        url = f"{self.base_url}/api/v1/pools/clusters/{cluster_id}/release"
        response = requests.post(url, headers=self.headers)
        response.raise_for_status()

    def get_pool_stats(self, pool_name):
        """Get real-time pool statistics"""
        url = f"{self.base_url}/api/v1/pools/{pool_name}/stats"
        response = requests.get(url, headers=self.headers)
        response.raise_for_status()
        return response.json()

# Example usage
if __name__ == "__main__":
    # Initialize client
    client = OcpctlPool(
        base_url="https://ocpctl.mg.dog8code.com",
        token=os.environ.get("OCPCTL_TOKEN")
    )

    # Check pool stats
    stats = client.get_pool_stats("dev-pool")
    print(f"Ready clusters: {stats['ready_clusters']}")

    # Lease cluster
    lease = client.lease_cluster(
        pool_name="dev-pool",
        leased_by=f"python-script-{int(time.time())}",
        metadata={
            "purpose": "integration testing",
            "timestamp": datetime.utcnow().isoformat()
        }
    )

    cluster_id = lease["cluster_id"]
    print(f"Leased cluster: {lease['cluster_name']}")
    print(f"API URL: {lease['api_url']}")
    print(f"Console: {lease['console_url']}")
    print(f"Expires: {lease['lease_expires_at']}")

    try:
        # Your test logic here
        print("Running tests...")
        time.sleep(5)  # Simulate work

    finally:
        # Always release
        print(f"Releasing cluster {cluster_id}...")
        client.release_cluster(cluster_id)
        print("Released!")
```

---

## Best Practices

### 1. Always Release Clusters

Even though auto-release exists, manually release when done:
- Frees resources faster for others
- Reduces costs
- Good citizenship

```bash
# In CI/CD, use trap or finally blocks
trap "release_cluster $CLUSTER_ID" EXIT  # Bash
finally: release_cluster(cluster_id)     # Python
```

### 2. Add Meaningful Metadata

Help with debugging and tracking:

```json
{
  "leased_by": "github-actions-123",
  "metadata": {
    "repo": "my-org/my-repo",
    "workflow": "integration-tests",
    "run_id": "12345",
    "branch": "feature-xyz",
    "commit_sha": "abc123"
  }
}
```

### 3. Check Pool Stats First

Before leasing, check if pools have capacity:

```bash
curl https://ocpctl.mg.dog8code.com/api/v1/pools/ci-pool/stats
# If ready_clusters = 0, wait or use different pool
```

### 4. Handle Lease Failures

Pools may be empty or temporarily unavailable:

```python
try:
    lease = client.lease_cluster("ci-pool", "my-job")
except requests.HTTPError as e:
    if e.response.status_code == 503:
        print("Pool empty, retrying in 60 seconds...")
        time.sleep(60)
        lease = client.lease_cluster("ci-pool", "my-job")
    else:
        raise
```

### 5. Use Appropriate Pools

Match your workload to the pool profile:
- **SNO pools**: Fast testing, single node
- **Standard pools**: Multi-node testing
- **Virtualization pools**: CNV workloads

### 6. Monitor Lease Expiration

Track your lease and release before expiration:

```bash
LEASE_EXPIRES=$(echo $LEASE_RESPONSE | jq -r '.lease_expires_at')
echo "Cluster will auto-release at: $LEASE_EXPIRES"
```

---

## Troubleshooting

### Pool Shows No Available Clusters

**Symptom:** `ready_clusters: 0` in stats

**Solutions:**
1. **Wait**: Pool may be replenishing (check `provisioning_clusters`)
2. **Check work hours**: Pool may be scaled down outside hours
3. **Contact admin**: Pool may need target_size increase

### Lease Request Fails with 503

**Error:** `{"error": "No clusters available in pool"}`

**Solutions:**
1. Retry after delay (pool replenishment in progress)
2. Use alternative pool
3. Request admin to increase pool size

### Cluster Not Releasing

**Symptom:** Cluster stuck in LEASED state

**Check:**
```bash
# View cluster details
curl https://ocpctl.mg.dog8code.com/api/v1/clusters/{cluster-id}

# Check lease expiration
# If expired, background job will auto-release within 60 seconds
```

**Manual fix (admin only):**
Contact administrator to manually release or check pool manager logs.

### Cannot Access Leased Cluster

**Symptom:** `kubectl` commands fail

**Solutions:**
1. **Verify kubeconfig download**:
   ```bash
   aws s3 ls s3://ocpctl-artifacts/clusters/{cluster-id}/kubeconfig
   ```

2. **Check API URL**:
   ```bash
   curl -k https://api.cluster.example.com:6443/healthz
   ```

3. **Verify cluster is READY**:
   ```bash
   curl https://ocpctl.mg.dog8code.com/api/v1/clusters/{cluster-id}
   # status should be "READY", not "CREATING"
   ```

---

## FAQ

### Q: How long can I lease a cluster?

**A:** Each pool has a `max_lease_duration_hours` setting (typically 2-4 hours). Your lease will auto-expire after this period. You can release manually at any time.

### Q: Can I extend a lease?

**A:** Not currently supported. Release and re-lease if you need more time. (Feature planned for future release)

### Q: What happens to my workloads when cluster is released?

**A:** All workloads are destroyed. Clusters are returned to a clean state. Always extract any important data before releasing.

### Q: Can I lease multiple clusters?

**A:** Yes! There's no limit on concurrent leases. Lease as many as you need from available pools.

### Q: How do I know which pool to use?

**A:** Check the pool's `profile` field. This tells you the cluster configuration:
- `aws-sno-ga`: Single node OpenShift (fast, cheap)
- `aws-standard`: 3 control + 3 workers (production-like)
- `aws-virtualization`: Metal workers for CNV

### Q: Are pools available 24/7?

**A:** Depends on pool configuration. Some pools use `scheduled_mode` and only run during work hours (e.g., 8am-6pm EST). Check pool details in the UI.

### Q: How much does using a pool cost?

**A:** Pools cost the same as regular clusters but are **shared**. A pool with 3 ready clusters costs ~$1.14/hr (for SNO) but serves unlimited users. Much cheaper than creating individual clusters.

---

## Admin Tasks

> **Note:** Admin-only features require the Admin role

### Create a New Pool

1. Navigate to **Admin** → **Cluster Pools**
2. Click **Create Pool**
3. Configure:
   - **Name**: Unique identifier (e.g., `dev-pool`)
   - **Display Name**: User-facing name
   - **Profile**: Cluster profile to use
   - **Target Size**: Desired number of READY clusters
   - **Max Lease Duration**: Hours before auto-release
   - **Work Hours**: Optional scheduling

4. Click **Create**

### Monitor Pool Health

1. Navigate to **Admin** → **Cluster Pools**
2. Click on pool name
3. View metrics:
   - Ready vs Target ratio
   - Lease utilization
   - Provisioning failures
   - Cost tracking

### Adjust Pool Size

**Via UI:**
1. Navigate to pool details
2. Click **Edit**
3. Update `target_size`
4. Save

**Via API:**
```bash
curl -X PATCH \
  -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"target_size": 5}' \
  https://ocpctl.mg.dog8code.com/api/v1/admin/pools/dev-pool
```

---

## Next Steps

- **Feature Documentation**: [docs/features/CLUSTER_POOLS.md](../features/CLUSTER_POOLS.md)
- **API Reference**: https://ocpctl.mg.dog8code.com/swagger/index.html
- **Cluster Management**: [cluster-management.md](./cluster-management.md)
- **Architecture**: [docs/architecture/architecture.md](../architecture/architecture.md)
