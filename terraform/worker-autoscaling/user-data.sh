#!/bin/bash
set -e

# User data script for OCPCTL worker instances
# This script downloads the worker binary and starts the worker service

# Configuration from Terraform template variables.
# NOTE: worker runtime config (DATABASE_URL, pull secret, cloud credentials incl.
# the Azure service principal) is NOT rendered here — it is pulled from
# s3://ocpctl-binaries/config/worker.env at boot so secrets never land in the
# launch template / tfstate, and there is a single source of truth shared with the
# primary workers (#80).
WORKER_BINARY_URL="${worker_binary_url}"

# Install required packages
yum update -y
yum install -y wget postgresql15 awscli jq

# Use VPC DNS resolver for proper resolution of RDS endpoints and other VPC resources
# (External DNS like 8.8.8.8 would resolve RDS to public IPs, breaking VPC connectivity)

# Install kubectl (required for IKS/GKE post-config)
echo "Installing kubectl..."
curl -LO "https://dl.k8s.io/release/$(curl -L -s https://dl.k8s.io/release/stable.txt)/bin/linux/amd64/kubectl"
install -o root -g root -m 0755 kubectl /usr/local/bin/kubectl
rm kubectl
echo "kubectl version: $(kubectl version --client --short 2>/dev/null || kubectl version --client)"

# Install gcloud CLI and gke-gcloud-auth-plugin (required for GKE)
echo "Installing gcloud CLI..."
# Import Google Cloud public key
rpm --import https://packages.cloud.google.com/yum/doc/rpm-package-key.gpg

# Add Google Cloud SDK repository
cat > /etc/yum.repos.d/google-cloud-sdk.repo <<'GCLOUDEOF'
[google-cloud-cli]
name=Google Cloud CLI
baseurl=https://packages.cloud.google.com/yum/repos/cloud-sdk-el9-x86_64
enabled=1
gpgcheck=1
repo_gpgcheck=0
gpgkey=https://packages.cloud.google.com/yum/doc/rpm-package-key.gpg
GCLOUDEOF

# Install gcloud and auth plugin
yum install -y google-cloud-cli google-cloud-cli-gke-gcloud-auth-plugin
echo "gcloud version: $(gcloud version --format='value(version)' 2>/dev/null || echo 'error')"
echo "gke-gcloud-auth-plugin version: $(gke-gcloud-auth-plugin --version 2>/dev/null || echo 'error')"

# Create ocpctl user. Home must be /opt/ocpctl so CLI login hooks can persist
# ~/.azure etc.; the default /home/ocpctl is never created (#80).
useradd -r -s /bin/bash -d /opt/ocpctl ocpctl || true
mkdir -p /opt/ocpctl
chown ocpctl:ocpctl /opt/ocpctl

# Download worker binary
echo "Downloading worker binary from $WORKER_BINARY_URL"
aws s3 cp "$WORKER_BINARY_URL" /usr/local/bin/ocpctl-worker
chmod +x /usr/local/bin/ocpctl-worker
chown root:root /usr/local/bin/ocpctl-worker

# Download profile definitions. Must land directly in /opt/ocpctl/profiles to match
# PROFILES_DIR in the shared worker.env (the primary-host layout); a nested
# definitions/ subdir leaves the worker finding zero profiles and exiting (#80).
echo "Downloading profile definitions"
mkdir -p /opt/ocpctl/profiles
aws s3 sync s3://ocpctl-binaries/profiles/ /opt/ocpctl/profiles/
chown -R ocpctl:ocpctl /opt/ocpctl/profiles

