# OCPCTL Dev Environment Operations Runbook

## Overview

This runbook provides operational procedures for the OCPCTL dev/test environment.

**Environment Details:**
- **Domain**: https://dev.ocpctl.mg.dog8code.com
- **Purpose**: Safe testing environment for code changes before production deployment
- **Infrastructure**: Separate EC2, RDS, and S3 resources managed by Terraform

## Quick Reference

### Server Access

```bash
# SSH to dev server
ssh -i ~/.ssh/ocpctl-dev-key ubuntu@<dev-server-ip>

# Or using terraform output
ssh -i ~/.ssh/ocpctl-dev-key ubuntu@$(cd terraform/dev && terraform output -raw dev_server_public_ip)
```

### Service Management

```bash
# On dev server:
sudo systemctl status ocpctl-api
sudo systemctl status ocpctl-worker

# Restart services
sudo systemctl restart ocpctl-api
sudo systemctl restart ocpctl-worker

# View logs
sudo journalctl -u ocpctl-api -f
sudo journalctl -u ocpctl-worker -f
```

### Database Access

```bash
# From local machine (requires SSH tunnel)
ssh -i ~/.ssh/ocpctl-dev-key -L 5432:<rds-endpoint>:5432 ubuntu@<dev-server-ip>
# Then connect to localhost:5432

# From dev server
psql "$DATABASE_URL" -c "SELECT version();"
```

## Infrastructure Management

### Terraform Operations

**Location**: `terraform/dev/`

#### View Current State

```bash
cd terraform/dev
terraform show
terraform output
```

#### Apply Changes

```bash
cd terraform/dev

# Preview changes
terraform plan

# Apply changes
terraform apply

# Apply specific change
terraform apply -target=aws_instance.dev_server
```

#### Get Outputs

```bash
cd terraform/dev

# All outputs
terraform output

# Specific output
terraform output dev_server_public_ip
terraform output -raw database_url
terraform output -raw ssh_private_key > ~/.ssh/ocpctl-dev-key
```

### Cost Management

#### Stop Dev Server (Save ~60% on EC2 costs)

```bash
# Get instance ID
INSTANCE_ID=$(cd terraform/dev && terraform output -raw dev_server_instance_id)

# Stop instance
aws ec2 stop-instances --instance-ids $INSTANCE_ID

# Start instance
aws ec2 start-instances --instance-ids $INSTANCE_ID

# Note: Elastic IP remains associated, so domain name continues to work
```

**Monthly Savings**: ~$18/month when stopped overnight and weekends

#### Automated Scheduling (Optional)

Create Lambda function or EventBridge rule:
- Stop: Weekdays 6 PM ET, Friday 6 PM (all weekend)
- Start: Weekdays 8 AM ET, Monday 8 AM

### Monitoring Costs

```bash
# View current month costs
aws ce get-cost-and-usage \
  --time-period Start=2026-06-01,End=2026-06-30 \
  --granularity MONTHLY \
  --metrics BlendedCost \
  --filter file://<(cat <<EOF
{
  "Tags": {
    "Key": "Environment",
    "Values": ["dev"]
  }
}
EOF
)

# Expected costs:
# - EC2 t3.medium: ~$30/month (running 24/7)
# - RDS db.t3.micro: ~$15/month
# - S3: ~$1-2/month
# - Total: ~$47/month
```

## Deployment Procedures

### Standard Deployment to Dev

```bash
# From repository root

# 1. Ensure latest code is committed
git status

# 2. Deploy to dev
./scripts/deploy-env.sh dev

# 3. Verify deployment
curl https://dev.ocpctl.mg.dog8code.com/version
curl https://dev.ocpctl.mg.dog8code.com/health

# 4. Check services
ssh -i ~/.ssh/ocpctl-dev-key ubuntu@<dev-ip> \
  'sudo systemctl status ocpctl-api ocpctl-worker'
```

### Deploy Specific Version

```bash
# Deploy specific git commit/tag
./scripts/deploy-env.sh dev v0.20260626.abc1234

# Verify version
curl https://dev.ocpctl.mg.dog8code.com/version
```

### Rollback

```bash
# List available versions
ssh -i ~/.ssh/ocpctl-dev-key ubuntu@<dev-ip> \
  'sudo ls -d /opt/ocpctl/releases/*'

# Deploy previous version
./scripts/deploy-env.sh dev <previous-version>
```

## Database Operations

### Run Migrations

