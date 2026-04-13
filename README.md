# ocpctl - Kubernetes Cluster Control Plane

Self-service provisioning and lifecycle management for ephemeral Kubernetes clusters on AWS and IBM Cloud.

## Overview

`ocpctl` is a production-ready service that provides a standardized workflow for requesting, managing, and terminating ephemeral Kubernetes clusters (OpenShift, EKS, IKS) with:

- **Reliable cleanup**: Preserves installer state artifacts for deterministic teardown
- **Cost control**: Automatic TTL-based destruction and resource tagging
- **Self-service**: Modern web UI for cluster lifecycle operations
- **Audit trail**: Complete tracking of ownership, operations, and compliance
- **Enterprise security**: Rate limiting, JWT/IAM auth, audit logging, and request tracing

## Status

🎯 **Production Ready** - Multi-platform Kubernetes cluster management with comprehensive security controls

- ✅ Full cluster lifecycle (create, monitor, destroy, hibernate, resume)
- ✅ **OpenShift** - Full support for OpenShift 4.x clusters on AWS and IBM Cloud
- ✅ **AWS EKS** - Production-ready support for Elastic Kubernetes Service
- ✅ **IBM Cloud IKS** - IBM Kubernetes Service support
- ✅ Web UI with Next.js + TypeScript
- ✅ Dual authentication (JWT + AWS IAM)
- ✅ Enterprise security features
- ✅ Orphaned resource detection and automated cleanup
- ✅ Production deployment guides

## Key Features

### Cluster Management
- **Multi-platform support**:
  - **OpenShift** (OCP) on AWS and IBM Cloud
  - **AWS EKS** (Elastic Kubernetes Service)
  - **IBM Cloud IKS** (IBM Kubernetes Service)
- **Standardized profiles**: Pre-configured cluster templates with policy controls
- **State preservation**: S3-backed artifact storage ensures clean destroy operations
- **Auto-cleanup**: TTL janitor and comprehensive orphan resource detection
- **Work hours hibernation**: Automatic cluster hibernation outside business hours (OpenShift on AWS)
- **Comprehensive resource tagging**: Automatic tagging of ALL AWS resources (EC2, ELB, Route53, S3, IAM) with cluster metadata
- **Orphaned resource management**:
  - Automated detection of orphaned AWS resources (VPCs, Load Balancers, EBS volumes, Elastic IPs, Security Groups, etc.)
  - Admin UI for reviewing and deleting orphaned resources
  - Automated deletion with proper dependency handling (load balancers before subnets, security groups after ENIs, etc.)
- **Cost attribution**: Tag-based tracking and FinOps reporting with ManagedBy:ocpctl tags
- **Retroactive tagging**: Tool to tag existing cluster resources for improved tracking

### Security & Compliance
- **Dual authentication**: JWT (email/password) + AWS IAM support
- **Rate limiting**: Protect against brute force and DoS attacks (5-100 req/min)
- **Audit logging**: Immutable audit trail for all security-relevant operations
- **Request tracing**: Unique request IDs for full observability
- **S3 presigned URLs**: Secure, time-limited kubeconfig downloads (15min expiration)
- **Security headers**: HSTS, CSP, X-Frame-Options, and more

### Operations
- **Health checks**: Liveness and readiness endpoints for all services
- **Structured logging**: JSON logs with correlation IDs and request context
- **Worker monitoring**: Dedicated health endpoints for async job processing
- **Role-based access**: Admin, User, and Viewer roles with ownership controls

## Architecture

```
┌─────────────┐     ┌─────────────┐     ┌──────────────┐
│   Web UI    │────▶│  API Server │────▶│  PostgreSQL  │
│  (Next.js)  │     │   (Echo)    │     │   (State)    │
└─────────────┘     └─────────────┘     └──────────────┘
                           │                     │
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
- **API Server** (Port 8080): REST API with Echo framework
- **Worker** (Port 8081): Async job processor with health checks
- **Web UI** (Port 3000): Next.js frontend with TypeScript
- **Janitor**: TTL-based cleanup and orphan detection (embedded in worker)

## Quick Start

### 🚀 Deploy to AWS (Test Instance)

**NEW!** Complete AWS deployment in 45-60 minutes:

```bash
# See comprehensive guide
docs/deployment/AWS_QUICKSTART.md

