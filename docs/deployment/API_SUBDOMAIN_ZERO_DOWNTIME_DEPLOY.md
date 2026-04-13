# Zero-Downtime API Subdomain Deployment

This guide walks through deploying the API subdomain (`api.ocpctl.mg.dog8code.com`) with **zero impact** on running workers and minimal risk to existing services.

## Pre-Deployment Checklist

- [ ] Verify current services are healthy
- [ ] Backup current nginx configuration
- [ ] Have rollback plan ready
- [ ] Schedule deployment during low-traffic period (optional but recommended)

## Impact Analysis

### ✅ Zero Impact
- **Workers**: Communicate directly with DB/S3, not through nginx
- **Active cluster operations**: Continue uninterrupted
- **Database connections**: Unaffected
- **S3 artifact storage**: Unaffected

### ⚠️ Minimal Impact (Graceful)
- **API requests**: nginx graceful reload keeps active connections alive
- **Web UI**: nginx graceful reload, users may see <1s delay
- **New requests**: Served immediately by new nginx workers

### 📊 Expected Downtime
**0 seconds** - nginx reload is graceful and non-disruptive

## Step-by-Step Deployment

### Step 1: Verify Current System Health

**Impact**: Read-only, zero risk

```bash
# SSH to production
ssh -i ~/.ssh/ocpctl-production-key ubuntu@44.201.165.78

# Check all services are healthy
sudo systemctl status nginx
sudo systemctl status ocpctl-api
sudo systemctl status ocpctl-worker

# Check worker jobs (should show normal processing)
curl http://localhost:8081/health

# Check API health
curl http://localhost:8080/health

# Check nginx is serving requests
curl -I http://localhost:80
```

**Expected**: All services show `active (running)` status

### Step 2: Backup Current Configuration

**Impact**: Read-only, zero risk

```bash
# Create backup directory
sudo mkdir -p /opt/ocpctl/backups/nginx

# Backup all nginx configs with timestamp
TIMESTAMP=$(date +%Y%m%d-%H%M%S)
sudo tar -czf /opt/ocpctl/backups/nginx/nginx-config-${TIMESTAMP}.tar.gz \
  /etc/nginx/sites-available/ \
  /etc/nginx/sites-enabled/ \
  /etc/nginx/nginx.conf

# Verify backup
ls -lh /opt/ocpctl/backups/nginx/

# Backup current SSL certs (if using custom certs)
sudo cp -r /etc/ssl/certs/ocpctl.crt /opt/ocpctl/backups/nginx/
sudo cp -r /etc/ssl/private/ocpctl.key /opt/ocpctl/backups/nginx/
```

**Expected**: Backup tarball created with timestamp

### Step 3: Create DNS Record

**Impact**: Zero impact until nginx config deployed

This step is safe because:
- DNS change won't affect anything until nginx is configured to handle it
- No existing traffic uses this domain yet
- Can be done ahead of time

#### Option A: Route 53 (AWS Console)

1. Go to Route 53 → Hosted Zones
2. Select `mg.dog8code.com`
3. Click "Create Record"
4. Configure:
   - Record name: `api.ocpctl`
   - Record type: `A` (or `CNAME`)
   - Value: `44.201.165.78` (or `ocpctl.mg.dog8code.com` for CNAME)
   - TTL: `300` (5 minutes)
5. Click "Create records"

#### Option B: Route 53 (AWS CLI)

```bash
# Get hosted zone ID
ZONE_ID=$(aws route53 list-hosted-zones \
  --query "HostedZones[?Name=='mg.dog8code.com.'].Id" \
  --output text | cut -d'/' -f3)

# Create A record pointing to EC2 IP
aws route53 change-resource-record-sets \
  --hosted-zone-id $ZONE_ID \
  --change-batch '{
    "Changes": [{
      "Action": "CREATE",
      "ResourceRecordSet": {
        "Name": "api.ocpctl.mg.dog8code.com",
        "Type": "A",
        "TTL": 300,
        "ResourceRecords": [{"Value": "44.201.165.78"}]
      }
    }]
  }'
```

#### Verify DNS (wait 2-5 minutes for propagation)

```bash
# From your local machine
dig api.ocpctl.mg.dog8code.com +short

# Should return: 44.201.165.78
```

**Expected**: DNS resolves to correct IP after 2-5 minutes

### Step 4: Update SSL Certificate

**Impact**: Graceful reload, <1 second delay for new connections

This step uses Let's Encrypt to add the new subdomain to the existing certificate.

#### Check Current Certificate

```bash
# See what domains are in current cert
sudo openssl x509 -in /etc/letsencrypt/live/ocpctl.mg.dog8code.com/fullchain.pem \
  -text -noout | grep DNS
```

#### Add api.ocpctl.mg.dog8code.com to Certificate

