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

1. **VPC** - Tagged with `Name={vpc_name}` (default VPC works fine)
2. **Security Group** - Tagged with `Name={project_name}-worker-sg`
3. **IAM Role** - Named `{project_name}-worker-role` with permissions for:
   - CloudWatch PutMetricData
   - S3 read access for worker binary and profile definitions
   - Database access (via security groups)
4. **S3 Bucket** - For storing worker binary and profile definitions
5. **Database** - PostgreSQL accessible from VPC
6. **OpenShift Pull Secret** - Required for cluster provisioning

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
| `PendingJobs` | Total pending jobs in queue | AutoScalingGroupName |
| `WorkerActive` | Worker instance active (1 or 0) | WorkerID |

The `PendingJobs` metric includes the `AutoScalingGroupName` dimension, which is required for the auto-scaling policy to function correctly.

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

## Worker Binary and Profile Definitions

### Worker Binary

The worker binary must be uploaded to S3:

```bash
# Build worker binary for Linux
GOOS=linux GOARCH=amd64 go build -o ocpctl-worker ./cmd/worker

# Upload to S3
aws s3 cp ocpctl-worker s3://my-bucket/binaries/ocpctl-worker
```

Update `worker_binary_url` to point to the S3 URL: `s3://my-bucket/binaries/ocpctl-worker`

### Profile Definitions

Cluster profile definitions must also be uploaded to S3:

```bash
# Create tarball of profile definitions (excluding macOS metadata files)
COPYFILE_DISABLE=1 tar -czf profiles.tar.gz -C internal/profile definitions/

# Upload to S3
aws s3 cp profiles.tar.gz s3://my-bucket/binaries/profiles.tar.gz
```

The user-data script automatically downloads and extracts profiles to `/opt/ocpctl/profiles/definitions` on each worker instance.

### OpenShift Pull Secret

The pull secret is stored in a separate file for security and reliability:

1. **During Deployment**: Pass the pull secret via the `openshift_pull_secret` Terraform variable
2. **On Worker Instance**: Stored at `/etc/ocpctl/pull-secret.json` with restricted permissions (ocpctl:ocpctl, mode 400)
3. **Worker Configuration**: The `OPENSHIFT_PULL_SECRET_FILE` environment variable points to this file

This approach is more secure than storing secrets in environment variables and avoids parsing issues with complex JSON in systemd's EnvironmentFile.

## Quick Start - Real World Example

Here's a complete end-to-end deployment based on an actual production deployment:

### 1. Prepare Database

Configure PostgreSQL to accept connections from VPC:

```bash
# SSH to database server
ssh ec2-user@<db-server-ip>

# Edit PostgreSQL config
sudo vi /var/lib/pgsql/data/postgresql.conf
# Set: listen_addresses = '*'

sudo vi /var/lib/pgsql/data/pg_hba.conf
# Add: host all all 172.31.0.0/16 md5

# Restart PostgreSQL
sudo systemctl restart postgresql
```

Update security group to allow port 5432 from VPC CIDR (172.31.0.0/16).

### 2. Create S3 Bucket

```bash
# Create bucket for binaries
aws s3 mb s3://ocpctl-binaries-$(aws sts get-caller-identity --query Account --output text)
```

### 3. Build and Upload Worker Binary

```bash
cd /path/to/ocpctl

# Build worker for Linux
GOOS=linux GOARCH=amd64 go build -o ocpctl-worker cmd/worker/main.go

# Upload to S3
aws s3 cp ocpctl-worker s3://ocpctl-binaries-$(aws sts get-caller-identity --query Account --output text)/binaries/ocpctl-worker
```

### 4. Upload Profile Definitions

```bash
# Create tarball (important: exclude macOS metadata files)
COPYFILE_DISABLE=1 tar -czf profiles.tar.gz -C internal/profile definitions/

# Upload to S3
aws s3 cp profiles.tar.gz s3://ocpctl-binaries-$(aws sts get-caller-identity --query Account --output text)/binaries/profiles.tar.gz
```

### 5. Add CloudWatch Permissions to IAM Role

```bash
# Create policy document
cat > /tmp/cloudwatch-policy.json <<'EOF'
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Action": ["cloudwatch:PutMetricData"],
      "Resource": "*"
    }
  ]
}
EOF

# Attach to worker role
aws iam put-role-policy \
  --role-name ocpctl-worker-role \
  --policy-name CloudWatchMetrics \
  --policy-document file:///tmp/cloudwatch-policy.json
```

### 6. Configure Terraform

