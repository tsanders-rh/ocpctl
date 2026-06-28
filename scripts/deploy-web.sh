#!/bin/bash
# Web frontend deployment script for ocpctl
# Usage: ./deploy-web.sh [dev|production]
#
# Examples:
#   ./deploy-web.sh dev                    # Deploy to dev
#   ./deploy-web.sh production             # Deploy to production

set -e

ENVIRONMENT=${1:-production}

# Colors
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

echo -e "${BLUE}=== OCPCTL Web Frontend Deployment ===${NC}"
echo ""

# Validate environment
if [[ "$ENVIRONMENT" != "dev" && "$ENVIRONMENT" != "production" ]]; then
    echo -e "${RED}Error: Invalid environment '$ENVIRONMENT'${NC}"
    echo "Usage: $0 [dev|production]"
    exit 1
fi

# Environment-specific configuration
case $ENVIRONMENT in
  dev)
    SSH_HOST="54.167.79.11"
    SSH_USER="ubuntu"
    SSH_KEY="$HOME/.ssh/ocpctl-dev-key"
    DOMAIN="dev.ocpctl.mg.dog8code.com"
    CONFIG_SUFFIX="dev"
    echo -e "${YELLOW}Environment: DEV${NC}"
    ;;
  production)
    SSH_HOST="44.201.165.78"
    SSH_USER="ubuntu"
    SSH_KEY="$HOME/.ssh/ocpctl-production-key"
    DOMAIN="ocpctl.mg.dog8code.com"
    CONFIG_SUFFIX="production"
    echo -e "${YELLOW}Environment: PRODUCTION${NC}"
    ;;
esac

REMOTE_BASE="/opt/ocpctl"
WEB_DIR="web"

echo -e "${YELLOW}Domain: $DOMAIN${NC}"
echo ""

# Safety check for production
if [ "$ENVIRONMENT" = "production" ]; then
    echo -e "${RED}⚠️  WARNING: Deploying to PRODUCTION ⚠️${NC}"
    echo ""
    echo "This will:"
    echo "  - Deploy web frontend to production"
    echo "  - Restart ocpctl-web service (brief frontend downtime)"
    echo ""
    read -p "Are you sure you want to continue? (yes/no): " -r
    echo
    if [[ ! $REPLY =~ ^[Yy][Ee][Ss]$ ]]; then
        echo "Deployment cancelled."
        exit 1
    fi
fi

# Check if web directory exists
if [ ! -d "$WEB_DIR" ]; then
    echo -e "${RED}Error: Web directory '$WEB_DIR' not found${NC}"
    echo "Are you running this from the project root?"
    exit 1
fi

# Get version info
VERSION="v0.$(date +%Y%m%d).$(git rev-parse --short HEAD 2>/dev/null || echo 'unknown')"
COMMIT=$(git rev-parse HEAD 2>/dev/null || echo "unknown")
BUILD_TIME=$(date -u +"%Y-%m-%dT%H:%M:%SZ")

echo -e "${YELLOW}Version:    $VERSION${NC}"
echo -e "${YELLOW}Commit:     $COMMIT${NC}"
echo -e "${YELLOW}Build Time: $BUILD_TIME${NC}"
echo ""

# Check Node.js version
echo -e "${YELLOW}Checking Node.js version...${NC}"
NODE_VERSION=$(node --version 2>/dev/null || echo "none")
if [ "$NODE_VERSION" = "none" ]; then
    echo -e "${RED}Error: Node.js not found${NC}"
    echo "Please install Node.js 18+ to build the frontend"
    exit 1
fi
echo -e "${GREEN}✓ Node.js $NODE_VERSION${NC}"
echo ""

# Install dependencies
echo -e "${YELLOW}Installing dependencies...${NC}"
cd "$WEB_DIR"
npm install
echo -e "${GREEN}✓ Dependencies installed${NC}"
echo ""

# Run linting
echo -e "${YELLOW}Running linter...${NC}"
if npm run lint; then
    echo -e "${GREEN}✓ Linting passed${NC}"
