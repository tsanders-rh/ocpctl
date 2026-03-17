# AWS Resource Management

This guide explains how ocpctl manages AWS resources, including automatic tagging, orphan detection, and cleanup workflows.

## Overview

Every OpenShift cluster provisioned by ocpctl creates numerous AWS resources across multiple services:

- **EC2**: VPCs, subnets, instances, volumes, security groups, elastic IPs
- **Elastic Load Balancing**: Network Load Balancers (NLB), Application Load Balancers (ALB)
- **Route53**: Private hosted zones for cluster DNS
- **S3**: Bootstrap buckets, OIDC discovery documents
- **IAM**: Service account roles, OIDC providers for STS authentication

Proper resource management ensures:
1. **Cost control**: No orphaned resources incurring charges after cluster deletion
2. **Security**: All resources properly tagged and auditable
3. **Cleanup**: Deterministic resource deletion matching creation
4. **Attribution**: Clear ownership and cost allocation via tags

## Automatic Resource Tagging

### Tag Format

All AWS resources created by ocpctl are automatically tagged with the following standard tags:

| Tag Key | Description | Example |
|---------|-------------|---------|
| `ManagedBy` | Identifies ocpctl-managed resources | `ocpctl` |
| `ClusterName` | Human-readable cluster name | `sanders12` |
| `Profile` | Cluster profile used | `aws-standard` |
| `InfraID` | OpenShift infrastructure ID | `sanders12-9hfvt` |
| `CreatedAt` | Cluster creation timestamp | `2024-03-17T10:30:00Z` |
| `OcpctlVersion` | Version of ocpctl used | `v0.20260317.bca1feb` |
| `kubernetes.io/cluster/<infraID>` | OpenShift cluster ownership tag | `owned` |

### When Tags Are Applied

Tags are applied **after** cluster creation completes (~30-45 minutes):

1. **During cluster creation**: `openshift-install` applies the `kubernetes.io/cluster/<infraID>` tag to all resources
2. **After cluster is ready**: ocpctl tags all resources with additional metadata tags
3. **Parallel execution**: Tags are applied concurrently across 5 AWS services (~5 seconds total)

### What Gets Tagged

#### EC2 Resources
- VPCs
- Subnets (public and private)
- EC2 instances (control-plane and worker nodes)
- EBS volumes
- Security groups
- Elastic IP addresses

#### Load Balancers
- Network Load Balancers (NLB) for API server
- Application Load Balancers (ALB) for ingress

#### Route53
- Private hosted zones for cluster internal DNS

#### S3 Buckets
- Bootstrap bucket (used during installation)
- OIDC discovery document bucket

#### IAM Resources
- Service account roles (created by ccoctl for Manual credentials mode)
- OIDC provider for pod identity

### Tag Discovery

ocpctl discovers resources by filtering on the `kubernetes.io/cluster/<infraID>` tag that OpenShift applies during installation:

```go
filter := []types.Filter{
    {
        Name:   aws.String(fmt.Sprintf("tag:kubernetes.io/cluster/%s", infraID)),
        Values: []string{"owned"},
    },
}
```

This ensures only resources belonging to the cluster are tagged, even in shared VPC scenarios.

## Orphan Detection

### How It Works

The janitor service runs periodically (every 10 minutes) to detect orphaned resources:

**Hybrid Detection Strategy:**

1. **Tag-based detection (primary)**:
   - Check for `ManagedBy=ocpctl` tag on resources
   - If present, check if corresponding cluster exists in database
   - If cluster doesn't exist or is destroyed, flag as orphaned

2. **Pattern matching (fallback)**:
   - For resources created before comprehensive tagging was implemented
   - Extracts cluster name from resource names (e.g., `sanders12-9hfvt-vpc` → `sanders12`)
   - Checks if cluster exists in database

### Orphaned Resource Types

The janitor detects orphans for:

- **VPCs** - Check for `ManagedBy=ocpctl` tag or pattern match on VPC name
- **Load Balancers** - Filter by `ManagedBy=ocpctl` tag
- **Route53 Hosted Zones** - Check for `ManagedBy=ocpctl` and `kubernetes.io/cluster/*` tags
- **EC2 Instances** - Filter by `kubernetes.io/cluster/*` tag
- **IAM Roles** - Check for `ManagedBy=ocpctl` tag or role name pattern
- **OIDC Providers** - Check for `ClusterName` tag or URL pattern

### Viewing Orphaned Resources

**Admin Console:**

Navigate to `/admin/orphaned-resources` to view detected orphans.

