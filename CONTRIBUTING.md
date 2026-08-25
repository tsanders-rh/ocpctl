# Contributing to ocpctl

Thanks for your interest in contributing! This is the quick guide. For the full
process — branching model, branch protection, CI gates, hotfixes, and where
different kinds of change live — see
[docs/development/CONTRIBUTING.md](docs/development/CONTRIBUTING.md) and
[docs/development/DEVOPS.md](docs/development/DEVOPS.md).

## Code of Conduct

Be respectful and constructive. This project is part of the
[migtools](https://github.com/migtools) community and follows the
[Konveyor community guidelines](https://www.konveyor.io/). See
[CODE_OF_CONDUCT.md](CODE_OF_CONDUCT.md) for the full text. Report unacceptable
behavior to the Konveyor Code of Conduct Committee at <conduct@konveyor.io>.

## Prerequisites

- Go **1.25+** (see `go.mod` for the exact version)
- Node.js 20+ (for `web/`)
- Docker and Docker Compose (local Postgres and dependencies)
- PostgreSQL client tools
- AWS CLI configured with appropriate credentials (for integration tests)
- `openshift-install` (for integration/e2e provisioning)

## Getting Started

```bash
# Clone
git clone https://github.com/tsanders-rh/ocpctl.git
cd ocpctl

# Install tooling and start local dependencies (Postgres, etc.)
make install-deps
make docker-up

# Run database migrations (goose)
export DATABASE_URL="postgres://ocpctl:ocpctl-dev-password@localhost:5432/ocpctl?sslmode=disable"
make migrate-up

# Run the services (each in its own terminal)
make run-api
make run-worker

# Frontend dev server
cd web && npm run dev
```

## Code Organization

- `cmd/` — main applications (api, worker, janitor, cli)
- `internal/` — private application code
- `pkg/` — public library code
- `web/` — Next.js frontend
- `terraform/` — infrastructure as code
- `docs/` — documentation
- `scripts/` — build and deployment scripts

## Before You Push — Local Checks

These mirror the CI gates, so run them first:

```bash
make fmt     # gofmt + web format
make lint    # golangci-lint + next lint  (config: .golangci.yml)
make test    # go test -race ./...   ← hard gate; MUST pass
make build   # compiles both binaries
```

**Unit tests are a hard gate.** If `make test` fails the change isn't ready — CI
blocks it and the deploy scripts refuse to build it. Add or update tests
alongside behavior changes, and put a regression test on every bug fix.

`make test-integration` runs tests that need a local Postgres and (for some) AWS
credentials.

## Commit Messages

Use **verb-first, imperative titles** that describe what the change does (≤ 72
chars), matching the project's history and the migtools convention:

```
Add azure-sno-ga prerelease profile
Fix TMPDIR creation so openshift-install writes bootstrap ignition
Update profile cost estimates to on-demand list prices
```

- Reference the PR/issue where relevant, e.g. `Add adhoc usage report (#114)`.
- Explain **why** in the body when it isn't obvious from the diff.
- One logical change per commit.

## Pull Requests

1. Branch off the latest `main` (`feature/…`, `fix/…`, `chore/…`); never push
   directly to `main`.
2. Fill in the PR template — what/why, test evidence, risk, and whether it
   touches DB migrations, Terraform/infra, or worker boot scripts.
3. Ensure **CI is green** (the `vet-and-test` check: `go vet` + `go test -race`
   plus a gofmt check).
4. Get at least one approving review, then **squash and merge**.

## Database Migrations

Migrations live in `internal/store/migrations/NNNNN_*.sql` and use
[goose](https://github.com/pressly/goose):

```bash
goose -dir internal/store/migrations -s create your_migration_name sql
```

Migrations must be additive/backward-compatible, reversible (both `+goose Up` and
`+goose Down`), and tested with `make migrate-up` / `make migrate-down`.

## Security

- Never commit secrets, credentials, or sensitive data.
- Sanitize logs to prevent secret leakage.
- Follow least-privilege for IAM roles.
- Report security issues privately to the maintainers.

## Questions?

- Full contributor process: [docs/development/CONTRIBUTING.md](docs/development/CONTRIBUTING.md)
- Deploy & infra mechanics: [docs/development/DEVOPS.md](docs/development/DEVOPS.md)
- Design: [docs/architecture/design-specification.md](docs/architecture/design-specification.md)
- Check existing issues and discussions, or reach out to the maintainers.

Thank you for contributing to ocpctl!
