# OCPCTL Feature Matrix

Comprehensive overview of features, platform support, and version compatibility.

**Last Updated:** March 17, 2024
**Version:** ocpctl v0.20260317.bca1feb

## Platform Support

| Feature | AWS | IBM Cloud | Notes |
|---------|-----|-----------|-------|
| **Cluster Provisioning** | ✅ | 🚧 | IBM Cloud profiles defined, execution pending |
| **Cluster Destruction** | ✅ | 🚧 | |
| **Auto-cleanup (TTL)** | ✅ | 🚧 | |
| **State Preservation** | ✅ | 🚧 | S3-backed artifact storage |
| **Comprehensive Resource Tagging** | ✅ | ❌ | EC2, ELB, Route53, S3, IAM |
| **Orphan Detection** | ✅ | ❌ | Tag-based + pattern matching |
| **Work Hours Hibernation** | ✅ | ❌ | Automatic cluster hibernation |
| **Cost Attribution** | ✅ | ❌ | Tag-based tracking |
| **Multi-region Support** | ✅ | 🚧 | AWS: any region; IBM: pending |

**Legend:**
- ✅ Fully supported
- 🚧 Planned/In progress
- ❌ Not supported

## OpenShift Version Support

| Version | Status | Notes |
|---------|--------|-------|
| **4.20.x** | ✅ Fully Supported | Latest stable, recommended |
| **4.19.x** | ✅ Fully Supported | |
| **4.18.x** | ✅ Fully Supported | |
| **4.17.x** | ⚠️ Limited Support | No dedicated binary, may work with 4.18 |
| **4.16.x and earlier** | ❌ Not Supported | Breaking changes in installer |

### Version-Specific Features

All versions support:
- Standard cluster profiles
- TTL-based cleanup
- State preservation
- Comprehensive tagging (4.18+)

## Resource Tagging by Service

| AWS Service | Auto-Tagged | Orphan Detection | Cleanup | Notes |
|-------------|-------------|------------------|---------|-------|
| **EC2** | | | | |
| VPCs | ✅ | ✅ Tag + Pattern | ✅ | |
| Subnets | ✅ | ❌ | ✅ | Deleted with VPC |
| Instances | ✅ | ✅ Tag + Pattern | ✅ | |
| Volumes | ✅ | ❌ | ✅ | Deleted with instance |
| Security Groups | ✅ | ❌ | ✅ | Deleted with VPC |
| Elastic IPs | ✅ | ❌ | ✅ | |
| **Elastic Load Balancing** | | | | |
| Network LB | ✅ | ✅ Tag | ✅ | API server |
| Application LB | ✅ | ✅ Tag | ✅ | Ingress controller |
| **Route53** | | | | |
| Private Hosted Zones | ✅ | ✅ Tag | ✅ | |
| DNS Records | ❌ | ✅ Pattern | ✅ | Tagged via zone |
| **S3** | | | | |
| Bootstrap Bucket | ✅ | ❌ | ✅ | Temporary, auto-deleted |
| OIDC Bucket | ✅ | ❌ | ✅ | |
| **IAM** | | | | |
| Service Roles | ✅ | ✅ Tag + Pattern | ✅ | Created by ccoctl |
| OIDC Provider | ✅ | ✅ Tag | ✅ | |

**Legend:**
- Tag = Tag-based detection (primary)
- Pattern = Pattern matching (fallback)
- ✅ = Supported
- ❌ = Not supported

## Authentication Methods

| Method | Status | Use Case | Notes |
|--------|--------|----------|-------|
| **JWT (Email/Password)** | ✅ | General users | Recommended for most users |
| **AWS IAM** | ✅ | AWS environments | Server-side Next.js API routes |
| **SSO/SAML** | ❌ | Enterprise | Planned for Phase 5 |
| **LDAP/AD** | ❌ | Enterprise | Planned for Phase 5 |

## Authorization (RBAC)