```bash
# Stop nginx briefly for certificate renewal (standalone mode)
sudo systemctl stop nginx

# Request new certificate with both domains
# Let's Encrypt will issue a single cert with both domains
sudo certbot certonly --standalone \
  -d ocpctl.mg.dog8code.com \
  -d api.ocpctl.mg.dog8code.com \
  --email your-email@example.com \
  --agree-tos \
  --non-interactive \
  --force-renewal

# Start nginx again
sudo systemctl start nginx

# Verify nginx is running
sudo systemctl status nginx
```

**Downtime**: ~30-60 seconds while nginx is stopped for cert renewal

**Alternative (zero downtime)**: If you have nginx running with webroot plugin:
```bash
# Use webroot plugin (no nginx restart needed)
sudo certbot certonly --webroot \
  -w /var/www/html \
  -d ocpctl.mg.dog8code.com \
  -d api.ocpctl.mg.dog8code.com \
  --email your-email@example.com \
  --agree-tos \
  --non-interactive
```

#### Verify New Certificate

```bash
# Check cert includes both domains
sudo openssl x509 -in /etc/letsencrypt/live/ocpctl.mg.dog8code.com/fullchain.pem \
  -text -noout | grep DNS

# Should show:
# DNS:ocpctl.mg.dog8code.com, DNS:api.ocpctl.mg.dog8code.com
```

**Expected**: Certificate includes both domains

### Step 5: Deploy nginx Configuration

**Impact**: Graceful reload, zero downtime

#### Upload Configuration

```bash
# From your local machine
scp -i ~/.ssh/ocpctl-production-key \
  deploy/nginx/api.ocpctl.conf \
  ubuntu@44.201.165.78:/tmp/
```

#### Install and Test Configuration

```bash
# SSH to production
ssh -i ~/.ssh/ocpctl-production-key ubuntu@44.201.165.78

# Move config to sites-available
sudo mv /tmp/api.ocpctl.conf /etc/nginx/sites-available/

# Create symlink to sites-enabled
sudo ln -sf /etc/nginx/sites-available/api.ocpctl.conf \
  /etc/nginx/sites-enabled/

# TEST configuration WITHOUT reloading
sudo nginx -t
```

**Expected output**:
```
nginx: the configuration file /etc/nginx/nginx.conf syntax is ok
nginx: configuration file /etc/nginx/nginx.conf test is successful
```

**If test fails**:
```bash
# Remove the symlink
sudo rm /etc/nginx/sites-enabled/api.ocpctl.conf

# Review errors
sudo nginx -t

# Fix config and try again
```

#### Apply Configuration (Graceful Reload)

**Only proceed if `nginx -t` succeeds**

```bash
# Graceful reload - keeps active connections alive
sudo systemctl reload nginx

# Alternative: nginx -s reload
# sudo nginx -s reload
```

**What happens during reload**:
1. nginx validates new config
2. Starts new worker processes with new config
3. Stops accepting new connections on old workers
4. Waits for old workers to finish current requests
5. Terminates old workers when idle
6. **Total disruption**: 0 seconds for existing connections

**Verify reload succeeded**:
```bash
# Check nginx status
sudo systemctl status nginx

# Check nginx error logs for any issues
sudo tail -20 /var/log/nginx/error.log

# Verify both configs are loaded
ls -l /etc/nginx/sites-enabled/
```

**Expected**: nginx shows `active (running)`, no errors in logs

### Step 6: Verify API Access

**Impact**: Read-only, zero risk

#### Test New Subdomain

```bash
# Health check
curl -I https://api.ocpctl.mg.dog8code.com/health

# Should return: 200 OK

# Test API version endpoint
curl https://api.ocpctl.mg.dog8code.com/v1/system/version

# Test Swagger docs
curl -I https://api.ocpctl.mg.dog8code.com/swagger/index.html
```

#### Verify Existing Access Still Works

```bash
# Old path-based access should still work
curl -I https://ocpctl.mg.dog8code.com/api/health
curl https://ocpctl.mg.dog8code.com/api/v1/system/version

# Web UI should still work
curl -I https://ocpctl.mg.dog8code.com
```

#### Test Full API Flow

```bash
# Login via new subdomain
TOKEN=$(curl -X POST https://api.ocpctl.mg.dog8code.com/v1/auth/login \
  -H "Content-Type: application/json" \
  -d '{"email":"your-email@example.com","password":"your-password"}' \
  | jq -r '.access_token')

# List clusters
curl https://api.ocpctl.mg.dog8code.com/v1/clusters \
  -H "Authorization: Bearer $TOKEN" | jq

# Get user profile
curl https://api.ocpctl.mg.dog8code.com/v1/users/me \
  -H "Authorization: Bearer $TOKEN" | jq
```

**Expected**: All endpoints return valid responses

### Step 7: Monitor for Issues

**Impact**: Read-only, zero risk

```bash
# Monitor nginx access logs (in real-time)
sudo tail -f /var/log/nginx/access.log

# Monitor nginx error logs
sudo tail -f /var/log/nginx/error.log

# Monitor API logs
sudo journalctl -u ocpctl-api -f

# Check worker is still processing jobs
curl http://localhost:8081/health | jq
```

