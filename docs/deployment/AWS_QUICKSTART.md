# AWS Test Instance Deployment - Quick Start Guide

This guide will walk you through deploying ocpctl to a test EC2 instance in AWS from scratch.

**Estimated Time:** 30-40 minutes (EC2 PostgreSQL) or 45-60 minutes (RDS PostgreSQL)

## Overview

We'll deploy:
- 1x EC2 instance (t3.medium recommended)
- PostgreSQL database (on EC2 or RDS - **EC2 recommended for testing**)
- Security Groups for network isolation
- ocpctl API server, Worker, and Web frontend

## Database Options

**Choose your database deployment:**

### Option A: PostgreSQL on EC2 (Recommended for Testing) ⚡

**Use this if:**
- Testing and validating ocpctl functionality
- Want fastest deployment (30-40 minutes)
- Lower cost (~$35/month vs ~$50/month)
- Don't need automated backups or high availability

**Pros:**
- ✅ Faster setup (2-3 minutes vs 10-15 minutes)
- ✅ Simpler architecture (no VPC/subnet complexity)
- ✅ Lower cost (saves $15/month)
- ✅ Easy to migrate to RDS later

**Cons:**
- ❌ No automated backups
- ❌ No multi-AZ failover
- ❌ Manual PostgreSQL management

### Option B: RDS PostgreSQL (Production-like)

**Use this if:**
- Need production-like testing environment
- Want automated backups and point-in-time recovery
- Plan to run this long-term (6+ months)
- Need to separate database from application tier

**Pros:**
- ✅ Automated backups (7 days retention)
- ✅ AWS-managed updates and patches
- ✅ Multi-AZ and read replica support
- ✅ Professional-grade reliability

**Cons:**
- ❌ Additional cost (+$15/month)
- ❌ Longer setup time
- ❌ More complex networking setup

---

