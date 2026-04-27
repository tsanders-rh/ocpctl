#!/bin/bash
set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Configuration
SSH_KEY="${HOME}/.ssh/ocpctl-production-key"
SSH_USER="ubuntu"
API_HOST="44.201.165.78"
WORKER_HOST="44.201.165.78"
GCP_PROJECT="migration-eng"
S3_BUCKET="s3://ocpctl-binaries"
LOCAL_KEY_FILE="${1:-}"

echo -e "${BLUE}=== GCP Credentials Configuration (All Workers + Autoscale) ===${NC}"
echo ""

# Check if key file was provided
if [ -z "$LOCAL_KEY_FILE" ]; then
    echo -e "${RED}Error: Please provide the path to your GCP service account JSON key file${NC}"
    echo "Usage: $0 /path/to/gcp-key.json"
    exit 1
fi

# Check if file exists
if [ ! -f "$LOCAL_KEY_FILE" ]; then
    echo -e "${RED}Error: File not found: $LOCAL_KEY_FILE${NC}"
    exit 1
fi

# Validate JSON format
if ! cat "$LOCAL_KEY_FILE" | python3 -m json.tool > /dev/null 2>&1; then
    echo -e "${RED}Error: Invalid JSON file${NC}"
    exit 1
fi

echo -e "${GREEN}✓ Found valid GCP service account key file${NC}"
echo ""

# Step 1: Update config/worker.env with GCP variables
echo -e "${YELLOW}Step 1: Updating config/worker.env with GCP variables...${NC}"
if ! grep -q "GCP_PROJECT" config/worker.env; then
    cat >> config/worker.env << 'EOF'

# GCP Configuration
GOOGLE_APPLICATION_CREDENTIALS=/opt/ocpctl/gcp-credentials.json
GCP_PROJECT=migration-eng
EOF
    echo -e "${GREEN}✓ Added GCP variables to config/worker.env${NC}"
else
    echo -e "${GREEN}✓ GCP variables already present in config/worker.env${NC}"
fi

# Step 2: Upload GCP credentials to S3
echo -e "${YELLOW}Step 2: Uploading GCP credentials to S3...${NC}"
aws s3 cp "$LOCAL_KEY_FILE" ${S3_BUCKET}/config/gcp-credentials.json
echo -e "${GREEN}✓ Uploaded GCP credentials to S3${NC}"

# Step 3: Upload updated worker.env to S3
echo -e "${YELLOW}Step 3: Uploading updated worker.env to S3...${NC}"
aws s3 cp config/worker.env ${S3_BUCKET}/config/worker.env
echo -e "${GREEN}✓ Uploaded worker.env to S3${NC}"

# Step 4: Update bootstrap-worker.sh to download GCP credentials
echo -e "${YELLOW}Step 4: Updating bootstrap-worker.sh to download GCP credentials...${NC}"
if ! grep -q "gcp-credentials.json" scripts/bootstrap-worker.sh; then
    # Add GCP credentials download after the manifests sync section
    sed -i.bak '/# Download ensure-installers script/i \
# Download GCP credentials from S3\
echo "Downloading GCP credentials from S3..."\
if aws s3 cp ${S3_BUCKET}/config/gcp-credentials.json ${REMOTE_BASE}/gcp-credentials.json; then\
    chmod 600 ${REMOTE_BASE}/gcp-credentials.json\
    echo "✓ GCP credentials downloaded"\
else\
    echo "WARNING: Failed to download GCP credentials from S3 (OK if not using GCP)"\
fi\
\
' scripts/bootstrap-worker.sh
    echo -e "${GREEN}✓ Updated bootstrap-worker.sh${NC}"

    # Upload updated bootstrap script to S3
    aws s3 cp scripts/bootstrap-worker.sh ${S3_BUCKET}/scripts/bootstrap-worker.sh
    echo -e "${GREEN}✓ Uploaded updated bootstrap-worker.sh to S3${NC}"
else
    echo -e "${GREEN}✓ bootstrap-worker.sh already configured for GCP credentials${NC}"
fi

# Step 5: Configure main API server
echo -e "${YELLOW}Step 5: Uploading GCP credentials to API server...${NC}"
scp -i "$SSH_KEY" "$LOCAL_KEY_FILE" $SSH_USER@$API_HOST:/tmp/gcp-credentials.json
ssh -i "$SSH_KEY" $SSH_USER@$API_HOST 'sudo mkdir -p /opt/ocpctl && sudo mv /tmp/gcp-credentials.json /opt/ocpctl/gcp-credentials.json && sudo chmod 600 /opt/ocpctl/gcp-credentials.json && sudo chown ocpctl:ocpctl /opt/ocpctl/gcp-credentials.json'
echo -e "${GREEN}✓ Uploaded to API server${NC}"

# Step 6: Configure API service environment
echo -e "${YELLOW}Step 6: Configuring API service environment variables...${NC}"
ssh -i "$SSH_KEY" $SSH_USER@$API_HOST << 'EOF'
    # Add GCP environment variables to API service
    if ! sudo grep -q "GOOGLE_APPLICATION_CREDENTIALS" /etc/systemd/system/ocpctl-api.service; then
        sudo sed -i '/\[Service\]/a Environment="GOOGLE_APPLICATION_CREDENTIALS=/opt/ocpctl/gcp-credentials.json"\nEnvironment="GCP_PROJECT=migration-eng"' /etc/systemd/system/ocpctl-api.service
        echo "Added GCP environment variables to API service"
    else
        echo "GCP environment variables already present in API service"
    fi
EOF
echo -e "${GREEN}✓ Configured API service${NC}"

