# OCPCTL Worker IAM Policies

This directory contains IAM policy documents for granting ocpctl workers the permissions needed to provision and manage OpenShift clusters on AWS.

## Available Policies

### 1. Full Policy (Recommended for Production)
**File:** `iam-policy-worker-full.json`

**Use when:**
- Running ocpctl in production
- Need to support all cluster features
- Provisioning clusters with STS (Manual mode)
- Using EFS storage
- Need OIDC provider management

**Capabilities:**
- ✅ Full OpenShift IPI cluster lifecycle (create, destroy, hibernate, resume)
- ✅ ROSA cluster support (with OCM_TOKEN)
- ✅ STS/Manual mode with OIDC providers
- ✅ EFS storage configuration
- ✅ VPC, subnet, security group management
- ✅ Load balancer (ELB/ALB/NLB) management
- ✅ Route53 DNS management
- ✅ S3 bucket management for OIDC and bootstrap
- ✅ Parameter Store access for secrets

**Permissions Breakdown:**
| Service | Statement ID | Purpose |
|---------|-------------|---------|
| EC2 | EC2FullManagement | VPC, instances, networking, volumes |
| ELB | LoadBalancerManagement | API and ingress load balancers |
| IAM | IAMManagement | Instance profiles, roles, OIDC providers |
| Route53 | Route53Management | DNS zones and records |
| S3 | S3Management | Bootstrap and OIDC buckets |
| EFS | EFSManagement | Shared storage file systems |
| Service Quotas | ServiceQuotasRead | Check AWS limits |
| Tagging | ResourceTagging | Resource organization and cost tracking |
| SSM | ParameterStoreRead | Retrieve secrets (database passwords, etc.) |

### 2. Minimal Policy (Testing Only)
**File:** `iam-policy-worker-minimal.json`

**Use when:**
- Quick testing or proof-of-concept
- Cost-conscious development
- Learning ocpctl basics

**Capabilities:**
- ✅ Basic OpenShift cluster creation
- ✅ Simple VPC and networking
- ✅ Basic DNS and load balancing
- ❌ No STS/Manual mode support
- ❌ No EFS storage
- ❌ No OIDC provider management
- ⚠️ May fail on advanced cluster configurations

**Limitations:**
- Cannot provision clusters in Manual mode (requires OIDC permissions)
- Cannot configure EFS storage
- May encounter permission errors with complex cluster profiles
- Not suitable for production use

### 3. Tagging Policy (Supplemental)
**File:** `iam-policy-tagging.json`

This is a supplemental policy for AWS resource tagging. It's included in the full policy but provided separately for reference.

---

## Usage

### Option 1: Create Managed IAM Policy (Recommended)

This creates a reusable managed policy that can be attached to multiple roles:

```bash
# Using the full policy
aws iam create-policy \
  --policy-name ocpctl-worker-full \
  --policy-document file://deploy/iam-policy-worker-full.json \
  --description "Full permissions for ocpctl worker to provision OpenShift clusters on AWS"

# Save the policy ARN
export POLICY_ARN=$(aws iam list-policies \
  --query 'Policies[?PolicyName==`ocpctl-worker-full`].Arn' \
  --output text)

echo "Policy ARN: $POLICY_ARN"
```

### Option 2: Create IAM Role and Attach Policy

```bash
# Create trust policy for EC2
cat > /tmp/trust-policy.json <<'EOF'
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Principal": {
        "Service": "ec2.amazonaws.com"
      },
      "Action": "sts:AssumeRole"
    }
  ]
}
EOF

# Create IAM role
aws iam create-role \
  --role-name ocpctl-worker-role \
  --assume-role-policy-document file:///tmp/trust-policy.json \
  --description "IAM role for ocpctl worker service"

# Attach the managed policy
aws iam attach-role-policy \
  --role-name ocpctl-worker-role \
  --policy-arn $POLICY_ARN

# Create instance profile
aws iam create-instance-profile \
  --instance-profile-name ocpctl-worker-role

# Add role to instance profile
aws iam add-role-to-instance-profile \
  --instance-profile-name ocpctl-worker-role \
  --role-name ocpctl-worker-role
```

### Option 3: Attach to Existing EC2 Instance

```bash
# Get your instance ID
export INSTANCE_ID=i-0123456789abcdef0

# Attach instance profile
aws ec2 associate-iam-instance-profile \
  --instance-id $INSTANCE_ID \
  --iam-instance-profile Name=ocpctl-worker-role

# Verify attachment
aws ec2 describe-instances \
  --instance-ids $INSTANCE_ID \
  --query 'Reservations[0].Instances[0].IamInstanceProfile'
```

### Option 4: Use Inline Policy (Not Recommended)

For quick testing only - managed policies are preferred for production:

```bash
aws iam put-role-policy \
  --role-name ocpctl-worker-role \
  --policy-name ocpctl-worker-inline \
  --policy-document file://deploy/iam-policy-worker-full.json
```

---

## Validation

### Verify Policy Attachment

```bash
# List attached managed policies
aws iam list-attached-role-policies --role-name ocpctl-worker-role

# Get policy version details
aws iam get-policy-version \
  --policy-arn $POLICY_ARN \
  --version-id v1
```