else
    echo -e "${RED}✗ Linting failed${NC}"
    read -p "Continue anyway? (yes/no): " -r
    echo
    if [[ ! $REPLY =~ ^[Yy][Ee][Ss]$ ]]; then
        echo "Deployment cancelled."
        exit 1
    fi
fi
echo ""

# Build production bundle
echo -e "${YELLOW}Building production bundle...${NC}"
if npm run build; then
    echo -e "${GREEN}✓ Build successful${NC}"
else
    echo -e "${RED}✗ Build failed${NC}"
    exit 1
fi
echo ""

# Go back to project root
cd ..

# Create deployment package
echo -e "${YELLOW}Creating deployment package...${NC}"
PACKAGE_NAME="ocpctl-web-${VERSION}.tar.gz"

tar czf "/tmp/${PACKAGE_NAME}" \
    --exclude="$WEB_DIR/node_modules" \
    --exclude="$WEB_DIR/.next/cache" \
    --exclude="$WEB_DIR/.env.local" \
    --exclude="$WEB_DIR/.git" \
    "$WEB_DIR/"

echo -e "${GREEN}✓ Package created: /tmp/${PACKAGE_NAME}${NC}"
PACKAGE_SIZE=$(du -h "/tmp/${PACKAGE_NAME}" | cut -f1)
echo -e "${GREEN}✓ Package size: ${PACKAGE_SIZE}${NC}"
echo ""

# Upload package to server
echo -e "${YELLOW}Uploading package to $SSH_HOST...${NC}"
scp -i "$SSH_KEY" "/tmp/${PACKAGE_NAME}" "$SSH_USER@$SSH_HOST:/tmp/${PACKAGE_NAME}"
echo -e "${GREEN}✓ Package uploaded${NC}"
echo ""

# Deploy on server
echo -e "${YELLOW}Deploying on server...${NC}"

# Stop web service
echo -e "${YELLOW}  Stopping ocpctl-web service...${NC}"
ssh -i "$SSH_KEY" "$SSH_USER@$SSH_HOST" 'sudo systemctl stop ocpctl-web'
echo -e "${GREEN}✓ Service stopped${NC}"

# Backup current deployment
echo -e "${YELLOW}  Creating backup of current deployment...${NC}"
ssh -i "$SSH_KEY" "$SSH_USER@$SSH_HOST" "
    if [ -d ${REMOTE_BASE}/web ]; then
        sudo cp -r ${REMOTE_BASE}/web ${REMOTE_BASE}/web.backup.\$(date +%Y%m%d-%H%M%S)
        echo '  ✓ Backup created'
    else
        echo '  ℹ No existing deployment to backup'
    fi
"

# Extract new deployment
echo -e "${YELLOW}  Extracting new deployment...${NC}"
ssh -i "$SSH_KEY" "$SSH_USER@$SSH_HOST" "
    cd ${REMOTE_BASE}
    sudo tar xzf /tmp/${PACKAGE_NAME}
    sudo chown -R ocpctl:ocpctl ${REMOTE_BASE}/web
"
echo -e "${GREEN}✓ Files extracted${NC}"

# Install production dependencies on server
echo -e "${YELLOW}  Installing production dependencies...${NC}"
ssh -i "$SSH_KEY" "$SSH_USER@$SSH_HOST" "
    cd ${REMOTE_BASE}/web
    npm install --production --quiet
"
echo -e "${GREEN}✓ Dependencies installed${NC}"

# Verify web.env exists
echo -e "${YELLOW}  Verifying configuration...${NC}"
CONFIG_EXISTS=$(ssh -i "$SSH_KEY" "$SSH_USER@$SSH_HOST" "[ -f /etc/ocpctl/web.env ] && echo 'yes' || echo 'no'")
if [ "$CONFIG_EXISTS" = "yes" ]; then
    echo -e "${GREEN}✓ Configuration file exists${NC}"
else
    echo -e "${RED}✗ Warning: /etc/ocpctl/web.env not found${NC}"
    echo -e "${YELLOW}  You may need to create it manually${NC}"
fi

# Start web service
echo -e "${YELLOW}  Starting ocpctl-web service...${NC}"
ssh -i "$SSH_KEY" "$SSH_USER@$SSH_HOST" 'sudo systemctl start ocpctl-web'

