# Disk Space Management

## Overview

OCPCTL EC2 instances can accumulate disk usage from:
1. **Binary releases** (~174M each, stored in `/opt/ocpctl/releases/`)
2. **OpenShift installer binaries** (~600M each, stored in `/usr/local/bin/`)
3. **Web frontend** (~3.1G in `/opt/ocpctl/web`)
4. **Systemd journal logs** (grows over time in `/var/log/journal`)
5. **Build artifacts** in home directory (npm cache, Next.js builds)

## Cluster Artifact Cleanup

✅ **Already Automated** - Cluster working directories are automatically cleaned up when clusters are destroyed:
- Location: `/tmp/ocpctl/<cluster-id>/`
- Cleaned up in: `internal/worker/handler_destroy.go:659-667`
- No manual intervention needed

## Automated Cleanup

### Setup Weekly Cleanup

Run once to install automated weekly cleanup:

```bash
# Install cron job (runs every Sunday at 2 AM)
sudo /opt/ocpctl/scripts/install-disk-cleanup-cron.sh
```

This will:
- Keep only last 5 binary releases
- Keep only last 2 web backups
- Rotate journal logs to 7 days
- Clean npm cache

### Verify Cron Job

```bash
# Check installed cron jobs
sudo crontab -l

# Check cleanup logs
sudo tail -f /var/log/ocpctl-cleanup.log
```

## Manual Cleanup

### Run Cleanup Immediately

```bash
sudo /opt/ocpctl/scripts/cleanup-disk-space.sh
```

### Check Disk Usage

```bash
# Overall disk usage
df -h /

# Top disk consumers
du -sh /opt/ocpctl/* | sort -h

# Large files
sudo du -ah / 2>/dev/null | sort -h | tail -20
```

## Cleanup Details

### 1. Binary Releases (4.1G → ~900M)

Keeps only the 5 most recent releases:
- Location: `/opt/ocpctl/releases/`
- Each release: ~174M (API + Worker binaries)
- Retention: 5 most recent versions
- Old versions removed automatically

**Manual cleanup:**
```bash
# List all releases
ls -lt /opt/ocpctl/releases/

# Remove specific old version
sudo rm -rf /opt/ocpctl/releases/v0.20260401.3689502
```

### 2. Web Backups (~100M)

Keeps only the 2 most recent web backups:
- Location: `/opt/ocpctl/web.backup.*`
- Created during deployments
- Retention: 2 most recent backups

**Manual cleanup:**
```bash
# List backups
ls -lt /opt/ocpctl/web.backup.*

# Remove old backup
sudo rm -rf /opt/ocpctl/web.backup.20260315-144740
```

### 3. Systemd Journal Logs (927M → ~100M)

Rotates logs older than 7 days:
- Location: `/var/log/journal`
- Default: unlimited retention
- After cleanup: 7 days

**Manual cleanup:**
```bash
# Check journal size
journalctl --disk-usage

# Vacuum to 7 days
sudo journalctl --vacuum-time=7d

# Or vacuum to specific size
sudo journalctl --vacuum-size=100M
```

### 4. Home Directory Artifacts (~580M)

Cleans up build artifacts:
- npm cache (`~/.npm`)
- Next.js builds (`~/ocpctl/web/.next`)
- Old ocpctl directories

**Manual cleanup:**
```bash
# Clean npm cache
npm cache clean --force

# Remove Next.js build cache
rm -rf ~/ocpctl/web/.next

# Remove old ocpctl directories
rm -rf ~/ocpctl-web
```

### 5. OpenShift Installer Binaries (1.8G → 600M)

**Manual cleanup only** (not automated for safety):
```bash
# Check current version in use
readlink -f /usr/local/bin/openshift-install

# List all versions
ls -lh /usr/local/bin/openshift-install*

# Remove old versions (example)
sudo rm /usr/local/bin/openshift-install-4.18
sudo rm /usr/local/bin/openshift-install-4.19
```

⚠️ **Warning:** Only remove installer versions you're certain are not in use by any profiles.

## Disk Space Monitoring

### Set Up Alerts

Add to cron for daily disk space checks:
```bash
# Add to root crontab
0 6 * * * df -h / | awk '$NF=="/" && $5+0>80 {print "WARNING: Disk usage at "$5}' | mail -s "Disk Space Alert" admin@example.com
```

### Check Before Deployment

The deploy script automatically shows disk usage:
```bash
./scripts/deploy.sh
# Shows disk usage during deployment
```

## Recommendations

### For Test/Dev Instances (30G disk)
- Run cleanup weekly (automated via cron)
- Keep 5 releases (current + 4 rollback options)
- Keep 7 days of logs
- Monitor disk usage monthly

### For Production Instances (50G+ disk recommended)
- Run cleanup monthly (or keep more releases)
- Keep 10 releases for longer rollback window
- Keep 30 days of logs
- Monitor disk usage weekly
- Consider log aggregation (CloudWatch, Splunk)

### Disk Size Recommendations

| Instance Type | Workload | Recommended Disk | Notes |
|---------------|----------|------------------|-------|
| Test/Dev | Light (<5 clusters/day) | 30G | Sufficient with weekly cleanup |
| Production | Medium (10-20 clusters/day) | 50G | More headroom for logs and releases |
| High Volume | Heavy (>20 clusters/day) | 100G | External log aggregation recommended |

## Emergency Cleanup

If disk is >95% full:

```bash
# 1. Immediate cleanup (fastest wins)
sudo journalctl --vacuum-size=50M          # ~800M freed
sudo rm -rf /opt/ocpctl/web.backup.*       # ~100M freed
npm cache clean --force                     # ~150M freed

# 2. Remove old releases (keep only last 2)
ls -1t /opt/ocpctl/releases/ | tail -n +3 | xargs -I {} sudo rm -rf /opt/ocpctl/releases/{}

# 3. If still critical, remove unused installers
sudo rm /usr/local/bin/openshift-install-4.18  # ~655M
sudo rm /usr/local/bin/openshift-install-4.19  # ~570M

# 4. Check disk usage
df -h /
```

## Troubleshooting

### Cleanup Script Fails

```bash
# Check script permissions
ls -l /opt/ocpctl/scripts/cleanup-disk-space.sh
sudo chmod +x /opt/ocpctl/scripts/cleanup-disk-space.sh

# Run with verbose output
sudo bash -x /opt/ocpctl/scripts/cleanup-disk-space.sh
```

### Cron Job Not Running

```bash
# Check cron service
sudo systemctl status crond

# Check cron logs
sudo grep cleanup /var/log/cron

# Check cleanup script logs
sudo tail -100 /var/log/ocpctl-cleanup.log
```

### Disk Still Full After Cleanup

```bash
# Find what's using space
sudo du -ah / 2>/dev/null | sort -h | tail -50

# Check for large files
sudo find / -type f -size +100M 2>/dev/null -exec ls -lh {} \;

# Check deleted but still-open files (not freed until restart)
sudo lsof | grep deleted
```

## See Also

- [AWS Quickstart](../deployment/AWS_QUICKSTART.md) - Initial disk sizing recommendations
- [Deployment Guide](../deployment/DEPLOYMENT_WEB.md) - Release management
- [Operations Guide](RESOURCE_TAGGING_OPERATIONS.md) - Other operational procedures
