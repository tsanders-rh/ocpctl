# Cluster Management Guide

This guide covers creating, configuring, and managing OpenShift clusters with OCPCTL.

## Creating a Cluster

### Step 1: Navigate to Cluster Creation

1. Click **Clusters** in the sidebar
2. Click the **Create Cluster** button (top right)

### Step 2: Choose a Profile

Select a cluster profile that matches your needs:

#### aws-sno-test (Recommended for Testing)
- **Configuration:** 1 control-plane node (schedulable, Single Node OpenShift)
- **Workers:** 0 worker nodes
- **Cost:** ~$0.80/hour
- **Max TTL:** 24 hours, Default: 8 hours
- **Best for:** Rapid testing and development

#### aws-minimal-test
- **Configuration:** 3 control-plane nodes (schedulable)
- **Workers:** 0 worker nodes
- **Max TTL:** 72 hours
- **Best for:** Multi-node testing without dedicated workers

#### aws-standard
- **Configuration:** 3 control-plane nodes + 3 worker nodes (m6i.2xlarge)
- **Max TTL:** 168 hours (7 days)
- **Best for:** Standard development and integration testing

#### aws-virtualization
- **Configuration:** 3 control-plane nodes (m6i.2xlarge) + 3 worker nodes (m6i.metal)
- **Cost:** ~$35.50/hour
- **Max TTL:** 168 hours, Default: 72 hours
- **Best for:** OpenShift Virtualization workloads requiring nested virtualization

### Step 3: Configure Cluster Details

Fill in the required fields:

**Basic Information:**
- **Cluster Name** - Unique identifier (3-63 characters, lowercase, numbers, hyphens)
- **Platform** - Select `aws` (IBM Cloud coming soon)
- **Version** - OpenShift version (e.g., 4.20.3)
- **Region** - AWS region (e.g., us-east-1)

**Organization Details:**
- **Owner Email** - Your email address (pre-filled)
- **Team** - Your team name for cost tracking
- **Cost Center** - Cost center code for billing

**Configuration:**
- **Base Domain** - DNS domain for the cluster (e.g., mg.dog8code.com)
- **TTL (Hours)** - How long the cluster should live before auto-destruction
  - Must be within the profile's allowed range
  - Default value is used if not specified
- **SSH Public Key** (Optional) - For debugging cluster nodes
- **Extra Tags** (Optional) - Additional AWS tags for cost tracking

**Advanced Options:**
- **Enable EFS Storage** - Attach AWS EFS for shared storage
- **Work Hours Enabled** - Enable automatic hibernation outside business hours
  - **Work Hours Schedule** - Define when cluster should be running (e.g., Mon-Fri 9am-5pm ET)
- **Off-hours Opt-in** - Allow cluster to run outside defined work hours

### Step 4: Create Cluster

1. Review your configuration
2. Click **Create Cluster**
3. Wait for provisioning to complete (typically 30-45 minutes)

**Status Updates:**
- **Pending** - Request accepted, waiting for worker to pick up
- **Provisioning** - Cluster is being created
- **Ready** - Cluster is available for use
- **Failed** - Provisioning failed (check logs)

## Cluster Operations

### Downloading Kubeconfig

Access your cluster using the kubeconfig file:

1. Click on your cluster card
2. Click **Download Kubeconfig** button
3. Save the file (e.g., `kubeconfig-mycluster.yaml`)
4. Use with kubectl:

```bash
export KUBECONFIG=/path/to/kubeconfig-mycluster.yaml
kubectl get nodes
oc get projects
```

**Security Note:** Kubeconfig download links expire after 15 minutes for security.

### Extending TTL

Postpone automatic cluster destruction:

1. Click on your cluster card
2. Click **Extend TTL** tab
3. Enter additional hours (must be within profile's max TTL)
4. Click **Extend**

**Policy Enforcement:** Extension requests are validated against:
- Profile maximum TTL
- Organizational policies
- Your user permissions

### Hibernating a Cluster

Save costs by shutting down cluster when not in use:

1. Click on your cluster card
2. Click **Hibernate** button
3. Confirm hibernation

**What happens:**
- All EC2 instances are stopped
- EBS volumes are retained
- Cluster data is preserved
- **Costs reduced by ~90%** (only storage charges apply)

**Automatic Hibernation:**
- If work hours are enabled, cluster hibernates automatically outside your schedule
- Next wake time is displayed on cluster details

### Resuming a Cluster

Start a hibernated cluster:

1. Click on your cluster card
2. Click **Resume** button
3. Wait for cluster to start (typically 5-10 minutes)

**Grace Periods:**
- Manual resume during off-hours: 8-hour grace period before next auto-hibernation
- Prevents cluster from immediately hibernating after manual resume

### Destroying a Cluster

Permanently delete a cluster:

1. Click on your cluster card
2. Click **Destroy** button
3. Confirm destruction

**Important:**
- This action is **irreversible**
- All cluster resources will be deleted
- Destruction typically takes 15-30 minutes
- Cluster status changes to **Destroying** then **Destroyed**

## Cluster Profiles

### Understanding Profiles

Cluster profiles are pre-configured templates that define:
- Node count and instance types
- Network configuration
- Maximum and default TTL
- Enabled features (hibernation, EFS, etc.)
- Cost estimates

### Profile Policies

Each profile enforces policies:
- **TTL Limits** - Maximum lifetime for clusters
- **Resource Quotas** - Node counts and instance sizes
- **Feature Flags** - Which features are enabled
- **Cost Controls** - Prevent expensive configurations

### Custom Profiles

Contact your administrator to:
- Request new profile configurations
- Modify existing profile policies
- Enable features for your team

## Work Hours and Hibernation

### Configuring Work Hours

When creating a cluster with work hours enabled:

1. Check **Work Hours Enabled**
2. Define your schedule:
   - **Days:** Monday-Friday (default)
   - **Start Time:** 9:00 AM (default)
   - **End Time:** 5:00 PM (default)
   - **Timezone:** America/New_York (default)
3. Optionally check **Off-hours Opt-in** to allow manual override

### How It Works

- **During work hours:** Cluster stays running
- **Outside work hours:** Cluster automatically hibernates
- **Grace periods:** 8 hours after manual resume to prevent immediate re-hibernation
- **Next check:** Displayed on cluster details page

### Manual Override

If you need access outside work hours:
1. Manually resume the cluster
2. You get an 8-hour grace period
3. After grace period, auto-hibernation resumes

## Troubleshooting

### Cluster Creation Failed

**Check the logs:**
1. Click on failed cluster
2. View **Logs** tab for error details

**Common issues:**
- AWS quotas exceeded (contact AWS support)
- Invalid configuration (check profile requirements)
- Network/DNS issues (verify base domain)
- Permission issues (contact administrator)

### Cannot Download Kubeconfig

**Possible causes:**
- Download link expired (15-minute limit) - refresh the page
- Cluster not ready yet - wait for **Ready** status
- Browser blocking download - check browser settings

### Cluster Stuck in Provisioning

**Typical provisioning time:** 30-45 minutes

**If stuck >1 hour:**
1. Check job status in admin dashboard (admin only)
2. Contact support with cluster ID
3. Check for AWS service issues

### Hibernation Not Working

**Verify:**
- Work hours schedule is correctly configured
- Current time is outside work hours window
- Cluster has been running long enough (check next hibernation time)
- No active grace period from recent manual resume

## Best Practices

### Cluster Naming

- Use descriptive names: `myapp-dev-cluster`, `testing-feature-x`
- Include purpose or ticket number: `jira-1234-test`
- Avoid generic names: `test`, `cluster1`

### TTL Management

- **Testing:** Use short TTLs (4-8 hours)
- **Development:** 24-48 hours
- **Integration:** Up to 7 days
- Extend TTL as needed rather than starting with maximum

### Cost Optimization

- Use **aws-sno-test** for simple testing (~$0.80/hour vs $5+/hour)
- Enable **work hours hibernation** for overnight cost savings
- Destroy clusters when done instead of letting them expire
- Use smaller profiles when possible

### Resource Planning

- **SNO** for simple testing and demos
- **Minimal** for multi-node testing
- **Standard** for realistic workloads
- **Virtualization** only when you need nested virt

## Getting Help

- **Documentation:** This user guide and API docs
- **Support:** Contact your OCPCTL administrator
- **Issues:** Report bugs to your internal support channel