| Role | Create Cluster | View Own Clusters | View All Clusters | Destroy Own | Destroy All | Admin Functions |
|------|----------------|-------------------|-------------------|-------------|-------------|-----------------|
| **Admin** | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| **User** | ✅ | ✅ | ❌ | ✅ | ❌ | ❌ |
| **Viewer** | ❌ | ✅ | ❌ | ❌ | ❌ | ❌ |

**Admin Functions:**
- User management
- View orphaned resources
- Configure policies
- View audit logs

## Cluster Profiles

### AWS Profiles

| Profile | Control Nodes | Worker Nodes | Max TTL | Default TTL | Cost/Hour | Status |
|---------|--------------|--------------|---------|-------------|-----------|--------|
| **aws-sno-test** | 1 (schedulable) | 0 | 24h | 8h | ~$0.80 | ✅ |
| **aws-minimal-test** | 3 (schedulable) | 0 | 72h | 24h | ~$2.50 | ✅ |
| **aws-standard** | 3 | 3 (m6i.2xlarge) | 168h | 72h | ~$8.00 | ✅ |
| **aws-virtualization** | 3 (m6i.2xlarge) | 3 (m6i.metal) | 168h | 72h | ~$35.50 | ✅ |
| **aws-sno-shared-vpc** | 1 | 0 | - | - | - | ⚠️ Template |
| **aws-sno-shared-vpc-custom** | 1 | 0 | 168h | 72h | ~$0.80 | ✅ |

### IBM Cloud Profiles

| Profile | Control Nodes | Worker Nodes | Max TTL | Default TTL | Status |
|---------|--------------|--------------|---------|-------------|--------|
| **ibmcloud-minimal-test** | 3 (schedulable) | 0 | 72h | 24h | 🚧 |
| **ibmcloud-standard** | 3 | 3 | 168h | 72h | 🚧 |

**Legend:**
- ✅ Active and ready
- ⚠️ Template (copy and customize)
- 🚧 Planned

## Security Features

| Feature | Status | Details |
|---------|--------|---------|
| **Authentication** | | |
| JWT Token Auth | ✅ | bcrypt password hashing |
| AWS IAM Auth | ✅ | Server-side verification |
| Token Expiration | ✅ | Configurable (default: 24h) |
| Refresh Tokens | ❌ | Planned |
| **Authorization** | | |
| Role-Based Access (RBAC) | ✅ | Admin, User, Viewer |
| Ownership Model | ✅ | Users see only their clusters |
| Resource Policies | ✅ | Profile-based restrictions |
| **Security Controls** | | |
| Rate Limiting | ✅ | Login, cluster ops, global |
| Audit Logging | ✅ | User mgmt, cluster ops, downloads |
| Request Tracing | ✅ | Unique request IDs |
| Security Headers | ✅ | HSTS, CSP, X-Frame-Options |
| **Data Security** | | |
| Password Hashing | ✅ | bcrypt cost factor 12 |
| Database SSL | ✅ | Required in production |
| Secret Management | ✅ | Environment variables |
| S3 Presigned URLs | ✅ | 15-minute expiration |
| **Network Security** | | |
| CORS Protection | ✅ | Origin whitelisting |
| TLS/HTTPS | ✅ | Let's Encrypt + nginx |
| VPC Isolation | ✅ | Cluster networks isolated |

## API Features

| Endpoint Category | Status | Notes |
|------------------|--------|-------|
| **Authentication** | | |
| POST /auth/login | ✅ | JWT + IAM |
| POST /auth/register | ✅ | Self-service registration |
| GET /auth/me | ✅ | Current user info |
| **Clusters** | | |
| POST /clusters | ✅ | Create cluster |
| GET /clusters | ✅ | List with filtering |
| GET /clusters/:id | ✅ | Cluster details |
| DELETE /clusters/:id | ✅ | Destroy cluster |
| GET /clusters/:id/kubeconfig | ✅ | Download kubeconfig |
| POST /clusters/:id/hibernate | ✅ | Hibernate (AWS only) |
| POST /clusters/:id/wake | ✅ | Wake from hibernation |
| **Admin** | | |
| GET /admin/orphaned-resources | ✅ | List orphans |
| GET /admin/users | ✅ | User management |
| POST /admin/users | ✅ | Create user |
| PUT /admin/users/:id | ✅ | Update user |
| DELETE /admin/users/:id | ✅ | Delete user |
| **Health & Monitoring** | | |
| GET /health | ✅ | API health check |
| GET /ready | ✅ | Readiness probe |
| GET /metrics | ❌ | Planned (Prometheus) |

