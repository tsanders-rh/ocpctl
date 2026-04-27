# GCP Setup Guide for ocpctl

This guide covers setting up ocpctl for Google Cloud Platform (GCP) support, including both GKE (Google Kubernetes Engine) and OpenShift on GCP deployments.

**Estimated Time:** 45-60 minutes

## Overview

This guide will help you:
- Configure GCP authentication and permissions
- Enable required GCP APIs
- Install and configure required tools (`gcloud`, `openshift-install`)
- Set up service accounts and IAM roles
- Deploy your first GKE or OpenShift cluster on GCP

## Supported Cluster Types

ocpctl supports two types of GCP clusters:

### Google Kubernetes Engine (GKE)
- **Managed Kubernetes** by Google Cloud
- **No control plane costs** (only compute resources billed)
- **Auto-scaling** node pools (1-10 nodes)
- **Workload Identity** for secure pod authentication
- **Kubernetes Dashboard** with token-based auth
- **Estimated cost**: ~$0.05/hour for 3x e2-medium nodes (~$37/month)

### OpenShift on GCP
- **Red Hat OpenShift** deployed on GCP Compute Engine
- **Full OpenShift features** (OperatorHub, Web Console, etc.)
- **Hibernate/Resume** support (stop/start VMs)
- **Custom VPC** and subnet configuration
- **Estimated cost**: ~$1.20/hour for standard 6-node cluster (~$870/month)

---

## Prerequisites

### Required Tools
- [ ] `gcloud` CLI (Google Cloud SDK)
- [ ] `kubectl` (Kubernetes CLI)
- [ ] `openshift-install` (for OpenShift clusters only)
- [ ] Git installed
- [ ] ocpctl deployed and running

### GCP Account Requirements
- [ ] GCP account with billing enabled
- [ ] Project with appropriate IAM permissions
- [ ] Service account JSON key (for programmatic access)
- [ ] OpenShift pull secret (for OpenShift clusters only, from console.redhat.com)

### Network & Firewall
- [ ] Network connectivity to GCP APIs
- [ ] Firewall rules allowing outbound HTTPS (443)
- [ ] SSH access to GCP VMs (optional, for debugging)

---

## Part 1: GCP Project Setup (10 minutes)

### 1.1 Create or Select GCP Project

```bash
# List existing projects
gcloud projects list

# Create a new project (or use an existing one)
export GCP_PROJECT_ID="ocpctl-dev"
gcloud projects create $GCP_PROJECT_ID --name="ocpctl Development"

# Set as default project
gcloud config set project $GCP_PROJECT_ID

# Link billing account (replace with your billing account ID)
# Find billing account ID: gcloud billing accounts list
gcloud billing projects link $GCP_PROJECT_ID --billing-account=XXXXXX-YYYYYY-ZZZZZZ
```

### 1.2 Enable Required APIs

```bash
# Core APIs
gcloud services enable compute.googleapis.com          # Compute Engine (required)
gcloud services enable container.googleapis.com        # GKE (required for GKE clusters)
gcloud services enable dns.googleapis.com              # Cloud DNS (optional)
gcloud services enable storage.googleapis.com          # Cloud Storage (optional)
gcloud services enable serviceusage.googleapis.com     # Service Usage API
gcloud services enable cloudresourcemanager.googleapis.com  # Resource Manager API

# Optional: Billing API (for cost tracking)
gcloud services enable cloudbilling.googleapis.com
gcloud services enable bigquery.googleapis.com         # For billing export

# Verify APIs are enabled
gcloud services list --enabled
```

**Expected output:**
```
NAME                                 TITLE
compute.googleapis.com              Compute Engine API
container.googleapis.com            Kubernetes Engine API
dns.googleapis.com                  Cloud DNS API
storage.googleapis.com              Cloud Storage
...
```

---

## Part 2: Service Account and IAM Setup (10 minutes)

### 2.1 Create Service Account

ocpctl needs a service account with appropriate permissions to manage GCP resources.

