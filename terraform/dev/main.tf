terraform {
  required_version = ">= 1.0"
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.0"
    }
    tls = {
      source  = "hashicorp/tls"
      version = "~> 4.0"
    }
  }
}

provider "aws" {
  region = var.aws_region
}

# Data sources - use existing VPC and Route53 zone
data "aws_vpc" "default" {
  default = true
}

data "aws_subnets" "default" {
  filter {
    name   = "vpc-id"
    values = [data.aws_vpc.default.id]
  }
}

data "aws_route53_zone" "main" {
  name = var.route53_zone_name
}

# Create SSH key pair for dev server
resource "tls_private_key" "dev_key" {
  algorithm = "RSA"
  rsa_bits  = 4096
}

resource "aws_key_pair" "dev" {
  key_name   = var.key_name
  public_key = tls_private_key.dev_key.public_key_openssh

  tags = merge(var.tags, {
    Name = var.key_name
  })
}

# Security group for dev server
resource "aws_security_group" "dev_server" {
  name        = "${var.project_name}-dev-server-sg"
  description = "Security group for OCPCTL dev server"
  vpc_id      = data.aws_vpc.default.id

  # SSH access
  ingress {
    description = "SSH"
    from_port   = 22
    to_port     = 22
    protocol    = "tcp"
    cidr_blocks = var.ssh_allowed_cidrs
  }

  # HTTP
  ingress {
    description = "HTTP"
    from_port   = 80
    to_port     = 80
    protocol    = "tcp"
    cidr_blocks = ["0.0.0.0/0"]
  }

  # HTTPS
  ingress {
    description = "HTTPS"
    from_port   = 443
    to_port     = 443
    protocol    = "tcp"
    cidr_blocks = ["0.0.0.0/0"]
  }

  # All outbound traffic
  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }

  tags = merge(var.tags, {
    Name = "${var.project_name}-dev-server-sg"
  })
}

# Security group for RDS
resource "aws_security_group" "dev_rds" {
  name        = "${var.project_name}-dev-rds-sg"
  description = "Security group for OCPCTL dev RDS instance"
  vpc_id      = data.aws_vpc.default.id

  # PostgreSQL from dev server only
  ingress {
    description     = "PostgreSQL from dev server"
    from_port       = 5432
    to_port         = 5432
    protocol        = "tcp"
    security_groups = [aws_security_group.dev_server.id]
  }

  # All outbound traffic
  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }

  tags = merge(var.tags, {
    Name = "${var.project_name}-dev-rds-sg"
  })
}

# IAM role for dev server
resource "aws_iam_role" "dev_server" {
  name = "${var.project_name}-dev-server-role"

  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Action = "sts:AssumeRole"
        Effect = "Allow"
        Principal = {
          Service = "ec2.amazonaws.com"
        }
      }
    ]
  })

  tags = var.tags
}

# IAM policy for S3 access
resource "aws_iam_role_policy" "dev_server_s3" {
  name = "${var.project_name}-dev-server-s3-policy"
  role = aws_iam_role.dev_server.id

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Effect = "Allow"
        Action = [
          "s3:GetObject",
          "s3:PutObject",
          "s3:DeleteObject",
          "s3:ListBucket"
        ]
        Resource = [
          "arn:aws:s3:::${var.s3_bucket_binaries}",
          "arn:aws:s3:::${var.s3_bucket_binaries}/*",
          "arn:aws:s3:::${var.s3_bucket_artifacts}",
          "arn:aws:s3:::${var.s3_bucket_artifacts}/*"
        ]
      }
    ]
  })
}

# IAM policy for EC2 describe (for instance metadata)
resource "aws_iam_role_policy" "dev_server_ec2" {
  name = "${var.project_name}-dev-server-ec2-policy"
  role = aws_iam_role.dev_server.id

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Effect = "Allow"
        Action = [
          "ec2:DescribeInstances",
          "ec2:DescribeTags"
        ]
        Resource = "*"
      }
    ]
  })
}

# IAM instance profile
resource "aws_iam_instance_profile" "dev_server" {
  name = "${var.project_name}-dev-server-instance-profile"
  role = aws_iam_role.dev_server.name

  tags = var.tags
}

