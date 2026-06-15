# Dev/Test Environment Plan for ocpctl

## Overview

This document outlines the strategy for creating a dev/test environment for ocpctl, enabling safe testing of changes before production deployment during maintenance windows.

## Goals

1. **Isolated Testing**: Test code changes, database migrations, and infrastructure updates without affecting production
2. **Feature Validation**: Validate new features end-to-end before production release
3. **Cost Efficiency**: Minimize additional infrastructure costs while maintaining functional parity
4. **Easy Promotion**: Simple path to promote tested changes to production
5. **Maintenance Windows**: Deploy to production during scheduled maintenance windows

## Architecture Comparison

### Production Environment (Current)
- **Server**: t3.large (2 vCPU, 8GB RAM) @ 44.201.165.78
- **Database**: RDS PostgreSQL (db.t3.medium)
- **S3 Buckets**:
  - `ocpctl-binaries` (worker binaries, profiles, addons)
  - `ocpctl-artifacts` (cluster kubeconfigs, installer dirs)
- **Domain**: https://ocpctl.mg.dog8code.com
- **Services**: API (port 8080), Worker (port 8081)
- **Autoscale Workers**: ASG with t3.xlarge instances

### Dev/Test Environment (Proposed)
- **Server**: t3.medium (2 vCPU, 4GB RAM) - new EC2 instance
- **Database**: Separate RDS PostgreSQL (db.t3.micro) OR shared RDS with separate database
- **S3 Buckets**:
  - Option 1: Separate buckets (`ocpctl-dev-binaries`, `ocpctl-dev-artifacts`)
  - Option 2: Same buckets with `dev/` prefix
- **Domain**: https://dev.ocpctl.mg.dog8code.com
- **Services**: API (port 8080), Worker (port 8081)
- **Autoscale Workers**: None (single worker node to reduce costs)

### Cost Analysis

| Component | Production | Dev/Test | Monthly Cost |
|-----------|-----------|----------|--------------|
| API Server | t3.large | t3.medium | ~$30 |
| Database | db.t3.medium | db.t3.micro | ~$15 |
| Worker (autoscale) | t3.xlarge ASG | None (use dev server) | $0 |
| S3 Storage | ~$50/month | ~$10/month | ~$10 |
| Data Transfer | ~$20/month | ~$5/month | ~$5 |
| **Total** | **~$320/month** | **~$60/month** | **~$60/month** |

## Implementation Plan

### Phase 1: Infrastructure Setup

#### 1.1 Create Dev Server (EC2)
```bash
# Use Terraform or AWS CLI
aws ec2 run-instances \
  --image-id ami-0c55b159cbfafe1f0 \  # Ubuntu 22.04 LTS
  --instance-type t3.medium \
  --key-name ocpctl-dev-key \
  --security-group-ids sg-xxx \
  --subnet-id subnet-xxx \
  --tag-specifications 'ResourceType=instance,Tags=[{Key=Name,Value=ocpctl-dev},{Key=Environment,Value=dev}]'
```

#### 1.2 Setup Dev Database
**Option A: Separate RDS (recommended for production-like testing)**
```bash
aws rds create-db-instance \
  --db-instance-identifier ocpctl-dev-db \
  --db-instance-class db.t3.micro \
  --engine postgres \
  --engine-version 15.4 \
  --master-username ocpctl_dev \
  --master-user-password [generated] \
  --allocated-storage 20 \
  --vpc-security-group-ids sg-xxx \
  --db-subnet-group-name ocpctl-dev-subnet-group \
  --backup-retention-period 1 \
  --no-multi-az \
  --tags Key=Environment,Value=dev
```

**Option B: Shared RDS with separate database (cost-effective)**
```sql
-- On existing RDS
CREATE DATABASE ocpctl_dev;
CREATE USER ocpctl_dev_user WITH PASSWORD 'generated_password';
GRANT ALL PRIVILEGES ON DATABASE ocpctl_dev TO ocpctl_dev_user;
```

#### 1.3 Setup S3 Buckets/Prefixes
**Option A: Separate buckets (recommended for clean isolation)**
```bash
aws s3 mb s3://ocpctl-dev-binaries
aws s3 mb s3://ocpctl-dev-artifacts
```

**Option B: Shared buckets with prefixes (cost-effective)**
```bash
# Use existing buckets, configure dev env to use:
# s3://ocpctl-binaries/dev/
# s3://ocpctl-artifacts/dev/
```

#### 1.4 DNS Configuration
```bash
# Add DNS record for dev environment
# dev.ocpctl.mg.dog8code.com -> [dev-server-ip]

# Update nginx on dev server for SSL
# Use Let's Encrypt for dev.ocpctl.mg.dog8code.com
```

