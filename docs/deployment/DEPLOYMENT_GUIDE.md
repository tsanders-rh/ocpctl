# ocpctl Deployment Guide

## Quick Reference

### Deploy Backend to Dev
```bash
./scripts/deploy-env.sh dev
```

### Deploy Backend to Production (with confirmation)
```bash
./scripts/deploy-env.sh production
```

### Deploy Backend Specific Version
```bash
./scripts/deploy-env.sh dev v0.20260614.abc1234
./scripts/deploy-env.sh production v0.20260614.abc1234
```

### Deploy Web Frontend
```bash
# Deploy to dev
./scripts/deploy-web.sh dev

# Deploy to production
./scripts/deploy-web.sh production
```

### Full Deployment (Backend + Frontend)
```bash
# Dev
./scripts/deploy-env.sh dev && ./scripts/deploy-web.sh dev

# Production
./scripts/deploy-env.sh production && ./scripts/deploy-web.sh production
```

### Rollback
```bash
# List available versions
ssh -i ~/.ssh/ocpctl-production-key ubuntu@44.201.165.78 'sudo ls -d /opt/ocpctl/releases/*'

# Deploy previous version
./scripts/deploy-env.sh production v0.20260601.xyz5678
```

---

## Software Release Process Overview

### Release Workflow

```
┌─────────────────────────────────────────────────────────────────────┐
│                        OCPCTL RELEASE PIPELINE                      │
└─────────────────────────────────────────────────────────────────────┘

1. DEVELOPMENT
   ├── Create feature branch (feature/my-feature)
   ├── Develop code (Go backend, Next.js frontend)
   ├── Local testing (make test, make run-api)
   └── Commit changes

2. DEV DEPLOYMENT (Integration Testing)
   ├── Deploy backend: ./scripts/deploy-env.sh dev
   ├── Deploy frontend: ./scripts/deploy-web.sh dev
   ├── Test on https://dev.ocpctl.mg.dog8code.com
   └── Verify all features work end-to-end

3. CODE REVIEW
   ├── Create Pull Request
   ├── Team review and approval
   ├── Automated checks (linting, security)
   └── Merge to main branch

4. PRODUCTION DEPLOYMENT (Maintenance Window)
   ├── Create RDS snapshot (backup)
   ├── Deploy backend: ./scripts/deploy-env.sh production
   ├── Deploy frontend: ./scripts/deploy-web.sh production
   ├── Run smoke tests
   └── Monitor for 30-60 minutes

5. VERIFICATION & MONITORING
   ├── Health checks (API, Worker, Web)
   ├── Check logs for errors
   ├── Verify cluster creation works
   └── Monitor metrics and user reports
```

### Version Format

All releases use semantic versioning with date-based format:

```
v0.YYYYMMDD.COMMITHASH

Examples:
  v0.20260627.22e3b9b  - June 27, 2026 deployment
  v0.20260614.abc1234  - June 14, 2026 deployment
```

**Components:**
- `v0` - Major version (always 0 for pre-1.0)
- `YYYYMMDD` - Deployment date
- `COMMITHASH` - Git commit short hash (7 chars)

### What Gets Released

**Backend (Go services):**
- API Server (`ocpctl-api`) - Port 8080
- Worker Service (`ocpctl-worker`) - Port 8081
- Cluster profiles (YAML definitions)
- Addon definitions (CNV, MTA, MTC, OADP)
- Kubernetes manifests
- Database migrations (automatic on API startup)

**Frontend (Next.js):**
- Web UI (`ocpctl-web`) - Port 3000
- Static assets (CSS, images)
- Client-side JavaScript bundles
- React components (Server + Client)

