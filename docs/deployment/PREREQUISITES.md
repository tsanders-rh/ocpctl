# OCPCTL Deployment Prerequisites

**Purpose:** Complete list of prerequisites for deploying ocpctl to AWS and provisioning OpenShift/Kubernetes clusters.

**Audience:** Anyone deploying ocpctl for the first time

**Estimated Setup Time:** 30-60 minutes (if creating resources from scratch)

---

## Quick Checklist

Use this checklist to verify you have everything before starting deployment:

```
AWS Requirements:
☐ AWS account with admin or power user access
☐ AWS CLI installed and configured
☐ IAM permissions for EC2, RDS, Route53, S3, IAM (see below)
☐ Route53 hosted zone for cluster domains
☐ SSH key pair in target AWS region
☐ Sufficient service quotas (EC2, VPC)

Local Machine:
☐ Operating system: Linux, macOS, or Windows WSL2
☐ Git installed
☐ SSH client installed
☐ Go 1.21+ (for building binaries)
☐ Node.js 18+ and npm 9+ (for web frontend)

Secrets & Credentials:
☐ OpenShift pull secret from console.redhat.com
☐ Database password generated (or Parameter Store configured)
☐ OCM token (if deploying ROSA clusters)
☐ JWT secret generated for API authentication

Optional but Recommended:
☐ Custom domain name
☐ SSL/TLS certificate (or Let's Encrypt setup)
☐ CloudWatch logging configured
☐ S3 buckets created (artifacts, binaries)
```

---

## 1. AWS Account Requirements

### 1.1 AWS Account Access

**Minimum Required:**
- AWS account ID
- IAM user or role with appropriate permissions
- Programmatic access (access key ID + secret access key)

**Verification:**
```bash
aws sts get-caller-identity
```

**Expected Output:**
```json
{
    "UserId": "AIDAXXXXXXXXXXXXXXXXX",
    "Account": "123456789012",
    "Arn": "arn:aws:iam::123456789012:user/your-username"
}
```

### 1.2 Required AWS Services

Ensure the following services are available in your target region:

- ✅ **EC2** (Elastic Compute Cloud)
- ✅ **VPC** (Virtual Private Cloud)
- ✅ **RDS** (Relational Database Service) - Optional, can use PostgreSQL on EC2
- ✅ **Route53** (DNS service)
- ✅ **S3** (Simple Storage Service)
- ✅ **IAM** (Identity and Access Management)
- ✅ **Systems Manager Parameter Store**
- ✅ **ELB** (Elastic Load Balancing)
- ✅ **EFS** (Elastic File System) - Optional, for shared storage

**Verification:**
```bash
# Check EC2 is accessible
aws ec2 describe-regions --region us-east-1

# Check Route53
aws route53 list-hosted-zones

# Check S3
aws s3 ls

# Check Parameter Store
aws ssm describe-parameters
```

### 1.3 IAM Permissions for Deployment User

The IAM user/role deploying ocpctl needs these permissions:

