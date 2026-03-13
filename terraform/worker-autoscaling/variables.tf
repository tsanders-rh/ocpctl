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

variable "vpc_name" {
  description = "Name of the VPC to deploy into"
  type        = string
}

variable "ami_id" {
  description = "AMI ID for worker instances (Amazon Linux 2023)"
  type        = string
}

variable "instance_type" {
  description = "EC2 instance type for workers"
  type        = string
  default     = "t3.small"
}

variable "spot_max_price" {
  description = "Maximum spot price (empty for on-demand pricing)"
  type        = string
  default     = ""
}

variable "asg_min_size" {
  description = "Minimum number of worker instances"
  type        = number
  default     = 1
}

variable "asg_max_size" {
  description = "Maximum number of worker instances"
  type        = number
  default     = 10
}

variable "asg_desired_capacity" {
  description = "Desired number of worker instances at start"
  type        = number
  default     = 1
}

variable "pending_jobs_per_worker" {
  description = "Target number of pending jobs per worker for auto-scaling"
  type        = number
  default     = 2
}

variable "database_url" {
  description = "PostgreSQL database connection URL"
  type        = string
  sensitive   = true
}

variable "work_dir" {
  description = "Working directory for worker processes"
  type        = string
  default     = "/var/lib/ocpctl"
}

variable "worker_poll_interval" {
  description = "How often workers poll for jobs (seconds)"
  type        = number
  default     = 10
}

variable "worker_max_concurrent" {
  description = "Maximum concurrent jobs per worker"
  type        = number
  default     = 3
}

variable "worker_binary_url" {
  description = "S3 URL or HTTP URL to download worker binary"
  type        = string
}

variable "sns_topic_arn" {
  description = "SNS topic ARN for CloudWatch alarms (optional)"
  type        = string
  default     = ""
}

variable "tags" {
  description = "Tags to apply to all resources"
  type        = map(string)
  default = {
    ManagedBy = "Terraform"
    Project   = "OCPCTL"
  }
}