**Orphan Status:**
- `active` - Resource is orphaned and awaiting action
- `resolved` - Resource has been cleaned up or reconciled

### False Positives Prevention

The `ManagedBy=ocpctl` tag **prevents false positives** from:

- OpenShift clusters created outside of ocpctl (e.g., via `openshift-install` directly)
- Shared infrastructure resources not managed by ocpctl
- Resources from other automation tools

Only resources with `ManagedBy=ocpctl` are flagged as orphans.

## Retroactive Tagging

For clusters created before comprehensive tagging was implemented, use the retroactive tagging tool.

### When to Use

- You deployed ocpctl before Phase 2 (AWS Resource Tagging) was implemented
- Your clusters don't have `ManagedBy=ocpctl` tags on EC2/ELB/Route53/S3 resources
- You want to enable tag-based orphan detection for existing clusters

### Usage

**Preview tags (dry run):**

```bash
./bin/tag-aws-resources -name <cluster-name> -dry-run
```

Example output:
```
2024-03-17 10:30:00 Tagging resources for cluster: sanders12 (ID: ...)
2024-03-17 10:30:01 Using cluster region: us-east-1
2024-03-17 10:30:02 Discovering infraID from AWS resources...
2024-03-17 10:30:03 Found infraID: sanders12-9hfvt

[DRY RUN] Would tag resources with:
  ManagedBy: ocpctl
  ClusterName: sanders12
  Profile: aws-standard
  InfraID: sanders12-9hfvt
  CreatedAt: 2024-03-15T10:30:00Z
  Region: us-east-1
  kubernetes.io/cluster/sanders12-9hfvt: owned
```

**Apply tags:**

```bash
./bin/tag-aws-resources -name <cluster-name>
```

The tool will:
1. Query the ocpctl database for cluster metadata
2. Discover the cluster's `infraID` from AWS VPC tags
3. Tag all AWS resources in parallel (~5 seconds)
4. Report success/failure for each service

**Tag by cluster ID:**

```bash
./bin/tag-aws-resources -id <cluster-uuid>
```

**Specify region:**

```bash
./bin/tag-aws-resources -name <cluster-name> -region us-west-2
```

### How It Works

The tool:

1. **Connects to database**: Retrieves cluster metadata (name, profile, region, created date)
2. **Discovers infraID**: Queries AWS EC2 for VPCs with `kubernetes.io/cluster/<infraID>` tag matching the cluster name
3. **Tags resources**: Applies standard tags to all AWS resources in parallel

**Auto-discovery algorithm:**

```go
// List all VPCs in region
vpcs := ec2.DescribeVpcs()

// Find VPC by Name tag containing cluster name
for vpc in vpcs:
    if vpc.Name contains clusterName:
        // Extract infraID from kubernetes.io/cluster/<infraID> tag
        for tag in vpc.Tags:
            if tag.Key starts with "kubernetes.io/cluster/":
                infraID = extractInfraID(tag.Key)
                return infraID
```

### Bulk Tagging

To tag multiple clusters:

```bash
#!/bin/bash
# Tag all active clusters

psql -d ocpctl -t -A -c "SELECT name FROM clusters WHERE status='ready';" | while read cluster_name; do
  echo "Tagging cluster: $cluster_name"
  ./bin/tag-aws-resources -name "$cluster_name"
done
```

### Verification

After tagging, verify tags in AWS console:

**EC2 Console:**
1. Navigate to VPCs
2. Select cluster VPC
3. Click "Tags" tab
4. Verify `ManagedBy=ocpctl` tag is present

**Load Balancers:**
1. Navigate to EC2 → Load Balancers
2. Select cluster LB
3. Click "Tags" tab

**Route53:**
1. Navigate to Route53 → Hosted Zones
2. Select cluster zone
3. View tags

**S3:**
1. Navigate to S3
2. Find buckets: `<cluster>-<infraID>-bootstrap`, `<infraID>-oidc`
3. View tags

**IAM:**
1. Navigate to IAM → Roles
2. Filter by cluster name or infraID
3. Select role and view tags

## Resource Cleanup

### Automatic Cleanup

**TTL-based deletion:**

- Clusters with expired TTLs are automatically destroyed by the janitor
- All AWS resources are deleted via `openshift-install destroy cluster`
- IAM resources are deleted via `ccoctl aws delete`

**Orphan cleanup (future):**

Once all clusters have `ManagedBy=ocpctl` tags, automated orphan cleanup will be enabled:

1. Janitor detects orphaned resources
2. Waits for confirmation period (24-48 hours)
3. Automatically deletes orphaned resources
4. Logs all deletions to audit trail

### Manual Cleanup

**For individual resources:**

Use AWS console or CLI to delete orphaned resources manually:

```bash
# Delete orphaned VPC
aws ec2 delete-vpc --vpc-id vpc-0123456789abcdef0

# Delete orphaned hosted zone
aws route53 delete-hosted-zone --id Z1234567890ABC

# Delete orphaned IAM role
aws iam delete-role --role-name sanders12-9hfvt-openshift-cloud-credential-...
```

**For entire cluster:**

If cluster metadata exists in ocpctl database but cluster destruction failed:

1. Navigate to cluster detail page
2. Click "Destroy" button
3. ocpctl will attempt to destroy using saved metadata

If metadata is missing:

```bash
# Use openshift-install directly with saved work directory
cd /tmp/ocpctl/<cluster-id>
openshift-install destroy cluster --dir .
```

## Cost Attribution

### Tag-based Cost Allocation

AWS Cost Explorer can filter by tags for cost attribution:

**Filter by ocpctl-managed resources:**
- Tag: `ManagedBy=ocpctl`

**Filter by cluster:**
- Tag: `ClusterName=<cluster-name>`

**Filter by profile:**
- Tag: `Profile=aws-standard`

**Filter by team/owner:**
- Use custom tags added via cluster request

### Cost Reports

**Monthly cost by cluster:**

1. AWS Cost Explorer → Cost & Usage Reports
2. Group by → Tag: `ClusterName`
3. Filter → Tag: `ManagedBy=ocpctl`
4. Time range → Last month

**Cost by profile:**

1. Group by → Tag: `Profile`
2. Compare `aws-sno-test` vs `aws-standard` vs `aws-virtualization`

**Hourly cost tracking:**

- Enable hourly granularity in Cost Explorer
- Track cluster lifecycle costs from creation to destruction

### FinOps Integration

Tags enable integration with FinOps tools:

- **CloudHealth**: Import tags for cost allocation
- **Apptio Cloudability**: Tag-based showback/chargeback
- **AWS Cost and Usage Report (CUR)**: Export with tag columns for analysis

## IAM Permissions

### Required Permissions for Tagging

The ocpctl worker service requires these IAM permissions:

**EC2:**
- `ec2:CreateTags`
- `ec2:Describe*` (VPCs, subnets, instances, volumes, security groups, addresses)

**ELB:**
- `elasticloadbalancing:AddTags`
- `elasticloadbalancing:DescribeLoadBalancers`
- `elasticloadbalancing:DescribeTags`

**Route53:**
- `route53:ChangeTagsForResource`
- `route53:ListHostedZones`
- `route53:ListTagsForResource`

**S3:**
- `s3:PutBucketTagging`
- `s3:GetBucketTagging`
- `s3:ListAllMyBuckets`

**IAM:**
- `iam:TagRole`
- `iam:ListRoles`
- `iam:ListRoleTags`
- `iam:TagOpenIDConnectProvider`
- `iam:GetOpenIDConnectProvider`

### Applying Permissions

**Option 1: Attach to existing instance profile**

```bash
# Create policy
aws iam create-policy \
  --policy-name OCPCTLTaggingPolicy \
  --policy-document file://deploy/iam-policy-tagging.json

# Get role name from instance profile
ROLE_NAME=$(aws iam get-instance-profile \
  --instance-profile-name ocpctl-instance-role \
  --query 'InstanceProfile.Roles[0].RoleName' \
  --output text)

# Attach policy
aws iam attach-role-policy \
  --role-name $ROLE_NAME \
  --policy-arn arn:aws:iam::<account-id>:policy/OCPCTLTaggingPolicy
```

**Option 2: Use AWS managed policies (testing only)**

```bash
# For testing only - grants overly broad permissions
aws iam attach-role-policy \
  --role-name ocpctl-instance-role \
  --policy-arn arn:aws:iam::aws:policy/AmazonEC2FullAccess

# Add other managed policies as needed
```

See `deploy/IAM-SETUP.md` for complete setup instructions.

## Troubleshooting

### Tags Not Applied

**Symptom:** New cluster created but resources don't have `ManagedBy=ocpctl` tag.

**Possible causes:**
1. IAM permissions not configured
2. Tagging failed but cluster creation succeeded
3. Using old ocpctl version before Phase 2