**For Platform Deployment:**
```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Sid": "EC2ManagementForPlatform",
      "Effect": "Allow",
      "Action": [
        "ec2:RunInstances",
        "ec2:TerminateInstances",
        "ec2:DescribeInstances",
        "ec2:CreateSecurityGroup",
        "ec2:AuthorizeSecurityGroupIngress",
        "ec2:DescribeSecurityGroups",
        "ec2:CreateTags",
        "ec2:DescribeKeyPairs",
        "ec2:DescribeImages",
        "ec2:DescribeSubnets",
        "ec2:DescribeVpcs",
        "ec2:AssociateIamInstanceProfile",
        "ec2:DescribeIamInstanceProfileAssociations"
      ],
      "Resource": "*"
    },
    {
      "Sid": "RDSManagement",
      "Effect": "Allow",
      "Action": [
        "rds:CreateDBInstance",
        "rds:CreateDBSubnetGroup",
        "rds:DescribeDBInstances",
        "rds:DescribeDBSubnetGroups",
        "rds:AddTagsToResource"
      ],
      "Resource": "*"
    },
    {
      "Sid": "IAMManagement",
      "Effect": "Allow",
      "Action": [
        "iam:CreateRole",
        "iam:CreatePolicy",
        "iam:AttachRolePolicy",
        "iam:CreateInstanceProfile",
        "iam:AddRoleToInstanceProfile",
        "iam:PassRole",
        "iam:GetRole",
        "iam:GetPolicy",
        "iam:ListAttachedRolePolicies"
      ],
      "Resource": "*"
    },
    {
      "Sid": "ParameterStoreManagement",
      "Effect": "Allow",
      "Action": [
        "ssm:PutParameter",
        "ssm:GetParameter",
        "ssm:DescribeParameters",
        "ssm:AddTagsToResource"
      ],
      "Resource": "arn:aws:ssm:*:*:parameter/ocpctl/*"
    },
    {
      "Sid": "Route53Basic",
      "Effect": "Allow",
      "Action": [
        "route53:ListHostedZones",
        "route53:GetHostedZone",
        "route53:ChangeResourceRecordSets"
      ],
      "Resource": "*"
    }
  ]
}
```

**Simplified Alternative:**
- Managed Policy: `PowerUserAccess` (allows all except IAM)
- Plus custom policy for IAM role/policy creation

### 1.4 IAM Permissions for Worker (Cluster Provisioning)

The EC2 instance running ocpctl worker needs comprehensive permissions. Use the policies in `deploy/iam-policy-worker-full.json`.

**Summary of Required Permissions:**
- **EC2:** Full management (create/destroy VPCs, instances, volumes, etc.)
- **ELB:** Load balancer management
- **IAM:** Create roles, instance profiles, OIDC providers
- **Route53:** DNS zone and record management
- **S3:** Bucket management, object storage
- **EFS:** File system management
- **Service Quotas:** Read quotas
- **Tagging:** Resource tagging
- **SSM:** Parameter Store read access

**Installation:**
```bash
# Create managed policy from file
aws iam create-policy \
  --policy-name ocpctl-worker-full \
  --policy-document file://deploy/iam-policy-worker-full.json
```

See [IAM Policies Documentation](../deploy/IAM_POLICIES.md) for complete details.

### 1.5 AWS Service Quotas

**Required Service Quotas:**

| Service | Quota Name | Minimum Required | Check Command |
|---------|-----------|------------------|---------------|
| EC2 | Running On-Demand Standard instances | 20 | `aws service-quotas get-service-quota --service-code ec2 --quota-code L-1216C47A` |
| VPC | VPCs per Region | 10 | `aws service-quotas get-service-quota --service-code vpc --quota-code L-F678F1CE` |
| VPC | Internet gateways per Region | 10 | `aws service-quotas get-service-quota --service-code vpc --quota-code L-A4707A72` |
| VPC | NAT gateways per AZ | 10 | `aws service-quotas get-service-quota --service-code vpc --quota-code L-FE5A380F` |
| ELB | Network Load Balancers per Region | 50 | `aws service-quotas get-service-quota --service-code elasticloadbalancing --quota-code L-69A177A2` |
| Route53 | Hosted zones | 500 | Default, no check needed |
| S3 | Buckets | 100 | Default |

**Why These Quotas Matter:**
- Each OpenShift cluster creates 1 VPC, 1 IGW, 1-3 NAT Gateways, 2 NLBs
- 10 clusters = 10 VPCs, 20 NLBs, 10-30 NAT Gateways
- Default quotas are usually sufficient for small deployments
- Large deployments (50+ clusters) may need quota increases

**Request Quota Increase:**
```bash
# Example: Request EC2 instance quota increase to 50
aws service-quotas request-service-quota-increase \
  --service-code ec2 \
  --quota-code L-1216C47A \
  --desired-value 50
```

