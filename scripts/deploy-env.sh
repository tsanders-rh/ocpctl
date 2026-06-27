#!/bin/bash
# Multi-environment deployment script for ocpctl
# Usage: ./deploy-env.sh [dev|production] [version]
#
# Examples:
#   ./deploy-env.sh dev                    # Deploy latest to dev
#   ./deploy-env.sh production             # Deploy latest to production
#   ./deploy-env.sh dev v0.20260614.abc    # Deploy specific version to dev

set -e

ENVIRONMENT=${1:-production}
VERSION=${2:-}

# Colors
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

echo -e "${BLUE}=== OCPCTL Multi-Environment Deployment ===${NC}"
echo ""

# Validate environment
if [[ "$ENVIRONMENT" != "dev" && "$ENVIRONMENT" != "production" ]]; then
    echo -e "${RED}Error: Invalid environment '$ENVIRONMENT'${NC}"
    echo "Usage: $0 [dev|production] [version]"
    exit 1
fi

# Environment-specific configuration
case $ENVIRONMENT in
  dev)
    API_HOST="54.167.79.11"
    WORKER_HOSTS=("54.167.79.11")
    SSH_USER="ubuntu"
    SSH_KEY="$HOME/.ssh/ocpctl-dev-key"
    S3_BUCKET="s3://ocpctl-dev-binaries"
    S3_ARTIFACTS_BUCKET="s3://ocpctl-dev-artifacts"
    DOMAIN="dev.ocpctl.mg.dog8code.com"
    AUTOSCALE_TAG="ocpctl-dev-worker"
    CONFIG_SUFFIX="dev"
    echo -e "${YELLOW}Environment: DEV${NC}"
    ;;
  production)
    API_HOST="44.201.165.78"
    WORKER_HOSTS=("44.201.165.78")
    SSH_USER="ubuntu"
    SSH_KEY="$HOME/.ssh/ocpctl-production-key"
    S3_BUCKET="s3://ocpctl-binaries"
    S3_ARTIFACTS_BUCKET="s3://ocpctl-artifacts"
    DOMAIN="ocpctl.mg.dog8code.com"
    AUTOSCALE_TAG="ocpctl-worker"
    CONFIG_SUFFIX="production"
    echo -e "${YELLOW}Environment: PRODUCTION${NC}"
    ;;
esac

REMOTE_BASE="/opt/ocpctl"

# Get version from argument or generate from git
if [ -n "$VERSION" ]; then
    echo -e "${YELLOW}Using specified version: $VERSION${NC}"
else
    # Try to get latest git tag, fallback to commit hash
    if git describe --tags --exact-match 2>/dev/null; then
        VERSION=$(git describe --tags --exact-match)
    else
        VERSION="v0.$(date +%Y%m%d).$(git rev-parse --short HEAD)"
    fi
fi

COMMIT=$(git rev-parse HEAD 2>/dev/null || echo "unknown")
BUILD_TIME=$(date -u +"%Y-%m-%dT%H:%M:%SZ")

echo -e "${YELLOW}Version:    $VERSION${NC}"
echo -e "${YELLOW}Commit:     $COMMIT${NC}"
echo -e "${YELLOW}Build Time: $BUILD_TIME${NC}"
echo ""

# Safety check for production
if [ "$ENVIRONMENT" = "production" ]; then
    echo -e "${RED}⚠️  WARNING: Deploying to PRODUCTION ⚠️${NC}"
    echo ""
    echo "This will:"
    echo "  - Deploy version $VERSION to production"
    echo "  - Restart API and worker services (brief downtime)"
    echo "  - Terminate autoscale workers"
    echo ""
    read -p "Are you sure you want to continue? (yes/no): " -r
    echo
    if [[ ! $REPLY =~ ^[Yy][Ee][Ss]$ ]]; then
        echo "Deployment cancelled."
        exit 1
    fi
fi

