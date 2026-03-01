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

# Install PostgreSQL 15 server
sudo dnf install -y postgresql15-server postgresql15

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
# Build binaries
cd /path/to/ocpctl
go build -o bin/api ./cmd/api
go build -o bin/worker ./cmd/worker

# Build web frontend
cd web
npm install
npm run build

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
# Move binaries to correct location
sudo mv /tmp/api /opt/ocpctl/bin/
sudo mv /tmp/worker /opt/ocpctl/bin/
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
CORS_ALLOWED_ORIGINS=http://$EC2_IP,https://$EC2_IP

# IAM Auth (optional)
ENABLE_IAM_AUTH=false

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
WORKER_WORK_DIR=/tmp/ocpctl
WORKER_HEALTH_PORT=8081

# OpenShift Pull Secret
# IMPORTANT: Add your pull secret here
OPENSHIFT_PULL_SECRET='PASTE_YOUR_PULL_SECRET_HERE'

# Environment
ENVIRONMENT=test
EOF

# Create Web environment file
sudo tee /etc/ocpctl/web.env > /dev/null <<EOF
# API endpoint
NEXT_PUBLIC_API_URL=http://localhost:8080/api/v1

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

### Step 8: Run Database Migrations

```bash
# Start API temporarily to run migrations
sudo systemctl start ocpctl-api

# Check logs to verify migrations ran
sudo journalctl -u ocpctl-api -n 50 --no-pager

# You should see migration logs
# If migrations don't run automatically, you may need to run them manually
```

### Step 9: Start All Services

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

### Step 10: Configure Nginx

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

### Step 11: Test the Deployment

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

### Step 12: Access the Web Interface

1. **Open browser:** Navigate to `http://<EC2_IP>`
2. **Login with default credentials:**
   - Email: `admin@localhost`
   - Password: `changeme`
3. **Change admin password immediately** in the admin panel
4. **Test features:**
   - Create a cluster profile
   - View available profiles
   - Create a test cluster (it will provision!)

### Step 13: Monitor Logs

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

### Enable HTTPS (Recommended)

```bash
# Install certbot
sudo dnf install -y certbot python3-certbot-nginx

# Get certificate (requires domain name)
sudo certbot --nginx -d your-domain.com

# Auto-renewal is configured automatically
```

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

# Delete IAM role (if created)
# aws iam remove-role-from-instance-profile \
#   --instance-profile-name ocpctl-ec2-role \
#   --role-name ocpctl-ec2-role
# aws iam delete-instance-profile --instance-profile-name ocpctl-ec2-role
# aws iam delete-role-policy --role-name ocpctl-ec2-role --policy-name ocpctl-s3-access
# aws iam delete-role --role-name ocpctl-ec2-role

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