### Test Permissions from EC2 Instance

SSH to your worker instance and test:

```bash
# Verify AWS credentials are available
aws sts get-caller-identity

# Should show:
# {
#     "UserId": "AROAXXXXXXXXX:i-0123456789abcdef0",
#     "Account": "123456789012",
#     "Arn": "arn:aws:sts::123456789012:assumed-role/ocpctl-worker-role/i-0123456789abcdef0"
# }

# Test EC2 permissions
aws ec2 describe-regions --region us-east-1

# Test Route53 permissions
aws route53 list-hosted-zones

# Test S3 permissions
aws s3 ls

# Test Parameter Store access
aws ssm get-parameter --name "/ocpctl/database/password" --with-decryption
```

---

## Security Best Practices

### 1. Use Managed Policies
- ✅ Easier to update across multiple roles
- ✅ Version controlled
- ✅ Can audit which roles have which policies

### 2. Principle of Least Privilege
- Start with minimal policy for testing
- Upgrade to full policy only when needed
- Document why each permission is required

### 3. Resource-Level Restrictions (Optional)

For enhanced security, restrict permissions to specific resources:

```json
{
  "Sid": "S3RestrictedAccess",
  "Effect": "Allow",
  "Action": ["s3:*"],
  "Resource": [
    "arn:aws:s3:::ocpctl-*",
    "arn:aws:s3:::ocpctl-*/*"
  ]
}
```

### 4. Condition-Based Access

Add conditions to limit when permissions apply:

```json
{
  "Sid": "EC2RestrictedRegions",
  "Effect": "Allow",
  "Action": ["ec2:*"],
  "Resource": "*",
  "Condition": {
    "StringEquals": {
      "aws:RequestedRegion": ["us-east-1", "us-west-2"]
    }
  }
}
```

### 5. Regular Audits

```bash
# Review policy usage
aws iam get-policy \
  --policy-arn $POLICY_ARN \
  --query 'Policy.{Name:PolicyName,Created:CreateDate,Updated:UpdateDate,AttachmentCount:AttachmentCount}'

# List all entities using this policy
aws iam list-entities-for-policy --policy-arn $POLICY_ARN
```

---

## Troubleshooting

### Permission Denied Errors

If cluster creation fails with permission errors:

1. **Check current permissions:**
   ```bash
   aws iam get-role --role-name ocpctl-worker-role
   aws iam list-attached-role-policies --role-name ocpctl-worker-role
   ```

2. **Verify the instance has the role:**
   ```bash
   aws ec2 describe-instances \
     --instance-ids $INSTANCE_ID \
     --query 'Reservations[0].Instances[0].IamInstanceProfile'
   ```

3. **Check worker logs for specific denied actions:**
   ```bash
   sudo journalctl -u ocpctl-worker | grep "AccessDenied"
   ```

4. **Add missing permissions:**
   - Identify the denied action (e.g., `ec2:CreateNatGateway`)
   - Add to policy JSON under appropriate statement
   - Update the managed policy:
     ```bash
     aws iam create-policy-version \
       --policy-arn $POLICY_ARN \
       --policy-document file://deploy/iam-policy-worker-full.json \
       --set-as-default
     ```

### Instance Metadata Service Issues

If credentials aren't available on EC2:

```bash
# Check IMDSv2 is accessible
TOKEN=$(curl -X PUT "http://169.254.169.254/latest/api/token" -H "X-aws-ec2-metadata-token-ttl-seconds: 21600")

# Check role credentials are available
curl -H "X-aws-ec2-metadata-token: $TOKEN" \
  http://169.254.169.254/latest/meta-data/iam/security-credentials/

# Should return role name, then:
curl -H "X-aws-ec2-metadata-token: $TOKEN" \
  http://169.254.169.254/latest/meta-data/iam/security-credentials/ocpctl-worker-role
```

---

## Comparison: Full vs Minimal

| Capability | Full Policy | Minimal Policy |
|-----------|-------------|----------------|
| OpenShift IPI (default mode) | ✅ | ✅ |
| OpenShift IPI (STS/Manual mode) | ✅ | ❌ |
| ROSA clusters | ✅ | ❌ |
| EFS storage | ✅ | ❌ |
| OIDC providers | ✅ | ❌ |
| NAT gateways | ✅ | ❌ |
| VPC endpoints | ✅ | ❌ |
| Service quotas checking | ✅ | ❌ |
| Production ready | ✅ | ❌ |
| **Total statements** | 10 | 6 |
| **Recommended for** | Production | Testing only |

---

## Related Documentation

- [AWS Quick Start Guide](../docs/deployment/AWS_QUICKSTART.md)
- [IAM Setup Guide](IAM-SETUP.md)
- [Security Configuration](../docs/deployment/SECURITY_CONFIGURATION.md)
- [AWS IAM Permissions Reference](../docs/operations/AWS_IAM_PERMISSIONS.md)

---

## Updates and Versioning

When updating policies:

1. Test changes in a non-production environment first
2. Use `create-policy-version` to update managed policies
3. Keep old versions for rollback (AWS keeps up to 5 versions)
4. Document changes in commit messages
5. Update this README with any new requirements

**Last Updated:** 2026-05-08
**Policy Version:** 1.0
