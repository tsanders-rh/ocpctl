# ocpctl

**Self-service Kubernetes cluster provisioning and lifecycle management for OpenShift, ROSA, EKS, IKS, GKE, ARO, and AKS.**

ocpctl is a production-ready platform that provides a standardized workflow for requesting, managing, and terminating ephemeral Kubernetes clusters on AWS, Google Cloud Platform (GCP), IBM Cloud, and Microsoft Azure with automated cost controls, comprehensive security, and enterprise-grade operations.

---

## Why ocpctl?

**The Problem:**
- Manual cluster provisioning is time-consuming and error-prone
- **CI/CD pipelines wait 30-60 minutes** for cluster provisioning
- Forgotten test clusters waste thousands in cloud costs
- No standardized way to track cluster ownership and costs
- Cleanup is unreliable - orphaned resources accumulate

**The Solution:**
- **Instant cluster access** - Pre-provisioned cluster pools provide clusters in < 5 seconds (100x faster)
- **Automated lifecycle management** - Create, monitor, hibernate, and destroy clusters via web UI or API
- **Automatic cost controls** - TTL-based destruction, work hours hibernation, and comprehensive resource tagging
- **Reliable cleanup** - Preserves installer state for deterministic teardown and detects orphaned resources
- **Enterprise security** - Dual authentication (JWT/IAM), rate limiting, audit logging, and RBAC
- **Multi-platform** - OpenShift, ROSA, ARO, EKS, AKS, GKE, and IKS across AWS, Azure, GCP, and IBM Cloud

---

## Key Features

### Platform Support
- ✅ **OpenShift 4.x** (AWS, GCP, IBM Cloud) - Full lifecycle including hibernation
- ✅ **ROSA (Red Hat OpenShift Service on AWS)** - Fully-managed OpenShift with AWS-managed control plane
- ✅ **ARO (Azure Red Hat OpenShift)** - Managed OpenShift on Microsoft Azure
- ✅ **Google Kubernetes Engine (GKE)** - Managed Kubernetes on Google Cloud
- ✅ **AWS EKS** - Elastic Kubernetes Service with VPC management
- ✅ **Azure AKS** - Azure Kubernetes Service with auto-scaling node pools
- ✅ **IBM Cloud IKS** - IBM Kubernetes Service
- ✅ **Multi-version support** - OpenShift 4.16 - 4.22+, Kubernetes 1.30 - 1.35

### Cluster Pools 🚀
- **Instant cluster access** - Lease pre-provisioned clusters in < 5 seconds (vs 30-60 minute provisioning)
- **ServiceAccount credentials** - Time-bound tokens with automatic expiration matching lease duration
- **CI/CD optimized** - Perfect for GitHub Actions, Jenkins, Tekton, and GitLab pipelines
- **Auto-scaling pools** - Maintains target number of ready clusters, scales with demand
- **Auto-release** - Clusters automatically return to pool after lease expiration
- **Work hours scheduling** - Pools scale down outside business hours to reduce costs
- **Real-time metrics** - Monitor pool health, utilization, and lease activity
- **Multi-tenancy** - Teams share pools without complex Prow-like infrastructure
- **REST API** - Simple lease/release API for automation
- **Cost-efficient** - Shared pools reduce overhead vs per-job cluster provisioning

**Use Cases:**
- Automated testing in CI/CD pipelines (100x faster than provisioning)
- Rapid development iterations without waiting
- Demo and presentation environments
- Integration testing with production-like clusters

### Cost Management
- **Automatic TTL-based destruction** - Clusters self-destruct after configured lifetime
- **Work hours hibernation** - Stop instances outside business hours (OpenShift, ROSA/ARO worker scaling, GKE/AKS auto-scaling)
- **Comprehensive resource tagging** - Track costs by owner, team, cost center, and cluster
- **Orphaned resource detection** - Identify and clean up resources from failed deployments (AWS, GCP, Azure)
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
- **Dev/test environments** - Multi-environment deployment support with maintenance window procedures
- **Easy rollback** - Version management with atomic symlink switching for instant rollbacks

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
- ✅ Cloud account with appropriate permissions (AWS, GCP, IBM Cloud, or Azure)
- ✅ Route53 hosted zone for cluster domains (AWS OpenShift)
- ✅ OpenShift pull secret from Red Hat (for OpenShift/ROSA/ARO clusters)
- ✅ Cloud CLI tools installed (aws-cli, gcloud, ibmcloud, or azure-cli)
- ✅ ~$150-500/month budget (varies by usage and platform)

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

- **[Deployment Guide](docs/deployment/DEPLOYMENT_GUIDE.md)** - Comprehensive deployment procedures and maintenance windows
- **[Dev/Test Environment Plan](docs/deployment/DEV_TEST_ENVIRONMENT_PLAN.md)** - Multi-environment setup strategy
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
└─────────────┘     └──────┬──────┘     └──────┬───────┘
                           │                    │
                           │  ┌─────────────────┘
                           │  │ Pool Manager (Background)
                           │  │ • Replenish pools
                           │  │ • Release expired leases
                           │  │ • Refresh aged clusters
                           ▼  ▼
                    ┌─────────────┐
                    │   Worker    │
                    │  Service    │
                    └──────┬──────┘
                           │
            ┌──────────────┼──────────────┬──────────────┐
            ▼              ▼              ▼              ▼
     ┌───────────┐  ┌────────────┐  ┌──────────┐  ┌────────┐
     │    S3     │  │ openshift- │  │ Janitor  │  │ Pools  │
     │(Artifacts)│  │  install   │  │ (Cleanup)│  │(Ready) │
     └───────────┘  └────────────┘  └──────────┘  └────────┘
