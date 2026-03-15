#!/bin/bash
# Deployment script for ocpctl-worker
# Usage: ./deploy-workers.sh

set -e

# Configuration
WORKER_HOSTS=("52.90.135.148" "54.235.4.38")
SSH_KEY="$HOME/.ssh/ocpctl-test-key.pem"
S3_BUCKET="s3://ocpctl-binaries"
BINARY_NAME="ocpctl-worker"

# Colors
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

echo -e "${GREEN}=== OCPCTL Worker Deployment ===${NC}"
echo ""

# Step 1: Build binary
echo -e "${YELLOW}Building worker binary...${NC}"
cd "$(dirname "$0")/.."
GOOS=linux GOARCH=amd64 go build -o bin/${BINARY_NAME}-linux ./cmd/worker

# Step 2: Upload to S3
echo -e "${YELLOW}Uploading to S3...${NC}"
aws s3 cp bin/${BINARY_NAME}-linux ${S3_BUCKET}/${BINARY_NAME}
echo -e "${GREEN}✓ Uploaded to ${S3_BUCKET}/${BINARY_NAME}${NC}"
echo ""

# Step 3: Deploy to each worker
for host in "${WORKER_HOSTS[@]}"; do
  echo -e "${YELLOW}Deploying to $host...${NC}"

  # Stop service
  ssh -i "$SSH_KEY" ec2-user@$host 'sudo systemctl stop ocpctl-worker'

  # Download latest binary from S3
  ssh -i "$SSH_KEY" ec2-user@$host "sudo aws s3 cp ${S3_BUCKET}/${BINARY_NAME} /usr/local/bin/ocpctl-worker && sudo chmod +x /usr/local/bin/ocpctl-worker"

  # Start service
  ssh -i "$SSH_KEY" ec2-user@$host 'sudo systemctl start ocpctl-worker'

  # Check status
  ssh -i "$SSH_KEY" ec2-user@$host 'sudo systemctl is-active ocpctl-worker' > /dev/null && \
    echo -e "${GREEN}✓ Worker on $host is running${NC}" || \
    echo -e "${RED}✗ Worker on $host failed to start${NC}"

  echo ""
done

echo -e "${GREEN}=== Deployment Complete ===${NC}"
echo ""
echo "Verify workers:"
for host in "${WORKER_HOSTS[@]}"; do
  echo "  ssh -i $SSH_KEY ec2-user@$host 'sudo systemctl status ocpctl-worker'"
done
