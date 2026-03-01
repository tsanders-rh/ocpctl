# Brix Headless Box Setup Guide

This guide explains how to set up ocpctl on a headless Brix box running Fedora for continuous testing and development.

## Overview

Your Brix box will run:
- **ocpctl API server** (port 8080)
- **ocpctl worker service** (processes cluster jobs)
- **ocpctl janitor service** (manages TTLs and cleanup)
- **ocpctl web frontend** (port 3000)
- **nginx reverse proxy** (port 80)
- **PostgreSQL database**

The Brix box runs the management services and `openshift-install` binary, which creates actual OpenShift clusters in AWS (not on the Brix itself).

## Prerequisites

- **Fedora-based Brix box** with network connectivity
- **SSH access** to the Brix
- **8GB RAM minimum** (16GB recommended)
- **4 CPU cores minimum**
- **100GB storage minimum**
- **Root/sudo access**

## Quick Setup (Automated)

### Option 1: Run from GitHub (Recommended)

```bash
# SSH into your Brix
ssh your-brix-ip

# Run the setup script directly from GitHub
sudo bash <(curl -fsSL https://raw.githubusercontent.com/tsanders-rh/ocpctl/main/scripts/setup-fedora-brix.sh)
```

### Option 2: Clone and Run Locally

```bash
# SSH into your Brix
ssh your-brix-ip

# Clone the repository (using SSH)
git clone git@github.com:tsanders-rh/ocpctl.git
cd ocpctl

# Make script executable
chmod +x scripts/setup-fedora-brix.sh

# Run setup
sudo ./scripts/setup-fedora-brix.sh
```

**Note**: If you don't have SSH keys set up with GitHub:

```bash
# Generate SSH key
ssh-keygen -t ed25519 -C "your-email@example.com"

# Display public key
cat ~/.ssh/id_ed25519.pub

# Add this key to GitHub at: https://github.com/settings/keys
# Then clone with SSH as shown above
```

## What the Script Does

The automated script performs the following:

1. **System Prerequisites**
   - Updates Fedora packages
   - Installs build tools (git, gcc, make)
   - Installs utilities (vim, htop, tmux)

2. **Runtime Installation**
   - Installs Go 1.21
   - Installs Node.js 20
   - Installs PostgreSQL 14

3. **Database Setup**
   - Creates `ocpctl` database
   - Creates `ocpctl` database user with secure password
   - Runs all migrations
   - Seeds admin user

4. **Application Setup**
   - Creates `ocpctl` system user
   - Clones repository to `/opt/ocpctl`
   - Creates environment files with generated secrets
   - Builds backend binaries
   - Builds frontend production bundle

5. **Service Configuration**
   - Creates systemd services for all components
   - Configures nginx reverse proxy
   - Sets up firewall rules
   - Starts all services

6. **Security**
   - Generates unique JWT secret
   - Generates secure database password
   - Configures systemd security hardening
   - Sets proper file permissions

## Post-Installation

### Access the Web UI

After installation, access the web UI from your laptop:

```bash
# Get the Brix IP address
ssh your-brix-ip hostname -I

# Open in browser:
http://<brix-ip>
```

**Default login:**
- Email: `admin@example.com`
- Password: `changeme`

**⚠️ Change the password immediately after first login!**

### Verify Services

```bash
# Check all service status
systemctl status 'ocpctl-*'

# Individual service status
systemctl status ocpctl-api
systemctl status ocpctl-worker
systemctl status ocpctl-janitor
systemctl status ocpctl-web
systemctl status nginx
```

### View Logs

```bash
# API logs
journalctl -u ocpctl-api -f

# Worker logs (cluster provisioning)
journalctl -u ocpctl-worker -f

# Janitor logs (TTL management)
journalctl -u ocpctl-janitor -f

# Web frontend logs
journalctl -u ocpctl-web -f

# Nginx logs
journalctl -u nginx -f

# All ocpctl logs combined
journalctl -u 'ocpctl-*' -f
```

### Database Access

```bash
# The script creates a database user and password
# Find credentials in setup output or:
cat /opt/ocpctl/.env | grep DATABASE_URL

# Connect to database
sudo -u ocpctl psql -d ocpctl

# Useful queries:
SELECT * FROM users;
SELECT id, name, status, platform FROM clusters;
SELECT id, job_type, status, cluster_id FROM jobs ORDER BY created_at DESC;
```

## Testing Without AWS

You can test 95% of ocpctl functionality without AWS credentials:

### What Works:
- ✅ Full web UI (login, forms, navigation)
- ✅ Complete API (all endpoints)
- ✅ Database operations
- ✅ Profile browsing
- ✅ Cluster creation (creates DB record and job)
- ✅ User management

### What Requires AWS:
- ❌ Actual cluster provisioning
- ❌ Cluster outputs (kubeconfig)

See [TESTING_WITHOUT_OPENSHIFT.md](TESTING_WITHOUT_OPENSHIFT.md) for complete testing guide.