```

**Services:**
- **API Server** (Port 8080) - REST API with authentication, rate limiting, and pool manager
- **Worker** (Port 8081) - Asynchronous cluster provisioning and lifecycle operations
- **Web UI** (Port 3000) - Modern React frontend with server-side rendering
- **Pool Manager** - Background service maintaining cluster pools (embedded in API server)
- **Janitor** - TTL enforcement and orphaned resource cleanup (embedded in worker)

---

## Documentation

### 📋 Getting Started
- **[AWS Quick Start](docs/deployment/AWS_QUICKSTART.md)** - Deploy to AWS in under an hour
- **[User Guide](docs/user-guide/getting-started.md)** - New user onboarding and first cluster
- **[Cluster Pools Guide](docs/user-guide/cluster-pools.md)** - Instant cluster access for CI/CD pipelines
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
- **[Cluster Pools Architecture](docs/features/CLUSTER_POOLS.md)** - Pool manager design and implementation
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

# --- Cluster Pools (Instant Access) ---

# List available pools
curl -X GET https://api.ocpctl.mg.dog8code.com/v1/pools \
  -H "Authorization: Bearer $TOKEN"

# Get pool statistics
curl -X GET https://api.ocpctl.mg.dog8code.com/v1/pools/ci-pool/stats \
  -H "Authorization: Bearer $TOKEN"

# Lease cluster from pool (< 5 seconds!)
LEASE=$(curl -X POST https://api.ocpctl.mg.dog8code.com/v1/pools/ci-pool/lease \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "leased_by": "github-actions-run-123",
    "metadata": {
      "repo": "my-org/my-app",
      "workflow": "integration-tests",
      "run_id": "12345"
    }
  }')

# Extract cluster details
CLUSTER_ID=$(echo $LEASE | jq -r '.cluster_id')
API_URL=$(echo $LEASE | jq -r '.api_url')
SA_TOKEN=$(echo $LEASE | jq -r '.sa_token')
OC_LOGIN_CMD=$(echo $LEASE | jq -r '.oc_login_command')

# Option 1: Login with ServiceAccount token (recommended for CI/CD)
eval $OC_LOGIN_CMD  # oc login https://api.cluster.example.com:6443 --token=sha256~...

# Option 2: Download kubeconfig from S3
KUBECONFIG_PATH=$(echo $LEASE | jq -r '.kubeconfig_path')
aws s3 cp $KUBECONFIG_PATH ./kubeconfig
export KUBECONFIG=./kubeconfig

# Use the cluster
kubectl get nodes
oc get clusterversion

# Release cluster back to pool when done
curl -X POST https://api.ocpctl.mg.dog8code.com/v1/pools/clusters/$CLUSTER_ID/release \
  -H "Authorization: Bearer $TOKEN"

# --- Traditional Cluster Creation (30-60 minutes) ---

# List clusters
curl -X GET https://api.ocpctl.mg.dog8code.com/v1/clusters \
  -H "Authorization: Bearer $TOKEN"

# Create AWS OpenShift cluster
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

# Create Azure ARO cluster
curl -X POST https://api.ocpctl.mg.dog8code.com/v1/clusters \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "my-aro-cluster",
    "platform": "azure",
    "cluster_type": "aro",
    "version": "4.20.15",
    "profile": "azure-aro-standard",
    "region": "eastus",
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

**Version:** v0.20260614.3f03e5d (June 14, 2026)

**Recent Updates:**
- ✅ **ServiceAccount Credentials for Pool Clusters** - Enhanced security with time-bound tokens
  - Automatic ServiceAccount creation with cluster-admin permissions
  - Time-bound tokens matching lease duration (no manual expiration needed)
  - Credentials persist through pool cleaning cycles
  - `oc login` command included in lease response
  - Displayed in both lease modal and cluster details page
- ✅ **Dev/Test Environment Support** - Multi-environment deployment infrastructure
  - Environment-specific configurations (dev/production)
  - Maintenance window deployment procedures
  - Emergency hotfix workflows
  - Cost-efficient dev environment (~$60/month vs $320/month production)
  - Comprehensive deployment guide with troubleshooting
- ✅ **API Documentation** - Updated Swagger/OpenAPI spec with ServiceAccount fields
- ✅ **Documentation Organization** - Restructured docs into logical subdirectories
- ✅ **Cluster Pools** - 🚀 Instant cluster access for CI/CD pipelines
  - Pre-provisioned clusters available in < 5 seconds (100x faster)
  - Auto-scaling pools with work hours scheduling
  - REST API for lease/release operations
  - Real-time pool metrics and utilization tracking
  - Perfect for GitHub Actions, Jenkins, Tekton pipelines
- ✅ **Azure Platform Support** - ARO (Azure Red Hat OpenShift) and AKS (Azure Kubernetes Service)
- ✅ **ROSA Support** - Full lifecycle management for Red Hat OpenShift Service on AWS
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
