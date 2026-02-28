# Testing ocpctl Without OpenShift Provisioning

This guide explains how to develop and test ocpctl without actually provisioning OpenShift clusters.

## Overview

You can test most of ocpctl's functionality without the `openshift-install` binary or cloud credentials:

- ✅ Full API functionality
- ✅ Complete web UI
- ✅ Database operations
- ✅ Authentication flows
- ✅ Profile validation
- ✅ Job creation
- ❌ Actual cluster provisioning
- ❌ Cluster outputs (kubeconfig, etc.)

## What Works Without openshift-install

### 1. API Server

All API endpoints work:

```bash
# Start API
make run-api

# Login
curl -X POST http://localhost:8080/api/v1/auth/login \
  -H "Content-Type: application/json" \
  -d '{"email":"admin@localhost","password":"changeme"}'

# Create cluster (creates DB record + job)
curl -X POST http://localhost:8080/api/v1/clusters \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "test-cluster",
    "platform": "aws",
    "profile": "aws-minimal-test",
    "region": "us-east-1",
    "version": "4.16.0",
    "owner": "admin@localhost",
    "team": "engineering",
    "cost_center": "eng-001",
    "ttl_hours": 8
  }'

# View clusters
curl http://localhost:8080/api/v1/clusters \
  -H "Authorization: Bearer $TOKEN"

# View jobs
curl http://localhost:8080/api/v1/jobs \
  -H "Authorization: Bearer $TOKEN"
```

### 2. Web UI

The complete frontend works:

```bash
# Start frontend
cd web && npm run dev

# Open browser: http://localhost:3000
# Login: admin@localhost / changeme
```

**Available features:**
- ✅ Login/logout
- ✅ View cluster list
- ✅ Create cluster form (validates and creates DB record)
- ✅ View cluster details
- ✅ Browse profiles
- ✅ View jobs
- ✅ Delete clusters (marks for deletion)
- ✅ Extend cluster TTL

**What you'll see:**
- Clusters remain in `PENDING` status (waiting for worker)
- Jobs remain in `PENDING` status
- No outputs/kubeconfig available

### 3. Database Operations

Full database functionality:

```bash
# Run migrations
make migrate-up

# Query clusters
psql $DATABASE_URL -c "SELECT id, name, status, platform FROM clusters;"

# Query jobs
psql $DATABASE_URL -c "SELECT id, job_type, status, cluster_id FROM jobs;"

# View users
psql $DATABASE_URL -c "SELECT email, role, active FROM users;"
```

## Testing Scenarios

### Scenario 1: UI/UX Testing

Test the complete user experience:

1. **Login flow**:
   - Navigate to http://localhost:3000
   - Login with admin@localhost / changeme
   - Verify redirect to clusters page

2. **Browse profiles**:
   - Click "Profiles" in sidebar
   - View different profiles (AWS minimal, IBM Cloud, etc.)
   - Filter by platform

3. **Create cluster request**:
   - Click "Create Cluster"
   - Fill out form with test data
   - View real-time execution panel updates
   - Submit form
   - Verify redirect to cluster detail page

4. **View clusters**:
   - See cluster in list with PENDING status
   - Click into cluster details
   - View metadata, tags, TTL

5. **Cluster actions**:
   - Try to extend TTL (works)
   - Try to delete cluster (works, creates destroy job)

### Scenario 2: API Integration Testing

Test API clients and integrations:

```bash
# Get auth token
TOKEN=$(curl -s -X POST http://localhost:8080/api/v1/auth/login \
  -H "Content-Type: application/json" \
  -d '{"email":"admin@localhost","password":"changeme"}' \
  | jq -r '.access_token')

# Test pagination
curl "http://localhost:8080/api/v1/clusters?page=1&per_page=10" \
  -H "Authorization: Bearer $TOKEN"

# Test filters
curl "http://localhost:8080/api/v1/clusters?platform=aws&status=PENDING" \
  -H "Authorization: Bearer $TOKEN"

# Test profile listing
curl "http://localhost:8080/api/v1/profiles?platform=aws" \
  -H "Authorization: Bearer $TOKEN"
```

### Scenario 3: Database Schema Testing

