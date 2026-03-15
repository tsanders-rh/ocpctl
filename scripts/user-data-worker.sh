#!/bin/bash
# User-data script for autoscaling worker instances
# This runs on first boot of new EC2 instances

set -e
exec > >(tee /var/log/ocpctl-bootstrap.log)
exec 2>&1

echo "=== OCPCTL Worker Bootstrap Started at $(date) ==="

# Wait for network
echo "Waiting for network..."
until ping -c1 s3.amazonaws.com &>/dev/null; do
    echo "Waiting for network..."
    sleep 1
done

# Install dependencies if needed
if ! command -v jq &> /dev/null; then
    echo "Installing jq..."
    yum install -y jq
fi

# Create ocpctl user if doesn't exist
if ! id ocpctl &>/dev/null; then
    echo "Creating ocpctl user..."
    useradd -r -s /bin/false ocpctl
fi

# Create directory structure
echo "Creating directory structure..."
mkdir -p /opt/ocpctl/releases
mkdir -p /var/lib/ocpctl/clusters
mkdir -p /var/lib/ocpctl/tmp
mkdir -p /etc/ocpctl

# Set ownership
chown -R ocpctl:ocpctl /var/lib/ocpctl
chown -R ocpctl:ocpctl /opt/ocpctl

# Copy bootstrap script from S3 if not exists
if [ ! -f /opt/ocpctl/bootstrap.sh ]; then
    echo "Downloading bootstrap script..."
    aws s3 cp s3://ocpctl-binaries/scripts/bootstrap-worker.sh /opt/ocpctl/bootstrap.sh
    chmod +x /opt/ocpctl/bootstrap.sh
fi

# Copy service file from S3 if not exists
if [ ! -f /etc/systemd/system/ocpctl-worker.service ]; then
    echo "Downloading systemd service file..."
    aws s3 cp s3://ocpctl-binaries/scripts/ocpctl-worker.service /etc/systemd/system/ocpctl-worker.service
    systemctl daemon-reload
fi

# Copy environment file from S3 if not exists
if [ ! -f /etc/ocpctl/worker.env ]; then
    echo "Downloading worker environment file..."
    aws s3 cp s3://ocpctl-binaries/config/worker.env /etc/ocpctl/worker.env
    chmod 600 /etc/ocpctl/worker.env
fi

# Bootstrap with latest version
echo "Running bootstrap for latest version..."
/opt/ocpctl/bootstrap.sh latest

# Enable and start service
echo "Enabling and starting ocpctl-worker service..."
systemctl enable ocpctl-worker
systemctl start ocpctl-worker

# Wait for service to be ready
echo "Waiting for worker to be ready..."
for i in {1..30}; do
    if systemctl is-active ocpctl-worker > /dev/null; then
        if curl -sf http://localhost:8081/ready > /dev/null; then
            echo "✓ Worker is ready!"
            VERSION=$(curl -s http://localhost:8081/version | jq -r '.version')
            echo "✓ Running version: ${VERSION}"
            break
        fi
    fi
    echo "Waiting for worker to be ready... ($i/30)"
    sleep 2
done

if ! systemctl is-active ocpctl-worker > /dev/null; then
    echo "ERROR: Worker failed to start!"
    journalctl -u ocpctl-worker -n 50 --no-pager
    exit 1
fi

echo "=== OCPCTL Worker Bootstrap Completed at $(date) ==="
