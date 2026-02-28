# OpenShift Install Setup Guide

This guide covers setting up the `openshift-install` binary and obtaining the required pull secret for cluster provisioning.

## Table of Contents

- [Overview](#overview)
- [Prerequisites](#prerequisites)
- [Installing openshift-install](#installing-openshift-install)
- [Obtaining Pull Secret](#obtaining-pull-secret)
- [AWS Credentials Setup](#aws-credentials-setup)
- [Configuration](#configuration)
- [Verification](#verification)
- [Troubleshooting](#troubleshooting)

## Overview

The ocpctl worker service uses the `openshift-install` binary to provision OpenShift clusters on AWS and IBM Cloud. This binary requires:

1. **openshift-install binary** - Official OpenShift installer
2. **Pull secret** - Authentication for pulling OpenShift images
3. **Cloud credentials** - AWS or IBM Cloud credentials

## Prerequisites

### For All Platforms

- **Red Hat account** - Required to download installer and pull secret
- **Cloud provider account** - AWS or IBM Cloud with appropriate permissions

### For AWS

- **AWS CLI** configured with credentials
- **IAM permissions** for creating VPCs, EC2 instances, ELBs, Route53 records, etc.
- **Service quotas** sufficient for cluster creation

### For IBM Cloud

- **IBM Cloud CLI** installed and configured
- **API key** with appropriate permissions
- **Resource groups** and quotas configured

## Installing openshift-install

### Option 1: Download from Red Hat (Recommended)

#### 1. Get the Latest Stable Version

Visit the OpenShift mirror and download the appropriate version for your OS:

**Linux (x86_64)**:
```bash
# Download the latest stable version
VERSION=stable-4.16  # or specific version like 4.16.0
wget https://mirror.openshift.com/pub/openshift-v4/clients/ocp/${VERSION}/openshift-install-linux.tar.gz

# Extract
tar xvf openshift-install-linux.tar.gz

# Move to PATH
sudo mv openshift-install /usr/local/bin/

# Make executable
sudo chmod +x /usr/local/bin/openshift-install

# Verify installation
openshift-install version
```

**macOS**:
```bash
# Download for macOS
VERSION=stable-4.16
wget https://mirror.openshift.com/pub/openshift-v4/clients/ocp/${VERSION}/openshift-install-darwin.tar.gz

# Extract
tar xvf openshift-install-darwin.tar.gz

# Move to PATH
sudo mv openshift-install /usr/local/bin/

# Make executable
sudo chmod +x /usr/local/bin/openshift-install

# Verify installation
openshift-install version
```

#### 2. Version-Specific Downloads

For a specific OpenShift version:

```bash
# Example: OpenShift 4.16.0
VERSION=4.16.0
wget https://mirror.openshift.com/pub/openshift-v4/clients/ocp/${VERSION}/openshift-install-linux.tar.gz
```

Browse available versions at: https://mirror.openshift.com/pub/openshift-v4/clients/ocp/

### Option 2: Using Package Managers

**macOS (Homebrew)**:
```bash
brew install openshift-install
```

**Linux (Custom Script)**:
```bash
# Download and install script
curl -LO https://mirror.openshift.com/pub/openshift-v4/clients/ocp/stable/openshift-install-linux.tar.gz
tar -xzf openshift-install-linux.tar.gz
sudo install -m 755 openshift-install /usr/local/bin/
```

### Verify Installation

```bash
# Check version
openshift-install version

# Expected output:
# openshift-install 4.16.0
# built from commit ...
# release image ...
```

## Obtaining Pull Secret

The pull secret authenticates the installer to pull OpenShift container images from Red Hat registries.

### Steps to Get Pull Secret

1. **Visit Red Hat Cloud Console**:
   - Go to https://console.redhat.com/openshift/install/pull-secret
   - Log in with your Red Hat account

2. **Copy Pull Secret**:
   - Click "Copy" or "Download" button
   - The secret is a JSON object containing authentication credentials

3. **Save Pull Secret**:

**Option A: Environment Variable** (Recommended for ocpctl):
```bash
# Add to .env file
cat >> .env << 'EOF'
OPENSHIFT_PULL_SECRET='{"auths":{"cloud.openshift.com":{"auth":"...","email":"..."},...}}'
EOF
```

**Option B: File on Disk**:
```bash
# Save to file
cat > ~/pull-secret.json << 'EOF'
{
  "auths": {
    "cloud.openshift.com": {
      "auth": "base64-encoded-credentials",
      "email": "your-email@example.com"
    },
    "quay.io": {
      "auth": "base64-encoded-credentials",
      "email": "your-email@example.com"
    },
    "registry.connect.redhat.com": {
      "auth": "base64-encoded-credentials",
      "email": "your-email@example.com"
    },
    "registry.redhat.io": {
      "auth": "base64-encoded-credentials",
      "email": "your-email@example.com"
    }
  }
}
EOF

# Set environment variable to point to file
echo 'OPENSHIFT_PULL_SECRET_FILE=~/pull-secret.json' >> .env
```

### Pull Secret Format

The pull secret is a JSON object with this structure:

```json
{
  "auths": {
    "cloud.openshift.com": {
      "auth": "base64-encoded-token",
      "email": "your-email@redhat.com"
    },
    "quay.io": {
      "auth": "base64-encoded-token",
      "email": "your-email@redhat.com"
    },
    "registry.connect.redhat.com": {
      "auth": "base64-encoded-token",
      "email": "your-email@redhat.com"
    },
    "registry.redhat.io": {
      "auth": "base64-encoded-token",
      "email": "your-email@redhat.com"
    }
  }
}
```

**Important Notes**:
- Keep the pull secret secure - it contains authentication credentials
- Do NOT commit the pull secret to version control
- Add `pull-secret.json` to `.gitignore`
- Pull secrets can be regenerated from the Red Hat console if compromised

## AWS Credentials Setup

### Option 1: AWS CLI (Recommended for Local Development)

```bash
# Install AWS CLI
# macOS
brew install awscli

# Linux
curl "https://awscli.amazonaws.com/awscli-exe-linux-x86_64.zip" -o "awscliv2.zip"
unzip awscliv2.zip
sudo ./aws/install

# Configure credentials
aws configure
# Enter:
# - AWS Access Key ID
# - AWS Secret Access Key
# - Default region (e.g., us-east-1)
# - Default output format (json)

# Verify
aws sts get-caller-identity
```

### Option 2: Environment Variables

Add to `.env`:

```bash
AWS_ACCESS_KEY_ID=AKIAIOSFODNN7EXAMPLE
AWS_SECRET_ACCESS_KEY=wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY
AWS_DEFAULT_REGION=us-east-1
```

### Option 3: EC2 Instance Role (Production)

For production deployments on EC2:

1. Create IAM role with required permissions
2. Attach role to EC2 instance
3. No explicit credentials needed - SDK uses instance metadata

### Required AWS Permissions

The AWS credentials need permissions to create:

- **VPC** and networking (subnets, route tables, internet gateways)
- **EC2** instances (control plane and worker nodes)
- **ELB** load balancers
- **Route53** DNS records
- **S3** buckets (for bootstrap ignition)
- **IAM** roles and instance profiles
- **Security Groups**

Minimum policy (use AWS-managed `AdministratorAccess` for development):

```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Action": [
        "ec2:*",
        "elasticloadbalancing:*",
        "iam:*",
        "route53:*",
        "s3:*"
      ],
      "Resource": "*"
    }
  ]
}
```

## Configuration

### Update ocpctl Environment

Edit `.env` file:

```bash
# OpenShift Installation
OPENSHIFT_PULL_SECRET='{"auths":{...}}'  # Your actual pull secret JSON
OPENSHIFT_INSTALL_BINARY=/usr/local/bin/openshift-install  # Path to binary (optional)

# AWS Configuration
AWS_REGION=us-east-1
AWS_ACCESS_KEY_ID=your-access-key-id
AWS_SECRET_ACCESS_KEY=your-secret-access-key

# Worker Configuration
WORK_DIR=/tmp/ocpctl  # Directory for cluster installation files
WORKER_CONCURRENCY=3  # Number of concurrent cluster operations
```

### Update Worker Service

If running as systemd service, add environment file:

```bash
# /etc/ocpctl/worker.env
OPENSHIFT_PULL_SECRET='{"auths":{...}}'
AWS_REGION=us-east-1
AWS_ACCESS_KEY_ID=...
AWS_SECRET_ACCESS_KEY=...
```

Update systemd service file:

```ini
[Service]
EnvironmentFile=/etc/ocpctl/worker.env
```

## Verification

### Test openshift-install

```bash
# Check version
openshift-install version

# Test help
openshift-install create cluster --help
```

### Test Pull Secret

```bash
# Validate JSON
echo $OPENSHIFT_PULL_SECRET | jq .

# Should output valid JSON with "auths" key
```

### Test AWS Credentials

```bash
# Verify identity
aws sts get-caller-identity

# Should output:
# {
#     "UserId": "AIDAI...",
#     "Account": "123456789012",
#     "Arn": "arn:aws:iam::123456789012:user/your-user"
# }

# Check permissions (create a test cluster to fully validate)
# or verify specific permissions:
aws ec2 describe-regions
aws s3 ls
```

### Test Full Stack

Create a test cluster through ocpctl:

```bash
# Start API server
make run-api

# Start worker
make run-worker

# In another terminal, create a test cluster via API or web UI
curl -X POST http://localhost:8080/api/v1/clusters \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "test-cluster",
    "platform": "aws",
    "profile": "aws-minimal-test",
    "region": "us-east-1",
    "version": "4.16.0",
    "owner": "admin@localhost",
    "team": "test",
    "cost_center": "test-001",
    "ttl_hours": 8
  }'

# Monitor logs
# API logs show cluster creation request
# Worker logs show job pickup and openshift-install execution
```

## Troubleshooting

### openshift-install: command not found

**Problem**: Binary not in PATH

**Solution**:
```bash
# Check if binary exists
which openshift-install

# If not found, verify installation
ls -la /usr/local/bin/openshift-install

# Add to PATH if needed
export PATH=$PATH:/usr/local/bin
```

### Pull Secret Errors

**Problem**: `error: unable to pull image` or authentication errors

**Solution**:
```bash
# Validate pull secret JSON
echo $OPENSHIFT_PULL_SECRET | jq .

# Regenerate pull secret from Red Hat console if corrupted

# Ensure no extra whitespace or newlines
OPENSHIFT_PULL_SECRET=$(cat pull-secret.json | tr -d '\n')
```

### AWS Credential Errors

**Problem**: `error: NoCredentialProviders` or permission denied

**Solution**:
```bash
# Verify credentials
aws sts get-caller-identity

# Check environment variables
echo $AWS_ACCESS_KEY_ID
echo $AWS_SECRET_ACCESS_KEY

# Re-configure AWS CLI
aws configure

# Check IAM permissions
aws iam get-user
```

### Cluster Installation Fails

**Problem**: `openshift-install create cluster` fails

**Solutions**:

1. **Check AWS Service Quotas**:
   ```bash
   # View quotas
   aws service-quotas list-service-quotas --service-code ec2

   # Request quota increase if needed
   ```

2. **Verify Region Support**:
   - Not all regions support all instance types
   - Check profile's instance types are available in selected region

3. **Review Installation Logs**:
   ```bash
   # Logs are in work directory
   tail -f /tmp/ocpctl/<cluster-id>/.openshift_install.log
   ```

4. **Check Bootstrap Errors**:
   ```bash
   # Bootstrap logs
   cat /tmp/ocpctl/<cluster-id>/.openshift_install_state.json
   ```

### Worker Not Processing Jobs

**Problem**: Jobs stuck in PENDING status

**Solution**:
```bash
# Check worker logs
journalctl -u ocpctl-worker -f

# Verify environment variables loaded
systemctl show ocpctl-worker | grep Environment

# Check database connectivity
psql $DATABASE_URL -c "SELECT * FROM jobs WHERE status = 'PENDING';"

# Restart worker
systemctl restart ocpctl-worker
```

## Additional Resources

- **OpenShift Documentation**: https://docs.openshift.com/container-platform/latest/installing/index.html
- **OpenShift Installer GitHub**: https://github.com/openshift/installer
- **Red Hat Console**: https://console.redhat.com/openshift
- **AWS Permissions Calculator**: https://github.com/openshift/installer/blob/master/docs/user/aws/iam.md

## Security Best Practices

1. **Rotate Credentials Regularly**:
   - Regenerate pull secret every 90 days
   - Rotate AWS credentials

2. **Use Least Privilege**:
   - Create dedicated IAM user for ocpctl
   - Limit permissions to only what's needed

3. **Secure Storage**:
   - Never commit secrets to git
   - Use AWS Secrets Manager or HashiCorp Vault in production
   - Encrypt environment files

4. **Audit Logging**:
   - Enable CloudTrail for AWS API calls
   - Monitor cluster creation activities
   - Alert on suspicious patterns

## Production Considerations

### Using AWS Secrets Manager

Instead of environment variables, use AWS Secrets Manager:

```go
// Fetch pull secret from AWS Secrets Manager
import (
    "github.com/aws/aws-sdk-go/aws/session"
    "github.com/aws/aws-sdk-go/service/secretsmanager"
)

sess := session.Must(session.NewSession())
svc := secretsmanager.New(sess)

result, err := svc.GetSecretValue(&secretsmanager.GetSecretValueInput{
    SecretId: aws.String("ocpctl/pull-secret"),
})

pullSecret := *result.SecretString
```

### Binary Version Management

Pin to specific versions in production:

```bash
# Lock to specific version
OPENSHIFT_VERSION=4.16.0
OPENSHIFT_INSTALL_BINARY=/usr/local/bin/openshift-install-${OPENSHIFT_VERSION}
```

### Work Directory Management

Configure proper cleanup and retention:

```bash
# Worker configuration
WORK_DIR=/var/lib/ocpctl/workdir
RETAIN_WORK_DIR_DAYS=7  # Clean up after 7 days
```

## Getting Help

If you encounter issues:

1. Check worker logs: `journalctl -u ocpctl-worker -f`
2. Check installer logs: `tail -f $WORK_DIR/<cluster-id>/.openshift_install.log`
3. Verify prerequisites: Run verification steps above
4. Consult OpenShift documentation
5. Open an issue on the ocpctl GitHub repository
