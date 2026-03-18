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

#### aws-virt-windows-minimal
- **Configuration:** 3 control-plane nodes (m6i.2xlarge) + 2 worker nodes (m6i.metal)
- **Cost:** ~$25/hour
- **Max TTL:** 168 hours, Default: 72 hours
- **Post-Deployment:** Automatically configures IRSA for Windows VM image access
- **Best for:** OpenShift Virtualization with Windows VM workloads

**Note:** Some profiles include automated post-deployment configuration. See [Post-Deployment Configuration](#post-deployment-configuration) below for details.

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
- **Skip Post-Deployment** - Skip automated configuration (operators, scripts, etc.) for profiles that include it

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

## Post-Deployment Configuration

### What is Post-Deployment Configuration?

Some cluster profiles include **automated post-deployment configuration** that runs automatically after your cluster is created and reaches **Ready** status. This automation can install operators, apply manifests, run scripts, or deploy Helm charts to prepare your cluster for specific workloads.

**When it runs:**
- Automatically triggered after cluster creation completes
- Runs as a separate `POST_CONFIGURE` job
- Typically takes 5-15 minutes depending on configuration
- Can be monitored via cluster status indicators

### What Can Be Automated?

Post-deployment configuration can include:

**Operators:**
- OpenShift operators installed via OLM (Operator Lifecycle Manager)
- Example: OpenShift Virtualization, OpenShift Data Foundation, Serverless
- Configured with custom resources if needed

**Scripts:**
- Shell scripts for custom configuration tasks
- Automatically provided with cluster context (name, region, kubeconfig)
- Example: IRSA setup for Windows VM image access in aws-virt-windows-minimal

**Manifests:**
- Kubernetes/OpenShift YAML manifests
- Applied directly to the cluster
- Example: Custom NetworkPolicies, ConfigMaps, ServiceAccounts

**Helm Charts:**
- Helm 3 chart installations
- Deployed with custom values if needed

### Viewing Post-Deployment Details

To see what will be installed automatically:

1. Navigate to **Profiles** page in the sidebar
2. Find your desired profile (e.g., aws-virt-windows-minimal)
3. Look for the **Post-Deployment** section in the profile card

This section shows:
- Which operators will be installed
- What scripts will run (with descriptions)
- Which manifests will be applied
- What Helm charts will be deployed

**Example:** The aws-virt-windows-minimal profile shows:
```
Post-Deployment
Automatically installed after cluster creation:

Script: setup-windows-irsa
  Automated IRSA setup for Windows VM image access from S3
```

### Skipping Post-Deployment

If you want a clean cluster without automated configuration:

1. When creating the cluster, expand **Advanced Options**
2. Check **Skip Post-Deployment**
3. Create the cluster normally

The cluster will reach **Ready** status without running any post-deployment automation.

**When to skip:**
- Testing cluster creation without extra automation
- You want to manually configure the cluster
- Troubleshooting post-deployment issues
- You don't need the automated features

### Monitoring Post-Deployment Progress

After cluster creation:

1. Cluster reaches **Ready** status (provisioning complete)
2. Post-deployment job starts automatically
3. Check cluster details page for post-deployment status:
   - **In Progress** - Configuration running
   - **Completed** - All automation succeeded
   - **Failed** - Configuration encountered errors

**Note:** The cluster is usable immediately when it reaches **Ready** status. Post-deployment runs in parallel and adds additional capabilities.

### Troubleshooting Post-Deployment Failures

If post-deployment fails:

**Check the logs:**
1. Navigate to cluster details page
2. View post-deployment job logs
3. Look for error messages in the configuration steps

**Common issues:**
- Operator catalog unavailable (temporary, retry later)
- Script permission errors (contact administrator)
- Resource quota exceeded (contact administrator)
- Network connectivity issues

**Impact on cluster:**
- Cluster is still **Ready** and usable
- Only automated features are affected
- You can manually install failed components if needed

### Example: Windows Virtualization Profile

The **aws-virt-windows-minimal** profile includes this post-deployment automation:

**What it does:**
- Configures AWS IAM Roles for Service Accounts (IRSA)
- Sets up permissions for Windows VM images stored in S3
- Creates necessary ServiceAccounts and IAM role mappings
- Verifies connectivity to S3 bucket

**Why it's automated:**
- Eliminates manual IRSA setup steps
- Ensures correct AWS permissions for Windows VMs
- Faster time-to-value for Windows virtualization workloads

**Result:**
After post-deployment completes, you can immediately deploy Windows VMs using the pre-configured image access, without manual AWS IAM configuration.

## Cluster Profiles

### Understanding Profiles

Cluster profiles are pre-configured templates that define:
- Node count and instance types
- Network configuration
- Maximum and default TTL
- Enabled features (hibernation, EFS, etc.)
- Post-deployment automation (operators, scripts, manifests)
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

### Profile Selection

- **Check the Profiles page** before creating clusters to see what automation is included
- **Use profiles with post-deployment** when you need specific operators or configurations pre-installed
- **Skip post-deployment** if you want manual control over cluster configuration
- **Review post-deployment time** - factor in an extra 5-15 minutes for automated configuration
- **aws-virt-windows-minimal** for Windows VMs - includes automated IRSA setup

## Getting Help

- **Documentation:** This user guide and API docs
- **Support:** Contact your OCPCTL administrator
- **Issues:** Report bugs to your internal support channel
