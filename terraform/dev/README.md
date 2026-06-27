# OCPCTL Dev Environment Terraform Configuration

This Terraform configuration provisions the complete dev/test environment for OCPCTL.

## Resources Created

- **EC2 Instance** (t3.medium): Dev server running API and Worker services
- **Elastic IP**: Static public IP for dev server
- **RDS PostgreSQL** (db.t3.micro): Isolated dev database
- **S3 Buckets**: ocpctl-dev-binaries, ocpctl-dev-artifacts
- **Security Groups**: Dev server and RDS access control
- **IAM Role**: EC2 instance profile with S3/EC2 permissions
- **Route53 Record**: dev.ocpctl.mg.dog8code.com → dev server IP
- **SSH Key Pair**: ocpctl-dev-key for server access

## Prerequisites

1. AWS CLI configured with appropriate credentials
2. Terraform >= 1.0 installed
3. Access to AWS account with permissions for EC2, RDS, S3, IAM, Route53

## Usage

### 1. Review Configuration

Edit `terraform.tfvars` to customize:
- `ssh_allowed_cidrs`: Restrict SSH access to your IP (security best practice)
- `db_password`: Change the generated password if desired
- Other settings as needed

### 2. Initialize Terraform

```bash
cd terraform/dev
terraform init
```

### 3. Plan Deployment

```bash
terraform plan
```

Review the plan to ensure it matches your expectations. This will create:
- 1 EC2 instance
- 1 RDS instance
- 2 S3 buckets
- 1 Elastic IP
- Security groups, IAM roles, etc.

### 4. Apply Configuration

```bash
terraform apply
```

Type `yes` when prompted. Deployment takes ~10-15 minutes (RDS creation is slow).

### 5. Save Outputs

After successful deployment:

```bash
# Save SSH private key
terraform output -raw ssh_private_key > ~/.ssh/ocpctl-dev-key
chmod 600 ~/.ssh/ocpctl-dev-key

# View all outputs
terraform output

# Get specific values
terraform output dev_server_public_ip
terraform output database_url
```

### 6. Verify Infrastructure

```bash
# SSH to dev server
ssh -i ~/.ssh/ocpctl-dev-key ubuntu@$(terraform output -raw dev_server_public_ip)

# Test database connectivity (from dev server)
psql "$(terraform output -raw database_url)" -c "SELECT version();"
```

## Next Steps

After infrastructure is provisioned:

1. **Update deploy-env.sh**
   ```bash
   # In scripts/deploy-env.sh, update line 35:
   API_HOST="$(terraform output -raw dev_server_public_ip)"
   ```

2. **Create config files**
   ```bash
   cp config/api.env.dev.template config/api.env.dev
   cp config/worker.env.dev.template config/worker.env.dev

   # Update with values from terraform output
   # DATABASE_URL, S3_BUCKET_NAME, etc.
   ```

3. **Bootstrap dev server**
   ```bash
   ./scripts/bootstrap-dev-server.sh $(terraform output -raw dev_server_public_ip)
   ```

4. **Initialize database**
   ```bash
   ./scripts/init-dev-database.sh
   ```

5. **Deploy services**
   ```bash
   ./scripts/deploy-env.sh dev
   ```

6. **Access dev environment**
   ```
   https://dev.ocpctl.mg.dog8code.com
   ```

## Cost Estimates

| Resource | Monthly Cost |
|----------|--------------|
| EC2 t3.medium | ~$30 |
| RDS db.t3.micro | ~$15 |
| S3 Storage | ~$1 |
| Data Transfer | ~$1 |
| **Total** | **~$47/month** |

### Cost Optimization

Stop dev server when not in use:
```bash
aws ec2 stop-instances --instance-ids $(terraform output -raw dev_server_instance_id)

# Restart when needed
aws ec2 start-instances --instance-ids $(terraform output -raw dev_server_instance_id)
```

**Savings**: ~60% on EC2 costs (~$18/month)

## Maintenance

### Update Infrastructure

After modifying .tf files:
```bash
terraform plan
terraform apply
```

### Destroy Environment

To completely remove all resources:
```bash
terraform destroy
```

⚠️ **Warning**: This will permanently delete:
- Dev server and all data
- RDS database (no final snapshot)
- S3 buckets and contents
- DNS records

## Troubleshooting

### SSH Connection Issues

```bash
# Verify security group allows your IP
aws ec2 describe-security-groups \
  --group-ids $(terraform output -json | jq -r '.dev_server_security_group_id.value')

# Check instance status
aws ec2 describe-instance-status \
  --instance-ids $(terraform output -raw dev_server_instance_id)
```

### RDS Connection Issues

```bash
# Verify RDS is available
aws rds describe-db-instances \
  --db-instance-identifier ocpctl-dev-db \
  --query 'DBInstances[0].DBInstanceStatus'

# Test from dev server (should work)
ssh -i ~/.ssh/ocpctl-dev-key ubuntu@$(terraform output -raw dev_server_public_ip) \
  "psql '$(terraform output -raw database_url)' -c 'SELECT 1;'"
```

### DNS Propagation

```bash
# Check DNS record
dig dev.ocpctl.mg.dog8code.com

# May take up to 5 minutes to propagate
```

## Security Notes

1. **SSH Access**: Restrict `ssh_allowed_cidrs` to your IP range
2. **RDS**: Not publicly accessible, only from dev server
3. **S3 Buckets**: Not public, accessed via IAM role
4. **Passwords**: Stored in terraform.tfvars (add to .gitignore!)
5. **SSH Keys**: Generated key is sensitive - store securely

## Additional Resources

- [Dev Environment Plan](../../docs/deployment/DEV_TEST_ENVIRONMENT_PLAN.md)
- [Deploy Script](../../scripts/deploy-env.sh)
- [Bootstrap Script](../../scripts/bootstrap-dev-server.sh)
