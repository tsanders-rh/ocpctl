#!/bin/bash

# Complete local development setup for ocpctl
# This script sets up everything needed for local development

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

echo -e "${BLUE}"
echo "╔═══════════════════════════════════════╗"
echo "║   ocpctl Local Development Setup     ║"
echo "╚═══════════════════════════════════════╝"
echo -e "${NC}\n"

# Step 1: Check prerequisites
echo -e "${YELLOW}Step 1: Checking prerequisites...${NC}\n"

# Check Go
if ! command -v go &> /dev/null; then
    echo -e "${RED}✗${NC} Go is not installed. Please install Go 1.21 or later"
    exit 1
fi
GO_VERSION=$(go version | awk '{print $3}')
echo -e "${GREEN}✓${NC} Go installed: $GO_VERSION"

# Check Node.js
if ! command -v node &> /dev/null; then
    echo -e "${RED}✗${NC} Node.js is not installed. Please install Node.js 18 or later"
    exit 1
fi
NODE_VERSION=$(node --version)
echo -e "${GREEN}✓${NC} Node.js installed: $NODE_VERSION"

# Check PostgreSQL
if ! command -v psql &> /dev/null; then
    echo -e "${RED}✗${NC} PostgreSQL client is not installed"
    echo -e "  Install with: ${YELLOW}brew install postgresql${NC} (macOS)"
    echo -e "  Or: ${YELLOW}sudo apt-get install postgresql-client${NC} (Ubuntu)"
    exit 1
fi
echo -e "${GREEN}✓${NC} PostgreSQL client installed"

# Check if PostgreSQL server is running
if ! pg_isready -h localhost > /dev/null 2>&1; then
    echo -e "${YELLOW}⚠${NC} PostgreSQL server is not running"
    echo -e "  Start with: ${YELLOW}brew services start postgresql${NC} (macOS)"
    echo -e "  Or: ${YELLOW}sudo systemctl start postgresql${NC} (Linux)"
    read -p "Press enter after starting PostgreSQL to continue..."
fi

# Step 2: Environment configuration
echo -e "\n${YELLOW}Step 2: Setting up environment configuration...${NC}\n"

if [ ! -f .env ]; then
    echo -e "${YELLOW}⚠${NC} .env file not found. Checking .env.example..."
    if [ -f .env.example ]; then
        cp .env.example .env
        echo -e "${GREEN}✓${NC} Created .env from .env.example"
        echo -e "${YELLOW}⚠${NC} Please review and update .env with your settings"
    else
        echo -e "${RED}✗${NC} .env.example not found"
        exit 1
    fi
else
    echo -e "${GREEN}✓${NC} .env file exists"
fi

if [ ! -f web/.env.local ]; then
    echo -e "${YELLOW}⚠${NC} web/.env.local not found"
    if [ -f web/.env.local.example ]; then
        cp web/.env.local.example web/.env.local
        echo -e "${GREEN}✓${NC} Created web/.env.local from example"
    fi
else
    echo -e "${GREEN}✓${NC} web/.env.local file exists"
fi

# Step 3: Install Go dependencies
echo -e "\n${YELLOW}Step 3: Installing Go dependencies...${NC}\n"
go mod download
echo -e "${GREEN}✓${NC} Go dependencies installed"

# Install development tools
echo -e "\n${YELLOW}Installing development tools...${NC}"
if ! command -v goose &> /dev/null; then
    go install github.com/pressly/goose/v3/cmd/goose@latest
    echo -e "${GREEN}✓${NC} goose installed"
else
    echo -e "${GREEN}✓${NC} goose already installed"
fi

if ! command -v golangci-lint &> /dev/null; then
    go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
    echo -e "${GREEN}✓${NC} golangci-lint installed"
else
    echo -e "${GREEN}✓${NC} golangci-lint already installed"
fi

# Step 4: Install Node.js dependencies
echo -e "\n${YELLOW}Step 4: Installing Node.js dependencies...${NC}\n"
cd web
npm install
echo -e "${GREEN}✓${NC} Node.js dependencies installed"
cd ..

# Step 5: Setup database
echo -e "\n${YELLOW}Step 5: Setting up database...${NC}\n"
./scripts/bootstrap-db.sh

# Step 6: Build binaries
echo -e "\n${YELLOW}Step 6: Building binaries...${NC}\n"
make build
echo -e "${GREEN}✓${NC} Binaries built successfully"

# Success message
echo -e "\n${GREEN}"
echo "╔═══════════════════════════════════════╗"
echo "║      Setup Complete! 🎉               ║"
echo "╚═══════════════════════════════════════╝"
echo -e "${NC}\n"

echo -e "${YELLOW}Quick Start:${NC}\n"
echo -e "  ${BLUE}1.${NC} Start the API server:"
echo -e "     ${GREEN}make run-api${NC}\n"
echo -e "  ${BLUE}2.${NC} In a new terminal, start the web frontend:"
echo -e "     ${GREEN}cd web && npm run dev${NC}\n"
echo -e "  ${BLUE}3.${NC} Open your browser:"
echo -e "     ${GREEN}http://localhost:3000${NC}\n"
echo -e "  ${BLUE}4.${NC} Login with default credentials:"
echo -e "     Email:    ${GREEN}admin@localhost${NC}"
echo -e "     Password: ${GREEN}changeme${NC}\n"

echo -e "${YELLOW}Useful Commands:${NC}\n"
echo -e "  ${GREEN}make help${NC}           - Show all available make targets"
echo -e "  ${GREEN}make test${NC}           - Run tests"
echo -e "  ${GREEN}make lint${NC}           - Run linters"
echo -e "  ${GREEN}make migrate-up${NC}     - Run database migrations"
echo -e "  ${GREEN}make migrate-down${NC}   - Rollback database migrations"
echo -e "  ${GREEN}make docker-up${NC}      - Start local dependencies (if using Docker)\n"

echo -e "${YELLOW}⚠  Security Reminder:${NC}"
echo -e "  - Change the JWT_SECRET in .env before deploying to production"
echo -e "  - Change the admin password after first login\n"