```bash
# On dev server
cd /opt/ocpctl/current
./ocpctl-api migrate up

# Check migration status
./ocpctl-api migrate status
```

### Database Backup

```bash
# Create manual snapshot
aws rds create-db-snapshot \
  --db-instance-identifier ocpctl-dev-db \
  --db-snapshot-identifier ocpctl-dev-manual-$(date +%Y%m%d-%H%M%S)

# List snapshots
aws rds describe-db-snapshots \
  --db-instance-identifier ocpctl-dev-db
```

### Restore from Snapshot

```bash
# 1. Delete current dev database
aws rds delete-db-instance \
  --db-instance-identifier ocpctl-dev-db \
  --skip-final-snapshot

# 2. Restore from snapshot
aws rds restore-db-instance-from-db-snapshot \
  --db-instance-identifier ocpctl-dev-db \
  --db-snapshot-identifier <snapshot-id>

# 3. Wait for restore to complete (~10 minutes)
aws rds wait db-instance-available \
  --db-instance-identifier ocpctl-dev-db
```

### Reset Dev Database

```bash
# WARNING: This deletes all data

# 1. Drop and recreate database
ssh -i ~/.ssh/ocpctl-dev-key ubuntu@<dev-ip>
psql "postgresql://ocpctl_dev_admin:<password>@<rds-endpoint>:5432/postgres?sslmode=require" << EOF
DROP DATABASE IF EXISTS ocpctl_dev;
CREATE DATABASE ocpctl_dev;
EOF

# 2. Re-run initialization
./scripts/init-dev-database.sh
```

## Troubleshooting

### API Server Not Responding

```bash
# 1. Check service status
ssh -i ~/.ssh/ocpctl-dev-key ubuntu@<dev-ip> \
  'sudo systemctl status ocpctl-api'

# 2. Check logs
ssh -i ~/.ssh/ocpctl-dev-key ubuntu@<dev-ip> \
  'sudo journalctl -u ocpctl-api -n 100 --no-pager'

# 3. Check if process is listening
ssh -i ~/.ssh/ocpctl-dev-key ubuntu@<dev-ip> \
  'sudo netstat -tlnp | grep 8080'

# 4. Check nginx
ssh -i ~/.ssh/ocpctl-dev-key ubuntu@<dev-ip> \
  'sudo systemctl status nginx'
ssh -i ~/.ssh/ocpctl-dev-key ubuntu@<dev-ip> \
  'sudo nginx -t'

# 5. Restart services
ssh -i ~/.ssh/ocpctl-dev-key ubuntu@<dev-ip> \
  'sudo systemctl restart ocpctl-api nginx'
```

### Worker Not Processing Jobs

```bash
# 1. Check worker service
ssh -i ~/.ssh/ocpctl-dev-key ubuntu@<dev-ip> \
  'sudo systemctl status ocpctl-worker'

# 2. Check worker logs
ssh -i ~/.ssh/ocpctl-dev-key ubuntu@<dev-ip> \
  'sudo journalctl -u ocpctl-worker -f'

# 3. Check for stuck jobs
psql "$DATABASE_URL" << EOF
SELECT id, cluster_id, job_type, status, started_at,
       NOW() - started_at as duration
FROM jobs
WHERE status = 'RUNNING'
  AND started_at < NOW() - INTERVAL '2 hours';
EOF

# 4. Restart worker
ssh -i ~/.ssh/ocpctl-dev-key ubuntu@<dev-ip> \
  'sudo systemctl restart ocpctl-worker'
```

### Database Connection Issues

```bash
# 1. Test from dev server
ssh -i ~/.ssh/ocpctl-dev-key ubuntu@<dev-ip> \
  'psql "$DATABASE_URL" -c "SELECT 1;"'

# 2. Check RDS status
aws rds describe-db-instances \
  --db-instance-identifier ocpctl-dev-db \
  --query 'DBInstances[0].DBInstanceStatus'

# 3. Check security group
aws ec2 describe-security-groups \
  --filters "Name=group-name,Values=ocpctl-dev-rds-sg" \
  --query 'SecurityGroups[0].IpPermissions'

# 4. Check connectivity from dev server
ssh -i ~/.ssh/ocpctl-dev-key ubuntu@<dev-ip> \
  'nc -zv <rds-endpoint> 5432'
```

### SSL Certificate Issues

