#!/bin/bash
# User data script for OCPCTL dev server
# This runs on first boot to perform basic initialization

set -e

echo "=== OCPCTL Dev Server Bootstrap ==="

# Set hostname
hostnamectl set-hostname ${hostname}
echo "${hostname}" > /etc/hostname

# Update package lists
apt-get update

# Install basic utilities
apt-get install -y \
  curl \
  wget \
  git \
  vim \
  jq \
  unzip \
  postgresql-client

# Install AWS CLI v2
if ! command -v aws &> /dev/null; then
  curl "https://awscli.amazonaws.com/awscli-exe-linux-x86_64.zip" -o "awscliv2.zip"
  unzip -q awscliv2.zip
  ./aws/install
  rm -rf aws awscliv2.zip
fi

# Install Docker
if ! command -v docker &> /dev/null; then
  curl -fsSL https://get.docker.com -o get-docker.sh
  sh get-docker.sh
  usermod -aG docker ubuntu
  rm get-docker.sh
fi

# Install kubectl
if ! command -v kubectl &> /dev/null; then
  curl -LO "https://dl.k8s.io/release/$(curl -L -s https://dl.k8s.io/release/stable.txt)/bin/linux/amd64/kubectl"
  install -o root -g root -m 0755 kubectl /usr/local/bin/kubectl
  rm kubectl
fi

# Create directory structure
mkdir -p /opt/ocpctl/{releases,profiles,addons,manifests,scripts}
mkdir -p /var/lib/ocpctl/{clusters,tmp}
mkdir -p /etc/ocpctl

echo "=== Bootstrap complete ==="
echo "Next steps:"
echo "1. Run bootstrap-dev-server.sh to complete setup"
echo "2. Deploy services with deploy-env.sh dev"
