.PHONY: help install-deps build test clean run-api run-worker run-janitor migrate-up migrate-down docker-up docker-down
.PHONY: build-linux deploy-binaries deploy-profiles deploy install-services start stop restart status logs logs-api logs-worker
.PHONY: build-web deploy-web install-web-service start-web stop-web restart-web status-web logs-web

# Deployment configuration
DEPLOY_HOST ?= ubuntu@your-ec2-instance.com
DEPLOY_PATH = /opt/ocpctl

# Database configuration
DATABASE_URL ?= $(shell grep DATABASE_URL .env 2>/dev/null | cut -d '=' -f2)

# Default target
help:
	@echo "Available targets:"
	@echo ""
	@echo "Development:"
	@echo "  setup           Complete local development setup (run once)"
	@echo "  bootstrap-db    Setup database with migrations and seed data"
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
	@echo ""
	@echo "Deployment:"
	@echo "  build-linux     Build binaries for Linux deployment"
	@echo "  deploy          Deploy binaries and profiles to server"
	@echo "  deploy-binaries Deploy only binaries to server"
	@echo "  deploy-profiles Deploy only profiles to server"
	@echo "  build-web       Build Next.js production bundle"
	@echo "  deploy-web      Deploy web frontend to server"
	@echo "  install-services Install systemd services (run on server)"
	@echo "  install-web-service Install web systemd service (run on server)"
	@echo ""
	@echo "Service Management (on server):"
	@echo "  start           Start all services"
	@echo "  stop            Stop all services"
	@echo "  restart         Restart all services"
	@echo "  status          Check service status"
	@echo "  logs            View all service logs"
	@echo "  logs-api        View API logs only"
	@echo "  logs-worker     View worker logs only"
	@echo "  start-web       Start web service"
	@echo "  stop-web        Stop web service"
	@echo "  restart-web     Restart web service"
	@echo "  status-web      Check web service status"
	@echo "  logs-web        View web service logs"

# Complete local development setup
setup:
	@echo "Running complete development setup..."
	@chmod +x scripts/*.sh
	./scripts/setup-dev.sh

# Bootstrap database (create DB, run migrations, seed data)
bootstrap-db:
	@echo "Bootstrapping database..."
	@chmod +x scripts/bootstrap-db.sh
	./scripts/bootstrap-db.sh

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

# Deployment targets
build-linux:
	@echo "Building for Linux (amd64)..."
	@mkdir -p bin
	GOOS=linux GOARCH=amd64 go build -o bin/ocpctl-api-linux ./cmd/api
	GOOS=linux GOARCH=amd64 go build -o bin/ocpctl-worker-linux ./cmd/worker
	@echo "Built Linux binaries in bin/"

deploy-binaries: build-linux
	@echo "Deploying binaries to $(DEPLOY_HOST)..."
	scp bin/ocpctl-api-linux $(DEPLOY_HOST):$(DEPLOY_PATH)/bin/ocpctl-api
	scp bin/ocpctl-worker-linux $(DEPLOY_HOST):$(DEPLOY_PATH)/bin/ocpctl-worker
	ssh $(DEPLOY_HOST) "sudo chown ocpctl:ocpctl $(DEPLOY_PATH)/bin/*"
	ssh $(DEPLOY_HOST) "sudo chmod +x $(DEPLOY_PATH)/bin/*"
	@echo "Binaries deployed successfully"

deploy-profiles:
	@echo "Deploying profiles to $(DEPLOY_HOST)..."
	rsync -av --delete internal/profile/definitions/ $(DEPLOY_HOST):$(DEPLOY_PATH)/profiles/
	ssh $(DEPLOY_HOST) "sudo chown -R ocpctl:ocpctl $(DEPLOY_PATH)/profiles"
	@echo "Profiles deployed successfully"

deploy: deploy-binaries deploy-profiles
	@echo ""
	@echo "=== Deployment Complete ==="
	@echo "Restart services with:"
	@echo "  ssh $(DEPLOY_HOST) 'sudo systemctl restart ocpctl-api ocpctl-worker'"

install-services:
	@echo "Installing systemd services..."
	sudo cp deploy/systemd/ocpctl-api.service /etc/systemd/system/
	sudo cp deploy/systemd/ocpctl-worker.service /etc/systemd/system/
	sudo cp deploy/systemd/ocpctl-web.service /etc/systemd/system/
	sudo systemctl daemon-reload
	@echo ""
	@echo "Services installed. Enable and start with:"
	@echo "  sudo systemctl enable ocpctl-api ocpctl-worker ocpctl-web"
	@echo "  sudo systemctl start ocpctl-api ocpctl-worker ocpctl-web"

# Service management (run on server)
start:
	sudo systemctl start ocpctl-api ocpctl-worker ocpctl-web

stop:
	sudo systemctl stop ocpctl-api ocpctl-worker ocpctl-web

restart:
	sudo systemctl restart ocpctl-api ocpctl-worker ocpctl-web

status:
	sudo systemctl status ocpctl-api ocpctl-worker ocpctl-web

logs:
	sudo journalctl -u ocpctl-api -u ocpctl-worker -u ocpctl-web -f

logs-api:
	sudo journalctl -u ocpctl-api -f

logs-worker:
	sudo journalctl -u ocpctl-worker -f

# Web frontend targets
build-web:
	@echo "Building Next.js production bundle..."
	cd web && npm install && npm run build
	@echo "Web frontend built successfully"

deploy-web: build-web
	@echo "Deploying web frontend to $(DEPLOY_HOST)..."
	rsync -avz --delete \
		--exclude node_modules \
		--exclude .next/cache \
		--exclude .env.local \
		web/ $(DEPLOY_HOST):$(DEPLOY_PATH)/web/
	ssh $(DEPLOY_HOST) "cd $(DEPLOY_PATH)/web && npm install --production"
	ssh $(DEPLOY_HOST) "sudo chown -R ocpctl:ocpctl $(DEPLOY_PATH)/web"
	@echo ""
	@echo "=== Web Frontend Deployed ==="
	@echo "Configure environment at: /etc/ocpctl/web.env"
	@echo "Restart web service with:"
	@echo "  ssh $(DEPLOY_HOST) 'sudo systemctl restart ocpctl-web'"

install-web-service:
	@echo "Installing web systemd service..."
	sudo cp deploy/systemd/ocpctl-web.service /etc/systemd/system/
	sudo systemctl daemon-reload
	@echo ""
	@echo "Web service installed. Enable and start with:"
	@echo "  sudo systemctl enable ocpctl-web"
	@echo "  sudo systemctl start ocpctl-web"

# Web service management (run on server)
start-web:
	sudo systemctl start ocpctl-web

stop-web:
	sudo systemctl stop ocpctl-web

restart-web:
	sudo systemctl restart ocpctl-web

status-web:
	sudo systemctl status ocpctl-web

logs-web:
	sudo journalctl -u ocpctl-web -f
