# OCPCTL Worker Auto-Scaling

This Terraform module provisions auto-scaling infrastructure for OCPCTL worker instances.

## Features

- **Auto Scaling Group** - Scales worker instances from 1 to 10 based on job queue depth
- **Spot Instances** - Uses EC2 spot instances for cost savings (~70% cheaper than on-demand)
- **Target Tracking** - Automatically scales to maintain 2 pending jobs per worker
- **CloudWatch Alarms** - Alerts when queue exceeds capacity or no workers are active
- **Rolling Updates** - Graceful instance replacement when launch template changes

## Prerequisites

Before using this module, you must have:

1. **VPC** - Tagged with `Name={vpc_name}`
2. **Private Subnets** - Tagged with `Type=private`
3. **Security Group** - Tagged with `Name={project_name}-worker-sg`
4. **IAM Role** - Named `{project_name}-worker-role` with permissions for:
   - CloudWatch PutMetricData
   - S3 read access for worker binary
   - Database access (via security groups)

## Usage

```hcl
module "worker_autoscaling" {
  source = "./terraform/worker-autoscaling"

  # Network Configuration
  aws_region   = "us-east-1"
  vpc_name     = "ocpctl-vpc"
  project_name = "ocpctl"

  # Instance Configuration
  ami_id        = "ami-0c55b159cbfafe1f0"  # Amazon Linux 2023
  instance_type = "t3.small"
  spot_max_price = ""  # Empty for on-demand pricing

  # Auto Scaling Configuration
  asg_min_size         = 1
  asg_max_size         = 10
  asg_desired_capacity = 1
  pending_jobs_per_worker = 2

  # Worker Configuration
  database_url          = "postgresql://user:pass@host:5432/ocpctl"
  work_dir              = "/var/lib/ocpctl"
  worker_poll_interval  = 10
  worker_max_concurrent = 3
  worker_binary_url     = "s3://my-bucket/ocpctl-worker"

  # Notifications (optional)
  sns_topic_arn = "arn:aws:sns:us-east-1:123456789012:ocpctl-alerts"

  tags = {
    ManagedBy = "Terraform"
    Project   = "OCPCTL"
  }
}
```

## Auto-Scaling Behavior

The Auto Scaling Group uses a **Target Tracking Scaling Policy** based on the `OCPCTL/PendingJobs` CloudWatch metric:

- **Target**: 2 pending jobs per worker instance
- **Scale Out**: When pending jobs > (current workers × 2)
- **Scale In**: When pending jobs < (current workers × 2)
- **Limits**: Min 1 worker, max 10 workers

### Example Scaling Scenarios

| Pending Jobs | Current Workers | Action |
|--------------|----------------|--------|
| 0 | 5 | Scale in to 1 (min size) |
| 3 | 1 | Scale out to 2 workers |
| 10 | 2 | Scale out to 5 workers |
| 20 | 10 | Stay at 10 (max size), alarm triggers |

## CloudWatch Metrics

Workers publish these metrics to CloudWatch:

| Metric | Description | Dimensions |
|--------|-------------|------------|
| `PendingJobs` | Total pending jobs in queue | None |
| `WorkerActive` | Worker instance active (1 or 0) | WorkerID |

## CloudWatch Alarms

### High Pending Jobs
- **Trigger**: Pending jobs > (max workers × jobs per worker)
- **Evaluation**: 2 periods of 60 seconds
- **Meaning**: Job queue exceeds capacity even at max scale

### No Active Workers
- **Trigger**: Sum of WorkerActive < 1
- **Evaluation**: 2 periods of 120 seconds
- **Meaning**: All workers are down or failing

Both alarms send notifications to the SNS topic if configured.

## Security

- **IMDSv2 Required** - Instance metadata requires session tokens
- **Private Subnets** - Workers run in private subnets with no public IPs
- **Security Groups** - Controlled via existing worker security group
- **IAM Instance Profile** - Least privilege access to required AWS services

## Cost Optimization

- **Spot Instances** - ~70% cheaper than on-demand instances
- **Auto-Scaling** - Only run workers when needed
- **Small Instance Type** - t3.small is sufficient for most workloads

**Estimated Costs** (us-east-1, t3.small spot):
- 1 worker (min): ~$5/month
- 10 workers (max): ~$50/month
- Typical (3 workers): ~$15/month

## Worker Binary

The worker binary must be uploaded to S3 or made available via HTTPS:

```bash
# Build worker binary for Linux
GOOS=linux GOARCH=amd64 go build -o ocpctl-worker ./cmd/worker

# Upload to S3
aws s3 cp ocpctl-worker s3://my-bucket/ocpctl-worker

# Make public or use IAM role permissions for download
```

Update `worker_binary_url` to point to the S3 URL or HTTPS endpoint.

## Quick Start with Deployment Script

The easiest way to deploy is using the automated deployment script:

```bash
./scripts/deploy-worker-autoscaling.sh \
  --region us-east-1 \
  --vpc-name ocpctl-vpc \
  --s3-bucket my-ocpctl-bucket \
  --database-url "postgresql://user:pass@host:5432/ocpctl" \
  --apply
```

