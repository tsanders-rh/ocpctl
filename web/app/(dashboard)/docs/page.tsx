"use client";

import { useState } from "react";
import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";
import { Book, Users } from "lucide-react";

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