# EC2 instance for dev server
resource "aws_instance" "dev_server" {
  ami           = var.ami_id
  instance_type = var.instance_type
  key_name      = aws_key_pair.dev.key_name
  subnet_id     = tolist(data.aws_subnets.default.ids)[0]

  vpc_security_group_ids = [aws_security_group.dev_server.id]
  iam_instance_profile   = aws_iam_instance_profile.dev_server.name

  root_block_device {
    volume_size           = 50
    volume_type           = "gp3"
    encrypted             = true
    delete_on_termination = true
  }

  user_data = templatefile("${path.module}/user-data.sh", {
    hostname = "ocpctl-dev"
  })

  metadata_options {
    http_endpoint               = "enabled"
    http_tokens                 = "required"
    http_put_response_hop_limit = 1
    instance_metadata_tags      = "enabled"
  }

  tags = merge(var.tags, {
    Name = "${var.project_name}-dev"
  })
}

# Elastic IP for dev server
resource "aws_eip" "dev_server" {
  domain   = "vpc"
  instance = aws_instance.dev_server.id

  tags = merge(var.tags, {
    Name = "${var.project_name}-dev-eip"
  })
}

# Route53 DNS record
resource "aws_route53_record" "dev" {
  zone_id = data.aws_route53_zone.main.zone_id
  name    = var.dev_subdomain
  type    = "A"
  ttl     = 300
  records = [aws_eip.dev_server.public_ip]
}

# RDS subnet group
resource "aws_db_subnet_group" "dev" {
  name       = "${var.project_name}-dev-subnet-group"
  subnet_ids = data.aws_subnets.default.ids

  tags = merge(var.tags, {
    Name = "${var.project_name}-dev-subnet-group"
  })
}

# RDS instance
resource "aws_db_instance" "dev" {
  identifier     = "${var.project_name}-dev-db"
  engine         = "postgres"
  engine_version = var.postgres_version
  instance_class = var.rds_instance_class

  allocated_storage     = var.rds_allocated_storage
  storage_type          = "gp3"
  storage_encrypted     = true

  db_name  = var.db_name
  username = var.db_username
  password = var.db_password

  db_subnet_group_name   = aws_db_subnet_group.dev.name
  vpc_security_group_ids = [aws_security_group.dev_rds.id]

  backup_retention_period = 1
  backup_window           = "03:00-04:00"
  maintenance_window      = "sun:04:00-sun:05:00"

  skip_final_snapshot       = true
  deletion_protection       = false
  publicly_accessible       = false

  tags = merge(var.tags, {
    Name = "${var.project_name}-dev-db"
  })
}

# S3 bucket for binaries
resource "aws_s3_bucket" "dev_binaries" {
  bucket = var.s3_bucket_binaries

  tags = merge(var.tags, {
    Name = var.s3_bucket_binaries
  })
}

resource "aws_s3_bucket_versioning" "dev_binaries" {
  bucket = aws_s3_bucket.dev_binaries.id

  versioning_configuration {
    status = "Enabled"
  }
}

resource "aws_s3_bucket_lifecycle_configuration" "dev_binaries" {
  bucket = aws_s3_bucket.dev_binaries.id

  rule {
    id     = "delete-old-versions"
    status = "Enabled"

    filter {}

    noncurrent_version_expiration {
      noncurrent_days = 30
    }
  }
}

# S3 bucket for artifacts
resource "aws_s3_bucket" "dev_artifacts" {
  bucket = var.s3_bucket_artifacts

  tags = merge(var.tags, {
    Name = var.s3_bucket_artifacts
  })
}

resource "aws_s3_bucket_versioning" "dev_artifacts" {
  bucket = aws_s3_bucket.dev_artifacts.id

  versioning_configuration {
    status = "Enabled"
  }
}

resource "aws_s3_bucket_lifecycle_configuration" "dev_artifacts" {
  bucket = aws_s3_bucket.dev_artifacts.id

  rule {
    id     = "delete-old-artifacts"
    status = "Enabled"

    filter {}

    expiration {
      days = 30
    }
  }

  rule {
    id     = "delete-old-versions"
    status = "Enabled"

    filter {}

    noncurrent_version_expiration {
      noncurrent_days = 7
    }
  }
}
