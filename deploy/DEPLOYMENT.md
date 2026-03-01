# OCPCTL Deployment Guide - AWS EC2

This guide explains how to deploy ocpctl on an AWS EC2 instance.

## Prerequisites

### AWS Resources

1. **EC2 Instance**
   - Recommended: t3.medium or larger (2 vCPU, 4GB RAM minimum)
   - OS: Ubuntu 22.04 LTS or Amazon Linux 2023
   - Storage: 50GB+ EBS volume (for cluster work directories)
   - Security Group:
     - Inbound: Port 8080 (API) from your IP or VPC
     - Outbound: Allow all (for OpenShift installer)

2. **RDS PostgreSQL** (or PostgreSQL on EC2)
   - Engine: PostgreSQL 14+
   - Instance: db.t3.micro or larger
   - Storage: 20GB minimum
   - Accessible from EC2 instance

3. **IAM Instance Role** (for AWS cluster provisioning)
   - Attach to EC2 instance
   - Permissions needed:
     - EC2: Full access
     - VPC: Full access
     - IAM: Create roles, policies, instance profiles
     - Route53: Manage hosted zones (if using AWS DNS)
     - S3: Create/manage buckets (optional, for future artifact storage)

### Required Secrets

1. **OpenShift Pull Secret**
   - Get from: https://console.redhat.com/openshift/install/pull-secret
   - Save as JSON file

2. **Database Password**
   - Generate secure password for PostgreSQL

## Deployment Steps

### Step 1: Launch EC2 Instance

```bash
# Example using AWS CLI
aws ec2 run-instances \
  --image-id ami-0c55b159cbfafe1f0 \  # Ubuntu 22.04 LTS (us-east-1)
  --instance-type t3.medium \
  --key-name your-key-pair \
  --security-group-ids sg-xxxxxxxxx \
  --iam-instance-profile Name=ocpctl-instance-role \
  --block-device-mappings 'DeviceName=/dev/sda1,Ebs={VolumeSize=50}' \
  --tag-specifications 'ResourceType=instance,Tags=[{Key=Name,Value=ocpctl-server}]'
```

Get the public IP:
```bash
aws ec2 describe-instances \
  --instance-ids i-xxxxxxxxx \
  --query 'Reservations[0].Instances[0].PublicIpAddress' \
  --output text
```

### Step 2: Setup Database

**Option A: RDS PostgreSQL (Recommended for Production)**
```bash
aws rds create-db-instance \
  --db-instance-identifier ocpctl-db \
  --db-instance-class db.t3.micro \
  --engine postgres \
  --engine-version 14.9 \
  --master-username ocpctl \
  --master-user-password 'your-secure-password' \
  --allocated-storage 20 \
  --vpc-security-group-ids sg-xxxxxxxxx \
  --db-name ocpctl \
  --publicly-accessible false \
  --storage-encrypted true
```

Get the RDS endpoint:
```bash
aws rds describe-db-instances \
  --db-instance-identifier ocpctl-db \
  --query 'DBInstances[0].Endpoint.Address' \
  --output text
```

**Note**: RDS automatically enforces SSL connections. Use `sslmode=require` in DATABASE_URL.

**Option B: PostgreSQL on EC2 (Development/Testing)**
```bash
# SSH to EC2 instance
sudo apt-get update
sudo apt-get install -y postgresql postgresql-contrib

# Configure PostgreSQL
sudo -u postgres psql -c "CREATE DATABASE ocpctl;"
sudo -u postgres psql -c "CREATE USER ocpctl WITH PASSWORD 'your-secure-password';"
sudo -u postgres psql -c "GRANT ALL PRIVILEGES ON DATABASE ocpctl TO ocpctl;"

# Allow local connections (edit /etc/postgresql/*/main/pg_hba.conf)
# Add: host ocpctl ocpctl 127.0.0.1/32 md5
sudo systemctl restart postgresql
```

**Note**: If using local PostgreSQL, you can use `sslmode=disable` for DATABASE_URL since it's local.
For production, consider using RDS with SSL instead.

### Step 3: Setup EC2 Instance

SSH to your instance:
```bash
ssh -i your-key.pem ubuntu@<ec2-public-ip>
```

Run the setup script:
```bash
# Clone the repository
git clone https://github.com/tsanders-rh/ocpctl.git
cd ocpctl

# Run setup script
sudo bash deploy/setup.sh
```

This creates:
- User: `ocpctl`
- Directories: `/opt/ocpctl`, `/var/lib/ocpctl`, `/etc/ocpctl`
- Installs: Go, AWS CLI, PostgreSQL client

### Step 4: Install OpenShift Installer

```bash
# Download openshift-install
VERSION="4.14.0"
wget https://mirror.openshift.com/pub/openshift-v4/clients/ocp/${VERSION}/openshift-install-linux.tar.gz

# Extract and install
tar -xzf openshift-install-linux.tar.gz
sudo mv openshift-install /usr/local/bin/
sudo chmod +x /usr/local/bin/openshift-install

# Verify
openshift-install version
```

