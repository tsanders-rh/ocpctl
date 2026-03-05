# OCPCTL AWS Deployment - Quick Start

This is a condensed deployment guide. See [DEPLOYMENT.md](DEPLOYMENT.md) for detailed instructions.

## Prerequisites

- AWS account with appropriate permissions
- SSH key pair for EC2 access
- OpenShift pull secret from Red Hat

## 1. Launch EC2 Instance

**Disk Sizing Guide:**
- **Small deployments (1-10 clusters):** 30GB root volume
- **Medium deployments (10-25 clusters):** 50GB root volume
- **Large deployments (25-50 clusters):** 100GB root volume

Each cluster work directory uses ~50-250MB depending on install time and failures.
The janitor automatically cleans up DESTROYED clusters after 30 days and FAILED directories after 7 days.

```bash
# Launch t3.medium with Ubuntu 22.04
# Adjust VolumeSize based on expected cluster count (50GB shown for 10-25 clusters)
aws ec2 run-instances \
  --image-id ami-0c55b159cbfafe1f0 \
  --instance-type t3.medium \
  --key-name your-key \
  --security-group-ids sg-xxxxx \
  --iam-instance-profile Name=ocpctl-role \
  --block-device-mappings 'DeviceName=/dev/sda1,Ebs={VolumeSize=50}'

# For 50 active clusters, use VolumeSize=100
# For 100+ clusters, consider dedicated EBS volume for /var/lib/ocpctl

# Note the instance ID and public IP
```

## 2. Setup PostgreSQL

**Option A: RDS**
```bash
aws rds create-db-instance \
  --db-instance-identifier ocpctl-db \
  --db-instance-class db.t3.micro \
  --engine postgres \
  --master-username ocpctl \
  --master-user-password 'CHANGE_ME' \
  --allocated-storage 20 \
  --db-name ocpctl
```

**Option B: On EC2**
```bash
ssh -i key.pem ubuntu@<ip>
sudo apt-get update && sudo apt-get install -y postgresql
sudo -u postgres psql << 'EOF'
CREATE DATABASE ocpctl;
CREATE USER ocpctl WITH PASSWORD 'CHANGE_ME';
GRANT ALL PRIVILEGES ON DATABASE ocpctl TO ocpctl;
EOF
```

## 3. Setup Server

```bash
# SSH to EC2
ssh -i key.pem ubuntu@<ip>

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

## 4. Deploy Application

**From your local machine:**

```bash
# Set deployment host
export DEPLOY_HOST=ubuntu@<ec2-ip>

# Build and deploy
make build-linux
make deploy
```

## 5. Configure

**On EC2 instance:**

```bash
# Create API config
sudo bash -c 'cat > /etc/ocpctl/api.env' << 'EOF'
DATABASE_URL=postgres://ocpctl:PASSWORD@localhost:5432/ocpctl?sslmode=disable
PORT=8080
PROFILES_DIR=/opt/ocpctl/profiles
ENVIRONMENT=production
EOF

# Upload pull secret first
# scp pull-secret.json ubuntu@<ec2-ip>:~/

# Create worker config
sudo bash -c 'cat > /etc/ocpctl/worker.env' << EOF
DATABASE_URL=postgres://ocpctl:PASSWORD@localhost:5432/ocpctl?sslmode=disable
WORKER_WORK_DIR=/var/lib/ocpctl/clusters
OPENSHIFT_PULL_SECRET=$(cat ~/pull-secret.json)
OPENSHIFT_INSTALL_BINARY=/usr/local/bin/openshift-install
AWS_REGION=us-east-1
ENVIRONMENT=production
EOF

# Secure configs
sudo chmod 600 /etc/ocpctl/*.env
sudo chown ocpctl:ocpctl /etc/ocpctl/*.env
rm ~/pull-secret.json
```

## 6. Start Services

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

## 7. Verify

```bash
# Health check
curl http://localhost:8080/health

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

# Check for clusters in DB but not in AWS (manually destroyed)
psql ocpctl -c "SELECT id, name, status, created_at FROM clusters WHERE status IN ('READY', 'CREATING') ORDER BY created_at DESC;"
# Then manually verify these clusters exist in AWS Console

# Check janitor logs for orphaned resources
sudo journalctl -u ocpctl-worker -g "orphaned" -n 50
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

## Security Checklist

- [ ] Security Group restricts API access
- [ ] Environment files are mode 600
- [ ] Database password is strong
- [ ] IAM instance role has minimum required permissions
- [ ] Pull secret file removed from home directory
- [ ] System packages are updated
- [ ] Backups configured

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
