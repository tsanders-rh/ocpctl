# OCPCTL AWS Deployment - Quick Start

This is a condensed deployment guide. See [DEPLOYMENT.md](DEPLOYMENT.md) for detailed instructions.

## Prerequisites

- AWS account with appropriate permissions
- SSH key pair for EC2 access
- OpenShift pull secret from Red Hat

## ⚠️ AWS Account Automated Cleanup

**CRITICAL**: This AWS account has Lambda functions that automatically purge resources:

- **aws_purge_instance_schedule**: Terminates EC2 instances every **Friday 8 PM EDT**
  - **Targets**: Instances with "test" in Name tag
  - **Prevention**: Tag production instances as `Name=ocpctl-production`

**What happened**: On 2026-04-11, the production API server was terminated because it was tagged as `Name=ocpctl-test`. The automated cleanup Lambda (`aws-reporting`) terminated it at exactly 8:00 PM EDT, causing complete service outage and loss of all cluster metadata.

To check current cleanup schedule:
```bash
aws events describe-rule --name aws_purge_instance_schedule
```

To disable cleanup (if needed):
```bash
aws events disable-rule --name aws_purge_instance_schedule
```

**NEVER tag production infrastructure with "test" in the name.**

## 1. Launch EC2 Instance

**Instance Sizing:**
- **Recommended**: t3.large (2 vCPU, 8 GB RAM) - $60/month
  - Sufficient for API + PostgreSQL + backup worker
  - Autoscaling workers handle most cluster deployments
  - Good balance of cost and performance

**Disk Sizing Guide:**
- **Small deployments (1-10 clusters):** 50GB root volume
- **Medium deployments (10-25 clusters):** 75GB root volume
- **Large deployments (25-50 clusters):** 100GB root volume

Each cluster work directory uses ~50-250MB depending on install time and failures.
The janitor automatically cleans up DESTROYED clusters after 30 days and FAILED directories after 7 days.

```bash
# Launch t3.large with Ubuntu 22.04
# CRITICAL: Tag as "ocpctl-production" NOT "ocpctl-test" to avoid automated Friday purge
# DeleteOnTermination=false preserves data if instance is accidentally terminated
aws ec2 run-instances \
  --image-id ami-05e86b3611c60b0b4 \
  --instance-type t3.large \
  --key-name ocpctl-production-key \
  --security-group-ids sg-0b95b88e8835675a6 \
  --iam-instance-profile Name=ocpctl-ec2-role \
  --tag-specifications 'ResourceType=instance,Tags=[{Key=Name,Value=ocpctl-production},{Key=Environment,Value=production},{Key=Purpose,Value=ocpctl-api-server}]' \
  --block-device-mappings 'DeviceName=/dev/sda1,Ebs={VolumeSize=100,DeleteOnTermination=false}'

# Note the instance ID and public IP
```

## 2. Generate Secure Database Password

**Use AWS Systems Manager Parameter Store** (free, secure, auditable):

```bash
# Generate and store a secure 32-character password
aws ssm put-parameter \
  --name "/ocpctl/database/password" \
  --description "PostgreSQL password for ocpctl production database" \
  --value "$(openssl rand -base64 32 | tr -d '/+=' | head -c 32)" \
  --type "SecureString" \
  --tier "Standard" \
  --tags "Key=Environment,Value=production" "Key=Application,Value=ocpctl"

# Verify it was created
aws ssm get-parameter \
  --name "/ocpctl/database/password" \
  --with-decryption \
  --query 'Parameter.{Name:Name,Type:Type,LastModified:LastModifiedDate}' \
  --output table
```

**Why Parameter Store?**
- ✅ **FREE** (no cost for standard parameters)
- ✅ **Encrypted at rest** (AWS KMS)
- ✅ **Audit trail** (CloudTrail logs every access)
- ✅ **IAM access control** (only authorized instances can retrieve)
- ✅ **Version history** (can rollback if needed)

**IAM Role Requirements:**

Ensure your EC2 instance role (`ocpctl-role`) has this permission:

```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Action": [
        "ssm:GetParameter"
      ],
      "Resource": "arn:aws:ssm:us-east-1:*:parameter/ocpctl/database/password"
    }
  ]
}
```

## 3. Setup PostgreSQL

**Option A: RDS** (recommended for production)

```bash
# Retrieve password from Parameter Store
DB_PASSWORD=$(aws ssm get-parameter \
  --name "/ocpctl/database/password" \
  --with-decryption \
  --query 'Parameter.Value' \
  --output text)

# Create RDS instance with secure password
aws rds create-db-instance \
  --db-instance-identifier ocpctl-db \
  --db-instance-class db.t3.micro \
  --engine postgres \
  --master-username ocpctl_user \
  --master-user-password "$DB_PASSWORD" \
  --allocated-storage 20 \
  --db-name ocpctl \
  --backup-retention-period 7 \
  --storage-encrypted \
  --tags "Key=Environment,Value=production" "Key=Application,Value=ocpctl"

# Note the endpoint for later use
aws rds describe-db-instances \
  --db-instance-identifier ocpctl-db \
  --query 'DBInstances[0].Endpoint.Address' \
  --output text
```

**Option B: On EC2** (simpler, co-located with API server)

```bash
# SSH to EC2
ssh -i ~/.ssh/ocpctl-production-key ubuntu@<ip>

# Install PostgreSQL
sudo apt-get update && sudo apt-get install -y postgresql

# Retrieve password from Parameter Store
DB_PASSWORD=$(aws ssm get-parameter \
  --name "/ocpctl/database/password" \
  --with-decryption \
  --query 'Parameter.Value' \
  --output text)

# Create database and user with secure password
sudo -u postgres psql << EOF
CREATE DATABASE ocpctl;
CREATE USER ocpctl_user WITH PASSWORD '$DB_PASSWORD';
GRANT ALL PRIVILEGES ON DATABASE ocpctl TO ocpctl_user;
\c ocpctl
CREATE EXTENSION IF NOT EXISTS "uuid-ossp";
EOF

# Verify connection
psql "postgresql://ocpctl_user:$DB_PASSWORD@localhost:5432/ocpctl" -c '\dt'
```

## 4. Setup Server

```bash
# SSH to EC2
ssh -i ~/.ssh/ocpctl-production-key ubuntu@<ip>

# Clone and setup
git clone https://github.com/tsanders-rh/ocpctl.git
cd ocpctl
sudo bash deploy/setup.sh

# Install OpenShift installer
# Browse available versions: https://mirror.openshift.com/pub/openshift-v4/clients/ocp/
# Choose version based on your needs (4.14, 4.15, 4.16, etc.)
VERSION="4.16.0"
wget https://mirror.openshift.com/pub/openshift-v4/clients/ocp/${VERSION}/openshift-install-linux.tar.gz
tar -xzf openshift-install-linux.tar.gz
sudo mv openshift-install /usr/local/bin/
sudo chmod +x /usr/local/bin/openshift-install
rm openshift-install-linux.tar.gz

# Verify installation
openshift-install version

# Output should show:
# openshift-install 4.16.0
# built from commit ...
```

## 5. Deploy Application

**From your local machine:**

```bash
# Set deployment host
export DEPLOY_HOST=ubuntu@<ec2-ip>

# Build and deploy
make build-linux
make deploy
```

## 6. Configure

**On EC2 instance:**