## Observability

| Feature | Status | Notes |
|---------|--------|-------|
| **Logging** | | |
| Structured JSON Logs | ✅ | |
| Request ID Correlation | ✅ | |
| Log Levels | ✅ | INFO, WARN, ERROR |
| Centralized Logging | ❌ | Planned (CloudWatch) |
| **Health Checks** | | |
| API Liveness | ✅ | GET /health |
| Worker Liveness | ✅ | GET /health (port 8081) |
| Worker Readiness | ✅ | GET /ready (DB check) |
| **Metrics** | | |
| Prometheus Metrics | ❌ | Planned for Phase 3 |
| Custom Metrics | ❌ | Planned |
| **Tracing** | | |
| Request Tracing | ✅ | Request ID propagation |
| Distributed Tracing | ❌ | Planned (OpenTelemetry) |
| **Monitoring** | | |
| CloudWatch Integration | ❌ | Planned for Phase 3 |
| Grafana Dashboards | ❌ | Planned |
| Alerting | ❌ | Planned |

## Web UI Features

| Feature | Status | Notes |
|---------|--------|-------|
| **Authentication** | | |
| Login (JWT) | ✅ | |
| Login (IAM) | ✅ | Server-side Next.js API |
| Logout | ✅ | |
| User Registration | ✅ | Self-service |
| **Cluster Management** | | |
| Create Cluster | ✅ | Profile selection, tags |
| List Clusters | ✅ | User's clusters |
| Cluster Details | ✅ | Status, outputs, logs |
| Download Kubeconfig | ✅ | S3 presigned URL |
| Destroy Cluster | ✅ | Confirmation modal |
| Hibernate/Wake | ✅ | AWS only |
| **Admin Features** | | |
| User Management | ✅ | CRUD operations |
| Orphaned Resources | ✅ | View and track |
| Audit Logs | ✅ | Filter and search |
| Policy Configuration | ❌ | Planned |
| **UI Features** | | |
| Responsive Design | ✅ | Mobile-friendly |
| Dark Mode | ❌ | Planned |
| Real-time Updates | ❌ | Planned (WebSocket) |
| Notifications | ✅ | Toast messages |

## Operational Tools

| Tool | Purpose | Status | Notes |
|------|---------|--------|-------|
| **tag-aws-resources** | Retroactive tagging | ✅ | NEW! Phase 2 |
| **cleanup-orphaned-iam** | IAM cleanup | ✅ | |
| **delete-iam-roles** | Bulk IAM deletion | ✅ | |
| **janitor** | TTL cleanup | ✅ | Embedded in worker |
| **api** | REST API server | ✅ | |
| **worker** | Async job processor | ✅ | |

## Deployment Targets

| Target | Status | Documentation | Notes |
|--------|--------|---------------|-------|
| **AWS EC2** | ✅ | [AWS Quick Start](deployment/AWS_QUICKSTART.md) | Recommended |
| **AWS ECS** | ❌ | | Planned |
| **Kubernetes** | ❌ | | Planned |
| **Local/Dev** | ✅ | [DEVELOPMENT.md](../DEVELOPMENT.md) | |
| **IBM Cloud VMs** | 🚧 | [BRIX Setup](setup/BRIX_SETUP.md) | Partial |

## Database Support

| Database | Status | Notes |
|----------|--------|-------|
| **PostgreSQL 15+** | ✅ | Recommended |
| **PostgreSQL 14** | ✅ | Supported |
| **PostgreSQL 13** | ⚠️ | May work, untested |
| **MySQL** | ❌ | Not supported |
| **SQLite** | ❌ | Not supported |