# Check if environment-specific config files exist
if [ ! -f "config/api.env.$CONFIG_SUFFIX" ]; then
    echo -e "${RED}Error: config/api.env.$CONFIG_SUFFIX not found${NC}"
    echo "Please create environment-specific config file"
    exit 1
fi

if [ ! -f "config/worker.env.$CONFIG_SUFFIX" ]; then
    echo -e "${RED}Error: config/worker.env.$CONFIG_SUFFIX not found${NC}"
    echo "Please create environment-specific config file"
    exit 1
fi

# Build binaries with version metadata
echo -e "${YELLOW}Building binaries with version metadata...${NC}"
cd "$(dirname "$0")/.."

LDFLAGS="-X main.Version=${VERSION} -X main.Commit=${COMMIT} -X main.BuildTime=${BUILD_TIME} -X main.Environment=${ENVIRONMENT}"

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
echo -e "${YELLOW}Uploading binaries to S3 ($S3_BUCKET)...${NC}"

# Upload versioned binary
aws s3 cp bin/ocpctl-worker-${VERSION} ${S3_BUCKET}/releases/${VERSION}/ocpctl-worker
echo "${VERSION}" | aws s3 cp - ${S3_BUCKET}/LATEST

# Also copy to stable path for autoscaling workers
aws s3 cp bin/ocpctl-worker-${VERSION} ${S3_BUCKET}/binaries/ocpctl-worker

echo -e "${GREEN}✓ Uploaded to ${S3_BUCKET}/releases/${VERSION}/${NC}"
echo -e "${GREEN}✓ Updated LATEST pointer to ${VERSION}${NC}"
echo -e "${GREEN}✓ Updated stable path ${S3_BUCKET}/binaries/ocpctl-worker${NC}"
echo ""

# Upload bootstrap artifacts to S3
echo -e "${YELLOW}Uploading bootstrap artifacts to S3...${NC}"

aws s3 cp scripts/bootstrap-worker.sh ${S3_BUCKET}/scripts/bootstrap-worker.sh
echo -e "${GREEN}✓ Uploaded bootstrap-worker.sh${NC}"

aws s3 cp scripts/ensure-installers.sh ${S3_BUCKET}/scripts/ensure-installers.sh
echo -e "${GREEN}✓ Uploaded ensure-installers.sh${NC}"

aws s3 cp scripts/download-specific-version.sh ${S3_BUCKET}/scripts/download-specific-version.sh
echo -e "${GREEN}✓ Uploaded download-specific-version.sh${NC}"

aws s3 cp scripts/ocpctl-worker.service ${S3_BUCKET}/scripts/ocpctl-worker.service
echo -e "${GREEN}✓ Uploaded ocpctl-worker.service${NC}"

# Upload environment-specific config
aws s3 cp config/worker.env.$CONFIG_SUFFIX ${S3_BUCKET}/config/worker.env
echo -e "${GREEN}✓ Uploaded worker.env${NC}"

aws s3 sync manifests/ ${S3_BUCKET}/manifests/ --delete
echo -e "${GREEN}✓ Synced manifests directory${NC}"

aws s3 sync internal/profile/definitions/ ${S3_BUCKET}/profiles/ --delete
echo -e "${GREEN}✓ Synced profiles directory${NC}"

aws s3 sync internal/addon/definitions/ ${S3_BUCKET}/addons/ --delete
echo -e "${GREEN}✓ Synced add-ons directory${NC}"

echo -e "${GREEN}✓ All bootstrap artifacts uploaded to S3${NC}"
echo ""

# Terminate autoscale workers to force them to pull latest version
echo -e "${YELLOW}Terminating autoscale workers to trigger refresh...${NC}"

# Find all running autoscale worker instances (tagged with environment-specific name)
AUTOSCALE_INSTANCES=$(aws ec2 describe-instances \
    --filters "Name=tag:Name,Values=$AUTOSCALE_TAG" \
              "Name=instance-state-name,Values=running" \
    --query 'Reservations[].Instances[].InstanceId' \
    --output text)