```bash
# 1. Check certificate expiry
ssh -i ~/.ssh/ocpctl-dev-key ubuntu@<dev-ip> \
  'sudo certbot certificates'

# 2. Renew certificate manually
ssh -i ~/.ssh/ocpctl-dev-key ubuntu@<dev-ip> \
  'sudo certbot renew --force-renewal'

# 3. Reload nginx
ssh -i ~/.ssh/ocpctl-dev-key ubuntu@<dev-ip> \
  'sudo systemctl reload nginx'
```

### Disk Space Issues

```bash
# 1. Check disk usage
ssh -i ~/.ssh/ocpctl-dev-key ubuntu@<dev-ip> \
  'df -h'

# 2. Check cluster directories
ssh -i ~/.ssh/ocpctl-dev-key ubuntu@<dev-ip> \
  'sudo du -sh /var/lib/ocpctl/clusters/*'

# 3. Clean up old cluster directories
ssh -i ~/.ssh/ocpctl-dev-key ubuntu@<dev-ip> \
  'sudo find /var/lib/ocpctl/clusters -type d -mtime +7 -exec rm -rf {} +'

# 4. Clean up old S3 artifacts
aws s3 ls s3://ocpctl-dev-artifacts/clusters/ | \
  awk '{if ($1 < "'$(date -d '30 days ago' +%Y-%m-%d)'") print $NF}' | \
  xargs -I {} aws s3 rm --recursive s3://ocpctl-dev-artifacts/clusters/{}
```

## Maintenance Windows

### Monthly Maintenance (First Sunday, 2-6 AM ET)

**Procedure:**
1. Deploy latest changes to dev (Friday before)
2. Test thoroughly on dev
3. During maintenance window:
   - Create database backup
   - Deploy to production
   - Monitor for 1 hour
   - Document any issues

### Emergency Hotfix

**Procedure:**
1. Fix bug locally
2. Deploy to dev: `./scripts/deploy-env.sh dev`
3. Test on dev (< 15 min)
4. Get approval
5. Deploy to production: `./scripts/deploy-env.sh production`
6. Monitor for 30 min
7. Create post-deployment PR for review

## Security

### Rotate Database Password

```bash
# 1. Generate new password
NEW_PASS=$(openssl rand -base64 32)

# 2. Update RDS password
aws rds modify-db-instance \
  --db-instance-identifier ocpctl-dev-db \
  --master-user-password "$NEW_PASS" \
  --apply-immediately

# 3. Update config files
# Edit config/api.env.dev and config/worker.env.dev

# 4. Redeploy services
./scripts/deploy-env.sh dev

# 5. Update Terraform state (optional)
cd terraform/dev
terraform apply -var="db_password=$NEW_PASS"
```

### Rotate SSH Key

```bash
# 1. Generate new key
ssh-keygen -t rsa -b 4096 -f ~/.ssh/ocpctl-dev-key-new

# 2. Add new key to server
ssh -i ~/.ssh/ocpctl-dev-key ubuntu@<dev-ip> \
  "echo '$(cat ~/.ssh/ocpctl-dev-key-new.pub)' >> ~/.ssh/authorized_keys"

# 3. Test new key
ssh -i ~/.ssh/ocpctl-dev-key-new ubuntu@<dev-ip> 'echo OK'

# 4. Remove old key
mv ~/.ssh/ocpctl-dev-key ~/.ssh/ocpctl-dev-key.old
mv ~/.ssh/ocpctl-dev-key-new ~/.ssh/ocpctl-dev-key
chmod 600 ~/.ssh/ocpctl-dev-key
```

## Disaster Recovery

### Complete Environment Recreation

If dev environment is completely destroyed:

```bash
# 1. Destroy existing infrastructure
cd terraform/dev
terraform destroy

# 2. Re-provision from scratch
terraform apply

# 3. Save new SSH key
terraform output -raw ssh_private_key > ~/.ssh/ocpctl-dev-key
chmod 600 ~/.ssh/ocpctl-dev-key

# 4. Bootstrap server
cd ../..
./scripts/bootstrap-dev-server.sh $(cd terraform/dev && terraform output -raw dev_server_public_ip)

# 5. Initialize database
./scripts/init-dev-database.sh

# 6. Create config files
# (copy from templates and populate)

# 7. Deploy services
./scripts/deploy-env.sh dev
```

**Time**: ~30 minutes total

## Contacts

- **Primary**: Tech lead
- **Secondary**: Platform team
- **Escalation**: Engineering manager

## Related Documents

- [Dev Environment Plan](DEV_TEST_ENVIRONMENT_PLAN.md)
- [Terraform README](../../terraform/dev/README.md)
- [Main Architecture Guide](../../CLAUDE.md)
