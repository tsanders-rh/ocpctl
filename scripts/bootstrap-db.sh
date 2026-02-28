#!/bin/bash

# Bootstrap database for ocpctl local development
# This script:
# 1. Creates the database if it doesn't exist
# 2. Runs all migrations
# 3. Seeds initial admin user

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

echo -e "${GREEN}=== ocpctl Database Bootstrap ===${NC}\n"

# Load environment variables
if [ -f .env ]; then
    export $(cat .env | grep -v '^#' | xargs)
    echo -e "${GREEN}✓${NC} Loaded .env file"
else
    echo -e "${RED}✗${NC} .env file not found. Please create one from .env.example"
    exit 1
fi

# Check if DATABASE_URL is set
if [ -z "$DATABASE_URL" ]; then
    echo -e "${RED}✗${NC} DATABASE_URL not set in .env"
    exit 1
fi

# Extract database details from DATABASE_URL
# Format: postgresql://user:pass@host:port/dbname?options
DB_NAME=$(echo $DATABASE_URL | sed -n 's|.*\/\([^?]*\).*|\1|p')
DB_HOST=$(echo $DATABASE_URL | sed -n 's|.*@\([^:]*\):.*|\1|p' || echo "localhost")
DB_PORT=$(echo $DATABASE_URL | sed -n 's|.*:\([0-9]*\)\/.*|\1|p' || echo "5432")
DB_USER=$(echo $DATABASE_URL | sed -n 's|.*://\([^:]*\):.*|\1|p' || echo "$(whoami)")

# Handle simple DATABASE_URL without user:pass
if [ -z "$DB_HOST" ]; then
    DB_HOST="localhost"
fi
if [ -z "$DB_PORT" ]; then
    DB_PORT="5432"
fi
if [ -z "$DB_USER" ]; then
    DB_USER="$(whoami)"
fi

echo -e "\n${YELLOW}Database Configuration:${NC}"
echo -e "  Host: $DB_HOST"
echo -e "  Port: $DB_PORT"
echo -e "  User: $DB_USER"
echo -e "  Database: $DB_NAME"

# Check if PostgreSQL is running
echo -e "\n${YELLOW}Checking PostgreSQL connection...${NC}"
if ! pg_isready -h $DB_HOST -p $DB_PORT > /dev/null 2>&1; then
    echo -e "${RED}✗${NC} PostgreSQL is not running or not accessible"
    echo -e "  Start PostgreSQL with: ${YELLOW}brew services start postgresql${NC} (macOS)"
    echo -e "  Or: ${YELLOW}sudo systemctl start postgresql${NC} (Linux)"
    exit 1
fi
echo -e "${GREEN}✓${NC} PostgreSQL is running"

# Check if database exists, create if not
echo -e "\n${YELLOW}Checking if database exists...${NC}"
if psql -h $DB_HOST -p $DB_PORT -U $DB_USER -lqt 2>/dev/null | cut -d \| -f 1 | grep -qw $DB_NAME; then
    echo -e "${GREEN}✓${NC} Database '$DB_NAME' already exists"
else
    echo -e "${YELLOW}⚠${NC} Database '$DB_NAME' does not exist, creating..."
    createdb -h $DB_HOST -p $DB_PORT -U $DB_USER $DB_NAME 2>/dev/null || createdb $DB_NAME
    echo -e "${GREEN}✓${NC} Database '$DB_NAME' created"
fi

# Check if goose is installed
echo -e "\n${YELLOW}Checking for goose migration tool...${NC}"
if ! command -v goose &> /dev/null; then
    echo -e "${RED}✗${NC} goose not found. Installing..."
    go install github.com/pressly/goose/v3/cmd/goose@latest
    echo -e "${GREEN}✓${NC} goose installed"
else
    echo -e "${GREEN}✓${NC} goose is installed"
fi

# Run migrations
echo -e "\n${YELLOW}Running database migrations...${NC}"
goose -dir internal/store/migrations postgres "$DATABASE_URL" up
echo -e "${GREEN}✓${NC} Migrations completed"

# Check migration status
echo -e "\n${YELLOW}Migration Status:${NC}"
goose -dir internal/store/migrations postgres "$DATABASE_URL" status

# Verify admin user
echo -e "\n${YELLOW}Verifying admin user...${NC}"
ADMIN_COUNT=$(psql "$DATABASE_URL" -t -c "SELECT COUNT(*) FROM users WHERE email = 'admin@localhost';" 2>/dev/null | xargs || echo "0")
if [ "$ADMIN_COUNT" -eq "1" ]; then
    echo -e "${GREEN}✓${NC} Admin user exists (admin@localhost)"
else
    echo -e "${RED}✗${NC} Admin user not found"
    echo -e "${YELLOW}⚠${NC} Run migration 00004 to seed admin user"
fi

# Display summary
echo -e "\n${GREEN}=== Bootstrap Complete ===${NC}"
echo -e "\n${YELLOW}Next Steps:${NC}"
echo -e "  1. Start the API server:"
echo -e "     ${GREEN}make run-api${NC}"
echo -e "\n  2. Start the web frontend:"
echo -e "     ${GREEN}cd web && npm run dev${NC}"
echo -e "\n  3. Open your browser:"
echo -e "     ${GREEN}http://localhost:3000${NC}"
echo -e "\n  4. Login with:"
echo -e "     Email: ${GREEN}admin@localhost${NC}"
echo -e "     Password: ${GREEN}changeme${NC}"
echo -e "\n${YELLOW}⚠ Remember to change the admin password in production!${NC}\n"
