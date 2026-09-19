# OCPCTL Troubleshooting Guide

**Purpose:** Comprehensive troubleshooting guide for common ocpctl deployment and operational issues.

**Audience:** Operators, administrators, and users experiencing issues with ocpctl.

**Last Updated:** 2026-09-19

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
├─ Database Issues → Section 7
└─ Orphaned resources / auto-remediation → Section 11
```

**Deployment-specific context this guide assumes:** binaries run from
`/opt/ocpctl/current` (a symlink to a versioned release dir); both environments
use RDS, not a local PostgreSQL; production processes jobs on an autoscaling
worker fleet whose instances are rebuilt from S3 on every boot (section 3.5);
and dev shares production's AWS account, which matters for orphan detection
(section 11).

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
11. [Orphaned Resources and Auto-Remediation](#11-orphaned-resources-and-auto-remediation)

---

## 1. Deployment and Installation Issues

### 1.1 Cannot SSH to EC2 Instance

**Symptoms:**
```
ssh: connect to host <PROD_HOST> port 22: Connection timed out
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
scp: /opt/ocpctl/releases/v0.YYYYMMDD.xxxx/ocpctl-api: Permission denied
```

**Layout you are deploying into.** Binaries do **not** live in `/opt/ocpctl/bin`.
Each deploy installs into a versioned directory and flips a symlink, which is
what the systemd units point at:

```
/opt/ocpctl/releases/v0.YYYYMMDD.HASH/ocpctl-api    # the actual binary
/opt/ocpctl/current -> releases/v0.YYYYMMDD.HASH    # symlink, flipped on deploy
```

`ExecStart=/opt/ocpctl/current/ocpctl-api` (see `deploy/systemd/*.service`).
Installing a binary anywhere else — `/opt/ocpctl/bin` included — leaves the
running service on the old version and breaks rollback, which works by pointing
`current` at an older release directory.

**Common Causes:**
- Release directory doesn't exist yet
- Uploading straight to a root-owned path instead of staging via `/tmp`
- Not using sudo when needed

**Solutions:**
```bash
# Prefer the deploy script, which does all of this correctly:
./scripts/deploy-env.sh dev

# If you must place a binary by hand, mirror what the script does:
export VERSION=v0.YYYYMMDD.xxxx
ssh -i "$SSH_KEY" ubuntu@$EC2_IP "sudo mkdir -p /opt/ocpctl/releases/$VERSION"
scp -i "$SSH_KEY" bin/ocpctl-api-$VERSION ubuntu@$EC2_IP:/tmp/
ssh -i "$SSH_KEY" ubuntu@$EC2_IP \
  "sudo install -m 755 /tmp/ocpctl-api-$VERSION /opt/ocpctl/releases/$VERSION/ocpctl-api && \
   sudo ln -snf /opt/ocpctl/releases/$VERSION /opt/ocpctl/current && \
   sudo systemctl restart ocpctl-api"
```

### 1.3 PostgreSQL Installation Fails (local/standalone installs only)

> Dev and production use **RDS** — you do not install PostgreSQL on those hosts.
> This applies only to a local or standalone deployment.

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

> **Migrations are applied by the API binary on startup**, not by a separate
> command — `cmd/api/main.go` calls `store.Migrate()`, which records applied
> versions in `schema_migrations`. Deploying the API is what migrates dev and
> prod. Do not point the `goose` CLI (`make migrate-up`) at those databases: it
> uses a different bookkeeping table and would try to re-apply every migration.
> See [DEVOPS.md section 5](../development/DEVOPS.md#5-database-migrations).

**Symptoms:**
```
ERROR: relation "schema_migrations" does not exist
ERROR: permission denied for database postgres
```

**Symptom: a migration you know exists never ran, with no error.**
The runner keys on the numeric filename prefix and skips any version already
recorded, so **two files sharing a prefix mean the second is silently never
applied**. Compare what shipped against what was recorded:
```bash
psql "$DATABASE_URL" -c "SELECT version FROM schema_migrations ORDER BY version DESC LIMIT 5;"
ls internal/store/migrations/ | tail -5
```
`internal/store/migrations_test.go` fails the build on duplicate prefixes, so
this should not reach a deployed binary — but a database migrated by an older
binary can still be missing the shadowed migration.

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

**If database doesn't exist** (local/standalone only — on RDS the database and
master user are created by Terraform):
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

# 3. Manually run binary to see error (loads the same env the unit does)
sudo -u ocpctl env $(sudo cat /etc/ocpctl/api.env | grep -v '^#' | xargs) \
  /opt/ocpctl/current/ocpctl-api
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
  /opt/ocpctl/current/ocpctl-worker
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

> The frontend is **built on your machine**, not on the server:
> `scripts/deploy-web.sh` runs `npm install`, lint and `npm run build` locally,
> ships a package, and then runs only `npm install --production` on the host.
> Because devDependencies are absent there, building in place will not work.

**Missing dependencies or missing build:**
```bash
# Re-run the real deploy from your workstation -- this is the fix for both.
./scripts/deploy-web.sh dev          # or: production
```

**Port in use:**
```bash
sudo netstat -tlnp | grep 3000
# Kill process or change web port
```

**Check what is actually deployed:**
```bash
ls -la /opt/ocpctl/web/.next/ && sudo systemctl status ocpctl-web
sudo journalctl -u ocpctl-web -n 50 --no-pager
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

### 3.5 Failures That Only Happen on Autoscale Workers

**Symptom shape:** a cluster create works on the static worker host but fails on
an ASG worker, or every create started after a scale-out fails the same way.

Autoscale workers do **not** share the static host's filesystem. They boot from
the Terraform launch-template user-data
(`terraform/worker-autoscaling/user-data.sh`), which re-downloads the binary,
profiles, manifests, hook scripts and `worker.env` from S3 and regenerates the
systemd unit on every boot. Anything that directory layout or those hooks get
wrong reappears on every new instance.

**First: confirm which worker ran the job.**
```bash
psql "$DATABASE_URL" -c \
  "SELECT id, locked_by, expires_at FROM job_locks WHERE cluster_id='$CLUSTER_ID';"
# locked_by identifies the worker; ASG instances are tagged Name=ocpctl-worker
aws ec2 describe-instances --filters "Name=tag:Name,Values=ocpctl-worker" \
  "Name=instance-state-name,Values=running" \
  --query 'Reservations[].Instances[].[InstanceId,LaunchTime,PrivateIpAddress]'
```

**Known failure modes on this path:**

| Error | Cause | Fix |
|-------|-------|-----|
| `failed to create tmp file for bootstrap ignition: /var/lib/ocpctl/tmp/...: no such file or directory` | `/var/lib/ocpctl` owned by root, so the `ocpctl` user cannot create `TMPDIR` alongside `clusters/` | user-data now creates `tmp/` and chowns the **parent**. Unblock in place: `sudo mkdir -p /var/lib/ocpctl/tmp && sudo chown -R ocpctl:ocpctl /var/lib/ocpctl` |
| `creating Azure session: failed to retrieve credentials from user: EOF` | `~/.azure/osServicePrincipal.json` missing, so `openshift-install` prompted interactively | `scripts/azure-login.sh` writes it after `az login`; verify the file exists, is owned by `ocpctl`, mode 600 |
| `script not found at path /opt/ocpctl/manifests/...` | `manifests/` was never synced to the instance, so addon tasks that run a manifest script fail | user-data now `aws s3 sync`s `manifests/`; confirm `/opt/ocpctl/manifests` is populated |

**Checking a live ASG worker:**
```bash
# Did user-data finish, and what did it do?
sudo tail -100 /var/log/cloud-init-output.log
ls -ld /var/lib/ocpctl /var/lib/ocpctl/tmp /var/lib/ocpctl/clusters   # all ocpctl:ocpctl
sudo ls -l /opt/ocpctl/manifests | head
sudo -u ocpctl ls -l /opt/ocpctl/.azure/osServicePrincipal.json
```

> **Where the fix belongs.** Hook scripts (`azure-login.sh`, `ibmcloud-login.sh`,
> `ensure-installers.sh`) are pulled fresh from S3 each boot, so editing the repo
> script and running `deploy.sh` is enough — it also recycles the workers.
> Anything in the **systemd unit, directory layout or boot steps** lives in the
> launch template and needs `terraform apply` in `terraform/worker-autoscaling/`;
> `deploy.sh` does **not** apply Terraform. Editing
> `scripts/bootstrap-worker.sh` reaches nothing — it is the legacy manual-AMI
> path. This has bitten the same way three times; see DEVOPS.md section 4.

### 3.6 Nightly / Prerelease Install Failures

These affect `track: prerelease` and nightly versions only — a GA install of the
same profile succeeding does not rule them out.

**Node never joins, bootstrap times out, MCD rejects the image:**
Nightly release images in `registry.ci` are unsigned, while nodes enforce
sigstore verification on release repos. The installer injects a permissive
`policy.json` via MachineConfig for prerelease installs
(`internal/installer/signature_policy.go`). If you see signature rejections in
the node journal, check that the cluster was actually created on a prerelease
track and that the policy MachineConfig rendered.

**`manifest unknown` when downloading the installer:**
5.x nightlies live in `registry.ci` under `ocp/release-<major>` (e.g.
`ocp/release-5`), not the 4.x `ocp/release`. Version resolution is major-aware;
a wrong-repo lookup surfaces as `manifest unknown`.

**`unauthorized` pulling from registry.ci:**
The registry.ci pull secret is an OAuth token with a **28-day lifetime** and it
expires silently — every nightly install starts failing at once.
```bash
./scripts/check-ci-pull-secret-expiry.sh        # reports days remaining
./scripts/refresh-ci-pull-secret.sh             # runbook: docs/operations/CI_PULL_SECRET_REFRESH.md
```
Refreshing is a manual chore. The token in use at the time of writing expires
**2026-10-17**; the refresh must be applied to dev, prod **and** the S3 copy the
ASG workers read.

### 3.7 Service Quota Exceeded

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

**Hibernation is platform-specific.** How the worker stops a cluster depends on
where it runs, so check you are looking at the right mechanism:

| Platform | Mechanism | Where to look |
|----------|-----------|---------------|
| AWS OpenShift IPI | Stop EC2 instances by `kubernetes.io/cluster/<infraID>` tag | EC2 console / CLI below |
| Azure OpenShift IPI | `az vm deallocate` across the cluster resource group | `az vm list -g <infraID>-rg -d` |
| IBM Cloud | Stop VSIs whose name matches the infraID prefix | `ibmcloud is instances` |
| EKS / GKE | Scale node groups / node pools to 0 | Control plane keeps running |

> Azure and IBM OpenShift hibernate/resume were added in #147. For Azure the
> resource group recorded in installer metadata is empty for installer-managed
> clusters, so the code falls back to `<infraID>-rg` — if hibernate reports "no
> resource group", that fallback is what to check. IBM requires the
> `vpc-infrastructure` ibmcloud CLI plugin on the worker.

**Solutions:**
```bash
# 1. Confirm the cluster's platform and that hibernation applies
psql "$DATABASE_URL" -c \
  "SELECT name, platform, cluster_type, status FROM clusters WHERE id='$CLUSTER_ID';"

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

### 4.5 Post-Configure / Addon Failures

**Symptoms:**
- Cluster reaches READY, then the POST_CONFIGURE job fails
- Addon (CNV, MTA, MTC, OADP) never becomes available
- The cluster itself is fine — only post-deployment failed

A POST_CONFIGURE job runs after a cluster reaches READY. Failures here are
addon-level, not install-level, so `.openshift_install.log` will tell you
nothing.

**Where to look first.** The failing task's error is surfaced in the UI —
the **Logs** tab streams the task error, and the **Jobs** card renders
`job.error_message`. From the database:
```bash
psql "$DATABASE_URL" -c \
  "SELECT job_type, status, error_message FROM jobs
    WHERE cluster_id='$CLUSTER_ID' AND job_type='POST_CONFIGURE'
    ORDER BY created_at DESC LIMIT 1;"
```

**Common causes:**

| Symptom | Cause | Fix |
|---------|-------|-----|
| Addon pods rejected: `violates PodSecurity "restricted:latest"` | The addon's manifests have no `securityContext` and land in a namespace with restricted PSA enforcement | Set baseline PSA on the addon's namespace, or add a compliant `securityContext`. ocpctl does **not** validate addon manifests against PSA — a bad manifest fails only at apply time |
| CNV catalog pod `ImagePullBackOff` / CNV post-config fails | Cluster pull secret lacks a `quay.io/openshift-cnv` entry; the nightly/stable-stage catalogs pull `quay.io/openshift-cnv/nightly-catalog:*` | Restore the `quay.io/openshift-cnv` credential in the pull secret used for the cluster |
| Operator never goes Available, times out in the CSV phase | Operator install genuinely stalled | The timeout message names the phase it was waiting in; check `oc get csv -n <addon-namespace>` |

**Re-running post-configuration:**
```bash
psql "$DATABASE_URL" -c \
  "UPDATE jobs SET status='PENDING', attempt=0
    WHERE cluster_id='$CLUSTER_ID' AND job_type='POST_CONFIGURE';"
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
# Vacuum/analyze first -- stale planner statistics are the usual cause
psql "$DATABASE_URL" -c "VACUUM ANALYZE;"

# Confirm the expected indexes are present (they ship in migrations, so a
# missing one means migrations did not fully apply -- see 1.4, do NOT hand-create)
psql "$DATABASE_URL" -c "\di idx_clusters*"
psql "$DATABASE_URL" -c "\di idx_jobs*"
```

> Indexes on `clusters(status)`, `clusters(owner)`, `jobs(cluster_id)` and
> `jobs(status)` have existed since migration `00001`; `00034` and `00040` add
> composite indexes for stuck-job detection and the job queue. If a query is
> slow, the fix is a new migration, not ad-hoc DDL on a live database.

**High memory usage:**
```bash
# Restart services
sudo systemctl restart ocpctl-api ocpctl-worker

# If persistent, upgrade instance
# t3.medium → t3.large
```

**Hitting the rate limiter:**
```bash
# Symptom is HTTP 429, not slowness. The limit is 300 requests/minute per
# client (internal/api/server.go, RateLimitRequests), with tighter per-route
# limits on auth (login 5/min, logout and refresh 10/min).
sudo journalctl -u ocpctl-api --since '15 minutes ago' | grep -i "rate limit"
```

> There is **no `RATE_LIMIT_REQUESTS` environment variable** — the value is a
> compile-time default in `internal/api/server.go`. Changing it requires a code
> change and a deploy; adding it to `api.env` has no effect.

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

# Check how many jobs each worker will run at once (logged at startup)
sudo journalctl -u ocpctl-worker | grep "max_concurrent" | tail -1
```

**Solutions:**

**Add worker capacity (the supported lever):**
```bash
# Production scales horizontally via the ocpctl-worker-asg ASG. Raise desired
# capacity; new instances boot from the Terraform launch template and pull the
# current binary, profiles and worker.env from S3.
aws autoscaling set-desired-capacity \
  --auto-scaling-group-name ocpctl-worker-asg --desired-capacity 3
```

> **Per-worker concurrency is not configurable at runtime.** `MaxConcurrent` is
> a compile-time default of 3 (`internal/worker/worker.go`); `cmd/worker/main.go`
> takes `worker.DefaultConfig()` and overrides only `WorkDir`. Neither
> `WORKER_CONCURRENCY` nor `CONCURRENCY` is read by anything — setting either in
> `worker.env` is a no-op. Changing it means changing the code. Scale out
> instead.

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
# If accessing via IP: http://<PROD_HOST>
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
# IAM auth is SigV4 REQUEST SIGNING, not a login endpoint: you sign a normal
# API call with your AWS credentials and the RequireAuthDual middleware
# verifies the signature. There is no endpoint that accepts an access key in a
# request body -- never POST a secret access key anywhere.
aws configure export-credentials --format env > /tmp/creds && . /tmp/creds
curl --aws-sigv4 "aws:amz:us-east-1:execute-api" \
  --user "$AWS_ACCESS_KEY_ID:$AWS_SECRET_ACCESS_KEY" \
  -H "x-amz-security-token: $AWS_SESSION_TOKEN" \
  http://localhost:8080/api/v1/clusters
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

> **Both environments use RDS.** Dev and production each point at their own RDS
> instance (`ocpctl-dev-db` / `ocpctl-db`); there is no PostgreSQL server running
> on the API host, so `systemctl start postgresql` and `sudo -u postgres psql`
> do not apply. `psql` is installed on the hosts purely as a client.

**Symptoms:**
```
ERROR: dial tcp: connect: connection refused
ERROR: pq: password authentication failed
```

**Solutions:**

**Check reachability first:**
```bash
# From the API/worker host. Endpoint for each env: config/environments.sh ($RDS_HOST)
psql "$DATABASE_URL" -c "SELECT version();"

# If it hangs rather than refusing, it is the security group, not credentials:
# the RDS SG must allow 5432 from the API/worker host and from the worker ASG.
aws rds describe-db-instances --db-instance-identifier ocpctl-db \
  --query 'DBInstances[0].[DBInstanceStatus,Endpoint.Address,VpcSecurityGroups]'
```

**Wrong password:**
```bash
# Credentials live in the DATABASE_URL inside the env files, which come from
# config/{api,worker}.env.<env> at deploy time (and s3://<binaries>/config/worker.env
# for ASG workers). Rotating means updating BOTH the handover bundle and the S3
# runtime copy -- see docs/operations/OWNERSHIP_HANDOVER.md.
aws rds modify-db-instance --db-instance-identifier ocpctl-db \
  --master-user-password '<new-password>' --apply-immediately
```

**Wrong hostname:**
```bash
# Should be the RDS endpoint, never localhost.
sudo grep -o '@[^/]*' /etc/ocpctl/api.env
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

## 11. Orphaned Resources and Auto-Remediation

The janitor periodically lists cloud resources and compares them against the
`clusters` table; anything it cannot attribute to a known cluster is recorded as
an **orphaned resource** (`internal/janitor/orphan_detector.go`). Detection runs
on its own interval (15 minutes, less often than the 5-minute janitor tick, to
stay under cloud API rate limits).

### 11.1 The safety property you must understand first

Detection is judged **against the environment's own database**. An environment
that shares a cloud account with another deployment will therefore classify the
other deployment's **live** infrastructure as orphaned. This is exactly the case
here: dev and production share AWS account `346869059911`, and dev has no live
clusters of its own, so dev's janitor sees production's live resources as
orphans. Dev's orphan count running into the thousands while prod's is in the
hundreds is expected, not a bug.

**Because of that, `on` mode is interlocked.** Real deletion requires
`ORPHAN_AUTO_DELETE_ALLOW_ON=true|1|yes` in the environment
(`internal/orphan/autodelete.go`, `AutoDeleteOnAllowed()`):

- The API refuses to save mode `on` with **403** when it is unset
  (`PUT /api/v1/admin/orphaned-resources/auto-remediation`).
- The janitor independently **clamps `on` → `dryrun`** at runtime, so an
  already-saved setting or a stale `ORPHAN_AUTO_DELETE=on` in `worker.env`
  still cannot delete.

Arming an environment means setting the flag on **both** the API host (to save
the setting) and the worker/janitor hosts (to act on it). **Leave it unset on
dev.** If you see `mode 'on' requested but ORPHAN_AUTO_DELETE_ALLOW_ON is not
set ... clamping to dryrun` in the worker journal, the interlock is doing its
job.

### 11.2 Modes

`off` (no detection action) · `dryrun` (log what would be deleted) · `on` (delete
for real, subject to the interlock above plus the per-resource safety and
ownership gates). Read or change it in the admin console, or:

```bash
curl -s .../api/v1/admin/orphaned-resources/auto-remediation     # current mode
sudo journalctl -u ocpctl-worker | grep orphan-auto-delete | tail -20
```

### 11.3 Live cluster resources being flagged as orphans

**Symptom:** load balancers, EBS volumes or EIPs belonging to a healthy,
READY cluster show up as ACTIVE orphans.

`openshift-install` truncates names longer than 21 characters when deriving the
infraID, so a long cluster name produces resource names that no longer match the
cluster name by prefix. Matching is truncation-aware; if you still see
false positives, compare the actual infraID against the cluster name:

```bash
oc get infrastructure cluster -o jsonpath='{.status.infrastructureName}'
psql "$DATABASE_URL" -c "SELECT name FROM clusters WHERE id='$CLUSTER_ID';"
```

Do not delete a flagged resource without confirming its cluster is genuinely
gone. Mark it resolved instead:
`PATCH /api/v1/admin/orphaned-resources/:id/resolve`.

### 11.4 Orphan records that never clear

**Symptom:** ACTIVE orphan rows stuck at `detection_count=1` that are no longer
actually present in the cloud.

A minimum-detections gate means a record detected once and never again can never
advance, so it sits ACTIVE forever. The janitor auto-resolves these: an
age-based sweep resolves ACTIVE records that are no longer re-detected, gated on
the per-cloud detection having **succeeded** (so a failed scan is never read as
"everything vanished"). Tunable via `ORPHAN_STALE_RESOLVE_HOURS` (default 2).

```bash
sudo journalctl -u ocpctl-worker | grep orphan-reconcile | tail -20
```

This sweep is database-only — it never touches cloud resources.

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

# Start services (no local postgresql -- the database is RDS)
sudo systemctl start nginx ocpctl-api ocpctl-worker ocpctl-web

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

Use only if a normal destroy has failed and 4.4 did not clear it. There is no
force-cleanup script in the repo — do this deliberately, by hand.

```bash
export INFRA_ID=<cluster-infra-id>   # oc get infrastructure cluster -o jsonpath='{.status.infrastructureName}'

# 1. Prefer re-running the installer's own destroy, which knows the dependency
#    order. Fetch the cluster's installer dir from S3 first:
aws s3 sync s3://ocpctl-artifacts/clusters/$CLUSTER_ID/ /tmp/$CLUSTER_ID/
openshift-install destroy cluster --dir /tmp/$CLUSTER_ID

# 2. Only if that fails, delete by tag in dependency order (LBs and NAT
#    gateways before subnets/VPC) using the queries in section 4.4.

# 3. Update the database LAST, once the cloud side is actually clean.
psql "$DATABASE_URL" -c \
  "UPDATE clusters SET status='DESTROYED', destroyed_at=NOW() WHERE id='$CLUSTER_ID';"
```

> Marking the row DESTROYED before the resources are gone is how you create
> orphans: the janitor matches live cloud resources against the `clusters` table,
> so anything left behind becomes an untracked orphaned resource (section 11).

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

**Document Version:** 1.1
**Last Updated:** 2026-09-19
**Feedback:** Report issues to GitHub or team Slack
