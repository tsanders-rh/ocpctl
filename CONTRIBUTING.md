# Contributing to ocpctl

## Development Setup

### Prerequisites

- Go 1.22 or higher
- Node.js 20 or higher
- Docker and Docker Compose
- PostgreSQL client tools
- AWS CLI configured with appropriate credentials
- `openshift-install` binary (for integration testing)

### Getting Started

1. Clone the repository:
```bash
git clone https://github.com/tsanders-rh/ocpctl.git
cd ocpctl
```

2. Install dependencies:
```bash
make install-deps
```

3. Start local dependencies:
```bash
make docker-up
```

4. Run database migrations:
```bash
export DATABASE_URL="postgres://ocpctl:ocpctl-dev-password@localhost:5432/ocpctl?sslmode=disable"
make migrate-up
```

5. Start the API server:
```bash
make run-api
```

6. In another terminal, start the worker:
```bash
make run-worker
```

7. Start the frontend development server:
```bash
cd web
npm start
```

## Code Organization

- `cmd/`: Main applications (api, worker, janitor, cli)
- `internal/`: Private application code
- `pkg/`: Public library code (reusable packages)
- `web/`: React frontend application
- `terraform/`: Infrastructure as Code
- `docs/`: Documentation
- `scripts/`: Build and deployment scripts

## Coding Standards

### Go

- Follow standard Go conventions and idioms
- Use `gofmt` for formatting
- Run `golangci-lint` before submitting
- Write tests for all new functionality
- Keep functions small and focused
- Use meaningful variable and function names

### TypeScript/React

- Use TypeScript for all new code
- Follow React best practices and hooks patterns
- Use ESLint and Prettier for formatting
- Write tests using Jest and React Testing Library
- Keep components small and reusable

## Testing

### Unit Tests

```bash
make test-unit
```

### Integration Tests

Integration tests require AWS credentials and will create real resources:

```bash
make test-integration
```

### End-to-End Tests

E2E tests run against a full deployment:

```bash
make test-e2e
```

## Pull Request Process

1. Create a feature branch from `main`:
```bash
git checkout -b feature/your-feature-name
```

2. Make your changes and commit:
```bash
git add .
git commit -m "feat: add new feature"
```

3. Run tests and linting:
```bash
make test
make lint
```

4. Push to your fork and create a pull request

5. Ensure CI passes and address any review feedback

## Commit Message Format

Follow conventional commits:

- `feat:` New feature
- `fix:` Bug fix
- `docs:` Documentation changes
- `test:` Adding or updating tests
- `refactor:` Code refactoring
- `perf:` Performance improvements
- `chore:` Build process or auxiliary tool changes

Example:
```
feat: add IBM Cloud cluster profile support

- Implement IBMCloudProvider interface
- Add ibm-minimal-test and ibm-standard profiles
- Update profile validation logic
```

## Architecture Decisions

Before making significant architectural changes:

1. Review the [design specification](docs/design-specification.md)
2. Discuss in an issue or discussion thread
3. Update design docs as needed
4. Get approval from maintainers

## Security

- Never commit secrets, credentials, or sensitive data
- Use AWS Secrets Manager for all secrets
- Sanitize logs to prevent secret leakage
- Follow least-privilege principles for IAM roles
- Report security issues privately to maintainers

## Database Migrations

Create new migrations using goose:

```bash
goose -dir internal/store/migrations create your_migration_name sql
```

Migrations must:
- Be reversible (include both up and down)
- Be idempotent where possible
- Include clear comments
- Be tested in a dev environment first

## Questions?

- Review the [design specification](docs/design-specification.md)
- Check existing issues and discussions
- Reach out to the maintainers

Thank you for contributing to ocpctl!