```bash
# Create service account
export SA_NAME="ocpctl-service-account"
export SA_EMAIL="${SA_NAME}@${GCP_PROJECT_ID}.iam.gserviceaccount.com"

gcloud iam service-accounts create $SA_NAME \
  --display-name="ocpctl Service Account" \
  --description="Service account for ocpctl to manage GCP clusters"
```

### 2.2 Grant IAM Roles

Grant the service account necessary permissions:

```bash
# Compute Admin (for VM instances, disks, networks)
gcloud projects add-iam-policy-binding $GCP_PROJECT_ID \
  --member="serviceAccount:${SA_EMAIL}" \
  --role="roles/compute.admin"

# Kubernetes Engine Admin (for GKE clusters)
gcloud projects add-iam-policy-binding $GCP_PROJECT_ID \
  --member="serviceAccount:${SA_EMAIL}" \
  --role="roles/container.admin"

# Storage Admin (for GCS buckets, artifacts)
gcloud projects add-iam-policy-binding $GCP_PROJECT_ID \
  --member="serviceAccount:${SA_EMAIL}" \
  --role="roles/storage.admin"

# DNS Administrator (optional, for Cloud DNS)
gcloud projects add-iam-policy-binding $GCP_PROJECT_ID \
  --member="serviceAccount:${SA_EMAIL}" \
  --role="roles/dns.admin"

# Service Account User (to act as service accounts)
gcloud projects add-iam-policy-binding $GCP_PROJECT_ID \
  --member="serviceAccount:${SA_EMAIL}" \
  --role="roles/iam.serviceAccountUser"

# Optional: Billing Account Viewer (for cost tracking)
gcloud projects add-iam-policy-binding $GCP_PROJECT_ID \
  --member="serviceAccount:${SA_EMAIL}" \
  --role="roles/billing.viewer"
```

### 2.3 Create and Download Service Account Key

```bash
# Create JSON key file
gcloud iam service-accounts keys create ~/ocpctl-gcp-key.json \
  --iam-account=${SA_EMAIL}

# Verify key was created
ls -lh ~/ocpctl-gcp-key.json

# Set restrictive permissions
chmod 600 ~/ocpctl-gcp-key.json
```

**⚠️ Security Warning:**
- Keep this JSON key file **secure** - it grants full access to your GCP project
- **Never commit** this file to Git or expose it publicly
- Rotate keys regularly (every 90 days recommended)
- Consider using Workload Identity for production deployments

### 2.4 Configure Service Account on ocpctl Server

Copy the service account JSON key to your ocpctl server:

```bash
# Copy to ocpctl server (adjust path and server as needed)
scp ~/ocpctl-gcp-key.json ocpctl-server:/opt/ocpctl/gcp-credentials.json

# SSH into ocpctl server
ssh ocpctl-server

# Set environment variable (add to systemd service files)
export GOOGLE_APPLICATION_CREDENTIALS=/opt/ocpctl/gcp-credentials.json
export GCP_PROJECT=ocpctl-dev

# Verify authentication
gcloud auth activate-service-account --key-file=/opt/ocpctl/gcp-credentials.json
gcloud config set project ocpctl-dev
```

---

## Part 3: Tool Installation (10-15 minutes)

### 3.1 Install Google Cloud SDK

The Google Cloud SDK includes `gcloud` CLI and `gsutil`.

**On Linux (Ubuntu/Debian):**

```bash
# Add Google Cloud SDK repository
echo "deb [signed-by=/usr/share/keyrings/cloud.google.gpg] https://packages.cloud.google.com/apt cloud-sdk main" | \
  sudo tee -a /etc/apt/sources.list.d/google-cloud-sdk.list

# Import Google Cloud public key
curl https://packages.cloud.google.com/apt/doc/apt-key.gpg | \
  sudo apt-key --keyring /usr/share/keyrings/cloud.google.gpg add -

# Update and install
sudo apt-get update
sudo apt-get install -y google-cloud-sdk

# Verify installation
gcloud version
```