---

## 2. DNS and Route53 Requirements

### 2.1 Route53 Hosted Zone (Required)

You **must** have a Route53 hosted zone for the domain where clusters will be created.

**Why Required:**
- OpenShift installer creates DNS records for cluster API and apps
- DNS must be publicly resolvable for Let's Encrypt certificates
- Cluster nodes use DNS for service discovery

**Options:**

**Option A: Use Existing Domain**
```bash
# List existing hosted zones
aws route53 list-hosted-zones --query 'HostedZones[*].[Name,Id]' --output table

# Example output:
# your-company.com.  /hostedzone/Z1234567890ABC
```

**Option B: Register New Domain**
```bash
# Register domain through Route53 (costs $12-50/year depending on TLD)
aws route53domains register-domain \
  --region us-east-1 \
  --domain-name your-clusters.com \
  --duration-in-years 1 \
  --admin-contact file://contact.json \
  --registrant-contact file://contact.json \
  --tech-contact file://contact.json

# Hosted zone is created automatically
```

**Option C: Delegate Subdomain**

If you have a domain managed outside AWS, delegate a subdomain:

```bash
# Create hosted zone for subdomain
aws route53 create-hosted-zone \
  --name clusters.your-company.com \
  --caller-reference $(date +%s)

# Get nameservers
aws route53 get-hosted-zone --id /hostedzone/Z1234567890ABC \
  --query 'DelegationSet.NameServers'

# Add NS records in parent domain:
# clusters.your-company.com. NS ns-123.awsdns-12.com.
# clusters.your-company.com. NS ns-456.awsdns-34.net.
```

### 2.2 DNS Verification

**Verify DNS is working:**
```bash
export DOMAIN=your-domain.com

# Check nameservers
dig NS $DOMAIN +short

# Should return AWS Route53 nameservers:
# ns-123.awsdns-12.com.
# ns-456.awsdns-34.net.
# ns-789.awsdns-45.co.uk.
# ns-012.awsdns-56.org.

# Test DNS resolution
dig test.$DOMAIN +short
# Should either resolve or return NXDOMAIN (not timeout)
```

### 2.3 Cost Considerations

- **Hosted Zone:** $0.50/month per zone
- **DNS Queries:** $0.40/million queries (first billion queries free per month)
- **Typical Cost:** $0.50-2.00/month depending on query volume

---

## 3. Local Machine Requirements

### 3.1 Operating System

**Supported:**
- ✅ Linux (Ubuntu 20.04+, RHEL 8+, Fedora 35+)
- ✅ macOS 11+ (Intel or Apple Silicon)
- ✅ Windows 10/11 with WSL2 (Ubuntu recommended)

**Not Supported:**
- ❌ Windows natively (must use WSL2)
- ❌ macOS 10.x (too old)

### 3.2 Required Software

#### AWS CLI

**Version:** 2.x or higher

**Installation:**

**macOS:**
```bash
# Using Homebrew
brew install awscli

# Or download installer
curl "https://awscli.amazonaws.com/AWSCLIV2.pkg" -o "AWSCLIV2.pkg"
sudo installer -pkg AWSCLIV2.pkg -target /
```

**Linux:**
```bash
curl "https://awscli.amazonaws.com/awscli-exe-linux-x86_64.zip" -o "awscliv2.zip"
unzip awscliv2.zip
sudo ./aws/install
```

**Windows (WSL2):**
```bash
# Inside WSL terminal
curl "https://awscli.amazonaws.com/awscli-exe-linux-x86_64.zip" -o "awscliv2.zip"
unzip awscliv2.zip
sudo ./aws/install
```

**Verification:**
```bash
aws --version
# Should return: aws-cli/2.x.x
```

**Configuration:**
```bash
aws configure
# AWS Access Key ID: <your-access-key>
# AWS Secret Access Key: <your-secret-key>
# Default region name: us-east-1
# Default output format: json
```