**Expected**: Normal traffic patterns, no errors

### Step 8: Update Monitoring (Optional)

Add health checks for the new subdomain to your monitoring system.

## Rollback Plan

If anything goes wrong, rollback is fast and safe:

### Quick Rollback (Remove API Subdomain)

```bash
# SSH to production
ssh -i ~/.ssh/ocpctl-production-key ubuntu@44.201.165.78

# Remove the new config symlink
sudo rm /etc/nginx/sites-enabled/api.ocpctl.conf

# Test config
sudo nginx -t

# Graceful reload
sudo systemctl reload nginx

# Verify main site still works
curl -I https://ocpctl.mg.dog8code.com
```

**Downtime**: 0 seconds (graceful reload)

### Full Rollback (Restore from Backup)

```bash
# Find backup
ls -lh /opt/ocpctl/backups/nginx/

# Extract backup
TIMESTAMP=20260413-194500  # Use your actual timestamp
cd /
sudo tar -xzf /opt/ocpctl/backups/nginx/nginx-config-${TIMESTAMP}.tar.gz

# Test config
sudo nginx -t

# Reload
sudo systemctl reload nginx
```

## Post-Deployment Checklist

- [ ] DNS resolves to correct IP
- [ ] SSL certificate includes both domains
- [ ] `https://api.ocpctl.mg.dog8code.com/health` returns 200
- [ ] Swagger UI accessible at new subdomain
- [ ] Old path-based API access still works
- [ ] Web UI still accessible
- [ ] Workers still processing jobs (check `/health` and job logs)
- [ ] No errors in nginx/API logs
- [ ] Update documentation with new API URL

## Verification Commands Summary

```bash
# Quick health check script
cat > /tmp/verify-api-subdomain.sh << 'EOF'
#!/bin/bash
echo "=== DNS Check ==="
dig api.ocpctl.mg.dog8code.com +short

echo -e "\n=== SSL Certificate Check ==="
openssl s_client -connect api.ocpctl.mg.dog8code.com:443 -servername api.ocpctl.mg.dog8code.com < /dev/null 2>&1 | grep -A 2 "subject="

echo -e "\n=== API Health (New Subdomain) ==="
curl -s https://api.ocpctl.mg.dog8code.com/health | jq

echo -e "\n=== API Health (Old Path) ==="
curl -s https://ocpctl.mg.dog8code.com/api/health | jq

echo -e "\n=== Web UI Health ==="
curl -I https://ocpctl.mg.dog8code.com 2>&1 | head -1

echo -e "\n=== Worker Health ==="
curl -s http://localhost:8081/health | jq

echo -e "\n=== Service Status ==="
systemctl status nginx | grep Active
systemctl status ocpctl-api | grep Active
systemctl status ocpctl-worker | grep Active

echo -e "\n✅ All checks complete"
EOF

chmod +x /tmp/verify-api-subdomain.sh
/tmp/verify-api-subdomain.sh
```

## Timeline

Estimated deployment time: **15-20 minutes**

- DNS record creation: 2-5 minutes (for propagation)
- SSL certificate update: 2-3 minutes
- nginx config deployment: 2-3 minutes
- Verification: 5-10 minutes

**Active work time**: ~10 minutes
**Waiting time**: ~5-10 minutes (DNS propagation, verification)

## FAQ

**Q: Will workers stop processing jobs during deployment?**
A: No. Workers communicate directly with the database and S3, not through nginx.

**Q: Will API requests fail during nginx reload?**
A: No. nginx reload is graceful - it keeps existing connections alive while loading new config.

**Q: What if the SSL certificate renewal fails?**
A: The existing certificate continues to work for `ocpctl.mg.dog8code.com`. The new subdomain just won't work yet.

**Q: Can I deploy this during business hours?**
A: Yes. The only brief disruption is during SSL renewal if using standalone mode (~30-60 seconds). Use webroot mode for zero downtime.

**Q: What if I need to rollback?**
A: Simply remove the symlink and reload nginx. Takes ~30 seconds, zero downtime.

**Q: Will this affect active cluster create/destroy operations?**
A: No. These operations run in the worker service, which doesn't use nginx.

## Success Criteria

✅ Deployment is successful if:
1. New API subdomain responds with 200 OK
2. Old API path still works
3. Web UI still works
4. Workers show healthy status
5. No errors in service logs
6. SSL certificate valid for both domains

## Support

If you encounter issues:
1. Check nginx error logs: `sudo tail -50 /var/log/nginx/error.log`
2. Check API logs: `sudo journalctl -u ocpctl-api -n 50`
3. Verify DNS: `dig api.ocpctl.mg.dog8code.com`
4. Test SSL: `openssl s_client -connect api.ocpctl.mg.dog8code.com:443`
5. Rollback if needed (see Rollback Plan above)