# Quick summary:
# 1. Create RDS PostgreSQL database
# 2. Launch EC2 instance (t3.medium)
# 3. Deploy binaries (API, Worker, Web)
# 4. Configure nginx reverse proxy
# 5. Access web UI at http://<EC2-IP>
```

**Estimated cost:** ~$50/month for test instance

### 🌐 Access Web UI

Navigate to your deployment URL and login:

**JWT Mode (Email/Password):**
- Email: `admin@localhost`
- Password: `changeme` (change immediately!)

**IAM Mode (AWS Credentials):**
- Provide AWS Access Key ID and Secret Access Key
- Uses AWS STS for verification
- Supported via Next.js server-side API routes

### 🔧 Local Development

```bash
# Start PostgreSQL
docker run -d \
  --name ocpctl-postgres \
  -e POSTGRES_PASSWORD=postgres \
  -e POSTGRES_DB=ocpctl \
  -p 5432:5432 \
  postgres:15

# Build and run API server
go build -o bin/api ./cmd/api
./bin/api

# Build and run worker
go build -o bin/worker ./cmd/worker
./bin/worker

# Start web frontend
cd web
npm install
npm run dev
```

Access at `http://localhost:3000`

## API Documentation

OCPCTL provides interactive API documentation powered by OpenAPI/Swagger.

### API Endpoints

For **production deployments**, the API is accessible via two methods:

1. **Dedicated API subdomain** (recommended for direct API access):
   ```
   https://api.ocpctl.mg.dog8code.com
   ```

2. **Path-based proxy** (through web UI domain):
   ```
   https://ocpctl.mg.dog8code.com/api
   ```

For **local development**:
```
http://localhost:8080
```

### Swagger UI

Access the interactive API documentation at:
- **Production**: `https://api.ocpctl.mg.dog8code.com/swagger/index.html`
- **Local**: `http://localhost:8080/swagger/index.html`

The Swagger UI provides:
- Complete API reference with all endpoints and parameters
- Request/response schemas with examples
- Interactive "Try it out" functionality to test endpoints directly
- Authentication support (JWT Bearer token)

### Generating Documentation

