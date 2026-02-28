# Web Frontend Deployment Guide

This guide covers deploying the ocpctl Next.js web frontend on the same EC2 instance as the API server.

## Architecture

```
                   ┌─────────────┐
                   │   Nginx     │
                   │   :80/:443  │
                   └──────┬──────┘
                          │
            ┌─────────────┴─────────────┐
            │                           │
     /api/* requests             / requests
            │                           │
    ┌───────▼────────┐         ┌────────▼────────┐
    │  ocpctl-api    │         │  ocpctl-web     │
    │  Go Server     │         │  Next.js        │
    │  :8080         │         │  :3000          │
    └────────────────┘         └─────────────────┘
```

## Prerequisites

- EC2 instance with ocpctl API already deployed
- Node.js 18+ installed
- Nginx installed
- systemd

## Installation Steps

### 1. Install Node.js (if not already installed)

```bash
# Install Node.js 18.x
curl -fsSL https://deb.nodesource.com/setup_18.x | sudo -E bash -
sudo apt-get install -y nodejs

# Verify installation
node --version  # Should be v18.x or higher
npm --version
```

### 2. Create Application User (if not exists)

```bash
# If not already created during API deployment
sudo useradd -r -s /bin/bash -d /opt/ocpctl ocpctl
```

### 3. Deploy Web Application

```bash
# Create web directory
sudo mkdir -p /opt/ocpctl/web
sudo chown -R ocpctl:ocpctl /opt/ocpctl

# Switch to ocpctl user
sudo su - ocpctl

# Clone or copy the web directory
cd /opt/ocpctl
# Copy your built web application here
# You can use rsync, git, or scp

# Install dependencies
cd web
npm install --production

# Build the application
npm run build
```

### 4. Configure Environment

```bash
# Create environment file
sudo mkdir -p /etc/ocpctl
sudo cp /opt/ocpctl/deploy/config/web.env.template /etc/ocpctl/web.env

# Edit environment file
sudo nano /etc/ocpctl/web.env
```

**Production Environment Configuration** (`/etc/ocpctl/web.env`):

```bash
# API endpoint (internal communication)
NEXT_PUBLIC_API_URL=http://localhost:8080/api/v1

# Authentication mode
NEXT_PUBLIC_AUTH_MODE=iam  # or jwt for password auth

# AWS region
NEXT_PUBLIC_AWS_REGION=us-east-1

# Node environment
NODE_ENV=production
```

**Important Notes**:
- Use `NEXT_PUBLIC_AUTH_MODE=iam` for AWS deployments with IAM authentication
- Use `NEXT_PUBLIC_AUTH_MODE=jwt` for email/password authentication
- API URL should use localhost since nginx proxies both services

### 5. Install systemd Service

```bash
# Copy service file
sudo cp /opt/ocpctl/deploy/systemd/ocpctl-web.service /etc/systemd/system/

# Reload systemd
sudo systemctl daemon-reload

# Enable service to start on boot
sudo systemctl enable ocpctl-web

# Start the service
sudo systemctl start ocpctl-web

# Check status
sudo systemctl status ocpctl-web
```

### 6. Configure Nginx

```bash
# Backup existing nginx config
sudo cp /etc/nginx/sites-available/default /etc/nginx/sites-available/default.bak

# Copy ocpctl nginx config
sudo cp /opt/ocpctl/deploy/nginx/ocpctl.conf /etc/nginx/sites-available/ocpctl

# Create symlink
sudo ln -sf /etc/nginx/sites-available/ocpctl /etc/nginx/sites-enabled/ocpctl

# Remove default config (optional)
sudo rm /etc/nginx/sites-enabled/default

# Test nginx configuration
sudo nginx -t

# Reload nginx
sudo systemctl reload nginx
```

### 7. Configure SSL with Let's Encrypt (Production)

```bash
# Install certbot
sudo apt-get install -y certbot python3-certbot-nginx

# Obtain SSL certificate
sudo certbot --nginx -d ocpctl.example.com

# Certbot will automatically update nginx config for HTTPS
# Certificates auto-renew via systemd timer
```

### 8. Configure Firewall

```bash
# Allow HTTP and HTTPS
sudo ufw allow 80/tcp
sudo ufw allow 443/tcp

# Reload firewall
sudo ufw reload
```

## Verification

### Check Services

```bash
# Check API server
sudo systemctl status ocpctl-api
curl http://localhost:8080/health

# Check web server
sudo systemctl status ocpctl-web
curl http://localhost:3000

# Check nginx
sudo systemctl status nginx
curl http://localhost
```

### Test Application