### Phase 2: Configuration Management

#### 2.1 Environment-Specific Config Files
```
config/
├── api.env.production       # Production API config
├── api.env.dev             # Dev API config
├── worker.env.production   # Production worker config
└── worker.env.dev          # Dev worker config
```

**Example: `config/api.env.dev`**
```bash
# Database
DATABASE_URL=postgresql://ocpctl_dev_user:xxx@ocpctl-dev-db.xxx.rds.amazonaws.com:5432/ocpctl_dev?sslmode=require

# Server
PORT=8080
PROFILES_DIR=/opt/ocpctl/profiles
ADDONS_DIR=/opt/ocpctl/addons

# JWT
JWT_SECRET=[different-secret-for-dev]
JWT_EXPIRY=24h

# Auth
AUTH_ENABLED=true
JWT_AUTH_ENABLED=true
IAM_AUTH_ENABLED=false

# CORS
CORS_ORIGINS=http://localhost:3000,https://dev.ocpctl.mg.dog8code.com

# Environment marker
ENVIRONMENT=dev
```

**Example: `config/worker.env.dev`**
```bash
# Database
DATABASE_URL=postgresql://ocpctl_dev_user:xxx@ocpctl-dev-db.xxx.rds.amazonaws.com:5432/ocpctl_dev?sslmode=require

# Worker
TMPDIR=/var/lib/ocpctl/tmp
PROFILES_DIR=/opt/ocpctl/profiles
WORKER_WORK_DIR=/var/lib/ocpctl/clusters
WORKER_HEALTH_PORT=8081
S3_BUCKET_NAME=ocpctl-dev-binaries  # or ocpctl-binaries with dev/ prefix

# Same cloud credentials as production
OPENSHIFT_PULL_SECRET='...'
AWS_ACCESS_KEY_ID=xxx
AWS_SECRET_ACCESS_KEY=xxx
IC_API_KEY=xxx
GOOGLE_APPLICATION_CREDENTIALS=/opt/ocpctl/gcp-credentials.json
OCM_TOKEN=xxx

# Environment marker
ENVIRONMENT=dev
AWS_REGION=us-east-1
```

#### 2.2 Enhanced Deploy Script
Create `scripts/deploy-env.sh` to support multiple environments:

```bash
#!/bin/bash
# Multi-environment deployment script
# Usage: ./deploy-env.sh [dev|staging|production] [version]

set -e

ENVIRONMENT=${1:-production}
VERSION=${2:-}

# Configuration per environment
case $ENVIRONMENT in
  dev)
    API_HOST="[dev-server-ip]"
    WORKER_HOSTS=("[dev-server-ip]")
    SSH_KEY="$HOME/.ssh/ocpctl-dev-key"
    S3_BUCKET="s3://ocpctl-dev-binaries"
    DOMAIN="dev.ocpctl.mg.dog8code.com"
    ;;
  production)
    API_HOST="44.201.165.78"
    WORKER_HOSTS=("44.201.165.78")
    SSH_KEY="$HOME/.ssh/ocpctl-production-key"
    S3_BUCKET="s3://ocpctl-binaries"
    DOMAIN="ocpctl.mg.dog8code.com"
    ;;
  *)
    echo "Unknown environment: $ENVIRONMENT"
    echo "Usage: $0 [dev|production] [version]"
    exit 1
    ;;
esac

# Rest of deployment logic...
# - Build binaries with environment tag
# - Upload to environment-specific S3
# - Deploy to environment-specific servers
# - Use environment-specific config files
```

### Phase 3: Database Migration Strategy

#### 3.1 Migration Testing Workflow
```bash
# 1. Test migration on dev database first
ssh ocpctl-dev "cd /opt/ocpctl/current && ./ocpctl-api migrate up"

# 2. Verify migration success and data integrity
ssh ocpctl-dev "psql \$DATABASE_URL -c '\dt'"

# 3. Test application with new schema
curl https://dev.ocpctl.mg.dog8code.com/health

# 4. During maintenance window, apply to production
ssh ocpctl-production "cd /opt/ocpctl/current && ./ocpctl-api migrate up"
```

#### 3.2 Data Seeding for Dev
Create script to populate dev database with test data:

```bash
#!/bin/bash
# scripts/seed-dev-data.sh
# Populate dev database with test clusters, users, pools

psql $DATABASE_URL <<EOF
-- Create test users
INSERT INTO users (id, email, name, role, password_hash) VALUES
  ('test-admin', 'admin@dev.local', 'Dev Admin', 'admin', '$2a$...'),
  ('test-user', 'user@dev.local', 'Dev User', 'user', '$2a$...');

-- Create test pool
INSERT INTO cluster_pools (id, name, display_name, profile, target_size, ...) VALUES
  ('test-pool-1', 'dev-test-pool', 'Dev Test Pool', 'aws-sno-ga', 2, ...);

-- Add test clusters to pool
-- (auto-generated via pool controller)
EOF
```

