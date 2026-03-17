"use client";

import { useState } from "react";
import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";
import { Book, Users, Shield, Sparkles } from "lucide-react";

const userGuides = [
  {
    id: "getting-started",
    title: "Getting Started",
    icon: Book,
    content: `# Getting Started with OCPCTL

Welcome to OCPCTL! This guide will help you get started with creating and managing ephemeral OpenShift clusters.

## What is OCPCTL?

OCPCTL is a self-service platform for provisioning and managing ephemeral OpenShift 4.20 clusters on AWS. It provides:

- **Self-service cluster creation** - Request clusters through an easy-to-use web interface
- **Automated lifecycle management** - Clusters are automatically destroyed after their TTL expires
- **Work hours hibernation** - Clusters can automatically hibernate outside business hours to save costs
- **Policy enforcement** - Cluster configurations are validated against organizational policies
- **Audit trail** - All operations are logged for compliance and tracking

## Logging In

OCPCTL supports two authentication methods:

### JWT Authentication (Email/Password)

1. Navigate to your OCPCTL deployment URL
2. Click **Sign In**
3. Enter your email address and password
4. Click **Sign In** button

**First-time users:** Contact your administrator to create an account for you.

### IAM Authentication (AWS Credentials)

1. Navigate to your OCPCTL deployment URL
2. Click **Sign In with AWS IAM**
3. Enter your AWS Access Key ID
4. Enter your AWS Secret Access Key
5. Click **Authenticate**

**Note:** IAM authentication uses AWS STS to verify your credentials and map your IAM identity to an OCPCTL user.

## Dashboard Overview

After logging in, you'll see the main dashboard with several sections:

### Sidebar Navigation

- **Clusters** - View and manage your clusters
- **Profiles** - Browse available cluster profiles
- **API Documentation** - Interactive API reference (Swagger UI)
- **User Guide** - This documentation
- **Admin Dashboard** (Admin only) - System overview and orphaned resources
- **User Management** (Admin only) - Manage user accounts

### Clusters Page

The Clusters page shows all clusters you have access to:

- **Your clusters** (Regular users) - Only clusters you created
- **All clusters** (Admins) - All clusters in the system

Each cluster card displays:
- **Name** - Cluster identifier
- **Status** - Current state (Ready, Provisioning, Destroying, etc.)
- **Platform** - AWS or IBM Cloud
- **Profile** - Configuration template used
- **Region** - Geographic location
- **Created** - When the cluster was created
- **Expires** - When the cluster will be automatically destroyed
- **Owner** - Who created the cluster

### Cluster Actions

Click on a cluster card to view details and perform actions:
- **Download Kubeconfig** - Get credentials to access the cluster
- **Extend TTL** - Postpone automatic destruction
- **Hibernate** - Shut down cluster to save costs
- **Resume** - Start a hibernated cluster
- **Destroy** - Immediately delete the cluster

## What's Next?

Click on **Cluster Management** in the navigation to learn how to create and manage clusters.
`,
  },
  {
    id: "cluster-management",
    title: "Cluster Management",
    icon: Users,
    content: `# Cluster Management Guide

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
- **Platform** - Select \`aws\` (IBM Cloud coming soon)
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
3. Save the file (e.g., \`kubeconfig-mycluster.yaml\`)
4. Use with kubectl:

\`\`\`bash
export KUBECONFIG=/path/to/kubeconfig-mycluster.yaml
kubectl get nodes
oc get projects
\`\`\`

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

## Best Practices

### Cluster Naming

- Use descriptive names: \`myapp-dev-cluster\`, \`testing-feature-x\`
- Include purpose or ticket number: \`jira-1234-test\`
- Avoid generic names: \`test\`, \`cluster1\`

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

## Getting Help

- **Documentation:** This user guide and API docs
- **Support:** Contact your OCPCTL administrator
- **Issues:** Report bugs to your internal support channel
`,
  },
  {
    id: "admin-features",
    title: "Admin Features",
    icon: Shield,
    content: `# Admin Features Guide

This guide covers administrative features available to users with Admin role.

## Admin Dashboard

Access the admin dashboard by clicking **Admin Dashboard** in the sidebar.

The dashboard provides:
- **Cluster Statistics** - System-wide metrics and cost breakdowns
- **Orphaned Resources** - AWS resources without database entries
- **User Management** - Create and manage user accounts

### Cluster Statistics

View comprehensive system metrics:

**Status Overview:**
- Total clusters by status (Ready, Provisioning, Hibernating, etc.)
- Active vs. inactive cluster counts
- Failed cluster tracking

**Cost Analysis:**
- Cost breakdown by profile (aws-sno-test, aws-standard, etc.)
- Cost breakdown by user/team
- Estimated hourly rates
- Total system cost projections

**Usage Patterns:**
- Most popular profiles
- Average cluster lifespan
- Peak usage times

## Orphaned Resources Management

**What are orphaned resources?**

Orphaned resources are AWS resources (VPCs, load balancers, DNS records, etc.) that:
- Were created by OpenShift clusters
- No longer have a corresponding database entry
- May have been left behind due to failed destructions

### Viewing Orphaned Resources

1. Click **Admin Dashboard** in sidebar
2. Navigate to **Orphaned Resources** tab
3. View list of detected resources

**Resource Types Detected:**
- **VPCs** - Virtual Private Clouds
- **Load Balancers** - Network/Application Load Balancers
- **Hosted Zones** - Route53 DNS zones
- **DNS Records** - Route53 A/CNAME records
- **EC2 Instances** - Orphaned EC2 instances
- **S3 Buckets** - Cluster-related S3 buckets

**Resource Information Displayed:**
- **Type** - Resource type (VPC, LoadBalancer, etc.)
- **Resource ID** - AWS resource identifier
- **Cluster Name** - Associated cluster (if detectable)
- **Region** - AWS region
- **Status** - Active, Resolved, or Ignored
- **First Detected** - When orphan was first discovered
- **Tags** - AWS tags including ManagedBy=ocpctl

### Filtering Orphaned Resources

Use filters to narrow down the list:

- **Status Filter** - Active, Resolved, Ignored
- **Type Filter** - VPC, LoadBalancer, HostedZone, etc.
- **Region Filter** - us-east-1, us-west-2, etc.

### Resource Actions

For each orphaned resource, you can:

#### Mark as Resolved

Use when you've manually cleaned up a resource in AWS Console:

1. Click **Mark Resolved** button
2. Add optional notes (e.g., "Manually deleted in AWS Console")
3. Confirm action

**Effect:** Resource status changes to "Resolved" and is hidden from active list.

#### Mark as Ignored

Use for false positives or resources you want to keep:

1. Click **Mark Ignored** button
2. Add reason (e.g., "Shared VPC - do not delete")
3. Confirm action

**Effect:** Resource status changes to "Ignored" and is hidden from active list.

#### Delete Resource

**⚠️ DANGER: This actually deletes the AWS resource!**

Currently supported for automated deletion:
- **Hosted Zones** - Deletes all DNS records and the zone
- **DNS Records** - Deletes specific A/CNAME records

**Not supported** (must delete manually in AWS Console):
- VPCs
- Load Balancers
- EC2 Instances
- S3 Buckets

**To delete a resource:**

1. Click **Delete** button
2. Review the warning message
3. Type the resource name to confirm
4. Click **Delete Resource**

**What happens:**
- Resource is deleted from AWS (irreversible!)
- Database record is marked as "Resolved"
- Audit log entry is created

### Orphan Detection Logic

OCPCTL uses a **hybrid detection** approach:

**Tag-based Detection (Primary):**
- Checks for \`ManagedBy=ocpctl\` tag
- Verifies cluster exists in database
- Accurate for clusters created after Phase 2

**Pattern Matching (Fallback):**
- Looks for OpenShift naming patterns
- Used for clusters created before tagging
- May have false positives from non-ocpctl clusters

### Best Practices

**Regular Review:**
- Check orphaned resources weekly
- Investigate resources older than 48 hours
- Clean up confirmed orphans promptly

**Cost Impact:**
- Orphaned VPCs cost ~$0.10/day (minimal)
- Orphaned load balancers cost ~$0.50/day
- Total orphan cost is usually <$20/month

**Investigation Process:**
1. Check if cluster recently failed to destroy
2. Look for matching cluster name in database
3. Verify resource tags in AWS Console
4. Confirm resource is truly orphaned before deleting

## User Management

**Access:** Admin Dashboard → Users tab

### Creating Users

1. Click **Create User** button
2. Fill in user details:
   - **Email** - User's email address (used for login)
   - **Username** - Display name
   - **Password** - Initial password (user should change)
   - **Role** - Admin, User, or Viewer
3. Click **Create**

**User Roles:**
- **Admin** - Full access including user management and orphaned resources
- **User** - Can create/manage own clusters
- **Viewer** - Read-only access to own clusters

### Updating Users

1. Find user in list
2. Click **Edit** button
3. Update fields:
   - Username
   - Role
   - Active/Inactive status
   - Work hours settings
4. Click **Save**

### Deactivating Users

Instead of deleting users (which preserves audit trail):

1. Click **Edit** on user
2. Uncheck **Active** checkbox
3. Click **Save**

**Effect:** User cannot log in but cluster ownership history is preserved.

### Resetting Passwords

**Current limitation:** Admins cannot reset user passwords directly.

**Workaround:**
1. Delete the user account
2. Recreate with same email
3. Provide new temporary password
4. User changes password on first login

## Audit Logs

All administrative actions are logged:

- User creation/updates/deletion
- Cluster creation/destruction
- Orphaned resource resolution
- Kubeconfig downloads

**Access:** Currently via database queries (web UI coming soon)

\`\`\`sql
SELECT * FROM audit_events
WHERE action LIKE 'orphan.%'
ORDER BY created_at DESC
LIMIT 50;
\`\`\`

## AWS Resource Tagging

**Background Feature (Automatic)**

All AWS resources created by clusters are automatically tagged with:

\`\`\`
ManagedBy: ocpctl
ClusterName: <cluster-name>
Profile: <profile-name>
InfraID: <openshift-infra-id>
CreatedAt: <timestamp>
OcpctlVersion: <version>
kubernetes.io/cluster/<infraID>: owned
\`\`\`

**Benefits:**
- **Accurate orphan detection** - Distinguishes ocpctl clusters from others
- **Cost attribution** - Track costs by cluster/profile/team
- **Compliance** - Identify cluster ownership
- **FinOps reporting** - AWS Cost Explorer filtering by tags

**Viewing Tags:**
1. Open AWS Console
2. Navigate to EC2 → VPCs (or other resource type)
3. Select a cluster resource
4. View Tags tab

**Cost Reporting:**
1. Open AWS Cost Explorer
2. Group by: Tag → ManagedBy
3. Filter: ManagedBy = ocpctl
4. View cluster-by-cluster breakdown

## Statistics and Reporting

**Cluster Statistics Dashboard:**

View system-wide metrics including:
- Total active clusters
- Cost breakdown by profile
- Cost breakdown by user
- Resource utilization
- Popular profiles

**Export Data:**
Currently requires database queries. Web UI export coming soon.

## Administrative Best Practices

**Security:**
- Limit admin role to trusted users only
- Regularly review user access
- Monitor audit logs for suspicious activity

**Cost Management:**
- Review orphaned resources weekly
- Clean up old destroyed cluster records
- Monitor total system costs via AWS Cost Explorer

**User Support:**
- Create accounts promptly for new team members
- Provide onboarding documentation
- Set up work hours templates for common schedules

**Maintenance:**
- Keep orphaned resource list under 20 items
- Investigate failed cluster creations
- Review statistics for usage patterns
`,
  },
  {
    id: "advanced-features",
    title: "Advanced Features",
    icon: Sparkles,
    content: `# Advanced Features Guide

This guide covers advanced features including post-deployment configurations, storage linking, and API usage.

## Post-Deployment Configurations

**What are post-deployment configurations?**

After a cluster is created and reaches "Ready" status, OCPCTL can automatically apply additional configurations:

- **Operator Installations** - Install operators from OperatorHub
- **Custom Resources** - Apply CRDs and custom resources
- **Helm Charts** - Deploy Helm charts
- **Manifest Files** - Apply any Kubernetes YAML manifests

### How It Works

**Automatic Configuration:**
1. Cluster creation completes (status: Ready)
2. Post-deployment job is created automatically
3. Configurations from profile are applied
4. Each configuration tracked individually

**Configuration Status:**
- **Pending** - Queued for application
- **In Progress** - Currently being applied
- **Completed** - Successfully applied
- **Failed** - Application failed (can be retried)
- **Skipped** - User chose to skip during creation

### Viewing Configurations

1. Click on a cluster card
2. Navigate to **Configurations** tab
3. View list of applied/pending configurations

**Information Displayed:**
- **Name** - Configuration identifier
- **Type** - Operator, CustomResource, HelmChart, or Manifest
- **Status** - Current state
- **Applied At** - When configuration was completed
- **Error** - Error message if failed

### Retrying Failed Configurations

If a configuration fails (e.g., due to temporary network issues):

1. Go to cluster **Configurations** tab
2. Find failed configuration
3. Click **Retry** button
4. Wait for retry to complete

**What happens:**
- Configuration status reset to "Pending"
- New job created to reapply
- Original configuration is replaced on success

### Manually Triggering Post-Deployment

If you skipped post-deployment during creation:

1. Go to cluster details page
2. Click **Trigger Post-Deployment** button
3. Confirm action
4. Wait for configurations to apply (5-10 minutes)

**Use Cases:**
- Skipped during creation but now need operators
- Want to reapply all configurations
- Testing configuration changes

### Skipping Post-Deployment

When creating a cluster, you can skip post-deployment:

1. In cluster creation form
2. Check **Skip Post-Deployment Configuration**
3. Create cluster

**Why skip?**
- Faster cluster creation (saves 5-10 minutes)
- Testing base OpenShift installation
- Planning to install operators manually
- Troubleshooting cluster issues

**Note:** You can always trigger post-deployment later!

## Storage Linking

**What is storage linking?**

Share persistent storage (EFS volumes) between clusters for:
- **Data migration** - Move data from old cluster to new
- **Shared datasets** - Multiple clusters access same data
- **Blue/green deployments** - Share storage across environments

### Prerequisites

Both clusters must:
- Be in the same AWS region
- Have EFS storage enabled
- Be in "Ready" status
- Belong to you (or you're an admin)

### Linking Storage

**To link storage TO your cluster FROM another cluster:**

1. Navigate to cluster details
2. Go to **Storage** tab
3. Click **Link Storage** button
4. Enter target cluster name or ID
5. Select source cluster from dropdown
6. Click **Link**

**What happens:**
- Storage group created if it doesn't exist
- Your cluster added to storage group
- EFS mount configuration provided
- Estimated time: 5-10 minutes

### Viewing Linked Storage

In the **Storage** tab, you'll see:

**Storage Group Information:**
- **Storage Group ID** - Unique identifier
- **Storage Type** - EFS, EBS, etc.
- **Status** - Ready, Provisioning, Failed
- **EFS File System ID** - AWS resource ID
- **Region** - AWS region

**Linked Clusters:**
- List of all clusters sharing this storage
- Your role (Source or Replica)
- When linked

### Mounting Linked Storage

**EFS Storage:**

OCPCTL provides mount configuration:

\`\`\`yaml
apiVersion: v1
kind: PersistentVolume
metadata:
  name: shared-efs
spec:
  capacity:
    storage: 100Gi
  accessModes:
    - ReadWriteMany
  csi:
    driver: efs.csi.aws.com
    volumeHandle: <efs-id>
\`\`\`

**To use in your cluster:**

1. Copy mount configuration from Storage tab
2. Apply to your cluster:
   \`\`\`bash
   kubectl apply -f pv-efs.yaml
   \`\`\`
3. Create PersistentVolumeClaim referencing the PV
4. Mount in your pods

### Unlinking Storage

To remove access to shared storage:

1. Go to cluster **Storage** tab
2. Find storage group to unlink
3. Click **Unlink** button
4. Confirm action

**Effect:**
- Your cluster removed from storage group
- Other clusters still have access
- Data is not deleted

**Warning:** Ensure no pods are using the storage before unlinking!

## API Documentation

OCPCTL provides a comprehensive REST API with Swagger/OpenAPI documentation.

### Accessing API Docs

**Web UI:**
1. Click **API Documentation** in sidebar
2. Opens Swagger UI in new tab
3. URL: \`http://your-domain.com/swagger/index.html\`

**Direct Access:**
- Swagger UI: \`/swagger/index.html\`
- OpenAPI JSON: \`/swagger/doc.json\`
- OpenAPI YAML: \`/swagger/swagger.yaml\`

### API Categories

**Authentication:**
- POST \`/auth/login\` - User login
- POST \`/auth/logout\` - User logout
- POST \`/auth/refresh\` - Refresh token
- GET \`/auth/me\` - Get current user
- PATCH \`/auth/me\` - Update profile
- POST \`/auth/password\` - Change password

**Clusters:**
- POST \`/clusters\` - Create cluster
- GET \`/clusters\` - List clusters
- GET \`/clusters/{id}\` - Get cluster details
- DELETE \`/clusters/{id}\` - Destroy cluster
- PATCH \`/clusters/{id}/extend\` - Extend TTL
- POST \`/clusters/{id}/hibernate\` - Hibernate cluster
- POST \`/clusters/{id}/resume\` - Resume cluster
- GET \`/clusters/{id}/outputs\` - Get outputs
- GET \`/clusters/{id}/kubeconfig\` - Download kubeconfig

**Configurations:**
- GET \`/clusters/{id}/configurations\` - List configs
- POST \`/clusters/{id}/configure\` - Trigger post-deployment
- PATCH \`/clusters/{id}/configurations/{config_id}/retry\` - Retry failed

**Orphaned Resources (Admin):**
- GET \`/admin/orphaned-resources\` - List orphans
- GET \`/admin/orphaned-resources/stats\` - Get statistics
- PATCH \`/admin/orphaned-resources/{id}/resolve\` - Mark resolved
- DELETE \`/admin/orphaned-resources/{id}\` - Delete resource

**Jobs:**
- GET \`/jobs\` - List jobs
- GET \`/jobs/{id}\` - Get job details

**Profiles:**
- GET \`/profiles\` - List profiles
- GET \`/profiles/{name}\` - Get profile details

**Users (Admin):**
- GET \`/users\` - List users
- POST \`/users\` - Create user
- PATCH \`/users/{id}\` - Update user
- DELETE \`/users/{id}\` - Delete user

### Using the API

**Step 1: Authentication**

Get access token via login:

\`\`\`bash
curl -X POST http://ocpctl.example.com/api/v1/auth/login \\
  -H "Content-Type: application/json" \\
  -d '{
    "email": "user@example.com",
    "password": "your-password"
  }'
\`\`\`

**Response:**
\`\`\`json
{
  "user": { ... },
  "access_token": "eyJhbGciOiJIUzI1...",
  "expires_in": 3600
}
\`\`\`

**Step 2: Make API Calls**

Include token in Authorization header:

\`\`\`bash
TOKEN="eyJhbGciOiJIUzI1..."

# List clusters
curl -X GET http://ocpctl.example.com/api/v1/clusters \\
  -H "Authorization: Bearer $TOKEN"

# Create cluster
curl -X POST http://ocpctl.example.com/api/v1/clusters \\
  -H "Authorization: Bearer $TOKEN" \\
  -H "Content-Type: application/json" \\
  -d '{
    "name": "api-test-cluster",
    "profile": "aws-sno-test",
    "platform": "aws",
    "version": "4.20.3",
    "region": "us-east-1",
    "base_domain": "mg.dog8code.com"
  }'
\`\`\`

**Step 3: Try It Out (Swagger UI)**

1. Open Swagger UI
2. Click **Authorize** button (top right)
3. Enter: \`Bearer <your-token>\`
4. Click **Authorize**
5. Navigate to any endpoint
6. Click **Try it out**
7. Fill in parameters
8. Click **Execute**
9. View response

### API Rate Limits

To prevent abuse, APIs are rate-limited:

- **Login:** 5 requests/minute per IP
- **Cluster Creation:** 10 requests/minute per user
- **Global:** 100 requests/minute per user

**Rate Limit Headers:**
\`\`\`
X-RateLimit-Limit: 100
X-RateLimit-Remaining: 95
X-RateLimit-Reset: 1234567890
\`\`\`

### API Code Generation

Use OpenAPI spec to generate client libraries:

**Download spec:**
\`\`\`bash
curl http://ocpctl.example.com/swagger/doc.json > ocpctl-api.json
\`\`\`

**Generate client (example with openapi-generator):**
\`\`\`bash
# Python client
openapi-generator-cli generate \\
  -i ocpctl-api.json \\
  -g python \\
  -o ./ocpctl-python-client

# JavaScript/TypeScript client
openapi-generator-cli generate \\
  -i ocpctl-api.json \\
  -g typescript-axios \\
  -o ./ocpctl-ts-client
\`\`\`

## Cluster Logs

View deployment logs for troubleshooting:

1. Click on cluster card
2. Navigate to **Logs** tab
3. View real-time deployment logs

**Log Details:**
- **Timestamp** - When log line was written
- **Level** - INFO, WARN, ERROR
- **Message** - Log content
- **Source** - openshift-install, ocpctl, etc.

**Filtering:**
- Search for keywords
- Filter by log level
- Jump to errors

**Use Cases:**
- Troubleshoot failed cluster creation
- Monitor installation progress
- Debug configuration issues

## Advanced Cluster Options

### SSH Access to Nodes

Add SSH public key during cluster creation:

1. In cluster creation form
2. Paste your public key in **SSH Public Key** field
3. Create cluster

**Accessing nodes:**
\`\`\`bash
# Get node IP
oc get nodes -o wide

# SSH to node
ssh core@<node-ip>
\`\`\`

### Custom Tags

Add extra AWS tags for cost tracking:

1. In cluster creation form
2. Navigate to **Extra Tags** section
3. Add key-value pairs (e.g., \`Project: MyApp\`)
4. Create cluster

**Tags appear in:**
- AWS Cost Explorer
- EC2 resource tags
- Cost allocation reports

### Work Hours Schedules

Advanced work hours configuration:

**Custom Schedule:**
- Define specific days (e.g., Mon/Wed/Fri only)
- Set precise times (9:30 AM - 4:45 PM)
- Use your timezone
- Enable off-hours opt-in for manual override

**Grace Periods:**
- 8 hours after manual resume before re-hibernation
- Prevents immediate shutdown after you start cluster

## Best Practices

**API Usage:**
- Store tokens securely (not in code)
- Refresh tokens before expiry
- Handle rate limits gracefully
- Use appropriate timeouts

**Storage Linking:**
- Plan storage strategy before creating clusters
- Document which clusters share storage
- Clean up unused storage groups
- Monitor EFS costs (~$0.30/GB/month)

**Configurations:**
- Test post-deployment configs in dev first
- Keep configuration definitions in git
- Use retries for transient failures
- Monitor configuration status after creation

**Automation:**
- Use API for CI/CD cluster provisioning
- Implement automatic cleanup scripts
- Tag clusters with build/pipeline info
- Monitor costs via AWS Cost Explorer

## Troubleshooting

**Configuration Failed:**
1. Check cluster logs for errors
2. Verify cluster has internet access
3. Retry the configuration
4. Check OperatorHub availability

**Storage Linking Failed:**
1. Verify both clusters in same region
2. Check EFS is enabled on both
3. Ensure clusters are Ready
4. Review error message in response

**API Errors:**
1. Check token is valid (not expired)
2. Verify request format matches schema
3. Check rate limit headers
4. Review Swagger docs for correct parameters

**Orphan Detection False Positive:**
1. Verify resource has ManagedBy tag
2. Check if cluster exists with similar name
3. Mark as Ignored if intentional
4. Report issue if truly a bug
`,
  },
];

export default function UserGuidePage() {
  const [activeGuide, setActiveGuide] = useState(userGuides[0]);

  return (
    <div className="flex h-full">
      {/* Navigation Sidebar */}
      <div className="w-64 border-r bg-card p-4 space-y-2">
        <h2 className="font-semibold text-lg mb-4">User Guide</h2>
        {userGuides.map((guide) => {
          const Icon = guide.icon;
          return (
            <button
              key={guide.id}
              onClick={() => setActiveGuide(guide)}
              className={`w-full flex items-center gap-3 px-3 py-2 rounded-md text-sm transition-colors ${
                activeGuide.id === guide.id
                  ? "bg-primary text-primary-foreground"
                  : "hover:bg-accent"
              }`}
            >
              <Icon className="h-4 w-4" />
              {guide.title}
            </button>
          );
        })}
      </div>

      {/* Content Area */}
      <div className="flex-1 overflow-auto p-8">
        <div className="mx-auto prose prose-sm max-w-4xl dark:prose-invert">
          <ReactMarkdown remarkPlugins={[remarkGfm]}>
            {activeGuide.content}
          </ReactMarkdown>
        </div>
      </div>
    </div>
  );
}
