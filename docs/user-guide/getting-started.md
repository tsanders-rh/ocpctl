# Getting Started with OCPCTL

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

- [Creating Your First Cluster](cluster-management.md#creating-a-cluster)
- [Understanding Cluster Profiles](cluster-management.md#cluster-profiles)
- [Managing Cluster Lifecycle](cluster-management.md#cluster-operations)
