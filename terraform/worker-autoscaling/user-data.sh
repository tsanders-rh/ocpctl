#!/bin/bash
set -e

# User data script for OCPCTL worker instances
# This script downloads the worker binary and starts the worker service

# Configuration from Terraform template variables
DATABASE_URL="${database_url}"
WORK_DIR="${work_dir}"
POLL_INTERVAL="${worker_poll_interval}"
MAX_CONCURRENT="${worker_max_concurrent}"
WORKER_BINARY_URL="${worker_binary_url}"
OPENSHIFT_PULL_SECRET='${openshift_pull_secret}'

# Install required packages
yum update -y
yum install -y wget postgresql15 awscli jq

# Create ocpctl user
useradd -r -s /bin/false ocpctl || true

# Create work directory
mkdir -p "$WORK_DIR"
chown ocpctl:ocpctl "$WORK_DIR"

# Download worker binary
echo "Downloading worker binary from $WORKER_BINARY_URL"
aws s3 cp "$WORKER_BINARY_URL" /usr/local/bin/ocpctl-worker
chmod +x /usr/local/bin/ocpctl-worker
chown root:root /usr/local/bin/ocpctl-worker

# Download and extract profile definitions
echo "Downloading profile definitions"
aws s3 cp "s3://ocpctl-binaries-346869059911/binaries/profiles.tar.gz" /tmp/profiles.tar.gz
mkdir -p /opt/ocpctl/profiles
tar -xzf /tmp/profiles.tar.gz -C /opt/ocpctl/profiles
rm /tmp/profiles.tar.gz
chown -R ocpctl:ocpctl /opt/ocpctl/profiles

# Create config directory and environment file
mkdir -p /etc/ocpctl

# Write pull secret to separate JSON file (better security and reliability)
cat > /etc/ocpctl/pull-secret.json <<'PULLSECRETEOF'
${openshift_pull_secret}
PULLSECRETEOF

chown ocpctl:ocpctl /etc/ocpctl/pull-secret.json
chmod 400 /etc/ocpctl/pull-secret.json

# Get AWS region from instance metadata
DETECTED_AWS_REGION=$(ec2-metadata --availability-zone | cut -d' ' -f2 | sed 's/[a-z]$//')

# Write environment file - use bash variables that were set at the top
cat > /etc/ocpctl/worker.env <<EOF
DATABASE_URL=$DATABASE_URL
WORKER_WORK_DIR=$WORK_DIR
WORKER_POLL_INTERVAL=$${POLL_INTERVAL}s
WORKER_MAX_CONCURRENT=$MAX_CONCURRENT
OPENSHIFT_PULL_SECRET_FILE=/etc/ocpctl/pull-secret.json
PROFILES_DIR=/opt/ocpctl/profiles/definitions
AWS_REGION=$DETECTED_AWS_REGION
EOF

chown ocpctl:ocpctl /etc/ocpctl/worker.env
chmod 400 /etc/ocpctl/worker.env

# Debug: Show the environment file contents and verify pull secret file exists
echo "Environment file contents:"
cat /etc/ocpctl/worker.env
echo ""
echo "Pull secret file check:"
ls -lh /etc/ocpctl/pull-secret.json

# Test worker startup and capture any errors
echo "Testing worker binary with environment..."
set +e
source /etc/ocpctl/worker.env && /usr/local/bin/ocpctl-worker --version 2>&1 || echo "Worker binary execution failed with exit code $?"
echo "Attempting to start worker with current config..."
timeout 5 bash -c 'source /etc/ocpctl/worker.env && /usr/local/bin/ocpctl-worker' 2>&1 | head -20
set -e

# Create systemd service
cat > /etc/systemd/system/ocpctl-worker.service <<'SERVICEEOF'
[Unit]
Description=OCPCTL Worker
After=network.target
Wants=network-online.target

[Service]
Type=simple
User=ocpctl
EnvironmentFile=/etc/ocpctl/worker.env
ExecStart=/usr/local/bin/ocpctl-worker
Restart=on-failure
RestartSec=10
StandardOutput=journal
StandardError=journal
SyslogIdentifier=ocpctl-worker

# Resource limits
LimitNOFILE=65536
LimitNPROC=4096

# Security
NoNewPrivileges=true
PrivateTmp=true

[Install]
WantedBy=multi-user.target
SERVICEEOF

# Enable and start worker service
systemctl daemon-reload
systemctl enable ocpctl-worker
systemctl start ocpctl-worker

# Log status
sleep 5
systemctl status ocpctl-worker

echo "OCPCTL worker instance setup complete"
