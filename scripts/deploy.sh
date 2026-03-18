#!/bin/bash
# Deployment script for ocpctl services with versioned releases
# Usage: ./deploy.sh [version]

set -e

# Configuration
WORKER_HOSTS=("52.90.135.148")  # API server also runs a worker (hybrid approach)
API_HOST="52.90.135.148"
SSH_KEY="$HOME/.ssh/ocpctl-test-key.pem"
REMOTE_BASE="/opt/ocpctl"

# Colors
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

echo -e "${BLUE}=== OCPCTL Versioned Deployment ===${NC}"
echo ""

# Get version from argument or generate from git
if [ -n "$1" ]; then
    VERSION="$1"
else
    # Try to get latest git tag, fallback to commit hash
    if git describe --tags --exact-match 2>/dev/null; then
        VERSION=$(git describe --tags --exact-match)
    else
        VERSION="v0.$(date +%Y%m%d).$(git rev-parse --short HEAD)"
    fi
fi

COMMIT=$(git rev-parse HEAD)
BUILD_TIME=$(date -u +"%Y-%m-%dT%H:%M:%SZ")

echo -e "${YELLOW}Version:    $VERSION${NC}"
echo -e "${YELLOW}Commit:     $COMMIT${NC}"
echo -e "${YELLOW}Build Time: $BUILD_TIME${NC}"
echo ""

# Build binaries with version metadata
echo -e "${YELLOW}Building binaries with version metadata...${NC}"
cd "$(dirname "$0")/.."

LDFLAGS="-X main.Version=${VERSION} -X main.Commit=${COMMIT} -X main.BuildTime=${BUILD_TIME}"

GOOS=linux GOARCH=amd64 go build -ldflags "$LDFLAGS" -o bin/ocpctl-api-${VERSION} ./cmd/api
GOOS=linux GOARCH=amd64 go build -ldflags "$LDFLAGS" -o bin/ocpctl-worker-${VERSION} ./cmd/worker

echo -e "${GREEN}✓ Built ocpctl-api-${VERSION}${NC}"
echo -e "${GREEN}✓ Built ocpctl-worker-${VERSION}${NC}"
echo ""

# Verify versions in binaries
echo -e "${YELLOW}Verifying version information in binaries...${NC}"
strings bin/ocpctl-api-${VERSION} | grep -q "${VERSION}" && echo -e "${GREEN}✓ API version embedded correctly${NC}" || echo -e "${RED}✗ API version NOT found in binary${NC}"
strings bin/ocpctl-worker-${VERSION} | grep -q "${VERSION}" && echo -e "${GREEN}✓ Worker version embedded correctly${NC}" || echo -e "${RED}✗ Worker version NOT found in binary${NC}"
echo ""

# Upload to S3 for autoscaling workers
echo -e "${YELLOW}Uploading binaries to S3 for autoscaling workers...${NC}"
S3_BUCKET="s3://ocpctl-binaries"

aws s3 cp bin/ocpctl-worker-${VERSION} ${S3_BUCKET}/releases/${VERSION}/ocpctl-worker
echo "${VERSION}" | aws s3 cp - ${S3_BUCKET}/LATEST

echo -e "${GREEN}✓ Uploaded to ${S3_BUCKET}/releases/${VERSION}/${NC}"
echo -e "${GREEN}✓ Updated LATEST pointer to ${VERSION}${NC}"
echo ""

# Upload bootstrap artifacts to S3
echo -e "${YELLOW}Uploading bootstrap artifacts to S3...${NC}"

aws s3 cp scripts/bootstrap-worker.sh ${S3_BUCKET}/scripts/bootstrap-worker.sh
echo -e "${GREEN}✓ Uploaded bootstrap-worker.sh${NC}"

aws s3 cp scripts/ensure-installers.sh ${S3_BUCKET}/scripts/ensure-installers.sh
echo -e "${GREEN}✓ Uploaded ensure-installers.sh${NC}"

aws s3 cp scripts/ocpctl-worker.service ${S3_BUCKET}/scripts/ocpctl-worker.service
echo -e "${GREEN}✓ Uploaded ocpctl-worker.service${NC}"

if [ -f config/worker.env ]; then
    aws s3 cp config/worker.env ${S3_BUCKET}/config/worker.env
    echo -e "${GREEN}✓ Uploaded worker.env${NC}"