Create `terraform.tfvars`:

```hcl
aws_region         = "us-east-1"
project_name       = "ocpctl"
vpc_name           = "default-vpc"  # or your VPC name
ami_id             = "ami-0c421724a94bba6d6"  # Amazon Linux 2023
database_url       = "postgres://ocpctl:PASSWORD@172.31.93.45:5432/ocpctl?sslmode=disable"
worker_binary_url  = "s3://ocpctl-binaries-123456789012/binaries/ocpctl-worker"
openshift_pull_secret = "{\"auths\":{\"cloud.openshift.com\":{...}}}"

# Auto-scaling config
asg_min_size              = 1
asg_max_size              = 10
asg_desired_capacity      = 1
pending_jobs_per_worker   = 2

# Worker config
instance_type         = "t3.small"
work_dir              = "/var/lib/ocpctl"
worker_poll_interval  = 10
worker_max_concurrent = 3

tags = {
  ManagedBy = "Terraform"
  Project   = "OCPCTL"
}
```

### 7. Deploy Infrastructure

```bash
cd terraform/worker-autoscaling

# Initialize Terraform
terraform init

# Plan deployment
terraform plan

# Apply infrastructure
terraform apply
```

### 8. Verify Deployment

```bash
# Check worker instances
aws ec2 describe-instances \
  --filters "Name=tag:Name,Values=ocpctl-worker" "Name=instance-state-name,Values=running" \
  --query 'Reservations[*].Instances[*].[InstanceId,State.Name,PublicIpAddress]' \
  --output table

# Check CloudWatch metrics
aws cloudwatch list-metrics --namespace OCPCTL

# View dashboard
terraform output dashboard_url
```

### 9. (Optional) Configure SSH Access for Debugging

Add SSH key to launch template and update security group:

```hcl
# In main.tf, add to launch_template resource:
resource "aws_launch_template" "worker" {
  ...
  key_name = "your-ssh-key-name"
  ...
}
```

Then SSH to instance:
```bash
ssh -i ~/.ssh/your-key.pem ec2-user@<instance-ip>
sudo journalctl -u ocpctl-worker -f
```

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
- **Job Queue Depth** - Pending jobs count over time (average and peak)
- **Active Worker Instances** - Number of workers currently running (aggregated across all workers)
- **Auto Scaling Group Size** - ASG desired/in-service/min/max capacity
- **Jobs per Worker** - Current ratio vs target (default: 2 jobs/worker)
- **Job Throughput** - Jobs started, succeeded, failed, and retried (5-min intervals)
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

SSH to a worker instance to check logs:
```bash
# Get worker IP
aws ec2 describe-instances \
  --filters "Name=tag:Name,Values=ocpctl-worker" "Name=instance-state-name,Values=running" \
  --query 'Reservations[*].Instances[*].[InstanceId,PublicIpAddress]' \
  --output table

# SSH to instance (if SSH key configured in launch template)
ssh -i ~/.ssh/your-key.pem ec2-user@<instance-ip>

# Check worker service status
sudo systemctl status ocpctl-worker

# View worker logs
sudo journalctl -u ocpctl-worker -f
```

Common issues:
- **Database connection failed**: Check security group allows database access from VPC CIDR
- **Worker binary download failed**: Verify IAM role has S3 read permissions
- **Profile definitions missing**: Ensure profiles.tar.gz is uploaded to S3
- **Pull secret invalid**: Check pull secret JSON is valid and file permissions are correct (ocpctl:ocpctl, 400)

### Dashboard shows no metrics

- **PendingJobs metric missing**: Worker must detect Auto Scaling Group name from EC2 instance metadata
  - Verify IMDSv2 is enabled and `instance-metadata-tags` is enabled in launch template
  - Check metric has `AutoScalingGroupName` dimension: `aws cloudwatch list-metrics --namespace OCPCTL --metric-name PendingJobs`
- **WorkerActive metric missing**: Check worker has CloudWatch PutMetricData permissions
- **ASG metrics missing**: These are published by AWS automatically but may take 5-15 minutes to appear

### Not scaling up
- Check `OCPCTL/PendingJobs` metric is being published with AutoScalingGroupName dimension
- Verify scaling policy is active: `aws autoscaling describe-policies --auto-scaling-group-name ocpctl-worker-asg`
- Check if max size limit reached
- Review Auto Scaling activity history: `aws autoscaling describe-scaling-activities --auto-scaling-group-name ocpctl-worker-asg`

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
| `openshift_pull_secret` | OpenShift pull secret JSON | (required) |
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
