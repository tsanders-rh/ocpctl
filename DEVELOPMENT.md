# ocpctl Development Guide

This guide covers local development setup and workflows for ocpctl.

## Table of Contents

- [Quick Start](#quick-start)
- [Prerequisites](#prerequisites)
- [Initial Setup](#initial-setup)
- [Running Locally](#running-locally)
- [Development Workflow](#development-workflow)
- [Testing](#testing)
- [Database Management](#database-management)
- [Troubleshooting](#troubleshooting)

## Quick Start

**First time setup:**

```bash
# Clone the repository
git clone https://github.com/tsanders-rh/ocpctl.git
cd ocpctl

# Run complete setup (installs deps, sets up DB, builds binaries)
make setup
```

**Daily development:**

```bash
# Terminal 1: Start API server
make run-api

# Terminal 2: Start web frontend
cd web && npm run dev

# Open browser: http://localhost:3000
# Login: admin@localhost / changeme
```

## Prerequisites

### Required

- **Go 1.21+** - [Download](https://golang.org/dl/)
- **Node.js 18+** and npm - [Download](https://nodejs.org/)
- **PostgreSQL 14+** - [Download](https://www.postgresql.org/download/)

### macOS Installation

```bash
brew install go node postgresql
brew services start postgresql
```

### Ubuntu Installation

```bash
sudo apt-get update
sudo apt-get install -y golang-go nodejs npm postgresql postgresql-contrib
sudo systemctl start postgresql
sudo systemctl enable postgresql
```

### Verify Installation

```bash
go version      # Should be 1.21 or higher
node --version  # Should be v18 or higher
psql --version  # Should be 14 or higher
```

## Initial Setup

### Option 1: Automated Setup (Recommended)

Run the complete setup script:

```bash
make setup
```

This will:
1. Check all prerequisites
2. Install Go and Node.js dependencies
3. Create `.env` and `web/.env.local` files
4. Create and migrate the database
5. Seed the initial admin user
6. Build all binaries

### Option 2: Manual Setup

#### 1. Install Dependencies

```bash
# Install Go dependencies and dev tools
go mod download
go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
go install github.com/pressly/goose/v3/cmd/goose@latest

# Install Node.js dependencies
cd web && npm install && cd ..
```

#### 2. Configure Environment

```bash
# Copy environment template
cp .env.example .env

# Edit .env with your settings
# At minimum, verify DATABASE_URL is correct
vim .env
```

**Backend Environment (`.env`)**:

```bash
DATABASE_URL=postgresql://localhost:5432/ocpctl?sslmode=disable
JWT_SECRET=change-me-in-production-min-32-chars-required-for-security
AWS_REGION=us-east-1
ENABLE_IAM_AUTH=false
PORT=8080
```

**Frontend Environment (`web/.env.local`)**:

```bash
NEXT_PUBLIC_API_URL=http://localhost:8080/api/v1
NEXT_PUBLIC_AUTH_MODE=jwt
NEXT_PUBLIC_AWS_REGION=us-east-1
```

#### 3. Setup Database

```bash
# Option A: Use bootstrap script (recommended)
make bootstrap-db

# Option B: Manual steps
createdb ocpctl
make migrate-up
```

#### 4. Build Binaries

```bash
make build
```

## Running Locally

### Start API Server

```bash
# Run directly
make run-api

# Or run the binary
./bin/ocpctl-api
```

The API will be available at `http://localhost:8080`

### Start Web Frontend

```bash
cd web
npm run dev
```

The web UI will be available at `http://localhost:3000`

### Start Worker Service (Optional)

For cluster provisioning to work, you need the worker service:

```bash
# In a new terminal
make run-worker
```

### Start Janitor Service (Optional)

For automatic cluster cleanup:

```bash
# In a new terminal
make run-janitor
```

### Default Login Credentials

- **Email**: `admin@localhost`
- **Password**: `changeme`

**⚠️ Change this password after first login!**

## Development Workflow

### Code Changes

The development servers support hot reload:

- **API Server**: Restart manually after Go code changes
- **Web Frontend**: Auto-reloads on file changes (via Next.js)

### Running Tests

```bash
# Run all tests
make test

# Run only unit tests
make test-unit

# Run integration tests
make test-integration

# Run tests with coverage
go test -v -race -coverprofile=coverage.out ./...
```

### Linting

```bash
# Lint Go code
make lint

# Format Go code
make fmt

# Lint frontend code
cd web && npm run lint
```

### Database Migrations

#### Create a New Migration

```bash
goose -dir internal/store/migrations create my_migration_name sql
```

This creates two files:
- `NNNN_my_migration_name.sql`

Edit the file and add your SQL:

```sql
-- +goose Up
CREATE TABLE my_table (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name VARCHAR(255) NOT NULL
);

-- +goose Down
DROP TABLE my_table;
```

#### Apply Migrations

```bash
make migrate-up
```

#### Rollback Migrations

```bash
make migrate-down
```

#### Check Migration Status

```bash
goose -dir internal/store/migrations postgres "$DATABASE_URL" status
```

### Adding New API Endpoints

1. **Define handler** in `internal/api/handlers/`
2. **Add route** in `internal/api/server.go`
3. **Add types** in `pkg/types/` if needed
4. **Add store methods** in `internal/store/` if database access needed
5. **Write tests** in `*_test.go` files
6. **Update OpenAPI spec** (if applicable)

### Adding New Frontend Pages

1. **Create page** in `web/app/(dashboard)/your-page/page.tsx`
2. **Add navigation** in `web/components/layout/Sidebar.tsx`
3. **Create API hooks** in `web/lib/hooks/` if needed
4. **Add types** in `web/types/api.ts` if needed

## Database Management

### Reset Database

```bash
# Drop and recreate database
dropdb ocpctl
createdb ocpctl
make migrate-up
```

### Backup Database

```bash
pg_dump ocpctl > backup.sql
```

### Restore Database

```bash
psql ocpctl < backup.sql
```

### Connect to Database

```bash
# Using DATABASE_URL from .env
psql $DATABASE_URL

# Or directly
psql -d ocpctl
```

### Useful SQL Queries

```sql
-- List all users
SELECT id, email, username, role, active FROM users;

-- List all clusters
SELECT id, name, platform, status, owner FROM clusters;

-- List all jobs
SELECT id, job_type, status, cluster_id FROM jobs ORDER BY created_at DESC;

-- View cluster profiles
SELECT * FROM profiles;
```

## Troubleshooting

### PostgreSQL Connection Issues

**Error**: `connection refused` or `role does not exist`

**Solution**:
```bash
# Check PostgreSQL status
pg_isready

# Start PostgreSQL (macOS)
brew services start postgresql

# Start PostgreSQL (Linux)
sudo systemctl start postgresql

# Create user if needed
createuser -s $(whoami)
```

### Migration Errors

**Error**: `goose: no such file or directory`

**Solution**:
```bash
go install github.com/pressly/goose/v3/cmd/goose@latest
```

**Error**: `migration already applied`

**Solution**:
```bash
# Check migration status
goose -dir internal/store/migrations postgres "$DATABASE_URL" status

# Manually rollback if needed
make migrate-down
```

### API Server Won't Start

**Error**: `bind: address already in use`

**Solution**:
```bash
# Find process using port 8080
lsof -i :8080

# Kill the process
kill -9 <PID>
```

**Error**: `failed to connect to database`

**Solution**:
1. Check PostgreSQL is running: `pg_isready`
2. Verify `DATABASE_URL` in `.env`
3. Check database exists: `psql -l | grep ocpctl`

### Frontend Build Issues

**Error**: `Module not found`

**Solution**:
```bash
cd web
rm -rf node_modules package-lock.json
npm install
```

**Error**: `Port 3000 already in use`

**Solution**:
```bash
# Find and kill process
lsof -i :3000
kill -9 <PID>

# Or use different port
cd web
PORT=3001 npm run dev
```

### Authentication Issues

**Error**: `invalid credentials` on login

**Solution**:
1. Verify admin user exists:
   ```sql
   SELECT * FROM users WHERE email = 'admin@localhost';
   ```
2. Re-run seed migration:
   ```bash
   make migrate-down
   make migrate-up
   ```

**Error**: `token expired` or `unauthorized`

**Solution**:
- Refresh the page (frontend will auto-refresh token)
- Or login again

### AWS Integration Issues

**Error**: `AWS credentials not found`

**Solution** (if using IAM auth):
```bash
# Configure AWS credentials
aws configure

# Or set environment variables
export AWS_ACCESS_KEY_ID=your-key
export AWS_SECRET_ACCESS_KEY=your-secret
export AWS_REGION=us-east-1
```

## Common Commands Reference

```bash
# Development
make setup              # Complete first-time setup
make bootstrap-db       # Setup/reset database
make build              # Build all binaries
make run-api            # Start API server
make test               # Run tests
make lint               # Lint code
make fmt                # Format code

# Database
make migrate-up         # Apply migrations
make migrate-down       # Rollback migrations

# Docker (optional)
make docker-up          # Start dependencies
make docker-down        # Stop dependencies

# Web Frontend
cd web && npm run dev   # Start dev server
cd web && npm run build # Build production bundle
cd web && npm run lint  # Lint frontend code

# Deployment
make deploy             # Deploy to server
make build-linux        # Build for Linux
```

## Project Structure

```
ocpctl/
├── cmd/
│   ├── api/            # API server entry point
│   ├── worker/         # Worker service entry point
│   ├── janitor/        # Janitor service entry point
│   └── cli/            # CLI tool entry point
├── internal/
│   ├── api/            # API handlers and routes
│   ├── auth/           # Authentication logic
│   ├── policy/         # Policy engine
│   ├── profile/        # Cluster profiles
│   ├── providers/      # Cloud provider clients
│   └── store/          # Database layer
├── pkg/
│   └── types/          # Shared types
├── web/
│   ├── app/            # Next.js pages
│   ├── components/     # React components
│   ├── lib/            # Frontend utilities
│   └── types/          # TypeScript types
├── scripts/            # Helper scripts
└── deploy/             # Deployment configs
```

## Next Steps

- Read [Architecture Documentation](docs/ARCHITECTURE.md)
- Review [API Documentation](docs/API.md)
- Check [Deployment Guide](docs/DEPLOYMENT.md)
- Explore [Profile Definitions](internal/profile/definitions/)

## Getting Help

- **Issues**: [GitHub Issues](https://github.com/tsanders-rh/ocpctl/issues)
- **Documentation**: `docs/` directory
- **Code Examples**: Check `*_test.go` files