else
    echo -e "${YELLOW}⚠ worker.env not found, skipping${NC}"
fi

aws s3 sync manifests/ ${S3_BUCKET}/manifests/ --delete
echo -e "${GREEN}✓ Synced manifests directory${NC}"

aws s3 sync internal/profile/definitions/ ${S3_BUCKET}/profiles/ --delete
echo -e "${GREEN}✓ Synced profiles directory${NC}"

echo -e "${GREEN}✓ All bootstrap artifacts uploaded to S3${NC}"
echo ""

# Deploy API server
echo -e "${YELLOW}Deploying API server to $API_HOST...${NC}"

# Create versioned directory
ssh -i "$SSH_KEY" ec2-user@$API_HOST "sudo mkdir -p ${REMOTE_BASE}/releases/${VERSION}"

# Copy binary
scp -i "$SSH_KEY" bin/ocpctl-api-${VERSION} ec2-user@$API_HOST:/tmp/ocpctl-api-${VERSION}
ssh -i "$SSH_KEY" ec2-user@$API_HOST "sudo install -m 755 /tmp/ocpctl-api-${VERSION} ${REMOTE_BASE}/releases/${VERSION}/ocpctl-api && rm /tmp/ocpctl-api-${VERSION}"

# Deploy profiles directory
echo -e "${YELLOW}  Deploying profiles directory...${NC}"
ssh -i "$SSH_KEY" ec2-user@$API_HOST "mkdir -p /tmp/profiles && sudo mkdir -p ${REMOTE_BASE}/profiles"
scp -i "$SSH_KEY" -r internal/profile/definitions/* ec2-user@$API_HOST:/tmp/profiles/
ssh -i "$SSH_KEY" ec2-user@$API_HOST "sudo cp -r /tmp/profiles/* ${REMOTE_BASE}/profiles/ && sudo chown -R ocpctl:ocpctl ${REMOTE_BASE}/profiles && rm -rf /tmp/profiles"

# Stop service
ssh -i "$SSH_KEY" ec2-user@$API_HOST 'sudo systemctl stop ocpctl-api'

# Update symlink atomically
ssh -i "$SSH_KEY" ec2-user@$API_HOST "sudo ln -snf ${REMOTE_BASE}/releases/${VERSION} ${REMOTE_BASE}/current"

# Start service
ssh -i "$SSH_KEY" ec2-user@$API_HOST 'sudo systemctl start ocpctl-api'

# Wait for service to start
sleep 3

# Verify service is running
if ssh -i "$SSH_KEY" ec2-user@$API_HOST 'sudo systemctl is-active ocpctl-api' > /dev/null; then
    echo -e "${GREEN}✓ API server is running${NC}"

    # Verify version endpoint
    DEPLOYED_VERSION=$(ssh -i "$SSH_KEY" ec2-user@$API_HOST 'curl -s http://localhost:8080/version' | jq -r '.version')
    if [ "$DEPLOYED_VERSION" = "$VERSION" ]; then
        echo -e "${GREEN}✓ API version verified: $DEPLOYED_VERSION${NC}"
    else
        echo -e "${RED}✗ API version mismatch! Expected: $VERSION, Got: $DEPLOYED_VERSION${NC}"
        exit 1
    fi
else
    echo -e "${RED}✗ API server failed to start${NC}"
    exit 1
fi

echo ""

# Deploy workers
for host in "${WORKER_HOSTS[@]}"; do
    echo -e "${YELLOW}Deploying worker to $host...${NC}"

    # Create versioned directory
    ssh -i "$SSH_KEY" ec2-user@$host "sudo mkdir -p ${REMOTE_BASE}/releases/${VERSION}"

    # Deploy ensure-installers script
    scp -i "$SSH_KEY" scripts/ensure-installers.sh ec2-user@$host:/tmp/ensure-installers.sh
    ssh -i "$SSH_KEY" ec2-user@$host "sudo mkdir -p ${REMOTE_BASE}/scripts && sudo install -m 755 /tmp/ensure-installers.sh ${REMOTE_BASE}/scripts/ensure-installers.sh && rm /tmp/ensure-installers.sh"

    # Deploy manifests directory (for post-deployment scripts)
    echo -e "${YELLOW}  Deploying manifests directory...${NC}"
    ssh -i "$SSH_KEY" ec2-user@$host "mkdir -p /tmp/manifests && sudo mkdir -p ${REMOTE_BASE}/manifests"
    scp -i "$SSH_KEY" -r manifests/* ec2-user@$host:/tmp/manifests/
    ssh -i "$SSH_KEY" ec2-user@$host "sudo cp -r /tmp/manifests/* ${REMOTE_BASE}/manifests/ && sudo chmod -R 755 ${REMOTE_BASE}/manifests && rm -rf /tmp/manifests"

    # Deploy profiles directory
    echo -e "${YELLOW}  Deploying profiles directory...${NC}"
    ssh -i "$SSH_KEY" ec2-user@$host "mkdir -p /tmp/profiles && sudo mkdir -p ${REMOTE_BASE}/profiles"
    scp -i "$SSH_KEY" -r internal/profile/definitions/* ec2-user@$host:/tmp/profiles/
    ssh -i "$SSH_KEY" ec2-user@$host "sudo cp -r /tmp/profiles/* ${REMOTE_BASE}/profiles/ && sudo chown -R ocpctl:ocpctl ${REMOTE_BASE}/profiles && rm -rf /tmp/profiles"

    # Deploy systemd service file
    scp -i "$SSH_KEY" deploy/systemd/ocpctl-worker.service ec2-user@$host:/tmp/ocpctl-worker.service
    ssh -i "$SSH_KEY" ec2-user@$host "sudo install -m 644 /tmp/ocpctl-worker.service /etc/systemd/system/ocpctl-worker.service && rm /tmp/ocpctl-worker.service && sudo systemctl daemon-reload"

    # Copy binary
    scp -i "$SSH_KEY" bin/ocpctl-worker-${VERSION} ec2-user@$host:/tmp/ocpctl-worker-${VERSION}
    ssh -i "$SSH_KEY" ec2-user@$host "sudo install -m 755 /tmp/ocpctl-worker-${VERSION} ${REMOTE_BASE}/releases/${VERSION}/ocpctl-worker && rm /tmp/ocpctl-worker-${VERSION}"

    # Stop service
    ssh -i "$SSH_KEY" ec2-user@$host 'sudo systemctl stop ocpctl-worker'

    # Update symlink atomically
    ssh -i "$SSH_KEY" ec2-user@$host "sudo ln -snf ${REMOTE_BASE}/releases/${VERSION} ${REMOTE_BASE}/current"

    # Start service
    ssh -i "$SSH_KEY" ec2-user@$host 'sudo systemctl start ocpctl-worker'

    # Wait for service to start
    sleep 3

    # Verify service is running
    if ssh -i "$SSH_KEY" ec2-user@$host 'sudo systemctl is-active ocpctl-worker' > /dev/null; then
        echo -e "${GREEN}✓ Worker on $host is running${NC}"

        # Verify version endpoint (assuming worker health check is on port 8081)
        DEPLOYED_VERSION=$(ssh -i "$SSH_KEY" ec2-user@$host 'curl -s http://localhost:8081/version' | jq -r '.version')
        if [ "$DEPLOYED_VERSION" = "$VERSION" ]; then
            echo -e "${GREEN}✓ Worker version verified: $DEPLOYED_VERSION${NC}"
        else
            echo -e "${RED}✗ Worker version mismatch! Expected: $VERSION, Got: $DEPLOYED_VERSION${NC}"
        fi
    else
        echo -e "${RED}✗ Worker on $host failed to start${NC}"
    fi

    echo ""
done

echo -e "${GREEN}=== Deployment Complete ===${NC}"
echo ""
echo -e "${BLUE}Deployed Version: ${VERSION}${NC}"
echo -e "${BLUE}Commit: ${COMMIT}${NC}"
echo ""
echo "Verify deployment:"
echo "  API:    curl http://ocpctl.mg.dog8code.com/version"
echo "  Worker: ssh -i $SSH_KEY ec2-user@52.90.135.148 'curl -s http://localhost:8081/version'"
echo ""
echo "Rollback to previous version:"
echo "  ssh -i $SSH_KEY ec2-user@$API_HOST 'sudo ls -d ${REMOTE_BASE}/releases/*'"
echo "  ./deploy.sh <previous-version>"
