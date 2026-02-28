#!/bin/bash
# OCPCTL Deployment Setup Script for AWS EC2
# Run this script with sudo on a fresh Ubuntu/Amazon Linux 2 instance

set -e

echo "=== OCPCTL Deployment Setup ==="
echo ""

# Check if running as root
if [ "$EUID" -ne 0 ]; then
  echo "Please run as root (use sudo)"
  exit 1
fi

# Detect OS
if [ -f /etc/os-release ]; then
  . /etc/os-release
  OS=$ID
else
  echo "Cannot detect OS"
  exit 1
fi

echo "Detected OS: $OS"

# Install dependencies based on OS
echo ""
echo "1. Installing system dependencies..."

if [ "$OS" = "ubuntu" ] || [ "$OS" = "debian" ]; then
  apt-get update
  apt-get install -y \
    postgresql-client \
    curl \
    wget \
    unzip \
    jq \
    git
elif [ "$OS" = "amzn" ] || [ "$OS" = "rhel" ] || [ "$OS" = "centos" ]; then
  yum update -y
  yum install -y \
    postgresql \
    curl \
    wget \
    unzip \
    jq \
    git
else
  echo "Unsupported OS: $OS"
  exit 1
fi

# Install Go (required for building)
echo ""
echo "2. Installing Go..."
GO_VERSION="1.21.5"
if ! command -v go &> /dev/null; then
  wget -q https://go.dev/dl/go${GO_VERSION}.linux-amd64.tar.gz
  rm -rf /usr/local/go
  tar -C /usr/local -xzf go${GO_VERSION}.linux-amd64.tar.gz
  rm go${GO_VERSION}.linux-amd64.tar.gz
  echo 'export PATH=$PATH:/usr/local/go/bin' >> /etc/profile
  export PATH=$PATH:/usr/local/go/bin
fi
go version

# Install AWS CLI (if not present)
echo ""
echo "3. Installing AWS CLI..."
if ! command -v aws &> /dev/null; then
  curl -s "https://awscli.amazonaws.com/awscli-exe-linux-x86_64.zip" -o "awscliv2.zip"
  unzip -q awscliv2.zip
  ./aws/install
  rm -rf aws awscliv2.zip
fi
aws --version

# Create ocpctl user and group
echo ""
echo "4. Creating ocpctl user and directories..."
if ! id ocpctl &>/dev/null; then
  useradd -r -s /bin/bash -m -d /opt/ocpctl ocpctl
fi

# Create directory structure
mkdir -p /opt/ocpctl/bin
mkdir -p /opt/ocpctl/profiles
mkdir -p /var/lib/ocpctl/clusters
mkdir -p /var/log/ocpctl
mkdir -p /etc/ocpctl

# Set ownership
chown -R ocpctl:ocpctl /opt/ocpctl
chown -R ocpctl:ocpctl /var/lib/ocpctl
chown -R ocpctl:ocpctl /var/log/ocpctl

echo ""
echo "5. Directory structure created:"
echo "  - /opt/ocpctl          (application home)"
echo "  - /opt/ocpctl/bin      (binaries)"
echo "  - /opt/ocpctl/profiles (cluster profiles)"
echo "  - /var/lib/ocpctl      (working directory)"
echo "  - /var/log/ocpctl      (logs)"
echo "  - /etc/ocpctl          (configuration)"

echo ""
echo "=== Setup Complete ==="
echo ""
echo "Next steps:"
echo "1. Build binaries: make build"
echo "2. Copy binaries to /opt/ocpctl/bin/"
echo "3. Copy profiles to /opt/ocpctl/profiles/"
echo "4. Configure /etc/ocpctl/api.env and worker.env"
echo "5. Install systemd services: make install-services"
echo "6. Start services: systemctl start ocpctl-api ocpctl-worker"
echo ""
echo "See deploy/DEPLOYMENT.md for detailed instructions"