### Step 5: Build and Deploy Binaries

**From your local machine:**

Update `DEPLOY_HOST` in Makefile or set as environment variable:
```bash
export DEPLOY_HOST=ubuntu@<ec2-public-ip>
```

Build and deploy:
```bash
# Build binaries for Linux
make build-linux

# Deploy binaries and profiles
make deploy

# Or deploy separately:
# make deploy-binaries
# make deploy-profiles
```

### Step 6: Configure Environment

**On the EC2 instance:**

**Generate JWT Secret (CRITICAL):**
```bash
# Generate a strong random secret (minimum 32 characters)
JWT_SECRET=$(openssl rand -base64 32)
echo "Generated JWT_SECRET: $JWT_SECRET"
# Save this somewhere secure!
```

Create API configuration:
```bash
# Replace placeholders with your actual values:
# - your-password: Your PostgreSQL password
# - your-jwt-secret: The JWT_SECRET you just generated
# - your-domain.com: Your production frontend domain (or EC2 public IP)

sudo bash -c 'cat > /etc/ocpctl/api.env' << 'EOF'
# Database Configuration (REQUIRED)
DATABASE_URL=postgres://ocpctl:your-password@localhost:5432/ocpctl?sslmode=require

# API Server Configuration
PORT=8080
PROFILES_DIR=/opt/ocpctl/profiles

# Authentication Configuration (CRITICAL - REQUIRED in production)
# Generate with: openssl rand -base64 32
JWT_SECRET=your-jwt-secret

# CORS Configuration (REQUIRED for web frontend)
# Set to your production frontend URL
CORS_ALLOWED_ORIGINS=https://your-domain.com

# IAM Authentication (optional)
ENABLE_IAM_AUTH=false

# Logging
LOG_LEVEL=info

# Environment (CRITICAL - triggers security validations)
ENVIRONMENT=production
EOF
```

**Important Security Notes:**
- **JWT_SECRET**: Server will FAIL TO START if this is missing or less than 32 characters in production
- **sslmode=require**: Always use SSL for database connections in production
- **CORS_ALLOWED_ORIGINS**: Must match your frontend URL exactly (include protocol and port)

Create Worker configuration:
```bash
# First, upload your pull secret
scp pull-secret.json ubuntu@<ec2-public-ip>:~/

# Then on EC2:
sudo bash -c 'cat > /etc/ocpctl/worker.env' << EOF
DATABASE_URL=postgres://ocpctl:your-password@localhost:5432/ocpctl?sslmode=disable
WORKER_WORK_DIR=/var/lib/ocpctl/clusters
OPENSHIFT_PULL_SECRET=$(cat ~/pull-secret.json)
OPENSHIFT_INSTALL_BINARY=/usr/local/bin/openshift-install
AWS_REGION=us-east-1
LOG_LEVEL=info
ENVIRONMENT=production
EOF

# Secure the files
sudo chmod 600 /etc/ocpctl/*.env
sudo chown ocpctl:ocpctl /etc/ocpctl/*.env

# Remove pull secret from home
rm ~/pull-secret.json
```

### Step 7: Verify Configuration

**CRITICAL: Verify all required configuration before starting services**

```bash
# Check API configuration
sudo cat /etc/ocpctl/api.env

# Verify critical settings:
# ✓ JWT_SECRET is set and ≥32 characters
# ✓ DATABASE_URL has correct password and sslmode
# ✓ CORS_ALLOWED_ORIGINS is set
# ✓ ENVIRONMENT=production
```

**Test database connection:**
```bash
# Extract DATABASE_URL and test connection
source /etc/ocpctl/api.env
psql "$DATABASE_URL" -c '\dt'
# Should connect successfully and show tables (or empty list on first run)
```

### Step 8: Install and Start Services

**On the EC2 instance:**

```bash
# Install systemd services
cd ocpctl
sudo make install-services

# Enable services to start on boot
sudo systemctl enable ocpctl-api ocpctl-worker

# Start services
sudo systemctl start ocpctl-api ocpctl-worker

# Check status
sudo systemctl status ocpctl-api ocpctl-worker
```

**If API fails to start, check logs:**
```bash
sudo journalctl -u ocpctl-api -n 50
# Common errors:
# - "CRITICAL: JWT_SECRET must be set" → Add JWT_SECRET to /etc/ocpctl/api.env
# - "JWT_SECRET must be at least 32 characters" → Generate new secret with openssl rand -base64 32
# - Database connection errors → Check DATABASE_URL and database accessibility
```

### Step 9: Verify Deployment

**Health check:**
```bash
# On EC2
curl http://localhost:8080/health

# From your machine (if security group allows)
curl http://<ec2-public-ip>:8080/health
```

**Check logs:**
```bash
# View all logs
sudo journalctl -u ocpctl-api -u ocpctl-worker -f

# API logs only
sudo journalctl -u ocpctl-api -f

# Worker logs only
sudo journalctl -u ocpctl-worker -f
```

