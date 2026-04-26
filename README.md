# ocpctl

**Self-service Kubernetes cluster provisioning and lifecycle management for OpenShift, EKS, and IKS.**

ocpctl is a production-ready platform that provides a standardized workflow for requesting, managing, and terminating ephemeral Kubernetes clusters on AWS and IBM Cloud with automated cost controls, comprehensive security, and enterprise-grade operations.

---

## Why ocpctl?

**The Problem:**
- Manual cluster provisioning is time-consuming and error-prone
- Forgotten test clusters waste thousands in cloud costs
- No standardized way to track cluster ownership and costs
- Cleanup is unreliable - orphaned resources accumulate

**The Solution:**
- **Automated lifecycle management** - Create, monitor, hibernate, and destroy clusters via web UI or API
- **Automatic cost controls** - TTL-based destruction, work hours hibernation, and comprehensive resource tagging
- **Reliable cleanup** - Preserves installer state for deterministic teardown and detects orphaned resources
- **Enterprise security** - Dual authentication (JWT/IAM), rate limiting, audit logging, and RBAC
- **Multi-platform** - OpenShift, AWS EKS, and IBM Cloud IKS with standardized profiles

---

## Key Features

### Platform Support
- ✅ **OpenShift 4.x** (AWS, IBM Cloud) - Full lifecycle including hibernation
- ✅ **AWS EKS** - Elastic Kubernetes Service with VPC management
- ✅ **IBM Cloud IKS** - IBM Kubernetes Service
- ✅ **Multi-version support** - OpenShift 4.17 - 4.22+

### Cost Management
- **Automatic TTL-based destruction** - Clusters self-destruct after configured lifetime
- **Work hours hibernation** - Stop instances outside business hours (OpenShift on AWS)
- **Comprehensive resource tagging** - Track costs by owner, team, cost center, and cluster
- **Orphaned resource detection** - Identify and clean up resources from failed deployments

### Security & Compliance
- **Dual authentication** - JWT (email/password) or AWS IAM credentials
- **Role-based access control** - Admin, User, and Viewer roles
- **Rate limiting** - Protect against brute force and abuse
- **Audit logging** - Immutable trail of all security-relevant operations
- **Request tracing** - Unique request IDs for full observability

### Operations
- **Modern web UI** - Next.js 14 with TypeScript and server-side rendering
- **RESTful API** - OpenAPI/Swagger documentation with interactive playground
- **Health monitoring** - Liveness and readiness endpoints for all services
- **Structured logging** - JSON logs with request context and correlation IDs

---

## Quick Start

### 🚀 Deploy to AWS

Complete AWS deployment in 45-60 minutes:

```bash
# See comprehensive deployment guide
docs/deployment/AWS_QUICKSTART.md

# Quick summary:
# 1. Create RDS PostgreSQL database
# 2. Launch EC2 instance (t3.medium recommended)
# 3. Deploy binaries (API, Worker, Web UI)
# 4. Configure nginx reverse proxy with HTTPS
# 5. Access web UI and create your first cluster
```

**Estimated cost:** ~$50/month for test instance

### 🌐 Access Web UI

After deployment, navigate to your instance URL and login:

**Default credentials:**
- Email: `admin@localhost`
- Password: `changeme` ⚠️ Change immediately!

**Or use AWS IAM:**
- Provide AWS Access Key ID and Secret Access Key
- Uses AWS STS for verification

### 🔧 Local Development

```bash
# Start PostgreSQL
docker run -d --name ocpctl-postgres \
  -e POSTGRES_PASSWORD=postgres \
  -e POSTGRES_DB=ocpctl \
  -p 5432:5432 postgres:15

# Set required environment variables
export DATABASE_URL="postgres://postgres:postgres@localhost:5432/ocpctl?sslmode=disable"
export JWT_SECRET="development-secret-min-32-chars"
export OPENSHIFT_PULL_SECRET='{"auths":{...}}'

# Build and run services
go build -o bin/api ./cmd/api && ./bin/api &
go build -o bin/worker ./cmd/worker && ./bin/worker &

# Start web UI
cd web && npm install && npm run dev
```

Access at http://localhost:3000

---

## Architecture

```
┌─────────────┐     ┌─────────────┐     ┌──────────────┐
│   Web UI    │────▶│  API Server │────▶│  PostgreSQL  │
│  (Next.js)  │     │   (Echo)    │     │   (State)    │
└─────────────┘     └─────────────┘     └──────────────┘
                           │                     │
                           ▼                     │
                    ┌─────────────┐              │
                    │   Worker    │◀─────────────┘
                    │  Service    │
                    └──────┬──────┘
                           │
            ┌──────────────┼──────────────┐
            ▼              ▼              ▼
     ┌───────────┐  ┌────────────┐  ┌──────────┐
     │    S3     │  │ openshift- │  │ Janitor  │
     │(Artifacts)│  │  install   │  │ (Cleanup)│
     └───────────┘  └────────────┘  └──────────┘
```

