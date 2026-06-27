#!/bin/bash
# Initialize OCPCTL dev database
# Usage: ./init-dev-database.sh
#
# This script:
# - Creates application database user
# - Runs database migrations
# - Optionally seeds test data

set -e

# Colors
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m'

echo -e "${YELLOW}=== OCPCTL Dev Database Initialization ===${NC}"
echo ""

# Check if terraform output is available
if [ ! -d "terraform/dev" ]; then
  echo -e "${RED}Error: terraform/dev directory not found${NC}"
  echo "Run this script from the repository root"
  exit 1
fi

cd terraform/dev

# Get database connection details from Terraform
echo -e "${YELLOW}Retrieving database details from Terraform...${NC}"

if [ ! -f "terraform.tfstate" ]; then
  echo -e "${RED}Error: Terraform state not found${NC}"
  echo "Run 'terraform apply' first to create infrastructure"
  exit 1
fi

RDS_ADDRESS=$(terraform output -raw rds_address)
DB_MASTER_USER=$(terraform output -json | jq -r '.db_username.value')
DB_MASTER_PASS=$(terraform output -json | jq -r '.db_password.value')
DB_NAME=$(terraform output -json | jq -r '.db_name.value')

# Generate app user password
APP_USER_PASS=$(openssl rand -base64 24 | tr -d '\n')

echo -e "${GREEN}✓ Database endpoint: $RDS_ADDRESS${NC}"
echo ""

cd ../..

# Create application user and grant privileges
echo -e "${YELLOW}Step 1: Creating application database user...${NC}"

MASTER_URL="postgresql://$DB_MASTER_USER:$DB_MASTER_PASS@$RDS_ADDRESS:5432/postgres?sslmode=require"
APP_DB_URL="postgresql://$DB_MASTER_USER:$DB_MASTER_PASS@$RDS_ADDRESS:5432/$DB_NAME?sslmode=require"

# Create application user (if doesn't exist)
psql "$MASTER_URL" << EOF
DO \$\$
BEGIN
  IF NOT EXISTS (SELECT FROM pg_catalog.pg_roles WHERE rolname = 'ocpctl_dev_user') THEN
    CREATE USER ocpctl_dev_user WITH PASSWORD '$APP_USER_PASS';
  END IF;
END
\$\$;

-- Grant privileges on database
GRANT ALL PRIVILEGES ON DATABASE $DB_NAME TO ocpctl_dev_user;

-- Connect to app database and grant schema privileges
\\c $DB_NAME
GRANT ALL PRIVILEGES ON SCHEMA public TO ocpctl_dev_user;
GRANT ALL PRIVILEGES ON ALL TABLES IN SCHEMA public TO ocpctl_dev_user;
GRANT ALL PRIVILEGES ON ALL SEQUENCES IN SCHEMA public TO ocpctl_dev_user;

-- Grant future object privileges
ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT ALL ON TABLES TO ocpctl_dev_user;
ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT ALL ON SEQUENCES TO ocpctl_dev_user;
EOF

echo -e "${GREEN}✓ Application user created${NC}"
echo ""

# Run migrations
echo -e "${YELLOW}Step 2: Running database migrations...${NC}"

# Build migration binary if needed
if [ ! -f "bin/ocpctl-api" ]; then
  echo "Building ocpctl-api for migrations..."
  make build
fi

# Run migrations using the master user (has full privileges)
export DATABASE_URL="$APP_DB_URL"
./bin/ocpctl-api migrate up

echo -e "${GREEN}✓ Migrations completed${NC}"
echo ""

# Seed test data
echo -e "${YELLOW}Step 3: Seeding test data (optional)...${NC}"
read -p "Do you want to seed test data? (yes/no): " -r
echo

if [[ $REPLY =~ ^[Yy][Ee][Ss]$ ]]; then
  psql "$APP_DB_URL" << 'EOF'
-- Create dev admin user
INSERT INTO users (id, email, name, role, created_at, updated_at)
VALUES (
  gen_random_uuid(),
  'dev-admin@ocpctl.local',
  'Dev Admin',
  'admin',
  NOW(),
  NOW()
) ON CONFLICT (email) DO NOTHING;

-- Create dev test user
INSERT INTO users (id, email, name, role, created_at, updated_at)
VALUES (
  gen_random_uuid(),
  'dev-user@ocpctl.local',
  'Dev User',
  'user',
  NOW(),
  NOW()
) ON CONFLICT (email) DO NOTHING;

-- Display created users
SELECT id, email, name, role FROM users WHERE email LIKE '%ocpctl.local';
EOF

  echo -e "${GREEN}✓ Test data seeded${NC}"
else
  echo "Skipping test data"
fi

echo ""
echo -e "${GREEN}=== Database Initialization Complete ===${NC}"
echo ""
echo "Application database URL (for config files):"
echo "DATABASE_URL=postgresql://ocpctl_dev_user:$APP_USER_PASS@$RDS_ADDRESS:5432/$DB_NAME?sslmode=require"
echo ""
echo -e "${YELLOW}IMPORTANT: Save this connection string in both config/api.env.dev and config/worker.env.dev${NC}"
echo ""
echo "Next steps:"
echo "1. Update config/api.env.dev with DATABASE_URL"
echo "2. Update config/worker.env.dev with DATABASE_URL"
echo "3. Deploy services: ./scripts/deploy-env.sh dev"
echo ""
