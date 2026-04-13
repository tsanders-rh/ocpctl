# API Subdomain Setup Guide

This guide explains how to set up a dedicated subdomain for direct API access at `api.ocpctl.mg.dog8code.com`.

## Overview

Instead of accessing the API through the path-based proxy:
```
https://ocpctl.mg.dog8code.com/api/v1/clusters
```

Users can access it through a dedicated subdomain:
```
https://api.ocpctl.mg.dog8code.com/v1/clusters
```

## Benefits

- **Cleaner URLs**: No `/api/` prefix needed
- **Separation of concerns**: API traffic separated from web traffic
- **Better for programmatic access**: Clearer endpoint for CLI tools and scripts
- **Independent scaling**: Can route API traffic to different backends in the future

## Prerequisites

- Access to DNS management for `mg.dog8code.com`
- SSH access to the production EC2 instance
- Ability to update SSL certificates

## Step 1: DNS Configuration

Create a CNAME record in your DNS provider (Route 53, Cloudflare, etc.):

```
Type: CNAME
Name: api.ocpctl.mg.dog8code.com
Value: ocpctl.mg.dog8code.com
TTL: 300 (5 minutes)
```

Or if using an A record:
```
Type: A
Name: api.ocpctl.mg.dog8code.com
Value: <EC2-PUBLIC-IP>  # Currently 44.201.165.78
TTL: 300
```

### Route 53 Example

```bash
# Get the hosted zone ID
ZONE_ID=$(aws route53 list-hosted-zones --query "HostedZones[?Name=='mg.dog8code.com.'].Id" --output text | cut -d'/' -f3)

# Create the CNAME record
aws route53 change-resource-record-sets --hosted-zone-id $ZONE_ID --change-batch '{
  "Changes": [{
    "Action": "CREATE",
    "ResourceRecordSet": {
      "Name": "api.ocpctl.mg.dog8code.com",
      "Type": "CNAME",
      "TTL": 300,
      "ResourceRecords": [{"Value": "ocpctl.mg.dog8code.com"}]
    }
  }]
}'
```

### Verify DNS

Wait a few minutes for propagation, then verify:

```bash
# Should return the EC2 IP
dig api.ocpctl.mg.dog8code.com

# Or
nslookup api.ocpctl.mg.dog8code.com
```

## Step 2: Update SSL Certificate

The SSL certificate must include `api.ocpctl.mg.dog8code.com` as a Subject Alternative Name (SAN).

### Option A: Let's Encrypt (Recommended)

If using Let's Encrypt, update the certificate to include both domains:

```bash
# SSH to production server
ssh -i ~/.ssh/ocpctl-production-key ubuntu@44.201.165.78

# Install certbot if not already installed
sudo apt-get update
sudo apt-get install -y certbot python3-certbot-nginx

# Stop nginx temporarily
sudo systemctl stop nginx

# Request certificate for both domains
sudo certbot certonly --standalone \
  -d ocpctl.mg.dog8code.com \
  -d api.ocpctl.mg.dog8code.com \
  --email your-email@example.com \
  --agree-tos \
  --non-interactive

# Update nginx to use Let's Encrypt certs
sudo sed -i 's|/etc/ssl/certs/ocpctl.crt|/etc/letsencrypt/live/ocpctl.mg.dog8code.com/fullchain.pem|g' /etc/nginx/sites-available/*.conf
sudo sed -i 's|/etc/ssl/private/ocpctl.key|/etc/letsencrypt/live/ocpctl.mg.dog8code.com/privkey.pem|g' /etc/nginx/sites-available/*.conf

# Set up auto-renewal
sudo certbot renew --dry-run
```

### Option B: Existing Certificate

If using an existing certificate, ensure it includes both:
- `ocpctl.mg.dog8code.com`
- `api.ocpctl.mg.dog8code.com`

Verify with:
```bash
openssl x509 -in /etc/ssl/certs/ocpctl.crt -text -noout | grep DNS
```

## Step 3: Deploy nginx Configuration

```bash
# From your local machine, copy the new config
scp -i ~/.ssh/ocpctl-production-key \
  deploy/nginx/api.ocpctl.conf \
  ubuntu@44.201.165.78:/tmp/

# SSH to production server
ssh -i ~/.ssh/ocpctl-production-key ubuntu@44.201.165.78

# Move config to nginx sites-available
sudo mv /tmp/api.ocpctl.conf /etc/nginx/sites-available/

# Create symlink to sites-enabled
sudo ln -sf /etc/nginx/sites-available/api.ocpctl.conf /etc/nginx/sites-enabled/

# Test nginx configuration
sudo nginx -t

# If test passes, reload nginx
sudo systemctl reload nginx
```

## Step 4: Verify API Access

Test the new subdomain:

```bash
# Health check
curl https://api.ocpctl.mg.dog8code.com/health

# API version endpoint
curl https://api.ocpctl.mg.dog8code.com/v1/system/version

# Swagger documentation
open https://api.ocpctl.mg.dog8code.com/swagger/index.html
```

## Step 5: Update Documentation

Update any documentation that references the API URL:

### Before
```
API Endpoint: https://ocpctl.mg.dog8code.com/api/v1
```

### After
```
API Endpoint: https://api.ocpctl.mg.dog8code.com/v1
Web UI: https://ocpctl.mg.dog8code.com
```

## API Usage Examples

### Using curl
```bash
# Get clusters
curl https://api.ocpctl.mg.dog8code.com/v1/clusters

# Create a cluster
curl -X POST https://api.ocpctl.mg.dog8code.com/v1/clusters \
  -H "Content-Type: application/json" \
  -d '{
    "name": "my-cluster",
    "profile_id": "aws-minimal"
  }'
```

### Using AWS CLI with IAM Auth
```bash
# Set API base URL
export OCPCTL_API_URL="https://api.ocpctl.mg.dog8code.com/v1"

# Sign request with AWS SigV4
aws-sigv4-proxy \
  --name ocpctl-api \
  --region us-east-1 \
  --host api.ocpctl.mg.dog8code.com

# Or use aws-curl wrapper
aws-curl https://api.ocpctl.mg.dog8code.com/v1/clusters
```

### Python Example
```python
import requests

API_BASE = "https://api.ocpctl.mg.dog8code.com/v1"

# List clusters
response = requests.get(f"{API_BASE}/clusters")
clusters = response.json()

# Create cluster
new_cluster = {
    "name": "my-cluster",
    "profile_id": "aws-minimal"
}
response = requests.post(f"{API_BASE}/clusters", json=new_cluster)
```

## Backward Compatibility

The existing path-based API access will continue to work:

```
✅ https://ocpctl.mg.dog8code.com/api/v1/clusters  (still works)
✅ https://api.ocpctl.mg.dog8code.com/v1/clusters  (new preferred method)
```

Both point to the same backend API server.

## Troubleshooting

### DNS Not Resolving

```bash
# Check DNS propagation
dig api.ocpctl.mg.dog8code.com +trace

# Clear local DNS cache (macOS)
sudo dscacheutil -flushcache
sudo killall -HUP mDNSResponder

# Clear local DNS cache (Linux)
sudo systemd-resolve --flush-caches
```

### SSL Certificate Issues

```bash
# Check certificate validity
openssl s_client -connect api.ocpctl.mg.dog8code.com:443 -servername api.ocpctl.mg.dog8code.com < /dev/null

# View certificate details
curl -vI https://api.ocpctl.mg.dog8code.com 2>&1 | grep -A 10 "SSL certificate"
```

### nginx Errors

```bash
# Check nginx error logs
sudo journalctl -u nginx -f

# Test configuration
sudo nginx -t

# Check which configs are enabled
ls -la /etc/nginx/sites-enabled/
```

### CORS Issues

If you get CORS errors from browser-based clients:

1. Check the `Access-Control-Allow-Origin` header is set
2. Verify credentials are being sent correctly
3. Check preflight OPTIONS requests are handled

```bash
# Test CORS preflight
curl -X OPTIONS https://api.ocpctl.mg.dog8code.com/v1/clusters \
  -H "Origin: https://ocpctl.mg.dog8code.com" \
  -H "Access-Control-Request-Method: GET" \
  -v
```

## Security Considerations

1. **Rate Limiting**: Consider enabling rate limiting in nginx (commented out in config)
2. **IP Whitelisting**: For sensitive operations, restrict by IP
3. **API Keys**: Require authentication for all endpoints (already implemented via IAM)
4. **HTTPS Only**: HTTP is redirected to HTTPS automatically
5. **CORS**: Configured to allow credentials from trusted origins

## Monitoring

Add monitoring for the API subdomain:

```bash
# Add to your monitoring system
- name: api_subdomain_health
  url: https://api.ocpctl.mg.dog8code.com/health
  interval: 60s
  expected_status: 200

- name: api_subdomain_ssl
  url: https://api.ocpctl.mg.dog8code.com
  interval: 3600s
  check_ssl_expiry: true
  ssl_days_warning: 30
```

## Next Steps

1. Update web frontend to allow users to choose API endpoint
2. Add API endpoint configuration to user profiles
3. Document API access in README
4. Create example scripts using the new API subdomain
5. Consider adding API versioning (v2, v3) in the future

## References

- nginx proxy configuration: `deploy/nginx/api.ocpctl.conf`
- Main nginx config: `deploy/nginx/ocpctl.conf`
- API server code: `internal/api/`
- IAM authentication: `docs/deployment/IAM_AUTHENTICATION.md`