#### Git

**Version:** 2.x or higher

**Installation:**
```bash
# macOS
brew install git

# Ubuntu/Debian
sudo apt-get update && sudo apt-get install -y git

# RHEL/Fedora
sudo dnf install -y git
```

**Verification:**
```bash
git --version
# Should return: git version 2.x.x
```

#### SSH Client

Usually pre-installed on Linux/macOS. On Windows, use WSL2 or install OpenSSH.

**Verification:**
```bash
ssh -V
# Should return: OpenSSH_8.x
```

#### Go (for building binaries)

**Version:** 1.21 or higher

**Installation:**

**macOS:**
```bash
brew install go
```

**Linux:**
```bash
# Download latest version
wget https://go.dev/dl/go1.22.0.linux-amd64.tar.gz

# Extract to /usr/local
sudo rm -rf /usr/local/go
sudo tar -C /usr/local -xzf go1.22.0.linux-amd64.tar.gz

# Add to PATH
echo 'export PATH=$PATH:/usr/local/go/bin' >> ~/.bashrc
source ~/.bashrc
```

**Verification:**
```bash
go version
# Should return: go version go1.22.x
```

#### Node.js and npm (for web frontend)

**Version:** Node.js 18.x or higher, npm 9.x or higher

**Installation:**

**macOS:**
```bash
brew install node@18
```

**Linux (using NodeSource):**
```bash
curl -fsSL https://deb.nodesource.com/setup_18.x | sudo -E bash -
sudo apt-get install -y nodejs
```

**Verification:**
```bash
node --version
# Should return: v18.x.x or higher

npm --version
# Should return: 9.x.x or higher
```

### 3.3 Optional Tools (Recommended)

#### jq (JSON processor)
```bash
# macOS
brew install jq

# Linux
sudo apt-get install -y jq
```

#### watch (for monitoring commands)
```bash
# macOS
brew install watch

# Linux (usually pre-installed)
sudo apt-get install -y procps
```

---

## 4. Secrets and Credentials

### 4.1 OpenShift Pull Secret (Required)

**Purpose:** Authenticate to Red Hat's container registries to download OpenShift images.

**How to Get:**
1. Go to https://console.redhat.com/openshift/install/pull-secret
2. Log in with Red Hat account (create free account if needed)
3. Click "Download pull secret"
4. Save to `~/pull-secret.json`

**Verification:**
```bash
# Check file exists
test -f ~/pull-secret.json && echo "Found" || echo "Missing"

# Validate JSON format
jq . ~/pull-secret.json > /dev/null 2>&1 && echo "Valid JSON" || echo "Invalid JSON"

# Check size (should be ~5-10 KB)
ls -lh ~/pull-secret.json
```

**Format:**
```json
{
  "auths": {
    "cloud.openshift.com": {
      "auth": "b3BlbnNo...",
      "email": "your-email@example.com"
    },
    "quay.io": {
      "auth": "b3BlbnNo...",
      "email": "your-email@example.com"
    },
    "registry.redhat.io": {
      "auth": "b3BlbnNo...",
      "email": "your-email@example.com"
    }
  }
}
```

### 4.2 Database Password

**Option A: Generate and Store in Parameter Store (Recommended)**

```bash
# Generate secure 32-character password
DB_PASSWORD=$(openssl rand -base64 32 | tr -d '/+=' | head -c 32)

# Store in AWS Systems Manager Parameter Store
aws ssm put-parameter \
  --name "/ocpctl/database/password" \
  --description "PostgreSQL password for ocpctl production database" \
  --value "$DB_PASSWORD" \
  --type "SecureString" \
  --tier "Standard"

# Verify
aws ssm get-parameter \
  --name "/ocpctl/database/password" \
  --with-decryption \
  --query 'Parameter.Name'
```