Test database operations:

```bash
# Create test cluster
psql $DATABASE_URL << EOF
INSERT INTO clusters (
  id, name, platform, version, profile, region, base_domain,
  status, owner, owner_id, team, cost_center, ttl_hours,
  created_at, updated_at
) VALUES (
  'test-cluster-1',
  'manual-test',
  'aws',
  '4.16.0',
  'aws-minimal-test',
  'us-east-1',
  'example.com',
  'READY',
  'test@example.com',
  (SELECT id FROM users WHERE email = 'admin@localhost'),
  'test-team',
  'test-001',
  8,
  NOW(),
  NOW()
);
EOF

# Verify in UI
# Should appear in cluster list as READY
```

### Scenario 4: Policy Engine Testing

Test policy validation:

```bash
# Test invalid profile
curl -X POST http://localhost:8080/api/v1/clusters \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "test",
    "platform": "aws",
    "profile": "nonexistent-profile",
    "region": "us-east-1",
    "version": "4.16.0",
    "owner": "admin@localhost",
    "team": "test",
    "cost_center": "test-001"
  }'
# Should return 400 error

# Test TTL limits
curl -X POST http://localhost:8080/api/v1/clusters \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "test",
    "platform": "aws",
    "profile": "aws-minimal-test",
    "region": "us-east-1",
    "version": "4.16.0",
    "owner": "admin@localhost",
    "team": "test",
    "cost_center": "test-001",
    "ttl_hours": 99999
  }'
# Should return validation error (exceeds max TTL)
```

## Mock Data Setup

### Create Mock Clusters

Create clusters in different states for UI testing:

```sql
-- READY cluster
INSERT INTO clusters (id, name, platform, version, profile, region, base_domain, status, owner, owner_id, team, cost_center, ttl_hours, created_at, updated_at)
VALUES (
  'ready-cluster-1', 'my-ready-cluster', 'aws', '4.16.0', 'aws-minimal-test', 'us-east-1', 'example.com',
  'READY', 'admin@localhost', (SELECT id FROM users WHERE email = 'admin@localhost'),
  'engineering', 'eng-001', 8, NOW() - INTERVAL '2 hours', NOW()
);

-- CREATING cluster
INSERT INTO clusters (id, name, platform, version, profile, region, base_domain, status, owner, owner_id, team, cost_center, ttl_hours, created_at, updated_at)
VALUES (
  'creating-cluster-1', 'my-creating-cluster', 'aws', '4.16.0', 'aws-standard', 'us-west-2', 'example.com',
  'CREATING', 'admin@localhost', (SELECT id FROM users WHERE email = 'admin@localhost'),
  'platform', 'plt-002', 24, NOW() - INTERVAL '30 minutes', NOW()
);

-- FAILED cluster
INSERT INTO clusters (id, name, platform, version, profile, region, base_domain, status, owner, owner_id, team, cost_center, ttl_hours, created_at, updated_at)
VALUES (
  'failed-cluster-1', 'my-failed-cluster', 'ibmcloud', '4.15.0', 'ibmcloud-minimal-test', 'us-south', 'example.com',
  'FAILED', 'admin@localhost', (SELECT id FROM users WHERE email = 'admin@localhost'),
  'qa', 'qa-003', 4, NOW() - INTERVAL '1 hour', NOW()
);
```

### Create Mock Jobs

```sql
-- PENDING job
INSERT INTO jobs (id, cluster_id, job_type, status, max_attempts, attempt, created_at, updated_at)
VALUES (
  'job-pending-1', 'ready-cluster-1', 'create', 'PENDING', 3, 0, NOW(), NOW()
);

-- RUNNING job
INSERT INTO jobs (id, cluster_id, job_type, status, max_attempts, attempt, started_at, created_at, updated_at)
VALUES (
  'job-running-1', 'creating-cluster-1', 'create', 'RUNNING', 3, 1, NOW() - INTERVAL '15 minutes', NOW() - INTERVAL '30 minutes', NOW()
);

-- FAILED job
INSERT INTO jobs (id, cluster_id, job_type, status, max_attempts, attempt, error_code, error_message, started_at, completed_at, created_at, updated_at)
VALUES (
  'job-failed-1', 'failed-cluster-1', 'create', 'FAILED', 3, 3,
  'PROVISION_FAILED', 'Timeout waiting for bootstrap complete',
  NOW() - INTERVAL '2 hours', NOW() - INTERVAL '1 hour', NOW() - INTERVAL '3 hours', NOW()
);
```

