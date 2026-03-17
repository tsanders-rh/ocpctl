# IAM Permissions Setup for Phase 2 Tagging

This document explains how to set up the additional IAM permissions needed for comprehensive AWS resource tagging (Phase 2 of issue #15).

## Why These Permissions Are Needed

The OCPCTL worker service tags AWS resources across multiple services (EC2, ELB, Route53, S3, IAM) to enable:
- Accurate orphan detection (no false positives)
- Safe automated cleanup
- Cost attribution by cluster

## Required Permissions

The file `iam-policy-tagging.json` contains the additional permissions needed for tagging operations.

## Setup Instructions

### Option 1: Add to Existing Instance Profile (Recommended)

If you already have an IAM instance profile attached to your OCPCTL EC2 instance:

1. **Create the IAM policy**:
   ```bash
   aws iam create-policy \
     --policy-name OCPCTLTaggingPolicy \
     --policy-document file://deploy/iam-policy-tagging.json \
     --description "Additional tagging permissions for OCPCTL Phase 2"
   ```

2. **Get your instance profile name**:
   ```bash
   # Find the instance profile attached to your OCPCTL instance
   aws ec2 describe-instances \
     --instance-ids <your-instance-id> \
     --query 'Reservations[0].Instances[0].IamInstanceProfile.Arn' \
     --output text
   ```

3. **Attach the policy to the instance profile's role**:
   ```bash
   # Get the role name from the instance profile
   aws iam get-instance-profile \
     --instance-profile-name ocpctl-instance-role \
     --query 'InstanceProfile.Roles[0].RoleName' \
     --output text

   # Attach the new policy
   aws iam attach-role-policy \
     --role-name <role-name-from-above> \
     --policy-arn arn:aws:iam::<account-id>:policy/OCPCTLTaggingPolicy
   ```

### Option 2: Create New Combined Policy

If you're setting up OCPCTL for the first time, you can create a combined policy with all permissions:

1. **Create a comprehensive policy** that includes:
   - Existing EC2/VPC/IAM/Route53/S3 cluster provisioning permissions
   - New tagging permissions from `iam-policy-tagging.json`

2. **Attach to a new instance role**:
   ```bash
   aws iam create-role \
     --role-name ocpctl-instance-role \
     --assume-role-policy-document file://trust-policy.json

   aws iam attach-role-policy \
     --role-name ocpctl-instance-role \
     --policy-arn arn:aws:iam::<account-id>:policy/OCPCTLFullPolicy
   ```

### Option 3: Use AWS Managed Policies (Quick Test)

For testing purposes only (not recommended for production):

```bash
# Attach broad AWS managed policies
aws iam attach-role-policy \
  --role-name ocpctl-instance-role \
  --policy-arn arn:aws:iam::aws:policy/AmazonEC2FullAccess

aws iam attach-role-policy \
  --role-name ocpctl-instance-role \
  --policy-arn arn:aws:iam::aws:policy/ElasticLoadBalancingFullAccess

aws iam attach-role-policy \
  --role-name ocpctl-instance-role \
  --policy-arn arn:aws:iam::aws:policy/AmazonRoute53FullAccess

aws iam attach-role-policy \
  --role-name ocpctl-instance-role \
  --policy-arn arn:aws:iam::aws:policy/AmazonS3FullAccess
```

⚠️ **Warning**: This grants overly broad permissions. Use Option 1 or 2 for production.

## Verifying Permissions

After applying the policy, verify the worker can tag resources:

1. **Check the instance has the profile**:
   ```bash
   aws ec2 describe-instances \
     --instance-ids <instance-id> \
     --query 'Reservations[0].Instances[0].IamInstanceProfile'
   ```

2. **Test tagging from the instance** (SSH into OCPCTL instance):
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

   If any command fails with "UnauthorizedOperation" or "AccessDenied", the permissions are not correctly applied.

## Troubleshooting

### Permission Denied Errors

If you see errors like:
```
Error: failed to tag EC2 resources: UnauthorizedOperation: You are not authorized to perform this operation
```

**Solution**:
1. Verify the policy is attached to the correct role
2. Check the role is attached to the EC2 instance profile
3. Wait 60 seconds after attaching policy (IAM eventual consistency)

### No Permissions After Attaching Policy

**Solution**: Restart the OCPCTL worker service to pick up new credentials:
```bash
sudo systemctl restart ocpctl-worker
```

## Security Best Practices

1. **Principle of Least Privilege**: The `iam-policy-tagging.json` grants only tagging and read permissions, not create/delete permissions.

2. **Separate Policies**: Keep tagging permissions separate from cluster provisioning permissions for easier auditing.

3. **Regular Review**: Periodically review attached policies and remove any that are no longer needed.

4. **Enable CloudTrail**: Monitor all API calls made by the OCPCTL instance:
   ```bash
   aws cloudtrail lookup-events \
     --lookup-attributes AttributeKey=ResourceName,AttributeValue=<instance-role-name> \
     --max-results 50
   ```

## Reference

- [AWS IAM Best Practices](https://docs.aws.amazon.com/IAM/latest/UserGuide/best-practices.html)
- [EC2 Instance Profiles](https://docs.aws.amazon.com/IAM/latest/UserGuide/id_roles_use_switch-role-ec2_instance-profiles.html)
- [Resource Tagging Best Practices](https://docs.aws.amazon.com/general/latest/gr/aws_tagging.html)
