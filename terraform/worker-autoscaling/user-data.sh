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

# Configure systemd-resolved to use external DNS for faster propagation of new Route53 records
echo "Configuring DNS resolution with external DNS servers..."
mkdir -p /etc/systemd/resolved.conf.d/
cat > /etc/systemd/resolved.conf.d/dns_servers.conf <<'DNSEOF'
[Resolve]
DNS=8.8.8.8 1.1.1.1
FallbackDNS=8.8.4.4 1.0.0.1
CacheFromLocalhost=no
DNSEOF

systemctl restart systemd-resolved
echo "DNS configuration applied - using Google/Cloudflare DNS for faster Route53 propagation"

# Install kubectl (required for IKS post-config)
echo "Installing kubectl..."
curl -LO "https://dl.k8s.io/release/$(curl -L -s https://dl.k8s.io/release/stable.txt)/bin/linux/amd64/kubectl"
install -o root -g root -m 0755 kubectl /usr/local/bin/kubectl
rm kubectl
echo "kubectl version: $(kubectl version --client --short 2>/dev/null || kubectl version --client)"

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

# Download profile definitions
echo "Downloading profile definitions"
mkdir -p /opt/ocpctl/profiles/definitions
aws s3 sync s3://ocpctl-binaries/profiles/ /opt/ocpctl/profiles/definitions/
chown -R ocpctl:ocpctl /opt/ocpctl/profiles

# Download OpenShift installer binaries for all supported versions
echo "Downloading OpenShift installer binaries..."
cd /tmp

# 4.18 (latest stable in 4.18 series)
wget -q https://mirror.openshift.com/pub/openshift-v4/x86_64/clients/ocp/4.18.35/openshift-install-linux.tar.gz
tar -xzf openshift-install-linux.tar.gz
mv openshift-install /usr/local/bin/openshift-install-4.18
chmod +x /usr/local/bin/openshift-install-4.18
rm openshift-install-linux.tar.gz README.md

wget -q https://mirror.openshift.com/pub/openshift-v4/x86_64/clients/ocp/4.18.35/ccoctl-linux.tar.gz
tar -xzf ccoctl-linux.tar.gz
mv ccoctl /usr/local/bin/ccoctl-4.18
chmod +x /usr/local/bin/ccoctl-4.18
rm ccoctl-linux.tar.gz

# 4.19 (latest stable in 4.19 series)
wget -q https://mirror.openshift.com/pub/openshift-v4/x86_64/clients/ocp/4.19.23/openshift-install-linux.tar.gz
tar -xzf openshift-install-linux.tar.gz
mv openshift-install /usr/local/bin/openshift-install-4.19
chmod +x /usr/local/bin/openshift-install-4.19
rm openshift-install-linux.tar.gz README.md

wget -q https://mirror.openshift.com/pub/openshift-v4/x86_64/clients/ocp/4.19.23/ccoctl-linux.tar.gz
tar -xzf ccoctl-linux.tar.gz
mv ccoctl /usr/local/bin/ccoctl-4.19
chmod +x /usr/local/bin/ccoctl-4.19
rm ccoctl-linux.tar.gz

# 4.20 (latest stable in 4.20 series)
wget -q https://mirror.openshift.com/pub/openshift-v4/x86_64/clients/ocp/4.20.5/openshift-install-linux.tar.gz
tar -xzf openshift-install-linux.tar.gz
mv openshift-install /usr/local/bin/openshift-install-4.20
chmod +x /usr/local/bin/openshift-install-4.20
rm openshift-install-linux.tar.gz README.md

wget -q https://mirror.openshift.com/pub/openshift-v4/x86_64/clients/ocp/4.20.5/ccoctl-linux.tar.gz
tar -xzf ccoctl-linux.tar.gz
mv ccoctl /usr/local/bin/ccoctl-4.20
chmod +x /usr/local/bin/ccoctl-4.20
rm ccoctl-linux.tar.gz

# 4.21 (latest stable in 4.21 series)
wget -q https://mirror.openshift.com/pub/openshift-v4/x86_64/clients/ocp/4.21.10/openshift-install-linux.tar.gz
tar -xzf openshift-install-linux.tar.gz
mv openshift-install /usr/local/bin/openshift-install-4.21
chmod +x /usr/local/bin/openshift-install-4.21
rm openshift-install-linux.tar.gz README.md

wget -q https://mirror.openshift.com/pub/openshift-v4/x86_64/clients/ocp/4.21.10/ccoctl-linux.tar.gz
tar -xzf ccoctl-linux.tar.gz
mv ccoctl /usr/local/bin/ccoctl-4.21
chmod +x /usr/local/bin/ccoctl-4.21
rm ccoctl-linux.tar.gz

# 4.22 (pre-release - use RHEL9 version for FIPS compatibility)
wget -q https://mirror.openshift.com/pub/openshift-v4/x86_64/clients/ocp-dev-preview/latest-4.22/openshift-install-linux.tar.gz
tar -xzf openshift-install-linux.tar.gz
mv openshift-install /usr/local/bin/openshift-install-4.22
chmod +x /usr/local/bin/openshift-install-4.22
rm openshift-install-linux.tar.gz README.md

wget -q https://mirror.openshift.com/pub/openshift-v4/x86_64/clients/ocp-dev-preview/latest-4.22/ccoctl-rhel9-linux.tar.gz
tar -xzf ccoctl-rhel9-linux.tar.gz
mv ccoctl /usr/local/bin/ccoctl-4.22-rhel9
chmod +x /usr/local/bin/ccoctl-4.22-rhel9
rm ccoctl-rhel9-linux.tar.gz

cd -
echo "OpenShift installer binaries installed"

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

# Static AWS credentials for OpenShift 4.22 compatibility (if needed)
# These should be provided via instance metadata or environment variables
# NOT hardcoded in user-data for security reasons
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
