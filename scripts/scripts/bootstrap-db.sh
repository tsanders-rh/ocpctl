#!/bin/bash
# Bootstrap database for ocpctl
set -e

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

echo -e "${GREEN}=== ocpctl Database Bootstrap ===${NC}\n"

# Load .env
if [ -f .env ]; then
    export $(cat .env | grep -v '^#' | xargs)
    echo -e "${GREEN}✓${NC} Loaded .env file"
else
    echo -e "${RED}✗${NC} .env file not found"
    exit 1
fi

# Extract DB name from DATABASE_URL
DB_NAME=$(echo $DATABASE_URL | sed -n 's|.*\/\([^?]*\).*|\1|p')
echo -e "\nDatabase: $DB_NAME"

# Check PostgreSQL
echo -e "\n${YELLOW}Checking PostgreSQL...${NC}"
if ! pg_isready > /dev/null 2>&1; then
    echo -e "${RED}✗${NC} PostgreSQL not running"
    exit 1
fi
echo -e "${GREEN}✓${NC} PostgreSQL is running"

# Create database if needed
if ! psql -lqt | cut -d \| -f 1 | grep -qw $DB_NAME; then
    echo -e "\n${YELLOW}Creating database...${NC}"
    createdb $DB_NAME
    echo -e "${GREEN}✓${NC} Database created"
else
    echo -e "${GREEN}✓${NC} Database exists"
fi

# Install goose if needed
if ! command -v goose &> /dev/null; then
    echo -e "\n${YELLOW}Installing goose...${NC}"
    go install github.com/pressly/goose/v3/cmd/goose@latest
    echo -e "${GREEN}✓${NC} goose installed"
fi

# Run migrations
echo -e "\n${YELLOW}Running migrations...${NC}"
goose -dir internal/store/migrations postgres "$DATABASE_URL" up
echo -e "${GREEN}✓${NC} Migrations completed"

# Verify admin user
ADMIN_COUNT=$(psql "$DATABASE_URL" -t -c "SELECT COUNT(*) FROM users WHERE email = 'admin@localhost';" 2>/dev/null | xargs || echo "0")
if [ "$ADMIN_COUNT" -eq "1" ]; then
    echo -e "${GREEN}✓${NC} Admin user exists"
else
    echo -e "${RED}✗${NC} Admin user not found"
fi

echo -e "\n${GREEN}=== Bootstrap Complete ===${NC}\n"
echo -e "Login: ${GREEN}admin@localhost${NC} / ${GREEN}changeme${NC}\n"
