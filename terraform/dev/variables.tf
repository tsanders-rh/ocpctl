variable "aws_region" {
  description = "AWS region to deploy resources"
  type        = string
  default     = "us-east-1"
}

variable "project_name" {
  description = "Project name used for resource naming"
  type        = string
  default     = "ocpctl"
}

variable "environment" {
  description = "Environment name"
  type        = string
  default     = "dev"
}

variable "ami_id" {
  description = "AMI ID for dev server (Ubuntu 22.04 LTS)"
  type        = string
}

variable "instance_type" {
  description = "EC2 instance type for dev server"
  type        = string
  default     = "t3.medium"
}

variable "key_name" {
  description = "Name for the SSH key pair"
  type        = string
  default     = "ocpctl-dev-key"
}

variable "ssh_allowed_cidrs" {
  description = "CIDR blocks allowed to SSH to dev server"
  type        = list(string)
  default     = ["0.0.0.0/0"]  # TODO: Restrict this to your IP
}

variable "route53_zone_name" {
  description = "Route53 hosted zone name"
  type        = string
  default     = "mg.dog8code.com"
}

variable "dev_subdomain" {
  description = "Subdomain for dev environment"
  type        = string
  default     = "dev.ocpctl"
}

variable "postgres_version" {
  description = "PostgreSQL version"
  type        = string
  default     = "17.9"
}

variable "rds_instance_class" {
  description = "RDS instance class"
  type        = string
  default     = "db.t3.micro"
}

variable "rds_allocated_storage" {
  description = "Allocated storage for RDS in GB"
  type        = number
  default     = 20
}

variable "db_name" {
  description = "Database name"
  type        = string
  default     = "ocpctl_dev"
}

variable "db_username" {
  description = "Database master username"
  type        = string
  default     = "ocpctl_dev_admin"
}

variable "db_password" {
  description = "Database master password"
  type        = string
  sensitive   = true
}

variable "s3_bucket_binaries" {
  description = "S3 bucket name for binaries"
  type        = string
  default     = "ocpctl-dev-binaries"
}

variable "s3_bucket_artifacts" {
  description = "S3 bucket name for artifacts"
  type        = string
  default     = "ocpctl-dev-artifacts"
}

variable "tags" {
  description = "Tags to apply to all resources"
  type        = map(string)
  default = {
    Environment = "dev"
    ManagedBy   = "terraform"
    Project     = "ocpctl"
  }
}