This script will:
1. Build the worker binary for Linux
2. Upload it to S3
3. Create IAM roles and security groups
4. Deploy the Terraform infrastructure

**Script Options:**
- `--region` - AWS region (default: us-east-1)
- `--vpc-name` - VPC name tag (required)
- `--s3-bucket` - S3 bucket for worker binary (required)
- `--database-url` - PostgreSQL connection URL (required)
- `--ami-id` - AMI ID (optional, auto-detects Amazon Linux 2023)
- `--skip-build` - Skip building worker binary
- `--skip-upload` - Skip uploading binary to S3
- `--skip-iam` - Skip IAM role creation
- `--skip-sg` - Skip security group creation
- `--apply` - Auto-approve Terraform apply (omit for plan-only)

## Manual Deployment

If you prefer to deploy manually:

1. **Build and Upload Worker Binary**:
   ```bash
   make build-linux
   aws s3 cp bin/ocpctl-worker-linux s3://my-bucket/binaries/ocpctl-worker
   ```

2. **Initialize Terraform**:
   ```bash
   cd terraform/worker-autoscaling
   terraform init
   ```

3. **Create terraform.tfvars**:
   ```hcl
   vpc_name         = "ocpctl-vpc"
   ami_id           = "ami-0c55b159cbfafe1f0"
   database_url     = "postgresql://..."
   worker_binary_url = "s3://bucket/binaries/ocpctl-worker"
   ```

4. **Plan and Apply**:
   ```bash
   terraform plan
   terraform apply
   ```

## Monitoring

### CloudWatch Dashboard

A CloudWatch dashboard is automatically created with the following widgets:
- **Job Queue Depth** - Pending jobs count over time
- **Active Worker Instances** - Number of workers currently running
- **Auto Scaling Group Size** - ASG desired/in-service/min/max capacity
- **Jobs per Worker** - Current ratio vs target (default: 2 jobs/worker)
- **Job Throughput** - Jobs started, completed, and failed (5-min intervals)
- **Job Duration** - Average, max, and p99 job execution time
- **Scaling Activity** - Recent scaling events log
- **Alarm Status** - Current state of CloudWatch alarms

Access the dashboard via the Terraform output:
```bash
terraform output dashboard_url
```

Or directly at:
```
https://console.aws.amazon.com/cloudwatch/home?region=us-east-1#dashboards:name=ocpctl-worker-metrics
```

### Command Line Monitoring

View auto-scaling activity from the command line:

```bash
# List instances in ASG
aws autoscaling describe-auto-scaling-groups \
  --auto-scaling-group-names ocpctl-worker-asg

# View CloudWatch metrics
aws cloudwatch get-metric-statistics \
  --namespace OCPCTL \
  --metric-name PendingJobs \
  --start-time 2026-03-13T00:00:00Z \
  --end-time 2026-03-13T23:59:59Z \
  --period 300 \
  --statistics Average

# View scaling history
aws autoscaling describe-scaling-activities \
  --auto-scaling-group-name ocpctl-worker-asg \
  --max-records 20
```

## Troubleshooting

### Workers not starting
- Check CloudWatch Logs for systemd service errors
- Verify worker binary URL is accessible from instances
- Check security group allows database access
- Verify IAM role has necessary permissions

### Not scaling up
- Check `OCPCTL/PendingJobs` metric is being published
- Verify scaling policy is active
- Check if max size limit reached
- Review Auto Scaling activity history

### Not scaling down
- Default scale-in cooldown is 300 seconds
- ASG won't scale below min_size (default: 1)
- Check for scale-in protection on instances

## Variables Reference

| Variable | Description | Default |
|----------|-------------|---------|
| `aws_region` | AWS region | us-east-1 |
| `vpc_name` | VPC name tag | (required) |
| `ami_id` | Amazon Linux 2023 AMI | (required) |
| `instance_type` | EC2 instance type | t3.small |
| `spot_max_price` | Max spot price (empty for on-demand) | "" |
| `asg_min_size` | Min workers | 1 |
| `asg_max_size` | Max workers | 10 |
| `asg_desired_capacity` | Initial workers | 1 |
| `pending_jobs_per_worker` | Target jobs per worker | 2 |
| `database_url` | PostgreSQL connection URL | (required) |
| `work_dir` | Worker work directory | /var/lib/ocpctl |
| `worker_poll_interval` | Poll interval (seconds) | 10 |
| `worker_max_concurrent` | Max concurrent jobs per worker | 3 |
| `worker_binary_url` | Worker binary download URL | (required) |
| `sns_topic_arn` | SNS topic for alarms | "" |

## Outputs Reference

| Output | Description |
|--------|-------------|
| `autoscaling_group_name` | ASG name |
| `autoscaling_group_arn` | ASG ARN |
| `launch_template_id` | Launch template ID |
| `scaling_policy_arn` | Scaling policy ARN |
| `high_pending_jobs_alarm_arn` | High pending jobs alarm ARN |
| `no_active_workers_alarm_arn` | No active workers alarm ARN |