# Download CLI login hooks + installer script referenced by the worker service.
echo "Downloading worker scripts from S3"
mkdir -p /opt/ocpctl/scripts
aws s3 cp s3://ocpctl-binaries/scripts/ensure-installers.sh /opt/ocpctl/scripts/ensure-installers.sh
aws s3 cp s3://ocpctl-binaries/scripts/azure-login.sh       /opt/ocpctl/scripts/azure-login.sh
aws s3 cp s3://ocpctl-binaries/scripts/ibmcloud-login.sh    /opt/ocpctl/scripts/ibmcloud-login.sh
chmod 755 /opt/ocpctl/scripts/*.sh
chown -R ocpctl:ocpctl /opt/ocpctl/scripts

# Install all cluster CLIs (openshift-install, oc, eksctl, gcloud, az, ibmcloud, ...)
# synchronously here so that az/etc. exist BEFORE the azure-login ExecStartPre hook
# runs at service start (otherwise the worker would come up with no Azure session,
# #80). Worker ASG uses EC2 health checks, so a longer cloud-init does not fail the
# instance. Non-fatal: the script has its own retry logic.
echo "Running ensure-installers.sh (foreground)..."
/opt/ocpctl/scripts/ensure-installers.sh > /var/log/ensure-installers.log 2>&1 || \
  echo "WARNING: ensure-installers.sh returned non-zero (continuing)"

# Pull worker runtime config from S3 — single source of truth shared with the
# primary workers. Includes DATABASE_URL, the pull secret, and all cloud
# credentials (incl. the Azure service principal). Pulling it here means no secrets
# are rendered into the launch template / tfstate.
mkdir -p /etc/ocpctl
echo "Downloading worker.env from S3"
aws s3 cp s3://ocpctl-binaries/config/worker.env /etc/ocpctl/worker.env
chown ocpctl:ocpctl /etc/ocpctl/worker.env
chmod 600 /etc/ocpctl/worker.env

# Create the runtime directories declared in worker.env and hand the whole
# /var/lib/ocpctl tree to the worker user. openshift-install writes its bootstrap
# ignition + CAPI/envtest scratch into $TMPDIR; if that dir is missing — or its
# parent is root-owned so the ocpctl-run worker cannot create it — a create fails
# with ENOENT ("failed to create tmp file for bootstrap ignition: ... no such
# file or directory"). WORKER_WORK_DIR holds the per-cluster install dirs. Both
# live under /var/lib/ocpctl, so chown the parent too (a plain `mkdir -p .../foo`
# leaves /var/lib/ocpctl itself root-owned).
WORKER_WORK_DIR=$(grep -E '^WORKER_WORK_DIR=' /etc/ocpctl/worker.env | cut -d= -f2- | tr -d '"')
WORKER_WORK_DIR=$${WORKER_WORK_DIR:-/var/lib/ocpctl/clusters}
WORKER_TMPDIR=$(grep -E '^TMPDIR=' /etc/ocpctl/worker.env | cut -d= -f2- | tr -d '"')
WORKER_TMPDIR=$${WORKER_TMPDIR:-/var/lib/ocpctl/tmp}
mkdir -p "$WORKER_WORK_DIR" "$WORKER_TMPDIR"
chown -R ocpctl:ocpctl /var/lib/ocpctl

# Install the worker systemd service. HOME is pinned to /opt/ocpctl so the az/gcloud/
# ibmcloud sessions written by the login hooks persist where the worker reads them
# (#80). azure-login.sh fails hard when Azure creds are set but login fails, so the
# worker will not silently accept ARO jobs it cannot run; Restart=on-failure retries
# transient AAD propagation errors.
cat > /etc/systemd/system/ocpctl-worker.service <<'SERVICEEOF'
[Unit]
Description=OCPCTL Worker
After=network.target
Wants=network-online.target

[Service]
Type=simple
User=ocpctl
Environment="PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"
Environment="HOME=/opt/ocpctl"
EnvironmentFile=/etc/ocpctl/worker.env

# Authenticate cloud CLIs before the worker starts.
ExecStartPre=/opt/ocpctl/scripts/azure-login.sh
ExecStartPre=/opt/ocpctl/scripts/ibmcloud-login.sh

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
systemctl status ocpctl-worker --no-pager || true

echo "OCPCTL worker instance setup complete"