## Adding AWS Support for Cluster Provisioning

To actually provision OpenShift clusters, you need:

1. **Install openshift-install binary**
2. **Configure Red Hat pull secret**
3. **Configure AWS credentials**

See [OPENSHIFT_INSTALL_SETUP.md](OPENSHIFT_INSTALL_SETUP.md) for complete setup.

### Quick AWS Setup

```bash
# SSH to Brix
ssh your-brix-ip

# Install AWS CLI
sudo dnf install -y awscli

# Configure AWS credentials
aws configure
# Enter your AWS Access Key ID
# Enter your AWS Secret Access Key
# Default region: us-east-1
# Default output: json

# Install openshift-install
VERSION=stable-4.16
wget https://mirror.openshift.com/pub/openshift-v4/clients/ocp/${VERSION}/openshift-install-linux.tar.gz
tar xvf openshift-install-linux.tar.gz
sudo mv openshift-install /usr/local/bin/
sudo chmod +x /usr/local/bin/openshift-install

# Get pull secret from: https://console.redhat.com/openshift/install/pull-secret
# Add to environment:
sudo -u ocpctl nano /opt/ocpctl/.env
# Add line:
# OPENSHIFT_PULL_SECRET='<paste-your-pull-secret-json>'

# Restart worker to pick up new credentials
sudo systemctl restart ocpctl-worker

# Verify
journalctl -u ocpctl-worker -f
```

## Accessing from Your Laptop

### Option 1: Direct Access (Simplest)

If Brix is on your local network:

```bash
# Find Brix IP
ssh your-brix-ip hostname -I

# Open in browser:
http://<brix-ip>

# Or make API calls:
curl http://<brix-ip>/api/v1/clusters
```

### Option 2: SSH Tunnel (Most Secure)

```bash
# Create SSH tunnel from laptop
ssh -L 8080:localhost:8080 -L 3000:localhost:3000 your-brix-ip

# Access in browser:
http://localhost:3000
```

### Option 3: Add DNS Entry

Add to your laptop's `/etc/hosts`:

```
<brix-ip>  ocpctl.local
```

Then access: `http://ocpctl.local`

## Common Tasks

### Restart All Services

```bash
sudo systemctl restart ocpctl-api ocpctl-worker ocpctl-janitor ocpctl-web
```

### Update ocpctl

```bash
# Pull latest code
cd /opt/ocpctl
sudo -u ocpctl git pull

# Rebuild backend
sudo -u ocpctl make build

# Rebuild frontend
cd web
sudo -u ocpctl npm install
sudo -u ocpctl npm run build

# Run migrations (if any)
sudo -u ocpctl /home/ocpctl/go/bin/goose -dir internal/store/migrations postgres "$(grep DATABASE_URL /opt/ocpctl/.env | cut -d= -f2)" up

# Restart services
cd /opt/ocpctl
sudo systemctl restart ocpctl-api ocpctl-worker ocpctl-janitor ocpctl-web
```

### Reset Database

```bash
# Backup first!
sudo -u postgres pg_dump ocpctl > /tmp/ocpctl-backup.sql

# Drop and recreate
sudo -u postgres psql -c "DROP DATABASE ocpctl;"
sudo -u postgres psql -c "CREATE DATABASE ocpctl OWNER ocpctl;"

# Run migrations
cd /opt/ocpctl
sudo -u ocpctl /home/ocpctl/go/bin/goose -dir internal/store/migrations postgres "$(grep DATABASE_URL .env | cut -d= -f2)" up
```

### Monitor Resources

```bash
# System resources
htop

# Disk usage
df -h

# Service resource usage
systemctl status ocpctl-api
systemctl status ocpctl-worker

# Database size
sudo -u postgres psql -c "SELECT pg_size_pretty(pg_database_size('ocpctl'));"
```

## Troubleshooting

### Services Won't Start

```bash
# Check service status
systemctl status ocpctl-api -l

# Check logs for errors
journalctl -u ocpctl-api --since "5 minutes ago"

# Check if ports are in use
ss -tulpn | grep -E ':(8080|3000|5432)'

# Verify environment file
cat /opt/ocpctl/.env

# Test database connection
sudo -u ocpctl psql -d ocpctl -c "SELECT 1;"
```

### Can't Access from Laptop

```bash
# Check firewall on Brix
sudo firewall-cmd --list-all

# Allow HTTP if needed
sudo firewall-cmd --permanent --add-service=http
sudo firewall-cmd --reload

# Check nginx status
systemctl status nginx

# Test nginx config
sudo nginx -t

# Check if services are listening
ss -tulpn | grep -E ':(80|8080|3000)'
```

### Database Issues

```bash
# Check PostgreSQL is running
systemctl status postgresql

# Check database exists
sudo -u postgres psql -l | grep ocpctl

# Check connections
sudo -u postgres psql -c "SELECT * FROM pg_stat_activity WHERE datname = 'ocpctl';"

# Restart PostgreSQL
sudo systemctl restart postgresql
```

