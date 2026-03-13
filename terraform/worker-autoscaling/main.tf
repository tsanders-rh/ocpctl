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

# Data sources
data "aws_vpc" "main" {
  filter {
    name   = "tag:Name"
    values = [var.vpc_name]
  }
}

data "aws_subnets" "private" {
  filter {
    name   = "vpc-id"
    values = [data.aws_vpc.main.id]
  }

  filter {
    name   = "tag:Type"
    values = ["private"]
  }
}

data "aws_security_group" "worker" {
  vpc_id = data.aws_vpc.main.id

  filter {
    name   = "tag:Name"
    values = ["${var.project_name}-worker-sg"]
  }
}

data "aws_iam_role" "worker" {
  name = "${var.project_name}-worker-role"
}

# EC2 Launch Template for worker instances
resource "aws_launch_template" "worker" {
  name_prefix   = "${var.project_name}-worker-"
  description   = "Launch template for OCPCTL worker instances"
  image_id      = var.ami_id
  instance_type = var.instance_type

  iam_instance_profile {
    arn = aws_iam_instance_profile.worker.arn
  }

  vpc_security_group_ids = [data.aws_security_group.worker.id]

  # Use spot instances for cost savings (workers are stateless)
  instance_market_options {
    market_type = "spot"
    spot_options {
      max_price          = var.spot_max_price
      spot_instance_type = "one-time"
    }
  }

  # IMDSv2 required for security
  metadata_options {
    http_endpoint               = "enabled"
    http_tokens                 = "required"
    http_put_response_hop_limit = 1
    instance_metadata_tags      = "enabled"
  }

  # User data script to start worker service
  user_data = base64encode(templatefile("${path.module}/user-data.sh", {
    database_url          = var.database_url
    work_dir              = var.work_dir
    worker_poll_interval  = var.worker_poll_interval
    worker_max_concurrent = var.worker_max_concurrent
    worker_binary_url     = var.worker_binary_url
  }))

  tag_specifications {
    resource_type = "instance"
    tags = merge(var.tags, {
      Name = "${var.project_name}-worker"
      Role = "worker"
    })
  }

  tag_specifications {
    resource_type = "volume"
    tags = merge(var.tags, {
      Name = "${var.project_name}-worker-volume"
    })
  }

  lifecycle {
    create_before_destroy = true
  }
}

# IAM instance profile for worker instances
resource "aws_iam_instance_profile" "worker" {
  name = "${var.project_name}-worker-instance-profile"
  role = data.aws_iam_role.worker.name

  tags = var.tags
}

# Auto Scaling Group
resource "aws_autoscaling_group" "worker" {
  name                = "${var.project_name}-worker-asg"
  vpc_zone_identifier = data.aws_subnets.private.ids
  min_size            = var.asg_min_size
  max_size            = var.asg_max_size
  desired_capacity    = var.asg_desired_capacity
  health_check_type   = "EC2"
  health_check_grace_period = 300

  launch_template {
    id      = aws_launch_template.worker.id
    version = "$Latest"
  }

  # Ensure smooth instance replacement
  instance_refresh {
    strategy = "Rolling"
    preferences {
      min_healthy_percentage = 50
    }
  }

  # Tags propagated to instances
  tag {
    key                 = "Name"
    value               = "${var.project_name}-worker"
    propagate_at_launch = true
  }

  tag {
    key                 = "ManagedBy"
    value               = "AutoScaling"
    propagate_at_launch = true
  }

  dynamic "tag" {
    for_each = var.tags
    content {
      key                 = tag.key
      value               = tag.value
      propagate_at_launch = true
    }
  }

  lifecycle {
    create_before_destroy = true
  }
}

# Target Tracking Scaling Policy - Scale based on pending jobs
resource "aws_autoscaling_policy" "target_tracking" {
  name                   = "${var.project_name}-worker-target-tracking"
  autoscaling_group_name = aws_autoscaling_group.worker.name
  policy_type            = "TargetTrackingScaling"

  target_tracking_configuration {
    customized_metric_specification {
      metric_dimension {
        name  = "AutoScalingGroupName"
        value = aws_autoscaling_group.worker.name
      }

      metric_name = "PendingJobs"
      namespace   = "OCPCTL"
      statistic   = "Average"
    }

    # Target: 2 pending jobs per worker instance
    target_value = var.pending_jobs_per_worker
  }
}

# CloudWatch Alarm for high pending jobs (manual notification)
resource "aws_cloudwatch_metric_alarm" "high_pending_jobs" {
  alarm_name          = "${var.project_name}-high-pending-jobs"
  comparison_operator = "GreaterThanThreshold"
  evaluation_periods  = 2
  metric_name         = "PendingJobs"
  namespace           = "OCPCTL"
  period              = 60
  statistic           = "Average"
  threshold           = var.asg_max_size * var.pending_jobs_per_worker
  alarm_description   = "Alert when pending jobs exceed capacity even at max workers"
  treat_missing_data  = "notBreaching"

  alarm_actions = var.sns_topic_arn != "" ? [var.sns_topic_arn] : []

  tags = var.tags
}

# CloudWatch Alarm for worker health (no active workers)
resource "aws_cloudwatch_metric_alarm" "no_active_workers" {
  alarm_name          = "${var.project_name}-no-active-workers"
  comparison_operator = "LessThanThreshold"
  evaluation_periods  = 2
  metric_name         = "WorkerActive"
  namespace           = "OCPCTL"
  period              = 120
  statistic           = "Sum"
  threshold           = 1
  alarm_description   = "Alert when no workers are active"
  treat_missing_data  = "breaching"

  alarm_actions = var.sns_topic_arn != "" ? [var.sns_topic_arn] : []

  tags = var.tags
}
