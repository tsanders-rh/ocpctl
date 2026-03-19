# Deployment Scripts

## Web Frontend Deployment

### Quick Start

```bash
./scripts/deploy-web.sh
```

This script will:
1. Build the Next.js app (`npm run build`)
2. Create a deployment tarball
3. Upload to production server
4. Deploy to `/opt/ocpctl/web` (the correct production path)
5. Fix ownership for the `ocpctl` user
6. Clear Next.js cache
7. Restart the `ocpctl-web` service

### Prerequisites

- SSH key at `~/.ssh/ocpctl-test-key.pem`
- Access to production server (52.90.135.148)
- Node.js and npm installed locally

### Production Configuration

**IMPORTANT:** The web service runs from these locations:

```
Production Path:  /opt/ocpctl/web
Service User:     ocpctl
Service File:     /etc/systemd/system/ocpctl-web.service
```

**DO NOT** manually deploy to `/home/ec2-user/ocpctl-web` - that's not where the service runs!

### Manual Deployment (Not Recommended)

If you need to deploy manually:

```bash
# 1. Build
cd web && npm run build

# 2. Create tarball
tar -czf /tmp/web-deploy.tar.gz .next package.json package-lock.json next.config.mjs

# 3. Upload
scp -i ~/.ssh/ocpctl-test-key.pem /tmp/web-deploy.tar.gz ec2-user@52.90.135.148:/tmp/

# 4. Deploy (SSH to server)
ssh -i ~/.ssh/ocpctl-test-key.pem ec2-user@52.90.135.148
sudo tar -xzf /tmp/web-deploy.tar.gz -C /opt/ocpctl/web
sudo chown -R ocpctl:ocpctl /opt/ocpctl/web
sudo systemctl restart ocpctl-web
```

### Troubleshooting

**Check service status:**
```bash
ssh -i ~/.ssh/ocpctl-test-key.pem ec2-user@52.90.135.148 \
  'sudo systemctl status ocpctl-web'
```

**Check logs:**
```bash
ssh -i ~/.ssh/ocpctl-test-key.pem ec2-user@52.90.135.148 \
  'sudo journalctl -u ocpctl-web -f'
```

**Verify deployment location:**
```bash
ssh -i ~/.ssh/ocpctl-test-key.pem ec2-user@52.90.135.148 \
  'cat /etc/systemd/system/ocpctl-web.service | grep WorkingDirectory'
```

**Check what's actually deployed:**
```bash
ssh -i ~/.ssh/ocpctl-test-key.pem ec2-user@52.90.135.148 \
  'ls -lh /opt/ocpctl/web/.next/BUILD_ID'
```

### Backup Strategy

The deployment script automatically backs up the previous `.next` directory:

```
/opt/ocpctl/web/.next.backup-YYYYMMDD-HHMMSS
```

To restore a previous version:

```bash
ssh -i ~/.ssh/ocpctl-test-key.pem ec2-user@52.90.135.148
sudo rm -rf /opt/ocpctl/web/.next
sudo mv /opt/ocpctl/web/.next.backup-YYYYMMDD-HHMMSS /opt/ocpctl/web/.next
sudo systemctl restart ocpctl-web
```

### Testing Deployment Locally

Before deploying to production, test that the build works:

```bash
cd web
npm run build
npm start
# Visit http://localhost:3000 and verify changes
```
