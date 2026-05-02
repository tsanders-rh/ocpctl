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

OCPCTL is a self-service platform for provisioning and managing ephemeral Kubernetes and OpenShift clusters on AWS and IBM Cloud. It provides:

- **Self-service cluster creation** - Request OpenShift, EKS, or IKS clusters through an easy-to-use web interface
- **Automated lifecycle management** - Clusters are automatically destroyed after their TTL expires
- **Work hours hibernation** - Clusters can automatically hibernate outside business hours to save costs
- **Policy enforcement** - Cluster configurations are validated against organizational policies
- **Audit trail** - All operations are logged for compliance and tracking
- **Post-deployment automation** - Automatically configure operators, scripts, and dashboards

**Supported Platforms:**
- **AWS OpenShift** - OpenShift 4.20+ clusters on AWS
- **AWS EKS** - Managed Kubernetes clusters with Kubernetes Dashboard
- **IBM Cloud IKS** - Managed Kubernetes clusters on IBM Cloud classic infrastructure

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
- **Console URL** (OpenShift) - Access OpenShift web console
- **Dashboard URL** (EKS) - Access Kubernetes Dashboard with provided token
- **Extend TTL** - Postpone automatic destruction
- **Hibernate** - Shut down cluster to save costs
- **Resume** - Start a hibernated cluster
- **Destroy** - Immediately delete the cluster

**Note:** EKS clusters include an automatically configured Kubernetes Dashboard accessible via LoadBalancer with token-based authentication. The dashboard token is securely stored and displayed in cluster outputs.

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

#### aws-virt-windows-minimal
- **Configuration:** 3 control-plane nodes (m6i.2xlarge) + 2 worker nodes (m6i.metal)
- **Cost:** ~$25/hour
- **Max TTL:** 168 hours, Default: 72 hours
- **Post-Deployment:** Automatically configures IRSA for Windows VM image access
- **Best for:** OpenShift Virtualization with Windows VM workloads

#### eks-minimal (AWS EKS)
- **Configuration:** 2-node managed Kubernetes cluster (t3.medium)
- **Cost:** ~$0.15/hour ($110/month with EKS control plane)
- **Max TTL:** 168 hours, Default: 48 hours
- **Post-Deployment:** Automatically installs Kubernetes Dashboard with token authentication
- **Best for:** Kubernetes development and testing without OpenShift

#### eks-standard (AWS EKS)
- **Configuration:** 3-node managed Kubernetes cluster with larger instances
- **Max TTL:** 168 hours
- **Post-Deployment:** Kubernetes Dashboard and monitoring tools
- **Best for:** Standard Kubernetes workloads

#### iks-minimal (IBM Cloud IKS)
- **Configuration:** 2-node managed Kubernetes cluster (b3c.4x16 - 4 vCPU, 16GB RAM)
- **Cost:** ~$0.28/hour ($202/month, free control plane)
- **Max TTL:** 168 hours, Default: 48 hours
- **Best for:** Kubernetes development on IBM Cloud