### Worker Not Processing Jobs

```bash
# Check worker logs
journalctl -u ocpctl-worker -f

# Check if openshift-install is installed
which openshift-install

# Check AWS credentials
aws sts get-caller-identity

# Check pull secret
grep OPENSHIFT_PULL_SECRET /opt/ocpctl/.env

# Verify job exists
sudo -u ocpctl psql -d ocpctl -c "SELECT * FROM jobs WHERE status = 'PENDING';"
```

## Performance Tuning

### PostgreSQL

```bash
# Edit PostgreSQL config for better performance
sudo vi /var/lib/pgsql/data/postgresql.conf

# Recommended settings for 8GB RAM Brix:
# shared_buffers = 2GB
# effective_cache_size = 6GB
# maintenance_work_mem = 512MB
# checkpoint_completion_target = 0.9
# wal_buffers = 16MB
# default_statistics_target = 100
# random_page_cost = 1.1
# effective_io_concurrency = 200
# work_mem = 10MB
# min_wal_size = 1GB
# max_wal_size = 4GB

# Restart PostgreSQL
sudo systemctl restart postgresql
```

### Worker Concurrency

```bash
# Edit worker concurrency
sudo vi /opt/ocpctl/.env

# Change WORKER_CONCURRENCY (default: 3)
# For 8GB RAM: WORKER_CONCURRENCY=2
# For 16GB RAM: WORKER_CONCURRENCY=3-4

# Restart worker
sudo systemctl restart ocpctl-worker
```

## Security Hardening

### Change Default Passwords

```bash
# 1. Change admin@localhost password via web UI

# 2. Change database password
NEW_DB_PASSWORD=$(openssl rand -base64 32 | tr -d "=+/" | cut -c1-25)
sudo -u postgres psql -c "ALTER USER ocpctl WITH PASSWORD '${NEW_DB_PASSWORD}';"

# Update .env
sudo sed -i "s|postgresql://ocpctl:[^@]*@|postgresql://ocpctl:${NEW_DB_PASSWORD}@|" /opt/ocpctl/.env

# Restart services
sudo systemctl restart ocpctl-api ocpctl-worker ocpctl-janitor
```

### Enable HTTPS (Production)

```bash
# Install certbot
sudo dnf install -y certbot python3-certbot-nginx

# Get certificate (requires public domain)
sudo certbot --nginx -d your-domain.com

# Auto-renewal is configured automatically
```

### Limit Network Access

```bash
# Only allow access from specific IPs
sudo firewall-cmd --permanent --zone=public --add-rich-rule='rule family="ipv4" source address="192.168.1.100" accept'
sudo firewall-cmd --reload
```

## Backup Strategy

### Automated Backup Script

Create `/opt/ocpctl/scripts/backup.sh`:

```bash
#!/bin/bash
BACKUP_DIR="/var/backups/ocpctl"
DATE=$(date +%Y%m%d_%H%M%S)

mkdir -p ${BACKUP_DIR}

# Backup database
sudo -u postgres pg_dump ocpctl | gzip > ${BACKUP_DIR}/db_${DATE}.sql.gz

# Backup environment files
cp /opt/ocpctl/.env ${BACKUP_DIR}/env_${DATE}

# Keep only last 7 days
find ${BACKUP_DIR} -name "*.sql.gz" -mtime +7 -delete
find ${BACKUP_DIR} -name "env_*" -mtime +7 -delete
```

Add to cron:

```bash
sudo crontab -e
# Add:
0 2 * * * /opt/ocpctl/scripts/backup.sh
```

## Next Steps

1. **Access web UI** - Login and verify everything works
2. **Change admin password** - Security first!
3. **Test without AWS** - Create mock clusters to test UI/API
4. **Add AWS credentials** - When ready for real provisioning
5. **Monitor logs** - Watch for any errors
6. **Set up backups** - Automate database backups

## Getting Help

- **Documentation**: `/opt/ocpctl/docs/`
- **Logs**: `journalctl -u ocpctl-api -f`
- **Database**: `sudo -u ocpctl psql -d ocpctl`
- **GitHub Issues**: https://github.com/tsanders-rh/ocpctl/issues

## Uninstall

```bash
# Stop and disable services
sudo systemctl stop ocpctl-api ocpctl-worker ocpctl-janitor ocpctl-web
sudo systemctl disable ocpctl-api ocpctl-worker ocpctl-janitor ocpctl-web

# Remove systemd units
sudo rm /etc/systemd/system/ocpctl-*.service
sudo systemctl daemon-reload

# Remove nginx config
sudo rm /etc/nginx/conf.d/ocpctl.conf
sudo systemctl reload nginx

# Drop database
sudo -u postgres psql -c "DROP DATABASE ocpctl;"
sudo -u postgres psql -c "DROP USER ocpctl;"

# Remove installation
sudo rm -rf /opt/ocpctl
sudo rm -rf /var/lib/ocpctl

# Remove user
sudo userdel -r ocpctl
```