```bash
# Retrieve database password from Parameter Store
DB_PASSWORD=$(aws ssm get-parameter \
  --name "/ocpctl/database/password" \
  --with-decryption \
  --query 'Parameter.Value' \
  --output text)

# Create API config
sudo bash -c "cat > /etc/ocpctl/api.env" << EOF
DATABASE_URL=postgresql://ocpctl_user:${DB_PASSWORD}@localhost:5432/ocpctl
PORT=8080
PROFILES_DIR=/opt/ocpctl/profiles
ADDONS_DIR=/opt/ocpctl/addons
ENVIRONMENT=production
EOF

# Upload pull secret first
# scp pull-secret.json ubuntu@<ec2-ip>:~/

# Create worker config
PULL_SECRET=\$(cat ~/pull-secret.json)
sudo bash -c "cat > /etc/ocpctl/worker.env" << EOF
DATABASE_URL=postgresql://ocpctl_user:${DB_PASSWORD}@localhost:5432/ocpctl
WORKER_WORK_DIR=/var/lib/ocpctl/clusters
OPENSHIFT_PULL_SECRET=\${PULL_SECRET}
AWS_REGION=us-east-1
S3_BUCKET_NAME=ocpctl-binaries
PROFILES_DIR=/opt/ocpctl/profiles
ENVIRONMENT=production
EOF

# Secure configs
sudo chmod 600 /etc/ocpctl/*.env
sudo chown ocpctl:ocpctl /etc/ocpctl/*.env
rm ~/pull-secret.json

# Upload worker config to S3 (for autoscaling workers)
# This allows ASG workers to bootstrap with correct database connection
aws s3 cp /etc/ocpctl/worker.env s3://ocpctl-binaries/config/worker.env
```

**Important**:
- Password is retrieved from Parameter Store (not hardcoded)
- The `worker.env` file must be uploaded to S3 for autoscaling workers (`ocpctl-worker-asg`)
- If you redeploy with a new database, update this file in S3 and terminate existing workers

## 7. Setup SSL/HTTPS (Production)

**Install Nginx:**

```bash
sudo apt-get update
sudo apt-get install -y nginx certbot python3-certbot-nginx
```

**Deploy Nginx configuration:**

```bash
# From your local machine
scp -i ~/.ssh/ocpctl-production-key deploy/nginx/ocpctl.conf ubuntu@<ec2-ip>:~/

# On EC2 instance
sudo mv ~/ocpctl.conf /etc/nginx/sites-available/ocpctl
sudo ln -s /etc/nginx/sites-available/ocpctl /etc/nginx/sites-enabled/
sudo rm /etc/nginx/sites-enabled/default  # Remove default site
```

**Option A: Let's Encrypt (Recommended for Production)**

```bash
# Request SSL certificate (requires DNS pointing to this server)
sudo certbot --nginx -d ocpctl.mg.dog8code.com

# Certbot will automatically:
# 1. Request certificate from Let's Encrypt
# 2. Update nginx configuration with SSL cert paths
# 3. Setup automatic renewal

# Test renewal
sudo certbot renew --dry-run
```

**Option B: Self-Signed Certificate (Development/Testing)**

```bash
# Generate self-signed certificate
sudo openssl req -x509 -nodes -days 365 -newkey rsa:2048 \
  -keyout /etc/ssl/private/ocpctl.key \
  -out /etc/ssl/certs/ocpctl.crt \
  -subj "/CN=ocpctl.mg.dog8code.com"

# Nginx is already configured to use these paths
```

**Start Nginx:**

```bash
# Test configuration
sudo nginx -t

# Start nginx
sudo systemctl enable nginx
sudo systemctl start nginx

# Verify HTTPS works
curl https://ocpctl.mg.dog8code.com/health
```

**Update DNS:**

Ensure `ocpctl.mg.dog8code.com` points to your EC2 instance public IP:
- Route53 A record: `ocpctl.mg.dog8code.com` → `<ec2-public-ip>`

## 8. Start Services

```bash
# Install systemd services
cd ocpctl
sudo make install-services

# Enable and start
sudo systemctl enable ocpctl-api ocpctl-worker
sudo systemctl start ocpctl-api ocpctl-worker

# Check status
sudo systemctl status ocpctl-api ocpctl-worker
```

## 8a. Autoscaling Workers (Production Setup)

Your AWS account already has an **Autoscaling Group** (`ocpctl-worker-asg`) that handles worker instances automatically.

**How it works:**

