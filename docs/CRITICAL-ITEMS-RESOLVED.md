# Critical Items Resolved

This document summarizes the critical architecture and design gaps that have been addressed to make the project implementation-ready.

**Status**: ✅ All critical blocking items resolved
**Date**: 2026-02-27
**Ready for**: MVP Phase 1 implementation

---

## Summary of Changes

### 1. Database Schema Enhancements ✅

**File**: `internal/store/migrations/00001_initial_schema.sql`

**What was missing**: The original design spec defined tables conceptually but lacked:
- Idempotency key management
- RBAC mapping structure
- Worker concurrency locks
- Proper unique constraints

**What was added**:

#### a) Idempotency Keys Table
```sql
CREATE TABLE idempotency_keys (
    id UUID PRIMARY KEY,
    key VARCHAR(255) NOT NULL UNIQUE,
    request_hash VARCHAR(64) NOT NULL,
    response_status_code INTEGER,
    response_body JSONB,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL,
    expires_at TIMESTAMP WITH TIME ZONE NOT NULL
);
```

**Why critical**: Without this, the API cannot implement idempotent operations as required by the design spec (docs/design-specification.md:149). Duplicate cluster creates would bypass policy controls.

#### b) RBAC Mappings Table
```sql
CREATE TABLE rbac_mappings (
    id UUID PRIMARY KEY,
    iam_principal_arn VARCHAR(512) NOT NULL,
    iam_principal_type VARCHAR(50) NOT NULL,
    team VARCHAR(255) NOT NULL,
    role VARCHAR(50) NOT NULL,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    ...
);
```

**Why critical**: The design calls for "IAM identity claims to platform RBAC mapping" but provided no schema. This table enables the authorization layer to map AWS IAM principals to team/role permissions.

#### c) Job Locks Table
```sql
CREATE TABLE job_locks (
    cluster_id VARCHAR(64) PRIMARY KEY,
    job_id VARCHAR(64) NOT NULL,
    locked_at TIMESTAMP WITH TIME ZONE NOT NULL,
    locked_by VARCHAR(255) NOT NULL,
    expires_at TIMESTAMP WITH TIME ZONE NOT NULL
);
```

**Why critical**: Multiple workers could operate on the same cluster simultaneously, corrupting state artifacts. This table provides cluster-scoped locking to prevent catastrophic race conditions.

#### d) Unique Constraint on Active Clusters
```sql
CREATE UNIQUE INDEX idx_unique_active_cluster
ON clusters(name, platform, base_domain)
WHERE status NOT IN ('DESTROYED', 'FAILED');
```

**Why critical**: Prevents users from creating duplicate clusters with the same name, which would cause DNS and cloud resource conflicts.

**Impact**: Database schema is now complete and implementation-ready.

---

### 2. Cluster Profile Definitions ✅

**Files**:
- `internal/profile/definitions/SCHEMA.md` (schema documentation)
- `internal/profile/definitions/aws-minimal-test.yaml`
- `internal/profile/definitions/aws-standard.yaml`
- `internal/profile/definitions/ibm-minimal-test.yaml`
- `internal/profile/definitions/ibm-standard.yaml`

**What was missing**: The design spec described profiles conceptually but didn't define:
- YAML schema structure
- Validation rules
- Actual profile content
- How profiles map to install-config.yaml

**What was added**:

#### Profile Schema
Comprehensive YAML schema covering:
- OpenShift version allowlists
- Region and base domain constraints
- Compute resources (control plane + workers)
- Lifecycle policies (TTL, scaling)
- Required and default tags
- Platform-specific configuration
- Cost controls and estimates

#### All 4 Required Profiles
- **aws-minimal-test**: 3 control plane (schedulable), 0 workers, 72h max TTL
- **aws-standard**: 3 control plane, 3 workers, 168h max TTL
- **ibm-minimal-test**: IBM Cloud equivalent (Phase 2)
- **ibm-standard**: IBM Cloud equivalent (Phase 2)

**Why critical**:
1. The policy engine cannot validate requests without profile definitions
2. Workers cannot render install-config.yaml without profile compute settings
3. Cost reporting needs estimated hourly costs from profiles

**Impact**: Profile-driven policy enforcement can now be implemented.

---

### 3. OpenAPI Specification ✅

**File**: `docs/api.yaml`