if [ -z "$AUTOSCALE_INSTANCES" ]; then
    echo -e "${YELLOW}  No autoscale workers found${NC}"
else
    echo -e "${YELLOW}  Found autoscale workers: $AUTOSCALE_INSTANCES${NC}"

    for instance_id in $AUTOSCALE_INSTANCES; do
        echo -e "${YELLOW}  Terminating $instance_id...${NC}"
        aws ec2 terminate-instances --instance-ids "$instance_id" > /dev/null
    done

    echo -e "${GREEN}✓ Terminated autoscale workers (ASG will launch replacements with new version)${NC}"
fi

echo ""

# Deploy API server
echo -e "${YELLOW}Deploying API server to $API_HOST ($ENVIRONMENT)...${NC}"

# Create versioned directory
ssh -i "$SSH_KEY" $SSH_USER@$API_HOST "sudo mkdir -p ${REMOTE_BASE}/releases/${VERSION}"

# Copy binary
scp -i "$SSH_KEY" bin/ocpctl-api-${VERSION} $SSH_USER@$API_HOST:/tmp/ocpctl-api-${VERSION}
ssh -i "$SSH_KEY" $SSH_USER@$API_HOST "sudo install -m 755 /tmp/ocpctl-api-${VERSION} ${REMOTE_BASE}/releases/${VERSION}/ocpctl-api && rm /tmp/ocpctl-api-${VERSION}"

