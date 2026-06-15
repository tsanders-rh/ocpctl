# ocpctl Deployment Guide

## Quick Reference

### Deploy to Dev
```bash
./scripts/deploy-env.sh dev
```

### Deploy to Production (with confirmation)
```bash
./scripts/deploy-env.sh production
```

### Deploy Specific Version
```bash
./scripts/deploy-env.sh dev v0.20260614.abc1234
./scripts/deploy-env.sh production v0.20260614.abc1234
```

### Rollback
```bash
# List available versions
ssh -i ~/.ssh/ocpctl-production-key ubuntu@44.201.165.78 'sudo ls -d /opt/ocpctl/releases/*'

# Deploy previous version
./scripts/deploy-env.sh production v0.20260601.xyz5678
```

---

## First-Time Dev Environment Setup

### 1. Provision Infrastructure

See [DEV_TEST_ENVIRONMENT_PLAN.md](./DEV_TEST_ENVIRONMENT_PLAN.md) for detailed infrastructure setup.

**Quick checklist:**
- [ ] Create dev EC2 instance (t3.medium)
- [ ] Create dev database (separate RDS or separate DB on shared RDS)
- [ ] Create S3 buckets (ocpctl-dev-binaries, ocpctl-dev-artifacts) OR configure prefixes
- [ ] Setup DNS record for dev.ocpctl.mg.dog8code.com
- [ ] Configure SSL certificate
- [ ] Create SSH key pair (ocpctl-dev-key)

### 2. Create Environment-Specific Config Files

```bash
# Create dev config files from templates
cp config/api.env.dev.template config/api.env.dev
cp config/worker.env.dev.template config/worker.env.dev

# Edit config files with actual values
vim config/api.env.dev
vim config/worker.env.dev
```

**Important fields to update:**
- `DATABASE_URL` - Point to dev database
- `JWT_SECRET` - Generate new secret for dev (different from production)
- `S3_BUCKET_NAME` - Use dev bucket or configure prefix
- Copy cloud credentials from production config

**Generate JWT secret:**
```bash
openssl rand -base64 32
```

### 3. Update deploy-env.sh with Dev Server IP

Edit `scripts/deploy-env.sh` and replace:
```bash
API_HOST="DEV_SERVER_IP"  # Line ~35
WORKER_HOSTS=("DEV_SERVER_IP")  # Line ~36
```

With actual dev server IP:
```bash
API_HOST="10.0.1.100"  # Example
WORKER_HOSTS=("10.0.1.100")
```

### 4. Initial Dev Deployment

```bash
# First deployment to dev
./scripts/deploy-env.sh dev
```

### 5. Run Database Migrations on Dev

```bash
# SSH to dev server
ssh -i ~/.ssh/ocpctl-dev-key ubuntu@DEV_SERVER_IP

# Run migrations
cd /opt/ocpctl/current
./ocpctl-api migrate up

# Verify migrations
psql $DATABASE_URL -c '\dt'
```

### 6. Create Dev Admin User

```bash
# On dev server or via API
psql $DATABASE_URL <<EOF
INSERT INTO users (id, email, name, role, password_hash, created_at, updated_at)
VALUES (
  gen_random_uuid(),
  'admin@dev.local',
  'Dev Admin',
  'admin',
  '$2a$10$...',  -- bcrypt hash of 'devpassword'
  NOW(),
  NOW()
);
EOF
```

### 7. Test Dev Environment

```bash
# Check services
curl https://dev.ocpctl.mg.dog8code.com/health
curl https://dev.ocpctl.mg.dog8code.com/version

# Test API
curl -X POST https://dev.ocpctl.mg.dog8code.com/api/v1/auth/login \
  -H "Content-Type: application/json" \
  -d '{"email": "admin@dev.local", "password": "devpassword"}'

# Create test cluster
# (Use UI or API to create a small SNO cluster)
```

---

## Standard Development Workflow

### Development Cycle