**Option B: Generate Locally**
```bash
# Generate password
openssl rand -base64 32 | tr -d '/+=' | head -c 32

# Save securely (use password manager)
```

### 4.3 JWT Secret (for API Authentication)

```bash
# Generate 48-byte random secret
openssl rand -base64 48

# Save output for use in /etc/ocpctl/api.env
```

### 4.4 OCM Token (for ROSA Clusters Only)

**Purpose:** Authenticate with Red Hat OpenShift Cluster Manager to provision ROSA clusters.

**How to Get:**
1. Go to https://console.redhat.com/openshift/token/rosa
2. Log in with Red Hat account
3. Copy "Offline Access Token"

**Verification:**
```bash
# Token should be ~400 characters
echo $OCM_TOKEN | wc -c
# Should return: ~400

# Test token (requires rosa CLI)
rosa whoami --token="$OCM_TOKEN"
```

**Note:** Only required if you plan to provision ROSA clusters. Not needed for standard OpenShift IPI, EKS, or GKE.

---

## 5. Network Requirements

### 5.1 Local Machine

**Outbound Access Required:**
- Port 443 (HTTPS) to:
  - AWS APIs (*.amazonaws.com)
  - GitHub (github.com, api.github.com)
  - Red Hat registries (quay.io, registry.redhat.io, console.redhat.com)
  - NPM registry (registry.npmjs.org)
  - Go module proxy (proxy.golang.org)

**No Inbound Access Required**

### 5.2 AWS Infrastructure

**VPC Requirements:**
- Default VPC is acceptable for testing
- Custom VPC must have:
  - Public subnets (for NAT Gateways, load balancers)
  - Private subnets (for cluster nodes)
  - Internet Gateway attached
  - NAT Gateway (for private subnet internet access)

**Security Groups:**
- ocpctl API server: Allow inbound 22 (SSH), 80 (HTTP), 443 (HTTPS)
- Database (if RDS): Allow inbound 5432 from API server security group
- Worker instances: Allow outbound to all (for provisioning clusters)

---

## 6. Storage Requirements

### 6.1 S3 Buckets

**Recommended (but optional):**
- **ocpctl-binaries:** Worker binaries, profiles, addons, scripts
- **ocpctl-artifacts:** Cluster kubeconfigs, installation logs

**Creation:**
```bash
# Create artifacts bucket
aws s3 mb s3://ocpctl-artifacts-$(aws sts get-caller-identity --query Account --output text)

# Create binaries bucket (if using autoscaling workers)
aws s3 mb s3://ocpctl-binaries-$(aws sts get-caller-identity --query Account --output text)

# Enable versioning (recommended)
aws s3api put-bucket-versioning \
  --bucket ocpctl-artifacts-$(aws sts get-caller-identity --query Account --output text) \
  --versioning-configuration Status=Enabled
```

**Cost:** ~$1-5/month depending on usage

### 6.2 Local Disk Space

**For Build Machine:**
- 10 GB free space (for Go modules, npm packages, compiled binaries)

**For Deployment:**
- Not applicable (building on local machine, deploying to EC2)

---

## 7. Knowledge Requirements

### 7.1 Recommended Skills

- **Required:**
  - Basic Linux/Unix command line
  - SSH and remote server management
  - AWS console navigation
  - Basic understanding of databases

- **Nice to Have:**
  - Docker and container concepts
  - Kubernetes/OpenShift basics
  - Nginx configuration
  - Git workflow

### 7.2 Documentation to Review

Before deploying, review these documents:

1. [AWS Quick Start Guide](AWS_QUICKSTART.md) - Full deployment walkthrough
2. [Deployment Checklist](DEPLOYMENT_CHECKLIST.md) - Verification steps
3. [IAM Policies](../deploy/IAM_POLICIES.md) - Permission requirements
4. [Cost Estimation](../operations/COST_ESTIMATION.md) - Budgeting

---

## 8. Time and Cost Estimates