# Deploy profiles directory
echo -e "${YELLOW}  Deploying profiles directory...${NC}"
ssh -i "$SSH_KEY" $SSH_USER@$API_HOST "mkdir -p /tmp/profiles && sudo mkdir -p ${REMOTE_BASE}/profiles"
scp -i "$SSH_KEY" -r internal/profile/definitions/* $SSH_USER@$API_HOST:/tmp/profiles/
ssh -i "$SSH_KEY" $SSH_USER@$API_HOST "sudo cp -r /tmp/profiles/* ${REMOTE_BASE}/profiles/ && sudo chown -R ocpctl:ocpctl ${REMOTE_BASE}/profiles && rm -rf /tmp/profiles"

# Deploy add-ons directory
echo -e "${YELLOW}  Deploying add-ons directory...${NC}"
ssh -i "$SSH_KEY" $SSH_USER@$API_HOST "mkdir -p /tmp/addons && sudo mkdir -p ${REMOTE_BASE}/addons"
scp -i "$SSH_KEY" -r internal/addon/definitions/* $SSH_USER@$API_HOST:/tmp/addons/
ssh -i "$SSH_KEY" $SSH_USER@$API_HOST "sudo cp -r /tmp/addons/* ${REMOTE_BASE}/addons/ && sudo chown -R ocpctl:ocpctl ${REMOTE_BASE}/addons && rm -rf /tmp/addons"

# Deploy environment-specific API config
echo -e "${YELLOW}  Deploying api.env configuration...${NC}"
scp -i "$SSH_KEY" config/api.env.$CONFIG_SUFFIX $SSH_USER@$API_HOST:/tmp/api.env
ssh -i "$SSH_KEY" $SSH_USER@$API_HOST "sudo install -m 600 /tmp/api.env /etc/ocpctl/api.env && sudo chown ocpctl:ocpctl /etc/ocpctl/api.env && rm /tmp/api.env"
echo -e "${GREEN}✓ Deployed api.env${NC}"

# Deploy environment-specific worker config
echo -e "${YELLOW}  Deploying worker.env configuration...${NC}"
scp -i "$SSH_KEY" config/worker.env.$CONFIG_SUFFIX $SSH_USER@$API_HOST:/tmp/worker.env
ssh -i "$SSH_KEY" $SSH_USER@$API_HOST "sudo install -m 600 /tmp/worker.env /etc/ocpctl/worker.env && sudo chown ocpctl:ocpctl /etc/ocpctl/worker.env && rm /tmp/worker.env"
echo -e "${GREEN}✓ Deployed worker.env${NC}"

# Update environment configuration
echo -e "${YELLOW}  Updating environment configuration...${NC}"
ssh -i "$SSH_KEY" $SSH_USER@$API_HOST "sudo bash -c 'grep -q \"^ADDONS_DIR=\" /etc/ocpctl/api.env && sed -i \"s|^ADDONS_DIR=.*|ADDONS_DIR=${REMOTE_BASE}/addons|\" /etc/ocpctl/api.env || echo \"ADDONS_DIR=${REMOTE_BASE}/addons\" >> /etc/ocpctl/api.env'"
echo -e "${GREEN}✓ Set ADDONS_DIR=${REMOTE_BASE}/addons${NC}"

# Stop service
ssh -i "$SSH_KEY" $SSH_USER@$API_HOST 'sudo systemctl stop ocpctl-api'

# Update symlink atomically
ssh -i "$SSH_KEY" $SSH_USER@$API_HOST "sudo ln -snf ${REMOTE_BASE}/releases/${VERSION} ${REMOTE_BASE}/current"

# Start service
ssh -i "$SSH_KEY" $SSH_USER@$API_HOST 'sudo systemctl start ocpctl-api'

# Wait for service to start
sleep 3

# Verify service is running
if ssh -i "$SSH_KEY" $SSH_USER@$API_HOST 'sudo systemctl is-active ocpctl-api' > /dev/null; then
    echo -e "${GREEN}✓ API server is running${NC}"

    # Verify version endpoint
    DEPLOYED_VERSION=$(ssh -i "$SSH_KEY" $SSH_USER@$API_HOST 'curl -s http://localhost:8080/version' | jq -r '.version')
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
FAILED_WORKERS=()
for host in "${WORKER_HOSTS[@]}"; do
    echo -e "${YELLOW}Deploying worker to $host ($ENVIRONMENT)...${NC}"

    WORKER_FAILED=false

    # Create versioned directory
    ssh -i "$SSH_KEY" $SSH_USER@$host "sudo mkdir -p ${REMOTE_BASE}/releases/${VERSION}"

    # Deploy installer scripts
    scp -i "$SSH_KEY" scripts/ensure-installers.sh $SSH_USER@$host:/tmp/ensure-installers.sh
    scp -i "$SSH_KEY" scripts/download-specific-version.sh $SSH_USER@$host:/tmp/download-specific-version.sh
    ssh -i "$SSH_KEY" $SSH_USER@$host "sudo mkdir -p ${REMOTE_BASE}/scripts && sudo install -m 755 /tmp/ensure-installers.sh ${REMOTE_BASE}/scripts/ensure-installers.sh && sudo install -m 755 /tmp/download-specific-version.sh ${REMOTE_BASE}/scripts/download-specific-version.sh && rm /tmp/ensure-installers.sh /tmp/download-specific-version.sh"

    # Run ensure-installers to download/update all required CLIs (openshift-install, rosa, eksctl, etc.)
    echo -e "${YELLOW}  Running ensure-installers to install/update CLIs...${NC}"
    ssh -i "$SSH_KEY" $SSH_USER@$host "sudo ${REMOTE_BASE}/scripts/ensure-installers.sh"

    # Deploy manifests directory (for post-deployment scripts)
    echo -e "${YELLOW}  Deploying manifests directory...${NC}"
    ssh -i "$SSH_KEY" $SSH_USER@$host "mkdir -p /tmp/manifests && sudo mkdir -p ${REMOTE_BASE}/manifests"
    scp -i "$SSH_KEY" -r manifests/* $SSH_USER@$host:/tmp/manifests/
    ssh -i "$SSH_KEY" $SSH_USER@$host "sudo cp -r /tmp/manifests/* ${REMOTE_BASE}/manifests/ && sudo chmod -R 755 ${REMOTE_BASE}/manifests && rm -rf /tmp/manifests"

    # Deploy profiles directory
    echo -e "${YELLOW}  Deploying profiles directory...${NC}"
    ssh -i "$SSH_KEY" $SSH_USER@$host "mkdir -p /tmp/profiles && sudo mkdir -p ${REMOTE_BASE}/profiles"
    scp -i "$SSH_KEY" -r internal/profile/definitions/* $SSH_USER@$host:/tmp/profiles/
    ssh -i "$SSH_KEY" $SSH_USER@$host "sudo cp -r /tmp/profiles/* ${REMOTE_BASE}/profiles/ && sudo chown -R ocpctl:ocpctl ${REMOTE_BASE}/profiles && rm -rf /tmp/profiles"

    # Deploy environment-specific worker config
    echo -e "${YELLOW}  Deploying worker.env configuration...${NC}"
    scp -i "$SSH_KEY" config/worker.env.$CONFIG_SUFFIX $SSH_USER@$host:/tmp/worker.env
    ssh -i "$SSH_KEY" $SSH_USER@$host "sudo install -m 600 /tmp/worker.env /etc/ocpctl/worker.env && sudo chown ocpctl:ocpctl /etc/ocpctl/worker.env && rm /tmp/worker.env"
    echo -e "${GREEN}✓ Deployed worker.env${NC}"

    # Deploy systemd service file
    scp -i "$SSH_KEY" deploy/systemd/ocpctl-worker.service $SSH_USER@$host:/tmp/ocpctl-worker.service
    ssh -i "$SSH_KEY" $SSH_USER@$host "sudo install -m 644 /tmp/ocpctl-worker.service /etc/systemd/system/ocpctl-worker.service && rm /tmp/ocpctl-worker.service && sudo systemctl daemon-reload"

    # Copy binary
    scp -i "$SSH_KEY" bin/ocpctl-worker-${VERSION} $SSH_USER@$host:/tmp/ocpctl-worker-${VERSION}
    ssh -i "$SSH_KEY" $SSH_USER@$host "sudo install -m 755 /tmp/ocpctl-worker-${VERSION} ${REMOTE_BASE}/releases/${VERSION}/ocpctl-worker && rm /tmp/ocpctl-worker-${VERSION}"

    # Requeue any RUNNING jobs before stopping worker (prevents orphaned jobs)
    echo -e "${YELLOW}  Requeuing any in-progress jobs...${NC}"
    # Extract DATABASE_URL from worker.env and use it for RDS connection
    REQUEUED=$(ssh -i "$SSH_KEY" $SSH_USER@$host 'DATABASE_URL=$(grep "^DATABASE_URL=" /etc/ocpctl/worker.env | cut -d= -f2-); psql "$DATABASE_URL" -t -c "UPDATE jobs SET status = '"'"'PENDING'"'"', started_at = NULL WHERE status = '"'"'RUNNING'"'"' RETURNING id;" 2>/dev/null | grep -o "-" | wc -l | tr -d " "' || echo "0")
    if [ "$REQUEUED" -gt 0 ] 2>/dev/null; then
        echo -e "${GREEN}✓ Requeued $REQUEUED job(s) to PENDING status${NC}"
    else
        echo -e "${YELLOW}  No jobs to requeue${NC}"
    fi

    # Clear any stale job locks from this worker (prevents blocked jobs after restart)
    echo -e "${YELLOW}  Clearing stale job locks...${NC}"
    INSTANCE_ID=$(ssh -i "$SSH_KEY" $SSH_USER@$host 'ec2-metadata --instance-id 2>/dev/null | cut -d " " -f 2' || echo "unknown")
    LOCKS_CLEARED=$(ssh -i "$SSH_KEY" $SSH_USER@$host 'DATABASE_URL=$(grep "^DATABASE_URL=" /etc/ocpctl/worker.env | cut -d= -f2-); psql "$DATABASE_URL" -t -c "DELETE FROM job_locks WHERE locked_by LIKE '"'"'%'$INSTANCE_ID'%'"'"' RETURNING job_id;" 2>/dev/null | grep -o "-" | wc -l | tr -d " "' || echo "0")
    if [ "$LOCKS_CLEARED" -gt 0 ] 2>/dev/null; then
        echo -e "${GREEN}✓ Cleared $LOCKS_CLEARED stale lock(s)${NC}"
    else
        echo -e "${YELLOW}  No locks to clear${NC}"
    fi

    # Stop service
    ssh -i "$SSH_KEY" $SSH_USER@$host 'sudo systemctl stop ocpctl-worker'

    # Update symlink atomically
    ssh -i "$SSH_KEY" $SSH_USER@$host "sudo ln -snf ${REMOTE_BASE}/releases/${VERSION} ${REMOTE_BASE}/current"

    # Start service
    ssh -i "$SSH_KEY" $SSH_USER@$host 'sudo systemctl start ocpctl-worker'

    # Wait for service to start
    sleep 3

    # Verify service is running
    if ssh -i "$SSH_KEY" $SSH_USER@$host 'sudo systemctl is-active ocpctl-worker' > /dev/null; then
        echo -e "${GREEN}✓ Worker on $host is running${NC}"

        # Verify version endpoint (assuming worker health check is on port 8081)
        DEPLOYED_VERSION=$(ssh -i "$SSH_KEY" $SSH_USER@$host 'curl -s http://localhost:8081/version' | jq -r '.version')
        if [ "$DEPLOYED_VERSION" = "$VERSION" ]; then
            echo -e "${GREEN}✓ Worker version verified: $DEPLOYED_VERSION${NC}"
        else
            echo -e "${RED}✗ Worker version mismatch! Expected: $VERSION, Got: $DEPLOYED_VERSION${NC}"
            WORKER_FAILED=true
            FAILED_WORKERS+=("$host (version mismatch)")
        fi
    else
        echo -e "${RED}✗ Worker on $host failed to start${NC}"
        WORKER_FAILED=true
        FAILED_WORKERS+=("$host (failed to start)")
    fi

    echo ""
done

# Deployment summary
if [ ${#FAILED_WORKERS[@]} -eq 0 ]; then
    echo -e "${GREEN}=== Deployment Complete ===${NC}"
    echo ""
    echo -e "${BLUE}Environment: ${ENVIRONMENT}${NC}"
    echo -e "${BLUE}Deployed Version: ${VERSION}${NC}"
    echo -e "${BLUE}Commit: ${COMMIT}${NC}"
    echo ""
    echo "Verify deployment:"
    echo "  API:    curl https://${DOMAIN}/version"
    echo "  Worker: ssh -i $SSH_KEY $SSH_USER@$API_HOST 'curl -s http://localhost:8081/version'"
    echo ""
    echo "Rollback to previous version:"
    echo "  ssh -i $SSH_KEY $SSH_USER@$API_HOST 'sudo ls -d ${REMOTE_BASE}/releases/*'"
    echo "  ./deploy-env.sh $ENVIRONMENT <previous-version>"

    exit 0
else
    echo -e "${RED}=== Deployment FAILED ===${NC}"
    echo ""
    echo -e "${RED}Failed workers (${#FAILED_WORKERS[@]}):${NC}"
    for worker in "${FAILED_WORKERS[@]}"; do
        echo -e "${RED}  ✗ $worker${NC}"
    done
    echo ""
    echo -e "${YELLOW}Rollback instructions:${NC}"
    echo "  ssh -i $SSH_KEY $SSH_USER@$API_HOST 'sudo ls -d ${REMOTE_BASE}/releases/*'"
    echo "  ./deploy-env.sh $ENVIRONMENT <previous-version>"
    echo ""

    exit 1
fi