**Infrastructure:**
- S3 binaries and artifacts
- Autoscaling worker instances (AWS ASG)
- Configuration files (/etc/ocpctl/*.env)

### Deployment Methods

#### Backend Deployment (`deploy-env.sh`)

**What it does:**
1. Builds Go binaries (Linux amd64) with version embedded
2. Uploads to S3 (versioned + stable paths)
3. Syncs profiles, addons, manifests to S3
4. Terminates autoscale workers (ASG replaces with new version)
5. Deploys to API server:
   - Creates `/opt/ocpctl/releases/VERSION/`
   - Updates symlink `/opt/ocpctl/current`
   - Restarts `ocpctl-api` service
6. Deploys to worker servers:
   - Requeues RUNNING jobs to PENDING
   - Clears stale locks
   - Restarts `ocpctl-worker` service
7. Verifies version endpoints

**Zero-downtime:** Uses atomic symlinks, services restart in <5 seconds

#### Frontend Deployment (`deploy-web.sh`)

**What it does:**
1. Installs npm dependencies locally
2. Runs ESLint (with optional continue)
3. Builds Next.js production bundle (`npm run build`)
4. Creates deployment package (excludes node_modules)
5. Uploads to server
6. Backs up current deployment (timestamped)
7. Extracts new files to `/opt/ocpctl/web`
8. Installs production dependencies on server
9. Restarts `ocpctl-web` service
10. Verifies HTTP 200/307 response

**Brief downtime:** ~3-5 seconds during service restart

### Release Environments

| Environment | Domain | Server | Database | Purpose |
|-------------|--------|--------|----------|---------|
| **Dev** | dev.ocpctl.mg.dog8code.com | 54.167.79.11 (t3.medium) | ocpctl_dev (PostgreSQL 17.9) | Integration testing, QA |
| **Production** | ocpctl.mg.dog8code.com | 44.201.165.78 (t3.large) | ocpctl (PostgreSQL 17.9) | Live service |

### Standard Release Cycle

**Weekly Release (Recommended):**
- **Day 1-4 (Mon-Thu):** Development and testing
- **Day 5 (Fri):** Deploy to dev, integration testing
- **Day 6-7 (Sat-Sun):** Production deployment during maintenance window

**Maintenance Window:**
- **Recommended:** First Sunday of month, 2-6 AM ET
- **Emergency hotfix:** Any time with on-call approval

### Database Migrations

**Automatic Migration:**
- Migrations run automatically when `ocpctl-api` starts
- Located in `internal/store/migrations/`
- Uses goose (up/down migrations)

**Migration File Format:**
```sql
-- +goose Up
ALTER TABLE clusters ADD COLUMN new_field VARCHAR(255);

-- +goose Down
ALTER TABLE clusters DROP COLUMN new_field;
```

**Testing Migrations:**
1. Test on dev first: `./scripts/deploy-env.sh dev`
2. Verify schema: `ssh dev 'psql $DATABASE_URL -c "\d clusters"'`
3. Deploy to production during maintenance window

### Rollback Procedures

**Backend Rollback:**
```bash
# List available versions
ssh -i ~/.ssh/ocpctl-production-key ubuntu@44.201.165.78 \
  'sudo ls -d /opt/ocpctl/releases/*'

# Deploy previous version
./scripts/deploy-env.sh production v0.20260601.xyz5678
```

**Frontend Rollback:**
```bash
# List backups
ssh server 'sudo ls -d /opt/ocpctl/web.backup.*'

# Restore backup
ssh server 'sudo systemctl stop ocpctl-web && \
  sudo rm -rf /opt/ocpctl/web && \
  sudo mv /opt/ocpctl/web.backup.20260627-070000 /opt/ocpctl/web && \
  sudo systemctl start ocpctl-web'
```

**Database Rollback:**
```bash
# Rollback last migration
ssh server 'cd /opt/ocpctl/current && ./ocpctl-api migrate down 1'

# Extreme: Restore from RDS snapshot
aws rds restore-db-instance-from-db-snapshot \
  --db-instance-identifier ocpctl-db-restored \
  --db-snapshot-identifier ocpctl-db-pre-maint-20260627
```

### Pre-Deployment Checklist

**Before deploying to production:**
- [ ] All changes tested on dev environment
- [ ] Database migrations tested on dev
- [ ] No breaking API changes (or documented)
- [ ] All tests passing (`make test`)
- [ ] Code reviewed and approved
- [ ] RDS snapshot created
- [ ] Rollback plan documented
- [ ] On-call engineer identified
- [ ] Users notified (if user-facing changes)

### Post-Deployment Verification

**Immediately after deployment:**
```bash
# 1. Health checks
curl https://ocpctl.mg.dog8code.com/health
curl https://ocpctl.mg.dog8code.com/version

# 2. Service status
ssh production 'sudo systemctl status ocpctl-api ocpctl-worker ocpctl-web'

# 3. Check logs
ssh production 'sudo journalctl -u ocpctl-api --since "10 minutes ago" | grep -i error'

# 4. Verify database
ssh production 'psql $DATABASE_URL -c "SELECT COUNT(*) FROM clusters;"'

# 5. Test cluster creation (optional)
# Create small SNO cluster via UI
```

**Monitor for 30-60 minutes:**
- Error rates in logs
- Job queue status
- Cluster creation success rate
- User-reported issues

### Emergency Hotfix Process

**Critical production bug (any time):**

```bash
# 1. Create hotfix branch
git checkout -b hotfix/critical-bug-fix

# 2. Fix bug (minimal changes only)
vim internal/worker/handler_create.go

# 3. Test locally
make test
go run ./cmd/worker

# 4. Quick validation on dev
./scripts/deploy-env.sh dev
# Test the fix works

# 5. Get approval from on-call manager
# Slack/call on-call manager

# 6. Deploy to production immediately
./scripts/deploy-env.sh production
./scripts/deploy-web.sh production  # if frontend changes

# 7. Monitor for 30 minutes
watch -n 10 'curl -s https://ocpctl.mg.dog8code.com/health'
ssh production 'sudo journalctl -u ocpctl-api -f'

# 8. Create PR for post-deployment review
git push origin hotfix/critical-bug-fix
gh pr create --title "[HOTFIX] Critical bug fix"
```

### Release Artifacts

**Build Artifacts:**
- Binaries: `s3://ocpctl-binaries/releases/VERSION/`
- Profiles: `s3://ocpctl-binaries/profiles/`
- Addons: `s3://ocpctl-binaries/addons/`
- Web bundle: `/opt/ocpctl/web/.next/`

**Deployed Locations:**
- Backend: `/opt/ocpctl/releases/VERSION/`
- Current: `/opt/ocpctl/current/` (symlink)
- Frontend: `/opt/ocpctl/web/`
- Configs: `/etc/ocpctl/*.env`

**Version Tracking:**
```bash
# Check deployed versions
curl https://ocpctl.mg.dog8code.com/version
curl https://dev.ocpctl.mg.dog8code.com/version

# Output:
# {
#   "version": "v0.20260627.22e3b9b",
#   "commit": "22e3b9b1234567890abcdef",
#   "buildTime": "2026-06-27T10:30:00Z"
# }
```

### Best Practices

**Development:**
- Always test on dev before production
- Keep feature branches short-lived (<1 week)
- Write database migrations with rollback support
- Include tests for new features

**Deployment:**
- Deploy during low-traffic periods
- Create database snapshots before migrations
- Monitor logs during and after deployment
- Keep rollback instructions handy

**Code Quality:**
- Run linters before committing
- Follow Go and React best practices
- Document breaking changes
- Update CLAUDE.md for architectural changes

**Communication:**
- Announce maintenance windows 1 week ahead
- Document what changed in each release
- Report completion (or issues) after deployment
- Keep runbook updated

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

## Web Frontend Deployment

### What deploy-web.sh Does

```
1. Install npm dependencies locally
   └── npm install

2. Run linting
   └── npm run lint (with option to continue if failed)

3. Build production bundle
   ├── npm run build
   └── Creates optimized .next/ directory

4. Create deployment package
   ├── tar czf ocpctl-web-VERSION.tar.gz
   └── Excludes: node_modules, .next/cache, .env.local

5. Upload to server
   └── scp package to /tmp/

6. Deploy on server
   ├── Stop ocpctl-web service
   ├── Backup current deployment
   ├── Extract new files to /opt/ocpctl/web
   ├── npm install --production (on server)
   └── Start ocpctl-web service

7. Verify deployment
   ├── Check service is running
   ├── Test http://localhost:3000
   └── Test public URL via nginx
```

### Web Frontend Files Deployed

```
/opt/ocpctl/web/
├── .next/              # Production build (optimized, minified)
├── public/             # Static assets
├── src/                # Source code (for error traces)
├── package.json        # Dependencies
├── next.config.mjs     # Next.js config
└── node_modules/       # Production dependencies only
```

### Web Configuration

Environment file: `/etc/ocpctl/web.env`

**Dev:**
```bash
NEXT_PUBLIC_API_URL=/api/v1
NEXT_PUBLIC_AUTH_MODE=jwt
NEXT_PUBLIC_AWS_REGION=us-east-1
NEXT_PUBLIC_APP_ENV=dev
NODE_ENV=production
PORT=3000
```

**Production:**
```bash
NEXT_PUBLIC_API_URL=/api/v1
NEXT_PUBLIC_AUTH_MODE=iam  # or jwt
NEXT_PUBLIC_AWS_REGION=us-east-1
NEXT_PUBLIC_APP_ENV=production
NODE_ENV=production
PORT=3000
```

### Web Frontend Rollback

```bash
# List backups
ssh -i ~/.ssh/ocpctl-dev-key ubuntu@54.167.79.11 \
  'sudo ls -d /opt/ocpctl/web.backup.*'

# Restore backup
ssh -i ~/.ssh/ocpctl-dev-key ubuntu@54.167.79.11 \
  'sudo systemctl stop ocpctl-web && \
   sudo rm -rf /opt/ocpctl/web && \
   sudo mv /opt/ocpctl/web.backup.20260627-070000 /opt/ocpctl/web && \
   sudo systemctl start ocpctl-web'
```

### Troubleshooting Web Deployment

**Build fails:**
```bash
# Check Node.js version (must be 18+)
node --version

# Clean and rebuild
cd web
rm -rf .next node_modules
npm install
npm run build
```

**Service won't start:**
```bash
# Check logs
ssh server 'sudo journalctl -u ocpctl-web -n 50 --no-pager'

# Check if port 3000 is in use
ssh server 'sudo lsof -i :3000'

# Verify environment file
ssh server 'sudo cat /etc/ocpctl/web.env'
```

**Frontend shows 404 or blank page:**
```bash
# Verify nginx routing
ssh server 'sudo nginx -t'
ssh server 'sudo cat /etc/nginx/sites-enabled/ocpctl'

# Check nginx logs
ssh server 'sudo tail -f /var/log/nginx/error.log'
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
