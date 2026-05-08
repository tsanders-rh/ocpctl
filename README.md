# ocpctl

**Self-service Kubernetes cluster provisioning and lifecycle management for OpenShift, ROSA, EKS, IKS, and GKE.**

ocpctl is a production-ready platform that provides a standardized workflow for requesting, managing, and terminating ephemeral Kubernetes clusters on AWS, Google Cloud Platform (GCP), and IBM Cloud with automated cost controls, comprehensive security, and enterprise-grade operations.

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
- ✅ **OpenShift 4.x** (AWS, GCP, IBM Cloud) - Full lifecycle including hibernation
- ✅ **ROSA (Red Hat OpenShift Service on AWS)** - Fully-managed OpenShift with AWS-managed control plane
- ✅ **Google Kubernetes Engine (GKE)** - Managed Kubernetes on Google Cloud
- ✅ **AWS EKS** - Elastic Kubernetes Service with VPC management
- ✅ **IBM Cloud IKS** - IBM Kubernetes Service
- ✅ **Multi-version support** - OpenShift 4.18 - 4.22+, Kubernetes 1.30 - 1.35

### Cost Management
- **Automatic TTL-based destruction** - Clusters self-destruct after configured lifetime
- **Work hours hibernation** - Stop instances outside business hours (OpenShift, ROSA worker scaling, GKE auto-scaling)
- **Comprehensive resource tagging** - Track costs by owner, team, cost center, and cluster
- **Orphaned resource detection** - Identify and clean up resources from failed deployments (AWS, GCP)
- **Cloud billing integration** - GCP BigQuery billing export for accurate cost tracking

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

## Getting Started

**Deployment Path:**
```
Prerequisites → Choose Method → Deploy → Verify
     ↓              ↓               ↓        ↓
  (Step 1)      (Step 2)        (Guide)  (Step 3)
```

### 📋 Step 1: Prerequisites

**Before deploying, verify you have everything needed:**

👉 **[Complete Prerequisites Guide](docs/deployment/PREREQUISITES.md)**

Quick checklist:
- ✅ AWS account with appropriate permissions
- ✅ Route53 hosted zone for cluster domains
- ✅ OpenShift pull secret from Red Hat
- ✅ AWS CLI, Git, and build tools installed
- ✅ ~$150-500/month budget (varies by usage)

### 🚀 Step 2: Choose Deployment Method

**Option A: Manual Deployment (Recommended for First-Time Users)**

Complete AWS deployment in 60 minutes:

👉 **[AWS Quick Start Guide](docs/deployment/AWS_QUICKSTART.md)**

1. Create PostgreSQL database (RDS or EC2)
2. Launch EC2 instance (t3.large recommended)
3. Deploy binaries (API, Worker, Web UI)
4. Configure nginx with HTTPS
5. Verify deployment

**Estimated cost:** ~$73-94/month for platform + cluster costs

**Option B: Terraform Deployment (For Teams & Production)**

Infrastructure as Code deployment:

👉 **[Terraform Deployment Guide](docs/deployment/TERRAFORM.md)**

- Version-controlled infrastructure
- Autoscaling worker pool
- CloudWatch monitoring included
- Ideal for multi-environment setups

### ✅ Step 3: Verify Deployment

After deployment, use the verification checklist:

👉 **[Deployment Verification Checklist](docs/deployment/DEPLOYMENT_CHECKLIST.md)**

- 100+ verification checks
- Pre-deployment, deployment, and post-deployment phases
- Troubleshooting decision trees
- Test cluster creation

### 📚 Additional Resources

- **[Cost Estimation Guide](docs/operations/COST_ESTIMATION.md)** - Detailed cost breakdown and optimization
- **[Troubleshooting Guide](docs/operations/TROUBLESHOOTING.md)** - Common issues and solutions
- **[IAM Policies Guide](deploy/IAM_POLICIES.md)** - Ready-to-use IAM policies

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

**Version:** v0.20260507.a07a92c (May 7, 2026)

**Recent Updates:**
- ✅ **ROSA Support** - Full lifecycle management for Red Hat OpenShift Service on AWS
- ✅ ROSA credentials fix - Automatic credential capture with retry logic
- ✅ Password visibility toggle - Eye icon to show/hide cluster credentials in UI
- ✅ Windows VM management - Script for bulk Windows VM operations on CNV clusters
- ✅ Security hardening - All critical/high/medium severity issues addressed

See [CHANGELOG.md](CHANGELOG.md) for complete release history.

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