### 8.1 Setup Time

| Task | Estimated Time |
|------|----------------|
| AWS account setup (if new) | 10-30 minutes |
| Route53 hosted zone setup | 5-10 minutes |
| Local machine tool installation | 10-20 minutes |
| Generate secrets and credentials | 5 minutes |
| IAM role/policy creation | 10-15 minutes |
| Total (first time) | **40-80 minutes** |

### 8.2 Recurring Costs

**Platform Infrastructure:**
- PostgreSQL on EC2: ~$73/month
- RDS PostgreSQL: ~$94/month
- With autoscaling workers: +$57/month (average)

**Per-Cluster Costs:**
- SNO (single node): ~$371/month (24/7) or ~$137/month (hibernated off-hours)
- Standard 3-node: ~$1,491/month (24/7) or ~$516/month (work hours only)

See [Cost Estimation Guide](../operations/COST_ESTIMATION.md) for complete breakdown.

---

## 9. Common Prerequisites Issues

### Issue: AWS CLI not configured
**Error:** `Unable to locate credentials`
**Solution:**
```bash
aws configure
# Enter your AWS access key and secret key
```

### Issue: Insufficient IAM permissions
**Error:** `AccessDenied` or `UnauthorizedOperation`
**Solution:**
- Attach `PowerUserAccess` managed policy (for deployment)
- Create custom policy using `deploy/iam-policy-worker-full.json` (for worker)

### Issue: Route53 hosted zone not found
**Error:** `HostedZoneNotFound`
**Solution:**
```bash
# Verify hosted zone exists
aws route53 list-hosted-zones

# Create if missing
aws route53 create-hosted-zone \
  --name your-domain.com \
  --caller-reference $(date +%s)
```

### Issue: Pull secret invalid
**Error:** `unauthorized: authentication required` during cluster creation
**Solution:**
- Re-download pull secret from https://console.redhat.com/openshift/install/pull-secret
- Ensure JSON is valid: `jq . ~/pull-secret.json`
- Verify file is not corrupted

### Issue: Service quota exceeded
**Error:** `InstanceLimitExceeded` or `VpcLimitExceeded`
**Solution:**
```bash
# Request quota increase
aws service-quotas request-service-quota-increase \
  --service-code ec2 \
  --quota-code L-1216C47A \
  --desired-value 50
```

---

## 10. Next Steps

After verifying all prerequisites:

1. **Choose Deployment Guide:**
   - [AWS Quick Start](AWS_QUICKSTART.md) - Fresh deployment (60 minutes)
   - [Production Deployment](AWS_PRODUCTION.md) - Enterprise deployment

2. **Prepare Deployment Environment:**
   - Clone ocpctl repository
   - Review configuration templates in `deploy/config/`
   - Plan infrastructure sizing

3. **Execute Deployment:**
   - Follow deployment guide step-by-step
   - Use [Deployment Checklist](DEPLOYMENT_CHECKLIST.md) to verify each phase

4. **Post-Deployment:**
   - Configure monitoring and alerts
   - Set up backup procedures
   - Test cluster provisioning

---

## Summary Checklist

Print this for reference during deployment:

```
[ ] AWS CLI installed and configured
[ ] AWS account with sufficient permissions
[ ] Route53 hosted zone for cluster domains
[ ] SSH key pair in AWS region
[ ] Service quotas verified (EC2, VPC)
[ ] Git installed
[ ] Go 1.21+ installed
[ ] Node.js 18+ and npm 9+ installed
[ ] OpenShift pull secret downloaded
[ ] Database password generated
[ ] JWT secret generated
[ ] OCM token (if using ROSA)
[ ] S3 buckets created (optional)
[ ] Reviewed deployment documentation
[ ] Budget allocated (~$150-500/month)
```

---

**Document Version:** 1.0
**Last Updated:** 2026-05-08
**Next Review:** 2026-06-01
