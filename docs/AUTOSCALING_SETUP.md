# Autoscaling Worker Setup

## Problem Statement

When AWS autoscaling launches new worker instances, they need to:
1. Download the correct version of `ocpctl-worker`
2. Configure themselves with the right environment
3. Start successfully and register with the database
4. Match the version running on existing workers

## Solution Overview

We use a **hybrid approach**:
- Base AMI has structure and bootstrap tooling
- User-data pulls specific version from S3
- S3 stores versioned binaries and config

## Architecture

```
/opt/ocpctl/
  releases/
    v0.20260314.2d1d41b/
      ocpctl-worker          # Downloaded from S3 on first boot
    v0.20260315.abc5678/
      ocpctl-worker
  current -> releases/v0.20260314.2d1d41b  # Symlink
  bootstrap.sh             # Pre-installed in AMI

/etc/ocpctl/
  worker.env               # Downloaded from S3 on first boot

/etc/systemd/system/
  ocpctl-worker.service    # Downloaded from S3 on first boot
```

## Setup Steps

### 1. Upload Bootstrap Artifacts to S3

```bash
chmod +x scripts/upload-bootstrap-artifacts.sh
./scripts/upload-bootstrap-artifacts.sh
```

This uploads:
- `bootstrap-worker.sh` → Used by instances to download versions
- `ocpctl-worker.service` → Systemd service file
- `worker.env` → Environment configuration

### 2. Deploy New Version

```bash
./scripts/deploy.sh
```

This:
- Builds versioned binaries with metadata
- Uploads worker binary to S3 at `s3://ocpctl-binaries/releases/${VERSION}/`
- Updates `s3://ocpctl-binaries/LATEST` pointer
- Deploys to existing workers (main + autoscaling)

### 3. Configure Launch Template

**Option A: User-Data (Easiest)**

Update your EC2 launch template with this user-data:

```bash
#!/bin/bash
curl -s https://raw.githubusercontent.com/tsanders-rh/ocpctl/main/scripts/user-data-worker.sh | bash
```

Or copy the contents of `scripts/user-data-worker.sh` directly.

**Option B: Custom AMI (Most Reliable)**

1. Launch a worker instance
2. SSH in and run:
   ```bash
   # Install bootstrap script
   sudo mkdir -p /opt/ocpctl
   sudo aws s3 cp s3://ocpctl-binaries/scripts/bootstrap-worker.sh /opt/ocpctl/bootstrap.sh
   sudo chmod +x /opt/ocpctl/bootstrap.sh

   # Install service file
   sudo aws s3 cp s3://ocpctl-binaries/scripts/ocpctl-worker.service /etc/systemd/system/ocpctl-worker.service
   sudo systemctl daemon-reload

   # Create directory structure
   sudo mkdir -p /opt/ocpctl/releases
   sudo mkdir -p /var/lib/ocpctl/{clusters,tmp}
   sudo mkdir -p /etc/ocpctl

   # Create user
   sudo useradd -r -s /bin/false ocpctl
   sudo chown -R ocpctl:ocpctl /var/lib/ocpctl /opt/ocpctl
   ```

3. Create AMI from this instance:
   ```bash
   aws ec2 create-image \
     --instance-id i-xxx \
     --name "ocpctl-worker-base-$(date +%Y%m%d)" \
     --description "Base AMI for OCPCTL workers with bootstrap tooling"
   ```

4. Update launch template to use new AMI with simple user-data:
   ```bash
   #!/bin/bash
   # Download config (contains DATABASE_URL, OPENSHIFT_PULL_SECRET, etc.)
   aws s3 cp s3://ocpctl-binaries/config/worker.env /etc/ocpctl/worker.env
   chmod 600 /etc/ocpctl/worker.env

   # Bootstrap with latest version
   /opt/ocpctl/bootstrap.sh latest

   # Start service
   systemctl enable ocpctl-worker
   systemctl start ocpctl-worker
   ```

## Verification

### Test Bootstrap on Existing Instance

```bash
# SSH to autoscaling worker
ssh -i ~/.ssh/ocpctl-test-key.pem ec2-user@54.235.4.38

# Run bootstrap manually
sudo /opt/ocpctl/bootstrap.sh latest

# Check version
curl http://localhost:8081/version

# Check service
sudo systemctl status ocpctl-worker
```

### After Autoscaling Event

When a new instance launches, verify:

```bash
# Get new instance IP from AWS console
NEW_INSTANCE_IP="x.x.x.x"

# SSH and check
ssh -i ~/.ssh/ocpctl-test-key.pem ec2-user@${NEW_INSTANCE_IP}

# Check bootstrap log
sudo tail -100 /var/log/ocpctl-bootstrap.log

# Check version
curl http://localhost:8081/version

# Should match deployed version
```

## Version Management

### Deploy New Version

```bash
./scripts/deploy.sh
```

This automatically:
1. Builds with new version
2. Uploads to S3
3. Updates existing workers
4. New autoscaling instances will get the new version

### Pin to Specific Version

```bash
# Deploy specific version to existing workers
./scripts/deploy.sh v0.20260314.abc1234

# For new instances, update LATEST pointer manually
echo "v0.20260314.abc1234" | aws s3 cp - s3://ocpctl-binaries/LATEST
```

### Rollback

```bash
# Existing workers
./scripts/deploy.sh v0.20260313.old5678

# New instances (update LATEST)
echo "v0.20260313.old5678" | aws s3 cp - s3://ocpctl-binaries/LATEST
```

## Troubleshooting

### New Instance Fails to Start

```bash
# SSH to instance
ssh -i ~/.ssh/ocpctl-test-key.pem ec2-user@${INSTANCE_IP}

# Check bootstrap log
sudo tail -100 /var/log/ocpctl-bootstrap.log

# Check service logs
sudo journalctl -u ocpctl-worker -n 100 --no-pager

# Manually run bootstrap
sudo /opt/ocpctl/bootstrap.sh latest

# Check what version is available in S3
aws s3 ls s3://ocpctl-binaries/LATEST
aws s3 ls s3://ocpctl-binaries/releases/
```

### Version Mismatch

```bash
# Check all workers
for host in 52.90.135.148 54.235.4.38; do
  echo "=== $host ==="
  ssh -i ~/.ssh/ocpctl-test-key.pem ec2-user@$host 'curl -s http://localhost:8081/version | jq'
done

# Check S3 version
aws s3 cp s3://ocpctl-binaries/LATEST -
```

## Production Checklist

- [ ] Bootstrap script uploaded to S3
- [ ] Service file uploaded to S3
- [ ] Worker config uploaded to S3 (encrypted if contains secrets)
- [ ] Launch template updated with user-data
- [ ] OR Custom AMI created with bootstrap tooling
- [ ] Test manual bootstrap on existing instance
- [ ] Test autoscaling scale-out event
- [ ] Verify new instance gets correct version
- [ ] Set up CloudWatch alarm for version mismatches
- [ ] Document rollback procedure for team

## Future: Container-Based Autoscaling

For Phase 2 (containers), consider:
- ECS with Fargate (simplest, fully managed)
- ECS with EC2 (more control)
- EKS (if already using Kubernetes)

Container benefits:
- Immutable deployments
- No bootstrap complexity
- Fast rollout/rollback
- Better resource utilization
