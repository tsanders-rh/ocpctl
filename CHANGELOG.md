# Changelog

All notable changes to ocpctl will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Planned
- CloudWatch/Prometheus metrics integration
- Enhanced monitoring dashboards
- Automated backups and disaster recovery
- IBM Cloud cluster execution
- Multi-region support
- Cost reporting and analytics

## [0.20.0] - 2024-03-17

### Added - Phase 2: AWS Resource Tagging (Issue #15)

**Comprehensive Resource Tagging:**
- Automatic tagging of ALL AWS resources created by OpenShift clusters
- Tags applied to EC2, ELB, Route53, S3, and IAM resources
- Standard tag format with `ManagedBy: ocpctl` identifier
- Parallel execution across 5 AWS services (~5 seconds total)
- Post-cluster-creation tagging (after 30-45 min IAM eventual consistency)

**Tag Format:**
```
ManagedBy: ocpctl
ClusterName: <cluster-name>
Profile: <profile-name>
InfraID: <openshift-infra-id>
CreatedAt: <timestamp>
OcpctlVersion: <version>
kubernetes.io/cluster/<infraID>: owned
```

**Orphan Detection Improvements:**
- Hybrid detection: tag-based (primary) + pattern matching (fallback)
- Eliminates false positives from non-ocpctl OpenShift clusters
- Checks for `ManagedBy=ocpctl` tag before flagging resources as orphaned
- Backward compatible with clusters created before Phase 2

**Retroactive Tagging Tool:**
- New CLI tool: `tag-aws-resources`
- Tags existing cluster resources for improved tracking
- Auto-discovers infraID from AWS VPC tags
- Dry-run mode for safe preview
- Bulk tagging support via shell script

**IAM Permissions:**
- New IAM policy: `OCPCTLTaggingPolicy`
- Permissions for EC2, ELB, Route53, S3, IAM tagging
- Setup guide: `deploy/IAM-SETUP.md`
- Applied to worker instance role

**Documentation:**
- [AWS Resource Management Guide](docs/user-guide/aws-resource-management.md) - Complete user guide
- [Resource Tagging Operations](docs/operations/resource-tagging-operations.md) - Operations runbook
- [Retroactive Tagging Tool](cmd/tag-aws-resources/README.md) - Tool documentation
- [Feature Matrix](docs/reference/FEATURE_MATRIX.md) - Comprehensive feature overview
- Updated README.md with Phase 2 completion

### Changed
- Orphan detection now prefers `ManagedBy` tag over pattern matching
- VPC orphan detection supports both tag-based and legacy pattern matching
- IAM role orphan detection supports both tag-based and legacy pattern matching
- Worker handler calls `TagAWSResources()` instead of `TagIAMResources()`

### Fixed
- False positives in orphan detection from non-ocpctl OpenShift clusters
- IAM role detection pagination for accounts with 1000+ roles
- Compilation error in orphan detector (unused infraID variable)

### Performance
- Tagging execution reduced from ~25s (sequential) to ~5s (parallel)
- Batch operations: EC2 (1000 resources/call), ELB (20 resources/call)
- Concurrent goroutines for EC2, ELB, Route53, S3, IAM services

### Deployment
- Version: `v0.20260317.bca1feb`
- Deployed to production API (52.90.135.148) and Worker (98.92.107.90)
- IAM permissions applied to both instance roles
- Services restarted to pick up new credentials

## [0.19.0] - 2024-02-15

### Added - Phase 2: Enterprise Security

**Authentication:**
- Dual authentication: JWT (email/password) + AWS IAM
- IAM authentication via Next.js server-side API routes
- JWT token generation with configurable expiration
- Password hashing with bcrypt (cost factor 12)

**Security Controls:**
- Rate limiting (login: 5 req/min, cluster creation: 10 req/min, global: 100 req/min)
- Audit logging for user management and cluster operations
- Request ID propagation for full observability
- S3 presigned URLs for secure kubeconfig downloads (15-minute expiration)
- Security headers: HSTS, CSP, X-Frame-Options, X-Content-Type-Options

**Work Hours Hibernation:**
- Automatic cluster hibernation outside business hours (AWS only)
- Configurable work hours and days per cluster
- Janitor-based hibernation enforcement
- Wake/hibernate API endpoints

**Orphaned Resource Management:**
- Admin UI for viewing orphaned AWS resources
- Detection of VPCs, load balancers, DNS records, EC2 instances, hosted zones
- Database tracking with status (active, resolved)
- Future: automated cleanup workflows

### Changed
- Worker health checks split into liveness (`/health`) and readiness (`/ready`)
- Database connectivity verified by readiness probe
- Error messages sanitized in API responses (detailed logs server-side)