```
┌─────────────────────────────────────────────────────┐
│  Autoscaling Group: ocpctl-worker-asg              │
│  - Min: 1, Max: 10, Desired: 1                     │
│  - Instance Type: t3.small                         │
│  - Self-healing: Auto-replaces failed instances    │
└─────────────────────────────────────────────────────┘
                     │
                     ▼
         ┌───────────────────────┐
         │  New Worker Instance  │
         │  (on first boot)      │
         └───────────────────────┘
                     │
        ┌────────────┴────────────┐
        ▼                         ▼
   Download from S3          Install & Start
   ────────────────          ───────────────
   • worker binary           • systemd service
   • worker.env (DB config)  • Health check
   • bootstrap script        • Ready for jobs
   • systemd service file
```

**Worker Bootstrap (automatic on instance launch):**

1. Downloads `bootstrap-worker.sh` from `s3://ocpctl-binaries/scripts/`
2. Downloads `worker.env` from `s3://ocpctl-binaries/config/` (contains DATABASE_URL)
3. Downloads latest worker binary from `s3://ocpctl-binaries/releases/LATEST/`
4. Installs systemd service and starts worker
5. Worker connects to API server database and starts polling for jobs

**After API Server Redeployment:**

When you redeploy the API server (new IP/database), workers automatically refresh:

```bash
# The deploy.sh script (scripts/deploy.sh) automatically:
# 1. Uploads new worker.env to S3 (line 86)
# 2. Uploads latest worker binary to S3 (line 66)
# 3. Terminates all ASG workers (lines 105-125)
# 4. ASG launches fresh workers that download new config

# Manual worker refresh (if needed):
aws ec2 terminate-instances --instance-ids $(aws autoscaling describe-auto-scaling-groups \
  --auto-scaling-group-names ocpctl-worker-asg \
  --query 'AutoScalingGroups[0].Instances[].InstanceId' --output text)

# ASG automatically launches replacement within 60 seconds
```

**Check worker status:**

```bash
# List ASG workers
aws autoscaling describe-auto-scaling-groups \
  --auto-scaling-group-names ocpctl-worker-asg \
  --query 'AutoScalingGroups[0].{Min:MinSize,Max:MaxSize,Desired:DesiredCapacity,Current:Instances[].InstanceId}'

# SSH to worker (if needed)
WORKER_IP=$(aws ec2 describe-instances \
  --instance-ids $(aws autoscaling describe-auto-scaling-groups \
    --auto-scaling-group-names ocpctl-worker-asg \
    --query 'AutoScalingGroups[0].Instances[0].InstanceId' --output text) \
  --query 'Reservations[0].Instances[0].PublicIpAddress' --output text)

ssh -i ~/.ssh/ocpctl-test-key.pem ec2-user@$WORKER_IP

# On worker, check service
sudo systemctl status ocpctl-worker
sudo journalctl -u ocpctl-worker -f
```

**Important Notes:**

- ✅ **API server runs a backup worker** (hybrid approach) - handles jobs if ASG is at 0
- ✅ **Workers auto-scale** - ASG can launch up to 10 workers during high load
- ✅ **Workers are ephemeral** - safe to terminate anytime (no persistent state)
- ✅ **S3 is source of truth** - all worker config stored in `s3://ocpctl-binaries/`
- ⚠️ **After new API deployment** - Run `scripts/deploy.sh` to refresh workers with new DB connection

## 9. Verify

```bash
# Health check (local)
curl http://localhost:8080/health

# Health check (HTTPS - production)
curl https://ocpctl.mg.dog8code.com/health

# Create test cluster
curl -X POST http://localhost:8080/api/v1/clusters \
  -H "Content-Type: application/json" \
  -d '{
    "name": "test-cluster",
    "platform": "aws",
    "version": "4.14.0",
    "profile": "aws-dev-small",
    "region": "us-east-1",
    "base_domain": "example.com",
    "owner": "admin",
    "team": "platform",
    "ttl_hours": 4
  }'

# Watch logs
sudo journalctl -u ocpctl-worker -f
```

## Common Commands

