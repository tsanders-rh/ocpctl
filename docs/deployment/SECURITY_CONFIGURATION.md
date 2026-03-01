# Security Configuration Guide

This document covers the security features and configuration requirements for ocpctl before deploying to production.

## Table of Contents

- [Critical Security Requirements](#critical-security-requirements)
- [JWT Authentication](#jwt-authentication)
- [Rate Limiting](#rate-limiting)
- [IAM Authentication](#iam-authentication)
- [Audit Logging](#audit-logging)
- [S3 Presigned URLs](#s3-presigned-urls)
- [Worker Health Checks](#worker-health-checks)
- [Security Headers](#security-headers)
- [CORS Configuration](#cors-configuration)

---

## Critical Security Requirements

These settings **MUST** be configured before production deployment:

### 1. JWT Secret

**Environment Variable:** `JWT_SECRET`
**Location:** `deploy/config/api.env`

```bash
# Generate a strong random secret (minimum 32 characters)
JWT_SECRET=$(openssl rand -base64 48)
```

**Important:**
- Never use the default value in production
- Keep this secret secure and rotate it periodically
- Store in a secrets management system (AWS Secrets Manager, HashiCorp Vault, etc.)
- If this secret is compromised, all JWT tokens become invalid and users must re-authenticate

### 2. Database SSL

**Environment Variable:** `DATABASE_URL`
**Location:** `deploy/config/api.env`, `deploy/config/worker.env`

```bash
# Always use sslmode=require in production
DATABASE_URL=postgres://user:password@host:5432/ocpctl?sslmode=require
```

### 3. CORS Configuration

**Environment Variables:**
- `CORS_ALLOWED_ORIGINS` (API)
- Frontend URL configuration (Web)

```bash
# API Configuration (deploy/config/api.env)
CORS_ALLOWED_ORIGINS=https://ocpctl.example.com

# Web Configuration (deploy/config/web.env)
# Ensure the API server's CORS allows this origin
NEXT_PUBLIC_API_URL=https://api.ocpctl.example.com/api/v1
```

### 4. Environment Indicator

**Environment Variable:** `ENVIRONMENT`

```bash
# Set to "production" to enable strict security checks
ENVIRONMENT=production
```

When `ENVIRONMENT=production`:
- API server fails to start if JWT_SECRET is not set or uses default value
- Worker fails to start if OPENSHIFT_PULL_SECRET is missing or invalid
- Enhanced security logging and validation

---

## JWT Authentication

### Configuration

The API server uses JWT (JSON Web Tokens) for stateless authentication.

**Required Environment Variables:**

```bash
# API Server (deploy/config/api.env)
JWT_SECRET=<strong-random-secret-min-32-chars>
```

### Token Lifecycle

- **Access Tokens:** Short-lived (15 minutes)
- **Refresh Tokens:** Long-lived (7 days), stored in database
- **HttpOnly Cookies:** Refresh tokens sent as secure cookies

### Password Requirements

Default admin user is created with:
- **Username:** `admin`
- **Default Password:** `admin` (MUST be changed immediately)

Password hashing:
- Algorithm: bcrypt
- Cost factor: 12 (configurable via `BCRYPT_COST`)

### Security Features

1. **Token Refresh:** Automatic access token refresh before expiration
2. **Revocation:** Refresh tokens can be revoked per-user
3. **Request ID Tracking:** All authentication events logged with request IDs

---

## Rate Limiting

Protects against brute force attacks and DoS attempts.

### Configuration

**Environment Variable:** `RATE_LIMIT_REQUESTS` (optional, defaults: 100 req/min global)

**Built-in Rate Limits:**

| Endpoint | Limit | Burst | Purpose |
|----------|-------|-------|---------|
| Global | 100/min | 10 | General API protection |
| `/auth/login` | 5/min | 1 | Prevent brute force |
| `/clusters` (POST) | 10/min | 1 | Prevent resource abuse |

### Customization

Edit `internal/api/server.go` to adjust limits:

```go
// Global rate limiting
s.echo.Use(apimiddleware.RateLimit(100, 10)) // 100 req/min, burst 10

// Strict rate limit for login
authGroup.POST("/login", authHandler.Login, apimiddleware.StrictRateLimit(5))
```

### Implementation Details

- **Per-IP tracking:** Rate limits are enforced per client IP
- **Token bucket algorithm:** Uses golang.org/x/time/rate
- **Automatic cleanup:** Inactive limiters removed every 5 minutes
- **HTTP 429 Response:** "Too Many Requests" with retry-after guidance

---

## IAM Authentication

AWS IAM authentication allows users to authenticate using their AWS credentials.

### Backend Configuration

**Environment Variable:** `ENABLE_IAM_AUTH=true`

**IAM Permissions Required:**

Users authenticating with IAM need:

```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Action": [
        "sts:GetCallerIdentity"
      ],
      "Resource": "*"
    }
  ]
}
```

### Frontend Configuration

**Next.js API Routes:**

Two server-side routes handle IAM authentication:

1. `/api/auth/iam/verify` - Verifies AWS credentials via STS
2. `/api/auth/iam/sign` - Signs API requests with AWS SigV4

**Required npm packages:**

```json
{
  "@aws-sdk/client-sts": "^3.1000.0",
  "@smithy/signature-v4": "^5.3.10",
  "@smithy/protocol-http": "^5.3.10",
  "@aws-crypto/sha256-js": "^5.2.0"
}
```

### Usage

Users can authenticate with:
- AWS Access Key ID
- AWS Secret Access Key
- Session Token (optional, for temporary credentials)
- AWS Region

The IAM auth provider:
1. Sends credentials to `/api/auth/iam/verify`
2. Server verifies with AWS STS `GetCallerIdentity`
3. For each API request, calls `/api/auth/iam/sign` to generate SigV4 headers
4. Sends signed request to ocpctl API

**Security Note:** AWS SDK operations are performed server-side only, as they require Node.js APIs not available in browsers.

---

## Audit Logging

Tracks all security-relevant user actions.

### Logged Events

| Event Type | Action | Details Captured |
|------------|--------|-----------------|
| User Management | `user.create` | Email, role |
| User Management | `user.update` | Changed fields |
| User Management | `user.delete` | User ID |
| Cluster Operations | `cluster.create` | Cluster ID, name, user ID |
| Cluster Operations | `cluster.delete` | Cluster ID, name, user ID |
| Kubeconfig Access | `kubeconfig.download` | Cluster ID, name |

### Database Schema

```sql
CREATE TABLE audit_events (
    id UUID PRIMARY KEY,
    actor VARCHAR(255) NOT NULL,  -- User performing action
    action VARCHAR(100) NOT NULL,  -- Action type
    target_cluster_id UUID REFERENCES clusters(id),
    target_job_id UUID REFERENCES jobs(id),
    target_user_id UUID REFERENCES users(id),
    status VARCHAR(50) NOT NULL,  -- success/failure
    metadata JSONB,  -- Additional context
    ip_address INET,  -- Client IP
    user_agent TEXT,  -- Client user agent
    created_at TIMESTAMP DEFAULT NOW()
);
```

### Querying Audit Logs

```sql
-- User actions
SELECT * FROM audit_events WHERE actor = 'user-id' ORDER BY created_at DESC;

-- Cluster events
SELECT * FROM audit_events WHERE target_cluster_id = 'cluster-id';

-- Failed events (potential security issues)
SELECT * FROM audit_events WHERE status = 'failure';
```

### Retention

- Audit events are immutable (no UPDATE or DELETE)
- Implement retention policy via scheduled cleanup job
- Recommended: Archive to S3 or data warehouse before deletion

---

## S3 Presigned URLs

Provides secure, time-limited access to kubeconfig files stored in S3.

### Configuration

**Required IAM Permissions:**

The API server needs:

```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Action": [
        "s3:GetObject"
      ],
      "Resource": "arn:aws:s3:::your-bucket/kubeconfigs/*"
    }
  ]
}
```

### Implementation Details

- **Expiration:** 15 minutes (configurable in code)
- **Endpoint:** `GET /api/v1/clusters/:id/kubeconfig/download-url`
- **Authentication:** Requires valid JWT or IAM auth
- **Authorization:** User must own the cluster or be admin

### Response Format

```json
{
  "download_url": "https://bucket.s3.amazonaws.com/path?X-Amz-Signature=...",
  "expires_in": "15 minutes",
  "filename": "kubeconfig-my-cluster.yaml"
}
```

### Frontend Integration

```javascript
// Get presigned URL
const response = await fetch(`/api/v1/clusters/${clusterId}/kubeconfig/download-url`);
const { download_url, filename } = await response.json();

// Trigger download
const link = document.createElement('a');
link.href = download_url;
link.download = filename;
link.click();
```

---

## Worker Health Checks

Enables monitoring and orchestration of worker services.

### Configuration

**Environment Variable:** `WORKER_HEALTH_PORT` (default: 8081)

```bash
# Worker Configuration (deploy/config/worker.env)
WORKER_HEALTH_PORT=8081
```

### Endpoints

#### GET /health

Returns 200 OK if worker service is running.

**Response:**
```json
{
  "status": "healthy",
  "timestamp": "2026-03-01T12:00:00Z"
}
```

#### GET /ready

Returns 200 OK if worker is ready to process jobs (database connected, initialized).

**Success Response (200):**
```json
{
  "status": "ready",
  "timestamp": "2026-03-01T12:00:00Z"
}
```

**Not Ready Response (503):**
```json
{
  "status": "not_ready",
  "reason": "database_unavailable"
}
```

### Integration

**Kubernetes Probes:**

```yaml
livenessProbe:
  httpGet:
    path: /health
    port: 8081
  initialDelaySeconds: 10
  periodSeconds: 30

readinessProbe:
  httpGet:
    path: /ready
    port: 8081
  initialDelaySeconds: 5
  periodSeconds: 10
```

**Load Balancer Health Checks:**
- Use `/ready` endpoint
- Check interval: 30 seconds
- Unhealthy threshold: 2 consecutive failures

---

## Security Headers

The Next.js web frontend includes comprehensive security headers.

### Configuration

Headers are configured in `web/next.config.mjs`:

```javascript
async headers() {
  return [
    {
      source: '/:path*',
      headers: [
        {
          key: 'X-DNS-Prefetch-Control',
          value: 'on',
        },
        {
          key: 'Strict-Transport-Security',
          value: 'max-age=63072000; includeSubDomains; preload',
        },
        {
          key: 'X-Frame-Options',
          value: 'SAMEORIGIN',
        },
        {
          key: 'X-Content-Type-Options',
          value: 'nosniff',
        },
        {
          key: 'X-XSS-Protection',
          value: '1; mode=block',
        },
        {
          key: 'Referrer-Policy',
          value: 'origin-when-cross-origin',
        },
        {
          key: 'Permissions-Policy',
          value: 'camera=(), microphone=(), geolocation=()',
        },
      ],
    },
  ];
},
```

### Header Descriptions

| Header | Purpose | Value |
|--------|---------|-------|
| `Strict-Transport-Security` | Force HTTPS | 2 years, include subdomains |
| `X-Frame-Options` | Prevent clickjacking | Same origin only |
| `X-Content-Type-Options` | Prevent MIME-sniffing | nosniff |
| `X-XSS-Protection` | XSS filter | Enabled with blocking |
| `Referrer-Policy` | Control referer info | Origin only on cross-origin |
| `Permissions-Policy` | Disable unused features | Camera, mic, location disabled |

---

## CORS Configuration

Controls which origins can access the API.

### API Server

**Environment Variable:** `CORS_ALLOWED_ORIGINS`

```bash
# Single origin
CORS_ALLOWED_ORIGINS=https://ocpctl.example.com

# Multiple origins (comma-separated)
CORS_ALLOWED_ORIGINS=https://ocpctl.example.com,https://staging.ocpctl.example.com
```

### Production Checklist

- [ ] Set CORS_ALLOWED_ORIGINS to actual frontend URL(s)
- [ ] Never use `*` (wildcard) in production
- [ ] Ensure protocol (https://) is included
- [ ] Match exact hostname (including subdomain)
- [ ] Test CORS from browser before go-live

### Troubleshooting

**Issue:** `CORS policy: No 'Access-Control-Allow-Origin' header`

**Solutions:**
1. Verify CORS_ALLOWED_ORIGINS includes your frontend URL
2. Check API server logs for CORS configuration
3. Ensure frontend URL matches exactly (https vs http, trailing slash)
4. Restart API server after configuration changes

---

## Request ID Propagation

All requests are assigned unique IDs for tracing across services.

### Format

Structured logging with consistent request ID format:

```
[INFO] request_id=abc-123 method=POST path=/api/v1/clusters message="cluster created" cluster_id=xyz
[ERROR] request_id=abc-123 method=POST path=/api/v1/clusters error="database unavailable"
```

### Implementation

**Middleware:** Echo's built-in RequestID middleware
**Header:** `X-Request-ID`

**Usage in Code:**

```go
import "github.com/tsanders-rh/ocpctl/internal/api"

// Info logging
api.LogInfo(c, "operation completed", "key1", value1, "key2", value2)

// Warning logging
api.LogWarning(c, "potential issue detected", "reason", reason)

// Error logging with generic response
return api.LogAndReturnGenericError(c, fmt.Errorf("detailed error: %w", err))
```

### Benefits

- **Request Tracing:** Follow a request through all log entries
- **Debugging:** Quickly identify all events related to a specific request
- **Audit Correlation:** Link audit events to request IDs
- **Performance Analysis:** Track request latency end-to-end

---

## Security Best Practices

### Pre-Deployment Checklist

- [ ] Generate and configure strong JWT_SECRET
- [ ] Enable SSL for database connections (sslmode=require)
- [ ] Configure CORS_ALLOWED_ORIGINS with production URLs
- [ ] Set ENVIRONMENT=production
- [ ] Change default admin password immediately
- [ ] Configure OPENSHIFT_PULL_SECRET for worker
- [ ] Set up audit log retention policy
- [ ] Configure monitoring for health check endpoints
- [ ] Test IAM authentication if enabled
- [ ] Verify rate limiting on login endpoint
- [ ] Enable HSTS preloading for domain
- [ ] Configure database backups
- [ ] Set up CloudWatch or log aggregation
- [ ] Review and test error messages (no internal details exposed)

### Ongoing Security

1. **Rotate Secrets:** Regularly rotate JWT_SECRET and database passwords
2. **Monitor Audit Logs:** Watch for suspicious patterns (multiple failed logins, unusual access)
3. **Update Dependencies:** Keep Go modules and npm packages up to date
4. **Security Scanning:** Run security scanners (gosec, npm audit)
5. **Review Access:** Periodically audit user roles and permissions

---

## Additional Resources

- [Deployment Guide](./DEPLOYMENT_WEB.md)
- [Architecture Documentation](../architecture/architecture.md)
- [API Reference](../README.md)
- [Security Issue #2](https://github.com/tsanders-rh/ocpctl/issues/2)