# Step 7: Configure main Worker server
echo -e "${YELLOW}Step 7: Configuring main Worker server...${NC}"
scp -i "$SSH_KEY" "$LOCAL_KEY_FILE" $SSH_USER@$WORKER_HOST:/tmp/gcp-credentials.json
ssh -i "$SSH_KEY" $SSH_USER@$WORKER_HOST 'sudo mkdir -p /opt/ocpctl && sudo mv /tmp/gcp-credentials.json /opt/ocpctl/gcp-credentials.json && sudo chmod 600 /opt/ocpctl/gcp-credentials.json && sudo chown ocpctl:ocpctl /opt/ocpctl/gcp-credentials.json'

ssh -i "$SSH_KEY" $SSH_USER@$WORKER_HOST << 'EOF'
    # Add GCP environment variables to Worker service
    if ! sudo grep -q "GOOGLE_APPLICATION_CREDENTIALS" /etc/systemd/system/ocpctl-worker.service; then
        sudo sed -i '/\[Service\]/a Environment="GOOGLE_APPLICATION_CREDENTIALS=/opt/ocpctl/gcp-credentials.json"\nEnvironment="GCP_PROJECT=migration-eng"' /etc/systemd/system/ocpctl-worker.service
        echo "Added GCP environment variables to Worker service"
    else
        echo "GCP environment variables already present in Worker service"
    fi
EOF
echo -e "${GREEN}✓ Configured main Worker service${NC}"

# Step 8: Reload systemd and restart services
echo -e "${YELLOW}Step 8: Restarting API and Worker services...${NC}"
ssh -i "$SSH_KEY" $SSH_USER@$API_HOST << 'EOF'
    sudo systemctl daemon-reload
    sudo systemctl restart ocpctl-api
    sudo systemctl restart ocpctl-worker
EOF
echo -e "${GREEN}✓ Services restarted${NC}"
echo ""

# Step 9: Terminate autoscale workers to pick up new config
echo -e "${YELLOW}Step 9: Terminating autoscale workers to pick up new configuration...${NC}"
AUTOSCALE_INSTANCES=$(aws ec2 describe-instances \
    --filters "Name=tag:Name,Values=ocpctl-worker" \
              "Name=instance-state-name,Values=running" \
    --query 'Reservations[*].Instances[*].[InstanceId]' \
    --output text)

if [ -n "$AUTOSCALE_INSTANCES" ]; then
    echo -e "${YELLOW}  Found autoscale workers: $AUTOSCALE_INSTANCES${NC}"
    for instance_id in $AUTOSCALE_INSTANCES; do
        echo -e "${YELLOW}  Terminating $instance_id...${NC}"
        aws ec2 terminate-instances --instance-ids "$instance_id" > /dev/null
    done
    echo -e "${GREEN}✓ Terminated autoscale workers (ASG will launch replacements with new config)${NC}"
else
    echo -e "${GREEN}✓ No autoscale workers currently running${NC}"
fi

# Step 10: Verify configuration
echo ""
echo -e "${YELLOW}Step 10: Verifying GCP authentication...${NC}"
ssh -i "$SSH_KEY" $SSH_USER@$API_HOST << 'EOF'
    # Test gcloud authentication
    export GOOGLE_APPLICATION_CREDENTIALS=/opt/ocpctl/gcp-credentials.json
    export GCP_PROJECT=migration-eng

    if gcloud auth activate-service-account --key-file=/opt/ocpctl/gcp-credentials.json 2>/dev/null; then
        echo "✓ Service account authentication successful"
        gcloud config set project migration-eng 2>/dev/null
        echo "✓ Project set to migration-eng"

        # Test API access
        if gcloud compute zones list --limit=1 --format="value(name)" 2>/dev/null | grep -q ".*"; then
            echo "✓ Compute Engine API access verified"
        else
            echo "⚠ Warning: Could not verify Compute Engine API access"
            echo "  Make sure the service account has these roles:"
            echo "  - Compute Admin"
            echo "  - Kubernetes Engine Admin"
            echo "  - Storage Admin"
            echo "  - Service Account User"
        fi
    else
        echo "✗ Service account authentication failed"
        exit 1
    fi
EOF

if [ $? -eq 0 ]; then
    echo ""
    echo -e "${GREEN}=== Configuration Complete ===${NC}"
    echo ""
    echo -e "${BLUE}GCP credentials configured successfully!${NC}"
    echo ""
    echo "Configuration applied to:"
    echo "  ✓ API server (44.201.165.78)"
    echo "  ✓ Main Worker server (44.201.165.78)"
    echo "  ✓ S3 bucket (for autoscale workers)"
    echo "  ✓ Bootstrap script updated"
    echo ""
    echo "Environment variables:"
    echo "  GOOGLE_APPLICATION_CREDENTIALS=/opt/ocpctl/gcp-credentials.json"
    echo "  GCP_PROJECT=migration-eng"
    echo ""
    echo "Next time autoscale workers launch, they will automatically:"
    echo "  1. Download GCP credentials from S3"
    echo "  2. Load GCP environment variables from worker.env"
    echo "  3. Be ready to create GCP/GKE clusters"
    echo ""
    echo -e "${GREEN}You can now create GCP clusters from ocpctl!${NC}"
else
    echo ""
    echo -e "${RED}=== Configuration Failed ===${NC}"
    echo ""
    echo "Please check the errors above and try again."
    echo ""
    echo "Common issues:"
    echo "  - Service account doesn't have required IAM roles"
    echo "  - GCP APIs not enabled (Compute Engine, GKE)"
    echo "  - Invalid JSON key file"
    exit 1
fi
