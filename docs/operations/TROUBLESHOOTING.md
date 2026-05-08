# OCPCTL Troubleshooting Guide

**Purpose:** Comprehensive troubleshooting guide for common ocpctl deployment and operational issues.

**Audience:** Operators, administrators, and users experiencing issues with ocpctl.

**Last Updated:** 2026-05-08

---

## Quick Diagnostic Decision Tree

```
Issue Type?
├─ Deployment/Installation → Section 1
├─ Services Won't Start → Section 2
├─ Cluster Creation Fails → Section 3
├─ Cluster Operation Issues → Section 4
├─ Performance/Slow → Section 5
├─ Access/Authentication → Section 6
└─ Database Issues → Section 7
```

---

## Table of Contents

1. [Deployment and Installation Issues](#1-deployment-and-installation-issues)
2. [Service Startup Issues](#2-service-startup-issues)
3. [Cluster Creation Failures](#3-cluster-creation-failures)
4. [Cluster Operation Issues](#4-cluster-operation-issues)
5. [Performance Issues](#5-performance-issues)
6. [Access and Authentication Issues](#6-access-and-authentication-issues)
7. [Database Issues](#7-database-issues)
8. [Network and Connectivity Issues](#8-network-and-connectivity-issues)
9. [Storage and Disk Space Issues](#9-storage-and-disk-space-issues)
10. [Common Error Messages](#10-common-error-messages)

---

## 1. Deployment and Installation Issues

### 1.1 Cannot SSH to EC2 Instance

**Symptoms:**
```
ssh: connect to host 44.201.165.78 port 22: Connection timed out
```

**Common Causes:**
- Security group doesn't allow SSH from your IP
- Instance not fully started
- Wrong SSH key
- Instance doesn't have public IP

**Diagnostic Steps:**
```bash
# 1. Check instance state
aws ec2 describe-instances --instance-ids $INSTANCE_ID \
  --query 'Reservations[0].Instances[0].State.Name'
# Should return: "running"

# 2. Check security group rules
aws ec2 describe-security-groups --group-ids $SG_ID \
  --query 'SecurityGroups[0].IpPermissions[?FromPort==`22`]'
# Should show your IP in CIDR format

# 3. Verify public IP exists
aws ec2 describe-instances --instance-ids $INSTANCE_ID \
  --query 'Reservations[0].Instances[0].PublicIpAddress'
# Should return IP address, not empty

# 4. Test from different network
# Try mobile hotspot or different WiFi
```

**Solutions:**

**If security group missing your IP:**
```bash
export MY_IP=$(curl -s https://checkip.amazonaws.com)
aws ec2 authorize-security-group-ingress \
  --group-id $SG_ID \
  --protocol tcp \
  --port 22 \
  --cidr $MY_IP/32
```

**If instance has no public IP:**
```bash
# Allocate and associate Elastic IP
aws ec2 allocate-address --query 'AllocationId' --output text
# Note allocation ID, then:
aws ec2 associate-address \
  --instance-id $INSTANCE_ID \
  --allocation-id eipalloc-xxxxx
```

**If wrong SSH key:**
```bash
# Verify key pair name
aws ec2 describe-instances --instance-ids $INSTANCE_ID \
  --query 'Reservations[0].Instances[0].KeyName'
# Use correct key file
```

### 1.2 Binary Upload Fails (SCP/rsync)

**Symptoms:**
```
scp: /opt/ocpctl/bin/ocpctl-api: Permission denied
```

**Common Causes:**
- Target directory doesn't exist
- Wrong permissions on target directory
- Not using sudo when needed

**Solutions:**
```bash
# Create directory with correct permissions first
ssh -i ~/.ssh/key.pem ubuntu@$EC2_IP \
  "sudo mkdir -p /opt/ocpctl/bin && sudo chown -R ubuntu:ubuntu /opt/ocpctl"

# Then upload
scp -i ~/.ssh/key.pem bin/ocpctl-api ubuntu@$EC2_IP:/tmp/
ssh -i ~/.ssh/key.pem ubuntu@$EC2_IP \
  "sudo mv /tmp/ocpctl-api /opt/ocpctl/bin/ && sudo chmod +x /opt/ocpctl/bin/ocpctl-api"
```

### 1.3 PostgreSQL Installation Fails

**Symptoms:**
```
E: Unable to locate package postgresql15-server
```

**Common Causes:**
- Wrong OS (Ubuntu vs Amazon Linux vs RHEL)
- Package repository not configured

**Solutions:**

**For Amazon Linux 2023:**
```bash
sudo dnf install -y postgresql15-server postgresql15
```

**For Ubuntu 22.04:**
```bash
sudo apt-get update
sudo apt-get install -y postgresql postgresql-contrib
```

**For RHEL/Rocky Linux:**
```bash
sudo dnf install -y postgresql-server postgresql
```

### 1.4 Database Migration Fails

**Symptoms:**
```
ERROR: relation "schema_migrations" does not exist
ERROR: permission denied for database postgres
```

**Diagnostic Steps:**
```bash
# 1. Check database exists
psql "$DATABASE_URL" -c "\l" | grep ocpctl

# 2. Check user permissions
psql "$DATABASE_URL" -c "\du" | grep ocpctl_user

# 3. Check connection works
psql "$DATABASE_URL" -c "SELECT version();"
```

**Solutions:**

**If database doesn't exist:**
```bash
sudo -u postgres psql << EOF
CREATE DATABASE ocpctl;
CREATE USER ocpctl_user WITH PASSWORD 'your-password';
GRANT ALL PRIVILEGES ON DATABASE ocpctl TO ocpctl_user;
\c ocpctl
CREATE EXTENSION IF NOT EXISTS "uuid-ossp";
GRANT ALL ON SCHEMA public TO ocpctl_user;
EOF
```

**If permission denied:**
```bash
# Connect as postgres and grant permissions
sudo -u postgres psql ocpctl << EOF
GRANT ALL PRIVILEGES ON ALL TABLES IN SCHEMA public TO ocpctl_user;
GRANT ALL PRIVILEGES ON ALL SEQUENCES IN SCHEMA public TO ocpctl_user;
ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT ALL ON TABLES TO ocpctl_user;
ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT ALL ON SEQUENCES TO ocpctl_user;
EOF
```

---

## 2. Service Startup Issues

### 2.1 API Service Won't Start

**Symptoms:**
```bash
sudo systemctl status ocpctl-api
# Shows: "Failed" or "activating (auto-restart)"
```

**Diagnostic Steps:**
```bash
# 1. Check recent logs
sudo journalctl -u ocpctl-api -n 50 --no-pager

# 2. Check environment file exists
sudo test -f /etc/ocpctl/api.env && echo "Found" || echo "Missing"

# 3. Manually run binary to see error
sudo -u ocpctl /opt/ocpctl/bin/ocpctl-api
```

**Common Errors and Solutions:**

#### Error: "DATABASE_URL not set"
```bash
# Check environment file
sudo cat /etc/ocpctl/api.env | grep DATABASE_URL

# If missing, add it
sudo bash -c 'echo "DATABASE_URL=postgres://..." >> /etc/ocpctl/api.env'
sudo systemctl restart ocpctl-api
```

#### Error: "dial tcp: lookup postgres: no such host"
```bash
# DATABASE_URL has wrong hostname
# Fix the hostname in /etc/ocpctl/api.env
sudo nano /etc/ocpctl/api.env
# Change to correct hostname (localhost or RDS endpoint)
```

#### Error: "bind: address already in use"
```bash
# Another process using port 8080
sudo netstat -tlnp | grep 8080
# Kill conflicting process or change port
sudo systemctl stop ocpctl-api
# Wait a moment, then:
sudo systemctl start ocpctl-api
```

#### Error: "failed to load profiles"
```bash
# Profiles directory missing or empty
ls -la /opt/ocpctl/profiles/

# Copy profiles if missing
sudo mkdir -p /opt/ocpctl/profiles
sudo cp internal/profile/definitions/*.yaml /opt/ocpctl/profiles/
sudo chown -R ocpctl:ocpctl /opt/ocpctl/profiles
```

### 2.2 Worker Service Won't Start

**Symptoms:**
```bash
sudo systemctl status ocpctl-worker
# Shows: "Failed" or crashes immediately
```

**Diagnostic Steps:**
```bash
# 1. Check logs for specific error
sudo journalctl -u ocpctl-worker -n 100 --no-pager | grep -i error

# 2. Check environment file
sudo cat /etc/ocpctl/worker.env

# 3. Test worker binary manually
sudo -u ocpctl OPENSHIFT_PULL_SECRET='{"auths":{}}' \
  DATABASE_URL='postgres://...' \
  /opt/ocpctl/bin/ocpctl-worker
```

**Common Errors:**

#### Error: "OPENSHIFT_PULL_SECRET not set"
```bash
# Pull secret missing from environment
sudo nano /etc/ocpctl/worker.env
# Add: OPENSHIFT_PULL_SECRET='<paste-pull-secret-json>'
sudo systemctl restart ocpctl-worker
```

#### Error: "failed to create work directory"
```bash
# Work directory doesn't exist or wrong permissions
sudo mkdir -p /var/lib/ocpctl/clusters
sudo chown -R ocpctl:ocpctl /var/lib/ocpctl
sudo systemctl restart ocpctl-worker
```

#### Error: "no space left on device"
```bash
# Disk full
df -h /var/lib/ocpctl

# Clean up old cluster directories
sudo find /var/lib/ocpctl/clusters -type d -name "cluster-*" -mtime +7 -exec rm -rf {} \;
```

### 2.3 Web Frontend Won't Start

**Symptoms:**
```
Error: Cannot find module 'next'
Error: EADDRINUSE: address already in use :::3000
```

**Solutions:**

**Missing dependencies:**
```bash
cd /opt/ocpctl/web
sudo -u ocpctl npm install
sudo systemctl restart ocpctl-web
```

**Port in use:**
```bash
sudo netstat -tlnp | grep 3000
# Kill process or change web port
```

**Build missing:**
```bash
cd /opt/ocpctl/web
sudo -u ocpctl npm run build
sudo systemctl restart ocpctl-web
```

---

## 3. Cluster Creation Failures

### 3.1 Cluster Stuck in "CREATING" Status

**Symptoms:**
- Cluster shows "CREATING" for > 60 minutes
- No progress in worker logs

**Diagnostic Steps:**
```bash
# 1. Check job status
psql "$DATABASE_URL" -c \
  "SELECT id, status, error_message FROM jobs WHERE cluster_id='$CLUSTER_ID';"

# 2. Check worker is processing
sudo journalctl -u ocpctl-worker --since '10 minutes ago' | grep "$CLUSTER_ID"

# 3. Check work directory
sudo ls -la /var/lib/ocpctl/clusters/$CLUSTER_ID/

# 4. Check openshift-install logs
sudo tail -50 /var/lib/ocpctl/clusters/$CLUSTER_ID/.openshift_install.log
```

**Common Causes and Solutions:**

#### Worker Not Picking Up Job
```bash
# Check job lock
psql "$DATABASE_URL" -c \
  "SELECT * FROM job_locks WHERE cluster_id='$CLUSTER_ID';"

# If locked by dead worker, clear lock
psql "$DATABASE_URL" -c \
  "DELETE FROM job_locks WHERE cluster_id='$CLUSTER_ID';"

# Restart worker
sudo systemctl restart ocpctl-worker
```

#### OpenShift Install Timeout
```bash
# Check install log for specific error
sudo grep -i error /var/lib/ocpctl/clusters/$CLUSTER_ID/.openshift_install.log

# Common issues:
# - API not reachable: Check security groups
# - Bootstrap timeout: Check NAT gateway, internet connectivity
# - Image pull errors: Check pull secret
```

#### AWS API Throttling
```bash
# Look for throttling errors
sudo journalctl -u ocpctl-worker | grep -i throttl

# Solution: Wait and retry, or contact AWS support for quota increase
```

### 3.2 Cluster Creation Fails with Permission Error

**Symptoms:**
```
ERROR: AccessDenied: User is not authorized to perform: ec2:RunInstances
```

**Diagnostic Steps:**
```bash
# 1. Check instance has IAM role
aws ec2 describe-instances --instance-ids $INSTANCE_ID \
  --query 'Reservations[0].Instances[0].IamInstanceProfile.Arn'

# 2. Check role has required policy
aws iam list-attached-role-policies --role-name ocpctl-worker-role

# 3. Test specific permission
ssh -i ~/.ssh/key.pem ubuntu@$EC2_IP \
  "aws ec2 describe-regions --region us-east-1"
```

**Solutions:**

**No IAM role attached:**
```bash
# Attach instance profile
aws ec2 associate-iam-instance-profile \
  --instance-id $INSTANCE_ID \
  --iam-instance-profile Name=ocpctl-worker-role
```

**Missing permissions:**
```bash
# Attach the full policy
export POLICY_ARN=$(aws iam list-policies \
  --query 'Policies[?PolicyName==`ocpctl-worker-full`].Arn' \
  --output text)

aws iam attach-role-policy \
  --role-name ocpctl-worker-role \
  --policy-arn $POLICY_ARN
```

### 3.3 Cluster Creation Fails - Invalid Pull Secret

**Symptoms:**
```
ERROR: unauthorized: authentication required
ERROR: failed to pull image quay.io/openshift-release-dev/...
```

**Solutions:**
```bash
# 1. Verify pull secret format
sudo grep OPENSHIFT_PULL_SECRET /etc/ocpctl/worker.env | \
  python3 -c "import sys, json; json.loads(sys.stdin.read().split('=', 1)[1])"
# Should parse without errors

# 2. Re-download pull secret
# Go to https://console.redhat.com/openshift/install/pull-secret
# Download new pull secret

# 3. Update worker config
sudo nano /etc/ocpctl/worker.env
# Replace OPENSHIFT_PULL_SECRET with new pull secret

# 4. Restart worker
sudo systemctl restart ocpctl-worker

# 5. Retry cluster creation or requeue job
psql "$DATABASE_URL" -c \
  "UPDATE jobs SET status='PENDING', attempt=0 WHERE cluster_id='$CLUSTER_ID';"
```

### 3.4 Route53 Hosted Zone Not Found

**Symptoms:**
```
ERROR: hosted zone 'your-domain.com' not found
ERROR: failed to create DNS records
```

**Solutions:**
```bash
# 1. Verify hosted zone exists
aws route53 list-hosted-zones --query 'HostedZones[*].[Name,Id]'

# 2. If missing, create hosted zone
aws route53 create-hosted-zone \
  --name your-domain.com \
  --caller-reference $(date +%s)

# 3. Update profile with correct base domain
sudo nano /opt/ocpctl/profiles/your-profile.yaml
# Ensure baseDomains.allowlist includes your domain

# 4. Restart API to reload profiles
sudo systemctl restart ocpctl-api
```

### 3.5 Service Quota Exceeded

**Symptoms:**
```
ERROR: VpcLimitExceeded: The maximum number of VPCs has been reached
ERROR: InstanceLimitExceeded: You have requested more instances than your current quota
```

**Solutions:**
```bash
# 1. Check current quotas
aws service-quotas list-service-quotas \
  --service-code vpc \
  --query 'Quotas[?QuotaName==`VPCs per Region`]'

# 2. Request quota increase
aws service-quotas request-service-quota-increase \
  --service-code vpc \
  --quota-code L-F678F1CE \
  --desired-value 20

# 3. While waiting, delete unused VPCs
aws ec2 describe-vpcs \
  --filters "Name=tag:ManagedBy,Values=ocpctl" \
  --query 'Vpcs[*].[VpcId,Tags[?Key==`Name`].Value|[0]]'

# Delete old cluster VPCs (be careful!)
```

---

## 4. Cluster Operation Issues

### 4.1 Cannot Access Cluster (Kubeconfig Issues)

**Symptoms:**
```
Error from server: Unauthorized
Unable to connect to the server: dial tcp: lookup api.cluster.domain.com: no such host
```

**Solutions:**

**Kubeconfig not found:**
```bash
# Check cluster outputs table
psql "$DATABASE_URL" -c \
  "SELECT kubeconfig_s3_uri FROM cluster_outputs WHERE cluster_id='$CLUSTER_ID';"

# Download from S3
aws s3 cp s3://bucket/path/kubeconfig ~/.kube/config-cluster

# Set KUBECONFIG
export KUBECONFIG=~/.kube/config-cluster
oc get nodes
```

**DNS not resolving:**
```bash
# Check if DNS record exists
dig api.cluster-name.your-domain.com +short

# If missing, check Route53
aws route53 list-resource-record-sets \
  --hosted-zone-id $ZONE_ID \
  --query "ResourceRecordSets[?Name=='api.cluster-name.your-domain.com.']"
```

### 4.2 Cluster Won't Hibernate

**Symptoms:**
```
ERROR: failed to hibernate cluster: timeout waiting for nodes to stop
Job status: FAILED
```

**Solutions:**
```bash
# 1. Check cluster type supports hibernation
# ROSA hibernation is limited (can only scale workers to 0)

# 2. Check for pending pods preventing drain
oc get pods -A | grep -v Running | grep -v Completed

# 3. Force drain nodes (careful!)
oc adm drain <node-name> --ignore-daemonsets --delete-emptydir-data --force

# 4. Manually hibernate via AWS (OpenShift IPI only)
# Get cluster infra ID
export INFRA_ID=$(oc get infrastructure cluster -o jsonpath='{.status.infrastructureName}')

# Stop all cluster instances
aws ec2 describe-instances \
  --filters "Name=tag:kubernetes.io/cluster/${INFRA_ID},Values=owned" \
  --query 'Reservations[].Instances[].InstanceId' \
  --output text | xargs aws ec2 stop-instances --instance-ids
```

### 4.3 Cluster Won't Resume

**Symptoms:**
```
ERROR: cluster failed to resume: API not reachable after 15 minutes
```

**Solutions:**
```bash
# 1. Check instances are starting
export INFRA_ID=<cluster-infra-id>
aws ec2 describe-instances \
  --filters "Name=tag:kubernetes.io/cluster/${INFRA_ID},Values=owned" \
  --query 'Reservations[].Instances[].[InstanceId,State.Name]'

# 2. If stopped, start manually
aws ec2 describe-instances \
  --filters "Name=tag:kubernetes.io/cluster/${INFRA_ID},Values=owned" \
  --query 'Reservations[].Instances[].InstanceId' \
  --output text | xargs aws ec2 start-instances --instance-ids

# 3. Wait for API to be reachable
while ! curl -k https://api.cluster-name.domain.com:6443/healthz; do
  echo "Waiting for API..."
  sleep 10
done
```

### 4.4 Cluster Destroy Fails

**Symptoms:**
```
ERROR: failed to destroy cluster: some resources could not be deleted
ERROR: DependencyViolation: resource has dependent objects
```

**Solutions:**
```bash
# 1. Check worker logs for specific errors
sudo journalctl -u ocpctl-worker | grep "$CLUSTER_ID" | grep -i error

# 2. Common stuck resources:
# - Load balancers: Delete manually
aws elbv2 describe-load-balancers \
  --query "LoadBalancers[?contains(LoadBalancerName, '$INFRA_ID')]"

aws elbv2 delete-load-balancer --load-balancer-arn <arn>

# - Security groups: Find and delete
aws ec2 describe-security-groups \
  --filters "Name=tag:kubernetes.io/cluster/${INFRA_ID},Values=owned"

# - NAT gateways: Delete NAT, then IGW
aws ec2 describe-nat-gateways \
  --filter "Name=vpc-id,Values=$VPC_ID"

aws ec2 delete-nat-gateway --nat-gateway-id <nat-id>

# 3. After manual cleanup, requeue destroy job
psql "$DATABASE_URL" -c \
  "UPDATE jobs SET status='PENDING', attempt=0 WHERE cluster_id='$CLUSTER_ID' AND job_type='DESTROY';"
```

---

## 5. Performance Issues

### 5.1 API Slow Response Times

**Symptoms:**
- API responses take > 5 seconds
- UI feels sluggish
- Timeouts on cluster list page

**Diagnostic Steps:**
```bash
# 1. Check API response time
time curl -s http://localhost:8080/health

# 2. Check database query performance
psql "$DATABASE_URL" -c "EXPLAIN ANALYZE SELECT * FROM clusters LIMIT 100;"

# 3. Check system resources
top
free -h
df -h
```

**Solutions:**

**Database slow:**
```bash
# Add indexes if missing
psql "$DATABASE_URL" << EOF
CREATE INDEX IF NOT EXISTS idx_clusters_status ON clusters(status);
CREATE INDEX IF NOT EXISTS idx_clusters_owner ON clusters(owner);
CREATE INDEX IF NOT EXISTS idx_jobs_cluster_id ON jobs(cluster_id);
CREATE INDEX IF NOT EXISTS idx_jobs_status ON jobs(status);
EOF

# Vacuum database
psql "$DATABASE_URL" -c "VACUUM ANALYZE;"
```

**High memory usage:**
```bash
# Restart services
sudo systemctl restart ocpctl-api ocpctl-worker

# If persistent, upgrade instance
# t3.medium → t3.large
```

**Too many concurrent requests:**
```bash
# Reduce rate limit or add more API instances
sudo nano /etc/ocpctl/api.env
# Increase: RATE_LIMIT_REQUESTS=200
sudo systemctl restart ocpctl-api
```

### 5.2 Worker Jobs Queuing Up

**Symptoms:**
- Many jobs stuck in PENDING
- Cluster creation taking hours to start

**Diagnostic Steps:**
```bash
# Check pending jobs count
psql "$DATABASE_URL" -c \
  "SELECT job_type, COUNT(*) FROM jobs WHERE status='PENDING' GROUP BY job_type;"

# Check running jobs
psql "$DATABASE_URL" -c \
  "SELECT id, job_type, started_at FROM jobs WHERE status='RUNNING';"

# Check worker concurrency
sudo grep WORKER_CONCURRENCY /etc/ocpctl/worker.env
```

**Solutions:**

**Increase worker concurrency:**
```bash
sudo nano /etc/ocpctl/worker.env
# Change: WORKER_CONCURRENCY=5  (from 3)
sudo systemctl restart ocpctl-worker
```

**Add autoscaling workers:**
```bash
# See autoscaling worker setup in deployment docs
# Or manually launch additional worker instances
```

**Clear stuck locks:**
```bash
# Find expired locks
psql "$DATABASE_URL" -c \
  "SELECT * FROM job_locks WHERE expires_at < NOW();"

# Delete expired locks
psql "$DATABASE_URL" -c \
  "DELETE FROM job_locks WHERE expires_at < NOW();"
```

### 5.3 Disk Space Running Out

**Symptoms:**
```
ERROR: no space left on device
df: /var/lib/ocpctl: No space left on device
```

**Solutions:**
```bash
# 1. Check disk usage
df -h /var/lib/ocpctl
du -sh /var/lib/ocpctl/clusters/*

# 2. Find largest directories
du -h /var/lib/ocpctl/clusters | sort -rh | head -20

# 3. Clean up old cluster directories
# WARNING: Only delete DESTROYED clusters
psql "$DATABASE_URL" -c \
  "SELECT id, name, status FROM clusters WHERE status='DESTROYED';" | \
  grep -oP 'cluster-[a-f0-9-]+' | while read cluster_id; do
    echo "Removing $cluster_id"
    sudo rm -rf /var/lib/ocpctl/clusters/$cluster_id
  done

# 4. Enable janitor for automatic cleanup
# Janitor runs every 5 minutes by default
sudo journalctl -u ocpctl-worker | grep janitor | tail -20

# 5. If disk still full, expand volume
# Stop services first
sudo systemctl stop ocpctl-api ocpctl-worker

# Expand volume in AWS console or:
aws ec2 modify-volume --volume-id vol-xxxxx --size 200

# Wait for modification to complete
aws ec2 describe-volumes-modifications --volume-id vol-xxxxx

# Resize filesystem
sudo growpart /dev/nvme0n1 1
sudo resize2fs /dev/nvme0n1p1

# Start services
sudo systemctl start ocpctl-api ocpctl-worker
```

---

## 6. Access and Authentication Issues

### 6.1 Cannot Login to Web UI

**Symptoms:**
- Login page loads but credentials don't work
- Error: "Invalid credentials"
- 401 Unauthorized

**Solutions:**

**Default credentials not working:**
```bash
# Reset admin password
psql "$DATABASE_URL" << EOF
UPDATE users SET password_hash='\$2a\$10\$abc...' WHERE email='admin@localhost';
EOF

# Or create new admin user
# Use API to create user, or directly in DB
```

**JWT token issues:**
```bash
# Check JWT_SECRET is set
sudo grep JWT_SECRET /etc/ocpctl/api.env

# Ensure it's at least 32 characters
# Regenerate if needed
openssl rand -base64 48 | sudo tee -a /etc/ocpctl/api.env
sudo systemctl restart ocpctl-api
```

**CORS issues:**
```bash
# Check CORS configuration
sudo grep CORS_ALLOWED_ORIGINS /etc/ocpctl/api.env

# Should match your frontend URL
# If accessing via IP: http://44.201.165.78
# If via domain: https://ocpctl.your-domain.com

# Update if wrong
sudo nano /etc/ocpctl/api.env
sudo systemctl restart ocpctl-api
```

### 6.2 IAM Authentication Not Working

**Symptoms:**
- IAM login option not available
- Error: "IAM authentication failed"
- "Access key not found"

**Solutions:**
```bash
# 1. Verify IAM auth is enabled
sudo grep ENABLE_IAM_AUTH /etc/ocpctl/api.env
# Should be: ENABLE_IAM_AUTH=true

# 2. Check API server has IAM permissions
# On the EC2 instance:
aws sts get-caller-identity
# Should show role ARN

# 3. Test IAM authentication manually
curl -X POST http://localhost:8080/api/v1/auth/iam \
  -H "Content-Type: application/json" \
  -d '{
    "access_key_id": "AKIA...",
    "secret_access_key": "...",
    "region": "us-east-1"
  }'
```

**IAM group restriction:**
```bash
# If using IAM_ALLOWED_GROUP, verify user is in group
aws iam list-groups-for-user --user-name your-username

# Or remove group restriction
sudo nano /etc/ocpctl/api.env
# Set: IAM_ALLOWED_GROUP=
sudo systemctl restart ocpctl-api
```

---

## 7. Database Issues

### 7.1 Database Connection Failed

**Symptoms:**
```
ERROR: dial tcp: connect: connection refused
ERROR: pq: password authentication failed
```

**Solutions:**

**PostgreSQL not running:**
```bash
sudo systemctl status postgresql
# If not running:
sudo systemctl start postgresql
```

**Wrong password:**
```bash
# Check password in Parameter Store matches database
DB_PASSWORD=$(aws ssm get-parameter \
  --name /ocpctl/database/password \
  --with-decryption \
  --query Parameter.Value \
  --output text)

# Reset PostgreSQL password to match
sudo -u postgres psql -c \
  "ALTER USER ocpctl_user WITH PASSWORD '$DB_PASSWORD';"
```

**Wrong hostname:**
```bash
# For PostgreSQL on EC2, should be: localhost
# For RDS, should be: <endpoint>.rds.amazonaws.com

# Update DATABASE_URL
sudo nano /etc/ocpctl/api.env
# Update hostname in connection string
sudo systemctl restart ocpctl-api ocpctl-worker
```

### 7.2 Database Corruption or Inconsistency

**Symptoms:**
```
ERROR: duplicate key value violates unique constraint
ERROR: invalid input syntax for type uuid
```

**Solutions:**
```bash
# 1. Backup database first
pg_dump "$DATABASE_URL" > /tmp/ocpctl_backup_$(date +%s).sql

# 2. Check for corruption
psql "$DATABASE_URL" -c "SELECT * FROM pg_stat_database WHERE datname='ocpctl';"

# 3. Reindex database
psql "$DATABASE_URL" -c "REINDEX DATABASE ocpctl;"

# 4. Vacuum full (requires downtime)
sudo systemctl stop ocpctl-api ocpctl-worker
psql "$DATABASE_URL" -c "VACUUM FULL ANALYZE;"
sudo systemctl start ocpctl-api ocpctl-worker
```

---

## 8. Network and Connectivity Issues

### 8.1 Cluster API Not Reachable

**Symptoms:**
```
Error: dial tcp: i/o timeout
Unable to connect to the server: dial tcp: lookup api.cluster.domain.com: no such host
```

**Solutions:**
```bash
# 1. Check DNS resolution
dig api.cluster-name.your-domain.com +short

# 2. Check load balancer exists
export INFRA_ID=<cluster-infra-id>
aws elbv2 describe-load-balancers \
  --query "LoadBalancers[?contains(LoadBalancerName, '$INFRA_ID')]"

# 3. Check security groups allow traffic
# API LB should allow 6443 from anywhere (or your IP)

# 4. Check cluster control plane nodes running
aws ec2 describe-instances \
  --filters "Name=tag:kubernetes.io/cluster/${INFRA_ID},Values=owned" \
            "Name=tag:Name,Values=*master*" \
  --query 'Reservations[].Instances[].[InstanceId,State.Name]'
```

### 8.2 Cannot Pull Images

**Symptoms:**
```
ERROR: failed to pull image: unauthorized
ERROR: ImagePullBackOff
```

**Solutions:**
```bash
# 1. Verify pull secret is configured
oc get secret pull-secret -n openshift-config -o yaml

# 2. Test pull secret works
podman login --authfile ~/pull-secret.json quay.io
podman pull --authfile ~/pull-secret.json quay.io/openshift-release-dev/ocp-v4.0-art-dev

# 3. If expired, update pull secret
# Download new pull secret from console.redhat.com
# Update cluster secret
oc set data secret/pull-secret \
  -n openshift-config \
  --from-file=.dockerconfigjson=~/pull-secret-new.json
```

---

## 9. Storage and Disk Space Issues

### 9.1 EFS Mount Failures

**Symptoms:**
```
ERROR: failed to mount EFS: mount.nfs4: Connection timed out
ERROR: EFS file system not found
```

**Solutions:**
```bash
# 1. Check EFS file system exists
aws efs describe-file-systems \
  --query "FileSystems[?Tags[?Key=='ClusterID' && Value=='$CLUSTER_ID']]"

# 2. Check security groups allow NFS (port 2049)
aws ec2 describe-security-groups \
  --filters "Name=tag:ClusterID,Values=$CLUSTER_ID"

# 3. Check mount targets in correct subnets
aws efs describe-mount-targets \
  --file-system-id fs-xxxxx

# 4. Test manual mount
sudo mount -t nfs4 \
  -o nfsvers=4.1,rsize=1048576,wsize=1048576,hard,timeo=600,retrans=2 \
  fs-xxxxx.efs.us-east-1.amazonaws.com:/ /mnt/efs-test
```

### 9.2 S3 Upload/Download Failures

**Symptoms:**
```
ERROR: failed to upload kubeconfig to S3: AccessDenied
ERROR: NoSuchBucket
```

**Solutions:**
```bash
# 1. Check S3 bucket exists
aws s3 ls s3://your-bucket-name

# 2. Check IAM permissions
aws iam get-policy \
  --policy-arn <policy-arn> \
  --query 'Policy.DefaultVersionId'

# 3. Test S3 access from worker instance
ssh -i ~/.ssh/key.pem ubuntu@$EC2_IP \
  "aws s3 ls s3://your-bucket-name"

# 4. Create bucket if missing
aws s3 mb s3://your-bucket-name
```

---

## 10. Common Error Messages

### "connection refused"
**Meaning:** Service not running or not listening on expected port
**Check:** `sudo systemctl status <service>`, `sudo netstat -tlnp | grep <port>`

### "permission denied"
**Meaning:** IAM permissions, file permissions, or database permissions
**Check:** IAM policies, `ls -l`, database grants

### "timeout"
**Meaning:** Network issue, firewall, or service overloaded
**Check:** Security groups, instance state, system resources

### "not found"
**Meaning:** Resource doesn't exist (cluster, file, DNS record)
**Check:** Database, AWS resources, filesystem

### "already exists"
**Meaning:** Duplicate resource (cluster name, VPC CIDR overlap)
**Check:** Existing resources, choose different name

### "quota exceeded"
**Meaning:** AWS service limit reached
**Check:** Service quotas, request increase

### "unauthorized"
**Meaning:** Authentication failed
**Check:** Credentials, pull secret, kubeconfig, JWT token

---

## Emergency Procedures

### Service Complete Restart
```bash
# Stop all services
sudo systemctl stop ocpctl-api ocpctl-worker ocpctl-web nginx

# Clear locks and requeue jobs
psql "$DATABASE_URL" << EOF
DELETE FROM job_locks WHERE expires_at < NOW() + INTERVAL '1 hour';
UPDATE jobs SET status='PENDING', attempt=0 WHERE status='RUNNING';
EOF

# Start services
sudo systemctl start postgresql nginx ocpctl-api ocpctl-worker ocpctl-web

# Verify health
curl http://localhost:8080/health
curl http://localhost:8081/health
```

### Database Recovery
```bash
# Stop services
sudo systemctl stop ocpctl-api ocpctl-worker

# Restore from backup
psql "$DATABASE_URL" < /path/to/backup.sql

# Start services
sudo systemctl start ocpctl-api ocpctl-worker
```

### Force Cluster Cleanup
```bash
# Use only if normal destroy fails
export INFRA_ID=<cluster-infra-id>
export VPC_ID=<cluster-vpc-id>

# Delete all cluster resources
scripts/force-cleanup-cluster.sh $INFRA_ID $VPC_ID

# Update database
psql "$DATABASE_URL" -c \
  "UPDATE clusters SET status='DESTROYED', destroyed_at=NOW() WHERE id='$CLUSTER_ID';"
```

---

## Getting Help

### Log Collection
```bash
# Collect all relevant logs
sudo journalctl -u ocpctl-api --since '1 hour ago' > /tmp/api.log
sudo journalctl -u ocpctl-worker --since '1 hour ago' > /tmp/worker.log
sudo dmesg > /tmp/dmesg.log
df -h > /tmp/disk.log
free -h > /tmp/memory.log

# Package logs
tar -czf ocpctl-logs-$(date +%s).tar.gz /tmp/*.log

# Share logs (remove sensitive data first!)
```

### Useful Diagnostic Commands
```bash
# Full system status
sudo systemctl status ocpctl-api ocpctl-worker postgresql nginx

# Recent errors across all services
sudo journalctl --since '1 hour ago' --priority err

# Database health
psql "$DATABASE_URL" -c "SELECT * FROM pg_stat_activity;"

# Disk and memory
df -h
free -h
top -bn1 | head -20

# Network connections
sudo netstat -tlnp
```

---

**Document Version:** 1.0
**Last Updated:** 2026-05-08
**Feedback:** Report issues to GitHub or team Slack