**This guide uses Option A (PostgreSQL on EC2) as the default path.**
For RDS instructions, see [Appendix: RDS PostgreSQL Setup](#appendix-rds-postgresql-setup) at the end.

---

## Prerequisites

### Local Machine
- [ ] AWS CLI configured (`aws configure`)
- [ ] SSH key pair for EC2 access
- [ ] Git installed
- [ ] Your OpenShift pull secret from console.redhat.com

### AWS Account
- [ ] AWS account with appropriate permissions
- [ ] VPC with public subnet (or use default VPC)
- [ ] AWS region selected (e.g., us-east-1)

---

## Part 1: Infrastructure Setup (5-10 minutes)

### Step 1: Set AWS Variables and Create EC2 Security Group

```bash
# Set variables
export AWS_REGION=us-east-1

# Get your default VPC ID (or specify your VPC)
export VPC_ID=$(aws ec2 describe-vpcs \
  --filters "Name=is-default,Values=true" \
  --query 'Vpcs[0].VpcId' \
  --output text \
  --region $AWS_REGION)

echo "Using VPC: $VPC_ID"

# Create security group for EC2 instance
aws ec2 create-security-group \
  --group-name ocpctl-app-sg \
  --description "ocpctl application server" \
  --vpc-id $VPC_ID \
  --region $AWS_REGION

export APP_SG_ID=$(aws ec2 describe-security-groups \
  --filters "Name=group-name,Values=ocpctl-app-sg" \
  --query 'SecurityGroups[0].GroupId' \
  --output text \
  --region $AWS_REGION)

echo "Security Group ID: $APP_SG_ID"

# Allow HTTP (80)
aws ec2 authorize-security-group-ingress \
  --group-id $APP_SG_ID \
  --protocol tcp \
  --port 80 \
  --cidr 0.0.0.0/0 \
  --region $AWS_REGION

# Allow HTTPS (443)
aws ec2 authorize-security-group-ingress \
  --group-id $APP_SG_ID \
  --protocol tcp \
  --port 443 \
  --cidr 0.0.0.0/0 \
  --region $AWS_REGION

# Allow SSH (22) - restrict to your IP
export MY_IP=$(curl -s https://checkip.amazonaws.com)
aws ec2 authorize-security-group-ingress \
  --group-id $APP_SG_ID \
  --protocol tcp \
  --port 22 \
  --cidr $MY_IP/32 \
  --region $AWS_REGION

echo "Security group configured successfully"
```

### Step 2: Launch EC2 Instance

```bash
# Get a public subnet from the VPC
export SUBNET_ID=$(aws ec2 describe-subnets \
  --filters "Name=vpc-id,Values=$VPC_ID" "Name=default-for-az,Values=true" \
  --query 'Subnets[0].SubnetId' \
  --output text \
  --region $AWS_REGION)

echo "Using subnet: $SUBNET_ID"

# Find latest Amazon Linux 2023 AMI
export AMI_ID=$(aws ec2 describe-images \
  --owners amazon \
  --filters "Name=name,Values=al2023-ami-2023.*-x86_64" \
  --query 'Images | sort_by(@, &CreationDate) | [-1].ImageId' \
  --output text \
  --region $AWS_REGION)

echo "Using AMI: $AMI_ID"

# Launch instance (without IAM role for now - we'll add it later if needed)
aws ec2 run-instances \
  --image-id $AMI_ID \
  --instance-type t3.medium \
  --key-name your-key-pair-name \
  --security-group-ids $APP_SG_ID \
  --subnet-id $SUBNET_ID \
  --block-device-mappings '[{"DeviceName":"/dev/xvda","Ebs":{"VolumeSize":30,"VolumeType":"gp3"}}]' \
  --tag-specifications 'ResourceType=instance,Tags=[{Key=Name,Value=ocpctl-test}]' \
  --region $AWS_REGION

# Wait a moment for instance to register
sleep 5

# Get instance ID
export INSTANCE_ID=$(aws ec2 describe-instances \
  --filters "Name=tag:Name,Values=ocpctl-test" "Name=instance-state-name,Values=pending,running" \
  --query 'Reservations[0].Instances[0].InstanceId' \
  --output text \
  --region $AWS_REGION)

echo "Instance ID: $INSTANCE_ID"

# Wait for instance to be running
echo "Waiting for instance to be running (this takes ~60 seconds)..."
aws ec2 wait instance-running \
  --instance-ids $INSTANCE_ID \
  --region $AWS_REGION

# Get public IP
export EC2_IP=$(aws ec2 describe-instances \
  --instance-ids $INSTANCE_ID \
  --query 'Reservations[0].Instances[0].PublicIpAddress' \
  --output text \
  --region $AWS_REGION)

echo "EC2 Instance IP: $EC2_IP"
echo "SSH command: ssh -i ~/.ssh/your-key.pem ec2-user@$EC2_IP"
echo ""
echo "IMPORTANT: Save these values for later steps"
echo "export EC2_IP=$EC2_IP"
echo "export INSTANCE_ID=$INSTANCE_ID"
```

**IAM Instance Profile (if using IAM auth):**

Create an IAM role for the EC2 instance:

```bash
# Create trust policy
cat > /tmp/trust-policy.json <<EOF
{
  "Version": "2012-10-17",
  "Statement": [{
    "Effect": "Allow",
    "Principal": {"Service": "ec2.amazonaws.com"},
    "Action": "sts:AssumeRole"
  }]
}
EOF

# Create role
aws iam create-role \
  --role-name ocpctl-ec2-role \
  --assume-role-policy-document file:///tmp/trust-policy.json

# Attach policies for S3 access (for kubeconfigs)
cat > /tmp/s3-policy.json <<EOF
{
  "Version": "2012-10-17",
  "Statement": [{
    "Effect": "Allow",
    "Action": [
      "s3:GetObject",
      "s3:PutObject",
      "s3:DeleteObject"
    ],
    "Resource": "arn:aws:s3:::your-ocpctl-bucket/*"
  }]
}
EOF

aws iam put-role-policy \
  --role-name ocpctl-ec2-role \
  --policy-name ocpctl-s3-access \
  --policy-document file:///tmp/s3-policy.json

# Create instance profile
aws iam create-instance-profile \
  --instance-profile-name ocpctl-ec2-role

aws iam add-role-to-instance-profile \
  --instance-profile-name ocpctl-ec2-role \
  --role-name ocpctl-ec2-role
```

---

## Part 2: Server Setup (10-15 minutes)

### Step 3: Connect and Install Dependencies

```bash
# SSH into instance
ssh -i ~/.ssh/your-key.pem ec2-user@$EC2_IP

# Update system
sudo dnf update -y

# Install PostgreSQL 15 server and contrib package
sudo dnf install -y postgresql15-server postgresql15 postgresql15-contrib

# Install Node.js 18
curl -fsSL https://rpm.nodesource.com/setup_18.x | sudo bash -
sudo dnf install -y nodejs

# Install nginx
sudo dnf install -y nginx

# Install git
sudo dnf install -y git

# Verify installations
node --version
npm --version
psql --version
nginx -v
```

### Step 4: Set Up PostgreSQL Database

```bash
# Initialize PostgreSQL database
sudo postgresql-setup --initdb

# Start and enable PostgreSQL
sudo systemctl enable postgresql
sudo systemctl start postgresql

# Check status
sudo systemctl status postgresql

# Create database and user
sudo -u postgres psql <<EOF
CREATE USER ocpctl WITH PASSWORD 'changeme-generate-secure-password';
CREATE DATABASE ocpctl OWNER ocpctl;
GRANT ALL PRIVILEGES ON DATABASE ocpctl TO ocpctl;
\q
EOF

# Enable uuid-ossp extension (required for migrations)
sudo -u postgres psql -d ocpctl -c 'CREATE EXTENSION IF NOT EXISTS "uuid-ossp";'

# Test connection
psql "postgres://ocpctl:changeme-generate-secure-password@localhost:5432/ocpctl" -c "SELECT version();"

# Set DATABASE_URL for later use
export DATABASE_URL="postgres://ocpctl:changeme-generate-secure-password@localhost:5432/ocpctl?sslmode=disable"
echo "export DATABASE_URL='$DATABASE_URL'" >> ~/.bashrc

echo "PostgreSQL configured successfully"
echo "IMPORTANT: Save your database password securely!"
```

**Note:** For production, generate a secure password with `openssl rand -base64 32` instead of using "changeme-generate-secure-password".

---

## Part 3: Deploy Application (15-20 minutes)

### Step 5: Create Application User and Deploy Binaries

```bash
# Create ocpctl user
sudo useradd -r -s /bin/bash -d /opt/ocpctl ocpctl

# Create directories
sudo mkdir -p /opt/ocpctl/{bin,profiles,web}
sudo chown -R ocpctl:ocpctl /opt/ocpctl

# From your LOCAL machine, build and copy binaries
# (Run these commands on your local machine)
```

**On your local machine:**

```bash
# Build binaries for Linux (use cross-compilation if building on Mac/Windows)
cd /path/to/ocpctl

# For Linux build machine:
go build -o bin/api ./cmd/api
go build -o bin/worker ./cmd/worker

# For Mac/Windows build machine (cross-compile for Linux):
GOOS=linux GOARCH=amd64 go build -o bin/api ./cmd/api
GOOS=linux GOARCH=amd64 go build -o bin/worker ./cmd/worker

# Verify binaries are Linux ELF format (should show "ELF 64-bit LSB executable")
file bin/api
file bin/worker

# Build web frontend with correct API URL
# IMPORTANT: NEXT_PUBLIC_* variables are embedded at build time
cd web
npm install
NEXT_PUBLIC_API_URL=http://$EC2_IP/api/v1 npm run build

# Copy to server
scp -i ~/.ssh/your-key.pem bin/api ec2-user@$EC2_IP:/tmp/
scp -i ~/.ssh/your-key.pem bin/worker ec2-user@$EC2_IP:/tmp/
scp -i ~/.ssh/your-key.pem -r deploy/ ec2-user@$EC2_IP:/tmp/
scp -i ~/.ssh/your-key.pem -r internal/profile/definitions/ ec2-user@$EC2_IP:/tmp/profiles/

# Copy web build
rsync -avz -e "ssh -i ~/.ssh/your-key.pem" \
  --exclude node_modules \
  --exclude .next/cache \
  web/ ec2-user@$EC2_IP:/tmp/web/
```

**Back on EC2 instance:**

```bash
# Move binaries to correct location with proper names
sudo mv /tmp/api /opt/ocpctl/bin/ocpctl-api
sudo mv /tmp/worker /opt/ocpctl/bin/ocpctl-worker
sudo chmod +x /opt/ocpctl/bin/*

# Move profiles
sudo mv /tmp/profiles/* /opt/ocpctl/profiles/

# Move web app
sudo mv /tmp/web/* /opt/ocpctl/web/

# Set ownership
sudo chown -R ocpctl:ocpctl /opt/ocpctl

# Install web dependencies
cd /opt/ocpctl/web
sudo -u ocpctl npm install --production
```

### Step 6: Configure Environment Variables

```bash
# Get EC2 public IP (needed for CORS configuration)
export EC2_IP=$(curl -s http://169.254.169.254/latest/meta-data/public-ipv4)
echo "EC2 Public IP: $EC2_IP"

# Generate JWT secret
export JWT_SECRET=$(openssl rand -base64 48)
echo "JWT Secret: $JWT_SECRET" > ~/ocpctl-jwt-secret.txt

# Create API environment file
sudo mkdir -p /etc/ocpctl
sudo tee /etc/ocpctl/api.env > /dev/null <<EOF
# Database
DATABASE_URL=$DATABASE_URL

# API Server
PORT=8080
API_HOST=0.0.0.0
PROFILES_DIR=/opt/ocpctl/profiles

# Authentication
JWT_SECRET=$JWT_SECRET
CORS_ALLOWED_ORIGINS=http://$EC2_IP

# IAM Auth (optional)
ENABLE_IAM_AUTH=false

# IAM Group Membership (optional)
# Restrict IAM auth to users in this group (requires iam:ListGroupsForUser permission)
IAM_ALLOWED_GROUP=

# Environment
ENVIRONMENT=test
LOG_LEVEL=info

# Rate Limiting
RATE_LIMIT_REQUESTS=100
EOF

# Create Worker environment file
sudo tee /etc/ocpctl/worker.env > /dev/null <<EOF
# Database
DATABASE_URL=$DATABASE_URL

# Worker
PROFILES_DIR=/opt/ocpctl/profiles
WORKER_WORK_DIR=/var/lib/ocpctl/clusters
WORKER_HEALTH_PORT=8081

# OpenShift Pull Secret
# IMPORTANT: Add your pull secret here
OPENSHIFT_PULL_SECRET='PASTE_YOUR_PULL_SECRET_HERE'

# Environment
ENVIRONMENT=test
EOF

# Create Web environment file
# NOTE: Web frontend runs in the browser, so it needs the public EC2 IP, not localhost
sudo tee /etc/ocpctl/web.env > /dev/null <<EOF
# API endpoint (uses nginx reverse proxy on port 80)
NEXT_PUBLIC_API_URL=http://$EC2_IP/api/v1

# Auth mode
NEXT_PUBLIC_AUTH_MODE=jwt

# AWS region
NEXT_PUBLIC_AWS_REGION=us-east-1

# Environment
NODE_ENV=production
EOF

# Secure the files
sudo chmod 600 /etc/ocpctl/*.env
sudo chown root:root /etc/ocpctl/*.env
```

**IMPORTANT:** Edit `/etc/ocpctl/worker.env` and add your OpenShift pull secret:

```bash
sudo nano /etc/ocpctl/worker.env
# Paste your pull secret from console.redhat.com
```

### Step 7: Install systemd Services

```bash
# Copy service files from deploy directory
sudo cp /tmp/deploy/systemd/ocpctl-api.service /etc/systemd/system/
sudo cp /tmp/deploy/systemd/ocpctl-worker.service /etc/systemd/system/

# Create web service file
sudo tee /etc/systemd/system/ocpctl-web.service > /dev/null <<'EOF'
[Unit]
Description=ocpctl Web Frontend
After=network.target ocpctl-api.service

[Service]
Type=simple
User=ocpctl
WorkingDirectory=/opt/ocpctl/web
EnvironmentFile=/etc/ocpctl/web.env
ExecStart=/usr/bin/npm start
Restart=on-failure
RestartSec=10

[Install]
WantedBy=multi-user.target
EOF

# Reload systemd
sudo systemctl daemon-reload

# Enable services
sudo systemctl enable ocpctl-api
sudo systemctl enable ocpctl-worker
sudo systemctl enable ocpctl-web
```

### Step 8: Install OpenShift Installer Binary

The worker needs the `openshift-install` binary to provision OpenShift clusters:

```bash
# Download openshift-install (adjust version as needed)
wget https://mirror.openshift.com/pub/openshift-v4/clients/ocp/4.20.3/openshift-install-linux.tar.gz

# Extract to /usr/local/bin
sudo tar -xzf openshift-install-linux.tar.gz -C /usr/local/bin/ openshift-install

# Make executable
sudo chmod +x /usr/local/bin/openshift-install

# Verify installation
openshift-install version

# Clean up
rm openshift-install-linux.tar.gz
```

**Note:** You can find other versions at https://mirror.openshift.com/pub/openshift-v4/clients/ocp/

### Step 9: Install OpenShift CLI (oc) Binary

The worker needs the `oc` binary to extract credential requests from OpenShift release images when deploying clusters in Manual mode (STS):

```bash
# Download oc CLI (use same version as openshift-install)
wget https://mirror.openshift.com/pub/openshift-v4/clients/ocp/4.20.3/openshift-client-linux.tar.gz

# Extract to /usr/local/bin
sudo tar -xzf openshift-client-linux.tar.gz -C /usr/local/bin/ oc kubectl

# Make executable
sudo chmod +x /usr/local/bin/oc /usr/local/bin/kubectl

# Verify installation
oc version --client

# Clean up
rm openshift-client-linux.tar.gz
```

**Note:** The download includes both `oc` and `kubectl` binaries. Both are installed to `/usr/local/bin/`.

### Step 10: Install ccoctl (Cloud Credential Operator) Binary

The worker needs the `ccoctl` binary to provision AWS IAM resources when deploying clusters in Manual mode (STS):

```bash
# Download ccoctl (use same version as openshift-install)
wget https://mirror.openshift.com/pub/openshift-v4/clients/ocp/4.20.3/ccoctl-linux.tar.gz

# Extract to /usr/local/bin
sudo tar -xzf ccoctl-linux.tar.gz -C /usr/local/bin/ ccoctl

# Make executable
sudo chmod +x /usr/local/bin/ccoctl

# Verify installation
ccoctl --help | head -5

# Clean up
rm ccoctl-linux.tar.gz
```

**Note:** ccoctl creates IAM roles and policies for OpenShift cluster components to use AWS STS temporary credentials instead of long-lived access keys.

### Step 11: Run Database Migrations

```bash
# Start API temporarily to run migrations
sudo systemctl start ocpctl-api

# Check logs to verify migrations ran
sudo journalctl -u ocpctl-api -n 50 --no-pager

# You should see migration logs
# If migrations don't run automatically, you may need to run them manually
```

### Step 12: Start All Services

```bash
# Start services
sudo systemctl start ocpctl-api
sudo systemctl start ocpctl-worker
sudo systemctl start ocpctl-web

# Check status
sudo systemctl status ocpctl-api
sudo systemctl status ocpctl-worker
sudo systemctl status ocpctl-web

# Verify health checks
curl http://localhost:8080/health
curl http://localhost:8081/health
curl http://localhost:8081/ready
curl http://localhost:3000
```

### Step 13: Configure Nginx

```bash
# Copy nginx config or create new one
sudo tee /etc/nginx/conf.d/ocpctl.conf > /dev/null <<'EOF'
upstream api {
    server localhost:8080;
}

upstream web {
    server localhost:3000;
}

server {
    listen 80;
    server_name _;

    # Hide nginx version
    server_tokens off;

    # API routes
    location /api/ {
        proxy_pass http://api;
        proxy_http_version 1.1;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection 'upgrade';
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_cache_bypass $http_upgrade;
    }

    # Web frontend
    location / {
        proxy_pass http://web;
        proxy_http_version 1.1;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection 'upgrade';
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_cache_bypass $http_upgrade;
    }
}
EOF

# Test nginx config
sudo nginx -t

# Start nginx
sudo systemctl enable nginx
sudo systemctl start nginx

# Check status
sudo systemctl status nginx
```

---

## Part 4: Verification (5 minutes)

### Step 14: Test the Deployment

```bash
# Check all services are running
sudo systemctl status ocpctl-api ocpctl-worker ocpctl-web nginx

# Test health endpoints
curl http://localhost:8080/health
curl http://localhost:8081/health
curl http://localhost:8081/ready

# Test API through nginx
curl http://localhost/api/v1/health

# Test web frontend
curl http://localhost
```

### Step 15: Access the Web Interface

1. **Open browser:** Navigate to `http://<EC2_IP>`
2. **Login with default credentials:**
   - Email: `admin@localhost`
   - Password: `changeme`
3. **Change admin password immediately** in the admin panel
4. **Test features:**
   - Create a cluster profile
   - View available profiles
   - Create a test cluster (it will provision!)

### Step 16: Monitor Logs

```bash
# API logs
sudo journalctl -u ocpctl-api -f

# Worker logs
sudo journalctl -u ocpctl-worker -f

# Web logs
sudo journalctl -u ocpctl-web -f

# Nginx logs
sudo tail -f /var/log/nginx/access.log
sudo tail -f /var/log/nginx/error.log
```

---

## Troubleshooting

### Services won't start

```bash
# Check environment files
sudo cat /etc/ocpctl/api.env
sudo cat /etc/ocpctl/worker.env
sudo cat /etc/ocpctl/web.env

# Check service logs
sudo journalctl -u ocpctl-api -n 50 --no-pager
sudo journalctl -u ocpctl-worker -n 50 --no-pager
sudo journalctl -u ocpctl-web -n 50 --no-pager

# Verify binaries
ls -la /opt/ocpctl/bin/
/opt/ocpctl/bin/api --version
```

### Database connection issues

```bash
# Test database connection
psql "$DATABASE_URL" -c "SELECT version();"

# Check if PostgreSQL is running
sudo systemctl status postgresql

# Check PostgreSQL logs
sudo journalctl -u postgresql -n 50 --no-pager

# Verify DATABASE_URL in environment files
grep DATABASE_URL /etc/ocpctl/api.env

# Test connection as postgres user
sudo -u postgres psql -c "SELECT version();"
```

### Web build issues

```bash
# Rebuild web frontend
cd /opt/ocpctl/web
sudo -u ocpctl npm install
sudo -u ocpctl npm run build
sudo systemctl restart ocpctl-web
```

### Can't access from browser

```bash
# Check security group allows HTTP
aws ec2 describe-security-groups --group-ids $APP_SG_ID

# Check nginx is running
sudo systemctl status nginx
curl http://localhost

# Check firewall
sudo iptables -L -n
```

---

## Next Steps

### Configure Custom Domain (Optional)

If you want to access ocpctl via a custom domain instead of the EC2 IP address:

```bash
# Get your EC2 instance's public IP
EC2_IP=$(curl -s http://169.254.169.254/latest/meta-data/public-ipv4)
echo "EC2 Public IP: $EC2_IP"

# Create Route53 A record (replace with your hosted zone ID)
# Example: opsctl.dog8code.com -> EC2 IP
HOSTED_ZONE_ID="Z0123456789ABCDEFGHIJ"  # Your dog8code.com hosted zone ID
DOMAIN_NAME="opsctl.dog8code.com"

# Create change batch file
cat > /tmp/route53-change.json <<EOF
{
  "Changes": [{
    "Action": "UPSERT",
    "ResourceRecordSet": {
      "Name": "${DOMAIN_NAME}",
      "Type": "A",
      "TTL": 300,
      "ResourceRecords": [{"Value": "${EC2_IP}"}]
    }
  }]
}
EOF

# Create the DNS record
aws route53 change-resource-record-sets \
  --hosted-zone-id $HOSTED_ZONE_ID \
  --change-batch file:///tmp/route53-change.json

# Verify DNS propagation (may take a few minutes)
dig +short $DOMAIN_NAME
# Should return your EC2 IP

# Update nginx to use the domain name
sudo sed -i 's/server_name _;/server_name opsctl.dog8code.com;/' /etc/nginx/conf.d/ocpctl.conf

# Test nginx config
sudo nginx -t

# Reload nginx
sudo systemctl reload nginx

# Clean up
rm /tmp/route53-change.json
```

**To find your Route53 Hosted Zone ID:**
```bash
# List all hosted zones
aws route53 list-hosted-zones --query 'HostedZones[*].[Name,Id]' --output table

# Find dog8code.com zone
aws route53 list-hosted-zones --query 'HostedZones[?Name==`dog8code.com.`].Id' --output text
```

### Enable HTTPS (Recommended)

Once you have a custom domain configured, enable HTTPS with Let's Encrypt:

```bash
# Install certbot
sudo dnf install -y certbot python3-certbot-nginx

# Get certificate (requires domain name to be already configured in nginx)
sudo certbot --nginx -d opsctl.dog8code.com

# Follow prompts to:
# - Enter email for renewal notifications
# - Agree to terms of service
# - Choose whether to redirect HTTP to HTTPS (recommended: yes)

# Auto-renewal is configured automatically via systemd timer
sudo systemctl status certbot-renew.timer

# Test auto-renewal
sudo certbot renew --dry-run
```

After enabling HTTPS, your ocpctl instance will be accessible at:
- **HTTP:** http://opsctl.dog8code.com (redirects to HTTPS)
- **HTTPS:** https://opsctl.dog8code.com

### Restrict IAM Authentication by Group (Optional)

To limit which IAM users can authenticate, require membership in a specific IAM group:

```bash
# 1. Create IAM group for ocpctl users
aws iam create-group --group-name ocpctl-users

# 2. Add users to the group
aws iam add-user-to-group --user-name alice --group-name ocpctl-users
aws iam add-user-to-group --user-name bob --group-name ocpctl-users

# 3. Grant API server permission to check group membership
# Update the API server's IAM role policy to include iam:ListGroupsForUser
cat > /tmp/iam-group-check-policy.json <<'EOF'
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Action": [
        "iam:ListGroupsForUser"
      ],
      "Resource": "*"
    }
  ]
}
EOF

aws iam put-role-policy \
  --role-name ocpctl-api-role \
  --policy-name IAMGroupChecking \
  --policy-document file:///tmp/iam-group-check-policy.json

# 4. Configure ocpctl API to enforce group membership
# Add to /etc/ocpctl/api.env:
echo "IAM_ALLOWED_GROUP=ocpctl-users" | sudo tee -a /etc/ocpctl/api.env

# 5. Restart API service
sudo systemctl restart ocpctl-api

# 6. Verify in logs
sudo journalctl -u ocpctl-api -f | grep "IAM auth:"
```

**Behavior:**
- Only IAM users in the `ocpctl-users` group can authenticate
- Users not in the group receive HTTP 403 Forbidden
- Assumed roles (e.g., from CI/CD) bypass the group check
- Leave `IAM_ALLOWED_GROUP` empty to allow all IAM users

**Security Note:** Group membership is checked on every login, not just during initial auto-provisioning.

### Set Up S3 Bucket for Kubeconfigs

```bash
# Create S3 bucket
aws s3 mb s3://your-ocpctl-kubeconfigs-bucket

# Update worker to use S3
# Edit /etc/ocpctl/worker.env and add:
# S3_BUCKET=your-ocpctl-kubeconfigs-bucket
```

### Configure Monitoring

```bash
# Install CloudWatch agent
wget https://s3.amazonaws.com/amazoncloudwatch-agent/amazon_linux/amd64/latest/amazon-cloudwatch-agent.rpm
sudo rpm -U ./amazon-cloudwatch-agent.rpm

# Configure CloudWatch to collect logs
# See: https://docs.aws.amazon.com/AmazonCloudWatch/latest/monitoring/Install-CloudWatch-Agent.html
```

### Backup Strategy

```bash
# For PostgreSQL on EC2, set up manual backups
# Create a backup script
sudo tee /opt/ocpctl/backup-db.sh > /dev/null <<'EOF'
#!/bin/bash
BACKUP_DIR="/opt/ocpctl/backups"
TIMESTAMP=$(date +%Y%m%d_%H%M%S)
mkdir -p $BACKUP_DIR

# Backup database
sudo -u postgres pg_dump ocpctl > $BACKUP_DIR/ocpctl_$TIMESTAMP.sql

# Keep only last 7 days of backups
find $BACKUP_DIR -name "ocpctl_*.sql" -mtime +7 -delete

echo "Backup completed: $BACKUP_DIR/ocpctl_$TIMESTAMP.sql"
EOF

sudo chmod +x /opt/ocpctl/backup-db.sh

# Run manually or add to cron
# sudo crontab -e
# Add: 0 2 * * * /opt/ocpctl/backup-db.sh
```

**For production:** Consider using RDS for automated backups. See [Appendix: RDS PostgreSQL Setup](#appendix-rds-postgresql-setup).

---

## Configure Worker IAM Permissions for Cluster Provisioning

The worker service needs AWS permissions to provision OpenShift clusters. The recommended approach is to attach an IAM instance role to your EC2 instance.

### Why IAM Instance Role?

**IAM Instance Role (Recommended):**
- ✅ No credentials to manage or rotate
- ✅ Automatic credential refresh
- ✅ More secure (credentials never stored on disk)
- ✅ Follows AWS best practices
- ✅ Easy to audit with CloudTrail

**Environment Variables (Alternative):**
- ❌ Credentials stored in plaintext config file
- ❌ Must manually rotate credentials
- ❌ Risk of credential leakage
- ❌ More difficult to audit

### Step 1: Create IAM Policy for OpenShift Provisioning

The worker needs comprehensive AWS permissions to provision OpenShift clusters using the IPI (Installer Provisioned Infrastructure) method.

```bash
# Create IAM policy document
cat > /tmp/ocpctl-worker-policy.json <<'EOF'
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Action": [
        "ec2:AllocateAddress",
        "ec2:AssociateAddress",
        "ec2:AssociateDhcpOptions",
        "ec2:AssociateRouteTable",
        "ec2:AttachInternetGateway",
        "ec2:AttachNetworkInterface",
        "ec2:AttachVolume",
        "ec2:AuthorizeSecurityGroupEgress",
        "ec2:AuthorizeSecurityGroupIngress",
        "ec2:CopyImage",
        "ec2:CreateDhcpOptions",
        "ec2:CreateInternetGateway",
        "ec2:CreateNatGateway",
        "ec2:CreateNetworkInterface",
        "ec2:CreateRoute",
        "ec2:CreateRouteTable",
        "ec2:CreateSecurityGroup",
        "ec2:CreateSnapshot",
        "ec2:CreateSubnet",
        "ec2:CreateTags",
        "ec2:CreateVolume",
        "ec2:CreateVpc",
        "ec2:CreateVpcEndpoint",
        "ec2:DeleteDhcpOptions",
        "ec2:DeleteInternetGateway",
        "ec2:DeleteNatGateway",
        "ec2:DeleteNetworkInterface",
        "ec2:DeleteRoute",
        "ec2:DeleteRouteTable",
        "ec2:DeleteSecurityGroup",
        "ec2:DeleteSnapshot",
        "ec2:DeleteSubnet",
        "ec2:DeleteTags",
        "ec2:DeleteVolume",
        "ec2:DeleteVpc",
        "ec2:DeleteVpcEndpoints",
        "ec2:DeregisterImage",
        "ec2:DescribeAccountAttributes",
        "ec2:DescribeAddresses",
        "ec2:DescribeAvailabilityZones",
        "ec2:DescribeDhcpOptions",
        "ec2:DescribeImages",
        "ec2:DescribeInstanceAttribute",
        "ec2:DescribeInstanceCreditSpecifications",
        "ec2:DescribeInstances",
        "ec2:DescribeInstanceTypes",
        "ec2:DescribeInternetGateways",
        "ec2:DescribeKeyPairs",
        "ec2:DescribeNatGateways",
        "ec2:DescribeNetworkAcls",
        "ec2:DescribeNetworkInterfaces",
        "ec2:DescribePrefixLists",
        "ec2:DescribeRegions",
        "ec2:DescribeRouteTables",
        "ec2:DescribeSecurityGroups",
        "ec2:DescribeSnapshots",
        "ec2:DescribeSubnets",
        "ec2:DescribeTags",
        "ec2:DescribeVolumes",
        "ec2:DescribeVpcAttribute",
        "ec2:DescribeVpcClassicLink",
        "ec2:DescribeVpcClassicLinkDnsSupport",
        "ec2:DescribeVpcEndpoints",
        "ec2:DescribeVpcs",
        "ec2:DetachInternetGateway",
        "ec2:DetachNetworkInterface",
        "ec2:DetachVolume",
        "ec2:DisassociateAddress",
        "ec2:DisassociateRouteTable",
        "ec2:GetEbsDefaultKmsKeyId",
        "ec2:ModifyInstanceAttribute",
        "ec2:ModifyNetworkInterfaceAttribute",
        "ec2:ModifySubnetAttribute",
        "ec2:ModifyVpcAttribute",
        "ec2:ReleaseAddress",
        "ec2:RevokeSecurityGroupEgress",
        "ec2:RevokeSecurityGroupIngress",
        "ec2:RunInstances",
        "ec2:TerminateInstances"
      ],
      "Resource": "*"
    },
    {
      "Effect": "Allow",
      "Action": [
        "elasticloadbalancing:AddTags",
        "elasticloadbalancing:ApplySecurityGroupsToLoadBalancer",
        "elasticloadbalancing:AttachLoadBalancerToSubnets",
        "elasticloadbalancing:ConfigureHealthCheck",
        "elasticloadbalancing:CreateListener",
        "elasticloadbalancing:CreateLoadBalancer",
        "elasticloadbalancing:CreateLoadBalancerListeners",
        "elasticloadbalancing:CreateTargetGroup",
        "elasticloadbalancing:DeleteListener",
        "elasticloadbalancing:DeleteLoadBalancer",
        "elasticloadbalancing:DeleteTargetGroup",
        "elasticloadbalancing:DeregisterInstancesFromLoadBalancer",
        "elasticloadbalancing:DeregisterTargets",
        "elasticloadbalancing:DescribeInstanceHealth",
        "elasticloadbalancing:DescribeListeners",
        "elasticloadbalancing:DescribeLoadBalancerAttributes",
        "elasticloadbalancing:DescribeLoadBalancers",
        "elasticloadbalancing:DescribeTags",
        "elasticloadbalancing:DescribeTargetGroupAttributes",
        "elasticloadbalancing:DescribeTargetGroups",
        "elasticloadbalancing:DescribeTargetHealth",
        "elasticloadbalancing:ModifyLoadBalancerAttributes",
        "elasticloadbalancing:ModifyTargetGroup",
        "elasticloadbalancing:ModifyTargetGroupAttributes",
        "elasticloadbalancing:RegisterInstancesWithLoadBalancer",
        "elasticloadbalancing:RegisterTargets",
        "elasticloadbalancing:RemoveTags",
        "elasticloadbalancing:SetLoadBalancerPoliciesOfListener",
        "elasticloadbalancing:SetSecurityGroups",
        "elasticloadbalancing:SetSubnets"
      ],
      "Resource": "*"
    },
    {
      "Effect": "Allow",
      "Action": [
        "iam:AddRoleToInstanceProfile",
        "iam:CreateInstanceProfile",
        "iam:CreateRole",
        "iam:DeleteInstanceProfile",
        "iam:DeleteRole",
        "iam:DeleteRolePolicy",
        "iam:GetInstanceProfile",
        "iam:GetRole",
        "iam:GetRolePolicy",
        "iam:GetUser",
        "iam:ListInstanceProfiles",
        "iam:ListInstanceProfilesForRole",
        "iam:ListRoles",
        "iam:ListUsers",
        "iam:PassRole",
        "iam:PutRolePolicy",
        "iam:RemoveRoleFromInstanceProfile",
        "iam:SimulatePrincipalPolicy",
        "iam:TagRole",
        "iam:TagInstanceProfile",
        "iam:ListRolePolicies",
        "iam:CreateOpenIDConnectProvider",
        "iam:DeleteOpenIDConnectProvider",
        "iam:GetOpenIDConnectProvider",
        "iam:ListOpenIDConnectProviders",
        "iam:TagOpenIDConnectProvider",
        "iam:UpdateOpenIDConnectProviderThumbprint"
      ],
      "Resource": "*"
    },
    {
      "Effect": "Allow",
      "Action": [
        "route53:ChangeResourceRecordSets",
        "route53:ChangeTagsForResource",
        "route53:CreateHostedZone",
        "route53:DeleteHostedZone",
        "route53:GetChange",
        "route53:GetHostedZone",
        "route53:ListHostedZones",
        "route53:ListHostedZonesByName",
        "route53:ListResourceRecordSets",
        "route53:ListTagsForResource",
        "route53:UpdateHostedZoneComment"
      ],
      "Resource": "*"
    },
    {
      "Effect": "Allow",
      "Action": [
        "s3:CreateBucket",
        "s3:DeleteBucket",
        "s3:DeleteObject",
        "s3:GetAccelerateConfiguration",
        "s3:GetBucketAcl",
        "s3:GetBucketCors",
        "s3:GetBucketLocation",
        "s3:GetBucketLogging",
        "s3:GetBucketObjectLockConfiguration",
        "s3:GetBucketPolicy",
        "s3:GetBucketRequestPayment",
        "s3:GetBucketTagging",
        "s3:GetBucketVersioning",
        "s3:GetBucketWebsite",
        "s3:GetEncryptionConfiguration",
        "s3:GetLifecycleConfiguration",
        "s3:GetObject",
        "s3:GetObjectAcl",
        "s3:GetObjectTagging",
        "s3:GetObjectVersion",
        "s3:GetReplicationConfiguration",
        "s3:ListBucket",
        "s3:ListBucketVersions",
        "s3:PutBucketAcl",
        "s3:PutBucketPolicy",
        "s3:PutBucketPublicAccessBlock",
        "s3:GetBucketPublicAccessBlock",
        "s3:PutBucketTagging",
        "s3:PutBucketVersioning",
        "s3:PutEncryptionConfiguration",
        "s3:PutLifecycleConfiguration",
        "s3:PutObject",
        "s3:PutObjectAcl",
        "s3:PutObjectTagging"
      ],
      "Resource": "*"
    },
    {
      "Effect": "Allow",
      "Action": [
        "elasticfilesystem:CreateFileSystem",
        "elasticfilesystem:CreateMountTarget",
        "elasticfilesystem:CreateTags",
        "elasticfilesystem:DeleteFileSystem",
        "elasticfilesystem:DeleteMountTarget",
        "elasticfilesystem:DescribeFileSystems",
        "elasticfilesystem:DescribeMountTargets",
        "elasticfilesystem:DescribeMountTargetSecurityGroups"
      ],
      "Resource": "*"
    },
    {
      "Effect": "Allow",
      "Action": [
        "servicequotas:GetServiceQuota",
        "servicequotas:ListServiceQuotas",
        "servicequotas:ListAWSDefaultServiceQuotas"
      ],
      "Resource": "*"
    },
    {
      "Effect": "Allow",
      "Action": [
        "tag:GetResources",
        "tag:GetTagKeys",
        "tag:GetTagValues",
        "tag:TagResources",
        "tag:UntagResources"
      ],
      "Resource": "*"
    }
  ]
}
EOF

# Create the IAM policy
aws iam create-policy \
  --policy-name ocpctl-worker-openshift-provisioning \
  --policy-document file:///tmp/ocpctl-worker-policy.json \
  --description "Permissions for ocpctl worker to provision OpenShift clusters on AWS"

# Save policy ARN
export WORKER_POLICY_ARN=$(aws iam list-policies \
  --query 'Policies[?PolicyName==`ocpctl-worker-openshift-provisioning`].Arn' \
  --output text)

echo "Policy ARN: $WORKER_POLICY_ARN"
```

**Key Permissions Explained:**

This comprehensive policy includes permissions for:

- **EC2**: Full cluster infrastructure management (VPC, subnets, instances, volumes, security groups, etc.)
- **ELB**: Load balancer creation and management for cluster ingress
- **IAM**:
  - Role and instance profile management for cluster nodes
  - **OpenID Connect (OIDC) Provider** management for STS token authentication:
    - `CreateOpenIDConnectProvider`, `DeleteOpenIDConnectProvider`, `GetOpenIDConnectProvider`
    - `ListOpenIDConnectProviders`, `TagOpenIDConnectProvider`
    - `UpdateOpenIDConnectProviderThumbprint` (required by ocpctl to fix OIDC thumbprint after creation)
- **Route53**: DNS zone and record management for cluster domains
- **S3**:
  - Bootstrap ignition and OIDC discovery document storage
  - **Public Access Block** management for OIDC bucket configuration:
    - `PutBucketPublicAccessBlock`, `GetBucketPublicAccessBlock`
- **EFS (Elastic File System)**: Shared file storage (RWX) configuration for clusters
  - File system and mount target creation/deletion
  - Required when "Enable EFS storage" option is selected during cluster creation
  - Enables ReadWriteMany (RWX) persistent volumes for multi-pod access
- **Service Quotas**: Checking AWS limits before provisioning
- **Resource Tagging**: Cluster resource tagging for identification

**Note:** These permissions are required for both standard IPI deployments and Manual mode (STS) deployments. The OIDC and S3 public access permissions are specifically needed for Manual mode where `ccoctl` creates an OIDC provider for service account token verification.

### Step 2: Create IAM Role and Instance Profile

```bash
# Create trust policy for EC2
cat > /tmp/trust-policy.json <<'EOF'
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Principal": {
        "Service": "ec2.amazonaws.com"
      },
      "Action": "sts:AssumeRole"
    }
  ]
}
EOF

# Create IAM role
aws iam create-role \
  --role-name ocpctl-worker-role \
  --assume-role-policy-document file:///tmp/trust-policy.json \
  --description "IAM role for ocpctl worker service"

# Attach the OpenShift provisioning policy
aws iam attach-role-policy \
  --role-name ocpctl-worker-role \
  --policy-arn $WORKER_POLICY_ARN

# Create instance profile
aws iam create-instance-profile \
  --instance-profile-name ocpctl-worker-role

# Add role to instance profile (there's a brief delay after creation)
sleep 10
aws iam add-role-to-instance-profile \
  --instance-profile-name ocpctl-worker-role \
  --role-name ocpctl-worker-role

echo "IAM role and instance profile created successfully"
```

### Step 3: Attach Instance Profile to EC2 Instance

```bash
# If you haven't already, get your instance ID
export INSTANCE_ID=$(aws ec2 describe-instances \
  --filters "Name=tag:Name,Values=ocpctl-test" "Name=instance-state-name,Values=running" \
  --query 'Reservations[0].Instances[0].InstanceId' \
  --output text \
  --region $AWS_REGION)

echo "Instance ID: $INSTANCE_ID"

# Attach instance profile to EC2 instance
aws ec2 associate-iam-instance-profile \
  --instance-id $INSTANCE_ID \
  --iam-instance-profile Name=ocpctl-worker-role \
  --region $AWS_REGION

echo "Instance profile attached successfully"
```

### Step 4: Verify IAM Role on EC2 Instance

```bash
# SSH into instance
ssh -i ~/.ssh/your-key.pem ec2-user@$EC2_IP

# Verify AWS credentials are available via instance metadata
aws sts get-caller-identity

# You should see output like:
# {
#     "UserId": "AROAXXXXXXXXXXXXXXXXX:i-0123456789abcdef0",
#     "Account": "123456789012",
#     "Arn": "arn:aws:sts::123456789012:assumed-role/ocpctl-worker-role/i-0123456789abcdef0"
# }

# Test EC2 permissions
aws ec2 describe-regions --region us-east-1

# Test Route53 permissions (use your actual hosted zone ID)
aws route53 list-hosted-zones

# Exit SSH session
exit
```

**Expected output:** If the role is properly attached, you should see your AWS account information and be able to list regions and Route53 hosted zones.

### Step 5: Remove AWS Credentials from Worker Config (If Present)

Since we're using IAM instance role, we should remove any AWS credentials from the worker environment file:

```bash
# SSH into instance
ssh -i ~/.ssh/your-key.pem ec2-user@$EC2_IP

# Edit worker environment file
sudo nano /etc/ocpctl/worker.env

# Remove or comment out these lines if present:
# AWS_ACCESS_KEY_ID=...
# AWS_SECRET_ACCESS_KEY=...
# AWS_REGION is OK to keep, but optional (worker can auto-detect)

# Save and exit (Ctrl+X, Y, Enter)
```

### Step 6: Restart Worker Service

```bash
# Restart worker to pick up IAM credentials
sudo systemctl restart ocpctl-worker

# Check status
sudo systemctl status ocpctl-worker

# Verify worker can access AWS APIs
sudo journalctl -u ocpctl-worker -n 50 --no-pager

# Check worker health
curl http://localhost:8081/health
curl http://localhost:8081/ready

# Exit SSH session
exit
```

### Step 7: Test Cluster Creation (Optional)

Now that the worker has AWS permissions, you can test cluster creation:

1. **Login to web interface:** Navigate to `http://<EC2_IP>`
2. **Navigate to Clusters** and click "Create Cluster"
3. **Select profile:** Choose "AWS Standard Development Cluster" or "AWS Minimal Test Cluster"
4. **Configure cluster:**
   - Name: `test-cluster-001`
   - Region: `us-east-1`
   - Base domain: `mg.dog8code.com` (should be pre-selected)
   - OpenShift version: `4.20.3` (or latest from profile)
5. **Click Create**
6. **Monitor progress:** Watch the cluster status change from "Pending" → "Creating" → "Ready"

**Monitor worker logs during cluster creation:**

```bash
ssh -i ~/.ssh/your-key.pem ec2-user@$EC2_IP
sudo journalctl -u ocpctl-worker -f
```

**Important:** OpenShift cluster creation takes 30-45 minutes. The worker will:
1. Download openshift-install binary
2. Generate install-config.yaml
3. Run `openshift-install create cluster`
4. Create AWS resources (VPC, subnets, security groups, EC2 instances, load balancers)
5. Create Route53 DNS records in your `mg.dog8code.com` hosted zone
6. Wait for cluster bootstrap to complete
7. Store kubeconfig and cluster details in database

### Updating Existing IAM Policy for EFS Support

If you already have ocpctl deployed and want to add EFS storage support, you need to update your existing IAM policy to include EFS permissions.

**Option 1: Update the Managed Policy (Recommended)**

If you used a managed policy (`ocpctl-worker-openshift-provisioning`), create a new policy version:

```bash
# Create updated policy document with EFS permissions
cat > /tmp/ocpctl-worker-policy-v2.json <<'EOF'
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Action": [
        "elasticfilesystem:CreateFileSystem",
        "elasticfilesystem:CreateMountTarget",
        "elasticfilesystem:CreateTags",
        "elasticfilesystem:DeleteFileSystem",
        "elasticfilesystem:DeleteMountTarget",
        "elasticfilesystem:DescribeFileSystems",
        "elasticfilesystem:DescribeMountTargets",
        "elasticfilesystem:DescribeMountTargetSecurityGroups"
      ],
      "Resource": "*"
    }
  ]
}
EOF

# Get your existing policy ARN
export POLICY_ARN=$(aws iam list-policies \
  --query 'Policies[?PolicyName==`ocpctl-worker-openshift-provisioning`].Arn' \
  --output text)

# Create a new policy version (this will add to existing permissions)
aws iam create-policy-version \
  --policy-arn $POLICY_ARN \
  --policy-document file:///tmp/ocpctl-worker-policy-v2.json \
  --set-as-default

echo "EFS permissions added to existing policy"
```

**Option 2: Add Inline Policy to Role**

Alternatively, you can add EFS permissions as an inline policy to your worker role:

```bash
# Create EFS policy
cat > /tmp/efs-policy.json <<'EOF'
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Action": [
        "elasticfilesystem:CreateFileSystem",
        "elasticfilesystem:CreateMountTarget",
        "elasticfilesystem:CreateTags",
        "elasticfilesystem:DeleteFileSystem",
        "elasticfilesystem:DeleteMountTarget",
        "elasticfilesystem:DescribeFileSystems",
        "elasticfilesystem:DescribeMountTargets",
        "elasticfilesystem:DescribeMountTargetSecurityGroups"
      ],
      "Resource": "*"
    }
  ]
}
EOF

# Add inline policy to worker role
aws iam put-role-policy \
  --role-name ocpctl-worker-role \
  --policy-name EFSStorageSupport \
  --policy-document file:///tmp/efs-policy.json

echo "EFS inline policy added to ocpctl-worker-role"
```

**Verify Permissions:**

```bash
# SSH into your instance
ssh -i ~/.ssh/your-key.pem ec2-user@$EC2_IP

# Test EFS permissions
aws efs describe-file-systems --region us-east-1

# Should return empty list (or existing EFS) without permission errors
# Exit SSH session
exit
```

No restart is required - IAM permissions take effect immediately. You can now enable EFS storage when creating clusters.

### Alternative: Using AWS Access Keys (Not Recommended)

If you prefer to use AWS access keys instead of an IAM instance role:

```bash
# SSH into instance
ssh -i ~/.ssh/your-key.pem ec2-user@$EC2_IP

# Edit worker environment file
sudo nano /etc/ocpctl/worker.env

# Add these lines (use your actual credentials):
AWS_ACCESS_KEY_ID=AKIA...
AWS_SECRET_ACCESS_KEY=...
AWS_REGION=us-east-1

# Save and exit

# Restart worker
sudo systemctl restart ocpctl-worker

# Exit
exit
```

**Security Warning:** This approach stores credentials in plaintext in `/etc/ocpctl/worker.env`. Use IAM instance roles instead for production deployments.

### Troubleshooting IAM Role Issues

**Worker can't access AWS APIs:**

```bash
# Check if instance profile is attached
aws ec2 describe-instances \
  --instance-ids $INSTANCE_ID \
  --query 'Reservations[0].Instances[0].IamInstanceProfile' \
  --region $AWS_REGION

# If empty, re-attach instance profile
aws ec2 associate-iam-instance-profile \
  --instance-id $INSTANCE_ID \
  --iam-instance-profile Name=ocpctl-worker-role \
  --region $AWS_REGION
```

**Permission denied errors in worker logs:**

```bash
# Check attached policies
aws iam list-attached-role-policies --role-name ocpctl-worker-role

# Verify policy document
aws iam get-policy-version \
  --policy-arn $WORKER_POLICY_ARN \
  --version-id v1

# Add missing permissions if needed
```

**Credentials not available on EC2:**

```bash
# Check instance metadata service (from EC2)
TOKEN=$(curl -X PUT "http://169.254.169.254/latest/api/token" -H "X-aws-ec2-metadata-token-ttl-seconds: 21600")
curl -H "X-aws-ec2-metadata-token: $TOKEN" http://169.254.169.254/latest/meta-data/iam/security-credentials/

# Should show role name, then:
curl -H "X-aws-ec2-metadata-token: $TOKEN" http://169.254.169.254/latest/meta-data/iam/security-credentials/ocpctl-worker-role
```

### Cost Considerations

**IAM Roles:** Free - no additional cost
**OpenShift Clusters:** Each cluster will incur costs for:
- EC2 instances (3 control plane + N workers)
- EBS volumes
- Load balancers (2 per cluster: API + Ingress)
- Data transfer
- Route53 queries

**Estimated cost per cluster:**
- Minimal test cluster (3 masters, no workers): ~$5-7/hour
- Standard dev cluster (3 masters + 3 workers): ~$8-12/hour

**Remember to delete test clusters when not in use!**

---

## Clean Up (When Done Testing)

```bash
# On the EC2 instance, stop services
ssh -i ~/.ssh/your-key.pem ec2-user@$EC2_IP
sudo systemctl stop ocpctl-api ocpctl-worker ocpctl-web nginx postgresql
exit

# From your local machine, terminate EC2 instance
aws ec2 terminate-instances --instance-ids $INSTANCE_ID --region $AWS_REGION

# Wait for termination (takes ~60 seconds)
aws ec2 wait instance-terminated --instance-ids $INSTANCE_ID --region $AWS_REGION

# Delete security group (after instance is terminated)
aws ec2 delete-security-group --group-id $APP_SG_ID --region $AWS_REGION

# Delete IAM role and instance profile (if created)
# Remove role from instance profile
aws iam remove-role-from-instance-profile \
  --instance-profile-name ocpctl-worker-role \
  --role-name ocpctl-worker-role

# Delete instance profile
aws iam delete-instance-profile \
  --instance-profile-name ocpctl-worker-role

# Detach managed policy
export WORKER_POLICY_ARN=$(aws iam list-policies \
  --query 'Policies[?PolicyName==`ocpctl-worker-openshift-provisioning`].Arn' \
  --output text)

aws iam detach-role-policy \
  --role-name ocpctl-worker-role \
  --policy-arn $WORKER_POLICY_ARN

# Delete IAM role
aws iam delete-role --role-name ocpctl-worker-role

# Delete IAM policy
aws iam delete-policy --policy-arn $WORKER_POLICY_ARN

echo "Cleanup complete!"
```

**Note:** Database data is deleted when the EC2 instance is terminated. If you need to preserve data, take a backup first.

---

## Summary

You now have:
- ✅ EC2 instance with PostgreSQL database
- ✅ All services running (API, Worker, Web)
- ✅ Nginx reverse proxy
- ✅ Web interface accessible at `http://<EC2_IP>`
- ✅ Worker processing cluster jobs
- ✅ Health checks enabled
- ✅ Security features active (rate limiting, audit logging)

**Total Monthly Cost (estimate):**
- EC2 t3.medium: ~$30/month
- EBS Storage (30GB): ~$3/month
- Data transfer: ~$2/month
- **Total: ~$35/month**

**Migration to RDS:** If you need production-grade database features (automated backups, multi-AZ, etc.), you can migrate to RDS later. See [Appendix: RDS PostgreSQL Setup](#appendix-rds-postgresql-setup) for instructions.

For production deployment, see:
- [Security Configuration Guide](./SECURITY_CONFIGURATION.md)
- [Deployment Guide](./DEPLOYMENT_WEB.md)

---

## Appendix: RDS PostgreSQL Setup

If you prefer to use RDS PostgreSQL instead of running PostgreSQL on the EC2 instance, follow these additional steps:

### A1: Create RDS Database (instead of Step 4 in main guide)

```bash
# Set variables
export AWS_REGION=us-east-1
export DB_PASSWORD=$(openssl rand -base64 32)
echo "Database password: $DB_PASSWORD" > ~/ocpctl-db-password.txt

# Get subnets from your VPC
export SUBNET1=$(aws ec2 describe-subnets \
  --filters "Name=vpc-id,Values=$VPC_ID" "Name=availability-zone,Values=${AWS_REGION}a" \
  --query 'Subnets[0].SubnetId' \
  --output text \
  --region $AWS_REGION)

export SUBNET2=$(aws ec2 describe-subnets \
  --filters "Name=vpc-id,Values=$VPC_ID" "Name=availability-zone,Values=${AWS_REGION}b" \
  --query 'Subnets[0].SubnetId' \
  --output text \
  --region $AWS_REGION)

# Create DB subnet group
aws rds create-db-subnet-group \
  --db-subnet-group-name ocpctl-db-subnet \
  --db-subnet-group-description "ocpctl database subnet group" \
  --subnet-ids $SUBNET1 $SUBNET2 \
  --region $AWS_REGION

# Create security group for RDS
aws ec2 create-security-group \
  --group-name ocpctl-db-sg \
  --description "ocpctl PostgreSQL database" \
  --vpc-id $VPC_ID \
  --region $AWS_REGION

export DB_SG_ID=$(aws ec2 describe-security-groups \
  --filters "Name=group-name,Values=ocpctl-db-sg" \
  --query 'SecurityGroups[0].GroupId' \
  --output text \
  --region $AWS_REGION)

# Allow EC2 to access RDS
aws ec2 authorize-security-group-ingress \
  --group-id $DB_SG_ID \
  --protocol tcp \
  --port 5432 \
  --source-group $APP_SG_ID \
  --region $AWS_REGION

# Create RDS instance
aws rds create-db-instance \
  --db-instance-identifier ocpctl-test-db \
  --db-instance-class db.t3.micro \
  --engine postgres \
  --engine-version 15.5 \
  --master-username ocpctl \
  --master-user-password "$DB_PASSWORD" \
  --allocated-storage 20 \
  --vpc-security-group-ids $DB_SG_ID \
  --db-subnet-group-name ocpctl-db-subnet \
  --backup-retention-period 7 \
  --no-publicly-accessible \
  --region $AWS_REGION

# Wait for database to be available (takes ~10-15 minutes)
echo "Creating RDS instance (this takes ~10-15 minutes)..."
aws rds wait db-instance-available \
  --db-instance-identifier ocpctl-test-db \
  --region $AWS_REGION

# Get database endpoint
export DB_ENDPOINT=$(aws rds describe-db-instances \
  --db-instance-identifier ocpctl-test-db \
  --query 'DBInstances[0].Endpoint.Address' \
  --output text \
  --region $AWS_REGION)

echo "Database endpoint: $DB_ENDPOINT"
echo "Database password: $DB_PASSWORD"
```

### A2: Configure DATABASE_URL for RDS

When configuring environment variables in Step 6, use:

```bash
export DATABASE_URL="postgres://ocpctl:$DB_PASSWORD@$DB_ENDPOINT:5432/postgres?sslmode=require"
```

**Important differences from EC2 PostgreSQL:**
- Use RDS endpoint instead of `localhost`
- Use `sslmode=require` for SSL/TLS connection
- Use database name `postgres` (default RDS database)
- No need to install or configure PostgreSQL on EC2 instance

### A3: Clean Up RDS (when done testing)

```bash
# Delete RDS instance
aws rds delete-db-instance \
  --db-instance-identifier ocpctl-test-db \
  --skip-final-snapshot \
  --region $AWS_REGION

# Wait for deletion (takes ~5 minutes)
aws rds wait db-instance-deleted \
  --db-instance-identifier ocpctl-test-db \
  --region $AWS_REGION

# Delete DB subnet group
aws rds delete-db-subnet-group \
  --db-subnet-group-name ocpctl-db-subnet \
  --region $AWS_REGION

# Delete RDS security group
aws ec2 delete-security-group --group-id $DB_SG_ID --region $AWS_REGION
```

**RDS Monthly Cost (additional):**
- RDS db.t3.micro: ~$15/month
- Total with RDS: ~$50/month (vs ~$35/month with EC2 PostgreSQL)

**When to use RDS:**
- Production deployments requiring automated backups
- Long-term deployments (6+ months)
- Need for multi-AZ failover or read replicas
- Compliance requirements for managed databases

---

## Creating Clusters in a Shared VPC

For migration testing with shared storage, create both clusters in the same VPC. This enables clusters to share an EFS filesystem and S3 bucket for testing migration scenarios.

### Why Shared VPC?

**Problem**: By default, each OpenShift cluster creates its own VPC. This prevents shared storage between clusters because:
- EFS filesystems are VPC-specific
- S3 access requires network connectivity
- Clusters in different VPCs cannot share resources without VPC peering

**Solution**: Deploy multiple clusters into the same persistent VPC using BYOVPC (Bring Your Own VPC) mode.

### VPC Requirements

Your VPC must have:

1. **At least 3 availability zones** with both private and public subnets in each AZ
2. **Internet Gateway** attached to the VPC
3. **NAT Gateways** in public subnets (at least one per AZ)
4. **Route Tables** properly configured:
   - Public subnets route 0.0.0.0/0 to Internet Gateway
   - Private subnets route 0.0.0.0/0 to NAT Gateway
5. **DNS Settings** enabled:
   - DNS resolution enabled
   - DNS hostnames enabled
6. **Non-overlapping CIDR** ranges:
   - VPC CIDR must not overlap with 10.128.0.0/14 (OpenShift pod network)
   - VPC CIDR must not overlap with 172.30.0.0/16 (OpenShift service network)
7. **Persistence**: VPC should be independent of any cluster lifecycle

### Step 1: Create Persistent Shared VPC

**Option A: Automated Script (Recommended)**

Use the provided script to create a properly configured VPC:

```bash
# SSH to your ocpctl server (or run locally with AWS credentials)
ssh -i ~/.ssh/ocpctl-test-key.pem ec2-user@<your-ec2-ip>
cd /opt/ocpctl

# Run the VPC creation script
chmod +x scripts/create-shared-vpc.sh

# For testing/dev (single NAT gateway, saves ~$64/month)
./scripts/create-shared-vpc.sh ocpctl-shared-vpc us-east-1 single

# For production (3 NAT gateways, high availability)
./scripts/create-shared-vpc.sh ocpctl-shared-vpc us-east-1 ha
```

The script will create:
- VPC with CIDR 10.0.0.0/16
- 3 larger private subnets (/22 each, ~1,000 IPs) for multiple clusters:
  - 10.0.0.0/22 (us-east-1a)
  - 10.0.4.0/22 (us-east-1b)
  - 10.0.8.0/22 (us-east-1c)
- 3 public subnets (/24 each, 256 IPs):
  - 10.0.64.0/24 (us-east-1a)
  - 10.0.65.0/24 (us-east-1b)
  - 10.0.66.0/24 (us-east-1c)
- Internet Gateway
- NAT Gateways (1 in single mode, 3 in ha mode)
- Properly configured route tables
- OpenShift/Kubernetes subnet tags for ELB discovery
- Machine-readable outputs (JSON + YAML profile snippet)

**Features:**
- **Idempotency**: Fails if VPC with same name already exists
- **Cleanup on failure**: Automatically removes partially created resources
- **Cost optimization**: Single NAT mode saves ~$64/month vs high-availability mode

**Option B: Manual VPC Creation**

If you prefer manual control or have an existing VPC:

```bash
# List available VPCs
aws ec2 describe-vpcs --query 'Vpcs[*].[VpcId,CidrBlock,Tags[?Key==`Name`].Value|[0]]' --output table

# List subnets in a VPC
VPC_ID=vpc-xxxxx
aws ec2 describe-subnets \
  --filters "Name=vpc-id,Values=$VPC_ID" \
  --query 'Subnets[*].[SubnetId,AvailabilityZone,CidrBlock,Tags[?Key==`Name`].Value|[0]]' \
  --output table
```

**IMPORTANT**: Do NOT use a VPC created by a cluster - those are destroyed with the cluster!

### Step 2: Get Subnet IDs from Your VPC

After creating or identifying your shared VPC, note the subnet IDs:

```bash
VPC_ID=vpc-xxxxx  # Your shared VPC ID

# Get private subnet IDs
aws ec2 describe-subnets \
  --filters "Name=vpc-id,Values=$VPC_ID" "Name=tag:Type,Values=private" \
  --query 'Subnets[*].[SubnetId,AvailabilityZone]' \
  --output table
```

Save these subnet IDs - you'll need them for the profile.

### Step 3: Create Custom Profile

Create a custom profile with your VPC subnet configuration:

```bash
# Create profile file
cat > internal/profile/definitions/aws-sno-my-vpc.yaml <<'EOF'
name: aws-sno-my-vpc
displayName: AWS Single Node OpenShift (My Shared VPC)
description: Single node cluster for shared VPC migration testing
platform: aws
enabled: true

openshiftVersions:
  allowlist:
    - "4.20.3"
    - "4.20.4"
    - "4.20.5"
  default: "4.20.3"

regions:
  allowlist:
    - us-east-1
  default: us-east-1

baseDomains:
  allowlist:
    - mg.dog8code.com
  default: mg.dog8code.com

compute:
  controlPlane:
    replicas: 1
    instanceType: m6i.2xlarge
    schedulable: true
  workers:
    replicas: 0
    minReplicas: 0
    maxReplicas: 0
    instanceType: m6i.2xlarge
    autoscaling: false

lifecycle:
  maxTTLHours: 24
  defaultTTLHours: 8
  allowCustomTTL: true
  warnBeforeDestroyHours: 1

networking:
  networkType: OVNKubernetes
  clusterNetworks:
    - cidr: 10.128.0.0/14
      hostPrefix: 23
  serviceNetwork:
    - 172.30.0.0/16
  machineNetwork:
    - cidr: 10.0.0.0/16

tags:
  required:
    Environment: test
  defaults:
    Purpose: migration-testing
    ManagedBy: cluster-control-plane
  allowUserTags: true

features:
  offHoursScaling: false
  fipsMode: false
  privateCluster: false

costControls:
  estimatedHourlyCost: 0.80
  maxMonthlyCost: 576
  budgetAlertThreshold: 0.8

platformConfig:
  aws:
    instanceMetadataService: required
    rootVolume:
      type: gp3
      size: 120
      iops: 3000
    # Replace with your VPC subnet IDs
    subnets:
      - subnet-071b5b7ad916b433c  # us-east-1a private
      - subnet-0f522e488c8d000a8  # us-east-1b private
      - subnet-0631a08791cb1b0f3  # us-east-1c private
      - subnet-064c667ddd75250d7  # us-east-1d private
      - subnet-096732a8ab8147452  # us-east-1f private
EOF

# Restart ocpctl-server to load new profile
sudo systemctl restart ocpctl-server
```

### Step 4: Create Clusters in Shared VPC

Create two clusters using the same profile to ensure they're in the same VPC:

```bash
# Create first cluster
curl -X POST http://localhost:8080/api/clusters \
  -H "Content-Type: application/json" \
  -d '{
    "name": "migration-source",
    "platform": "aws",
    "version": "4.20.3",
    "profile": "aws-sno-my-vpc",
    "region": "us-east-1",
    "base_domain": "mg.dog8code.com",
    "owner": "your-name",
    "team": "platform-team",
    "cost_center": "733",
    "ttl_hours": 24,
    "ssh_public_key": "ssh-ed25519 AAAA... your-key",
    "pull_secret": "YOUR_PULL_SECRET_JSON",
    "tags": {
      "Purpose": "migration-source"
    }
  }'

# Wait for first cluster to reach READY status (30-45 minutes)
watch -n 30 'curl -s http://localhost:8080/api/clusters | jq ".[] | select(.name==\"migration-source\") | {name, status}"'

# Create second cluster
curl -X POST http://localhost:8080/api/clusters \
  -H "Content-Type: application/json" \
  -d '{
    "name": "migration-target",
    "platform": "aws",
    "version": "4.20.3",
    "profile": "aws-sno-my-vpc",
    "region": "us-east-1",
    "base_domain": "mg.dog8code.com",
    "owner": "your-name",
    "team": "platform-team",
    "cost_center": "733",
    "ttl_hours": 24,
    "ssh_public_key": "ssh-ed25519 AAAA... your-key",
    "pull_secret": "YOUR_PULL_SECRET_JSON",
    "tags": {
      "Purpose": "migration-target"
    }
  }'

# Wait for second cluster to reach READY status
watch -n 30 'curl -s http://localhost:8080/api/clusters | jq ".[] | select(.name==\"migration-target\") | {name, status}"'
```

### Step 5: Verify Clusters in Same VPC

Once both clusters are READY, verify they're in the same VPC:

```bash
# Get cluster IDs
SOURCE_ID=$(curl -s http://localhost:8080/api/clusters | jq -r '.[] | select(.name=="migration-source") | .id')
TARGET_ID=$(curl -s http://localhost:8080/api/clusters | jq -r '.[] | select(.name=="migration-target") | .id')

# Get infraID from metadata.json for each cluster
SOURCE_INFRA=$(jq -r '.infraID' ~/.ocpctl/clusters/$SOURCE_ID/metadata.json)
TARGET_INFRA=$(jq -r '.infraID' ~/.ocpctl/clusters/$TARGET_ID/metadata.json)

# Get VPC ID for each cluster
SOURCE_VPC=$(aws ec2 describe-vpcs \
  --filters "Name=tag:kubernetes.io/cluster/$SOURCE_INFRA,Values=owned" \
  --query 'Vpcs[0].VpcId' --output text)

TARGET_VPC=$(aws ec2 describe-vpcs \
  --filters "Name=tag:kubernetes.io/cluster/$TARGET_INFRA,Values=owned" \
  --query 'Vpcs[0].VpcId' --output text)

echo "Source cluster VPC: $SOURCE_VPC"
echo "Target cluster VPC: $TARGET_VPC"

# Should show the same VPC ID for both clusters
if [ "$SOURCE_VPC" == "$TARGET_VPC" ]; then
  echo "✅ Both clusters are in the same VPC!"
else
  echo "❌ Clusters are in different VPCs - shared storage will not work"
fi
```

### Step 6: Configure Shared Storage

Once both clusters are READY and in the same VPC, configure shared storage for migration testing:

```bash
# Link clusters for shared storage
curl -X POST "http://localhost:8080/api/clusters/$SOURCE_ID/storage/link" \
  -H "Content-Type: application/json" \
  -d "{\"target_cluster_id\": \"$TARGET_ID\"}"

# Monitor shared storage provisioning job
sudo journalctl -u ocpctl-worker -f | grep -A 10 "PROVISION_SHARED_STORAGE"

# Expected output:
# Provisioning shared storage between migration-source and migration-target
# Source VPC: vpc-0299b53877cffb78a
# Target VPC: vpc-0299b53877cffb78a
# ✅ Clusters are in the same VPC
# Creating EFS file system...
# Creating S3 bucket...
# Successfully provisioned shared storage
```

### Step 7: Verify Shared Storage Configuration

Check that both clusters have shared storage configured:

```bash
# Get storage configuration for both clusters
curl -s http://localhost:8080/api/clusters/$SOURCE_ID | jq '.storage_config'
curl -s http://localhost:8080/api/clusters/$TARGET_ID | jq '.storage_config'

# Both should show:
# {
#   "shared_efs_id": "fs-xxxxxxxxx",
#   "shared_s3_bucket": "ocpctl-shared-storage-...",
#   "storage_group_id": "..."
# }
```

### Troubleshooting

**Clusters created in different VPCs:**
- Verify profile has correct `subnets` field in `platformConfig.aws`
- Check that profile was reloaded (restart ocpctl-server)
- Ensure subnets exist and are in the same VPC

**Shared storage provisioning fails:**
- Verify both clusters are READY status
- Check clusters are in same region
- Verify VPC has proper security group rules for EFS
- Check worker logs: `sudo journalctl -u ocpctl-worker -n 100`

**Profile not available:**
- Check profile file is in `internal/profile/definitions/`
- Verify YAML syntax is correct
- Restart ocpctl-server to reload profiles
- Check server logs: `sudo journalctl -u ocpctl-server -n 50`

### Cost Considerations

**Shared VPC deployment costs:**
- **VPC infrastructure:** $0 (existing VPC)
- **NAT Gateway:** ~$32/month per AZ (if not already present)
- **Cluster compute:** Same as standard deployment
- **Shared EFS:** ~$0.30/GB-month for Standard storage
- **Shared S3:** ~$0.023/GB-month for Standard storage

**Cost savings:**
- Eliminates duplicate VPC creation (~$32/month per NAT Gateway)
- Shared storage resources instead of per-cluster storage
- Ideal for temporary migration testing scenarios

## EFS Storage Configuration on STS-Enabled Clusters

### Overview

OpenShift clusters deployed with STS (AWS Security Token Service) use IAM Roles for Service Accounts (IRSA) instead of static AWS credentials. ocpctl automatically detects STS mode and configures the EFS CSI driver appropriately.

### STS Detection

The CONFIGURE_EFS job automatically detects STS mode by:
1. Checking if the cluster has an OIDC issuer configured (`oc get authentication cluster`)
2. Verifying the `aws-efs-cloud-credentials` secret exists in `openshift-cluster-csi-drivers` namespace
3. Checking if the secret contains an IAM role ARN (STS) vs static access keys

### Prerequisites for STS Clusters

For STS-enabled clusters, the following must be in place:

**1. OIDC Provider**
- Created automatically by `openshift-install` in Manual mode
- Hosted in S3 with public bucket policy for discovery
- Thumbprint configured with correct certificate chain

**2. EFS IAM Role**
- Created automatically during cluster installation via `ccoctl`
- Includes the EFS CredentialsRequest from `internal/installer/credreqs/efs-csi-driver.yaml`
- Grants permissions for EFS operations (CreateFileSystem, CreateAccessPoint, etc.)
- Trust policy allows OIDC-authenticated service accounts

**3. Credentials Secret**
- Name: `aws-efs-cloud-credentials`
- Namespace: `openshift-cluster-csi-drivers`
- Contains role ARN instead of static credentials
- Created by `ccoctl` during cluster installation

### How It Works

**During Cluster Installation:**
1. Installer detects STS credentials (session token or IMDS)
2. Uses Manual mode workflow instead of direct `openshift-install`
3. Runs `ccoctl` with EFS CredentialsRequest included
4. Creates IAM role with EFS permissions
5. Creates secret with role ARN in cluster

**During EFS Configuration (CONFIGURE_EFS job):**
1. Script detects OIDC issuer to identify STS mode
2. Verifies credentials secret exists and contains role ARN
3. Installs EFS CSI Driver Operator
4. Creates ClusterCSIDriver with STS configuration:
   ```yaml
   spec:
     managementState: Managed
     driverConfig:
       driverType: AWS
       aws:
         efsVolumeMetrics:
           state: RecursiveWalk
           refreshPeriod: 1h
   ```
5. Annotates service accounts with IAM role ARN
6. Driver pods use service account token projection to assume IAM role

### Service Account Configuration

For STS mode, the following service accounts receive IAM role annotations:

```yaml
apiVersion: v1
kind: ServiceAccount
metadata:
  name: aws-efs-csi-driver-controller-sa
  namespace: openshift-cluster-csi-drivers
  annotations:
    eks.amazonaws.com/role-arn: arn:aws:iam::123456789012:role/cluster-abc123-openshift-efs-csi-driver
```

```yaml
apiVersion: v1
kind: ServiceAccount
metadata:
  name: aws-efs-csi-driver-node-sa
  namespace: openshift-cluster-csi-drivers
  annotations:
    eks.amazonaws.com/role-arn: arn:aws:iam::123456789012:role/cluster-abc123-openshift-efs-csi-driver
```

### Verification

**1. Check cluster is using STS:**
```bash
# Should return an OIDC URL
oc get authentication cluster -o jsonpath='{.spec.serviceAccountIssuer}'
```

**2. Verify EFS credentials secret exists:**
```bash
oc get secret aws-efs-cloud-credentials -n openshift-cluster-csi-drivers

# Check if it contains a role ARN (STS) or access keys (static)
oc get secret aws-efs-cloud-credentials -n openshift-cluster-csi-drivers \
  -o jsonpath='{.data.credentials}' | base64 -d
```

**3. Verify IAM role exists:**
```bash
# Get infrastructure name
INFRA_ID=$(oc get infrastructure cluster -o jsonpath='{.status.infrastructureName}')

# Check IAM role exists
aws iam get-role --role-name "${INFRA_ID}-openshift-efs-csi-driver"
```

**4. Check service account annotations:**
```bash
oc get serviceaccount aws-efs-csi-driver-controller-sa \
  -n openshift-cluster-csi-drivers \
  -o jsonpath='{.metadata.annotations.eks\.amazonaws\.com/role-arn}'
```

**5. Verify EFS CSI driver is working:**
```bash
# Check ClusterCSIDriver status
oc get clustercsidriver efs.csi.aws.com -o yaml

# Check driver pods are running
oc get pods -n openshift-cluster-csi-drivers | grep efs

# Verify storage class exists
oc get storageclass efs-sc
```

### Troubleshooting STS EFS Issues

**Secret not found:**
```bash
# ERROR: EFS credentials secret (aws-efs-cloud-credentials) not found

# Cause: ccoctl didn't include EFS CredentialsRequest during installation
# Solution: This shouldn't happen with ocpctl - check installer logs
sudo journalctl -u ocpctl-worker | grep "Adding EFS CredentialsRequest"
```

**IAM role doesn't exist:**
```bash
# Verify role was created by ccoctl
aws iam list-roles | grep openshift-efs-csi-driver

# Check role trust policy allows OIDC
aws iam get-role --role-name "${INFRA_ID}-openshift-efs-csi-driver" \
  --query 'Role.AssumeRolePolicyDocument'
```

**OIDC provider issues:**
```bash
# Get OIDC issuer
OIDC_ISSUER=$(oc get authentication cluster -o jsonpath='{.spec.serviceAccountIssuer}')

# Verify provider exists in AWS
aws iam list-open-id-connect-providers

# Check provider URL matches cluster
aws iam get-open-id-connect-provider \
  --open-id-connect-provider-arn arn:aws:iam::ACCOUNT:oidc-provider/...
```

**Driver pods can't create EFS resources:**
```bash
# Check pod logs for permission errors
oc logs -n openshift-cluster-csi-drivers \
  deployment/aws-efs-csi-driver-controller-sa

# Look for:
# - "AccessDenied" errors → IAM role policy missing permissions
# - "AssumeRole" errors → OIDC trust policy misconfigured
# - "InvalidToken" errors → Service account annotation missing
```

**Storage Config Verification:**

After successful EFS configuration, the cluster's storage_config will include:

```json
{
  "efs_enabled": true,
  "local_efs_id": "fs-0abc123def456789",
  "local_efs_sg_id": "sg-0xyz789abc123456",
  "auth_mode": "sts",
  "iam_role_arn": "arn:aws:iam::123456789012:role/cluster-abc123-openshift-efs-csi-driver"
}
```

For non-STS clusters, the `auth_mode` will be `"static"` and `iam_role_arn` will be absent.

### EFS Permissions Required

The IAM role created by ccoctl includes these permissions:

**EFS Operations:**
- `elasticfilesystem:CreateAccessPoint`
- `elasticfilesystem:CreateFileSystem`
- `elasticfilesystem:CreateMountTarget`
- `elasticfilesystem:DeleteAccessPoint`
- `elasticfilesystem:DeleteFileSystem`
- `elasticfilesystem:DeleteMountTarget`
- `elasticfilesystem:DescribeAccessPoints`
- `elasticfilesystem:DescribeFileSystems`
- `elasticfilesystem:DescribeMountTargets`
- `elasticfilesystem:DescribeMountTargetSecurityGroups`
- `elasticfilesystem:ModifyMountTargetSecurityGroups`
- `elasticfilesystem:TagResource`

**EC2 Operations (for network interface management):**
- `ec2:DescribeSubnets`
- `ec2:DescribeNetworkInterfaces`
- `ec2:DescribeSecurityGroups`
- `ec2:CreateNetworkInterface`
- `ec2:DeleteNetworkInterface`

### References

- [OpenShift EFS CSI Driver Documentation](https://docs.openshift.com/container-platform/latest/storage/container_storage_interface/persistent-storage-csi-aws-efs.html)
- [AWS EFS CSI Driver GitHub](https://github.com/kubernetes-sigs/aws-efs-csi-driver)
- [OpenShift Cloud Credential Operator](https://docs.openshift.com/container-platform/latest/authentication/managing_cloud_provider_credentials/about-cloud-credential-operator.html)
- [AWS IAM Roles for Service Accounts (IRSA)](https://docs.aws.amazon.com/eks/latest/userguide/iam-roles-for-service-accounts.html)
