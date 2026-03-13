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

# Create config directory and environment file
mkdir -p /etc/ocpctl
cat > /etc/ocpctl/worker.env <<EOF
DATABASE_URL=$DATABASE_URL
WORK_DIR=$WORK_DIR
WORKER_POLL_INTERVAL=$${POLL_INTERVAL}s
WORKER_MAX_CONCURRENT=$MAX_CONCURRENT
AWS_REGION=$(ec2-metadata --availability-zone | cut -d' ' -f2 | sed 's/[a-z]$//')
EOF

chmod 600 /etc/ocpctl/worker.env

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