The API documentation is auto-generated from code annotations using [swaggo/swag](https://github.com/swaggo/swag). To regenerate after making API changes:

```bash
# Install swag CLI (if not already installed)
go install github.com/swaggo/swag/cmd/swag@latest

# Generate docs (from project root)
swag init -g cmd/api/main.go -o docs --parseDependency --parseInternal
```

This generates:
- `docs/swagger.json` - OpenAPI specification (JSON)
- `docs/swagger.yaml` - OpenAPI specification (YAML)
- `docs/docs.go` - Go code to embed documentation

### API Integration

For programmatic access to the OCPCTL API:

1. **Authentication:** Obtain a JWT token via `POST /api/v1/auth/login`
2. **Authorization:** Include token in `Authorization: Bearer <token>` header
3. **Endpoints:** See Swagger UI for complete endpoint reference
4. **OpenAPI Spec:** Download from `/swagger/doc.json` for code generation

Example using curl:

```bash
# For production (using API subdomain)
API_URL="https://api.ocpctl.mg.dog8code.com/v1"

# For local development
# API_URL="http://localhost:8080/api/v1"

# Login and get token
TOKEN=$(curl -X POST ${API_URL}/auth/login \
  -H "Content-Type: application/json" \
  -d '{"email":"admin@localhost","password":"changeme"}' \
  | jq -r '.access_token')

# List clusters
curl -X GET ${API_URL}/clusters \
  -H "Authorization: Bearer $TOKEN"
```

See [API Subdomain Setup Guide](docs/deployment/API_SUBDOMAIN_SETUP.md) for detailed configuration instructions.

## Project Structure

```
ocpctl/
├── cmd/
│   ├── api/                # API server entrypoint
│   └── worker/             # Worker + Janitor entrypoint
├── internal/
│   ├── api/                # HTTP handlers (Echo framework)
│   │   └── middleware/     # Rate limiting, CORS, auth
│   ├── worker/             # Cluster provisioning logic
│   ├── janitor/            # TTL cleanup and orphan detection
│   ├── store/              # Database access layer (PostgreSQL)
│   ├── s3/                 # S3 presigned URL generation
│   ├── auth/               # JWT + IAM authentication
│   ├── policy/             # Policy enforcement engine
│   ├── profile/            # Cluster profile system
│   └── installer/          # openshift-install wrapper
├── pkg/
│   └── types/              # Shared types and models
├── web/                    # Next.js 14 frontend
│   ├── app/                # App router pages
│   │   ├── (dashboard)/    # Authenticated routes
│   │   └── api/            # Next.js API routes (IAM auth)
│   ├── components/         # React components
│   ├── lib/                # Client libraries
│   └── public/             # Static assets
├── deploy/
│   ├── config/             # Environment templates
│   ├── systemd/            # Service files
│   └── nginx/              # Nginx configuration
├── docs/
│   ├── deployment/         # Deployment guides
│   ├── architecture/       # Architecture docs
│   ├── setup/              # Setup instructions
│   └── phases/             # Implementation phases
└── bin/                    # Compiled binaries (gitignored)
```

## Cluster Profiles

### OpenShift Profiles (AWS)

#### aws-sno-test (Default)
- 1 control-plane node (schedulable, Single Node OpenShift)
- 0 worker nodes
- Max TTL: 24 hours, Default: 8 hours
- Cost: ~$0.80/hour
- Use case: Rapid testing and development (fastest deployment, lowest cost)

#### aws-minimal-test
- 3 control-plane nodes (schedulable)
- 0 worker nodes
- Max TTL: 72 hours
- Use case: Quick testing without dedicated worker nodes

#### aws-standard
- 3 control-plane nodes
- 3 worker nodes (m6i.2xlarge)
- Max TTL: 168 hours
- Use case: Standard development and integration testing

#### aws-virtualization
- 3 control-plane nodes (m6i.2xlarge)
- 3 worker nodes (m6i.metal with nested virtualization)
- Max TTL: 168 hours, Default: 72 hours
- Cost: ~$35.50/hour
- Use case: OpenShift Virtualization workloads

#### aws-virt-windows-minimal
- Virtualization-enabled cluster with Windows support
- Use case: Testing Windows containers and VMs

#### aws-sno-shared
- Single Node OpenShift in shared VPC
- Use case: Cost-effective shared infrastructure deployments

#### aws-sno-shared-vpc
- Template for shared VPC deployments
- Enabled: false (template - copy and customize)
- Use case: Template for deploying SNO clusters in persistent shared VPCs

#### aws-sno-shared-vpc-custom
- Custom shared VPC configuration
- Pre-configured subnets for specific VPC
- Use case: Production shared VPC deployments

### EKS Profiles (AWS)

#### eks-minimal
- EKS managed Kubernetes cluster
- Minimal configuration for testing
- Use case: AWS-native Kubernetes for testing

#### eks-standard
- EKS with managed node groups
- Standard configuration
- Use case: Production-like EKS testing

### OpenShift Profiles (IBM Cloud)

#### ibmcloud-minimal-test
- 3 control-plane nodes (schedulable)
- 0 worker nodes
- Max TTL: 72 hours
- Use case: IBM Cloud testing

#### ibmcloud-standard
- 3 control-plane nodes
- 3 worker nodes
- Max TTL: 168 hours
- Use case: IBM Cloud development

### IKS Profiles (IBM Cloud)

#### iks-minimal
- IBM Kubernetes Service cluster
- Minimal configuration
- Use case: IKS testing

#### iks-standard
- IKS with worker nodes
- Standard configuration
- Use case: IKS development and testing

## Development

### Prerequisites

- **Go 1.22+** - Backend services
- **Node.js 18+** - Web frontend
- **PostgreSQL 15+** - Database
- **AWS CLI** - Cloud operations
- **OpenShift pull secret** - From console.redhat.com
- **openshift-install binary** - v4.20.3 recommended

### Local Setup

```bash
# Clone repository
git clone https://github.com/tsanders-rh/ocpctl.git
cd ocpctl

# Start PostgreSQL
docker run -d \
  --name ocpctl-postgres \
  -e POSTGRES_PASSWORD=postgres \
  -e POSTGRES_DB=ocpctl \
  -p 5432:5432 \
  postgres:15

# Set environment variables
export DATABASE_URL="postgres://postgres:postgres@localhost:5432/ocpctl?sslmode=disable"
export JWT_SECRET="development-secret-change-in-production"
export OPENSHIFT_PULL_SECRET='{"auths":{...}}'

# Build API server
go build -o bin/api ./cmd/api

# Build worker
go build -o bin/worker ./cmd/worker

# Run migrations (automatic on first API start)
./bin/api  # Ctrl+C after migrations complete

# Start API server (terminal 1)
./bin/api

# Start worker (terminal 2)
./bin/worker

# Start web frontend (terminal 3)
cd web
npm install
npm run dev

# Access at http://localhost:3000
# Login: admin@localhost / changeme
```

### Testing

```bash
# Run Go tests
go test ./... -short

# Test builds
go build -o bin/api ./cmd/api
go build -o bin/worker ./cmd/worker

# Build web frontend
cd web && npm run build

# All tests and builds should pass before deployment
```

See [docs/setup/](docs/setup/) for detailed development setup instructions.

## Deployment

### Production Deployment

**📚 Comprehensive Guides:**
- **[AWS Quick Start](docs/deployment/AWS_QUICKSTART.md)** - Deploy to AWS in 45-60 minutes (~$50/month)
- **[Security Configuration](docs/deployment/SECURITY_CONFIGURATION.md)** - All security features and settings
- **[Web Deployment](docs/deployment/DEPLOYMENT_WEB.md)** - Detailed deployment instructions

**Quick Deploy Summary:**
1. Create RDS PostgreSQL database
2. Launch EC2 instance (t3.medium)
3. Deploy binaries (API, Worker, Web)
4. Configure environment variables
5. Set up systemd services
6. Configure nginx reverse proxy
7. Enable HTTPS with Let's Encrypt

**Pre-Deployment Checklist:**
- [ ] Generate strong JWT_SECRET (min 32 chars)
- [ ] Configure DATABASE_URL with SSL
- [ ] Set CORS_ALLOWED_ORIGINS
- [ ] Set ENVIRONMENT=production
- [ ] Configure OPENSHIFT_PULL_SECRET
- [ ] Change default admin password
- [ ] Review [Security Configuration Guide](docs/deployment/SECURITY_CONFIGURATION.md)

### Local/Development Deployment

See [Development](#development) section above.

## Security

### Authentication & Authorization
- **Dual Auth**: JWT (email/password) OR AWS IAM credentials
- **IAM Implementation**: Server-side Next.js API routes with AWS SDK
- **RBAC**: Admin, User, and Viewer roles
- **Ownership**: Users can only access their own clusters (unless admin)
- **Password Security**: bcrypt hashing with cost factor 12

### Security Features
- **Rate Limiting**: Prevent brute force and DoS attacks
  - Login: 5 requests/minute
  - Cluster creation: 10 requests/minute
  - Global: 100 requests/minute
- **Audit Logging**: Immutable audit trail in database
  - User management (create, update, delete)
  - Cluster operations (create, delete)
  - Kubeconfig downloads
- **Request Tracing**: Unique request IDs for full observability
- **Secure Downloads**: S3 presigned URLs (15-minute expiration)
- **Security Headers**: HSTS, CSP, X-Frame-Options, X-Content-Type-Options

### Infrastructure Security
- **Database**: SSL required (sslmode=require in production)
- **Secrets**: Environment variables, never in code
- **CORS**: Strict origin whitelisting
- **TLS**: HTTPS enforced via nginx + Let's Encrypt
- **Error Handling**: Generic messages to clients, detailed logs server-side

**Security Review:** All critical and high-priority items from [Issue #2](https://github.com/tsanders-rh/ocpctl/issues/2) addressed.

## Observability

### Health Checks
- **API**: `GET /health` (port 8080)
- **Worker**: `GET /health` (port 8081) - Liveness probe
- **Worker**: `GET /ready` (port 8081) - Readiness probe (checks DB connectivity)

### Logging
- **Structured Logs**: JSON format with request context
- **Request IDs**: Unique ID per request for tracing
- **Log Levels**: INFO, WARN, ERROR with context
- **Format**: `[LEVEL] request_id=xxx method=POST path=/api/v1/clusters message="..." key=value`

### Monitoring
- **Health endpoints** for load balancer checks
- **Audit events** in database for security monitoring
- **Service logs** via systemd/journald
- **Request tracing** with correlation IDs

**Future:** Prometheus metrics, OpenTelemetry tracing, Grafana dashboards

## Roadmap

### Phase 0 (Foundation) ✅ COMPLETE
- [x] Project scaffolding
- [x] Complete database schema with migrations
- [x] Cluster profile definitions (4 profiles: AWS + IBM Cloud)
- [x] Worker concurrency safety design
- [x] RBAC mapping model
- [x] Idempotency mechanism

### Phase 1 (Core Platform) ✅ COMPLETE
- [x] PostgreSQL connection pooling and store package
- [x] Profile YAML loader and validation engine
- [x] install-config.yaml renderer
- [x] Policy enforcement engine
- [x] API service with Echo framework
- [x] Worker service for AWS create/destroy
- [x] S3 artifact storage
- [x] TTL janitor and cleanup
- [x] Web UI (Next.js + TypeScript)
- [x] JWT authentication system
- [x] User management and RBAC

### Phase 2 (Enterprise Security & Resource Management) ✅ COMPLETE
- [x] Rate limiting (login, cluster creation, global)
- [x] Audit logging (user management, cluster operations)
- [x] AWS IAM authentication (Next.js API routes)
- [x] Request ID propagation and structured logging
- [x] S3 presigned URLs for secure downloads
- [x] Worker health checks (liveness/readiness)
- [x] Security headers (HSTS, CSP, X-Frame-Options)
- [x] Production deployment guides
- [x] Security configuration documentation
- [x] Work hours hibernation (automatic cluster hibernation)
- [x] Orphaned resource management:
  - [x] Admin UI for reviewing orphaned resources
  - [x] Automated deletion of orphaned AWS resources
  - [x] Support for VPCs, Load Balancers, EBS Volumes, Elastic IPs, Security Groups, Network Interfaces, IAM Roles, OIDC Providers, CloudWatch Log Groups
  - [x] Proper dependency handling (delete LBs before subnets, SGs after ENIs)
  - [x] Classic Load Balancer support
  - [x] NAT Gateway detection and cleanup
  - [x] Intelligent instance termination for attached EBS volumes
- [x] Comprehensive AWS resource tagging (EC2, ELB, Route53, S3, IAM)
- [x] Hybrid orphan detection (tag-based + pattern matching)
- [x] Retroactive tagging tool for existing clusters
- [x] IAM permissions for tagging and cleanup operations
- [x] EKS destroy reconciler with VPC dependency cleanup

### Phase 3 (Production Operations) 🚧 IN PROGRESS
- [ ] CloudWatch/monitoring integration
- [ ] Enhanced metrics and dashboards
- [ ] Automated backups and disaster recovery
- [ ] Performance testing and optimization

### Phase 4 (Platform Expansion) 🚧 IN PROGRESS
- [x] AWS EKS implementation (production-ready)
- [x] IBM Cloud IKS implementation (production-ready)
- [ ] Multi-region support
- [ ] Cost reporting and analytics
- [ ] Off-hours worker scaling ([Issue #11](https://github.com/tsanders-rh/ocpctl/issues/11))
- [ ] Admin policy configuration UI
- [ ] Advanced filtering and search
- [ ] Bulk operations

### Phase 5 (Advanced Features) 💡 FUTURE
- [ ] Cluster templates and customization
- [ ] Snapshot and restore
- [ ] Cluster upgrade workflows
- [ ] Integration with ITSM systems
- [ ] Advanced cost optimization
- [ ] ML-based usage predictions

## Changelog

See [CHANGELOG.md](CHANGELOG.md) for detailed version history and release notes.

**Latest Version:** v0.20260405.2d69e13 (April 5, 2026)
- ✅ Comprehensive worker robustness improvements (auto-recovery, state persistence, error handling)
- ✅ Enhanced deployment verification and error handling
- ✅ Publish strategy validation to prevent DNS issues
- ✅ Early resource tagging to prevent IAM role leaks
- ✅ FAILED cluster destroy support (no artifacts required)
- ✅ MTA addon updates (versions 6.0-8.1, Tackle instance manifests)
- ✅ EKS destroy reconciler with VPC dependency cleanup
- ✅ Automated deletion of orphaned AWS resources
- ✅ Multi-platform support: OpenShift, EKS, and IKS
- ✅ Phase 2: AWS Resource Tagging complete

## Contributing

This is an internal tool. See development guidelines in this README.

## License

Internal use only - All rights reserved.

## Documentation

### 📋 Feature Matrix
- **[Feature Matrix](docs/reference/FEATURE_MATRIX.md)** - **NEW!** Comprehensive feature overview, platform support, version compatibility

### 🚀 Getting Started
- **[AWS Quick Start Guide](docs/deployment/AWS_QUICKSTART.md)** - Deploy to AWS in 45-60 minutes
- **[Security Configuration](docs/deployment/SECURITY_CONFIGURATION.md)** - Complete security reference
- **[Web Deployment Guide](docs/deployment/DEPLOYMENT_WEB.md)** - Detailed deployment instructions

### 👥 User Guides
- **[Getting Started](docs/user-guide/getting-started.md)** - New user onboarding
- **[Cluster Management](docs/user-guide/cluster-management.md)** - Cluster lifecycle operations
- **[AWS Resource Management](docs/user-guide/aws-resource-management.md)** - **NEW!** Resource tagging, orphan detection, cost attribution

### 🔧 Operations
- **[Disk Space Management](docs/operations/DISK_SPACE_MANAGEMENT.md)** - **NEW!** Automated cleanup, monitoring, and troubleshooting
- **[Resource Tagging Operations](docs/operations/resource-tagging-operations.md)** - Operational procedures for AWS resource tagging

### 📚 Reference Documentation
- **[Architecture Overview](docs/architecture/architecture.md)** - System architecture and design
- **[Design Specification](docs/architecture/design-specification.md)** - Complete design spec
- **[Worker Concurrency](docs/architecture/worker-concurrency-safety.md)** - Concurrency model
- **[Store Package](internal/store/README.md)** - Database layer documentation
- **[API Documentation](internal/api/README.md)** - API endpoint reference

### 🔧 Setup Guides
- **[OpenShift Install Setup](docs/setup/OPENSHIFT_INSTALL_SETUP.md)** - Get openshift-install binary
- **[Testing Without OpenShift](docs/setup/TESTING_WITHOUT_OPENSHIFT.md)** - Mock testing setup
- **[BRIX Setup](docs/setup/BRIX_SETUP.md)** - IBM Cloud BRIX configuration

### 📋 Implementation
- **[Phase 3 Complete](docs/phases/PHASE-3-COMPLETE.md)** - Phase 3 summary
- **[Implementation Guide](docs/phases/IMPLEMENTATION-GUIDE.md)** - Development roadmap

### 🐛 Issues & Support
- **Issues**: [GitHub Issues](https://github.com/tsanders-rh/ocpctl/issues)
- **Security Issues**: [Issue #2 - Security Review](https://github.com/tsanders-rh/ocpctl/issues/2)

---

## Quick Reference

**Default Credentials (Change Immediately!):**
- Email: `admin@localhost`
- Password: `changeme`

**Service Ports:**
- API Server: 8080
- Worker Health: 8081
- Web UI: 3000
- Nginx: 80/443

**Health Endpoints:**
- API: `http://localhost:8080/health`
- Worker Liveness: `http://localhost:8081/health`
- Worker Readiness: `http://localhost:8081/ready`

**Deployment Cost (Test Instance):**
- ~$50/month (EC2 t3.medium + RDS db.t3.micro)

---

**Built with ❤️ for OpenShift ephemeral cluster management**