### Phase 4: CI/CD Integration

#### 4.1 GitHub Actions Workflow
```yaml
# .github/workflows/deploy.yml
name: Deploy

on:
  push:
    branches:
      - main        # Auto-deploy to dev
      - release/*   # Manual deploy to production
  workflow_dispatch:
    inputs:
      environment:
        description: 'Environment to deploy'
        required: true
        type: choice
        options:
          - dev
          - production

jobs:
  deploy-dev:
    if: github.ref == 'refs/heads/main'
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v3
      - name: Deploy to Dev
        run: ./scripts/deploy-env.sh dev

  deploy-production:
    if: startsWith(github.ref, 'refs/heads/release/')
    runs-on: ubuntu-latest
    environment: production  # Requires manual approval
    steps:
      - uses: actions/checkout@v3
      - name: Deploy to Production
        run: ./scripts/deploy-env.sh production
```

#### 4.2 Pre-deployment Checks
Add validation script to run before deployment:

```bash
#!/bin/bash
# scripts/pre-deploy-checks.sh

echo "Running pre-deployment checks..."

# 1. Build and test locally
go test ./...
if [ $? -ne 0 ]; then
  echo "Tests failed!"
  exit 1
fi

# 2. Verify migrations can run
go run cmd/api/main.go migrate validate

# 3. Check environment config files exist
if [ ! -f "config/api.env.$ENVIRONMENT" ]; then
  echo "Missing api.env.$ENVIRONMENT"
  exit 1
fi

# 4. Verify S3 buckets exist
aws s3 ls s3://ocpctl-$ENVIRONMENT-binaries/ || exit 1

echo "Pre-deployment checks passed!"
```

### Phase 5: Development Workflow

#### 5.1 Standard Development Flow
```bash
# 1. Make changes locally
git checkout -b feature/my-new-feature

# 2. Test locally
make run-api
make test

# 3. Commit and push
git commit -am "Add new feature"
git push origin feature/my-new-feature

# 4. Create PR, get review

# 5. Merge to main -> auto-deploys to dev
git checkout main
git merge feature/my-new-feature
git push  # Triggers GitHub Action -> deploys to dev

# 6. Test on dev environment
curl https://dev.ocpctl.mg.dog8code.com/api/v1/clusters

# 7. During maintenance window, deploy to production
git checkout -b release/v1.2.3
git push origin release/v1.2.3
# Manually approve GitHub Action to deploy to production
# OR: ./scripts/deploy-env.sh production
```

#### 5.2 Hotfix Workflow
```bash
# 1. Create hotfix branch from main
git checkout -b hotfix/critical-bug

# 2. Fix bug, test locally
make test

# 3. Deploy to dev for validation
./scripts/deploy-env.sh dev

# 4. Test on dev
curl https://dev.ocpctl.mg.dog8code.com/health

# 5. Fast-track to production during emergency
./scripts/deploy-env.sh production

# 6. Create PR for review (post-deployment)
```

### Phase 6: Monitoring & Observability

#### 6.1 Environment Tagging
Update metrics to include environment tag:

```go
// internal/metrics/collector.go
type Metrics struct {
    Environment string // "dev", "staging", "production"
    // ... existing fields
}

func (m *Metrics) RecordAPIRequest(path string) {
    apiRequestsTotal.WithLabelValues(
        m.Environment, // Add environment label
        path,
    ).Inc()
}
```

#### 6.2 Separate Dashboards
- CloudWatch: Filter by Environment tag
- Logs: Separate log groups (`/ocpctl/dev/api`, `/ocpctl/production/api`)

### Phase 7: Cost Optimization

#### 7.1 Dev Environment Scheduling
```bash
# Cron to stop dev server overnight (save ~60% on compute)
# /etc/cron.d/ocpctl-dev-schedule

# Stop at 6 PM ET weekdays
0 18 * * 1-5 root /usr/local/bin/aws ec2 stop-instances --instance-ids i-xxx --region us-east-1

# Start at 8 AM ET weekdays
0 8 * * 1-5 root /usr/local/bin/aws ec2 start-instances --instance-ids i-xxx --region us-east-1

# Stop all weekend
0 18 * * 5 root /usr/local/bin/aws ec2 stop-instances --instance-ids i-xxx --region us-east-1
0 8 * * 1 root /usr/local/bin/aws ec2 start-instances --instance-ids i-xxx --region us-east-1
```