**Note:** Some profiles include automated post-deployment configuration. See [Post-Deployment Configuration](#post-deployment-configuration) below for details.

### Step 3: Configure Cluster Details

Fill in the required fields:

**Basic Information:**
- **Cluster Name** - Unique identifier (3-63 characters, lowercase, numbers, hyphens)
- **Platform** - Select \`aws\` (OpenShift, EKS) or \`ibmcloud\` (OpenShift, IKS)
- **Version** - OpenShift version (e.g., 4.20.3) or Kubernetes version (e.g., 1.30)
- **Region** - AWS region (e.g., us-east-1) or IBM Cloud region (e.g., us-south)

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
- **Addon Version Selection** - Override default addon versions for post-deployment operators
- **Additional Registry Credentials** - Add custom pull secrets for private container registries

**Cloud Credentials (AWS OpenShift only):**

OpenShift clusters need AWS credentials to manage cloud resources. OCPCTL supports four credentials modes:

**Auto (Recommended for most deployments)**
- Installer automatically selects the best credentials strategy
- Uses temporary EC2 instance credentials during installation
- Works with GA releases (OpenShift 4.18-4.21)
- Simplest option - no manual AWS configuration needed

**When to use:** Most production and development clusters

**Manual (IRSA with OIDC)**
- Creates AWS IAM Roles for Service Accounts (IRSA)
- Sets up OIDC provider and fine-grained IAM roles
- Required for: OpenShift Virtualization with Windows VMs
- Enables workload identity federation

**When to use:**
- OpenShift Virtualization profiles requiring Windows VM support (e.g., aws-virt-windows-minimal)
- When you need fine-grained IAM permissions per operator
- Production workloads requiring least-privilege security

**Mint (Fine-grained credentials)**
- Cloud Credential Operator creates short-lived credentials with specific permissions
- Requires permanent AWS credentials (Access Key ID/Secret) during installation
- Best for testing pre-release versions (OpenShift 4.22+)

**When to use:**
- Testing OpenShift 4.22 pre-release versions
- When you need CCO to manage credentials automatically
- Development/testing environments where you have permanent AWS credentials

**Static (Shared credentials)**
- All cluster components use the same static AWS credentials
- Simpler credential model than Mint
- Requires permanent AWS credentials during installation
- Available for OpenShift 4.22+

**When to use:**
- Testing OpenShift 4.22+ with simpler credential requirements
- Development clusters where credential complexity isn't needed
- When Mint mode is not working as expected

**Important Notes:**
- OpenShift 4.22 pre-release requires Mint or Static mode (Auto will not work)
- Profiles with credentials_mode set will pre-select the appropriate mode
- For most users, leaving it on **Auto** is the best choice

**Addon Version Selection:**

By default, post-deployment operators are installed using the version specified in the cluster profile. However, you can override specific addon versions during cluster creation.

**When to use:**
- Test a newer operator version before updating the profile
- Pin a specific operator version for compatibility
- Install nightly/preview channels for testing unreleased features
- Use different versions for dev vs. production clusters

**How to configure:**

1. In the cluster creation form, expand **Advanced Options**
2. Scroll to **Addon Version Overrides** section
3. Click **Add Override**
4. Configure the override:
   - **Addon Name** - Name of the operator (must match post-deployment config)
   - **Channel** - Operator channel (e.g., stable, fast, candidate, nightly)
   - **Version** (Optional) - Specific CSV version (e.g., v4.14.1)
   - **Source** (Optional) - Catalog source (defaults to redhat-operators)

**Example overrides:**

\`\`\`json
{
  "addon_version_overrides": [
    {
      "addon_name": "openshift-virtualization",
      "channel": "stable-4.14",
      "version": "v4.14.2"
    },
    {
      "addon_name": "openshift-serverless",
      "channel": "stable"
    },
    {
      "addon_name": "kubernetes-dashboard",
      "channel": "alpha",
      "source": "community-operators"
    }
  ]
}
\`\`\`

**Channel Options:**
- \`stable\` - Latest stable release (recommended for production)
- \`fast\` - Newer features, may have minor issues
- \`candidate\` - Pre-release testing
- \`nightly\` - Daily builds (unstable, for development only)
- \`stable-X.Y\` - Pin to specific minor version (e.g., stable-4.14)

**Important Notes:**
- Addon name must exactly match the name in your post-deployment configuration
- Invalid channel names will cause addon installation to fail
- Version is optional - if omitted, latest version in channel is used
- Overrides only apply to operators defined in post-deployment config
- Not all operators support all channels (check OperatorHub for availability)

**Verification:**

After cluster creation, check the **Post-Deployment Execution Order** card to verify:
1. The operator was installed from the correct channel
2. The CSV (ClusterServiceVersion) matches your specified version
3. No errors occurred during installation

**Additional Registry Credentials:**

If your post-deployment configuration pulls images from private container registries, you need to provide pull secrets. OCPCTL supports multiple credential formats.

**When to use:**
- Pull images from private Docker Hub repositories
- Access private Quay.io namespaces
- Use custom internal registries
- Pull Helm charts from authenticated registries
- Access images from AWS ECR, GCP GCR, Azure ACR

**How to configure:**

1. In the cluster creation form, expand **Advanced Options**
2. Scroll to **Additional Registry Credentials** section
3. Click **Add Credential**
4. Select credential type and provide details

**Credential Types:**

**Docker Hub / Generic Registry:**

\`\`\`json
{
  "registry": "docker.io",
  "username": "myusername",
  "password": "mypassword",
  "email": "user@example.com"
}
\`\`\`

**Quay.io with Robot Account:**

\`\`\`json
{
  "registry": "quay.io",
  "username": "myorg+robotaccount",
  "password": "ROBOT_TOKEN_HERE",
  "email": "robot@example.com"
}
\`\`\`

**AWS ECR (Elastic Container Registry):**

\`\`\`json
{
  "registry": "123456789012.dkr.ecr.us-east-1.amazonaws.com",
  "username": "AWS",
  "password": "eyJwYXlsb2FkIjoi...",
  "email": "aws@example.com"
}
\`\`\`

**Note:** For ECR, use \`aws ecr get-login-password\` to generate the password token.

**Google Container Registry (GCR):**

\`\`\`json
{
  "registry": "gcr.io",
  "username": "_json_key",
  "password": "{\"type\":\"service_account\",\"project_id\":\"...\"}",
  "email": "gcr@example.com"
}
\`\`\`

**Azure Container Registry (ACR):**

\`\`\`json
{
  "registry": "myregistry.azurecr.io",
  "username": "myregistry",
  "password": "SERVICE_PRINCIPAL_PASSWORD",
  "email": "acr@example.com"
}
\`\`\`

**Harbor / GitLab Registry:**

\`\`\`json
{
  "registry": "harbor.example.com",
  "username": "admin",
  "password": "Harbor12345",
  "email": "admin@example.com"
}
\`\`\`

**Multiple Registries:**

You can add multiple credential sets for different registries:

\`\`\`json
{
  "additional_registry_credentials": [
    {
      "registry": "docker.io",
      "username": "dockeruser",
      "password": "dockerpass",
      "email": "docker@example.com"
    },
    {
      "registry": "quay.io",
      "username": "quayuser",
      "password": "quaypass",
      "email": "quay@example.com"
    },
    {
      "registry": "harbor.internal.corp",
      "username": "employee",
      "password": "secretpass",
      "email": "employee@corp.com"
    }
  ]
}
\`\`\`

**How it works:**

1. OCPCTL creates a Kubernetes Secret in the \`openshift-config\` namespace (OpenShift) or \`kube-system\` (Kubernetes)
2. The secret is named \`ocpctl-registry-credentials-{index}\`
3. For OpenShift, credentials are linked to the global pull secret
4. For EKS/IKS, credentials are available for Pods to reference

**Accessing from Pods (EKS/IKS):**

\`\`\`yaml
apiVersion: v1
kind: Pod
metadata:
  name: my-app
spec:
  imagePullSecrets:
  - name: ocpctl-registry-credentials-0
  containers:
  - name: app
    image: quay.io/myorg/myapp:latest
\`\`\`

**Security Best Practices:**

- **Use robot accounts** instead of personal credentials
- **Rotate credentials regularly** (every 90 days recommended)
- **Limit scope** - Grant only pull permissions, not push
- **Encrypt at rest** - Credentials are stored as Kubernetes Secrets (base64 encoded)
- **Use short-lived tokens** when possible (e.g., AWS ECR tokens expire in 12 hours)
- **Avoid hardcoding** - Consider using External Secrets Operator for production

**Common Issues:**

**"ImagePullBackOff" errors:**
- Verify registry URL is correct (no http:// or https://)
- Check username/password are valid
- Ensure credentials have pull permissions
- For private namespaces, include namespace in image path

**"unauthorized: authentication required":**
- Credentials may have expired (especially ECR)
- Username format may be wrong (e.g., Quay requires orgname+robotname)
- Password may contain special characters that need escaping

**Secret not found:**
- Check secret was created: \`oc get secrets -n openshift-config\` (OpenShift)
- Verify secret is referenced in global pull secret (OpenShift only)
- For EKS/IKS, ensure imagePullSecrets is set in Pod spec

**Verification:**

After cluster creation, verify credentials were applied:

**OpenShift:**
\`\`\`bash
# Check global pull secret includes your registry
oc get secret/pull-secret -n openshift-config -o jsonpath='{.data.\\.dockerconfigjson}' | base64 -d | jq .

# Verify registry is listed
oc get secret/pull-secret -n openshift-config -o jsonpath='{.data.\\.dockerconfigjson}' | base64 -d | jq '.auths | keys'
\`\`\`

**Kubernetes (EKS/IKS):**
\`\`\`bash
# List secrets in kube-system
kubectl get secrets -n kube-system | grep ocpctl-registry

# View secret contents
kubectl get secret ocpctl-registry-credentials-0 -n kube-system -o jsonpath='{.data.\\.dockerconfigjson}' | base64 -d | jq .
\`\`\`

**Test pull:**
\`\`\`bash
# Create a test pod using your private image
oc run test --image=quay.io/yourorg/yourimage:latest --restart=Never

# Check if it pulled successfully
oc describe pod test | grep -A 10 Events
\`\`\`

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

## Post-Deployment Configuration

### What is Post-Deployment Configuration?

Some cluster profiles include **automated post-deployment configuration** that runs automatically after your cluster is created and reaches **Ready** status. This automation can install operators, apply manifests, run scripts, or deploy Helm charts to prepare your cluster for specific workloads.

**When it runs:**
- Automatically triggered after cluster creation completes
- Runs as a separate \`POST_CONFIGURE\` job
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

### Task Dependencies and Execution Order

**What are task dependencies?**

Dependencies control the order in which post-deployment tasks execute. Use the \`dependsOn\` field to ensure tasks run in the correct sequence.

**When to use dependencies:**
- Operator must be installed before applying custom resources
- Script needs operator to be ready before running
- Manifest requires namespace created by earlier task
- Helm chart depends on CRDs from operator installation

**Example: Operator → Custom Resource dependency**
\`\`\`json
{
  "operators": [
    {
      "name": "openshift-virtualization",
      "namespace": "openshift-cnv",
      "source": "redhat-operators",
      "channel": "stable"
    }
  ],
  "scripts": [
    {
      "name": "setup-windows-irsa",
      "content": "#!/bin/bash\\n...",
      "dependsOn": ["openshift-virtualization"]
    }
  ]
}
\`\`\`

**How it works:**
- \`setup-windows-irsa\` script waits for \`openshift-virtualization\` operator to complete
- Execution order automatically calculated based on dependencies
- Tasks with no dependencies start immediately
- Failed dependencies prevent dependent tasks from starting

**Multiple dependencies:**
\`\`\`json
{
  "name": "configure-monitoring",
  "dependsOn": ["prometheus-operator", "create-namespace", "setup-rbac"]
}
\`\`\`
Task waits for **all** listed dependencies to complete before starting.

### Conditional Execution

**What is conditional execution?**

Use the \`condition\` field to run tasks only when specific criteria are met. This allows the same post-deployment configuration to work across different cluster types or platforms.

**Available Conditions:**
- \`clusterType == 'openshift'\` - Only on OpenShift clusters
- \`clusterType == 'eks'\` - Only on EKS clusters
- \`clusterType == 'iks'\` - Only on IKS clusters
- \`platform == 'aws'\` - Only on AWS
- \`platform == 'ibmcloud'\` - Only on IBM Cloud
- \`region == 'us-east-1'\` - Only in specific region

**Example: Platform-specific scripts**
\`\`\`json
{
  "scripts": [
    {
      "name": "configure-aws-storage",
      "content": "#!/bin/bash\\noc create storageclass gp3...",
      "condition": "platform == 'aws'"
    },
    {
      "name": "configure-ibm-storage",
      "content": "#!/bin/bash\\noc create storageclass ibmc-file...",
      "condition": "platform == 'ibmcloud'"
    }
  ]
}
\`\`\`

**Example: Cluster type specific operators**
\`\`\`json
{
  "operators": [
    {
      "name": "kubernetes-dashboard",
      "namespace": "kubernetes-dashboard",
      "source": "community-operators",
      "channel": "stable",
      "condition": "clusterType == 'eks'"
    }
  ]
}
\`\`\`

**Combining conditions with dependencies:**
\`\`\`json
{
  "name": "setup-windows-vm",
  "dependsOn": ["openshift-virtualization"],
  "condition": "platform == 'aws' && clusterType == 'openshift'"
}
\`\`\`

### Template Variables in Scripts and Manifests

**What are template variables?**

Template variables provide dynamic cluster information to your scripts and manifests using \`{{.VariableName}}\` syntax.

**Available Variables:**
- \`{{.CLUSTER_NAME}}\` - Cluster name
- \`{{.CLUSTER_ID}}\` - Cluster UUID
- \`{{.REGION}}\` - AWS/IBM Cloud region
- \`{{.INFRA_ID}}\` - OpenShift infrastructure ID
- \`{{.PLATFORM}}\` - Platform (aws, ibmcloud)
- \`{{.BASE_DOMAIN}}\` - Cluster base domain
- \`{{.KUBECONFIG}}\` - Path to kubeconfig file
- \`{{.NAMESPACE}}\` - Target namespace (for manifests)

**Script example:**
\`\`\`bash
#!/bin/bash
echo "Configuring cluster: {{.CLUSTER_NAME}}"
echo "Region: {{.REGION}}"
echo "Infrastructure ID: {{.INFRA_ID}}"

# Create IAM role for cluster
aws iam create-role \\
  --role-name "{{.CLUSTER_NAME}}-windows-irsa" \\
  --region {{.REGION}} \\
  --tags Key=ClusterID,Value={{.CLUSTER_ID}}

# Configure kubeconfig
export KUBECONFIG={{.KUBECONFIG}}
oc create namespace {{.CLUSTER_NAME}}-apps
\`\`\`

**Manifest example:**
\`\`\`yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: cluster-metadata
  namespace: {{.NAMESPACE}}
data:
  cluster_name: "{{.CLUSTER_NAME}}"
  cluster_id: "{{.CLUSTER_ID}}"
  region: "{{.REGION}}"
  platform: "{{.PLATFORM}}"
  base_domain: "{{.BASE_DOMAIN}}"
  infrastructure_id: "{{.INFRA_ID}}"
\`\`\`

**Environment variables in scripts:**

In addition to template variables, scripts support custom environment variables:

\`\`\`json
{
  "name": "configure-backup",
  "content": "#!/bin/bash\\naws s3 cp data.tar.gz s3://$BACKUP_BUCKET/{{.CLUSTER_NAME}}/",
  "env": {
    "BACKUP_BUCKET": "ocpctl-backups",
    "BACKUP_REGION": "{{.REGION}}",
    "RETENTION_DAYS": "30"
  }
}
\`\`\`

**Note:** Environment variables also support template variable substitution!

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
\`\`\`
Post-Deployment
Automatically installed after cluster creation:

Script: setup-windows-irsa
  Automated IRSA setup for Windows VM image access from S3
\`\`\`

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
3. Check cluster details page for detailed execution visualization

**Post-Deployment Execution Order Visualization:**

The cluster detail page shows a visual timeline of all post-deployment tasks:

**Task Display:**
- **Order Number** - Sequential execution order (1, 2, 3, etc.)
- **Task Icon & Name** - Visual indicator and descriptive name
- **Type Badge** - Color-coded badge (Script, Manifest, Operator, Helm Chart)
- **Dependencies** - Shows which tasks must complete before this one starts
- **Real-time Status** - Current state with icon:
  - ⏱️ **Pending** - Waiting for dependencies
  - ⚙️ **Installing** - Currently executing (animated spinner)
  - ✅ **Completed** - Successfully finished
  - ❌ **Failed** - Encountered error (click for details)
- **Progress Bar** - Overall completion percentage at bottom

**Viewing Task Details:**

Click on any task card to open a detailed view showing:

**For Scripts:**
- Full script content (syntax highlighted)
- Description and timeout
- Environment variables
- Template variables used

**For Manifests:**
- Complete YAML/JSON content
- Description
- Target namespace

**For Operators:**
- Operator namespace
- Catalog source (e.g., redhat-operators)
- Subscription channel
- Custom Resource configuration (if any)

**For Helm Charts:**
- Chart repository URL
- Chart name and version
- Target namespace
- Values configuration (full YAML)

**Example:** Click on "setup-windows-vm-infrastructure" to see the complete bash script that configures IRSA for Windows image access.

**Note:** The cluster is usable immediately when it reaches **Ready** status. Post-deployment runs in parallel and adds additional capabilities.

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

## Disaster Recovery

**Access:** See operations documentation on GitHub or internal deployment docs

### Backup and Recovery Strategy

OCPCTL uses AWS-managed solutions for disaster recovery:

**RDS Database Backups:**
- **Automated backups:** 30-day retention
- **Point-in-time recovery:** Restore to any moment within retention period
- **RPO:** < 5 minutes (WAL archiving)
- **RTO:** < 30 minutes (automated restore)
- **Deletion protection:** Enabled to prevent accidental database deletion

**S3 Artifact Storage:**
- **Versioning:** Enabled on primary bucket (ocpctl-binaries)
- **Cross-region replication:** us-east-1 → us-west-2
- **Replication lag:** < 15 minutes
- **Recovery:** Instant for deleted files via versioning

### DR Scripts and Procedures

**Setup Disaster Recovery (first time):**
\`\`\`bash
./scripts/setup-disaster-recovery.sh
\`\`\`

**Verify Backups (monthly):**
\`\`\`bash
./scripts/verify-backups.sh
\`\`\`

**Monthly DR Drill:**
Perform disaster recovery drills on the first Monday of each month to ensure recovery procedures work correctly.

### Recovery Scenarios

**Database Corruption or Data Loss:**
- Restore from latest automated backup (< 15 minutes)
- Point-in-time recovery to specific timestamp (< 20 minutes)
- Full documentation in \`docs/operations/DISASTER_RECOVERY.md\`

**Accidental File Deletion from S3:**
- Restore from S3 version history (< 5 minutes)
- Zero data loss due to versioning

**Region Failure:**
- Switch to replica bucket in us-west-2 (< 2 hours)
- Promote read replica if configured (< 2 hours)

**Complete Disaster:**
- Full rebuild from backups (< 4 hours)
- Restore database from latest snapshot
- Sync from replica bucket

### Cost Estimates

Disaster recovery backup storage costs approximately **$6.71/month**:
- RDS automated backups (30 days): ~$1.00
- RDS snapshot storage: ~$0.95
- S3 versioning overhead: ~$0.46
- S3 cross-region replication: ~$2.00
- S3 replica storage: ~$2.30

**Note:** These costs are essential for production deployments and scale with data volume.

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
- Disaster recovery operations

**Access:** Database queries (web UI export planned for future release)

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
Currently requires database queries. Web UI export planned for future release.

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

This guide covers advanced features including API keys, templates, post-deployment configurations, storage linking, and API usage.

## API Keys Management

**What are API Keys?**

API keys provide programmatic access to OCPCTL without requiring username/password authentication. They're ideal for:
- **CI/CD pipelines** - Automate cluster provisioning in your build pipelines
- **Automation scripts** - Create/destroy clusters programmatically
- **Third-party integrations** - Integrate OCPCTL with external tools
- **Service accounts** - Long-lived credentials for services

### Creating an API Key

1. Navigate to your **Profile** page (click your email in sidebar)
2. Scroll to **API Keys** section
3. Click **Create API Key** button
4. Configure the key:
   - **Name** - Descriptive name (e.g., "CI Pipeline", "Terraform Integration")
   - **Scope** - Access level:
     - \`read_only\` - Can list and view clusters, but cannot create/modify/delete
     - \`full_access\` - Can perform all operations including create/destroy clusters
   - **Expiration** (Optional) - Auto-revoke after date

5. Click **Create**
6. **IMPORTANT:** Copy the API key immediately - it will only be shown once!

**Example API Key:**
\`\`\`
ocpctl_ak_1a2b3c4d5e6f7g8h9i0j1k2l3m4n5o6p7q8r9s0t1u2v3w4x5y6z
\`\`\`

### Using API Keys

Include the API key in the \`Authorization\` header:

\`\`\`bash
# List clusters
curl -X GET https://ocpctl.example.com/api/v1/clusters \\
  -H "Authorization: Bearer ocpctl_ak_your_key_here"

# Create cluster
curl -X POST https://ocpctl.example.com/api/v1/clusters \\
  -H "Authorization: Bearer ocpctl_ak_your_key_here" \\
  -H "Content-Type: application/json" \\
  -d '{
    "name": "ci-test-cluster",
    "profile": "aws-sno-test",
    "platform": "aws",
    "version": "4.20.3",
    "region": "us-east-1",
    "base_domain": "mg.dog8code.com"
  }'
\`\`\`

**Python Example:**
\`\`\`python
import requests

API_KEY = "ocpctl_ak_your_key_here"
BASE_URL = "https://ocpctl.example.com/api/v1"

headers = {
    "Authorization": f"Bearer {API_KEY}",
    "Content-Type": "application/json"
}

# List clusters
response = requests.get(f"{BASE_URL}/clusters", headers=headers)
clusters = response.json()

# Create cluster
cluster_config = {
    "name": "python-test-cluster",
    "profile": "aws-sno-test",
    "platform": "aws",
    "version": "4.20.3",
    "region": "us-east-1",
    "base_domain": "mg.dog8code.com"
}
response = requests.post(f"{BASE_URL}/clusters", json=cluster_config, headers=headers)
\`\`\`

### Managing API Keys

**View your keys:**
1. Go to **Profile** page
2. Scroll to **API Keys** section
3. See list of all your keys with:
   - Name and scope
   - Created date
   - Last used date
   - Expiration date (if set)

**Update key name:**
1. Find the key in the list
2. Click **Edit** button
3. Change the name
4. Click **Save**

**Revoke a key:**
1. Find the key in the list
2. Click **Revoke** button
3. Confirm revocation

**Effect:** Key is immediately invalidated and cannot be used for authentication.

**Delete a key:**
1. Find the key in the list
2. Click **Delete** button
3. Confirm deletion

**Effect:** Key is permanently removed from the system (audit trail preserved).

### Security Best Practices

**Storage:**
- Store API keys in environment variables, not in code
- Use secret management systems (AWS Secrets Manager, HashiCorp Vault, etc.)
- Never commit API keys to git repositories

**Rotation:**
- Rotate API keys regularly (every 90 days recommended)
- Create new key before revoking old one to avoid downtime
- Use expiration dates for temporary access

**Scope:**
- Use \`read_only\` scope unless write access is required
- Create separate keys for different purposes (CI, monitoring, etc.)
- Revoke keys immediately when no longer needed

**Monitoring:**
- Check "Last Used" date to identify unused keys
- Review API key list monthly
- Revoke keys not used in 90+ days

## Templates for Post-Deployment Configuration

**What are Templates?**

Templates allow you to save post-deployment configurations as reusable blueprints. Instead of manually configuring operators, scripts, and manifests for every cluster, create a template once and apply it to multiple clusters.

**Use Cases:**
- **Standardized environments** - Same operators/config across dev/test/prod
- **Team templates** - Share common configurations with your team
- **Testing variations** - Quickly test different operator configurations
- **Multi-cluster deployments** - Apply identical config to multiple clusters

### Creating a Template

**From the UI:**
1. Navigate to **Templates** page (in sidebar)
2. Click **Create Template** button
3. Configure the template:
   - **Name** - Descriptive name (e.g., "Standard Monitoring Stack")
   - **Description** - What this template provides
   - **Operators** - List of operators to install
   - **Scripts** - Custom scripts to run
   - **Manifests** - YAML manifests to apply
   - **Helm Charts** - Helm charts to deploy

4. Click **Create Template**

**From existing cluster:**
1. Go to cluster details page
2. Navigate to **Configurations** tab
3. Click **Save as Template** button
4. Enter template name and description
5. Click **Save**

### Template Structure

**Example template with all configuration types:**
\`\`\`json
{
  "name": "monitoring-and-backup",
  "description": "Prometheus monitoring + Velero backup",
  "operators": [
    {
      "name": "prometheus",
      "namespace": "openshift-monitoring",
      "source": "redhat-operators",
      "channel": "stable"
    }
  ],
  "scripts": [
    {
      "name": "configure-monitoring",
      "content": "#!/bin/bash\\noc patch cluster-monitoring-config ...",
      "timeout": "5m"
    }
  ],
  "manifests": [
    {
      "name": "custom-serviceaccount",
      "content": "apiVersion: v1\\nkind: ServiceAccount\\nmetadata:\\n  name: backup-sa"
    }
  ],
  "helmCharts": [
    {
      "name": "velero",
      "repo": "https://vmware-tanzu.github.io/helm-charts",
      "chart": "velero",
      "version": "5.0.0",
      "namespace": "velero",
      "values": {
        "configuration": {
          "provider": "aws"
        }
      }
    }
  ]
}
\`\`\`

### Applying a Template to a Cluster

**During cluster creation:**
1. In cluster creation form
2. Expand **Post-Deployment** section
3. Select **Apply Template** checkbox
4. Choose template from dropdown
5. Create cluster

**After cluster creation:**
1. Go to cluster details page
2. Click **Apply Template** button
3. Select template from dropdown
4. Click **Apply**
5. Wait for configuration to complete (5-15 minutes)

### Managing Templates

**List templates:**
- Navigate to **Templates** page in sidebar
- View all available templates
- Search and filter by name

**Update template:**
1. Find template in list
2. Click **Edit** button
3. Modify operators, scripts, manifests, or helm charts
4. Click **Save**

**Delete template:**
1. Find template in list
2. Click **Delete** button
3. Confirm deletion

**Note:** Deleting a template does not affect clusters that already used it.

### Template Variables

Templates support variable substitution for dynamic values:

**Available Variables:**
- \`{{.CLUSTER_NAME}}\` - Name of the cluster
- \`{{.CLUSTER_ID}}\` - UUID of the cluster
- \`{{.REGION}}\` - AWS/IBM Cloud region
- \`{{.INFRA_ID}}\` - OpenShift infrastructure ID
- \`{{.PLATFORM}}\` - Platform type (aws, ibmcloud)
- \`{{.BASE_DOMAIN}}\` - Cluster base domain
- \`{{.NAMESPACE}}\` - Target namespace (for manifests)

**Example script with variables:**
\`\`\`bash
#!/bin/bash
echo "Configuring cluster: {{.CLUSTER_NAME}}"
echo "Region: {{.REGION}}"
oc create namespace {{.CLUSTER_NAME}}-apps
oc label namespace {{.CLUSTER_NAME}}-apps cluster-id={{.CLUSTER_ID}}
\`\`\`

**Example manifest with variables:**
\`\`\`yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: cluster-info
  namespace: {{.NAMESPACE}}
data:
  cluster_name: "{{.CLUSTER_NAME}}"
  region: "{{.REGION}}"
  infrastructure_id: "{{.INFRA_ID}}"
\`\`\`

### Sharing Templates

**Export template:**
1. Go to **Templates** page
2. Find template to export
3. Click **Export** button
4. Save JSON file

**Import template:**
1. Go to **Templates** page
2. Click **Import Template** button
3. Upload JSON file or paste JSON
4. Click **Import**

**Share with team:**
- Export template to JSON
- Share via git repository
- Import in other OCPCTL installations
- Commit to infrastructure-as-code repo

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

## Deployment Logs

**What are Deployment Logs?**

Deployment logs provide real-time visibility into cluster creation, destruction, and post-deployment configuration processes. These logs are essential for troubleshooting failures, monitoring progress, and understanding what's happening during cluster lifecycle operations.

**Accessing Deployment Logs:**

1. Navigate to cluster details page
2. Click on the **Logs** tab
3. View live-streaming deployment logs

**When Logs Are Available:**

Logs are accessible for clusters in the following states:
- **CREATING** - Installation in progress
- **READY** - Installation completed (logs retained for reference)
- **FAILED** - Installation failed (critical for debugging)
- **DESTROYING** - Cluster deletion in progress

**Log Information:**

Each log entry displays:
- **Timestamp** - Exact time the log was written (in your timezone)
- **Level** - Log severity:
  - \`INFO\` - Normal operational messages
  - \`WARN\` - Warnings that don't block progress
  - \`ERROR\` - Errors that may cause failures
  - \`DEBUG\` - Detailed diagnostic information
- **Message** - The actual log content
- **Source** - What generated the log:
  - \`openshift-install\` - OpenShift installer binary
  - \`eksctl\` - EKS cluster creation tool
  - \`ibmcloud\` - IBM Cloud CLI
  - \`ocpctl-worker\` - OCPCTL orchestration logic
  - \`terraform\` - Infrastructure provisioning
  - \`addon-installer\` - Post-deployment automation

**Features:**

**Real-time Updates:**
- Logs automatically refresh every 2 seconds while jobs are active
- No need to manually reload the page
- Scroll to bottom to follow latest entries

**Search and Filter:**
- **Search Box** - Find specific keywords, error codes, or resource names
- **Level Filter** - Show only ERROR logs to quickly identify failures
- **Auto-scroll** - Toggle to stay at bottom (latest logs) or review earlier entries

**Error Highlighting:**
- ERROR level logs are displayed in red for easy identification
- Failed job steps are clearly marked
- Stack traces and error details are preserved

**Log Retention:**
- Logs are stored for the lifetime of the cluster
- Destroyed clusters retain logs for 30 days (configurable)
- Download logs via API for long-term archival

**Use Cases:**

**Troubleshooting Failed Installations:**

If cluster creation fails, check logs for:
- AWS quota errors (e.g., "VPC limit exceeded")
- DNS resolution failures
- Invalid credentials or permissions
- OpenShift installer errors

Example search queries:
- Search for \`"error"\` to find all error messages
- Search for \`"quota"\` to check AWS service limits
- Search for \`"timeout"\` to identify slow operations

**Monitoring Installation Progress:**

Track installation milestones:
- Bootstrap VM creation
- Control plane initialization
- Worker node joining
- Operator deployments
- Post-deployment addon installation

**Debugging Post-Deployment Issues:**

If addons fail to install:
- Filter by source: \`addon-installer\`
- Search for addon name (e.g., \`"openshift-virtualization"\`)
- Look for timeout errors or missing dependencies
- Check if CRDs were applied successfully

**Performance Analysis:**

Identify slow steps:
- Search for \`"Waiting for"\` to find blocking operations
- Check timestamps to calculate step duration
- Identify network-related delays

**Downloading Logs:**

To download logs for offline analysis or support tickets:

\`\`\`bash
# Using the API
curl -X GET "https://ocpctl.example.com/api/v1/clusters/{cluster-id}/logs" \\
  -H "Authorization: Bearer {your-token}" \\
  -o cluster-logs.txt

# With filtering
curl -X GET "https://ocpctl.example.com/api/v1/clusters/{cluster-id}/logs?level=ERROR" \\
  -H "Authorization: Bearer {your-token}" \\
  -o errors.txt
\`\`\`

**Log Rotation:**

- Logs are automatically rotated when they exceed 10MB
- Up to 5 rotated log files are retained
- Oldest logs are deleted automatically
- Total log retention: ~50MB per cluster

**Best Practices:**

**During Creation:**
- Keep logs tab open to monitor progress
- Watch for warnings that might indicate future issues
- Note any timeouts (may need TTL extension)

**After Failure:**
- Download full logs before destroying the cluster
- Search for first ERROR occurrence (root cause usually appears first)
- Check timestamps to identify when failure occurred
- Share logs with support team if needed

**For Production:**
- Export logs to external logging system (CloudWatch, Splunk, etc.)
- Set up alerting for ERROR-level logs
- Archive logs for compliance requirements

**Common Log Patterns:**

**Successful Installation:**
\`\`\`
INFO: Cluster initialization complete
INFO: Waiting for cluster operators to stabilize
INFO: All cluster operators are ready
INFO: Cluster is ready
\`\`\`

**AWS Quota Error:**
\`\`\`
ERROR: failed to create VPC: VpcLimitExceeded: You have reached the limit for VPCs in this region
\`\`\`

**DNS Resolution Failure:**
\`\`\`
ERROR: Failed to wait for bootstrap complete: API server not reachable
WARN: DNS record api.{cluster-name}.{domain} not resolving
\`\`\`

**Addon Installation Success:**
\`\`\`
INFO: [addon-installer] Installing operator: openshift-virtualization
INFO: [addon-installer] Waiting for CSV to reach Succeeded phase
INFO: [addon-installer] Operator openshift-virtualization is ready
\`\`\`

**Integration with Jobs:**

The logs are generated by background jobs. Each job type produces specific log patterns:

- **CREATE** jobs - Full installation logs from openshift-install, eksctl, or ibmcloud CLI
- **DESTROY** jobs - Cleanup logs showing resource deletion progress
- **POST_CONFIGURE** jobs - Addon installation logs with task execution order
- **HIBERNATE/RESUME** jobs - EC2 instance state change logs

You can correlate logs with job status on the Jobs card to understand which operation is currently running.

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