1. **Open browser**: Navigate to your domain (e.g., https://ocpctl.example.com)
2. **Login**:
   - JWT mode: Use admin@localhost / changeme
   - IAM mode: Ensure AWS credentials configured
3. **Test features**:
   - View cluster list
   - Create new cluster
   - View cluster details
   - Browse profiles

### Check Logs

```bash
# Web application logs
sudo journalctl -u ocpctl-web -f

# API logs
sudo journalctl -u ocpctl-api -f

# Nginx access logs
sudo tail -f /var/log/nginx/access.log

# Nginx error logs
sudo tail -f /var/log/nginx/error.log
```

## Updating the Application

```bash
# Stop the service
sudo systemctl stop ocpctl-web

# Switch to ocpctl user
sudo su - ocpctl
cd /opt/ocpctl/web

# Pull latest changes or copy new build
# git pull  # if using git
# or use rsync/scp

# Install dependencies
npm install --production

# Build
npm run build

# Exit ocpctl user
exit

# Start service
sudo systemctl start ocpctl-web

# Verify
sudo systemctl status ocpctl-web
```

## Automated Deployment with Makefile

Add to your repository's Makefile:

```makefile
.PHONY: deploy-web

DEPLOY_HOST := ec2-user@ocpctl.example.com
DEPLOY_PATH := /opt/ocpctl/web

deploy-web:
	# Build locally
	cd web && npm run build

	# Deploy to server
	rsync -avz --delete \
		--exclude node_modules \
		--exclude .next/cache \
		web/ $(DEPLOY_HOST):$(DEPLOY_PATH)/

	# Install dependencies and restart
	ssh $(DEPLOY_HOST) '\
		cd $(DEPLOY_PATH) && \
		npm install --production && \
		sudo systemctl restart ocpctl-web'

	@echo "Deployment complete!"
```

Usage:
```bash
make deploy-web
```

## Troubleshooting

### Service Won't Start

```bash
# Check service logs
sudo journalctl -u ocpctl-web -n 50 --no-pager

# Check if port 3000 is already in use
sudo lsof -i :3000

# Verify environment file
sudo cat /etc/ocpctl/web.env

# Verify permissions
ls -la /opt/ocpctl/web
sudo systemctl status ocpctl-web
```

### Build Fails

```bash
# Check Node.js version
node --version  # Must be 18+

# Clean build
cd /opt/ocpctl/web
rm -rf .next node_modules
npm install
npm run build
```

### API Connection Issues

```bash
# Verify API is running
curl http://localhost:8080/health

# Check NEXT_PUBLIC_API_URL in environment
grep NEXT_PUBLIC_API_URL /etc/ocpctl/web.env

# Check nginx proxy configuration
sudo nginx -t
sudo cat /etc/nginx/sites-enabled/ocpctl
```

### Authentication Issues (JWT mode)

```bash
# Verify API is issuing tokens
curl -X POST http://localhost:8080/api/v1/auth/login \
  -H "Content-Type: application/json" \
  -d '{"email":"admin@localhost","password":"changeme"}'

# Check browser console for errors
# Check that cookies are being set (refresh_token)
```

### Authentication Issues (IAM mode)

```bash
# Verify IAM authentication is enabled in API
grep ENABLE_IAM_AUTH /etc/ocpctl/api.env

# Test IAM auth from server
aws sts get-caller-identity

# Verify EC2 instance role has permissions
# Check API logs for IAM auth errors
sudo journalctl -u ocpctl-api -f | grep -i iam
```

## Performance Tuning

### Production Optimizations

**PM2 for Process Management** (alternative to systemd):

```bash
# Install PM2
sudo npm install -g pm2

# Start with PM2
cd /opt/ocpctl/web
pm2 start npm --name "ocpctl-web" -- start

# Save PM2 config
pm2 save

# Setup PM2 to start on boot
pm2 startup systemd
```

**Nginx Caching**:

Add to nginx config:
```nginx
# Cache Next.js static assets
location /_next/static {
    proxy_pass http://localhost:3000;
    proxy_cache_valid 200 60m;
    add_header Cache-Control "public, immutable";
}
```

## Security Checklist

- [ ] HTTPS enabled with valid certificate
- [ ] Firewall configured (only 80, 443 exposed)
- [ ] Security headers configured in nginx (HSTS, X-Frame-Options, etc.)
- [ ] Environment variables stored securely (not in source code)
- [ ] IAM mode enabled for production (no password exposure)
- [ ] Services running as non-root user (ocpctl)
- [ ] Nginx configured to hide server version
- [ ] Regular security updates enabled

## Monitoring

### Health Checks

Add to monitoring system:

```bash
# Web frontend health
curl -f http://localhost:3000 || alert

# API health
curl -f http://localhost:8080/health || alert

# Nginx health
systemctl is-active nginx || alert
```

### Metrics to Monitor

- Response times (Nginx access logs)
- Error rates (Nginx error logs, app logs)
- Memory usage (`systemctl status ocpctl-web`)
- Disk usage (Next.js cache can grow)
- SSL certificate expiration

## Backup

```bash
# Backup configuration
sudo cp /etc/ocpctl/web.env /backup/web.env.$(date +%Y%m%d)
sudo cp /etc/nginx/sites-available/ocpctl /backup/nginx-ocpctl.$(date +%Y%m%d)

# Backup is not needed for application code (use git/rsync from source)
```

## Rollback

```bash
# Stop service
sudo systemctl stop ocpctl-web

# Restore previous version
# (copy previous build or git checkout)
cd /opt/ocpctl/web
git checkout <previous-commit>
npm install --production
npm run build

# Restart service
sudo systemctl start ocpctl-web
```

## Support

For issues or questions:
- Check logs: `sudo journalctl -u ocpctl-web -f`
- Review nginx logs: `sudo tail -f /var/log/nginx/error.log`
- Verify environment: `sudo cat /etc/ocpctl/web.env`