### Security
- All critical and high-priority items from Issue #2 addressed
- Production deployment guides with security checklists
- Security configuration documentation

## [0.18.0] - 2024-01-30

### Added - Phase 1: Core Platform

**Cluster Management:**
- PostgreSQL database with pgx driver
- Complete database schema with migrations
- Cluster provisioning via openshift-install
- State preservation with S3 artifact storage
- TTL-based automatic cleanup (janitor service)

**Profile System:**
- YAML-based cluster profile definitions
- 7 pre-configured profiles (AWS + IBM Cloud)
- Policy enforcement engine
- install-config.yaml renderer

**Web UI:**
- Next.js 14 with App Router
- TypeScript throughout
- Responsive design (desktop and mobile)
- Cluster creation, monitoring, destruction
- Kubeconfig download

**Authentication & Authorization:**
- JWT-based authentication
- User management (CRUD operations)
- Role-based access control (Admin, User, Viewer)
- Ownership model (users see only their clusters)

**API:**
- RESTful API with Echo framework
- OpenAPI/Swagger documentation
- Cluster lifecycle endpoints
- User management endpoints
- Health checks

**Worker Service:**
- Async job processor
- Cluster create/destroy handlers
- Concurrency-safe job execution
- PostgreSQL-backed job queue

### AWS Profiles
- `aws-sno-test`: Single Node OpenShift (1 control-plane, 0 workers, max 24h TTL)
- `aws-minimal-test`: Minimal test (3 control-plane schedulable, 0 workers, max 72h TTL)
- `aws-standard`: Standard development (3 control-plane, 3 workers, max 168h TTL)
- `aws-virtualization`: OpenShift Virtualization (3+3 m6i.metal, max 168h TTL)

### IBM Cloud Profiles
- `ibmcloud-minimal-test`: Minimal test (3 control-plane schedulable, 0 workers)
- `ibmcloud-standard`: Standard development (3 control-plane, 3 workers)

### Infrastructure
- PostgreSQL 15 database
- S3 bucket for artifacts
- Systemd service files
- Nginx reverse proxy configuration
- Let's Encrypt SSL/TLS

## [0.1.0] - 2024-01-15

### Added - Initial Release

**Foundation:**
- Project scaffolding
- Go module structure
- Database schema design
- Basic API server skeleton
- Worker service skeleton

**Development:**
- Local development setup
- Docker Compose for PostgreSQL
- Build scripts
- Basic testing

---

## Version Naming

Format: `v0.YYYYMMDD.commit`

Example: `v0.20260317.bca1feb`
- `0` = Major version (pre-1.0)
- `20260317` = Date (March 17, 2026 in YYYYMMDD format)
- `bca1feb` = Git commit short hash

## Release Process

1. **Development**
   - Feature branches merged to `main`
   - All tests passing
   - Documentation updated

2. **Versioning**
   - Git tag created: `git tag v0.YYYYMMDD.commit`
   - Changelog updated

3. **Build**
   - Run `./scripts/deploy.sh`
   - Generates versioned binaries
   - Uploads to S3

4. **Deployment**
   - Deploy to API server
   - Deploy to worker instance
   - Verify health checks
   - Monitor logs

5. **Announcement**
   - Update README.md
   - Notify team
   - Update documentation

## Support

### Current Support

| Version | Status | Support Until |
|---------|--------|---------------|
| 0.20.x | ✅ Active | Ongoing |
| 0.19.x | ⚠️ Deprecated | 2024-04-17 (30 days) |
| 0.18.x | ❌ End of Life | 2024-03-17 |

### Upgrade Paths

- **0.19.x → 0.20.x**: Safe upgrade, automatic database migrations
- **0.18.x → 0.20.x**: Safe upgrade via 0.19.x recommended
- **Downgrade**: ❌ Not supported

**Always backup database before upgrading.**

## Breaking Changes

### 0.20.0
- **Worker API**: `TagIAMResources()` renamed to `TagAWSResources()`
  - **Impact**: Low (internal API only)
  - **Migration**: Automatic

### 0.19.0
- **Authentication**: IAM auth moved to Next.js API routes
  - **Impact**: Medium (client integration)
  - **Migration**: Update API client code

### 0.18.0
- **Database**: New schema with migrations
  - **Impact**: High (new deployment)
  - **Migration**: Automatic on first start

---

## Contributing

Changes should be documented in this changelog using the following categories:

- **Added** - New features
- **Changed** - Changes to existing functionality
- **Deprecated** - Soon-to-be removed features
- **Removed** - Removed features
- **Fixed** - Bug fixes
- **Security** - Security fixes and improvements
- **Performance** - Performance improvements
- **Deployment** - Deployment-related changes

---

**Last Updated:** March 17, 2024