```
1. Create feature branch
   ├── git checkout -b feature/my-feature
   └── Make changes, commit locally

2. Test locally
   ├── make test
   ├── make run-api  (test API locally)
   └── go run cmd/api/main.go migrate up  (test migrations)

3. Deploy to dev
   ├── git push origin feature/my-feature
   ├── ./scripts/deploy-env.sh dev
   └── Test on https://dev.ocpctl.mg.dog8code.com

4. Create PR, get review
   └── Merge to main

5. Deploy to production (during maintenance window)
   ├── ./scripts/deploy-env.sh production
   └── Monitor logs, verify deployment
```

### Feature Development Example

```bash
# 1. Create feature branch
git checkout -b feature/add-azure-support

# 2. Implement feature
vim internal/worker/handler_create.go
vim internal/azure/client.go

# 3. Test locally
make test
go run cmd/api/main.go

# 4. Deploy to dev
git add .
git commit -m "Add Azure AKS support"
git push origin feature/add-azure-support
./scripts/deploy-env.sh dev

# 5. Test on dev
curl https://dev.ocpctl.mg.dog8code.com/api/v1/profiles | grep azure

# 6. Create PR
gh pr create --title "Add Azure AKS support" --body "..."

# 7. After PR approval, merge to main
git checkout main
git pull
git merge feature/add-azure-support

# 8. During maintenance window, deploy to production
./scripts/deploy-env.sh production
```

---

## Database Migration Workflow

### Adding New Migration

```bash
# 1. Create migration file
cat > internal/store/migrations/00042_add_azure_support.sql <<EOF
-- Add Azure-specific fields to clusters table
ALTER TABLE clusters ADD COLUMN azure_resource_group VARCHAR(255);
ALTER TABLE clusters ADD COLUMN azure_location VARCHAR(100);

-- Add indexes
CREATE INDEX idx_clusters_azure_rg ON clusters(azure_resource_group)
WHERE azure_resource_group IS NOT NULL;
EOF

# 2. Test locally
psql postgresql://localhost:5432/ocpctl_local < internal/store/migrations/00042_add_azure_support.sql

# 3. Test on dev
./scripts/deploy-env.sh dev
ssh dev "cd /opt/ocpctl/current && ./ocpctl-api migrate up"

# 4. Verify migration
ssh dev "psql \$DATABASE_URL -c '\d clusters'"

# 5. During maintenance window, apply to production
ssh production "cd /opt/ocpctl/current && ./ocpctl-api migrate up"
```

### Migration Rollback

```bash
# If migration fails on dev
ssh dev "cd /opt/ocpctl/current && ./ocpctl-api migrate down 1"

# Fix migration file, redeploy
vim internal/store/migrations/00042_add_azure_support.sql
./scripts/deploy-env.sh dev
ssh dev "cd /opt/ocpctl/current && ./ocpctl-api migrate up"
```

---

## Maintenance Window Procedure

### Pre-Maintenance Checklist (1 week before)

- [ ] All changes tested on dev environment
- [ ] Database migrations tested on dev
- [ ] Performance impact assessed
- [ ] Rollback plan documented
- [ ] Schedule announced to users (if user-facing changes)
- [ ] On-call engineer identified

### Maintenance Window (Example: First Sunday 2-6 AM ET)

**Hour 1: 2:00-3:00 AM - Preparation**
```bash
# 1. Final validation on dev
./scripts/deploy-env.sh dev
curl https://dev.ocpctl.mg.dog8code.com/health

# 2. Create RDS snapshot (production)
aws rds create-db-snapshot \
  --db-instance-identifier ocpctl-db \
  --db-snapshot-identifier ocpctl-db-pre-maint-$(date +%Y%m%d-%H%M)

# 3. Verify snapshot creation
aws rds describe-db-snapshots \
  --db-snapshot-identifier ocpctl-db-pre-maint-$(date +%Y%m%d-%H%M)

# 4. Note current version
CURRENT_VERSION=$(curl -s https://ocpctl.mg.dog8code.com/version | jq -r '.version')
echo "Current production version: $CURRENT_VERSION"
```

