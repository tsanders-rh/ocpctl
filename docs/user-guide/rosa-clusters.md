# ROSA Clusters User Guide

Complete guide to working with Red Hat OpenShift Service on AWS (ROSA) clusters in ocpctl.

## Overview

**ROSA (Red Hat OpenShift Service on AWS)** is a fully-managed OpenShift service where AWS manages the control plane infrastructure. This differs from OpenShift IPI (Installer Provisioned Infrastructure) where you manage all cluster components.

### Key Characteristics

- **Managed Control Plane**: AWS handles control plane operation, maintenance, and updates
- **Faster Provisioning**: 30-40 minutes vs 45-60 minutes for IPI
- **SLA-backed**: AWS provides uptime SLA for the control plane
- **STS Authentication**: Uses AWS STS (Security Token Service) with auto-created IAM roles
- **Always-on Control Plane**: Cannot be stopped, runs 24/7 at $0.03/hour

## When to Use ROSA

### Choose ROSA if you need:

✅ **Managed Service** - You want AWS to handle control plane operations
✅ **Faster Setup** - Need clusters quickly (30-40 min provisioning)
✅ **Production SLA** - Require AWS-backed uptime guarantees
✅ **Less Operational Overhead** - Prefer managed services over self-managed
✅ **Automatic Updates** - AWS handles control plane patches and updates

### Choose OpenShift IPI if you need:

✅ **Full Control** - Need to customize control plane configuration
✅ **Multi-Cloud** - Planning to run on GCP, IBM Cloud, or hybrid
✅ **Lower Cost** - Can fully hibernate to $0 (vs ROSA's $0.03/hr minimum)
✅ **Air-Gapped** - Need disconnected or restricted network deployments
✅ **Custom Networking** - Require advanced network configurations

## Cost Comparison

### ROSA Pricing

| Component | Cost | Notes |
|-----------|------|-------|
| Control Plane | $0.03/hour | Always running, cannot be stopped |
| Workers (m5.xlarge) | $0.192/hour each | Can be scaled to 0 during hibernation |
| **Minimal Profile (2 workers)** | **$0.414/hour** | $0.03 + (2 × $0.192) |
| **Hibernated** | **$0.03/hour** | Control plane only, 93% savings |

### OpenShift IPI Pricing

| Component | Cost | Notes |
|-----------|------|-------|
| Control nodes (3 × m6i.2xlarge) | $1.152/hour | Can be stopped during hibernation |
| Workers (3 × m6i.2xlarge) | $1.152/hour | Can be stopped during hibernation |
| **Standard Profile** | **$2.304/hour** | Full cluster running |
| **Hibernated** | **~$0/hour** | Only EBS storage (~$0.05/hr) |

### Monthly Cost Estimate

| Scenario | ROSA | IPI | Winner |
|----------|------|-----|--------|
| **24/7 Running** | ~$299/month | ~$1,663/month | 🏆 ROSA |
| **Work Hours Only** | ~$190/month | ~$467/month | 🏆 ROSA |
| **Minimal Usage** | ~$95/month | ~$150/month | 🏆 ROSA |
| **Hibernated** | ~$22/month | ~$4/month | 🏆 IPI |

**Takeaway**: ROSA is more cost-effective for actively-used clusters. IPI is better for infrequently-used clusters that can be fully hibernated.

## Creating a ROSA Cluster

### Via Web UI

1. Navigate to **Clusters** → **Create Cluster**
2. Select **Platform**: `AWS`
3. Select **Cluster Type**: `ROSA (Managed OpenShift)`
4. Choose a **Profile**:
   - `aws-rosa-minimal` - 2 workers, single-AZ, cost-effective
   - `aws-rosa-standard` - 3 workers, multi-AZ, production-ready
5. Configure:
   - **Name**: `my-rosa-cluster` (DNS-friendly, lowercase)
   - **Region**: `us-east-1` (or your preferred region)
   - **Version**: Select OpenShift version (4.18 - 4.22)
   - **TTL**: Time until auto-destruction
   - **Tags**: Owner, team, cost center
6. Click **Create Cluster**

### Via API

```bash
# Login and get token
TOKEN=$(curl -X POST https://api.ocpctl.mg.dog8code.com/v1/auth/login \
  -H "Content-Type: application/json" \
  -d '{"email":"user@example.com","password":"yourpassword"}' \
  | jq -r '.access_token')

# Create ROSA cluster
curl -X POST https://api.ocpctl.mg.dog8code.com/v1/clusters \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "my-rosa-cluster",
    "platform": "aws",
    "cluster_type": "rosa",
    "version": "4.20.3",
    "profile": "aws-rosa-minimal",
    "region": "us-east-1",
    "owner": "user@example.com",
    "team": "engineering",
    "cost_center": "dev-ops"
  }'
```

## Accessing Your ROSA Cluster

### Getting Credentials

ROSA clusters use **admin credentials** that expire after 72 hours:

1. Go to **Cluster Details** page
2. Find **Console Credentials** section
3. View username (usually `cluster-admin`)
4. Click the **eye icon** to reveal password
5. Click **copy** to copy password to clipboard

### Logging In

```bash
# oc login with credentials from UI
oc login https://api.your-cluster.openshiftapps.com:6443 \
  --username cluster-admin \
  --password <password-from-ui>

# Verify access
oc whoami
oc get nodes
```

### Downloading Kubeconfig

1. Go to **Cluster Details**
2. Click **Download Kubeconfig** button
3. Use the kubeconfig:
   ```bash
   export KUBECONFIG=./kubeconfig-my-rosa-cluster.yaml
   oc whoami
   ```

## Hibernating ROSA Clusters

### What Happens During Hibernation

- **Workers scale to 0** - All worker nodes are terminated
- **Control plane continues running** - Cannot be stopped (AWS-managed)
- **Cost drops to $0.03/hour** - Only control plane charges
- **Cluster is inaccessible** - No workloads can run

### Hibernate via Web UI

1. Go to **Cluster Details**
2. Click **Hibernate** button
3. Confirm the action
4. Status changes to **HIBERNATING** → **HIBERNATED**

### Hibernate via API

```bash
curl -X POST https://api.ocpctl.mg.dog8code.com/v1/clusters/{cluster-id}/hibernate \
  -H "Authorization: Bearer $TOKEN"
```

### Resume (Wake) Cluster

1. Go to **Cluster Details**
2. Click **Resume** button
3. Workers scale back to original count (2-5 minutes)
4. Status changes to **RESUMING** → **READY**

## Work Hours Automation

ROSA clusters support automatic hibernation during off-hours:

### Default Work Hours
- **Active**: Monday-Friday, 8am-6pm EST
- **Hibernated**: Nights and weekends

### Override Work Hours

To keep a ROSA cluster running 24/7:

1. Go to **Cluster Details**
2. Click **Edit**
3. Toggle **Ignore Work Hours** → `Enabled`
4. Save

Or via API:
```bash
curl -X PATCH https://api.ocpctl.mg.dog8code.com/v1/clusters/{cluster-id} \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"ignore_work_hours": true}'
```

## Destroying ROSA Clusters

### Via Web UI

1. Go to **Cluster Details**
2. Click **Destroy** button (red button)
3. Type cluster name to confirm
4. Click **Confirm Destruction**

### Via API

```bash
curl -X DELETE https://api.ocpctl.mg.dog8code.com/v1/clusters/{cluster-id} \
  -H "Authorization: Bearer $TOKEN"
```

### What Gets Deleted

ROSA deletion is comprehensive and automatic:
- ✅ Control plane (AWS-managed)
- ✅ Worker machine pools
- ✅ VPC and networking (if created by ROSA)
- ✅ Load balancers
- ✅ IAM roles (operator roles and account roles)
- ✅ OIDC provider
- ✅ S3 buckets (OIDC, bootstrap)
- ✅ Route53 DNS records

**No manual cleanup required** - ROSA handles everything.

## Post-Deployment Configuration

ROSA clusters support the same post-deployment addons as IPI OpenShift:

### Available Addons

- **OpenShift Virtualization (CNV)** - Run VMs on OpenShift
  - Supports Windows VMs with automated IRSA setup
  - Versions: 4.22 (stable-stage), 4.99 (nightly)
- **Migration Toolkit for Applications (MTA)** - Application migration analysis
- **Migration Toolkit for Containers (MTC)** - Migrate workloads between clusters
- **OpenShift API for Data Protection (OADP)** - Backup and restore

### Enabling Addons

1. Select addons during cluster creation, or
2. Go to **Cluster Details** → **Post-Deployment**
3. Select desired addons
4. Click **Run Post-Deployment**

## Troubleshooting

### Cluster Stuck in CREATING

**Symptom**: Cluster status doesn't progress beyond CREATING after 60 minutes

**Check**:
```bash
# View cluster creation logs
# Go to Cluster Details → Logs tab in UI
```

**Common causes**:
- AWS service quota limits (worker instance type)
- OCM token expired (check worker logs)
- Network connectivity issues

**Resolution**:
- Check AWS quotas for chosen instance type
- Refresh OCM token in worker.env
- Try a different region

### Cannot Login with Admin Credentials

**Symptom**: `oc login` fails with authentication error

**Causes**:
- Credentials expired (72-hour lifetime)
- Cluster not fully ready yet
- Wrong API URL

**Resolution**:
```bash
# Check cluster status
oc get clusterversion

# Regenerate admin credentials (Admin only)
# Contact ocpctl admin to run:
# rosa create admin --cluster=<cluster-name>
```

### Hibernation Not Reducing Costs

**Symptom**: Hibernated cluster still costs more than expected

**Check**: ROSA control plane always runs at $0.03/hour

**Expected costs**:
- Hibernated: $0.03/hour (control plane)
- Running: $0.03/hour + worker costs

If costs are higher, check:
- Workers actually scaled to 0: `rosa list machinepools --cluster=<name>`
- No additional AWS resources created (EBS volumes, snapshots)

### Cluster Won't Resume

**Symptom**: Resume operation fails or times out

**Check worker logs**:
```bash
# SSH to production
ssh -i ~/.ssh/ocpctl-production-key ubuntu@44.201.165.78

# Check logs
sudo journalctl -u ocpctl-worker -f | grep <cluster-name>
```

**Common issues**:
- AWS instance limits (can't launch workers)
- Network issues during worker provisioning

**Resolution**:
- Check AWS service quotas
- Verify VPC/subnet configuration intact
- Try destroy and recreate if stuck

## Best Practices

### Cluster Naming

✅ **Good**: `dev-api-testing`, `qa-frontend-app`, `staging-ml-pipeline`
❌ **Bad**: `TEST123`, `my-cluster`, `temp`

- Use descriptive names indicating purpose
- Include environment (dev/qa/staging/prod)
- Keep DNS-friendly (lowercase, hyphens)

### Tagging

Always include:
- **Owner**: Your email
- **Team**: Your team name
- **Cost Center**: Billing code
- **Purpose**: Why cluster exists

Example:
```json
{
  "owner": "developer@company.com",
  "team": "platform-engineering",
  "cost_center": "eng-r-and-d",
  "purpose": "testing-new-operator"
}
```

### TTL Management

- **Development**: 72 hours (3 days)
- **Testing**: 168 hours (1 week)
- **Staging**: 336 hours (2 weeks)
- **Production**: Request extended TTL from admin

### Cost Optimization

1. **Use work hours automation** - Let clusters hibernate nights/weekends
2. **Right-size workers** - Start with minimal profile, scale up if needed
3. **Set appropriate TTL** - Don't let forgotten clusters run indefinitely
4. **Clean up when done** - Destroy clusters you're not actively using

### Security

1. **Rotate credentials** - Admin password expires every 72 hours
2. **Use RBAC** - Configure project-level access, not cluster-admin everywhere
3. **Monitor access** - Review audit logs periodically
4. **Tag appropriately** - Helps with compliance and cost tracking

## FAQ

### Can I upgrade a ROSA cluster?

Not through ocpctl currently. ROSA upgrades are managed through AWS console or `rosa` CLI directly.

### Can I add more workers to a ROSA cluster?

Not through ocpctl UI. Machine pool scaling is managed through `rosa edit machinepool` CLI command.

### What regions support ROSA?

All major AWS regions:
- us-east-1, us-east-2, us-west-2
- eu-west-1, eu-central-1
- ap-southeast-1, ap-southeast-2

Full list: https://docs.openshift.com/rosa/rosa_architecture/rosa_policy_service_definition/rosa-service-definition.html#rosa-sdpolicy-regions-az_rosa-service-definition

### Can I convert a ROSA cluster to IPI or vice versa?

No. ROSA and IPI are fundamentally different architectures. You would need to:
1. Backup workloads from source cluster
2. Create new cluster of desired type
3. Restore workloads to new cluster

Consider using Migration Toolkit for Containers (MTC) addon for this.

### Does ROSA support private clusters?

Yes, ROSA supports AWS PrivateLink for private clusters. This is configured at profile level - contact admin to create custom profile with `privateLink: true`.

### Can I use ROSA in an existing VPC?

ROSA can deploy into existing VPCs, but this requires custom profile configuration. Contact admin for assistance.

## Additional Resources

- **ROSA Documentation**: https://docs.openshift.com/rosa/
- **ROSA Support Plan**: [docs/features/ROSA_SUPPORT_PLAN.md](../features/ROSA_SUPPORT_PLAN.md)
- **Cluster Management Guide**: [cluster-management.md](cluster-management.md)
- **Getting Started**: [getting-started.md](getting-started.md)
- **Feature Matrix**: [docs/reference/FEATURE_MATRIX.md](../reference/FEATURE_MATRIX.md)

## Support

For issues or questions:
- Internal ocpctl team: ocpctl-support@company.com
- GitHub Issues: https://github.com/tsanders-rh/ocpctl/issues
- Red Hat ROSA Support: https://access.redhat.com/support/

---

**Last Updated**: May 7, 2026
**Version**: ocpctl v0.20260507.a07a92c
