"use client";

import { useState } from "react";
import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";
import { Book, Users, Shield, Sparkles, Database, PackageCheck } from "lucide-react";

const userGuides = [
  {
    id: "getting-started",
    title: "Getting Started",
    icon: Book,
    content: `# Getting Started with OCPCTL

Welcome to OCPCTL! This comprehensive guide will help you get started with creating and managing Kubernetes and OpenShift clusters across multiple cloud providers.

## What is OCPCTL?

OCPCTL is a self-service platform for provisioning and managing Kubernetes and OpenShift clusters on AWS, Google Cloud, and IBM Cloud. It provides instant access to pre-provisioned clusters or on-demand cluster creation with full lifecycle management.

**Core Capabilities:**
- **Cluster Pools** - Instant access to pre-provisioned clusters for CI/CD and rapid testing
- **Self-service cluster creation** - Request clusters through an easy-to-use web interface or API
- **Automated lifecycle management** - Clusters are automatically destroyed after their TTL expires
- **Work hours hibernation** - Clusters automatically hibernate outside business hours to save costs
- **Policy enforcement** - Cluster configurations validated against organizational policies
- **Audit trail** - All operations logged for compliance and tracking
- **Post-deployment automation** - Automatically configure operators, scripts, and dashboards
- **CI/CD integration** - REST API and SDKs for automated cluster provisioning

**Supported Platforms & Cluster Types:**
- **AWS OpenShift** - OpenShift 4.18-4.22 clusters (IPI installer)
- **AWS EKS** - Managed Kubernetes clusters with Kubernetes Dashboard
- **GCP GKE** - Google Kubernetes Engine clusters
- **GCP OpenShift** - OpenShift clusters on Google Cloud
- **IBM Cloud IKS** - Managed Kubernetes on IBM Cloud classic infrastructure

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

**Main Navigation:**
- **Clusters** - View and manage all your clusters (active, hibernated, provisioning)
- **Cluster Pools** - Browse and lease pre-provisioned clusters for instant access
- **Profiles** - Browse available cluster profiles and post-deployment configurations
- **API Documentation** - Interactive API reference (Swagger UI)
- **User Guide** - This comprehensive documentation

**Admin Navigation** (Admin role only):
- **Admin Dashboard** - System overview, statistics, and orphaned resources
- **User Management** - Create and manage user accounts
- **Team Management** - Configure team-based access control
- **Cluster Pools** - Create and manage cluster pools (admin view with full controls)

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
- **Download Kubeconfig** - Get credentials to access the cluster via kubectl/oc
- **Console URL** (OpenShift) - Access OpenShift web console
- **Dashboard URL** (EKS/GKE) - Access Kubernetes Dashboard with provided token
- **Extend TTL** - Postpone automatic destruction (within profile limits)
- **Hibernate** - Shut down cluster to save costs (reduces costs by ~90%)
- **Resume** - Start a hibernated cluster (takes 5-10 minutes)
- **Release Cluster** (Pool-leased clusters) - Return cluster to pool for others to use
- **Destroy** - Permanently delete the cluster (irreversible)

**Notes:**
- EKS and GKE clusters include an automatically configured Kubernetes Dashboard accessible via LoadBalancer with token-based authentication
- Dashboard tokens are securely stored and displayed in cluster outputs
- Pool-leased clusters show lease expiration time and auto-release countdown

## What's Next?

**For rapid testing and CI/CD:**
- Click on **Cluster Pools** to get instant access to pre-provisioned clusters

**To create custom clusters:**
- Click on **Cluster Management** in the navigation to learn how to create and manage clusters

**For automation:**
- Check out **Advanced Features** for API keys, templates, and programmatic access
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

Select a cluster profile that matches your needs. Profiles define the cluster configuration, size, and optional post-deployment automation.

**💡 Tip:** For instant access, check **Cluster Pools** instead of creating a new cluster.

#### AWS OpenShift Profiles

**aws-sno-ga** (Recommended for Quick Testing)
- **Configuration:** Single Node OpenShift (SNO) - 1 schedulable control-plane node
- **Instance:** m6i.2xlarge (8 vCPU, 32GB RAM)
- **Cost:** ~$0.38/hour
- **OpenShift Versions:** 4.18-4.21 (default: 4.20)
- **Max TTL:** 72 hours, Default: 24 hours
- **Best for:** Rapid testing, development, learning OpenShift

**aws-minimal-test**
- **Configuration:** 3 control-plane nodes (schedulable), 0 dedicated workers
- **Instance:** m6i.2xlarge
- **Cost:** ~$1.15/hour
- **Max TTL:** 72 hours
- **Best for:** Multi-node HA testing without dedicated workers

**aws-standard**
- **Configuration:** 3 control-plane + 3 worker nodes
- **Instances:** m6i.2xlarge
- **Cost:** ~$2.30/hour
- **Max TTL:** 168 hours (7 days)
- **Best for:** Production-like environments, integration testing

**aws-virtualization**
- **Configuration:** 3 control-plane (m6i.2xlarge) + 3 workers (m6i.metal - bare metal)
- **Cost:** ~$35.50/hour
- **Max TTL:** 168 hours, Default: 72 hours
- **Best for:** OpenShift Virtualization (CNV), nested virtualization workloads

**aws-virt-windows-minimal**
- **Configuration:** 3 control-plane (m6i.2xlarge) + 2 workers (m6i.metal)
- **Cost:** ~$25/hour
- **Max TTL:** 168 hours, Default: 72 hours
- **Post-Deployment:** Auto-configures IRSA for Windows VM image access from S3
- **Best for:** OpenShift Virtualization with Windows VM workloads

**aws-prerelease** (Testing Pre-GA Versions)
- **Configuration:** Configurable (SNO or multi-node)
- **OpenShift Versions:** 4.22+ pre-release versions
- **Credentials:** Requires Mint or Static mode (Auto not supported)
- **Best for:** Testing upcoming OpenShift releases

#### AWS Kubernetes (EKS) Profiles

**eks-minimal**
- **Configuration:** 2-node managed Kubernetes cluster
- **Instances:** t3.medium (2 vCPU, 4GB RAM)
- **Cost:** ~$0.15/hour + $0.10/hour EKS control plane
- **Kubernetes Versions:** 1.28-1.31
- **Max TTL:** 168 hours, Default: 48 hours
- **Post-Deployment:** Kubernetes Dashboard with token auth
- **Best for:** Kubernetes testing without OpenShift overhead

**eks-standard**
- **Configuration:** 3-node managed Kubernetes cluster
- **Instances:** t3.large (2 vCPU, 8GB RAM)
- **Cost:** ~$0.30/hour + $0.10/hour EKS control plane
- **Max TTL:** 168 hours
- **Best for:** Standard Kubernetes development and testing

#### GCP Profiles

**gke-standard** (Google Kubernetes Engine)
- **Configuration:** 3-node managed Kubernetes cluster
- **Machine Type:** n1-standard-2 (2 vCPU, 7.5GB RAM)
- **Cost:** ~$0.32/hour (free GKE control plane)
- **Kubernetes Versions:** 1.28-1.31
- **Max TTL:** 168 hours
- **Best for:** Kubernetes development on Google Cloud

**gcp-openshift-standard** (OpenShift on GCP)
- **Configuration:** 3 control-plane + 3 worker nodes
- **Machine Type:** n2-standard-4 (4 vCPU, 16GB RAM)
- **Cost:** ~$1.50/hour
- **OpenShift Versions:** 4.20-4.21
- **Max TTL:** 168 hours
- **Best for:** OpenShift testing on Google Cloud Platform

#### IBM Cloud Profiles

**iks-minimal** (IBM Cloud Kubernetes Service)
- **Configuration:** 2-node managed Kubernetes cluster
- **Instance:** b3c.4x16 (4 vCPU, 16GB RAM)
- **Cost:** ~$0.28/hour (free IKS control plane)
- **Kubernetes Versions:** 1.28-1.30
- **Max TTL:** 168 hours, Default: 48 hours
- **Best for:** Kubernetes development on IBM Cloud classic infrastructure

**Note:** Profiles with post-deployment automation install operators and configure clusters automatically after creation. See [Post-Deployment Configuration](#post-deployment-configuration) for details.

### Step 3: Configure Cluster Details

Fill in the required fields:

**Basic Information:**
- **Cluster Name** - Unique identifier (3-63 characters, lowercase letters, numbers, hyphens only)
- **Profile** - Select from available profiles (determines platform, size, and configuration)
- **Platform** - Automatically set by profile: \`aws\`, \`gcp\`, or \`ibmcloud\`
- **Version** - OpenShift version (4.18-4.22) or Kubernetes version (1.28-1.31)
  - Versions available depend on selected profile
  - Default version recommended for stability
- **Region** - Geographic location for cluster deployment
  - AWS: us-east-1, us-east-2, us-west-1, us-west-2, eu-west-1, ap-southeast-1, etc.
  - GCP: us-central1, us-east1, us-west1, europe-west1, asia-east1, etc.
  - IBM Cloud: us-south, us-east, eu-gb, eu-de, au-syd, etc.

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
    id: "cluster-pools",
    title: "Cluster Pools",
    icon: Database,
    content: `# Cluster Pools Guide

Get instant access to pre-provisioned OpenShift clusters for testing and development.

## What are Cluster Pools?

Cluster Pools are collections of pre-provisioned, ready-to-use clusters that you can lease instantly instead of waiting 30-60 minutes for provisioning. Perfect for CI/CD pipelines, rapid testing, and development workflows.

**Benefits:**
- ⚡ **Instant access** - Get a cluster in seconds, not minutes
- 🔄 **Auto-release** - Clusters automatically return to pool when lease expires
- 💰 **Cost-efficient** - Shared pool resources reduce overall infrastructure costs
- 🎯 **Self-service** - Lease and release via Web UI or API
- 🤖 **CI/CD ready** - Perfect for automated testing pipelines

**Use Cases:**
- GitHub Actions, Jenkins, or Tekton CI/CD pipelines
- Rapid development iterations and testing
- Demo and presentation environments
- Automated integration testing
- Temporary cluster needs without provisioning delays

---

## Quick Start: Web UI

### Browse Available Pools

1. **Navigate to Cluster Pools**
   - Click **Cluster Pools** in the sidebar
   - Or visit: \`https://ocpctl.mg.dog8code.com/pools\`

2. **View Pool Information**
   - Each pool card shows:
     - **Pool name and description**
     - **Profile** - Cluster configuration (SNO, standard, etc.)
     - **Target size** - Desired number of ready clusters
     - **Max lease duration** - How long you can keep a cluster
     - **Work hours schedule** - When pool is active (if configured)

### Lease a Cluster

1. **Select a Pool**
   - Click **View Pool Details** on any pool card
   - Or navigate directly to \`/pools/{pool-name}\`

2. **Check Real-Time Statistics**
   The pool details page shows live metrics:
   - **Ready Clusters** - Available for immediate lease (green)
   - **Leased Clusters** - Currently in use (blue)
   - **Provisioning Clusters** - Being created to replenish pool (orange)
   - **Total Clusters** - Overall pool size (purple)

3. **Lease a Cluster**
   - Click the **Lease Cluster** button
   - Wait a few seconds for confirmation
   - You'll be automatically redirected to the cluster details page

4. **Access Your Cluster**
   - View cluster API URL and OpenShift console URL
   - Download kubeconfig from the cluster details page
   - Start using the cluster immediately!

### Release a Cluster

When you're finished with a cluster:

1. Navigate to the cluster details page (\`/clusters/{cluster-id}\`)
2. Click the **Release Cluster** button
3. Cluster is immediately returned to the pool and becomes available for others

**Auto-Release**: If you forget to release, the cluster automatically returns to the pool after the lease period expires (typically 2-4 hours).

---

## API Usage

### Authentication

All API requests require authentication. Use either a JWT token or API key:

**JWT Token (Login):**
\`\`\`bash
# Login to get token
curl -X POST https://ocpctl.mg.dog8code.com/api/v1/auth/login \\
  -H "Content-Type: application/json" \\
  -d '{"email": "user@example.com", "password": "your-password"}'

# Extract token from response
export TOKEN="your-jwt-token"
\`\`\`

**API Key (Recommended for automation):**
\`\`\`bash
# Use API key created in your profile
export TOKEN="ocpctl_ak_your_key_here"
\`\`\`

### List Available Pools

\`\`\`bash
curl -H "Authorization: Bearer $TOKEN" \\
  https://ocpctl.mg.dog8code.com/api/v1/pools?enabled_only=true
\`\`\`

**Response:**
\`\`\`json
{
  "pools": [
    {
      "name": "dev-pool",
      "display_name": "Development Pool",
      "description": "Fast SNO clusters for development",
      "profile": "aws-sno-ga",
      "target_size": 3,
      "max_lease_duration_hours": 2,
      "enabled": true
    }
  ]
}
\`\`\`

### Get Pool Statistics

Check pool availability before leasing:

\`\`\`bash
curl -H "Authorization: Bearer $TOKEN" \\
  https://ocpctl.mg.dog8code.com/api/v1/pools/dev-pool/stats
\`\`\`

**Response:**
\`\`\`json
{
  "pool_name": "dev-pool",
  "total_clusters": 5,
  "ready_clusters": 3,
  "leased_clusters": 1,
  "provisioning_clusters": 1
}
\`\`\`

### Lease a Cluster

\`\`\`bash
curl -X POST \\
  -H "Authorization: Bearer $TOKEN" \\
  -H "Content-Type: application/json" \\
  -d '{
    "leased_by": "my-test-job-123",
    "metadata": {
      "purpose": "integration testing",
      "ticket": "JIRA-456",
      "team": "platform-team"
    }
  }' \\
  https://ocpctl.mg.dog8code.com/api/v1/pools/dev-pool/lease
\`\`\`

**Response:**
\`\`\`json
{
  "cluster_id": "abc-123-def-456",
  "cluster_name": "dev-pool-xyz789",
  "leased_by": "my-test-job-123",
  "leased_at": "2026-05-22T10:00:00Z",
  "lease_expires_at": "2026-05-22T12:00:00Z",
  "api_url": "https://api.cluster.example.com:6443",
  "console_url": "https://console-openshift-console.apps.cluster.example.com",
  "kubeconfig_path": "s3://ocpctl-artifacts/clusters/abc-123-def-456/kubeconfig"
}
\`\`\`

### Download and Use Kubeconfig

\`\`\`bash
# Extract kubeconfig path from lease response
KUBECONFIG_PATH="s3://ocpctl-artifacts/clusters/abc-123-def-456/kubeconfig"

# Download using AWS CLI
aws s3 cp $KUBECONFIG_PATH ./kubeconfig
chmod 600 ./kubeconfig

# Use the cluster
export KUBECONFIG=./kubeconfig
kubectl get nodes
oc get clusterversion
oc get pods -A
\`\`\`

### Release a Cluster

Always release clusters when done to free them for others:

\`\`\`bash
curl -X POST \\
  -H "Authorization: Bearer $TOKEN" \\
  https://ocpctl.mg.dog8code.com/api/v1/pools/clusters/abc-123-def-456/release
\`\`\`

**Response:** 204 No Content (success)

---

## CI/CD Integration Examples

### GitHub Actions

Create \`.github/workflows/integration-test.yml\`:

\`\`\`yaml
name: Integration Tests
on: [push, pull_request]

jobs:
  test-with-cluster-pool:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v3

      - name: Lease Cluster from Pool
        id: lease
        run: |
          RESPONSE=$(curl -X POST \\
            -H "Authorization: Bearer \${{ secrets.OCPCTL_TOKEN }}" \\
            -H "Content-Type: application/json" \\
            -d '{
              "leased_by": "github-run-\${{ github.run_id }}",
              "metadata": {
                "repo": "\${{ github.repository }}",
                "workflow": "\${{ github.workflow }}",
                "run_id": "\${{ github.run_id }}",
                "branch": "\${{ github.ref_name }}"
              }
            }' \\
            https://ocpctl.mg.dog8code.com/api/v1/pools/ci-pool/lease)

          echo "Response: $RESPONSE"

          CLUSTER_ID=$(echo $RESPONSE | jq -r '.cluster_id')
          KUBECONFIG_PATH=$(echo $RESPONSE | jq -r '.kubeconfig_path')

          echo "cluster_id=$CLUSTER_ID" >> $GITHUB_OUTPUT
          echo "kubeconfig_path=$KUBECONFIG_PATH" >> $GITHUB_OUTPUT

      - name: Configure AWS Credentials
        uses: aws-actions/configure-aws-credentials@v2
        with:
          aws-access-key-id: \${{ secrets.AWS_ACCESS_KEY_ID }}
          aws-secret-access-key: \${{ secrets.AWS_SECRET_ACCESS_KEY }}
          aws-region: us-east-1

      - name: Download Kubeconfig
        run: |
          aws s3 cp \${{ steps.lease.outputs.kubeconfig_path }} ./kubeconfig
          chmod 600 ./kubeconfig

      - name: Run Integration Tests
        env:
          KUBECONFIG: ./kubeconfig
        run: |
          kubectl get nodes
          kubectl get pods -A
          ./scripts/run-integration-tests.sh

      - name: Release Cluster
        if: always()
        run: |
          curl -X POST \\
            -H "Authorization: Bearer \${{ secrets.OCPCTL_TOKEN }}" \\
            https://ocpctl.mg.dog8code.com/api/v1/pools/clusters/\${{ steps.lease.outputs.cluster_id }}/release
\`\`\`

**Setup Instructions:**
1. Add \`OCPCTL_TOKEN\` to GitHub repository secrets (get from your OCPCTL profile)
2. Add AWS credentials for kubeconfig downloads: \`AWS_ACCESS_KEY_ID\`, \`AWS_SECRET_ACCESS_KEY\`
3. Update \`ci-pool\` to match your pool name
4. Customize test script path

### Bash Script

\`\`\`bash
#!/bin/bash
set -euo pipefail

# Configuration
POOL_NAME="dev-pool"
OCPCTL_URL="https://ocpctl.mg.dog8code.com"
TOKEN="\${OCPCTL_TOKEN}"  # Set via environment variable

# Colors for output
RED='\\033[0;31m'
GREEN='\\033[0;32m'
YELLOW='\\033[1;33m'
NC='\\033[0m' # No Color

echo "\${GREEN}Leasing cluster from pool: $POOL_NAME\${NC}"

# Lease cluster
RESPONSE=$(curl -s -X POST \\
    -H "Authorization: Bearer $TOKEN" \\
    -H "Content-Type: application/json" \\
    -d "{
        \\"leased_by\\": \\"script-$(date +%s)\\",
        \\"metadata\\": {
            \\"script\\": \\"$0\\",
            \\"user\\": \\"$(whoami)\\",
            \\"hostname\\": \\"$(hostname)\\"
        }
    }" \\
    "$OCPCTL_URL/api/v1/pools/$POOL_NAME/lease")

# Extract cluster ID
CLUSTER_ID=$(echo "$RESPONSE" | jq -r '.cluster_id')
KUBECONFIG_PATH=$(echo "$RESPONSE" | jq -r '.kubeconfig_path')

if [ -z "$CLUSTER_ID" ] || [ "$CLUSTER_ID" = "null" ]; then
    echo "\${RED}Error: Failed to lease cluster\${NC}"
    echo "$RESPONSE" | jq .
    exit 1
fi

echo "\${GREEN}Leased cluster: $CLUSTER_ID\${NC}"

# Ensure cleanup on exit
cleanup() {
    echo "\${YELLOW}Releasing cluster $CLUSTER_ID...\${NC}"
    curl -s -X POST \\
        -H "Authorization: Bearer $TOKEN" \\
        "$OCPCTL_URL/api/v1/pools/clusters/$CLUSTER_ID/release"
    echo "\${GREEN}Cluster released\${NC}"
}
trap cleanup EXIT

# Download kubeconfig
echo "Downloading kubeconfig..."
aws s3 cp "$KUBECONFIG_PATH" ./kubeconfig
export KUBECONFIG=./kubeconfig

# Verify cluster access
echo "Verifying cluster access..."
kubectl get nodes

# Run your tests here
echo "\${GREEN}Running tests on cluster...\${NC}"
# ./your-test-script.sh

echo "\${GREEN}Tests complete!\${NC}"
\`\`\`

### Python Client

\`\`\`python
#!/usr/bin/env python3
import requests
import os
import sys
import time
from datetime import datetime

class OcpctlPoolClient:
    def __init__(self, base_url, token):
        self.base_url = base_url
        self.headers = {
            "Authorization": f"Bearer {token}",
            "Content-Type": "application/json"
        }

    def get_pool_stats(self, pool_name):
        """Get real-time pool statistics"""
        url = f"{self.base_url}/api/v1/pools/{pool_name}/stats"
        response = requests.get(url, headers=self.headers)
        response.raise_for_status()
        return response.json()

    def lease_cluster(self, pool_name, leased_by, metadata=None):
        """Lease a cluster from the specified pool"""
        url = f"{self.base_url}/api/v1/pools/{pool_name}/lease"
        payload = {
            "leased_by": leased_by,
            "metadata": metadata or {}
        }

        response = requests.post(url, json=payload, headers=self.headers)
        response.raise_for_status()
        return response.json()

    def release_cluster(self, cluster_id):
        """Release a leased cluster back to the pool"""
        url = f"{self.base_url}/api/v1/pools/clusters/{cluster_id}/release"
        response = requests.post(url, headers=self.headers)
        response.raise_for_status()

# Example usage
if __name__ == "__main__":
    # Initialize client
    client = OcpctlPoolClient(
        base_url="https://ocpctl.mg.dog8code.com",
        token=os.environ.get("OCPCTL_TOKEN")
    )

    pool_name = "dev-pool"

    # Check pool availability
    print(f"Checking pool: {pool_name}")
    stats = client.get_pool_stats(pool_name)
    print(f"  Ready clusters: {stats['ready_clusters']}")
    print(f"  Total clusters: {stats['total_clusters']}")

    if stats['ready_clusters'] == 0:
        print("No clusters available. Exiting.")
        sys.exit(1)

    # Lease cluster
    print(f"\\nLeasing cluster from {pool_name}...")
    lease = client.lease_cluster(
        pool_name=pool_name,
        leased_by=f"python-script-{int(time.time())}",
        metadata={
            "purpose": "integration testing",
            "timestamp": datetime.utcnow().isoformat(),
            "user": os.environ.get("USER", "unknown")
        }
    )

    cluster_id = lease["cluster_id"]
    print(f"✓ Leased cluster: {lease['cluster_name']}")
    print(f"  API URL: {lease['api_url']}")
    print(f"  Console: {lease['console_url']}")
    print(f"  Lease expires: {lease['lease_expires_at']}")

    try:
        # Download kubeconfig
        print(f"\\nDownloading kubeconfig from: {lease['kubeconfig_path']}")
        # Use AWS CLI or boto3 to download
        # os.system(f"aws s3 cp {lease['kubeconfig_path']} ./kubeconfig")

        # Your test logic here
        print("\\nRunning tests...")
        time.sleep(2)  # Simulate work
        print("✓ Tests completed successfully")

    except Exception as e:
        print(f"Error during tests: {e}", file=sys.stderr)
        raise

    finally:
        # Always release
        print(f"\\nReleasing cluster {cluster_id}...")
        client.release_cluster(cluster_id)
        print("✓ Cluster released and returned to pool")
\`\`\`

---

## Best Practices

### 1. Always Release Clusters

Even though auto-release exists, manually release when done:

- Frees resources faster for other users
- Reduces infrastructure costs
- Good team citizenship

**In CI/CD pipelines**, use cleanup handlers:
\`\`\`bash
# Bash: trap ensures release even on failure
trap "release_cluster $CLUSTER_ID" EXIT

# Python: try/finally block
try:
    # Use cluster
finally:
    release_cluster(cluster_id)
\`\`\`

### 2. Add Meaningful Metadata

Help with debugging, tracking, and audit trails:

\`\`\`json
{
  "leased_by": "github-actions-workflow-123",
  "metadata": {
    "repo": "my-org/my-app",
    "workflow": "integration-tests",
    "run_id": "12345",
    "branch": "feature/new-api",
    "commit_sha": "abc123def456",
    "pr_number": "789",
    "purpose": "API integration testing"
  }
}
\`\`\`

### 3. Check Pool Stats First

Before leasing, verify pools have capacity:

\`\`\`bash
curl https://ocpctl.mg.dog8code.com/api/v1/pools/ci-pool/stats

# If ready_clusters = 0:
# - Wait for replenishment (check provisioning_clusters)
# - Use a different pool
# - Contact admin to increase pool size
\`\`\`

### 4. Handle Lease Failures Gracefully

Pools may be empty or temporarily unavailable:

\`\`\`python
import time
import requests

def lease_with_retry(client, pool_name, max_retries=3):
    for attempt in range(max_retries):
        try:
            return client.lease_cluster(pool_name, "my-job")
        except requests.HTTPError as e:
            if e.response.status_code == 503:  # Service Unavailable
                if attempt < max_retries - 1:
                    wait_time = 60 * (attempt + 1)  # Exponential backoff
                    print(f"Pool empty, retrying in {wait_time}s...")
                    time.sleep(wait_time)
                else:
                    raise Exception("Pool unavailable after retries")
            else:
                raise
\`\`\`

### 5. Use Appropriate Pools

Match your workload to the pool's profile:

- **SNO pools** (\`aws-sno-ga\`) - Fast testing, single node, ~$0.38/hr
- **Standard pools** (\`aws-standard\`) - Multi-node testing, production-like
- **Virtualization pools** (\`aws-virtualization\`) - CNV workloads, metal instances

Check the pool's \`profile\` field to understand cluster configuration.

### 6. Monitor Lease Expiration

Track your lease time and release before expiration:

\`\`\`bash
LEASE_EXPIRES=$(echo $LEASE_RESPONSE | jq -r '.lease_expires_at')
echo "⚠️  Cluster will auto-release at: $LEASE_EXPIRES"

# Convert to timestamp and calculate remaining time
EXPIRES_TS=$(date -d "$LEASE_EXPIRES" +%s)
NOW_TS=$(date +%s)
REMAINING=$((EXPIRES_TS - NOW_TS))
echo "Time remaining: $((REMAINING / 60)) minutes"
\`\`\`

---

## Troubleshooting

### Pool Shows No Available Clusters

**Symptom:** \`ready_clusters: 0\` in stats API

**Possible Causes & Solutions:**

1. **Replenishment in progress**
   - Check \`provisioning_clusters\` count
   - Wait 30-45 minutes for new clusters to be ready
   - Pool manager automatically maintains target size

2. **Work hours enforcement**
   - Pool may be scaled down outside business hours
   - Check pool's \`scheduled_mode\` setting
   - Wait until next work hours window or contact admin

3. **Pool size too small**
   - All clusters are currently leased
   - Contact admin to increase \`target_size\`

### Lease Request Fails with 503 Error

**Error Message:** \`{"error": "No clusters available in pool"}\`

**Solutions:**
- **Retry with delay**: Pool may be replenishing
- **Check stats endpoint**: See provisioning status
- **Use alternative pool**: Switch to backup pool
- **Contact admin**: Request pool size increase

**Example retry logic:**
\`\`\`bash
for i in {1..3}; do
  if CLUSTER_ID=$(lease_cluster); then
    break
  fi
  echo "Retry $i/3 in 60 seconds..."
  sleep 60
done
\`\`\`

### Cluster Not Releasing

**Symptom:** Cluster stuck in LEASED state

**Diagnostic Steps:**

1. **Check cluster details:**
   \`\`\`bash
   curl -H "Authorization: Bearer $TOKEN" \\
     https://ocpctl.mg.dog8code.com/api/v1/clusters/{cluster-id}
   \`\`\`

2. **Verify lease expiration:**
   - If \`lease_expires_at\` is in the past, background job will auto-release within 60 seconds
   - Pool manager runs every minute

3. **Check pool manager status** (admin only):
   - Review pool manager logs
   - Verify background scheduler is running

**Manual Fix (Admin Only):**
Contact administrator to manually release or investigate pool manager issues.

### Cannot Access Leased Cluster

**Symptom:** \`kubectl\` or \`oc\` commands fail with connection errors

**Diagnostic Checklist:**

**1. Verify kubeconfig download:**
\`\`\`bash
# Check if kubeconfig exists in S3
aws s3 ls s3://ocpctl-artifacts/clusters/{cluster-id}/kubeconfig

# Verify file was downloaded
ls -lh ./kubeconfig

# Check file permissions
chmod 600 ./kubeconfig
\`\`\`

**2. Test API connectivity:**
\`\`\`bash
# Get API URL from lease response
API_URL="https://api.cluster.example.com:6443"

# Test health endpoint
curl -k $API_URL/healthz

# Expected: "ok"
\`\`\`

**3. Verify cluster is READY:**
\`\`\`bash
curl -H "Authorization: Bearer $TOKEN" \\
  https://ocpctl.mg.dog8code.com/api/v1/clusters/{cluster-id}

# status should be "READY", not "CREATING" or "PROVISIONING"
\`\`\`

**4. Check DNS resolution:**
\`\`\`bash
# Extract API hostname from URL
API_HOST=$(echo $API_URL | sed 's|https://||' | sed 's|:6443||')

# Test DNS
nslookup $API_HOST
dig $API_HOST

# Verify it resolves to valid IP
\`\`\`

---

## Frequently Asked Questions

### How long can I lease a cluster?

Each pool has a \`max_lease_duration_hours\` setting, typically 2-4 hours. Your lease will automatically expire after this period. You can release manually at any time before expiration.

**Future enhancement:** Lease extension API is planned for a future release.

### Can I extend an active lease?

Not currently supported. If you need more time:
1. Release the current cluster
2. Immediately lease a new one from the pool
3. Migrate any necessary data/state

**Workaround:** Create your cluster with a longer TTL instead of using pools for extended testing.

### What happens to my workloads when a cluster is released?

**All workloads are permanently deleted.** Clusters are returned to a clean state before being added back to the pool.

**Important:** Extract any important data, logs, or artifacts **before** releasing the cluster.

### Can I lease multiple clusters simultaneously?

**Yes!** There's no limit on concurrent leases per user. You can:
- Lease multiple clusters from the same pool
- Lease clusters from different pools
- Mix pool-leased clusters with regular created clusters

Each lease is tracked independently with its own expiration.

### How do I know which pool to use?

Check the pool's \`profile\` field to understand cluster configuration:

- **\`aws-sno-ga\`** - Single Node OpenShift, fast and cheap (~$0.38/hr)
  - Best for: Quick testing, development, learning

- **\`aws-standard\`** - 3 control plane + 3 worker nodes
  - Best for: Production-like testing, HA scenarios

- **\`aws-virtualization\`** - Metal worker nodes for CNV
  - Best for: OpenShift Virtualization, Windows VMs, nested virtualization

- **\`eks-minimal\`** - 2-node Kubernetes cluster
  - Best for: Kubernetes (non-OpenShift) testing

Browse pool details in the UI to see full configuration.

### Are pools available 24/7?

**Depends on pool configuration.** Some pools use \`scheduled_mode\` for cost optimization:

- **Always-on pools:** Available 24/7
- **Scheduled pools:** Only active during business hours (e.g., Mon-Fri 8am-6pm EST)

Check pool details to see:
- \`scheduled_mode\`: true/false
- \`schedule_timezone\`: Timezone (e.g., America/New_York)
- \`schedule_start_hour\` / \`schedule_end_hour\`: Active hours
- \`schedule_days_of_week\`: Active days (0=Sunday, 1=Monday, etc.)

### How much does using a pool cost?

**Pools are shared infrastructure**, making them cost-effective:

**Example:** A pool with 3 ready SNO clusters costs ~$1.14/hr but serves unlimited users:
- Individual SNO cluster: $0.38/hr × 30 min wait = $0.19 wasted
- Pool SNO cluster: $0.00 wait (instant), shared cost across team

**Cost comparison:**
- **Traditional:** Create cluster ($0.38/hr) → wait 45 min ($0.29 wasted) → test 15 min → destroy
- **Pool:** Lease instantly → test 15 min → release → reused by others

**Team savings:** If 10 developers each create 3 clusters/day:
- Without pools: 30 clusters × $0.29 wait = **$8.70/day wasted**
- With pools: 3 shared clusters × $0.38/hr × 10 hrs = **$11.40/day total**

### What if I lose my lease information?

If you lose the lease response with \`cluster_id\`:

1. **Check your clusters page:**
   \`\`\`
   https://ocpctl.mg.dog8code.com/clusters
   \`\`\`
   Pool-leased clusters show up with \`pool_state: LEASED\`

2. **API query:**
   \`\`\`bash
   curl -H "Authorization: Bearer $TOKEN" \\
     https://ocpctl.mg.dog8code.com/api/v1/clusters
   # Filter for clusters with your leased_by identifier
   \`\`\`

3. **Let it auto-release:**
   If you can't find it, the cluster will auto-release when the lease expires

---

## Additional Resources

- **Full Feature Documentation**: See \`docs/features/CLUSTER_POOLS.md\` on GitHub
- **API Reference**: https://ocpctl.mg.dog8code.com/swagger/index.html (search for "Pools")
- **Cluster Management Guide**: See "Cluster Management" section in this guide
- **Architecture Details**: \`docs/architecture/architecture.md\`

**Need Help?**
- Contact your OCPCTL administrator
- Check API documentation for endpoint details
- Review cluster logs if lease fails
`,
  },
  {
    id: "addons",
    title: "Addons",
    icon: PackageCheck,
    content: `# Addons

## Overview

### What are Addons?

Addons are pre-packaged post-deployment configurations that automatically install and configure software on your clusters after creation. They can include:

- **Operators** - OpenShift Operators from catalogs (e.g., OpenShift Virtualization, Pipelines)
- **Scripts** - Bash scripts for cluster configuration and setup tasks
- **Manifests** - Kubernetes YAML resources (pods, services, config maps, etc.)
- **Helm Charts** - Packaged applications from Helm repositories

**Use Cases:**
- Standardize cluster configurations across teams
- Install common development tools automatically
- Deploy monitoring and logging stacks
- Configure backup and disaster recovery solutions
- Set up CI/CD tooling (Tekton, ArgoCD, etc.)

### System Addons vs User Addons

**System Addons** (🔵 Blue Badge):
- Pre-configured by OCPCTL administrators
- Available to all users
- Immutable (cannot be edited or deleted)
- Production-tested and supported
- Examples: CNV (OpenShift Virtualization), MTA, MTC, OADP

**User Addons**:
- Created by users for custom requirements
- Two states: Draft or Published
- Full control over configuration
- Can be shared with team or kept private

**Published User Addons** (🟢 Green Badge):
- Visible to all users
- Immutable (cannot be edited)
- Production-ready
- To make changes, clone to create a new draft version

**Draft User Addons** (🟠 Orange Badge):
- Only visible to the author
- Can be edited and deleted
- Can be tested in cluster creation before publishing
- Perfect for development and testing

### Addon Lifecycle

\`\`\`
1. Create → 🟠 Draft
   ↓ (test, edit, refine)
2. Publish → 🟢 Published
   ↓ (immutable, visible to all)
3. Clone → 🟠 New Draft Version
   ↓ (make changes)
4. Publish → 🟢 Published (new version)
\`\`\`

**Key Points:**
- Publishing is **one-way** - you cannot unpublish
- To update a published addon, you must **clone** it to create a new version
- Draft addons can be used by the author for testing before publishing
- Version history is maintained via \`parent_version_id\`

### Available System Addons

#### CNV (OpenShift Virtualization)

Enables running virtual machines alongside containers on OpenShift.

**Available Versions:**
- **4.22 stable-stage** - Production-ready for OpenShift 4.22
- **4.22 stable-stage + Windows VM** - Includes Windows 10 VM support with OADP
- **4.99 nightly** - Latest development builds
- **4.99 nightly + Windows VM** - Development builds with Windows support

**Requirements:**
- OpenShift 4.18+
- Bare metal or nested virtualization support
- Windows versions require Manual (IRSA) credentials mode

**What Gets Installed:**
- HyperConverged Operator
- CNV operator and CRDs
- Virtualization catalog source
- Sample VMs (optional)

#### MTA (Migration Toolkit for Applications)

Application analysis and migration toolkit for modernizing Java applications.

**Features:**
- Analyze applications for cloud-native migration
- Identify code changes needed for containerization
- Generate migration reports and recommendations
- Support for multiple frameworks (Spring, EJB, Hibernate, etc.)

**Requirements:**
- OpenShift 4.12+
- 8GB+ memory recommended

#### MTC (Migration Toolkit for Containers)

Migrate applications between Kubernetes/OpenShift clusters with state preservation.

**Features:**
- Migrate workloads between clusters
- Preserve persistent volume data
- Support for direct and indirect migrations
- Rollback capabilities

**Requirements:**
- OpenShift 4.12+
- Network connectivity between source and target clusters

#### OADP (OpenShift API for Data Protection)

Backup and restore solution for OpenShift applications and persistent data.

**Features:**
- Backup namespaces, resources, and volumes
- Scheduled backups
- Restore to same or different cluster
- S3-compatible storage backends

**Built on:**
- Velero open-source project
- Restic for volume backups

**Requirements:**
- OpenShift 4.10+
- S3-compatible object storage

## Using Addons in Cluster Creation

### Accessing the Addon Browser

When creating a cluster:

1. Fill in basic cluster details (name, platform, version, region, etc.)
2. Scroll down to the **Addons** section
3. The Addon Browser displays all available addons

### Search and Filter

**Search Bar:**
- Type to search by addon name or description
- Real-time filtering as you type
- Case-insensitive matching

**Category Filter:**
- Dropdown to filter by functional category
- Categories: Virtualization, Migration, Backup, Development, Monitoring, etc.
- Select "All Categories" to see everything

**Platform Filter (Automatic):**
- Addons automatically filtered based on your selected platform and profile
- Only shows compatible addons
- Platform field on addon card shows: aws, gcp, ibmcloud

### Understanding Addon Cards

Each addon card displays:

**Header:**
- **Checkbox** - Select/deselect addon
- **Name** - Display name with status badge
- **Badge** - Addon type (System/Published/Draft)

**Details:**
- **Description** - What the addon does
- **Category** - Functional grouping
- **Platform Support** - Compatible platforms

**When Selected:**
- **Version Selector** - Choose from available versions
- Recommended version marked with "(recommended)"

### Selecting and Configuring Addons

**To select an addon:**

1. **Click the checkbox** next to the addon name
2. **Version selector appears** below the addon
3. **Choose version:**
   - Default/recommended version pre-selected
   - Click dropdown to see other options
   - Each version shows: channel name, display name

**Version Examples:**
\`\`\`
stable → "CNV 4.22 (recommended)"
stable-stage → "CNV 4.22 Stable Stage"
nightly → "CNV 4.99 Nightly"
\`\`\`

**To deselect:**
- Uncheck the checkbox
- Version selector disappears
- Addon removed from cluster configuration

### Addon Conflicts

Some addons cannot be installed together due to conflicts.

**How Conflicts Work:**

**Scenario:** CNV 4.22 conflicts with CNV 4.99 (can't install two versions)

1. **Select CNV 4.22** - Works normally
2. **Try to select CNV 4.99:**
   - CNV 4.99 checkbox is **disabled** (grayed out)
   - Shows warning badge: 🟡 **Conflicts with CNV 4.22**
   - Tooltip explains: "This addon conflicts with CNV 4.22. Deselect CNV 4.22 first."

**Automatic Conflict Resolution:**

Some addons use automatic resolution:
1. Select Addon A
2. Select Addon B (conflicts with A)
3. Addon A is **automatically deselected**
4. Only Addon B remains selected

**Viewing Conflicts:**
- Conflict information shown on addon card
- Warning badge appears when conflict detected
- Tooltip provides details

### Testing Draft Addons

**For Addon Authors:**

If you created a custom addon but haven't published it:

1. Your draft addon appears in the Addon Browser
2. Marked with 🟠 **Draft** badge
3. **Only YOU can see it** - other users don't see drafts
4. You can select and test it in cluster creation

**Testing Workflow:**

1. Create draft addon in **Addons** page
2. Go to **Clusters** → **Create Cluster**
3. Find your draft addon (shows 🟠 Draft badge)
4. Select it and create a test cluster
5. Verify it installs correctly
6. Review job logs for any errors
7. Edit addon if needed
8. Repeat until satisfied
9. **Publish** when ready for production use

**Best Practice:**
- Always test draft addons before publishing
- Use a dedicated test cluster
- Check job logs for warnings/errors
- Verify all components installed successfully

### Addon Execution Order

After cluster creation completes, selected addons are executed automatically:

**Dependency Resolution:**
- Items with \`dependsOn\` wait for their dependencies
- Independent items run in parallel
- Topological sort ensures correct ordering

**Execution Flow:**
\`\`\`
1. Cluster reaches READY status
2. POST_CONFIGURE job created
3. Addon components merged with profile post-config
4. Dependencies analyzed
5. Execution order calculated
6. Items executed in order
7. Job succeeds or fails
\`\`\`

**Viewing Execution:**
- Go to cluster details page
- Expand **Post-Deployment Execution Order** card
- See ordered list of operators, scripts, manifests
- Green checkmarks for completed items
- Red X for failed items

**Debugging Failed Addons:**
- Check job logs (filter by POST_CONFIGURE)
- Look for error messages
- Verify dependencies completed successfully
- Check resource requirements (memory, CPU)
- Ensure platform compatibility

### Selection Summary

At the bottom of the Addon Browser:

**Summary Card:**
\`\`\`
✓ 2 add-ons selected

Selected add-ons will be installed after cluster creation
\`\`\`

Shows:
- Count of selected addons
- Reminder that installation happens post-creation
- Updates in real-time as you select/deselect

## Creating Custom Addons

### When to Create a Custom Addon

Create custom addons when you need to:

- **Standardize configurations** - Install the same tools on multiple clusters
- **Share with team** - Distribute configurations to team members
- **Version control** - Track changes to cluster configurations over time
- **Package deployments** - Bundle multi-step deployments into one addon
- **Automate setup** - Eliminate manual post-install steps

**Examples:**
- Development tooling (debugging pods, CLI tools, dashboards)
- Monitoring stack (Prometheus, Grafana, alerts)
- CI/CD pipelines (Tekton, ArgoCD, Jenkins)
- Security tooling (Falco, Trivy, admission controllers)
- Custom operators for internal services

### Creating Your First Addon

**Step 1: Navigate to Addons Page**

1. Click **Addons** in the sidebar
2. Click **Create Addon** button (top right)

**Step 2: Fill in Basic Information**

**Addon ID** (required):
- Unique identifier across the system
- Lowercase letters, numbers, hyphens only
- Cannot be changed after creation
- Examples: \`dev-tools\`, \`monitoring-stack\`, \`my-app-setup\`

**Name** (required):
- Display name shown to users
- Can include spaces and capitals
- Examples: "Development Tools", "Monitoring Stack"

**Description** (required):
- Brief explanation of what the addon does
- Shown in addon browser cards
- Keep it concise (1-2 sentences)
- Example: "Installs debugging pods and CLI tools for development"

**Category** (required):
- Functional grouping for filtering
- Options: Development, Monitoring, Backup, Migration, Virtualization, Networking, Security, Storage, Custom

**Version** (required):
- Version channel identifier
- Examples: \`stable\`, \`v1.0\`, \`beta\`, \`nightly\`
- Cannot be changed after creation
- Use semantic versioning recommended

**Display Name** (required):
- User-friendly version name
- Examples: "Dev Tools v1.0", "Stable Release", "Beta 2024-05"

**Step 3: Platform Support**

Select which platforms this addon supports:

- ☐ **aws** - Works on AWS (OpenShift, EKS)
- ☐ **gcp** - Works on GCP (OpenShift, GKE)
- ☐ **ibmcloud** - Works on IBM Cloud (IKS)

**Tips:**
- Select all that apply
- Test on each platform before publishing
- Platform-specific requirements go in metadata notes

**Step 4: Configure Components**

Choose which components to include (at least one required):

#### Operators

Install OpenShift Operators from catalogs.

**When to use:**
- Installing operators from OperatorHub
- Need operator CRDs and controllers
- Using OpenShift operator lifecycle management

**Configuration:**
\`\`\`json
{
  "operators": [
    {
      "name": "openshift-pipelines-operator-rh",
      "namespace": "openshift-operators",
      "source": "redhat-operators",
      "channel": "latest",
      "depends_on": []
    }
  ]
}
\`\`\`

**Fields:**
- \`name\` - Package name from catalog
- \`namespace\` - Target namespace (usually openshift-operators)
- \`source\` - Catalog source (redhat-operators, community-operators, certified-operators)
- \`channel\` - Update channel (stable, fast, latest, etc.)
- \`depends_on\` - Array of component names to wait for (optional)

**Finding Operator Names:**
1. Go to OperatorHub in OpenShift console
2. Search for operator
3. Click on it to see package name
4. Use exact package name in \`name\` field

#### Scripts

Run bash scripts for configuration tasks.

**When to use:**
- Cluster configuration (network policies, quotas, etc.)
- Namespace creation
- Resource validation
- Custom installation steps
- Waiting for resources to be ready

**Configuration:**
\`\`\`json
{
  "scripts": [
    {
      "name": "create-namespace",
      "description": "Create dev-tools namespace",
      "content": "#!/bin/bash\\nset -euo pipefail\\n\\nkubectl create namespace dev-tools || echo 'Namespace exists'",
      "timeout": "60s",
      "dependsOn": []
    }
  ]
}
\`\`\`

**Fields:**
- \`name\` - Script identifier (unique within addon)
- \`description\` - What the script does
- \`content\` - Inline bash script (use \`\\n\` for newlines)
- \`url\` - OR fetch script from URL
- \`path\` - OR path to script file
- \`timeout\` - Max execution time (default: 300s)
- \`dependsOn\` - Wait for these components first

**Script Best Practices:**
\`\`\`bash
#!/bin/bash
set -euo pipefail  # Exit on error, undefined vars, pipe failures

# Your script here
kubectl create namespace demo

# Check results
kubectl get namespace demo
\`\`\`

**Available Commands:**
- \`kubectl\` - Kubernetes CLI
- \`oc\` - OpenShift CLI (on OpenShift clusters)
- \`curl\`, \`wget\` - HTTP clients
- \`jq\`, \`yq\` - JSON/YAML processors
- Standard bash utilities

#### Manifests

Apply Kubernetes YAML resources.

**When to use:**
- Deploying pods, services, deployments
- Creating config maps, secrets
- Applying CRs after operator installation
- Setting up RBAC (roles, bindings)

**Configuration:**
\`\`\`json
{
  "manifests": [
    {
      "name": "debug-pod",
      "description": "Deploy debug pod with network tools",
      "content": "apiVersion: v1\\nkind: Pod\\nmetadata:\\n  name: debug-pod\\n  namespace: dev-tools\\nspec:\\n  containers:\\n  - name: debug\\n    image: nicolaka/netshoot:latest\\n    command: ['sleep', 'infinity']",
      "namespace": "dev-tools",
      "dependsOn": ["create-namespace"]
    }
  ]
}
\`\`\`

**Fields:**
- \`name\` - Manifest identifier
- \`description\` - What this creates
- \`content\` - Inline YAML (use \`\\n\` for newlines)
- \`url\` - OR fetch YAML from URL
- \`namespace\` - Override namespace (optional)
- \`dependsOn\` - Wait for these components

**Multi-Resource Manifests:**
Use \`---\` to separate multiple resources:
\`\`\`yaml
apiVersion: v1
kind: Namespace
metadata:
  name: demo
---
apiVersion: v1
kind: Pod
metadata:
  name: demo
  namespace: demo
spec:
  containers:
  - name: app
    image: nginx:alpine
\`\`\`

#### Helm Charts

Install Helm charts from repositories.

**When to use:**
- Deploying packaged applications
- Using community Helm charts
- Installing complex multi-resource apps
- Leveraging Helm's upgrade capabilities

**Configuration:**
\`\`\`json
{
  "helm_charts": [
    {
      "name": "grafana",
      "repo": "https://grafana.github.io/helm-charts",
      "chart": "grafana",
      "version": "6.50.0",
      "namespace": "monitoring",
      "values": {
        "persistence.enabled": false,
        "adminPassword": "admin123"
      },
      "depends_on": ["create-monitoring-ns"]
    }
  ]
}
\`\`\`

**Fields:**
- \`name\` - Helm release name
- \`repo\` - Helm repository URL
- \`chart\` - Chart name in repository
- \`version\` - Chart version (optional, uses latest if omitted)
- \`namespace\` - Target namespace
- \`values\` - Chart values as JSON object (optional)
- \`depends_on\` - Wait for these components

**Finding Helm Charts:**
- Artifact Hub: https://artifacthub.io
- Helm Hub: https://hub.helm.sh
- Chart repository README files

**Step 5: Metadata (Optional but Recommended)**

**Conflicts With:**
- List addon IDs that conflict with this one
- Example: \`["other-monitoring-stack", "legacy-tools"]\`
- Prevents users from selecting conflicting addons

**Required Capabilities:**
- Platform features needed for this addon
- Examples: \`["efs-storage"]\`, \`["bare-metal"]\`, \`["gpu-nodes"]\`
- Shown as warnings to users

**Requires Bare Metal:**
- Check if addon needs bare metal nodes
- Common for: virtualization, high-performance workloads

**Notes:**
- Usage instructions for users
- How to access deployed applications
- Configuration options
- Example:
  \`\`\`
  Access Grafana at: https://grafana-route-monitoring.apps.<cluster-domain>
  Default credentials: admin / admin123
  Change password after first login
  \`\`\`

**Warnings:**
- Important caveats users should know
- Resource requirements
- Cost implications
- Example:
  \`\`\`
  - Requires 16GB+ worker nodes
  - Uses 50GB persistent storage
  - Increases cluster cost by ~$50/month
  \`\`\`

**Step 6: Save Draft**

1. Click **Save Draft** button
2. Addon saved with 🟠 Draft status
3. Only you can see and use it
4. Redirected to addon details page

### Complete Example: Development Tools Addon

\`\`\`json
{
  "addon_id": "dev-tools",
  "name": "Development Tools",
  "description": "Debugging pod with network tools and sample ConfigMap",
  "category": "development",
  "version": "stable",
  "display_name": "Dev Tools v1.0",
  "supported_platforms": ["aws", "gcp"],
  "config": {
    "scripts": [
      {
        "name": "verify-namespace",
        "description": "Ensure dev-tools namespace exists",
        "content": "#!/bin/bash\\nset -euo pipefail\\n\\nif kubectl get namespace dev-tools; then\\n  echo 'Namespace exists'\\nelse\\n  kubectl create namespace dev-tools\\nfi",
        "timeout": "60s"
      },
      {
        "name": "wait-for-pod",
        "description": "Wait for debug pod to be ready",
        "content": "#!/bin/bash\\nkubectl wait --for=condition=ready pod/debug-pod -n dev-tools --timeout=120s",
        "timeout": "180s",
        "dependsOn": ["debug-pod-manifest"]
      }
    ],
    "manifests": [
      {
        "name": "debug-pod-manifest",
        "description": "Deploy debug pod with network tools",
        "content": "apiVersion: v1\\nkind: Pod\\nmetadata:\\n  name: debug-pod\\n  namespace: dev-tools\\nspec:\\n  containers:\\n  - name: debug\\n    image: nicolaka/netshoot:latest\\n    command: ['sleep', 'infinity']",
        "namespace": "dev-tools",
        "dependsOn": ["verify-namespace"]
      },
      {
        "name": "configmap-example",
        "description": "Sample ConfigMap with debug commands",
        "content": "apiVersion: v1\\nkind: ConfigMap\\nmetadata:\\n  name: debug-commands\\n  namespace: dev-tools\\ndata:\\n  test-dns.sh: |\\n    #!/bin/bash\\n    dig kubernetes.default.svc.cluster.local",
        "namespace": "dev-tools",
        "dependsOn": ["verify-namespace"]
      }
    ]
  },
  "metadata": {
    "notes": [
      "Access debug pod: kubectl exec -it debug-pod -n dev-tools -- bash",
      "Available tools: curl, wget, dig, ping, traceroute, netstat"
    ],
    "warnings": [
      "Debug pod runs with default service account",
      "For production, configure RBAC appropriately"
    ]
  }
}
\`\`\`

**Execution Order for This Addon:**
\`\`\`
1. verify-namespace (no dependencies)
2. debug-pod-manifest (depends on verify-namespace)
3. configmap-example (depends on verify-namespace)
4. wait-for-pod (depends on debug-pod-manifest)
\`\`\`

### Dependency Management

Use \`dependsOn\` / \`depends_on\` to control execution order.

**Rules:**
- Components with no dependencies run first
- Components wait for all dependencies to complete
- Independent components run in parallel
- Circular dependencies cause validation errors

**Common Patterns:**

**Pattern 1: Namespace First**
\`\`\`json
{
  "scripts": [
    {"name": "create-ns", "content": "kubectl create ns demo"}
  ],
  "manifests": [
    {
      "name": "pod",
      "namespace": "demo",
      "dependsOn": ["create-ns"]
    }
  ]
}
\`\`\`

**Pattern 2: Operator → Custom Resource**
\`\`\`json
{
  "operators": [
    {"name": "prometheus-operator", "namespace": "operators"}
  ],
  "manifests": [
    {
      "name": "prometheus-instance",
      "content": "apiVersion: monitoring.coreos.com/v1...",
      "dependsOn": []  // Operator installation includes wait
    }
  ]
}
\`\`\`

**Pattern 3: Sequential Scripts**
\`\`\`json
{
  "scripts": [
    {"name": "step1", "content": "..."},
    {"name": "step2", "content": "...", "dependsOn": ["step1"]},
    {"name": "step3", "content": "...", "dependsOn": ["step2"]}
  ]
}
\`\`\`

**Pattern 4: Parallel with Final Validation**
\`\`\`json
{
  "manifests": [
    {"name": "pod-a"},
    {"name": "pod-b"},
    {"name": "service"}
  ],
  "scripts": [
    {
      "name": "verify-all",
      "dependsOn": ["pod-a", "pod-b", "service"]
    }
  ]
}
\`\`\`

## Managing Your Addons

### Viewing Addons

**All Addons Tab:**
- System addons (🔵 System badge)
- Published user addons from all users (🟢 Published badge)
- Your unpublished addons (🟠 Draft badge)
- Search and filter available

**My Addons Tab:**
- Only addons you created
- Both draft and published versions
- Quick access to edit/manage

### Addon Status Badges

- 🔵 **System** - Built-in system addon (read-only, cannot edit or delete)
- 🟢 **Published** - Published addon (yours or someone else's, immutable)
- 🟠 **Draft** - Your unpublished addon (editable, deletable)

### Editing Draft Addons

**Only draft addons can be edited.**

1. Go to **My Addons** tab
2. Click on a 🟠 Draft addon card
3. Click **Edit** button on details page
4. Make your changes
5. Click **Save Changes**

**Editable Fields:**
- Name, description, category
- Display name
- Platform support
- All configuration (operators, scripts, manifests, helm charts)
- Metadata (conflicts, notes, warnings)

**Non-Editable Fields:**
- Addon ID (immutable after creation)
- Version (immutable after creation)
- Created by, created at

**Published addons cannot be edited.** To make changes, you must clone the addon to create a new draft version.

### Publishing Addons

When your addon is tested and production-ready:

**Steps:**
1. Go to **My Addons** tab
2. Click on your 🟠 Draft addon
3. Review all configuration one final time
4. Click **Publish** button
5. Confirm the action in the dialog

**What Happens:**
- Status changes from 🟠 Draft → 🟢 Published
- Addon becomes **immutable** (cannot be edited)
- Addon becomes **visible to all users**
- Anyone can use it in cluster creation
- \`published_at\` timestamp recorded
- \`is_immutable\` flag set to true

**Important:**
- **Publishing is one-way** - you cannot unpublish
- Test thoroughly before publishing
- Ensure all metadata is complete
- Verify platform support is correct

**Best Practices Before Publishing:**
- Test on a development cluster
- Review all notes and warnings
- Check for typos in descriptions
- Validate all scripts run successfully
- Verify operator names are correct
- Ensure dependencies are properly set

### Creating New Versions (Cloning)

To update a published addon, clone it to create a new version.

**Steps:**
1. Go to **My Addons** tab
2. Click on your 🟢 Published addon
3. Click **Clone** button
4. New draft is created with:
   - Same addon ID
   - New version (increment yourself)
   - All configuration copied
   - \`parent_version_id\` links to original

**After Cloning:**
1. Edit the draft to update version field
2. Make your changes
3. Test thoroughly
4. Publish when ready

**Version Strategy:**
\`\`\`
v1.0 (published)
  ↓ clone
v1.0-draft (draft)
  ↓ edit → change version to "v1.1"
v1.1 (draft)
  ↓ test
v1.1 (published)
\`\`\`

**Semantic Versioning Example:**
- \`v1.0.0\` → \`v1.0.1\` (bug fix)
- \`v1.0.0\` → \`v1.1.0\` (new features)
- \`v1.0.0\` → \`v2.0.0\` (breaking changes)

**Multiple Versions:**
- Same addon ID, different versions
- All versions available in addon browser
- Users choose which version to install
- Older versions remain available

### Deleting Addons

**Draft addons can be deleted:**

1. Go to **My Addons** tab
2. Click on 🟠 Draft addon
3. Click **Delete** button
4. Confirm the action

**What happens:**
- Addon permanently deleted from database
- Cannot be recovered
- Other users never saw it (was draft)

**Published addons CANNOT be deleted:**
- Ensures clusters using the addon continue to work
- Ensures version history is preserved
- Prevents breaking changes for other users

**To deprecate a published addon:**
- Create a new version without the deprecated functionality
- Add warning in notes
- Document migration path
- Publish new version

### Addon Details Page

Click any addon to view comprehensive details:

**Basic Information:**
- Name, display name, description
- Category, version
- Platform support
- Status badge (System/Published/Draft)

**Configuration:**
- **Operators** - List with name, namespace, channel, source
- **Scripts** - With descriptions and dependencies
- **Manifests** - With descriptions and dependencies
- **Helm Charts** - With repo, chart name, version

**Metadata:**
- Conflicts with (other addon IDs)
- Required capabilities
- Requires bare metal
- Notes (usage instructions)
- Warnings (important caveats)

**Audit Information:**
- Created by (user email)
- Created at (timestamp)
- Updated at (timestamp)
- Published at (if published)
- Parent version (if cloned)

**Actions:**
- **Edit** (🟠 Draft only)
- **Publish** (🟠 Draft only)
- **Clone** (🟢 Published or 🔵 System)
- **Delete** (🟠 Draft only)

## Best Practices

### Naming Conventions

**Addon IDs:**
- Use lowercase with hyphens: \`my-dev-tools\`
- Make descriptive: \`openshift-pipelines-setup\`
- Include purpose: \`monitoring-stack\`
- Avoid generic: \`test\`, \`addon1\`, \`temp\`

**Display Names:**
- Use title case: "Development Tools"
- Include version info: "Dev Tools v1.0"
- Be descriptive: "CNV 4.22 with Windows VM Support"

**Versions:**
- Semantic versioning: \`v1.0\`, \`v1.1\`, \`v2.0\`
- Channel names: \`stable\`, \`nightly\`, \`beta\`
- Be consistent across your addons

### Testing Guidelines

**Before Publishing:**

1. **Create test cluster with draft addon**
   - Select your draft in cluster creation
   - Monitor cluster creation logs
   - Check POST_CONFIGURE job

2. **Verify all components installed**
   - Check operator CSVs reached Succeeded state
   - Verify pods are running
   - Test scripts completed successfully
   - Validate manifests were applied

3. **Review job logs**
   - Look for errors or warnings
   - Check execution order was correct
   - Verify timeouts were sufficient

4. **Test on all supported platforms**
   - If platform: ["aws", "gcp"], test on both
   - Verify platform-specific resources work
   - Check for platform-dependent issues

5. **Test with different OpenShift versions**
   - Minimum supported version
   - Maximum supported version
   - Latest stable release

**Edge Cases to Test:**
- Empty clusters (no prior configuration)
- Clusters with existing namespaces
- Slow network conditions (timeout handling)
- Resource constrained clusters
- Multiple addon conflicts

### Security Best Practices

**Scripts:**
- Always use \`set -euo pipefail\` for bash safety
- Never hardcode secrets (use environment variables or secrets)
- Validate inputs before use
- Use HTTPS for downloads, never HTTP
- Avoid \`curl | bash\` patterns
- Use explicit versions for images and packages

**Images:**
- Use trusted registries (quay.io, docker.io official images)
- Pin image tags - never use \`:latest\`
- Scan images for vulnerabilities before use
- Use minimal base images (alpine, distroless)
- Verify image signatures when possible

**Permissions:**
- Request minimum required permissions
- Document why elevated permissions are needed
- Use namespace-scoped resources when possible
- Avoid cluster-admin role unless absolutely necessary
- Use RBAC to limit service account permissions

**Secrets Management:**
- Use Kubernetes secrets for sensitive data
- Never include secrets in addon configuration
- Reference external secret managers (Vault, AWS Secrets Manager)
- Document secret requirements in addon notes
- Provide example secret templates

**Example Secure Script:**
\`\`\`bash
#!/bin/bash
set -euo pipefail

# Validate required environment
if [ -z "\${NAMESPACE:-}" ]; then
  echo "ERROR: NAMESPACE not set"
  exit 1
fi

# Use specific versions
IMAGE="registry.access.redhat.com/ubi9/ubi-minimal:9.1"

# Avoid running as root
kubectl create serviceaccount myapp -n "\$NAMESPACE"

# Apply resource limits
kubectl apply -f - <<EOF
apiVersion: v1
kind: Pod
metadata:
  name: myapp
  namespace: \$NAMESPACE
spec:
  serviceAccountName: myapp
  securityContext:
    runAsNonRoot: true
    seccompProfile:
      type: RuntimeDefault
  containers:
  - name: app
    image: \$IMAGE
    resources:
      requests:
        memory: "64Mi"
        cpu: "100m"
      limits:
        memory: "128Mi"
        cpu: "200m"
EOF
\`\`\`

### Performance Optimization

**Parallel Execution:**
- Independent items run concurrently automatically
- Only use \`dependsOn\` when truly necessary
- Group related operations for efficiency

**Resource Limits:**
- Set appropriate CPU/memory requests and limits
- Avoid resource-intensive operations during install
- Use node selectors for large workloads
- Consider cluster size variations

**Timeout Tuning:**
- Start with conservative timeouts
- Reduce after testing confirms faster completion
- Account for slow networks and large images
- Consider cluster load variations

**Recommended Timeouts:**
- Simple scripts (kubectl get/create): 60s
- Namespace creation: 30s
- Operator installation: 600s (10 minutes)
- Large image pulls: 900s (15 minutes)
- Complex deployments: 1800s (30 minutes)

### Documentation in Addons

**Always include comprehensive metadata:**

**Description:**
- What the addon installs
- When to use it
- What gets deployed

**Notes:**
- Post-install access instructions
- Configuration options
- Usage examples
- Troubleshooting tips

**Example Notes:**
\`\`\`
- Access Grafana at: https://grafana-route-monitoring.apps.<cluster-domain>
- Default credentials: admin / (check secret monitoring/grafana-admin)
- Change password after first login
- To configure data sources: oc edit configmap grafana-datasources -n monitoring
- Dashboards: Import from https://grafana.com/grafana/dashboards
\`\`\`

**Warnings:**
- Resource requirements
- Platform limitations
- Compatibility constraints
- Cost implications
- Security considerations

**Example Warnings:**
\`\`\`
- Requires worker nodes with 16GB+ memory
- Uses 50GB persistent storage per Prometheus instance
- Increases cluster cost by approximately $50-75/month
- Default retention is 7 days (configurable)
- Bare metal or nested virtualization required
\`\`\`

### Common Pitfalls to Avoid

❌ **Don't:**
- Publish without thorough testing
- Use \`:latest\` image tags
- Hardcode cluster-specific values
- Create circular dependencies
- Set timeouts too short
- Forget to document breaking changes
- Skip platform compatibility testing
- Ignore error handling in scripts
- Use insecure HTTP downloads
- Run containers as root unnecessarily

✅ **Do:**
- Test extensively before publishing
- Pin specific image versions
- Use variables and parameters
- Validate dependency order
- Set reasonable timeouts with buffer
- Maintain clear version history
- Test on all supported platforms
- Add proper error handling
- Use HTTPS for all downloads
- Follow security best practices

### Version Management Strategy

**Semantic Versioning:**
- **Patch** (v1.0.0 → v1.0.1): Bug fixes, no new features
- **Minor** (v1.0.0 → v1.1.0): New features, backward compatible
- **Major** (v1.0.0 → v2.0.0): Breaking changes

**Channel Strategy:**
- \`stable\` - Production-ready, well-tested
- \`beta\` - Feature-complete, testing phase
- \`alpha\` / \`nightly\` - Development, unstable

**Migration Path:**
1. Document breaking changes in notes
2. Provide migration scripts if needed
3. Test migration thoroughly
4. Communicate changes to users
5. Keep old version available

**Example Version Progression:**
\`\`\`
v1.0 → Initial release (stable)
v1.1 → Add monitoring dashboard (stable)
v1.2 → Bug fixes (stable)
v2.0 → Migrate to new API (stable)
v2.0-beta → Testing phase
v2.1-nightly → Development builds
\`\`\`
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

The dashboard provides comprehensive system oversight:
- **Cluster Statistics** - System-wide metrics, cost breakdowns, and usage patterns
- **Orphaned Resources** - AWS/GCP/IBM Cloud resources without database entries
- **User Management** - Create and manage user accounts with RBAC
- **Team Management** - Configure team-based access control and quotas
- **Cluster Pool Management** - Create and monitor cluster pools
- **Long-Running Clusters** - Identify clusters exceeding expected TTL
- **Infrastructure Overview** - Real-time platform resource utilization

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

## Cluster Pool Management

**Access:** Admin Dashboard → Cluster Pools

### Creating a Pool

1. Navigate to **Admin** → **Cluster Pools**
2. Click **Create Pool** button
3. Configure pool settings:
   - **Name** - Unique identifier (e.g., \`dev-pool\`, \`ci-pool\`)
   - **Display Name** - User-facing name
   - **Description** - What this pool provides
   - **Profile** - Cluster profile to use (determines cluster configuration)
   - **Target Size** - Desired number of READY clusters
   - **Min Size** - Minimum pool size (default: 1)
   - **Max Size** - Maximum pool size (default: 2x target)
   - **Max Lease Duration** - Hours before auto-release (default: 2)
   - **Auto-Release Enabled** - Automatically release expired leases
   - **Max Cluster Age** - Days before clusters are refreshed (default: 7)
   - **Auto-Refresh Enabled** - Automatically destroy and recreate aged clusters
   - **Scheduled Mode** - Enable work hours scheduling
   - **Schedule Settings** - Timezone, start/end hours, days of week
   - **Cluster Config** - JSON overrides for cluster configuration

4. Click **Create Pool**

**Example Pool Configuration:**
\`\`\`json
{
  "name": "dev-pool",
  "display_name": "Development Pool",
  "description": "Fast SNO clusters for development and testing",
  "profile": "aws-sno-ga",
  "target_size": 3,
  "min_size": 1,
  "max_size": 5,
  "max_lease_duration_hours": 2,
  "auto_release_enabled": true,
  "max_cluster_age_days": 7,
  "scheduled_mode": false
}
\`\`\`

### Monitoring Pool Health

1. Navigate to pool details page
2. View real-time statistics:
   - **Ready Clusters** - Available for lease
   - **Leased Clusters** - Currently in use
   - **Provisioning Clusters** - Being created
   - **Total Clusters** - Overall pool size
   - **Health Ratio** - ready_clusters / target_size
   - **Utilization** - leased_clusters / total_clusters

**Key Metrics:**
- **Pool Health** - Ratio should be close to 1.0 (100%)
- **Lease Utilization** - High utilization indicates demand
- **Provisioning Time** - Average time to create new clusters
- **Failed Provisions** - Clusters that failed to create

### Adjusting Pool Size

**Via UI:**
1. Navigate to **Admin** → **Cluster Pools**
2. Click on pool name
3. Click **Edit** button
4. Update \`target_size\`, \`min_size\`, or \`max_size\`
5. Click **Save**

**Via API:**
\`\`\`bash
curl -X PATCH \\
  -H "Authorization: Bearer $ADMIN_TOKEN" \\
  -H "Content-Type: application/json" \\
  -d '{"target_size": 5}' \\
  https://ocpctl.mg.dog8code.com/api/v1/admin/pools/dev-pool
\`\`\`

**When to adjust:**
- **Increase** target_size if pools frequently empty
- **Decrease** target_size during off-hours to save costs
- **Enable scheduled_mode** for automatic work hours scaling

### Enabling/Disabling Pools

**Disable a pool:**
1. Navigate to pool details
2. Click **Edit**
3. Uncheck **Enabled**
4. Click **Save**

**Effect:**
- Pool no longer visible to regular users
- Existing leases continue until expiration
- New clusters are not provisioned
- Existing READY clusters remain available for admin

**Use cases:**
- Temporarily disable during maintenance
- Deprecate old pools without deleting
- Prevent new leases during migrations

### Deleting a Pool

1. Navigate to pool details
2. Click **Delete Pool** button
3. Confirm deletion

**⚠️ Important:**
- Pool is permanently deleted from database
- Clusters in pool are **orphaned** (pool_id set to NULL)
- Clusters continue running as regular clusters
- Users lose pool lease metadata
- Cannot be undone

**Best Practice:** Disable pools instead of deleting to preserve history.

### Pool Manager Background Service

The pool manager runs automatically and handles:

**1. Pool Replenishment** (every 30 seconds)
- Checks if ready_clusters < target_size
- Creates new clusters with pool_state=PROVISIONING
- Respects min_size and max_size bounds

**2. Lease Expiration** (every 60 seconds)
- Finds clusters with lease_expires_at < NOW()
- Updates pool_state to EXPIRED
- Queues clusters for release/cleanup

**3. Cluster Refresh** (daily)
- Identifies clusters older than max_cluster_age_days
- Destroys aged clusters
- Replenishes with fresh clusters

**4. Work Hours Enforcement** (if scheduled_mode enabled)
- Checks current time against schedule
- Outside hours: Scales pool to 0 (destroys READY clusters)
- During hours: Restores pool to target_size

**Monitoring Pool Manager:**
\`\`\`bash
# SSH to API server
ssh -i ~/.ssh/ocpctl-production-key ubuntu@44.201.165.78

# Check pool manager logs
sudo journalctl -u ocpctl-api -f | grep -i "pool"

# Look for:
# - "Pool manager: replenishing pool X (ready: Y, target: Z)"
# - "Pool manager: releasing expired lease for cluster X"
# - "Pool manager: refreshing aged cluster X"
\`\`\`

### Troubleshooting Pools

**Pool Not Replenishing:**
1. Check pool manager logs for errors
2. Verify API server is running: \`systemctl status ocpctl-api\`
3. Check if profile exists and is valid
4. Verify AWS/GCP credentials are configured
5. Check for quota limits in cloud provider

**High Lease Rejections:**
1. Increase target_size
2. Check provisioning_clusters count (may be slow)
3. Verify pool is enabled
4. Check work hours schedule

**Clusters Stuck in PROVISIONING:**
1. Check worker service logs: \`journalctl -u ocpctl-worker -f\`
2. Look for provisioning job errors
3. Check cloud provider console for failed resources
4. May need to manually destroy and retry

**Cost Optimization:**
- Use scheduled_mode for dev/test pools
- Set max_cluster_age_days to prevent stale clusters
- Monitor utilization and adjust target_size accordingly
- Use SNO profiles for cost-effective pools

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