**Resolution:**
```bash
# Check worker logs
journalctl -u ocpctl-worker -n 100 | grep -i tag

# Look for errors like:
# "Warning: failed to tag AWS resources: AccessDenied"

# If permissions issue, apply IAM policy (see IAM Permissions section)

# If old version, upgrade and use retroactive tagging tool:
./bin/tag-aws-resources -name <cluster-name>
```

### Orphan Detection False Positives

**Symptom:** Resources from non-ocpctl clusters are flagged as orphaned.

**Resolution:**

This should not happen if comprehensive tagging is working correctly. Ocpctl only flags resources with `ManagedBy=ocpctl` tag.

If it happens:
1. Verify the resource has `ManagedBy=ocpctl` tag
2. If it does and shouldn't, manually remove the tag:
   ```bash
   aws ec2 delete-tags \
     --resources vpc-0123456789abcdef0 \
     --tags Key=ManagedBy,Value=ocpctl
   ```

### InfraID Not Found

**Symptom:** Retroactive tagging tool fails with "No VPC found with kubernetes.io/cluster/<infraID> tag".

**Causes:**
- Cluster was created manually outside ocpctl
- VPC was deleted but cluster record still exists
- Wrong region specified

**Resolution:**
```bash
# Verify cluster exists in AWS
aws ec2 describe-vpcs --region us-east-1 | grep <cluster-name>

# Specify correct region
./bin/tag-aws-resources -name <cluster-name> -region us-west-2

# If cluster is truly gone, delete from database:
psql -d ocpctl -c "UPDATE clusters SET status='destroyed' WHERE name='<cluster-name>';"
```

### AccessDenied Errors

**Symptom:** Tagging fails with "AccessDenied" errors.

**Resolution:**

Apply IAM permissions as described in IAM Permissions section.

Verify current permissions:
```bash
# Test EC2 tagging
aws ec2 describe-vpcs --max-results 1

# Test ELB tagging
aws elbv2 describe-load-balancers --max-results 1

# Test Route53 tagging
aws route53 list-hosted-zones --max-items 1

# Test S3 tagging
aws s3api list-buckets
```

If any command fails, apply the corresponding permission from `deploy/iam-policy-tagging.json`.

## Best Practices

### For Administrators

1. **Apply IAM permissions** immediately after ocpctl deployment
2. **Verify tagging** for first cluster created after deployment
3. **Run retroactive tagging** for all existing clusters
4. **Monitor orphan detection** regularly via admin console
5. **Review cost reports** monthly using tag filters

### For Users

1. **Use descriptive cluster names** for easier cost tracking
2. **Add custom tags** via request_tags for cost center allocation
3. **Set appropriate TTLs** to avoid orphaned resources
4. **Verify cluster destruction** completed successfully
5. **Report orphaned resources** to administrators

### For Developers

1. **Always test tagging** in development before deploying
2. **Check IAM permissions** in deployment checklist
3. **Log tagging failures** as warnings, not errors
4. **Use parallel execution** for tagging to minimize latency
5. **Respect AWS API rate limits** when batch tagging

## Reference

### Tag Format Specification

```go
type TagSet struct {
    ManagedBy      string // Always "ocpctl"
    ClusterName    string // Cluster name from database
    Profile        string // Profile name (e.g., "aws-standard")
    InfraID        string // OpenShift infrastructure ID
    CreatedAt      string // RFC3339 timestamp
    OcpctlVersion  string // Version of ocpctl binary
    KubernetesCluster string // "kubernetes.io/cluster/<infraID>: owned"
}
```

### API Rate Limits

AWS API rate limits per service (soft limits):

- **EC2**: 2,000 requests/second (CreateTags: 50 req/sec)
- **ELB**: 20 requests/second (AddTags: 10 req/sec)
- **Route53**: 5 requests/second
- **S3**: 3,500 PUT/sec per prefix
- **IAM**: 20 requests/second

Ocpctl respects these limits through:
- Batch operations (EC2: 1000 resources/call, ELB: 20 resources/call)
- Parallel execution (separate goroutines per service)
- Error handling with exponential backoff

### Related Documentation

- [IAM Setup Guide](../../deploy/IAM-SETUP.md) - Complete IAM permission setup
- [Retroactive Tagging Tool](../../cmd/tag-aws-resources/README.md) - Tool documentation
- [Orphaned Resource Management](orphaned-resources.md) - Admin guide for orphan cleanup
- [Getting Started Guide](getting-started.md) - New user onboarding

---

**Last Updated:** March 17, 2024
**Version:** ocpctl v0.20260317.bca1feb