**What was missing**: The design spec listed API endpoints but lacked:
- Complete request/response schemas
- Validation rules and constraints
- Error response formats
- Authentication details

**What was added**:

#### Complete OpenAPI 3.0 Specification
- All 8 endpoints fully documented
- Request/response schemas with validation
- AWS Signature V4 authentication
- Idempotency-Key header requirement
- Error handling patterns
- Rate limiting documentation

#### Key Endpoints Defined:
```
POST   /clusters                     # Create cluster
GET    /clusters                     # List clusters
GET    /clusters/{id}                # Get cluster details
POST   /clusters/{id}/destroy        # Destroy cluster
POST   /clusters/{id}/scale-workers  # Scale workers
GET    /clusters/{id}/outputs        # Get credentials
GET    /jobs/{id}                    # Job status
GET    /reports/usage                # Usage report
```

**Why critical**:
1. Frontend developers need contract to build UI
2. CLI tool needs schema for request construction
3. Backend developers need schemas for validation logic
4. Testing teams need spec for contract tests

**Impact**:
- API implementation can begin immediately
- OpenAPI code generation can produce client SDKs
- API testing can be automated against spec

---

### 4. Worker Concurrency Safety Strategy ✅

**File**: `docs/worker-concurrency-safety.md`

**What was missing**: The architecture showed horizontally scaled workers but didn't address:
- How to prevent concurrent operations on same cluster
- Worker crash recovery
- Lock expiry and staleness
- SQS FIFO queue configuration
- State transition safety

**What was added**:

#### Comprehensive Locking Strategy
1. **Database-level job locks** with cluster-scoped exclusivity
2. **PostgreSQL advisory locks** for critical sections
3. **Lock heartbeat mechanism** to handle worker crashes
4. **SQS FIFO queue** with message grouping by cluster ID
5. **State transition validation** enforcing valid status changes

#### Worker Job Processing Algorithm
```
1. Receive SQS message
2. Acquire database lock (row-level + job_locks table)
3. Validate cluster status allows job type
4. Update job status to RUNNING
5. Start lock heartbeat goroutine
6. Execute job (create/destroy/scale)
7. Release lock and update final status
```

#### Safety Guarantees
- ✅ Only one worker operates on a cluster at a time
- ✅ Crashed workers release locks via expiry (60 min TTL)
- ✅ Concurrent submissions serialized by lock acquisition
- ✅ Idempotent job execution for safe retries

**Why critical**: Without this, the platform would have catastrophic failure modes:
- Multiple destroys corrupting cloud state
- Parallel creates causing resource conflicts
- Lost S3 artifacts from concurrent uploads
- Database race conditions

**Impact**: Worker implementation can proceed with confidence in correctness.

---

## Critical Risks Mitigated

| Risk | Mitigation |
|------|-----------|
| **Duplicate cluster creates bypass policy** | Idempotency keys table + API validation |
| **Unauthorized access to clusters** | RBAC mappings table + IAM principal checks |
| **Concurrent worker operations corrupt state** | Job locks table + advisory locks + FIFO queue |
| **Worker crashes leave clusters stuck** | Lock expiry + janitor cleanup of stale locks |
| **Frontend/backend contract misalignment** | OpenAPI specification as single source of truth |
| **Profile validation logic inconsistent** | Formal YAML schema with documented constraints |

---

## Implementation Readiness Checklist

### ✅ Ready to Implement

- [x] Database schema complete with all tables
- [x] Migration file ready for goose
- [x] Cluster profiles defined with all parameters
- [x] OpenAPI spec for API contract
- [x] Worker locking strategy documented
- [x] State transition rules defined
- [x] Idempotency mechanism designed
- [x] RBAC model specified

### 🟡 Ready to Start (requires code)

- [ ] API service Go implementation
- [ ] Worker service Go implementation
- [ ] Janitor service Go implementation
- [ ] CLI tool implementation
- [ ] React frontend implementation
- [ ] Profile validation engine
- [ ] IAM authentication middleware

### 🔴 Blocked (dependencies needed)

None - all blocking items resolved!

---

## Next Steps for MVP Implementation

### Phase 1a: Data Layer (Week 1-2)

1. **Initialize Go module dependencies**
   ```bash
   go get github.com/jackc/pgx/v5
   go get github.com/aws/aws-sdk-go-v2
   # ... other dependencies
   ```

