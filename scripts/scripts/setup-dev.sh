#!/bin/bash
# Complete local development setup for ocpctl
set -e

BLUE='\033[0;34m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m'

echo -e "${BLUE}╔═══════════════════════════════════════╗${NC}"
echo -e "${BLUE}║   ocpctl Local Development Setup     ║${NC}"
echo -e "${BLUE}╚═══════════════════════════════════════╝${NC}\n"

# Check prerequisites
echo -e "${YELLOW}Checking prerequisites...${NC}\n"

if ! command -v go &> /dev/null; then
    echo -e "${RED}✗${NC} Go not installed"
    exit 1
fi
echo -e "${GREEN}✓${NC} Go: $(go version)"

if ! command -v node &> /dev/null; then
    echo -e "${RED}✗${NC} Node.js not installed"
    exit 1
fi
echo -e "${GREEN}✓${NC} Node.js: $(node --version)"

if ! command -v psql &> /dev/null; then
    echo -e "${RED}✗${NC} PostgreSQL client not installed"
    exit 1
fi
echo -e "${GREEN}✓${NC} PostgreSQL client installed"

# Setup environment files
echo -e "\n${YELLOW}Setting up environment...${NC}\n"
[ ! -f .env ] && [ -f .env.example ] && cp .env.example .env && echo -e "${GREEN}✓${NC} Created .env"
[ ! -f web/.env.local ] && [ -f web/.env.local.example ] && cp web/.env.local.example web/.env.local && echo -e "${GREEN}✓${NC} Created web/.env.local"

# Install dependencies
echo -e "\n${YELLOW}Installing Go dependencies...${NC}"
go mod download
go install github.com/pressly/goose/v3/cmd/goose@latest
go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
echo -e "${GREEN}✓${NC} Go dependencies installed"

echo -e "\n${YELLOW}Installing Node.js dependencies...${NC}"
cd web && npm install && cd ..
echo -e "${GREEN}✓${NC} Node.js dependencies installed"

# Setup database
echo -e "\n${YELLOW}Setting up database...${NC}\n"
./scripts/bootstrap-db.sh

# Build
echo -e "\n${YELLOW}Building binaries...${NC}"
make build
echo -e "${GREEN}✓${NC} Build complete"

# Success
echo -e "\n${GREEN}╔═══════════════════════════════════════╗${NC}"
echo -e "${GREEN}║      Setup Complete! 🎉               ║${NC}"
echo -e "${GREEN}╚═══════════════════════════════════════╝${NC}\n"

echo -e "${YELLOW}Quick Start:${NC}\n"
echo -e "  1. ${GREEN}make run-api${NC}         - Start API server"
echo -e "  2. ${GREEN}cd web && npm run dev${NC} - Start web frontend"
echo -e "  3. ${GREEN}http://localhost:3000${NC} - Open browser"
echo -e "  4. Login: ${GREEN}admin@localhost / changeme${NC}\n"