**Services:**
- **API Server** (Port 8080) - REST API with authentication and rate limiting
- **Worker** (Port 8081) - Asynchronous cluster provisioning and lifecycle operations
- **Web UI** (Port 3000) - Modern React frontend with server-side rendering
- **Janitor** - TTL enforcement and orphaned resource cleanup (embedded in worker)

---

## Documentation

### 📋 Getting Started
- **[AWS Quick Start](docs/deployment/AWS_QUICKSTART.md)** - Deploy to AWS in under an hour
- **[User Guide](docs/user-guide/getting-started.md)** - New user onboarding and first cluster
- **[Cluster Management](docs/user-guide/cluster-management.md)** - Cluster lifecycle operations
- **[Feature Matrix](docs/reference/FEATURE_MATRIX.md)** - Platform support and version compatibility

### 🔒 Security & Operations
- **[Security Configuration](docs/deployment/SECURITY_CONFIGURATION.md)** - Authentication, authorization, and security controls
- **[IAM Authentication](docs/deployment/IAM_AUTHENTICATION.md)** - AWS IAM integration details
- **[Disk Space Management](docs/operations/DISK_SPACE_MANAGEMENT.md)** - Automated cleanup and monitoring
- **[AWS IAM Permissions](docs/operations/AWS_IAM_PERMISSIONS.md)** - Required AWS permissions

### 🏗️ Architecture & Design
- **[Architecture Overview](docs/architecture/architecture.md)** - System design and components
- **[Design Specification](docs/architecture/design-specification.md)** - Complete design specification
- **[Worker Concurrency](docs/architecture/worker-concurrency-safety.md)** - Concurrency model and safety

### 🔧 Setup & Configuration
- **[OpenShift Install Setup](docs/setup/OPENSHIFT_INSTALL_SETUP.md)** - Get openshift-install binary
- **[Multi-version Setup](docs/setup/MULTIVERSION_SETUP.md)** - Support multiple OpenShift versions
- **[IBM Cloud Setup](docs/setup/BRIX_SETUP.md)** - IBM Cloud BRIX configuration

### 📚 Additional Resources
- **[API Documentation](docs/deployment/API_SUBDOMAIN_SETUP.md)** - API subdomain and Swagger UI
- **[Deployment Guides](docs/deployment/)** - Production deployment and configuration
- **[Operations Guides](docs/operations/)** - Day-2 operations and troubleshooting

---

## API Reference

Interactive API documentation is available via Swagger UI:

**Production:** https://api.ocpctl.mg.dog8code.com/swagger/index.html
**Local:** http://localhost:8080/swagger/index.html

### Quick API Example

```bash
# Login and get token
TOKEN=$(curl -X POST https://api.ocpctl.mg.dog8code.com/v1/auth/login \
  -H "Content-Type: application/json" \
  -d '{"email":"user@example.com","password":"yourpassword"}' \
  | jq -r '.access_token')

# List clusters
curl -X GET https://api.ocpctl.mg.dog8code.com/v1/clusters \
  -H "Authorization: Bearer $TOKEN"

# Create cluster
curl -X POST https://api.ocpctl.mg.dog8code.com/v1/clusters \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "my-test-cluster",
    "platform": "aws",
    "cluster_type": "openshift",
    "version": "4.22.0",
    "profile": "aws-sno-test",
    "region": "us-west-2",
    "owner": "user@example.com",
    "team": "engineering",
    "cost_center": "dev-ops"
  }'
```

See [API Documentation](docs/deployment/API_SUBDOMAIN_SETUP.md) for complete endpoint reference.

---

## Project Status

**Production Ready** - All critical and high-priority issues resolved

### Latest Release

**Version:** v0.20260413.1346b69 (April 13, 2026)

**Recent Updates:**
- ✅ Security hardening - All critical/high/medium severity issues addressed
- ✅ EKS destroy reconciler - Fixed infinite loop with AWS-managed ENIs
- ✅ Password change feature - User-facing password management
- ✅ API subdomain deployment - Dedicated subdomain with clean URLs
- ✅ OpenShift 4.21+ support - Multi-version installer support

See [CHANGELOG.md](CHANGELOG.md) for complete release history.

### Roadmap

**Completed:**
- ✅ Phase 0: Foundation (scaffolding, schema, profiles)
- ✅ Phase 1: Core Platform (API, worker, web UI, authentication)
- ✅ Phase 2: Enterprise Security (rate limiting, audit logging, IAM, hibernation)
- ✅ Phase 3: Production Operations (monitoring, deployment guides, API subdomain)

**In Progress:**
- 🚧 Phase 4: Platform Expansion (multi-region, cost analytics, bulk operations)

**Future:**
- 💡 Phase 5: Advanced Features (snapshots, upgrades, ML-based predictions)

---

## Contributing

This is an internal tool. For development setup and contribution guidelines, see:
- [Local Development](#-local-development) above
- [Architecture Documentation](docs/architecture/)
- [Implementation Guides](docs/phases/)

---

## License

Internal use only - All rights reserved.

---

**Built with ❤️ for ephemeral Kubernetes cluster management**