**Hour 2: 3:00-4:00 AM - Deployment**
```bash
# 1. Deploy to production
./scripts/deploy-env.sh production

# 2. Monitor deployment logs
ssh -i ~/.ssh/ocpctl-production-key ubuntu@44.201.165.78 \
  'sudo journalctl -u ocpctl-api -f'

# 3. Verify version
curl https://ocpctl.mg.dog8code.com/version
```

**Hour 3: 4:00-5:00 AM - Validation**
```bash
# 1. Smoke tests
curl https://ocpctl.mg.dog8code.com/health
curl https://ocpctl.mg.dog8code.com/api/v1/profiles
curl https://ocpctl.mg.dog8code.com/api/v1/clusters

# 2. Create test cluster (if safe)
# Use UI to create small SNO cluster, verify it reaches READY

# 3. Check metrics
curl https://ocpctl.mg.dog8code.com/metrics | grep -E 'api_requests|worker_jobs'

# 4. Monitor error logs
ssh production 'sudo journalctl -u ocpctl-api -u ocpctl-worker --since "1 hour ago" | grep -i error'
```

**Hour 4: 5:00-6:00 AM - Buffer/Rollback**
```bash
# If issues detected, rollback:

# 1. Rollback code
./scripts/deploy-env.sh production $CURRENT_VERSION

# 2. Rollback database (if migration was applied)
ssh production "cd /opt/ocpctl/current && ./ocpctl-api migrate down 1"

# 3. Restore from snapshot (EXTREME - only if corruption)
aws rds restore-db-instance-from-db-snapshot \
  --db-instance-identifier ocpctl-db-restored \
  --db-snapshot-identifier ocpctl-db-pre-maint-$(date +%Y%m%d-%H%M)

# 4. Verify rollback
curl https://ocpctl.mg.dog8code.com/version
```

### Post-Maintenance Checklist

- [ ] All services running normally
- [ ] No error spike in logs
- [ ] Metrics look healthy
- [ ] Test cluster created successfully (if applicable)
- [ ] Announce completion (if user-facing changes)
- [ ] Document any issues encountered
- [ ] Update runbook if procedures changed

---

## Emergency Hotfix Procedure

### Critical Bug in Production (24/7)

```bash
# 1. Create hotfix branch
git checkout -b hotfix/critical-bug-fix

# 2. Fix bug (minimal changes)
vim internal/worker/handler_create.go

# 3. Test locally
make test
go run cmd/worker/main.go  # Verify fix

# 4. Deploy to dev for quick validation
git add .
git commit -m "Hotfix: Fix cluster creation deadlock"
./scripts/deploy-env.sh dev

# 5. Test on dev
# Create test cluster, verify fix works

# 6. Get approval from on-call manager
# Call/Slack on-call manager for approval

# 7. Deploy to production (no maintenance window needed)
./scripts/deploy-env.sh production

# 8. Monitor for 30 minutes
watch -n 10 'curl -s https://ocpctl.mg.dog8code.com/health'
ssh production 'sudo journalctl -u ocpctl-api -u ocpctl-worker -f'

# 9. Create PR for review (post-deployment)
git push origin hotfix/critical-bug-fix
gh pr create --title "[HOTFIX] Fix cluster creation deadlock" \
  --body "Emergency hotfix deployed to production at $(date)"
```

---

## Deployment Troubleshooting

### Deployment Failed: "API server failed to start"

```bash
# 1. Check logs
ssh production 'sudo journalctl -u ocpctl-api -n 100'

# 2. Check config
ssh production 'sudo cat /etc/ocpctl/api.env | grep -v SECRET | grep -v PASSWORD'

# 3. Check binary permissions
ssh production 'ls -la /opt/ocpctl/current/ocpctl-api'

# 4. Test binary directly
ssh production 'sudo -u ocpctl /opt/ocpctl/current/ocpctl-api --version'

# 5. Rollback if needed
./scripts/deploy-env.sh production <previous-version>
```

### Deployment Failed: "Version mismatch"

