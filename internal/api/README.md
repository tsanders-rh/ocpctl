# API Layer

The API layer provides a RESTful HTTP interface for the ocpctl cluster management system. Built with Echo framework, it exposes endpoints for cluster lifecycle management, profile queries, and job monitoring.

## Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                         API Layer                           │
│                                                             │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐     │
│  │   Cluster    │  │   Profile    │  │     Job      │     │
│  │   Handlers   │  │   Handlers   │  │   Handlers   │     │
│  └──────┬───────┘  └──────┬───────┘  └──────┬───────┘     │
│         │                  │                  │             │
│         └──────────────────┴──────────────────┘             │
│                            │                                │
│         ┌──────────────────┴──────────────────┐             │
│         │                                     │             │
│         ▼                                     ▼             │
│  ┌─────────────┐                      ┌─────────────┐      │
│  │   Policy    │                      │    Store    │      │
│  │   Engine    │                      │   (Data)    │      │
│  └─────────────┘                      └─────────────┘      │
└─────────────────────────────────────────────────────────────┘
```

## Server Configuration

### Environment Variables

```bash
DATABASE_URL=postgres://localhost:5432/ocpctl?sslmode=disable
PROFILES_DIR=internal/profile/definitions
PORT=8080
```

### Server Config Options

- **Port**: HTTP server port (default: 8080)
- **EnableCORS**: Enable CORS middleware (default: true)
- **EnableAuth**: Enable authentication (default: false for Phase 1)
- **MaxBodySize**: Maximum request body size (default: 1M)
- **RateLimitRequests**: Requests per minute limit (default: 100)
- **ShutdownTimeout**: Graceful shutdown timeout (default: 10s)

## Middleware Stack

The server applies middleware in the following order:

1. **Recover**: Panic recovery with stack traces
2. **RequestID**: Unique ID for request tracing
3. **Logger**: Structured JSON logging
4. **CORS**: Cross-origin resource sharing
5. **BodyLimit**: Request size limiting
6. **RateLimiter**: Rate limiting by IP
7. **Timeout**: Request timeout (30s)

## API Endpoints

### Health & Readiness

#### `GET /health`

Basic health check. Always returns 200 if server is running.

**Response**:
```json
{
  "status": "healthy",
  "time": "2026-02-27T21:00:00Z"
}
```

#### `GET /ready`

Readiness check. Verifies database connectivity.

**Response** (200):
```json
{
  "status": "ready",
  "time": "2026-02-27T21:00:00Z"
}
```

**Response** (503):
```json
{
  "status": "not ready",
  "error": "database unavailable"
}
```

---

### Clusters

#### `POST /api/v1/clusters`

Create a new cluster. Validates request against profile policies, creates cluster record, and enqueues provision job.

**Request Body**:
```json
{
  "name": "my-test-cluster",
  "platform": "aws",
  "version": "4.20.3",
  "profile": "aws-minimal-test",
  "region": "us-east-1",
  "base_domain": "labs.example.com",
  "owner": "user@example.com",
  "team": "engineering",
  "cost_center": "cc-123",
  "ttl_hours": 24,
  "ssh_public_key": "ssh-rsa AAAA...",
  "extra_tags": {
    "Project": "demo"
  },
  "offhours_opt_in": true,
  "idempotency_key": "unique-request-id"
}
```

**Validation**:
- Name: DNS-compatible (lowercase alphanumeric + hyphens, 3-63 chars)
- Platform: Must be `aws` or `ibmcloud`
- Version: Must be in profile's allowlist
- Region: Must be in profile's allowlist
- BaseDomain: Must be in profile's allowlist
- TTL: Must be <= profile's max TTL
- Tags: Cannot override reserved keys (ManagedBy, ClusterName, Owner, etc.)

**Response** (201):
```json
{
  "id": "f47ac10b-58cc-4372-a567-0e02b2c3d479",
  "name": "my-test-cluster",
  "platform": "aws",
  "version": "4.20.3",
  "profile": "aws-minimal-test",
  "region": "us-east-1",
  "base_domain": "labs.example.com",
  "status": "pending",
  "owner": "user@example.com",
  "team": "engineering",
  "cost_center": "cc-123",
  "ttl_hours": 24,
  "tags": {
    "ManagedBy": "cluster-control-plane",
    "ClusterName": "my-test-cluster",
    "Owner": "user@example.com",
    "Team": "engineering",
    "CostCenter": "cc-123",
    "Profile": "aws-minimal-test",
    "Platform": "aws",
    "Project": "demo",
    "Environment": "test"
  },
  "destroy_at": "2026-02-28T21:00:00Z",
  "offhours_opt_in": true,
  "created_at": "2026-02-27T21:00:00Z",
  "updated_at": "2026-02-27T21:00:00Z"
}
```

**Error Response** (422 - Validation Failed):
```json
{
  "error": "validation_failed",
  "message": "Request validation failed",
  "details": [
    {
      "field": "version",
      "message": "version 4.19.0 not in profile allowlist: [4.20.3, 4.20.4, 4.20.5]"
    }
  ]
}
```

**Idempotency**:
- Include `idempotency_key` to ensure exactly-once semantics
- Keys are valid for 24 hours
- Duplicate requests return the original cluster (200 instead of 201)

---

#### `GET /api/v1/clusters`

List clusters with pagination and filtering.

**Query Parameters**:
- `page`: Page number (default: 1)
- `per_page`: Items per page (default: 50, max: 100)
- `platform`: Filter by platform (aws, ibmcloud)
- `profile`: Filter by profile name
- `owner`: Filter by owner email
- `team`: Filter by team
- `cost_center`: Filter by cost center
- `status`: Filter by status (pending, provisioning, ready, error, deleting, deleted)

**Example**: `GET /api/v1/clusters?page=2&per_page=25&platform=aws&status=ready`

**Response** (200):
```json
{
  "data": [
    {
      "id": "f47ac10b-58cc-4372-a567-0e02b2c3d479",
      "name": "my-test-cluster",
      "platform": "aws",
      "status": "ready",
      ...
    }
  ],
  "pagination": {
    "page": 2,
    "per_page": 25,
    "total": 150,
    "total_pages": 6
  },
  "filters": {
    "platform": "aws",
    "status": "ready"
  }
}
```

---

#### `GET /api/v1/clusters/:id`

Get a single cluster by ID.

**Response** (200):
```json
{
  "id": "f47ac10b-58cc-4372-a567-0e02b2c3d479",
  "name": "my-test-cluster",
  "platform": "aws",
  ...
}
```

**Response** (404):
```json
{
  "error": "not_found",
  "message": "Cluster not found"
}
```

---

#### `DELETE /api/v1/clusters/:id`

Delete a cluster. Sets status to `deleting` and enqueues deprovision job.

**Response** (200):
```json
{
  "id": "f47ac10b-58cc-4372-a567-0e02b2c3d479",
  "name": "my-test-cluster",
  "status": "deleting",
  ...
}
```

**Response** (409):
```json
{
  "error": "conflict",
  "message": "Cluster is already being deleted"
}
```

---

#### `PATCH /api/v1/clusters/:id/extend`

Extend cluster TTL (postpone automatic deletion).

**Request Body**:
```json
{
  "ttl_hours": 48
}
```

**Response** (200):
```json
{
  "id": "f47ac10b-58cc-4372-a567-0e02b2c3d479",
  "ttl_hours": 48,
  "destroy_at": "2026-03-01T21:00:00Z",
  ...
}
```

---

### Profiles

#### `GET /api/v1/profiles`

List available cluster profiles.

**Query Parameters**:
- `platform`: Filter by platform (aws, ibmcloud)

**Example**: `GET /api/v1/profiles?platform=aws`

**Response** (200):
```json
[
  {
    "name": "aws-minimal-test",
    "display_name": "AWS Minimal Test Cluster",
    "description": "Compact 3-node cluster for AWS testing",
    "platform": "aws",
    "enabled": true,
    "openshift_versions": {
      "allowlist": ["4.20.3", "4.20.4", "4.20.5"],
      "default": "4.20.3"
    },
    "regions": {
      "allowlist": ["us-east-1", "us-west-2"],
      "default": "us-east-1"
    },
    "base_domains": {
      "allowlist": ["labs.example.com"],
      "default": "labs.example.com"
    },
    "compute": {
      "control_plane": {
        "replicas": 3,
        "instance_type": "m6i.xlarge",
        "schedulable": true
      },
      "workers": {
        "replicas": 0,
        "min_replicas": 0,
        "max_replicas": 3,
        "instance_type": "m6i.2xlarge"
      }
    },
    "lifecycle": {
      "max_ttl_hours": 72,
      "default_ttl_hours": 24,
      "allow_custom_ttl": true
    },
    "features": {
      "offhours_scaling": true,
      "fips_mode": false,
      "private_cluster": false
    },
    ...
  }
]
```

---

#### `GET /api/v1/profiles/:name`

Get a single profile by name.

**Example**: `GET /api/v1/profiles/aws-minimal-test`

**Response** (200):
```json
{
  "name": "aws-minimal-test",
  "display_name": "AWS Minimal Test Cluster",
  ...
}
```

**Response** (404):
```json
{
  "error": "not_found",
  "message": "Profile not found: profile 'invalid-name' not found"
}
```

---

### Jobs

#### `GET /api/v1/jobs`

List background jobs with pagination and filtering.

**Query Parameters**:
- `page`: Page number (default: 1)
- `per_page`: Items per page (default: 50, max: 100)
- `cluster_id`: Filter by cluster ID
- `type`: Filter by job type (provision, deprovision, ttl_check)
- `status`: Filter by status (pending, running, completed, failed)

**Example**: `GET /api/v1/jobs?cluster_id=f47ac10b-58cc-4372-a567-0e02b2c3d479&status=completed`

**Response** (200):
```json
{
  "data": [
    {
      "id": "a1b2c3d4-e5f6-4a5b-8c9d-0e1f2a3b4c5d",
      "cluster_id": "f47ac10b-58cc-4372-a567-0e02b2c3d479",
      "type": "provision",
      "status": "completed",
      "attempt": 1,
      "max_attempts": 3,
      "created_at": "2026-02-27T21:00:00Z",
      "started_at": "2026-02-27T21:00:10Z",
      "completed_at": "2026-02-27T21:15:32Z"
    }
  ],
  "pagination": {
    "page": 1,
    "per_page": 50,
    "total": 1,
    "total_pages": 1
  },
  "filters": {
    "cluster_id": "f47ac10b-58cc-4372-a567-0e02b2c3d479",
    "status": "completed"
  }
}
```

---

#### `GET /api/v1/jobs/:id`

Get a single job by ID.

**Response** (200):
```json
{
  "id": "a1b2c3d4-e5f6-4a5b-8c9d-0e1f2a3b4c5d",
  "cluster_id": "f47ac10b-58cc-4372-a567-0e02b2c3d479",
  "type": "provision",
  "status": "completed",
  ...
}
```

**Response** (404):
```json
{
  "error": "not_found",
  "message": "Job not found"
}
```

---

## Error Responses

All error responses follow a consistent format:

```json
{
  "error": "error_code",
  "message": "Human-readable error message",
  "details": [
    {
      "field": "field_name",
      "message": "Field-specific error"
    }
  ]
}
```

### Error Codes

- `bad_request` (400): Invalid request syntax or parameters
- `unauthorized` (401): Authentication required (Phase 2)
- `forbidden` (403): Insufficient permissions (Phase 2)
- `not_found` (404): Resource not found
- `conflict` (409): Resource state conflict
- `validation_failed` (422): Request validation failed
- `internal_error` (500): Internal server error
- `service_unavailable` (503): Service temporarily unavailable

## Running the Server

### Local Development

```bash
# Start PostgreSQL
docker run -d \
  --name ocpctl-db \
  -e POSTGRES_DB=ocpctl \
  -e POSTGRES_PASSWORD=password \
  -p 5432:5432 \
  postgres:15

# Run migrations
export DATABASE_URL="postgres://postgres:password@localhost:5432/ocpctl?sslmode=disable"
go run cmd/api/main.go

# Server starts on http://localhost:8080
```

### Production

```bash
export DATABASE_URL="postgres://user:pass@prod-db:5432/ocpctl?sslmode=disable"
export PROFILES_DIR="/etc/ocpctl/profiles"
export PORT=8080

./ocpctl-api
```

## Testing

```bash
# Run all API tests
go test ./internal/api/... -v

# Run with coverage
go test ./internal/api/... -cover

# Test specific handler
go test ./internal/api/handlers -run TestClusterHandler_Create -v
```

## Next Steps (Future Phases)

- **Phase 2**: Add AWS IAM Identity Center authentication
- **Phase 2**: Implement RBAC authorization middleware
- **Phase 3**: Add WebSocket support for real-time job updates
- **Phase 3**: Add metrics endpoint (Prometheus format)
- **Phase 3**: Add distributed tracing (OpenTelemetry)
