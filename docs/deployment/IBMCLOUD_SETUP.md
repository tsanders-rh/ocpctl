# IBM Cloud Platform Setup Guide

This guide covers the configuration and deployment requirements for using ocpctl with IBM Cloud VPC.

## Table of Contents

- [Prerequisites](#prerequisites)
- [IBM Cloud Credentials](#ibm-cloud-credentials)
- [DNS Configuration](#dns-configuration)
- [Environment Variables](#environment-variables)
- [IAM Permissions](#iam-permissions)
- [Creating Your First IBM Cloud Cluster](#creating-your-first-ibm-cloud-cluster)
- [Troubleshooting](#troubleshooting)

## Prerequisites

Before using ocpctl with IBM Cloud, ensure you have:

1. **IBM Cloud Account** with active subscription
2. **IAM API Key** with required permissions (see [IAM Permissions](#iam-permissions))
3. **IBM Cloud CLI** (optional, for manual verification)
4. **IBM Cloud Internet Services (CIS)** instance for DNS
5. **Resource Group** (default or custom)
6. **OpenShift Pull Secret** from Red Hat

## IBM Cloud Credentials

ocpctl supports multiple credential sources for IBM Cloud, checked in the following order:

### 1. Environment Variables (Recommended)

Set these environment variables on the ocpctl worker/API server:

```bash
# Required
export IC_API_KEY="<your-ibm-cloud-api-key>"

# Optional (auto-detected if not provided)
export IC_ACCOUNT_ID="<your-account-id>"
export IC_REGION="us-south"
export IC_RESOURCE_GROUP="default"
```

### 2. IBM Cloud CLI Configuration

If environment variables are not set, ocpctl will attempt to use credentials from `~/.bluemix/config.json` (IBM Cloud CLI configuration).

### Creating an API Key

1. Log in to IBM Cloud Console
2. Navigate to **Manage > Access (IAM) > API keys**
3. Click **Create an IBM Cloud API key**
4. Enter a name (e.g., "ocpctl-automation")
5. Click **Create** and save the API key securely
6. Copy the API key and store it in your environment variables

## DNS Configuration

IBM Cloud OpenShift clusters require IBM Cloud Internet Services (CIS) for DNS management. ocpctl uses subdomain delegation to support both AWS and IBM Cloud clusters under the same parent domain.

### DNS Architecture

```
mg.dog8code.com (Route 53 - Parent Domain)
├── cluster1.mg.dog8code.com (AWS cluster)
├── cluster2.mg.dog8code.com (AWS cluster)
└── ibm.mg.dog8code.com (CIS - Delegated Subdomain)
    ├── test1.ibm.mg.dog8code.com (IBM Cloud cluster)
    └── dev1.ibm.mg.dog8code.com (IBM Cloud cluster)
```

### Setting Up DNS Delegation

#### Step 1: Create CIS Instance (if not already created)

```bash
# Create CIS instance
ibmcloud resource service-instance-create ocpctl-cis \
  internet-svcs standard-next global

# Get CIS name servers
ibmcloud cis instances
ibmcloud cis domain-add ibm.mg.dog8code.com --instance ocpctl-cis
ibmcloud cis domain ibm.mg.dog8code.com --instance ocpctl-cis
```

The output will show CIS name servers like:
```
ns1.bluemix.net
ns2.bluemix.net
```

#### Step 2: Delegate Subdomain in Route 53

Create an NS record in Route 53 for the IBM Cloud subdomain:

```bash
# Create NS record delegation
aws route53 change-resource-record-sets \
  --hosted-zone-id Z2GE8CSGW2ZA8W \
  --change-batch '{
    "Changes": [{
      "Action": "CREATE",
      "ResourceRecordSet": {
        "Name": "ibm.mg.dog8code.com",
        "Type": "NS",
        "TTL": 300,
        "ResourceRecords": [
          {"Value": "ns1.bluemix.net"},
          {"Value": "ns2.bluemix.net"}
        ]
      }
    }]
  }'
```

#### Step 3: Verify DNS Delegation

```bash
# Query subdomain via public DNS
dig @8.8.8.8 ibm.mg.dog8code.com NS

# Expected output shows CIS name servers
# ibm.mg.dog8code.com.    300    IN    NS    ns1.bluemix.net.
# ibm.mg.dog8code.com.    300    IN    NS    ns2.bluemix.net.
```

#### Step 4: Update Profile Base Domains

The IBM Cloud profiles are already configured to use `ibm.mg.dog8code.com`:

```yaml
# internal/profile/definitions/ibmcloud-standard.yaml
baseDomains:
  allowlist:
    - ibm.mg.dog8code.com
  default: ibm.mg.dog8code.com
```

## Environment Variables

### Worker/API Server Configuration

Add these to `/etc/ocpctl/worker.env` or your environment:

```bash
# IBM Cloud Authentication
IC_API_KEY=<your-ibm-cloud-api-key>
IC_ACCOUNT_ID=<your-account-id>  # Optional
IC_REGION=us-south               # Optional, defaults to profile region
IC_RESOURCE_GROUP=default        # Optional

# OpenShift Pull Secret (required for all platforms)
OPENSHIFT_PULL_SECRET='<your-pull-secret-json>'

# Work Directory
WORK_DIR=/var/ocpctl/work

# Profile Directory
PROFILES_DIR=/opt/ocpctl/profiles
```

### Restart Services After Configuration

```bash
# Restart worker to pick up new environment variables
sudo systemctl restart ocpctl-worker

# Verify environment is loaded
sudo systemctl status ocpctl-worker
sudo journalctl -u ocpctl-worker -n 50
```

## IAM Permissions

The IBM Cloud IAM API key requires the following permissions:

### Required Service Roles

| Service | Role | Purpose |
|---------|------|---------|
| VPC Infrastructure Services | Administrator | Create and manage VPC, subnets, security groups, instances |
| Cloud Object Storage | Manager | Store cluster artifacts and CCO credentials |
| IAM Identity Service | Operator | Create and manage service IDs for CCO |
| Resource Controller | Administrator | Manage resource groups |
| Internet Services (CIS) | Manager | Create DNS records for cluster ingress |

### Creating a Service ID with Required Permissions

```bash
# Create service ID
ibmcloud iam service-id-create ocpctl-automation \
  --description "Service ID for ocpctl cluster automation"

# Create API key for service ID
ibmcloud iam service-api-key-create ocpctl-key ocpctl-automation \
  --description "API key for ocpctl automation"

# Assign access policies
ibmcloud iam service-policy-create ocpctl-automation \
  --roles Administrator \
  --service-name is

ibmcloud iam service-policy-create ocpctl-automation \
  --roles Manager \
  --service-name cloud-object-storage

ibmcloud iam service-policy-create ocpctl-automation \
  --roles Operator \
  --service-name iam-identity

ibmcloud iam service-policy-create ocpctl-automation \
  --roles Administrator \
  --service-name resource-controller
```

### Validating Permissions

ocpctl automatically validates IAM permissions when creating an IBM Cloud cluster:

```go
// Validation happens in internal/ibmcloud/credentials.go
- VPC list access (validates VPC permissions)
- Resource group access (validates resource management)
- IAM service ID creation (validates identity management)
```

If permissions are insufficient, you'll see an error during cluster creation:
```
Error: validate IAM permissions: insufficient permissions for VPC infrastructure
```

## Creating Your First IBM Cloud Cluster

### 1. Verify Configuration

```bash
# Check IBM Cloud credentials are configured
echo $IC_API_KEY  # Should show your API key

# Verify DNS delegation
dig @8.8.8.8 ibm.mg.dog8code.com NS

# Check profile availability
curl -H "Authorization: Bearer <token>" \
  http://ocpctl.mg.dog8code.com/api/v1/profiles | jq '.data[] | select(.platform == "ibmcloud")'
```

### 2. Create Cluster via API

```bash
# Minimal test cluster (3-node, masters schedulable)
curl -X POST http://ocpctl.mg.dog8code.com/api/v1/clusters \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "ibm-test-1",
    "platform": "ibmcloud",
    "version": "4.20.3",
    "profile": "ibmcloud-minimal-test",
    "region": "us-south",
    "base_domain": "ibm.mg.dog8code.com",
    "owner": "user@example.com",
    "team": "Platform Engineering",
    "cost_center": "733",
    "ttl_hours": 24
  }'
```

### 3. Monitor Cluster Creation

```bash
# Get cluster status
curl -H "Authorization: Bearer <token>" \
  http://ocpctl.mg.dog8code.com/api/v1/clusters/<cluster-id>

# Watch logs
curl -H "Authorization: Bearer <token>" \
  http://ocpctl.mg.dog8code.com/api/v1/clusters/<cluster-id>/logs
```

### 4. Access Cluster

Once the cluster is in `READY` status:

```bash
# Get kubeconfig
curl -H "Authorization: Bearer <token>" \
  http://ocpctl.mg.dog8code.com/api/v1/clusters/<cluster-id>/kubeconfig \
  > kubeconfig

# Set KUBECONFIG
export KUBECONFIG=./kubeconfig

# Verify cluster access
oc get nodes
oc get co  # Check cluster operators
```

## Cloud Credential Operator (CCO) Workflow

IBM Cloud clusters use CCO in **Manual Mode**, which requires the following workflow:

### 1. Pre-Installation (Automatic)

ocpctl handles CCO setup automatically via `handler_create_ibmcloud.go`:

```
1. Detect IBM Cloud credentials (IC_API_KEY)
2. Validate credentials and IAM permissions
3. Run ccoctl to extract CredentialsRequests from release image
4. Create service IDs and API keys for each operator
5. Generate manifests with credentials
6. Pass manifests to openshift-install
```

### 2. During Installation

OpenShift installer uses the CCO-generated manifests to configure operators with dedicated service IDs:

```
- openshift-ingress-operator → Service ID: <cluster-name>-openshift-ingress-operator
- openshift-cluster-csi-drivers → Service ID: <cluster-name>-openshift-cluster-csi-drivers
- openshift-image-registry → Service ID: <cluster-name>-openshift-image-registry
- openshift-machine-api → Service ID: <cluster-name>-openshift-machine-api
```

### 3. Post-Destruction (Automatic)

ocpctl cleans up service IDs during cluster deletion via `handler_destroy_ibmcloud.go`:

```
1. Run openshift-install destroy cluster
2. Run ccoctl ibmcloud delete-service-id --name=<cluster-name>
3. Remove all operator service IDs
4. Clean up work directory
```

## Troubleshooting

### Credential Detection Issues

**Problem:** `Error: detect IBM Cloud credentials: IC_API_KEY not found`

**Solution:**
```bash
# Verify environment variable is set
echo $IC_API_KEY

# If using systemd, add to service file
sudo vi /etc/systemd/system/ocpctl-worker.service

[Service]
Environment="IC_API_KEY=<your-key>"

# Reload and restart
sudo systemctl daemon-reload
sudo systemctl restart ocpctl-worker
```

### DNS Resolution Failures

**Problem:** Cluster creation fails with `DNS resolution failed for ibm.mg.dog8code.com`

**Solution:**
```bash
# Verify delegation is configured
dig @8.8.8.8 ibm.mg.dog8code.com NS

# Check CIS instance is active
ibmcloud cis instances
ibmcloud cis domain ibm.mg.dog8code.com --instance ocpctl-cis

# Verify domain is in "Active" state (not "Pending")
```

### CCO Workflow Failures

**Problem:** `Error: run ccoctl for IBM Cloud: failed to create service IDs`

**Solution:**
```bash
# Verify IAM permissions
ibmcloud iam user-policies <your-email>

# Check service ID quota
ibmcloud iam service-ids | wc -l
# IBM Cloud free tier allows up to 100 service IDs

# Manually test ccoctl
ccoctl ibmcloud create-service-id \
  --credentials-requests-dir ./credsreqs \
  --name test-cluster \
  --output-dir ./cco-output \
  --region us-south
```

### Instance Type Validation Errors

**Problem:** `Error: invalid control plane instance type: bx2-2x4`

**Solution:**
```bash
# Check available instance types in region
ibmcloud is instance-profiles --region us-south

# Use supported types from profile definitions
# - bx2-4x16 (balanced, 4 vCPU, 16 GB RAM)
# - bx2-8x32 (balanced, 8 vCPU, 32 GB RAM)
# - cx2-4x8 (compute, 4 vCPU, 8 GB RAM)
# - mx2-4x32 (memory, 4 vCPU, 32 GB RAM)
```

### Region Not Supported

**Problem:** `Error: invalid IBM Cloud region: us-west`

**Solution:**

ocpctl currently supports these IBM Cloud regions:
- `us-south` (Dallas)
- `us-east` (Washington DC)
- `eu-de` (Frankfurt)
- `eu-gb` (London)
- `jp-tok` (Tokyo)
- `jp-osa` (Osaka)
- `au-syd` (Sydney)
- `ca-tor` (Toronto)
- `br-sao` (São Paulo)

Update profile configuration in `internal/profile/definitions/ibmcloud-*.yaml` to add more regions.

### Installation Timeout

**Problem:** Cluster creation exceeds timeout (60 minutes)

**Solution:**
```bash
# Check IBM Cloud service status
ibmcloud status

# View detailed installation logs
curl -H "Authorization: Bearer <token>" \
  http://ocpctl.mg.dog8code.com/api/v1/clusters/<cluster-id>/logs

# Common causes:
# - VPC quota exceeded
# - Insufficient instance capacity in zone
# - CIS DNS propagation delays
# - Image pull rate limiting
```

## Reference Links

- [OpenShift on IBM Cloud VPC Documentation](https://docs.openshift.com/container-platform/latest/installing/installing_ibm_cloud_public/installing-ibm-cloud-vpc.html)
- [IBM Cloud VPC Instance Profiles](https://cloud.ibm.com/docs/vpc?topic=vpc-profiles)
- [IBM Cloud IAM Permissions](https://cloud.ibm.com/docs/account?topic=account-iam-service-roles-actions)
- [Cloud Credential Operator Manual Mode](https://docs.openshift.com/container-platform/latest/authentication/managing_cloud_provider_credentials/cco-mode-manual.html)
- [IBM Cloud Internet Services (CIS)](https://cloud.ibm.com/docs/cis)
