# ocpctl Documentation

This directory contains comprehensive documentation for the ocpctl project.

## 📋 Reference Documentation

### [reference/](reference/)

Reference documentation and specifications:

- **[FEATURE_MATRIX.md](reference/FEATURE_MATRIX.md)** - **NEW!** Comprehensive feature overview
  - Platform support (AWS, IBM Cloud)
  - OpenShift version compatibility
  - Resource tagging by service
  - Authentication methods
  - Cluster profiles
  - Security features
  - API endpoint reference
  - Known limitations and workarounds

## Directory Structure

### 📦 [setup/](setup/)

Installation and setup guides:

- **[BRIX_SETUP.md](setup/BRIX_SETUP.md)** - Complete guide for setting up ocpctl on a Fedora Brix headless box
  - Automated installation script
  - Post-installation configuration
  - Testing strategies
  - Troubleshooting

- **[OPENSHIFT_INSTALL_SETUP.md](setup/OPENSHIFT_INSTALL_SETUP.md)** - Guide for setting up OpenShift installer
  - Installing openshift-install binary
  - Obtaining Red Hat pull secret
  - AWS credentials configuration
  - Verification and testing

- **[TESTING_WITHOUT_OPENSHIFT.md](setup/TESTING_WITHOUT_OPENSHIFT.md)** - Development without cluster provisioning
  - What works without openshift-install
  - Mock data setup
  - Testing scenarios
  - Workarounds

### 🚀 [deployment/](deployment/)

Deployment and production guides:

- **[DEPLOYMENT_WEB.md](deployment/DEPLOYMENT_WEB.md)** - Web frontend deployment guide
  - Production build configuration
  - Systemd service setup
  - Nginx reverse proxy
  - SSL/TLS configuration

### 🏗️ [architecture/](architecture/)

Architecture and design documentation:

- **[architecture.md](architecture/architecture.md)** - System architecture overview
  - Component diagram
  - Data flow
  - Technology stack

- **[design-specification.md](architecture/design-specification.md)** - Detailed design specification
  - API design
  - Database schema
  - Profile system
  - Policy engine

- **[worker-concurrency-safety.md](architecture/worker-concurrency-safety.md)** - Worker service concurrency design
  - Job queue implementation
  - Concurrency safety
  - Database locking
  - Error handling

- **[api.yaml](architecture/api.yaml)** - OpenAPI specification
  - REST API endpoints
  - Request/response schemas
  - Authentication flows

### 📋 [phases/](phases/)

Implementation phase tracking (historical):

- **[PHASE-1B-COMPLETE.md](phases/PHASE-1B-COMPLETE.md)** - Data layer implementation
- **[PHASE-1C-COMPLETE.md](phases/PHASE-1C-COMPLETE.md)** - Worker service implementation
- **[PHASE-2-AUTH-DESIGN.md](phases/PHASE-2-AUTH-DESIGN.md)** - Authentication design
- **[PHASE-2-COMPLETE.md](phases/PHASE-2-COMPLETE.md)** - Authentication implementation
- **[PHASE-3-COMPLETE.md](phases/PHASE-3-COMPLETE.md)** - Web frontend implementation
- **[CRITICAL-ITEMS-RESOLVED.md](phases/CRITICAL-ITEMS-RESOLVED.md)** - Critical issues tracking
- **[DATA-LAYER-COMPLETE.md](phases/DATA-LAYER-COMPLETE.md)** - Data layer completion
- **[IMPLEMENTATION-GUIDE.md](phases/IMPLEMENTATION-GUIDE.md)** - Implementation guide

### 👥 [user-guide/](user-guide/)

User documentation and guides:

- **[getting-started.md](user-guide/getting-started.md)** - New user onboarding guide
- **[cluster-management.md](user-guide/cluster-management.md)** - Cluster lifecycle management
- **[aws-resource-management.md](user-guide/aws-resource-management.md)** - **NEW!** AWS resource tagging, orphan detection, and cost attribution

### 🔧 [operations/](operations/)

Operations and maintenance guides:

- **[resource-tagging-operations.md](operations/resource-tagging-operations.md)** - **NEW!** Operational procedures for AWS resource tagging and monitoring

### 🔬 [issues/](issues/)

Feature specifications and technical documentation:

- **[off-hours-scaling.md](issues/off-hours-scaling.md)** - Complete technical specification for off-hours worker scaling feature
- **[github-issue-off-hours-scaling.md](issues/github-issue-off-hours-scaling.md)** - GitHub issue summary for off-hours scaling
- **[OIDC_FIX_PLAN.md](issues/OIDC_FIX_PLAN.md)** - OIDC issuer validation and troubleshooting plan
- **[OIDC_STS_TOKEN_FIX.md](issues/OIDC_STS_TOKEN_FIX.md)** - Root cause analysis and fix for AWS STS InvalidIdentityToken errors

## Quick Links

### Getting Started

1. **New Users**: Start with [setup/BRIX_SETUP.md](setup/BRIX_SETUP.md) or the main [DEVELOPMENT.md](../DEVELOPMENT.md)
2. **Architecture Overview**: See [architecture/architecture.md](architecture/architecture.md)
3. **API Reference**: See [architecture/api.yaml](architecture/api.yaml)

### Common Tasks

- **Setting up development environment**: [../DEVELOPMENT.md](../DEVELOPMENT.md)
- **Deploying to production**: [deployment/DEPLOYMENT_WEB.md](deployment/DEPLOYMENT_WEB.md)
- **Installing OpenShift**: [setup/OPENSHIFT_INSTALL_SETUP.md](setup/OPENSHIFT_INSTALL_SETUP.md)
- **Testing without provisioning**: [setup/TESTING_WITHOUT_OPENSHIFT.md](setup/TESTING_WITHOUT_OPENSHIFT.md)

### Reference

- **Database Schema**: [architecture/design-specification.md](architecture/design-specification.md#database-schema)
- **Profile System**: [architecture/design-specification.md](architecture/design-specification.md#profile-system)
- **API Endpoints**: [architecture/api.yaml](architecture/api.yaml)
- **Worker Concurrency**: [architecture/worker-concurrency-safety.md](architecture/worker-concurrency-safety.md)

## Contributing

When adding new documentation:

- Place setup guides in `setup/`
- Place deployment guides in `deployment/`
- Place architectural docs in `architecture/`
- Place historical tracking docs in `phases/`
- Update this README with links to new documents

## Additional Resources

- **Main README**: [../README.md](../README.md)
- **Development Guide**: [../DEVELOPMENT.md](../DEVELOPMENT.md)
- **GitHub Repository**: https://github.com/tsanders-rh/ocpctl