# Wait for service to start
sleep 3

# Verify service is running
if ssh -i "$SSH_KEY" "$SSH_USER@$SSH_HOST" 'sudo systemctl is-active ocpctl-web' > /dev/null; then
    echo -e "${GREEN}✓ Web service is running${NC}"
else
    echo -e "${RED}✗ Web service failed to start${NC}"
    echo ""
    echo -e "${YELLOW}Showing last 20 lines of service logs:${NC}"
    ssh -i "$SSH_KEY" "$SSH_USER@$SSH_HOST" 'sudo journalctl -u ocpctl-web -n 20 --no-pager'
    exit 1
fi
echo ""

# Verify web frontend is responding
echo -e "${YELLOW}Verifying web frontend...${NC}"
HTTP_CODE=$(ssh -i "$SSH_KEY" "$SSH_USER@$SSH_HOST" 'curl -s -o /dev/null -w "%{http_code}" http://localhost:3000' || echo "000")
if [ "$HTTP_CODE" = "200" ] || [ "$HTTP_CODE" = "307" ]; then
    echo -e "${GREEN}✓ Web frontend responding (HTTP $HTTP_CODE)${NC}"
else
    echo -e "${RED}✗ Web frontend not responding (HTTP $HTTP_CODE)${NC}"
    echo ""
    echo -e "${YELLOW}Showing last 20 lines of service logs:${NC}"
    ssh -i "$SSH_KEY" "$SSH_USER@$SSH_HOST" 'sudo journalctl -u ocpctl-web -n 20 --no-pager'
    exit 1
fi
echo ""

# Test via nginx (public URL)
echo -e "${YELLOW}Testing public URL...${NC}"
PUBLIC_HTTP_CODE=$(curl -s -o /dev/null -w "%{http_code}" "https://$DOMAIN" || echo "000")
if [ "$PUBLIC_HTTP_CODE" = "200" ] || [ "$PUBLIC_HTTP_CODE" = "307" ]; then
    echo -e "${GREEN}✓ Public URL responding (HTTP $PUBLIC_HTTP_CODE)${NC}"
else
    echo -e "${YELLOW}⚠ Public URL returned HTTP $PUBLIC_HTTP_CODE${NC}"
    echo -e "${YELLOW}  This may be normal if nginx redirects to /login${NC}"
fi
echo ""

# Cleanup
echo -e "${YELLOW}Cleaning up...${NC}"
rm -f "/tmp/${PACKAGE_NAME}"
ssh -i "$SSH_KEY" "$SSH_USER@$SSH_HOST" "rm -f /tmp/${PACKAGE_NAME}"
echo -e "${GREEN}✓ Cleanup complete${NC}"
echo ""

# Deployment summary
echo -e "${GREEN}=== Web Frontend Deployment Complete ===${NC}"
echo ""
echo -e "${BLUE}Deployed Version: ${VERSION}${NC}"
echo -e "${BLUE}Commit: ${COMMIT}${NC}"
echo -e "${BLUE}Environment: ${ENVIRONMENT}${NC}"
echo ""
echo "Access the web UI:"
echo -e "  ${BLUE}https://$DOMAIN${NC}"
echo ""
echo "Verify deployment:"
echo "  curl https://$DOMAIN"
echo "  ssh -i $SSH_KEY $SSH_USER@$SSH_HOST 'sudo systemctl status ocpctl-web'"
echo ""
echo "View logs:"
echo "  ssh -i $SSH_KEY $SSH_USER@$SSH_HOST 'sudo journalctl -u ocpctl-web -f'"
echo ""
echo "Rollback if needed:"
echo "  ssh -i $SSH_KEY $SSH_USER@$SSH_HOST 'sudo ls -d ${REMOTE_BASE}/web.backup.*'"
echo "  ssh -i $SSH_KEY $SSH_USER@$SSH_HOST 'sudo systemctl stop ocpctl-web && sudo mv ${REMOTE_BASE}/web.backup.YYYYMMDD-HHMMSS ${REMOTE_BASE}/web && sudo systemctl start ocpctl-web'"
echo ""
