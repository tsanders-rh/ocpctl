# AWS Resource Tagging Tool

This tool retroactively tags AWS resources for clusters that were created before Phase 2 comprehensive tagging was implemented.

## Purpose

Before Phase 2, only IAM roles and OIDC providers were tagged with `ManagedBy: ocpctl`. This tool adds comprehensive tags to ALL AWS resources for existing clusters:

- EC2 resources (VPCs, subnets, instances, volumes, security groups, elastic IPs)
- Load balancers (Network Load Balancers, Application Load Balancers)
- Route53 hosted zones
- S3 buckets (bootstrap, OIDC)
- IAM roles and OIDC providers

## Usage

### Tag by Cluster Name

```bash
go run cmd/tag-aws-resources/main.go -name <cluster-name>
```

Example:
```bash
go run cmd/tag-aws-resources/main.go -name sanders12
```

### Tag by Cluster ID

```bash
go run cmd/tag-aws-resources/main.go -id <cluster-id>
```

Example:
```bash
go run cmd/tag-aws-resources/main.go -id 550e8400-e29b-41d4-a716-446655440000
```

### Dry Run Mode

Preview what would be tagged without making changes:

```bash
go run cmd/tag-aws-resources/main.go -name <cluster-name> -dry-run
```

Example output:
```
[DRY RUN] Would tag resources with:
  ManagedBy: ocpctl
  ClusterName: sanders12
  Profile: aws-standard
  InfraID: sanders12-9hfvt
  CreatedAt: 2024-03-15T10:30:00Z
  Region: us-east-1
  kubernetes.io/cluster/sanders12-9hfvt: owned
```

### Specify OpenShift Version

By default, the tool uses the cluster's version from the database. To override:

```bash
go run cmd/tag-aws-resources/main.go -name <cluster-name> -version 4.18
```

## Requirements

1. **AWS Credentials**: Must have AWS credentials configured (via environment variables, shared credentials file, or EC2 instance profile)

2. **IAM Permissions**: The credentials must have tagging permissions for:
   - EC2: `ec2:CreateTags`, `ec2:Describe*`
   - ELB: `elasticloadbalancing:AddTags`, `elasticloadbalancing:Describe*`
   - Route53: `route53:ChangeTagsForResource`, `route53:ListTagsForResource`
   - S3: `s3:PutBucketTagging`, `s3:GetBucketTagging`
   - IAM: `iam:TagRole`, `iam:ListRoleTags`

   See `deploy/iam-policy-tagging.json` for the complete policy.

3. **Database Access**: Must be able to connect to the OCPCTL database to retrieve cluster metadata

4. **InfraID**: The cluster must have an `infraID` stored in the database (required for resource discovery)

## How It Works

1. **Retrieves cluster metadata** from the OCPCTL database (name, profile, infraID, region, created date)

2. **Discovers AWS resources** using the `kubernetes.io/cluster/<infraID>` tag that OpenShift applies during installation

3. **Tags resources in parallel** across 5 AWS services:
   - EC2 resources
   - Load balancers
   - Route53 hosted zones
   - S3 buckets
   - IAM roles and OIDC provider

4. **Applies standard tags**:
   ```
   ManagedBy: ocpctl
   ClusterName: <cluster-name>
   Profile: <profile-name>
   InfraID: <infra-id>
   CreatedAt: <timestamp>
   OcpctlVersion: <version>
   kubernetes.io/cluster/<infraID>: owned
   ```

## Verifying Tags

After running the tool, verify tags in AWS console:

### EC2 Console
1. Navigate to EC2 Dashboard → VPCs
2. Select the cluster's VPC
3. Click "Tags" tab
4. Verify `ManagedBy=ocpctl` tag is present

### Load Balancers
1. Navigate to EC2 Dashboard → Load Balancers
2. Select cluster load balancer
3. Click "Tags" tab
4. Verify `ManagedBy=ocpctl` tag is present

### Route53
1. Navigate to Route53 → Hosted Zones
2. Select cluster hosted zone
3. View tags
4. Verify `ManagedBy=ocpctl` tag is present

### S3 Buckets
1. Navigate to S3
2. Find buckets with pattern `<cluster>-<infraID>-bootstrap` or `<infraID>-oidc`
3. View tags
4. Verify `ManagedBy=ocpctl` tag is present

### IAM Roles
1. Navigate to IAM → Roles
2. Filter by cluster name or infraID
3. Select a role
4. View tags
5. Verify `ManagedBy=ocpctl` tag is present

## Troubleshooting

### "Cluster has no InfraID - cannot tag resources"

**Cause**: The cluster record in the database doesn't have an `infraID` field populated.

**Solution**:
1. Check if the cluster was created successfully
2. Verify the cluster metadata was saved during creation
3. If metadata is missing, you may need to manually extract the infraID from AWS resource names

### "No EC2 resources found for infraID"

**Cause**: Resources don't have the `kubernetes.io/cluster/<infraID>` tag, or the infraID is incorrect.

**Solution**:
1. Verify the cluster's infraID in the database matches AWS resource tags
2. Check AWS console for resources with `kubernetes.io/cluster/*` tags
3. Ensure the cluster was created by OpenShift installer (not manually)

### "Failed to tag resources: AccessDenied"

**Cause**: AWS credentials don't have sufficient permissions.

**Solution**: Apply the IAM policy from `deploy/iam-policy-tagging.json` to the IAM role or user.

## Safety

- The tool only tags resources that already have the `kubernetes.io/cluster/<infraID>` tag
- It does NOT create, modify, or delete resources
- Tagging is idempotent - running multiple times is safe
- Use `-dry-run` to preview changes before applying

## Examples

### Tag a single cluster
```bash
go run cmd/tag-aws-resources/main.go -name prod-cluster-01
```

### Tag all clusters (shell script)
```bash
#!/bin/bash
# Tag all active clusters

psql -d ocpctl -t -A -c "SELECT name FROM clusters WHERE status='ready';" | while read cluster_name; do
  echo "Tagging cluster: $cluster_name"
  go run cmd/tag-aws-resources/main.go -name "$cluster_name"
done
```

### Preview tags for a cluster
```bash
go run cmd/tag-aws-resources/main.go -name test-cluster -dry-run
```

## Integration with Orphan Detection

After tagging existing clusters, the orphan detection system will:

1. **Prefer ManagedBy tag**: Check for `ManagedBy=ocpctl` first
2. **Eliminate false positives**: Only flag ocpctl-managed resources as orphaned
3. **Maintain backward compatibility**: Fall back to pattern matching for untagged resources

This enables safe automated orphan cleanup without accidentally deleting non-ocpctl OpenShift clusters.