2. **Implement store package**
   - Database connection pool
   - Migration runner integration
   - Query builders for clusters, jobs, locks
   - Transaction helpers

3. **Run migrations**
   ```bash
   make migrate-up
   ```

4. **Write store package tests**
   - Lock acquisition/release
   - Idempotency key validation
   - RBAC mapping lookups

### Phase 1b: Profile Engine (Week 2)

1. **Implement profile loader**
   - YAML parsing
   - Schema validation
   - Profile registry

2. **Build policy engine**
   - Request validation against profile
   - Tag merging logic
   - TTL enforcement

3. **Write profile tests**
   - Load all 4 profiles
   - Validate constraints
   - Test policy violations

### Phase 1c: API Service (Week 3-4)

1. **Implement API server**
   - Chi router setup
   - OpenAPI schema validation
   - IAM authentication middleware
   - Idempotency middleware
   - RBAC authorization

2. **Build API handlers**
   - POST /clusters (create)
   - GET /clusters (list)
   - POST /clusters/{id}/destroy
   - GET /jobs/{id}

3. **Write API integration tests**
   - Against OpenAPI spec
   - IAM auth flows
   - Idempotency behavior

### Phase 1d: Worker Service (Week 5-6)

1. **Implement worker core**
   - SQS queue consumer
   - Lock acquisition logic
   - Heartbeat mechanism
   - Job executor framework

2. **Build job handlers**
   - CREATE job (mock installer for now)
   - DESTROY job (mock installer)
   - S3 artifact upload/download
   - Status updates

3. **Write worker tests**
   - Lock contention
   - Crash recovery
   - Idempotent retries

### Phase 1e: Integration (Week 7-8)

1. **End-to-end testing**
   - Create → READY → Destroy → DESTROYED
   - Concurrent create attempts
   - TTL expiry simulation

2. **Janitor service**
   - TTL destroyer
   - Stale lock cleanup
   - Failed job retry

3. **Basic UI**
   - Cluster create form
   - Cluster list view
   - Job status polling

---

## Open Questions to Resolve Before Production

### Team Decisions Needed

1. **Max concurrent creates per team**
   - Recommendation: 5 concurrent creates, 10 total active clusters per team
   - Rationale: Prevents runaway resource usage

2. **Artifact retention period**
   - Recommendation: 90 days for destroyed clusters
   - Rationale: Balances compliance needs with S3 costs

3. **kubeadmin password access controls**
   - Recommendation: Require break-glass approval for password retrieval
   - Rationale: Least-privilege access, audit trail

4. **IBM Cloud launch timing**
   - Recommendation: Phase 2 (after AWS is stable)
   - Rationale: Validate architecture with one platform first

### Technical Decisions

1. **Workflow orchestrator**
   - Option A: Custom worker implementation (current plan)
   - Option B: Temporal.io for durable workflows
   - Recommendation: Start with Option A, evaluate Temporal in Phase 2

2. **Frontend state management**
   - Recommendation: React Query for API caching + WebSockets for live updates
   - Rationale: Minimal complexity, good developer experience

3. **Metrics backend**
   - Recommendation: Prometheus + Grafana (as specified)
   - Already aligned with design spec

---

## Files Modified/Created

### New Files Created

```
internal/store/migrations/00001_initial_schema.sql
internal/profile/definitions/SCHEMA.md
internal/profile/definitions/aws-minimal-test.yaml
internal/profile/definitions/aws-standard.yaml
internal/profile/definitions/ibm-minimal-test.yaml
internal/profile/definitions/ibm-standard.yaml
docs/api.yaml
docs/worker-concurrency-safety.md
docs/CRITICAL-ITEMS-RESOLVED.md (this file)
```

### Existing Files Referenced

```
docs/architecture.md (reviewed, no changes)
docs/design-specification.md (reviewed, no changes)
README.md (reviewed, no changes)
```

---

## Conclusion

**All critical blocking items have been resolved.** The project is now ready for MVP Phase 1 implementation.

**Key achievements**:
1. ✅ Complete database schema with safety mechanisms
2. ✅ Formal profile definitions and schema
3. ✅ OpenAPI specification for API contract
4. ✅ Worker concurrency safety strategy

**Next milestone**: Complete Phase 1a (data layer) within 2 weeks.

**Confidence level**: High - all major design risks mitigated with concrete solutions.