```bash
# View logs
sudo make logs              # All services
sudo make logs-api          # API only
sudo make logs-worker       # Worker only

# Restart services
sudo make restart

# Deploy updates (from local machine)
make deploy && ssh ubuntu@<ip> 'sudo systemctl restart ocpctl-api ocpctl-worker'

# List clusters
curl http://localhost:8080/api/v1/clusters | jq

# Check database
psql ocpctl -c "SELECT name, status, created_at FROM clusters ORDER BY created_at DESC LIMIT 5;"

# Check autoscaling workers
aws autoscaling describe-auto-scaling-groups \
  --auto-scaling-group-names ocpctl-worker-asg \
  --query 'AutoScalingGroups[0].{Desired:DesiredCapacity,Current:length(Instances),Healthy:length(Instances[?HealthStatus==`Healthy`])}'

# List all worker instances (API server + ASG)
aws ec2 describe-instances \
  --filters "Name=tag:Name,Values=ocpctl-production,ocpctl-worker" \
            "Name=instance-state-name,Values=running" \
  --query 'Reservations[].Instances[].{Name:Tags[?Key==`Name`].Value|[0],ID:InstanceId,IP:PublicIpAddress,Type:InstanceType}' \
  --output table

# Check for clusters in DB but not in AWS (manually destroyed)
psql ocpctl -c "SELECT id, name, status, created_at FROM clusters WHERE status IN ('READY', 'CREATING') ORDER BY created_at DESC;"
# Then manually verify these clusters exist in AWS Console

# Check janitor logs for orphaned AWS resources (VPCs, LBs, EC2 instances without DB records)
sudo journalctl -u ocpctl-worker -g "orphaned" -n 100
sudo journalctl -u ocpctl-worker --since "1 hour ago" | grep -A 20 "orphaned AWS resources"

# Watch for orphan detection in real-time
sudo journalctl -u ocpctl-worker -f | grep -i orphan
```

## Troubleshooting

### Common Issues

```bash
# Service won't start
sudo journalctl -u ocpctl-api -n 50
sudo journalctl -u ocpctl-worker -n 50

# Test database
psql "$DATABASE_URL" -c '\dt'

# Check pending jobs
psql ocpctl -c "SELECT id, job_type, status FROM jobs WHERE status='PENDING';"

# View install logs
sudo tail -f /var/lib/ocpctl/clusters/<cluster-id>/.openshift_install.log

# Check disk space
df -h /
df -h /var/lib/ocpctl

# Check cluster directory sizes
du -sh /var/lib/ocpctl/clusters/*
du -sh /var/lib/ocpctl/clusters/  # Total size

# Monitor largest directories
du -h /var/lib/ocpctl/clusters/ | sort -rh | head -20

# Count active cluster directories
ls -1 /var/lib/ocpctl/clusters/ | wc -l

# Check inode usage (can also run out)
df -i /var/lib/ocpctl
```

### Clusters Destroyed Outside of ocpctl

If a cluster is manually destroyed in AWS (console, CLI, or Terraform) but still shows as READY in ocpctl:

```bash
# 1. Verify cluster is actually gone in AWS
aws ec2 describe-vpcs --filters "Name=tag:Name,Values=*<cluster-name>*" --region us-east-1

# 2. If confirmed gone, manually mark as destroyed
psql ocpctl <<EOF
UPDATE clusters
SET status = 'DESTROYED',
    destroyed_at = NOW(),
    updated_at = NOW()
WHERE id = '<cluster-id>';
EOF

# 3. Clean up work directory
sudo rm -rf /var/lib/ocpctl/clusters/<cluster-id>

# 4. Verify cleanup
psql ocpctl -c "SELECT name, status, destroyed_at FROM clusters WHERE id = '<cluster-id>';"
```

**Prevention:** Use ocpctl's destroy functionality to ensure proper cleanup and database synchronization.

### Database Password Rotation

Rotate the database password every 90 days for production security:

```bash
# 1. Generate new password and update Parameter Store
NEW_PASSWORD=$(openssl rand -base64 32 | tr -d '/+=' | head -c 32)

aws ssm put-parameter \
  --name "/ocpctl/database/password" \
  --value "$NEW_PASSWORD" \
  --type "SecureString" \
  --overwrite

# 2. Update PostgreSQL password
sudo -u postgres psql -c "ALTER USER ocpctl_user WITH PASSWORD '$NEW_PASSWORD';"

# 3. Update API server config
DB_PASSWORD=$(aws ssm get-parameter \
  --name "/ocpctl/database/password" \
  --with-decryption \
  --query 'Parameter.Value' \
  --output text)

sudo bash -c "cat > /etc/ocpctl/api.env" << EOF
DATABASE_URL=postgresql://ocpctl_user:${DB_PASSWORD}@localhost:5432/ocpctl
PORT=8080
PROFILES_DIR=/opt/ocpctl/profiles
ADDONS_DIR=/opt/ocpctl/addons
ENVIRONMENT=production
EOF

# 4. Update worker config (API server)
PULL_SECRET=\$(sudo cat /etc/ocpctl/worker.env | grep OPENSHIFT_PULL_SECRET | cut -d= -f2-)
sudo bash -c "cat > /etc/ocpctl/worker.env" << EOF
DATABASE_URL=postgresql://ocpctl_user:${DB_PASSWORD}@localhost:5432/ocpctl
WORKER_WORK_DIR=/var/lib/ocpctl/clusters
OPENSHIFT_PULL_SECRET=\${PULL_SECRET}
AWS_REGION=us-east-1
S3_BUCKET_NAME=ocpctl-binaries
PROFILES_DIR=/opt/ocpctl/profiles
ENVIRONMENT=production
EOF

# 5. Upload updated worker config to S3 (for ASG workers)
aws s3 cp /etc/ocpctl/worker.env s3://ocpctl-binaries/config/worker.env

# 6. Restart API and worker services
sudo systemctl restart ocpctl-api ocpctl-worker

# 7. Terminate ASG workers to refresh with new password
aws ec2 terminate-instances --instance-ids $(aws autoscaling describe-auto-scaling-groups \
  --auto-scaling-group-names ocpctl-worker-asg \
  --query 'AutoScalingGroups[0].Instances[].InstanceId' --output text)

# 8. Verify new password works
psql "postgresql://ocpctl_user:${DB_PASSWORD}@localhost:5432/ocpctl" -c '\dt'
```

**Rotation Schedule:**
- Production: Every 90 days
- Development: Every 180 days or on suspected compromise

**Audit Trail:**
```bash
# View password access history in CloudTrail
aws cloudtrail lookup-events \
  --lookup-attributes AttributeKey=ResourceName,AttributeValue=/ocpctl/database/password \
  --max-results 50 \
  --query 'Events[].{Time:EventTime,User:Username,Action:EventName}' \
  --output table
```

## Security Checklist

- [ ] Security Group restricts API access
- [ ] Environment files are mode 600
- [ ] Database password stored in Parameter Store (not hardcoded)
- [ ] Database password is 32+ characters (auto-generated)
- [ ] IAM instance role has minimum required permissions
- [ ] IAM role has ssm:GetParameter permission for `/ocpctl/database/password`
- [ ] Pull secret file removed from home directory
- [ ] System packages are updated
- [ ] Backups configured (database + S3 artifacts)
- [ ] Password rotation scheduled (every 90 days)
- [ ] CloudTrail logging enabled (audit trail for secret access)

## Architecture

```
┌─────────────────────────────────────────┐
│         AWS EC2 Instance                │
│  ┌─────────────┐      ┌──────────────┐ │
│  │  API Server │      │    Worker    │ │
│  │   :8080     │      │   +Janitor   │ │
│  └─────────────┘      └──────────────┘ │
│         │                    │          │
│         └────────┬───────────┘          │
│                  │                      │
│         ┌────────▼────────┐             │
│         │   PostgreSQL    │             │
│         │   (local/RDS)   │             │
│         └─────────────────┘             │
│                                         │
│  Work Dir: /var/lib/ocpctl/clusters    │
└─────────────────────────────────────────┘
              │
              │ (Creates/Destroys)
              ▼
        OpenShift Clusters
        (AWS Infrastructure)
```

See [DEPLOYMENT.md](DEPLOYMENT.md) for complete documentation.