**On macOS:**

```bash
# Using Homebrew
brew install --cask google-cloud-sdk

# Or download installer from:
# https://cloud.google.com/sdk/docs/install-sdk

# Verify installation
gcloud version
```

**Expected output:**
```
Google Cloud SDK 467.0.0
bq 2.1.3
core 2024.02.23
gcloud-crc32c 1.0.0
gsutil 5.27
```

### 3.2 Install kubectl

```bash
# Install kubectl via gcloud
gcloud components install kubectl

# Or install via package manager
# Ubuntu/Debian:
sudo apt-get install -y kubectl

# macOS:
brew install kubectl

# Verify installation
kubectl version --client
```

### 3.3 Install openshift-install (For OpenShift Clusters)

Skip this section if you only plan to use GKE clusters.

```bash
# Download openshift-install for your OpenShift version
# Example: OpenShift 4.20
export OCP_VERSION=4.20.3
export OCP_ARCH=linux  # or 'darwin' for macOS

# Download from mirror.openshift.com
wget https://mirror.openshift.com/pub/openshift-v4/${OCP_ARCH}/clients/ocp/${OCP_VERSION}/openshift-install-${OCP_ARCH}.tar.gz

# Extract
tar -xzf openshift-install-${OCP_ARCH}.tar.gz

# Move to PATH
sudo mv openshift-install /usr/local/bin/

# Verify installation
openshift-install version
```

**Expected output:**
```
openshift-install 4.20.3
built from commit abcd1234567890
release image quay.io/openshift-release-dev/ocp-release@sha256:...
```

---

## Part 4: GCP Networking Setup (Optional, 5-10 minutes)

ocpctl can automatically create VPCs and subnets, but you may want to create shared infrastructure.

### 4.1 Create Shared VPC (Optional)

```bash
# Create VPC
export VPC_NAME="ocpctl-shared-vpc"
gcloud compute networks create $VPC_NAME \
  --subnet-mode=custom \
  --description="Shared VPC for ocpctl clusters"

# Create subnet for us-central1
gcloud compute networks subnets create ${VPC_NAME}-us-central1 \
  --network=$VPC_NAME \
  --region=us-central1 \
  --range=10.0.0.0/20

# Create firewall rule to allow internal traffic
gcloud compute firewall-rules create ${VPC_NAME}-allow-internal \
  --network=$VPC_NAME \
  --allow=tcp,udp,icmp \
  --source-ranges=10.0.0.0/8,172.16.0.0/12,192.168.0.0/16 \
  --description="Allow internal traffic"

# Create firewall rule for SSH
gcloud compute firewall-rules create ${VPC_NAME}-allow-ssh \
  --network=$VPC_NAME \
  --allow=tcp:22 \
  --source-ranges=0.0.0.0/0 \
  --description="Allow SSH from anywhere"

# Create firewall rule for HTTPS/API
gcloud compute firewall-rules create ${VPC_NAME}-allow-https \
  --network=$VPC_NAME \
  --allow=tcp:443,tcp:6443 \
  --source-ranges=0.0.0.0/0 \
  --description="Allow HTTPS and Kubernetes API"
```

---

## Part 5: Cost Tracking Setup (Optional, 15 minutes)

Enable BigQuery billing export for accurate cost tracking.

### 5.1 Create BigQuery Dataset

```bash
# Create dataset for billing export
export BILLING_DATASET="billing_export"
bq mk --dataset --location=US $BILLING_DATASET

# Verify dataset
bq ls
```

### 5.2 Enable Billing Export