## Limitations

### What You Cannot Test

1. **Actual Cluster Provisioning**:
   - Worker will fail when processing jobs
   - No real OpenShift clusters created

2. **Cluster Outputs**:
   - GET /clusters/:id/outputs returns error (no kubeconfig)
   - Download kubeconfig endpoint fails

3. **Cloud Provider Integration**:
   - No AWS resource creation
   - No IBM Cloud interaction

4. **Janitor Service**:
   - Runs but cannot destroy clusters (no infrastructure exists)

### Workarounds

**Manual Status Updates**:

Simulate cluster becoming ready:

```sql
-- Update cluster to READY
UPDATE clusters
SET status = 'READY', updated_at = NOW()
WHERE id = 'your-cluster-id';

-- Mark job as succeeded
UPDATE jobs
SET status = 'SUCCEEDED',
    completed_at = NOW(),
    updated_at = NOW()
WHERE cluster_id = 'your-cluster-id'
  AND job_type = 'create';
```

**Mock Outputs**:

Create fake kubeconfig for testing:

```bash
# Create work directory
mkdir -p /tmp/ocpctl/your-cluster-id/auth

# Create mock kubeconfig
cat > /tmp/ocpctl/your-cluster-id/auth/kubeconfig << 'EOF'
apiVersion: v1
kind: Config
clusters:
- cluster:
    server: https://api.test-cluster.example.com:6443
  name: test-cluster
contexts:
- context:
    cluster: test-cluster
    user: admin
  name: admin
current-context: admin
users:
- name: admin
  user:
    token: mock-token-for-testing
EOF

# Create mock password
echo "mock-password-123" > /tmp/ocpctl/your-cluster-id/auth/kubeadmin-password

# Now GET /clusters/:id/outputs will work
```

## Development Workflow Without Provisioning

Recommended workflow:

1. **Start services**:
   ```bash
   # Terminal 1
   make run-api

   # Terminal 2
   cd web && npm run dev
   ```

2. **Develop features**:
   - Make code changes
   - Test via web UI or API
   - Clusters stay in PENDING (expected)

3. **Manual testing**:
   - Create clusters via UI
   - Verify database records
   - Test UI components and flows

4. **When ready for real provisioning**:
   - Follow [OPENSHIFT_INSTALL_SETUP.md](OPENSHIFT_INSTALL_SETUP.md)
   - Start worker service
   - Create test cluster
   - Monitor worker logs

## Continuous Integration

For CI/CD without cloud credentials:

```yaml
# .github/workflows/test.yml
name: Test
on: [push, pull_request]

jobs:
  test:
    runs-on: ubuntu-latest
    services:
      postgres:
        image: postgres:14
        env:
          POSTGRES_DB: ocpctl_test
          POSTGRES_PASSWORD: test
        ports:
          - 5432:5432

    steps:
      - uses: actions/checkout@v3

      - name: Setup Go
        uses: actions/setup-go@v4
        with:
          go-version: '1.21'

      - name: Run migrations
        run: |
          export DATABASE_URL=postgresql://postgres:test@localhost:5432/ocpctl_test
          make migrate-up

      - name: Run tests
        run: make test

      - name: Test API startup
        run: |
          export DATABASE_URL=postgresql://postgres:test@localhost:5432/ocpctl_test
          make run-api &
          sleep 5
          curl http://localhost:8080/health
```

## Next Steps

Once you're ready to test actual provisioning:

1. Read [OPENSHIFT_INSTALL_SETUP.md](OPENSHIFT_INSTALL_SETUP.md)
2. Install `openshift-install` binary
3. Configure pull secret and AWS credentials
4. Start worker service: `make run-worker`
5. Create a real test cluster

For questions or issues, see [DEVELOPMENT.md](../DEVELOPMENT.md) troubleshooting section.
