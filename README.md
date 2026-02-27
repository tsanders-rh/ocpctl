# ocpctl - OpenShift Ephemeral Cluster Control Plane

Self-service provisioning and lifecycle management for ephemeral OpenShift clusters on AWS and IBM Cloud.

## Overview

`ocpctl` is an internal service that provides a standardized workflow for requesting, managing, and terminating ephemeral OpenShift 4.20 clusters with:

- **Reliable cleanup**: Preserves installer state artifacts for deterministic teardown
- **Cost control**: Automatic TTL-based destruction and resource tagging
- **Self-service**: Web UI and CLI for cluster lifecycle operations
- **Audit trail**: Complete tracking of ownership, operations, and compliance

## Key Features

- **Multi-cloud support**: AWS and IBM Cloud (phased rollout)
- **Standardized profiles**: Pre-configured cluster templates with policy controls
- **State preservation**: S3-backed artifact storage ensures clean destroy operations
- **Auto-cleanup**: TTL janitor and orphan resource detection
- **Cost attribution**: Tag-based tracking and FinOps reporting
- **Off-hours scaling**: Optional worker node scale-down/up for cost savings
- **IAM-based auth**: AWS IAM Identity Center integration

## Architecture

```
┌─────────────┐     ┌─────────────┐     ┌──────────────┐
│   Web UI    │────▶│  API Service│────▶│  Job Queue   │
│  (React)    │     │    (Go)     │     │    (SQS)     │
└─────────────┘     └─────────────┘     └──────────────┘
                           │                     │
                           ▼                     ▼
                    ┌─────────────┐     ┌──────────────┐
                    │  PostgreSQL │     │    Worker    │
                    │   (State)   │     │   Service    │
                    └─────────────┘     └──────────────┘
                                               │
                    ┌──────────────────────────┴────────┐
                    ▼                                   ▼
             ┌─────────────┐                  ┌──────────────┐
             │     S3      │                  │  openshift-  │
             │ (Artifacts) │                  │   install    │
             └─────────────┘                  └──────────────┘
```

## Quick Start

### CLI Usage

```bash
# Create a cluster
ocpctl create aws-minimal-test \
  --name team-a-smoke-01 \
  --ttl 24 \
  --owner team-a \
  --cost-center sandbox

# Check status
ocpctl status --cluster-id clu_01J...

# Scale workers down (off-hours)
ocpctl scale-workers --cluster-id clu_01J... --replicas 0

# Destroy cluster
ocpctl destroy --cluster-id clu_01J...
```

### Web UI

Access the web interface at `https://ocpctl.example.com` (requires AWS IAM Identity Center login).

## Project Structure

```
ocpctl/
├── cmd/
│   ├── api/          # API server entrypoint
│   ├── worker/       # Async job worker entrypoint
│   ├── janitor/      # TTL cleanup and orphan detection
│   └── cli/          # CLI tool (ocpctl command)
├── internal/
│   ├── api/          # HTTP handlers and routing
│   ├── worker/       # Job execution logic
│   ├── janitor/      # Cleanup and maintenance tasks
│   ├── store/        # Database access layer
│   ├── artifacts/    # S3 artifact management
│   ├── auth/         # IAM authentication/authorization
│   ├── policy/       # Policy enforcement engine
│   └── profile/      # Cluster profile definitions
├── pkg/
│   ├── types/        # Shared types and models
│   ├── logger/       # Structured logging
│   └── metrics/      # Prometheus metrics
├── web/
│   ├── src/          # React frontend source
│   └── public/       # Static assets
├── terraform/
│   ├── modules/      # Reusable Terraform modules
│   └── environments/ # Environment-specific configs
├── docs/
│   ├── design-specification.md
│   ├── api.md
│   ├── runbooks/
│   └── architecture/
└── scripts/          # Development and ops scripts
```

## Cluster Profiles

### aws-minimal-test
- 3 control-plane nodes (schedulable)
- 0 worker nodes
- Max TTL: 72 hours
- Use case: Quick testing and development

### aws-standard
- 3 control-plane nodes
- 3 worker nodes
- Max TTL: 168 hours
- Use case: Standard development and integration testing

### ibm-minimal-test
- 3 control-plane nodes (schedulable)
- 0 worker nodes
- Max TTL: 72 hours
- Use case: IBM Cloud testing

### ibm-standard
- 3 control-plane nodes
- 3 worker nodes
- Max TTL: 168 hours
- Use case: IBM Cloud development

## Development

### Prerequisites

- Go 1.22+
- Node.js 20+
- Docker
- PostgreSQL 15+
- AWS CLI configured
- `openshift-install` binary

### Local Setup

```bash
# Install dependencies
make install-deps

# Start local PostgreSQL
docker-compose up -d postgres

# Run database migrations
make migrate-up

# Start API server
make run-api

# Start worker (in another terminal)
make run-worker

# Start frontend dev server
cd web && npm install && npm start
```

### Testing

```bash
# Run all tests
make test

# Run integration tests (requires AWS credentials)
make test-integration

# Run end-to-end tests
make test-e2e
```

## Deployment

See [docs/deployment.md](docs/deployment.md) for infrastructure provisioning and deployment instructions.

## Security

- **Authentication**: AWS IAM Identity Center only
- **Authorization**: Team-based RBAC with policy controls
- **Secrets**: AWS Secrets Manager for pull secrets and credentials
- **Encryption**: S3 SSE-KMS for artifacts, TLS for all network traffic
- **Audit**: Immutable audit log for all create/destroy operations

## Observability

- **Metrics**: Prometheus-compatible metrics endpoint
- **Logs**: Structured JSON logs with correlation IDs
- **Tracing**: OpenTelemetry support (optional)
- **Dashboards**: Grafana dashboards for job status, costs, and SLOs

## Operational Runbooks

- [Failed Create Recovery](docs/runbooks/failed-create.md)
- [Failed Destroy Recovery](docs/runbooks/failed-destroy.md)
- [Missing State Artifact](docs/runbooks/missing-state.md)
- [Orphan Resource Cleanup](docs/runbooks/orphan-cleanup.md)

## Roadmap

### Phase 1 (MVP)
- [x] Project scaffolding
- [ ] API service with PostgreSQL
- [ ] Worker service for AWS create/destroy
- [ ] S3 artifact storage
- [ ] Basic web UI
- [ ] TTL janitor

### Phase 2
- [ ] IBM Cloud support
- [ ] Cost reporting by tags
- [ ] Retry orchestration
- [ ] Orphan detection

### Phase 3
- [ ] Off-hours worker scaling
- [ ] Admin policy UI
- [ ] Enhanced dashboards and SLA reports

## Contributing

This is an internal tool. See [CONTRIBUTING.md](CONTRIBUTING.md) for development guidelines.

## License

Internal use only - All rights reserved.

## Support

- Issues: [GitHub Issues](https://github.com/tsanders-rh/ocpctl/issues)
- Documentation: [docs/](docs/)
- Design Spec: [docs/design-specification.md](docs/design-specification.md)
