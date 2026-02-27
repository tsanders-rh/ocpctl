.PHONY: help install-deps build test clean run-api run-worker run-janitor migrate-up migrate-down docker-up docker-down

# Default target
help:
	@echo "Available targets:"
	@echo "  install-deps    Install development dependencies"
	@echo "  build           Build all binaries"
	@echo "  test            Run all tests"
	@echo "  test-unit       Run unit tests only"
	@echo "  test-integration Run integration tests"
	@echo "  clean           Remove build artifacts"
	@echo "  run-api         Run API server locally"
	@echo "  run-worker      Run worker service locally"
	@echo "  run-janitor     Run janitor service locally"
	@echo "  migrate-up      Run database migrations"
	@echo "  migrate-down    Rollback database migrations"
	@echo "  docker-up       Start local development dependencies"
	@echo "  docker-down     Stop local development dependencies"
	@echo "  lint            Run linters"
	@echo "  fmt             Format code"

# Install development dependencies
install-deps:
	go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
	go install github.com/pressly/goose/v3/cmd/goose@latest
	cd web && npm install

# Build all binaries
build:
	@echo "Building API service..."
	go build -o bin/ocpctl-api ./cmd/api
	@echo "Building worker service..."
	go build -o bin/ocpctl-worker ./cmd/worker
	@echo "Building janitor service..."
	go build -o bin/ocpctl-janitor ./cmd/janitor
	@echo "Building CLI..."
	go build -o bin/ocpctl ./cmd/cli
	@echo "Building frontend..."
	cd web && npm run build

# Run tests
test: test-unit

test-unit:
	go test -v -race -coverprofile=coverage.out ./...

test-integration:
	go test -v -tags=integration ./...

# Clean build artifacts
clean:
	rm -rf bin/
	rm -rf web/build/
	rm -f coverage.out

# Run services locally
run-api:
	go run ./cmd/api

run-worker:
	go run ./cmd/worker

run-janitor:
	go run ./cmd/janitor

# Database migrations
migrate-up:
	@echo "Running migrations..."
	goose -dir internal/store/migrations postgres "$${DATABASE_URL}" up

migrate-down:
	@echo "Rolling back migrations..."
	goose -dir internal/store/migrations postgres "$${DATABASE_URL}" down

# Docker compose for local dependencies
docker-up:
	docker-compose up -d

docker-down:
	docker-compose down

# Linting and formatting
lint:
	golangci-lint run ./...
	cd web && npm run lint

fmt:
	go fmt ./...
	cd web && npm run format

# Development workflow
dev: docker-up
	@echo "Starting development environment..."
	@echo "Run 'make run-api' and 'make run-worker' in separate terminals"
