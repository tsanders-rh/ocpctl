# RDS Migration Guide

## Overview

This guide explains how to run database migrations against the RDS PostgreSQL instance.

## Prerequisites

1. **Network Access**: Ensure you can reach the RDS instance at `44.201.165.78:5432`
2. **RDS Password**: Obtain the database password from AWS Secrets Manager or your team
3. **goose CLI**: Install if not already present: `go install github.com/pressly/goose/v3/cmd/goose@latest`

## Database Connection

### Option 1: Direct Connection (if RDS security group allows)

If your IP is whitelisted in the RDS security group:

```bash
# Set the RDS password
export RDS_PASSWORD='your-rds-password-here'

# Run migrations
./scripts/migrate-rds.sh up
```

### Option 2: SSH Tunnel (via bastion/jump host)

If you need to go through a bastion host:

```bash
# Step 1: Open SSH tunnel (in a separate terminal)
ssh -L 5432:44.201.165.78:5432 ec2-user@your-bastion-host

# Step 2: Run migration through tunnel (in another terminal)
export RDS_PASSWORD='your-rds-password-here'
export RDS_HOST='localhost'  # Use localhost since we're tunneling
./scripts/migrate-rds.sh up
```

### Option 3: Session Manager (if using AWS SSM)

```bash
# Start port forwarding session
aws ssm start-session \
    --target i-xxxxxxxxxxxxx \
    --document-name AWS-StartPortForwardingSessionToRemoteHost \
    --parameters '{"portNumber":["5432"],"localPortNumber":["5432"],"host":["44.201.165.78"]}'

# In another terminal:
export RDS_PASSWORD='your-rds-password-here'
export RDS_HOST='localhost'
./scripts/migrate-rds.sh up
```

## Migration Commands

### Apply All Pending Migrations
```bash
./scripts/migrate-rds.sh up
```

### Check Migration Status
```bash
./scripts/migrate-rds.sh status
```

### Check Current Version
```bash
./scripts/migrate-rds.sh version
```

### Rollback Last Migration
```bash
./scripts/migrate-rds.sh down
```

## Migration 00039: GCP Orphan Resource Types

The latest migration (00039) adds support for tracking GCP orphaned resources:

**What it does:**
- Adds `GCPServiceAccount`, `GCPNetwork`, `GCPSubnetwork`, `GCPDisk`, `GCPInstance`, `GCPBucket`, and `GCPIPAddress` to the allowed resource types
- Fixes the constraint violation error when the janitor detects orphaned GCP resources

**Before the migration:**
```
WARNING: Failed to persist orphaned resource to database: upsert orphaned resource:
ERROR: new row for relation "orphaned_resources" violates check constraint
"orphaned_resources_resource_type_check" (SQLSTATE 23514)
```

**After the migration:**
- GCP orphaned resources will be successfully saved to the database
- The admin console will display GCP orphaned resources alongside AWS ones

## Troubleshooting

### Connection Timeout
```
dial tcp 44.201.165.78:5432: connect: operation timed out
```

**Solution:** Your IP is not whitelisted in the RDS security group. Either:
1. Add your IP to the security group
2. Use an SSH tunnel through a whitelisted bastion host

### SSL/TLS Error
```
SSL is required
```

**Solution:** The migration script already uses `sslmode=require`. This should not occur.

### Authentication Failed
```
password authentication failed
```

**Solution:** Check that your RDS_PASSWORD is correct. Get the password from AWS Secrets Manager:
```bash
aws secretsmanager get-secret-value --secret-id ocpctl/rds/password --query SecretString --output text
```

## Security Best Practices

1. **Never commit passwords**: Always use environment variables
2. **Rotate credentials**: RDS passwords should be rotated regularly
3. **Use least privilege**: Migration user should only have schema modification rights
4. **Audit trail**: All migrations are logged in the `goose_db_version` table

## Verifying the Fix

After running migration 00039, verify it works:

1. Check migration status:
   ```bash
   ./scripts/migrate-rds.sh status
   ```

2. Monitor worker logs for GCP orphan detection:
   ```bash
   # On the worker host
   journalctl -u ocpctl-worker -f | grep "orphaned GCP"
   ```

3. You should see successful persistence instead of constraint violations:
   ```
   2026/04/29 23:47:00 WARNING: Found 1 orphaned GCP resources:
   2026/04/29 23:47:00   - GCPServiceAccount: migqe-gcp-sa
   ```

## Next Steps

After migrating:
1. Restart the worker to pick up the new constraint
2. Check the admin console at `/admin/orphaned-resources`
3. Review and clean up any detected orphaned resources