1. Go to [Google Cloud Console → Billing → Billing Export](https://console.cloud.google.com/billing)
2. Click "Enable Billing Export"
3. Select your BigQuery dataset: `billing_export`
4. Click "Save"

**Note:** Billing data appears within 24 hours of export setup.

### 5.3 Grant Billing Permissions to Service Account

```bash
# Grant BigQuery Data Viewer role
gcloud projects add-iam-policy-binding $GCP_PROJECT_ID \
  --member="serviceAccount:${SA_EMAIL}" \
  --role="roles/bigquery.dataViewer"

# Grant BigQuery Job User role (to run queries)
gcloud projects add-iam-policy-binding $GCP_PROJECT_ID \
  --member="serviceAccount:${SA_EMAIL}" \
  --role="roles/bigquery.jobUser"
```

### 5.4 Configure ocpctl for Billing Tracking

Add to your ocpctl environment configuration:

```bash
# On ocpctl server
export GCP_PROJECT="ocpctl-dev"
export GCP_BILLING_DATASET="billing_export"
export GCP_BILLING_TABLE="gcp_billing_export_v1_XXXXXX_YYYYYY_ZZZZZZ"
```

**Note:** The billing table name is auto-generated by GCP. Find it with:
```bash
bq ls $GCP_BILLING_DATASET
```

For detailed cost tracking documentation, see [GCP_COST_TRACKING.md](../GCP_COST_TRACKING.md).

---

## Part 6: Deploy Your First Cluster (10-15 minutes)

### 6.1 Create GKE Cluster

Using the ocpctl Web UI:

1. Navigate to **Clusters → Create New Cluster**
2. Select **Platform**: Google Cloud Platform
3. Select **Cluster Type**: Google Kubernetes Engine (GKE)
4. Select **Profile**: GCP GKE Standard Cluster
5. Choose **Kubernetes Version**: 1.34 (or latest)
6. Choose **Region**: us-central1
7. Set **Cluster Name**: `my-gke-test`
8. Set **Owner Email**: your email
9. Set **Team**: your team
10. Click **Create Cluster**

**Estimated creation time:** 10-15 minutes

### 6.2 Create OpenShift on GCP

Using the ocpctl Web UI:

1. Navigate to **Clusters → Create New Cluster**
2. Select **Platform**: Google Cloud Platform
3. Select **Cluster Type**: OpenShift
4. Select **Profile**: GCP OpenShift Standard Cluster
5. Choose **OpenShift Version**: 4.20.3 (or latest GA)
6. Choose **Region**: us-central1
7. Choose **Base Domain**: (your registered domain)
8. Set **Cluster Name**: `my-ocp-test`
9. Set **Owner Email**: your email
10. Set **Team**: your team
11. Click **Create Cluster**

**Estimated creation time:** 45-60 minutes

---

## Part 7: Verification (5 minutes)

### 7.1 Verify GKE Cluster

```bash
# Get cluster credentials
gcloud container clusters get-credentials my-gke-test --region=us-central1

# Verify cluster access
kubectl get nodes

# Expected output:
# NAME                                       STATUS   ROLES    AGE   VERSION
# gke-my-gke-test-default-pool-xxxxx-yyyy   Ready    <none>   5m    v1.34.0-gke.1500
```

### 7.2 Verify OpenShift Cluster

```bash
# Get kubeconfig from ocpctl UI
# Navigate to Cluster Details → Outputs → Download Kubeconfig

# Set KUBECONFIG environment variable
export KUBECONFIG=/path/to/downloaded/kubeconfig

# Verify cluster access
oc get nodes

# Expected output:
# NAME                        STATUS   ROLES                  AGE   VERSION
# my-ocp-test-master-0        Ready    control-plane,master   60m   v1.27.0+xxxxx
# my-ocp-test-worker-0        Ready    worker                 55m   v1.27.0+xxxxx
```

### 7.3 Access Web Consoles

**GKE Kubernetes Dashboard:**
1. Navigate to Cluster Details in ocpctl UI
2. Click **Kubernetes Dashboard URL**
3. Use the provided token to authenticate

**OpenShift Web Console:**
1. Navigate to Cluster Details in ocpctl UI
2. Click **Console URL**
3. Log in with `kubeadmin` and the provided password

---

## Cost Management

### Daily Cost Estimates

**GKE Standard (3x e2-medium nodes):**
- Running: ~$0.05/hour ($1.20/day, $37/month)
- Hibernated: ~$0.0015/hour ($0.04/day, $1/month)
- **Savings with hibernation:** ~97%

**OpenShift Standard (6-node cluster):**
- Running: ~$1.20/hour ($28.80/day, $870/month)
- Hibernated: ~$0.12/hour ($2.88/day, $87/month)
- **Savings with hibernation:** ~90%

### Cost Optimization Tips

1. **Enable Work Hours Automation**
   - Automatically hibernate clusters during off-hours
   - Configure in User Profile → Work Hours settings
   - Example: 9am-5pm weekdays saves ~65%

2. **Use Appropriate Instance Types**
   - GKE: e2-micro for testing, e2-medium for dev
   - OpenShift: n2-standard-4 for control plane, n2-standard-8 for workers

3. **Enable Cluster Autoscaling (GKE)**
   - Nodes automatically scale from 1 to 10 based on load
   - Saves costs during low-usage periods

4. **Set Cluster TTL**
   - Automatically destroy clusters after X hours
   - Prevents forgotten clusters from accumulating costs

5. **Monitor with Cloud Billing**
   - Set up budget alerts in GCP Console
   - Track costs by cluster using labels

---

## Troubleshooting

### "Permission denied" errors

**Cause:** Service account lacks required IAM roles

**Solution:**
```bash
# Verify service account permissions
gcloud projects get-iam-policy $GCP_PROJECT_ID \
  --flatten="bindings[].members" \
  --filter="bindings.members:serviceAccount:${SA_EMAIL}"

# Re-grant missing roles (see Part 2.2)
```

### "API not enabled" errors

**Cause:** Required GCP APIs not enabled

**Solution:**
```bash
# List enabled APIs
gcloud services list --enabled

# Enable missing APIs
gcloud services enable compute.googleapis.com container.googleapis.com
```

### "Quota exceeded" errors

**Cause:** GCP project quota limits reached

**Solution:**
1. Check quotas: [GCP Console → IAM & Admin → Quotas](https://console.cloud.google.com/iam-admin/quotas)
2. Request quota increase for:
   - CPUs (regional)
   - In-use IP addresses
   - Persistent disks

### GKE cluster creation fails

**Common causes:**
- Insufficient quota
- Network/subnet conflicts
- Service account permissions

**Debug:**
```bash
# Check GKE operation logs
gcloud container operations list --region=us-central1

# Describe failed operation
gcloud container operations describe OPERATION_ID --region=us-central1
```

### OpenShift installation hangs

**Common causes:**
- DNS resolution issues
- API rate limits
- Disk space on control plane

**Debug:**
1. Check cluster logs in ocpctl UI
2. SSH into bastion/bootstrap node
3. Review openshift-install logs

---

## Next Steps

- **Configure DNS**: Set up Cloud DNS zones for custom domains
- **Enable Monitoring**: Set up Cloud Monitoring and Logging
- **Set Up Shared Storage**: Configure GCS buckets or Filestore
- **Review Cost Tracking**: See [GCP_COST_TRACKING.md](../GCP_COST_TRACKING.md)
- **Explore Profiles**: Review available GCP profiles in `/internal/profile/definitions/`

---

## Additional Resources

- [GCP Documentation](https://cloud.google.com/docs)
- [GKE Documentation](https://cloud.google.com/kubernetes-engine/docs)
- [OpenShift on GCP Installation](https://docs.openshift.com/container-platform/4.18/installing/installing_gcp/preparing-to-install-on-gcp.html)
- [gcloud CLI Reference](https://cloud.google.com/sdk/gcloud/reference)
- [GCP Cost Optimization Best Practices](https://cloud.google.com/architecture/cost-optimization-best-practices)

---

## Support

For issues or questions:
- Check the [Troubleshooting](#troubleshooting) section above
- Review GCP service health: https://status.cloud.google.com/
- File an issue: https://github.com/tsanders-rh/ocpctl/issues
