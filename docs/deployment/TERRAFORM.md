# OCPCTL Terraform Infrastructure

**Purpose:** Guide for deploying ocpctl infrastructure using Terraform for reproducible, version-controlled deployments.

**Audience:** DevOps engineers, infrastructure teams, and users preferring Infrastructure as Code.

**Last Updated:** 2026-05-08

---

## Table of Contents

1. [When to Use Terraform](#when-to-use-terraform)
2. [Available Modules](#available-modules)
3. [Quick Start](#quick-start)
4. [Full Stack Deployment](#full-stack-deployment)
5. [Module-by-Module Guide](#module-by-module-guide)
6. [State Management](#state-management)
7. [Environment Organization](#environment-organization)
8. [Best Practices](#best-practices)
9. [Migration from Manual Deployment](#migration-from-manual-deployment)

---

## When to Use Terraform

### ✅ Use Terraform When:

- **Multiple Environments:** Managing dev, staging, and production
- **Team Collaboration:** Multiple team members deploying infrastructure
- **Reproducibility:** Need consistent, repeatable deployments
- **Version Control:** Want infrastructure changes tracked in Git
- **Compliance:** Audit trail and review process required
- **Automation:** CI/CD pipeline deployment
- **Complex Infrastructure:** Managing many resources (VPC, RDS, ASG, etc.)

### ⚠️ Use Manual Deployment When:

- **Learning/Testing:** First time trying ocpctl
- **Quick Proof of Concept:** < 1 day deployment
- **Single Environment:** Just one production instance
- **Small Scale:** < 5 clusters expected
- **Rapid Iteration:** Frequent experimental changes

---

## Available Modules

### 1. Worker Autoscaling Module (Production-Ready)

**Location:** `terraform/worker-autoscaling/`

**What it Deploys:**
- Auto Scaling Group (1-10 worker instances)
- Launch Template with spot instances
- CloudWatch alarms and dashboard
- Target tracking scaling policy

**When to Use:**
- Production deployments handling > 5 clusters
- Need automatic scaling based on workload
- Want cost optimization with spot instances
- Require monitoring and alerting

**Documentation:** [terraform/worker-autoscaling/README.md](../../terraform/worker-autoscaling/README.md)

### 2. Full Stack Module (Coming Soon)

**Planned Components:**
- VPC with public/private subnets
- RDS PostgreSQL instance
- API server EC2 instance
- Worker autoscaling group
- Application Load Balancer
- Route53 DNS records
- S3 buckets (artifacts, binaries)
- IAM roles and policies

**Status:** Manual deployment currently recommended for full stack

---

## Quick Start

### Prerequisites

**Install Terraform:**
```bash
# macOS
brew install terraform

# Linux (Ubuntu/Debian)
wget https://releases.hashicorp.com/terraform/1.7.0/terraform_1.7.0_linux_amd64.zip
unzip terraform_1.7.0_linux_amd64.zip
sudo mv terraform /usr/local/bin/
```

**Verify Installation:**
```bash
terraform --version
# Should return: Terraform v1.7.x or higher
```

**AWS Credentials:**
```bash
# Ensure AWS CLI is configured
aws sts get-caller-identity
```

### Deploy Worker Autoscaling (5 Minutes)

```bash
# 1. Navigate to module
cd terraform/worker-autoscaling

# 2. Create terraform.tfvars
cat > terraform.tfvars <<EOF
aws_region          = "us-east-1"
project_name        = "ocpctl"
vpc_name            = "default-vpc"
ami_id              = "ami-0c421724a94bba6d6"  # Amazon Linux 2023
database_url        = "postgres://user:pass@host:5432/ocpctl"
worker_binary_url   = "s3://your-bucket/binaries/ocpctl-worker"
openshift_pull_secret = "$(cat ~/pull-secret.json)"

asg_min_size         = 1
asg_max_size         = 10
asg_desired_capacity = 1
EOF

# 3. Initialize Terraform
terraform init

# 4. Review plan
terraform plan

# 5. Deploy
terraform apply

# 6. Verify
terraform output dashboard_url
```

---

## Full Stack Deployment

While a unified Terraform module for full stack deployment doesn't exist yet, you can create one using individual resources:

### Example Full Stack Configuration

**Directory Structure:**
```
terraform/
├── environments/
│   ├── dev/
│   │   ├── main.tf
│   │   ├── terraform.tfvars
│   │   └── backend.tf
│   ├── staging/
│   │   ├── main.tf
│   │   ├── terraform.tfvars
│   │   └── backend.tf
│   └── production/
│       ├── main.tf
│       ├── terraform.tfvars
│       └── backend.tf
└── modules/
    ├── network/         # VPC, subnets, NAT
    ├── database/        # RDS PostgreSQL
    ├── api-server/      # EC2 API server
    ├── worker-asg/      # Worker autoscaling
    └── storage/         # S3 buckets
```

### Sample Full Stack main.tf

```hcl
terraform {
  required_version = ">= 1.0"
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.0"
    }
  }
}

provider "aws" {
  region = var.aws_region
}

# Network Infrastructure
module "network" {
  source = "../../modules/network"

  project_name = var.project_name
  vpc_cidr     = "10.0.0.0/16"
  azs          = ["us-east-1a", "us-east-1b"]

  tags = var.tags
}

# Storage
module "storage" {
  source = "../../modules/storage"

  project_name      = var.project_name
  artifacts_bucket  = "${var.project_name}-artifacts-${data.aws_caller_identity.current.account_id}"
  binaries_bucket   = "${var.project_name}-binaries-${data.aws_caller_identity.current.account_id}"

  tags = var.tags
}

# Database
module "database" {
  source = "../../modules/database"

  project_name     = var.project_name
  vpc_id           = module.network.vpc_id
  subnet_ids       = module.network.private_subnet_ids
  instance_class   = "db.t3.micro"
  allocated_storage = 20

  # Retrieve password from Parameter Store
  master_password = data.aws_ssm_parameter.db_password.value

  tags = var.tags
}

# API Server
module "api_server" {
  source = "../../modules/api-server"

  project_name  = var.project_name
  vpc_id        = module.network.vpc_id
  subnet_id     = module.network.public_subnet_ids[0]
  instance_type = "t3.large"
  ami_id        = data.aws_ami.ubuntu.id
  key_name      = var.ssh_key_name

  database_url = "postgres://${module.database.username}:${data.aws_ssm_parameter.db_password.value}@${module.database.endpoint}/${module.database.database_name}"

  tags = var.tags
}

# Worker Autoscaling
module "worker_autoscaling" {
  source = "../../worker-autoscaling"

  project_name      = var.project_name
  vpc_name          = module.network.vpc_name
  ami_id            = data.aws_ami.amazon_linux_2023.id
  instance_type     = "t3.small"

  database_url      = "postgres://${module.database.username}:${data.aws_ssm_parameter.db_password.value}@${module.database.endpoint}/${module.database.database_name}"
  worker_binary_url = "s3://${module.storage.binaries_bucket}/binaries/ocpctl-worker"

  openshift_pull_secret = data.aws_ssm_parameter.pull_secret.value

  asg_min_size         = 1
  asg_max_size         = 10
  asg_desired_capacity = 1

  tags = var.tags
}

# Data sources
data "aws_caller_identity" "current" {}

data "aws_ssm_parameter" "db_password" {
  name            = "/ocpctl/database/password"
  with_decryption = true
}

data "aws_ssm_parameter" "pull_secret" {
  name            = "/ocpctl/pull-secret"
  with_decryption = true
}

data "aws_ami" "ubuntu" {
  most_recent = true
  owners      = ["099720109477"]  # Canonical

  filter {
    name   = "name"
    values = ["ubuntu/images/hvm-ssd/ubuntu-jammy-22.04-amd64-server-*"]
  }
}

data "aws_ami" "amazon_linux_2023" {
  most_recent = true
  owners      = ["amazon"]

  filter {
    name   = "name"
    values = ["al2023-ami-*-x86_64"]
  }
}
```

### Sample terraform.tfvars

```hcl
aws_region   = "us-east-1"
project_name = "ocpctl"
ssh_key_name = "ocpctl-production-key"

tags = {
  ManagedBy   = "Terraform"
  Project     = "OCPCTL"
  Environment = "production"
  Owner       = "platform-team"
}
```

---

## Module-by-Module Guide

### Network Module (DIY)

**What to Create:**
```hcl
# VPC
resource "aws_vpc" "main" {
  cidr_block           = "10.0.0.0/16"
  enable_dns_hostnames = true
  enable_dns_support   = true

  tags = {
    Name = "${var.project_name}-vpc"
  }
}

# Public Subnets (for NAT, ALB, API server)
resource "aws_subnet" "public" {
  count             = 2
  vpc_id            = aws_vpc.main.id
  cidr_block        = "10.0.${count.index}.0/24"
  availability_zone = data.aws_availability_zones.available.names[count.index]

  map_public_ip_on_launch = true

  tags = {
    Name = "${var.project_name}-public-${count.index + 1}"
  }
}

# Private Subnets (for workers, RDS)
resource "aws_subnet" "private" {
  count             = 2
  vpc_id            = aws_vpc.main.id
  cidr_block        = "10.0.${count.index + 10}.0/24"
  availability_zone = data.aws_availability_zones.available.names[count.index]

  tags = {
    Name = "${var.project_name}-private-${count.index + 1}"
  }
}

# Internet Gateway
resource "aws_internet_gateway" "main" {
  vpc_id = aws_vpc.main.id

  tags = {
    Name = "${var.project_name}-igw"
  }
}

# NAT Gateway (one per AZ for HA)
resource "aws_eip" "nat" {
  count  = 2
  domain = "vpc"
}

resource "aws_nat_gateway" "main" {
  count         = 2
  allocation_id = aws_eip.nat[count.index].id
  subnet_id     = aws_subnet.public[count.index].id

  tags = {
    Name = "${var.project_name}-nat-${count.index + 1}"
  }
}

# Route Tables
resource "aws_route_table" "public" {
  vpc_id = aws_vpc.main.id

  route {
    cidr_block = "0.0.0.0/0"
    gateway_id = aws_internet_gateway.main.id
  }

  tags = {
    Name = "${var.project_name}-public-rt"
  }
}

resource "aws_route_table" "private" {
  count  = 2
  vpc_id = aws_vpc.main.id

  route {
    cidr_block     = "0.0.0.0/0"
    nat_gateway_id = aws_nat_gateway.main[count.index].id
  }

  tags = {
    Name = "${var.project_name}-private-rt-${count.index + 1}"
  }
}
```

### Database Module (RDS PostgreSQL)

```hcl
resource "aws_db_subnet_group" "main" {
  name       = "${var.project_name}-db-subnet"
  subnet_ids = var.subnet_ids

  tags = {
    Name = "${var.project_name}-db-subnet-group"
  }
}

resource "aws_security_group" "rds" {
  name        = "${var.project_name}-rds-sg"
  description = "Security group for RDS PostgreSQL"
  vpc_id      = var.vpc_id

  ingress {
    from_port   = 5432
    to_port     = 5432
    protocol    = "tcp"
    cidr_blocks = [data.aws_vpc.main.cidr_block]
  }

  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }
}

resource "aws_db_instance" "main" {
  identifier     = "${var.project_name}-db"
  engine         = "postgres"
  engine_version = "15.4"
  instance_class = var.instance_class

  allocated_storage     = var.allocated_storage
  storage_type          = "gp3"
  storage_encrypted     = true

  db_name  = "ocpctl"
  username = "ocpctl_user"
  password = var.master_password

  db_subnet_group_name   = aws_db_subnet_group.main.name
  vpc_security_group_ids = [aws_security_group.rds.id]

  backup_retention_period = 7
  backup_window          = "03:00-04:00"
  maintenance_window     = "mon:04:00-mon:05:00"

  skip_final_snapshot = var.environment != "production"
  final_snapshot_identifier = var.environment == "production" ? "${var.project_name}-final-snapshot-${formatdate("YYYYMMDDhhmmss", timestamp())}" : null

  tags = var.tags
}
```

### Storage Module (S3)

```hcl
# Artifacts Bucket
resource "aws_s3_bucket" "artifacts" {
  bucket = var.artifacts_bucket

  tags = merge(var.tags, {
    Purpose = "Cluster artifacts storage"
  })
}

resource "aws_s3_bucket_versioning" "artifacts" {
  bucket = aws_s3_bucket.artifacts.id

  versioning_configuration {
    status = "Enabled"
  }
}

resource "aws_s3_bucket_server_side_encryption_configuration" "artifacts" {
  bucket = aws_s3_bucket.artifacts.id

  rule {
    apply_server_side_encryption_by_default {
      sse_algorithm = "AES256"
    }
  }
}

resource "aws_s3_bucket_lifecycle_configuration" "artifacts" {
  bucket = aws_s3_bucket.artifacts.id

  rule {
    id     = "delete-old-destroyed-clusters"
    status = "Enabled"

    filter {
      prefix = "clusters/"
    }

    expiration {
      days = 90
    }
  }
}

# Binaries Bucket
resource "aws_s3_bucket" "binaries" {
  bucket = var.binaries_bucket

  tags = merge(var.tags, {
    Purpose = "Worker binaries and profiles"
  })
}

resource "aws_s3_bucket_versioning" "binaries" {
  bucket = aws_s3_bucket.binaries.id

  versioning_configuration {
    status = "Enabled"
  }
}
```

---

## State Management

### Local State (Development)

**When to Use:** Single user, testing, development

```hcl
# No backend configuration - state stored locally in terraform.tfstate
```

**Pros:**
- Simple setup
- No dependencies

**Cons:**
- Not suitable for teams
- No locking
- Easily lost or corrupted

### S3 Backend (Recommended for Production)

**Setup:**

```bash
# Create S3 bucket for state
aws s3 mb s3://ocpctl-terraform-state-$(aws sts get-caller-identity --query Account --output text)

# Enable versioning
aws s3api put-bucket-versioning \
  --bucket ocpctl-terraform-state-$(aws sts get-caller-identity --query Account --output text) \
  --versioning-configuration Status=Enabled

# Create DynamoDB table for locking
aws dynamodb create-table \
  --table-name ocpctl-terraform-locks \
  --attribute-definitions AttributeName=LockID,AttributeType=S \
  --key-schema AttributeName=LockID,KeyType=HASH \
  --billing-mode PAY_PER_REQUEST
```

**backend.tf:**
```hcl
terraform {
  backend "s3" {
    bucket         = "ocpctl-terraform-state-123456789012"
    key            = "production/terraform.tfstate"
    region         = "us-east-1"
    encrypt        = true
    dynamodb_table = "ocpctl-terraform-locks"
  }
}
```

**Benefits:**
- ✅ State locking (prevents concurrent modifications)
- ✅ State encryption
- ✅ State versioning (rollback capability)
- ✅ Team collaboration
- ✅ Audit trail

---

## Environment Organization

### Recommended Structure

```
terraform/
├── environments/
│   ├── dev/
│   │   ├── main.tf
│   │   ├── backend.tf
│   │   ├── terraform.tfvars
│   │   └── outputs.tf
│   ├── staging/
│   │   ├── main.tf
│   │   ├── backend.tf
│   │   ├── terraform.tfvars
│   │   └── outputs.tf
│   └── production/
│       ├── main.tf
│       ├── backend.tf
│       ├── terraform.tfvars
│       └── outputs.tf
├── modules/
│   ├── network/
│   ├── database/
│   ├── api-server/
│   └── storage/
└── worker-autoscaling/  # Existing module
```

### Environment-Specific Variables

**environments/dev/terraform.tfvars:**
```hcl
aws_region   = "us-east-1"
project_name = "ocpctl-dev"
environment  = "dev"

# Smaller resources for dev
api_instance_type = "t3.medium"
db_instance_class = "db.t3.micro"
asg_max_size      = 3

tags = {
  Environment = "dev"
  ManagedBy   = "Terraform"
  Project     = "OCPCTL"
  CostCenter  = "733"
}
```

**environments/production/terraform.tfvars:**
```hcl
aws_region   = "us-east-1"
project_name = "ocpctl"
environment  = "production"

# Production-sized resources
api_instance_type = "t3.large"
db_instance_class = "db.t3.small"
asg_max_size      = 10
db_multi_az       = true
db_backup_retention = 30

tags = {
  Environment = "production"
  ManagedBy   = "Terraform"
  Project     = "OCPCTL"
  CostCenter  = "733"
  Compliance  = "required"
}
```

---

## Best Practices

### 1. Use Variables for Secrets

**Never hardcode secrets in .tf files:**

```hcl
# ❌ BAD
database_url = "postgres://user:password123@host:5432/db"

# ✅ GOOD - Retrieve from Parameter Store
data "aws_ssm_parameter" "db_password" {
  name            = "/ocpctl/database/password"
  with_decryption = true
}

variable "database_url" {
  description = "Database URL (retrieve from Parameter Store)"
  type        = string
  sensitive   = true
}
```

### 2. Tag Everything

```hcl
locals {
  common_tags = {
    ManagedBy   = "Terraform"
    Project     = "OCPCTL"
    Environment = var.environment
    Owner       = var.owner
    CostCenter  = var.cost_center
  }
}

resource "aws_instance" "api" {
  # ...
  tags = merge(local.common_tags, {
    Name = "${var.project_name}-api-server"
    Role = "api"
  })
}
```

### 3. Use Data Sources for AMIs

```hcl
# ❌ BAD - AMI ID hardcoded, breaks in other regions
ami_id = "ami-0c55b159cbfafe1f0"

# ✅ GOOD - Dynamically query latest AMI
data "aws_ami" "amazon_linux_2023" {
  most_recent = true
  owners      = ["amazon"]

  filter {
    name   = "name"
    values = ["al2023-ami-*-x86_64"]
  }

  filter {
    name   = "virtualization-type"
    values = ["hvm"]
  }
}

ami_id = data.aws_ami.amazon_linux_2023.id
```

### 4. Implement Lifecycle Policies

```hcl
resource "aws_instance" "api" {
  # ...

  lifecycle {
    create_before_destroy = true
    ignore_changes        = [ami_id]  # Prevent recreation on AMI updates
  }
}
```

### 5. Use terraform.tfvars.example

```bash
# Commit terraform.tfvars.example to git
cp terraform.tfvars terraform.tfvars.example

# Add terraform.tfvars to .gitignore
echo "terraform.tfvars" >> .gitignore
echo "*.tfstate*" >> .gitignore
echo ".terraform/" >> .gitignore
```

### 6. Run Plan Before Apply

```bash
# Always review changes
terraform plan -out=tfplan

# Review the plan
terraform show tfplan

# Apply only if plan looks good
terraform apply tfplan
```

### 7. Use Workspaces for Environments (Alternative)

```bash
# Create workspaces
terraform workspace new dev
terraform workspace new staging
terraform workspace new production

# Switch between environments
terraform workspace select production
terraform apply -var-file=production.tfvars
```

---

## Migration from Manual Deployment

### Step 1: Import Existing Resources

If you have manually created resources, import them into Terraform state:

```bash
# Import API server EC2 instance
terraform import aws_instance.api i-0123456789abcdef0

# Import RDS database
terraform import aws_db_instance.main ocpctl-db

# Import S3 buckets
terraform import aws_s3_bucket.artifacts ocpctl-artifacts-123456789012
```

### Step 2: Generate Configuration

Use `terraform show` to generate configuration from imported state:

```bash
terraform show -no-color > imported-resources.tf
```

Edit and organize the generated configuration into proper modules.

### Step 3: Validate State Matches

```bash
terraform plan
# Should show: "No changes. Infrastructure is up-to-date."
```

### Step 4: Gradually Adopt Terraform

- Start with new environments (dev/staging)
- Test extensively before production
- Import production resources carefully
- Keep manual deployment docs as fallback

---

## Troubleshooting

### Error: "Backend configuration changed"

**Cause:** Moved or renamed backend
**Solution:**
```bash
terraform init -migrate-state
```

### Error: "Resource already exists"

**Cause:** Manually created resource conflicts with Terraform
**Solution:**
```bash
# Import existing resource
terraform import <resource_type>.<name> <resource_id>
```

### Error: "Error locking state"

**Cause:** Previous operation didn't release lock
**Solution:**
```bash
# Force unlock (use carefully!)
terraform force-unlock <lock-id>
```

### State Drift Detected

**Cause:** Manual changes made outside Terraform
**Solution:**
```bash
# Refresh state
terraform apply -refresh-only

# Or import changes
terraform import <resource> <id>
```

---

## Additional Resources

- [Official Terraform Documentation](https://www.terraform.io/docs)
- [AWS Provider Documentation](https://registry.terraform.io/providers/hashicorp/aws/latest/docs)
- [Worker Autoscaling Module README](../../terraform/worker-autoscaling/README.md)
- [OCPCTL Prerequisites](PREREQUISITES.md)
- [OCPCTL Deployment Checklist](DEPLOYMENT_CHECKLIST.md)

---

**Document Version:** 1.0
**Last Updated:** 2026-05-08
**Terraform Version:** >= 1.0
**AWS Provider Version:** ~> 5.0