```bash
# Binary didn't upload correctly or wrong version built

# 1. Check what was built locally
./bin/ocpctl-api-<version> --version

# 2. Check what's on server
ssh production '/opt/ocpctl/releases/<version>/ocpctl-api --version'

# 3. Rebuild and redeploy
rm bin/ocpctl-api-<version>
./scripts/deploy-env.sh production <version>
```

### Database Migration Failed

```bash
# 1. Check migration status
ssh production 'cd /opt/ocpctl/current && ./ocpctl-api migrate status'

# 2. Check error logs
ssh production 'sudo journalctl -u ocpctl-api -n 100 | grep -i migration'

# 3. Fix migration (if syntax error)
vim internal/store/migrations/00042_xxx.sql

# 4. Rollback failed migration
ssh production 'cd /opt/ocpctl/current && ./ocpctl-api migrate down 1'

# 5. Redeploy fixed migration
./scripts/deploy-env.sh production

# 6. Apply migration
ssh production 'cd /opt/ocpctl/current && ./ocpctl-api migrate up'
```

---

## Monitoring Deployment Health

### Post-Deployment Health Checks

```bash
# API health
curl https://ocpctl.mg.dog8code.com/health
# Expected: {"status":"healthy","timestamp":"..."}

# Version check
curl https://ocpctl.mg.dog8code.com/version
# Expected: {"version":"v0.20260614.xxx","commit":"...","buildTime":"..."}

# Worker health
ssh production 'curl -s http://localhost:8081/health'
# Expected: {"status":"healthy","workers_active":1,"jobs_pending":0}

# Database connectivity
ssh production 'psql $DATABASE_URL -c "SELECT COUNT(*) FROM clusters;"'
# Expected: (count)

# Service status
ssh production 'sudo systemctl status ocpctl-api ocpctl-worker'
# Expected: Active: active (running)
```

### Monitoring Commands

```bash
# Watch logs live
ssh production 'sudo journalctl -u ocpctl-api -u ocpctl-worker -f'

# Check error rate
ssh production 'sudo journalctl -u ocpctl-api --since "10 minutes ago" | grep -c ERROR'

# Check job queue
ssh production 'psql $DATABASE_URL -c "SELECT status, COUNT(*) FROM jobs GROUP BY status;"'

# Check cluster states
ssh production 'psql $DATABASE_URL -c "SELECT status, COUNT(*) FROM clusters GROUP BY status;"'

# Check autoscale workers
aws ec2 describe-instances \
  --filters "Name=tag:Name,Values=ocpctl-worker" "Name=instance-state-name,Values=running" \
  --query 'Reservations[].Instances[].[InstanceId,LaunchTime,State.Name]' \
  --output table
```

---

## Reference

### Key Files

| File | Purpose |
|------|---------|
| `scripts/deploy.sh` | Original single-environment deployment |
| `scripts/deploy-env.sh` | Multi-environment deployment (NEW) |
| `config/api.env.production` | Production API config |
| `config/api.env.dev` | Dev API config |
| `config/worker.env.production` | Production worker config |
| `config/worker.env.dev` | Dev worker config |

### Environment Comparison

| Aspect | Dev | Production |
|--------|-----|------------|
| Domain | dev.ocpctl.mg.dog8code.com | ocpctl.mg.dog8code.com |
| Server | t3.medium (NEW) | t3.large (44.201.165.78) |
| Database | ocpctl_dev (separate or shared RDS) | ocpctl (RDS) |
| S3 Bucket | ocpctl-dev-binaries | ocpctl-binaries |
| SSH Key | ~/.ssh/ocpctl-dev-key | ~/.ssh/ocpctl-production-key |
| Auto-deploy | Yes (on push to main) | No (manual, maintenance window) |
| Autoscale | No | Yes (ASG) |

### Support

For deployment issues:
1. Check this guide
2. Review [DEV_TEST_ENVIRONMENT_PLAN.md](./DEV_TEST_ENVIRONMENT_PLAN.md)
3. Check GitHub Issues
4. Contact on-call engineer