## CI/CD Integration

| Feature | Status | Notes |
|---------|--------|-------|
| **Automated Testing** | ✅ | Go tests, build verification |
| **Build Automation** | ✅ | ./scripts/deploy.sh |
| **Version Tagging** | ✅ | Git-based versioning |
| **GitHub Actions** | ❌ | Planned |
| **GitLab CI** | ❌ | Planned |

## Roadmap Priority

### High Priority (Phase 3)
- [ ] CloudWatch/Prometheus metrics
- [ ] Enhanced monitoring dashboards
- [ ] Automated backups
- [ ] Performance optimization

### Medium Priority (Phase 4)
- [ ] IBM Cloud execution
- [ ] Multi-region support
- [ ] Cost reporting and analytics
- [ ] Off-hours worker scaling
- [ ] Admin policy configuration UI

### Low Priority (Phase 5)
- [ ] SSO/SAML authentication
- [ ] Cluster templates
- [ ] Snapshot and restore
- [ ] Cluster upgrade workflows
- [ ] ITSM integration

## Known Limitations

### Current Limitations

1. **Single Region Worker**: Worker processes clusters in one region (configurable)
2. **Manual Scaling**: Worker scaling is manual, not auto-scaling
3. **No Multi-tenancy**: Single deployment per organization
4. **IBM Cloud Execution**: Profiles ready, execution not implemented
5. **Limited Observability**: No Prometheus metrics yet
6. **No Backup/Restore**: Database backups are manual

### Workarounds

1. **Multi-region**: Deploy separate ocpctl instances per region
2. **Auto-scaling**: Use off-hours scaling (Issue #11)
3. **Multi-tenancy**: Use team/owner tags for soft multi-tenancy
4. **IBM Cloud**: Use openshift-install directly
5. **Metrics**: Use health endpoints + log aggregation
6. **Backups**: Use RDS automated backups

## Compatibility Matrix

### OpenShift Installer Compatibility

| ocpctl Version | Installer 4.18 | Installer 4.19 | Installer 4.20 |
|----------------|---------------|----------------|----------------|
| v0.1.x - v0.19.x | ✅ | ❌ | ❌ |
| v0.20.x+ | ✅ | ✅ | ✅ |

### Database Migration Compatibility

All versions support forward migration (automatic).
Downgrade support: ❌ Not supported.

**Always backup database before upgrading.**

### API Versioning

Current API: **v1**

Breaking changes will increment major version (v2, v3, etc.)
Current API guarantees backward compatibility within v1.

## Support Matrix

| Component | Support Level | SLA | Contact |
|-----------|---------------|-----|---------|
| **Core Platform** | ✅ Production | 99.9% | ocpctl-team@example.com |
| **AWS Integration** | ✅ Production | 99.9% | |
| **IBM Cloud** | ⚠️ Beta | Best effort | |
| **Web UI** | ✅ Production | 99.9% | |
| **Documentation** | ✅ Maintained | N/A | |

---

## Version History

### v0.20.x (Current - March 2024)
- ✅ Comprehensive AWS resource tagging (Phase 2)
- ✅ Hybrid orphan detection (tag + pattern)
- ✅ Retroactive tagging tool
- ✅ Multi-version OpenShift support (4.18, 4.19, 4.20)
- ✅ Work hours hibernation
- ✅ Orphaned resource management

### v0.19.x (February 2024)
- ✅ Web UI (Next.js 14)
- ✅ Dual authentication (JWT + IAM)
- ✅ Enterprise security features
- ✅ Audit logging

### v0.18.x (January 2024)
- ✅ Core platform
- ✅ Worker service
- ✅ Profile system
- ✅ TTL janitor

---

**For detailed feature documentation, see:**
- [User Guides](user-guide/)
- [Operations Guides](operations/)
- [Deployment Guides](deployment/)