#### 7.2 Database Cleanup
```sql
-- Cron to clean up old dev clusters (keep last 7 days)
DELETE FROM clusters WHERE environment = 'dev' AND created_at < NOW() - INTERVAL '7 days';
DELETE FROM jobs WHERE environment = 'dev' AND created_at < NOW() - INTERVAL '7 days';
```

## Rollout Plan

### Week 1: Infrastructure
- [ ] Provision dev EC2 instance (t3.medium)
- [ ] Create dev RDS instance (db.t3.micro) OR dev database on shared RDS
- [ ] Create S3 buckets or configure prefixes
- [ ] Setup DNS for dev.ocpctl.mg.dog8code.com
- [ ] Configure SSL certificates (Let's Encrypt)

### Week 2: Configuration & Deployment
- [ ] Create environment-specific config files
- [ ] Enhance deploy script to support multiple environments
- [ ] Test deployment to dev environment
- [ ] Verify all services running on dev

### Week 3: Database & Testing
- [ ] Run migrations on dev database
- [ ] Create data seeding scripts
- [ ] Test cluster creation on dev
- [ ] Validate feature parity with production

### Week 4: CI/CD & Documentation
- [ ] Setup GitHub Actions workflow
- [ ] Create runbooks for dev/production deployment
- [ ] Document maintenance window procedures
- [ ] Train team on new workflow

## Maintenance Window Procedures

### Standard Maintenance Window (Monthly)
```
Scheduled: First Sunday of each month, 2 AM - 6 AM ET

Hour 1 (2-3 AM):
- Deploy to dev, final validation
- Create database backup snapshot
- Announce maintenance window (if user-facing changes)

Hour 2 (3-4 AM):
- Deploy to production
- Run database migrations
- Verify services started

Hour 3 (4-5 AM):
- Smoke tests (create test cluster, API checks)
- Monitor logs for errors
- Performance validation

Hour 4 (5-6 AM):
- Buffer for rollback if needed
- Final checks
- Close maintenance window
```

### Emergency Hotfix (24/7)
```
1. Fix and test locally (< 30 min)
2. Deploy to dev, verify (< 15 min)
3. Get approval from on-call manager
4. Deploy to production (< 10 min)
5. Monitor for 30 min
6. Document incident, create PR
```

## Rollback Procedures

### Dev Environment
```bash
# Quick rollback using existing mechanism
ssh ocpctl-dev "sudo ls -d /opt/ocpctl/releases/*"
./scripts/deploy-env.sh dev v0.20260601.abc1234
```

### Production Environment
```bash
# During maintenance window
ssh ocpctl-production "sudo ls -d /opt/ocpctl/releases/*"
./scripts/deploy-env.sh production v0.20260601.abc1234

# Emergency rollback
ssh -i ~/.ssh/ocpctl-production-key ubuntu@44.201.165.78 \
  "sudo systemctl stop ocpctl-api ocpctl-worker && \
   sudo ln -snf /opt/ocpctl/releases/v0.20260601.abc1234 /opt/ocpctl/current && \
   sudo systemctl start ocpctl-api ocpctl-worker"
```

## Security Considerations

1. **Separate Credentials**: Dev uses different JWT secrets, API keys where possible
2. **Network Isolation**: Dev in separate security group, restricted access
3. **Data Protection**: No production data copied to dev (use synthetic test data)
4. **Access Control**: Dev server accessible only via VPN or IP whitelist
5. **Secrets Management**: Use AWS Secrets Manager or similar for credentials

## Monitoring Checklist

### Dev Environment
- [ ] CloudWatch alarms for high error rates
- [ ] Disk space monitoring
- [ ] Database connection pool monitoring
- [ ] S3 bucket size monitoring

### Production Environment (existing)
- [ ] All existing alarms remain active
- [ ] Add alarm for failed deployments
- [ ] Monitor rollback frequency

## Success Metrics

- **Deployment Confidence**: 0 production incidents due to untested changes (first 3 months)
- **Deployment Frequency**: Increase from monthly to bi-weekly
- **Lead Time**: Reduce change lead time from 30 days to 7 days
- **MTTR**: Reduce mean time to recovery from 2 hours to 30 minutes
- **Cost Impact**: Dev environment < 20% of production monthly cost

## Next Steps

1. **Review & Approve**: Get stakeholder approval for plan and budget
2. **Provision Infrastructure**: Follow Week 1 rollout plan
3. **Implement Deploy Script**: Create `deploy-env.sh` with environment support
4. **Test Deployment**: Deploy current version to dev
5. **Document Workflow**: Update team documentation with new procedures
6. **Schedule First Maintenance Window**: Plan production deployment for first Sunday of next month
