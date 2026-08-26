# API Subdomain Setup Guide

This guide explains how to set up a dedicated subdomain for direct API access at `api.ocpctl.<BASE_DOMAIN>`.

## Overview

Instead of accessing the API through the path-based proxy:
```
https://ocpctl.<BASE_DOMAIN>/api/v1/clusters
```

Users can access it through a dedicated subdomain:
```
https://api.ocpctl.<BASE_DOMAIN>/v1/clusters
```

## Benefits

- **Cleaner URLs**: No `/api/` prefix needed
- **Separation of concerns**: API traffic separated from web traffic
- **Better for programmatic access**: Clearer endpoint for CLI tools and scripts
- **Independent scaling**: Can route API traffic to different backends in the future

## Prerequisites

- Access to DNS management for `<BASE_DOMAIN>`
- SSH access to the production EC2 instance
- Ability to update SSL certificates

## Step 1: DNS Configuration

Create a CNAME record in your DNS provider (Route 53, Cloudflare, etc.):

```
Type: CNAME
Name: api.ocpctl.<BASE_DOMAIN>
Value: ocpctl.<BASE_DOMAIN>
TTL: 300 (5 minutes)
```

Or if using an A record:
```
Type: A
Name: api.ocpctl.<BASE_DOMAIN>
Value: <EC2-PUBLIC-IP>  # Currently <PROD_HOST>
TTL: 300
```

### Route 53 Example

```bash
# Get the hosted zone ID
ZONE_ID=$(aws route53 list-hosted-zones --query "HostedZones[?Name=='<BASE_DOMAIN>.'].Id" --output text | cut -d'/' -f3)

# Create the CNAME record
aws route53 change-resource-record-sets --hosted-zone-id $ZONE_ID --change-batch '{
  "Changes": [{
    "Action": "CREATE",
    "ResourceRecordSet": {
      "Name": "api.ocpctl.<BASE_DOMAIN>",
      "Type": "CNAME",
      "TTL": 300,
      "ResourceRecords": [{"Value": "ocpctl.<BASE_DOMAIN>"}]
    }
  }]
}'
```

### Verify DNS

Wait a few minutes for propagation, then verify:

```bash
# Should return the EC2 IP
dig api.ocpctl.<BASE_DOMAIN>

# Or
nslookup api.ocpctl.<BASE_DOMAIN>
```

## Step 2: Update SSL Certificate

The SSL certificate must include `api.ocpctl.<BASE_DOMAIN>` as a Subject Alternative Name (SAN).

### Option A: Let's Encrypt (Recommended)

If using Let's Encrypt, update the certificate to include both domains:

```bash
# SSH to production server
ssh -i ~/.ssh/<PROD_SSH_KEY> ubuntu@<PROD_HOST>

# Install certbot if not already installed
sudo apt-get update
sudo apt-get install -y certbot python3-certbot-nginx

# Stop nginx temporarily
sudo systemctl stop nginx

# Request certificate for both domains
sudo certbot certonly --standalone \
  -d ocpctl.<BASE_DOMAIN> \
  -d api.ocpctl.<BASE_DOMAIN> \
  --email your-email@example.com \
  --agree-tos \
  --non-interactive

# Update nginx to use Let's Encrypt certs
sudo sed -i 's|/etc/ssl/certs/ocpctl.crt|/etc/letsencrypt/live/ocpctl.<BASE_DOMAIN>/fullchain.pem|g' /etc/nginx/sites-available/*.conf
sudo sed -i 's|/etc/ssl/private/ocpctl.key|/etc/letsencrypt/live/ocpctl.<BASE_DOMAIN>/privkey.pem|g' /etc/nginx/sites-available/*.conf

# Set up auto-renewal
sudo certbot renew --dry-run
```

### Option B: Existing Certificate

If using an existing certificate, ensure it includes both:
- `ocpctl.<BASE_DOMAIN>`
- `api.ocpctl.<BASE_DOMAIN>`

Verify with:
```bash
openssl x509 -in /etc/ssl/certs/ocpctl.crt -text -noout | grep DNS
```

## Step 3: Deploy nginx Configuration

```bash
# From your local machine, copy the new config
scp -i ~/.ssh/<PROD_SSH_KEY> \
  deploy/nginx/api.ocpctl.conf \
  ubuntu@<PROD_HOST>:/tmp/

# SSH to production server
ssh -i ~/.ssh/<PROD_SSH_KEY> ubuntu@<PROD_HOST>

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
curl https://api.ocpctl.<BASE_DOMAIN>/health

# API version endpoint
curl https://api.ocpctl.<BASE_DOMAIN>/v1/system/version

# Swagger documentation
open https://api.ocpctl.<BASE_DOMAIN>/swagger/index.html
```

## Step 5: Update Documentation

Update any documentation that references the API URL:

### Before
```
API Endpoint: https://ocpctl.<BASE_DOMAIN>/api/v1
```

### After
```
API Endpoint: https://api.ocpctl.<BASE_DOMAIN>/v1
Web UI: https://ocpctl.<BASE_DOMAIN>
```

## API Usage Examples

### Using curl
```bash
# Get clusters
curl https://api.ocpctl.<BASE_DOMAIN>/v1/clusters

# Create a cluster
curl -X POST https://api.ocpctl.<BASE_DOMAIN>/v1/clusters \
  -H "Content-Type: application/json" \
  -d '{
    "name": "my-cluster",
    "profile_id": "aws-minimal"
  }'
```

### Using AWS CLI with IAM Auth
```bash
# Set API base URL
export OCPCTL_API_URL="https://api.ocpctl.<BASE_DOMAIN>/v1"

# Sign request with AWS SigV4
aws-sigv4-proxy \
  --name ocpctl-api \
  --region us-east-1 \
  --host api.ocpctl.<BASE_DOMAIN>

# Or use aws-curl wrapper
aws-curl https://api.ocpctl.<BASE_DOMAIN>/v1/clusters
```

### Python Example
```python
import requests

API_BASE = "https://api.ocpctl.<BASE_DOMAIN>/v1"

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
✅ https://ocpctl.<BASE_DOMAIN>/api/v1/clusters  (still works)
✅ https://api.ocpctl.<BASE_DOMAIN>/v1/clusters  (new preferred method)
```

Both point to the same backend API server.

## Troubleshooting

### DNS Not Resolving

```bash
# Check DNS propagation
dig api.ocpctl.<BASE_DOMAIN> +trace

# Clear local DNS cache (macOS)
sudo dscacheutil -flushcache
sudo killall -HUP mDNSResponder

# Clear local DNS cache (Linux)
sudo systemd-resolve --flush-caches
```

### SSL Certificate Issues

```bash
# Check certificate validity
openssl s_client -connect api.ocpctl.<BASE_DOMAIN>:443 -servername api.ocpctl.<BASE_DOMAIN> < /dev/null

# View certificate details
curl -vI https://api.ocpctl.<BASE_DOMAIN> 2>&1 | grep -A 10 "SSL certificate"
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
curl -X OPTIONS https://api.ocpctl.<BASE_DOMAIN>/v1/clusters \
  -H "Origin: https://ocpctl.<BASE_DOMAIN>" \
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
  url: https://api.ocpctl.<BASE_DOMAIN>/health
  interval: 60s
  expected_status: 200

- name: api_subdomain_ssl
  url: https://api.ocpctl.<BASE_DOMAIN>
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
