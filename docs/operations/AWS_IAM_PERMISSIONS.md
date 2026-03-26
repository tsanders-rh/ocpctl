# AWS IAM Permissions for OCPCTL Workers

## Overview

OCPCTL workers require specific AWS IAM permissions to create, manage, hibernate, and destroy OpenShift clusters. This document explains the IAM setup and how to ensure new workers get the correct permissions.

## IAM Architecture

### Instance Profiles and Roles

1. **Main Worker/API Server** (52.90.135.148):
   - Instance Profile: `ocpctl-ec2-role`
   - IAM Role: `ocpctl-ec2-role`
   - Usage: Primary worker that runs both API and worker services

2. **Autoscale Workers** (Auto Scaling Group):
   - Instance Profile: `ocpctl-worker-instance-profile`
   - IAM Role: `ocpctl-worker-role`
   - Usage: Dynamically created workers that scale based on job queue depth

### Managed Policies

Both roles use the following **managed policy**:
- **Policy Name**: `ocpctl-worker-openshift-provisioning`
- **Policy ARN**: `arn:aws:iam::346869059911:policy/ocpctl-worker-openshift-provisioning`
- **Current Version**: v6 (as of 2026-03-17)

## Required Permissions

### Core OpenShift Provisioning

The `ocpctl-worker-openshift-provisioning` policy includes permissions for:

**EC2 (Compute & Networking)**:
- Create/delete VPCs, subnets, security groups, internet gateways
- Create/delete EC2 instances
- Manage volumes, snapshots
- **Hibernate/Resume**: `ec2:StopInstances`, `ec2:StartInstances` (added v6)
- Terminate instances
- Describe all EC2 resources

**Elastic Load Balancing**:
- Create/delete load balancers (NLB, ALB)
- Manage target groups
- Configure health checks

**Route53 (DNS)**:
- Create/delete hosted zones
- Manage DNS records
- Tag DNS resources

**S3 (Storage)**:
- Create/delete buckets
- Upload/download objects
- Manage bucket policies and encryption
- Artifact storage for clusters

**IAM (Identity & Access)**:
- Create/delete IAM roles and instance profiles
- Manage OIDC providers (for STS/Manual mode)
- Tag IAM resources
- PassRole for cluster service accounts

**Other Services**:
- Service Quotas: Check limits
- Resource Groups Tagging: Tag resources

### Tagging Policy

Separate managed policy for resource tagging:
- **Policy Name**: `OCPCTLTaggingPolicy`
- **Purpose**: Tag all AWS resources with cluster metadata
- **Permissions**: EC2 CreateTags, ELB AddTags, Route53 ChangeTagsForResource, etc.

## How New Workers Get Permissions

### Autoscale Workers

New autoscale workers **automatically get the correct permissions** through:

1. **Auto Scaling Launch Template** specifies the instance profile: `ocpctl-worker-instance-profile`
2. Instance profile **links to IAM role**: `ocpctl-worker-role`
3. IAM role has **managed policies attached**: `ocpctl-worker-openshift-provisioning` + `OCPCTLTaggingPolicy`
4. **Policy updates are immediate**: When you update a managed policy version, all instances using that role get the new permissions instantly

**No instance restart required** - IAM policy changes take effect on the next API call.

### Verification

To verify a worker has correct permissions:

```bash
# SSH to worker
ssh -i ~/.ssh/ocpctl-test-key.pem ec2-user@<worker-ip>

# Test specific permissions
aws iam simulate-principal-policy \
  --policy-source-arn arn:aws:iam::346869059911:role/ocpctl-worker-role \
  --action-names ec2:StopInstances ec2:StartInstances ec2:DescribeInstances \
  --query "EvaluationResults[].{Action:EvalActionName,Decision:EvalDecision}" \
  --output table
```

Expected output:
```
------------------------------------
|      SimulatePrincipalPolicy     |
+---------------------+------------+
|       Action        | Decision   |
+---------------------+------------+
|  ec2:StopInstances  |  allowed   |
|  ec2:StartInstances |  allowed   |
|  ec2:DescribeInstances | allowed |
+---------------------+------------+
```

## Policy Update History

### v6 (2026-03-17)
- **Added**: `ec2:StartInstances`, `ec2:StopInstances`
- **Reason**: Enable hibernate/resume on autoscale workers
- **Issue**: Autoscale workers could not hibernate clusters after S3 artifact storage deployment allowed job distribution across workers

### v5 (Previous)
- Initial comprehensive OpenShift provisioning permissions

## Updating Permissions

### To Add New Permissions

1. Get current policy document:
   ```bash
   aws iam get-policy-version \
     --policy-arn arn:aws:iam::346869059911:policy/ocpctl-worker-openshift-provisioning \
     --version-id v6 \
     --query 'PolicyVersion.Document' \
     --output json > current-policy.json
   ```

2. Edit the JSON to add permissions

3. Create new policy version:
   ```bash
   aws iam create-policy-version \
     --policy-arn arn:aws:iam::346869059911:policy/ocpctl-worker-openshift-provisioning \
     --policy-document file://updated-policy.json \
     --set-as-default
   ```

4. Verify on a worker:
   ```bash
   ssh worker 'aws iam simulate-principal-policy ...'
   ```

### Important Notes

- AWS allows **5 versions** of a managed policy at a time
- If you have 5 versions, delete an old one before creating a new one:
  ```bash
  aws iam delete-policy-version \
    --policy-arn arn:aws:iam::346869059911:policy/ocpctl-worker-openshift-provisioning \
    --version-id v1
  ```
- **Always use managed policies** instead of inline policies for workers - it ensures consistency across all instances
- Test with `simulate-principal-policy` before deploying to production

## Troubleshooting

### "UnauthorizedOperation" Errors

If you see errors like:
```
User: arn:aws:sts::346869059911:assumed-role/ocpctl-worker-role/i-xxx
is not authorized to perform: ec2:SomeAction
```

**Solution**:
1. Check if the permission exists in the policy:
   ```bash
   aws iam get-policy-version \
     --policy-arn arn:aws:iam::346869059911:policy/ocpctl-worker-openshift-provisioning \
     --version-id v6 \
     --query 'PolicyVersion.Document.Statement[0].Action[]' \
     --output text | grep SomeAction
   ```

2. If missing, add it following the "Updating Permissions" section above

3. Verify it works:
   ```bash
   ssh worker 'aws iam simulate-principal-policy ...'
   ```

### Permission Changes Not Taking Effect

- **IAM policy updates are immediate** - no instance restart needed
- Wait 5-10 seconds for AWS IAM to propagate changes globally
- If still not working, check:
  - Is the new policy version set as default? `--set-as-default`
  - Is the role attached to the instance profile?
  - Is the instance profile assigned to the EC2 instance?

## Security Best Practices

1. **Least Privilege**: Only grant permissions required for cluster operations
2. **Use Managed Policies**: Easier to audit and update across all workers
3. **Tag Everything**: Use `ManagedBy: ocpctl` tags to track resources
4. **Resource ARNs**: Where possible, scope permissions to specific resource ARNs instead of `"*"`
5. **Monitor Usage**: Use CloudTrail to audit which permissions are actually used
6. **Regular Review**: Audit permissions quarterly to remove unused permissions

## Reference

- AWS IAM Policies: https://docs.aws.amazon.com/IAM/latest/UserGuide/access_policies.html
- OpenShift AWS Permissions: https://docs.openshift.com/container-platform/latest/installing/installing_aws/installing-aws-account.html