**Test cluster creation:**
```bash
# Create a test cluster
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
```

## Ongoing Operations

### Updating Code

**From your local machine:**
```bash
# Build and deploy new version
make deploy

# Restart services
ssh ubuntu@<ec2-public-ip> 'sudo systemctl restart ocpctl-api ocpctl-worker'
```

### Monitoring

```bash
# Check service status
sudo systemctl status ocpctl-api ocpctl-worker

# View logs
sudo make logs          # All logs
sudo make logs-api      # API only
sudo make logs-worker   # Worker only

# Check cluster status
curl http://localhost:8080/api/v1/clusters | jq

# Check pending jobs
curl http://localhost:8080/api/v1/jobs?status=PENDING | jq
```

### Database Maintenance

```bash
# Backup database
pg_dump ocpctl > backup-$(date +%Y%m%d).sql

# Check database size
psql ocpctl -c "SELECT pg_size_pretty(pg_database_size('ocpctl'));"

# List clusters
psql ocpctl -c "SELECT id, name, status, created_at FROM clusters ORDER BY created_at DESC LIMIT 10;"
```

### Cleanup Old Clusters

The janitor automatically cleans up expired clusters. To manually check:

```bash
# List expired clusters
psql ocpctl -c "SELECT id, name, status, destroy_at FROM clusters WHERE destroy_at < NOW() AND status != 'DESTROYED';"

# The janitor runs every 5 minutes and will create destroy jobs automatically
```

## Troubleshooting

### Services won't start

```bash
# Check service logs
sudo journalctl -u ocpctl-api -n 50
sudo journalctl -u ocpctl-worker -n 50

# Check environment files
sudo cat /etc/ocpctl/api.env
sudo cat /etc/ocpctl/worker.env

# Test database connection
psql "$DATABASE_URL" -c '\dt'
```

### Worker not processing jobs

```bash
# Check worker logs
sudo journalctl -u ocpctl-worker -f

# Check for pending jobs
psql ocpctl -c "SELECT id, job_type, status, attempt, max_attempts FROM jobs WHERE status='PENDING';"

# Check for locks
psql ocpctl -c "SELECT * FROM job_locks;"

# Restart worker
sudo systemctl restart ocpctl-worker
```

### OpenShift install failing

```bash
# Check install logs in work directory
sudo tail -f /var/lib/ocpctl/clusters/<cluster-id>/.openshift_install.log

# Check AWS credentials
aws sts get-caller-identity

# Check IAM permissions
aws iam get-instance-profile --instance-profile-name ocpctl-instance-role
```

### High disk usage

```bash
# Check disk usage
df -h

# Find large directories
du -sh /var/lib/ocpctl/clusters/*

# Clean up destroyed clusters (worker does this automatically)
sudo find /var/lib/ocpctl/clusters -type d -name "*" -exec du -sh {} \; | sort -h
```

## Security Considerations

1. **Network Security**
   - Restrict API access via Security Group (don't expose to 0.0.0.0/0)
   - Use VPN or bastion host for access
   - Consider placing behind ALB with SSL

2. **Secrets Management**
   - Environment files are mode 600, owned by ocpctl user
   - Consider using AWS Secrets Manager for pull secret
   - Rotate database password regularly

3. **IAM Permissions**
   - Instance role has broad AWS permissions for cluster provisioning
   - Review and limit permissions based on your needs
   - Enable CloudTrail to audit API calls

4. **Updates**
   - Keep system packages updated: `sudo apt-get update && sudo apt-get upgrade`
   - Update openshift-install binary regularly
   - Monitor for Go security updates

## Cost Optimization

1. **Instance Sizing**
   - Start with t3.medium, monitor CPU/memory usage
   - Scale up if worker processes are slow
   - Consider spot instances for non-production

2. **Database**
   - db.t3.micro is sufficient for small deployments
   - Enable automated backups
   - Use Multi-AZ only if needed for production

3. **Cluster Cleanup**
   - Set appropriate TTLs (default 4-8 hours)
   - Janitor automatically destroys expired clusters
   - Monitor for stuck clusters

## Backup and Recovery

### Backup Database

```bash
# Manual backup
pg_dump -h <rds-endpoint> -U ocpctl ocpctl > backup.sql

# Automated backup (cron)
0 2 * * * pg_dump ocpctl > /backup/ocpctl-$(date +\%Y\%m\%d).sql
```

### Restore Database

```bash
psql -h <rds-endpoint> -U ocpctl ocpctl < backup.sql
```

### Backup Configuration

```bash
# Backup environment and service files
sudo tar czf ocpctl-config-backup.tar.gz \
  /etc/ocpctl/ \
  /etc/systemd/system/ocpctl-*.service
```

## Next Steps

- **Phase 2**: Add authentication (JWT, API keys, RBAC)
- **Phase 4**: Build CLI client for easier management
- **Monitoring**: Add Prometheus metrics and Grafana dashboards
- **HA Setup**: Run multiple API servers behind load balancer
- **S3 Storage**: Move artifacts from local disk to S3
