# Windows VM Deployment via S3

This directory contains manifests for automated Windows 10 VM deployment using S3-backed storage for OpenShift Virtualization.

## Overview

These manifests implement [Issue #20](https://github.com/tsanders-rh/ocpctl/issues/20): automated Windows VM deployment for the `aws-virt-windows-minimal` profile using S3 as the image distribution source.

**Benefits:**
- Fast deployment: 5-10 minutes (vs 25-30 minutes from Artifactory)
- Automated: No manual script execution required
- Reusable: Base image downloaded once, cloned for each VM
- Secure: Private S3 bucket with IAM-based access control

## Architecture

```
S3 Bucket (ocpctl-binaries)
  └── windows-images/
      └── windows-10-oadp.qcow2 (23GB)
               ↓
      CDI DataVolume (imports)
               ↓
      PVC (70Gi) in openshift-virtualization-os-images namespace
               ↓
      DataSource (reference)
               ↓
      VM Template (clones for each VM)
```

## Authentication Methods

**Two methods are supported for S3 access:**

### 🔐 Method 1: IRSA (Recommended - More Secure)
IAM Roles for Service Accounts - no static credentials stored in cluster
- ✅ Temporary credentials that auto-rotate
- ✅ Credentials never stored in etcd
- ✅ Native AWS integration
- 📖 See [IRSA Setup Guide](#irsa-setup-recommended) below

### 🔑 Method 2: Static Credentials (Quick Start)
AWS IAM user access keys stored in a Kubernetes Secret
- ⚠️ Credentials stored in cluster (encrypted at rest)
- ⚠️ Manual rotation required
- ✅ Simpler initial setup
- 📖 See [Static Credentials Setup](#static-credentials-setup) below

## Prerequisites

1. **OpenShift Virtualization** installed on the cluster
2. **EBS CSI storage** with an `Immediate`-binding StorageClass available (script handles this automatically)
3. **Windows qcow2 image** uploaded to S3 (see admin setup below)
4. **AWS credentials** with S3 read access - either:
   - IRSA role (recommended) OR
   - Static IAM user credentials

## Admin Setup (One-Time)

### Step 1: Upload Windows Image to S3

The Windows 10 QCOW2 image must be uploaded to the `ocpctl-binaries` S3 bucket (workers already have access to this bucket).

```bash
# Upload the Windows qcow2 image
aws s3 cp ~/Downloads/windows-10-oadp.qcow2 \
  s3://ocpctl-binaries/windows-images/windows-10-oadp.qcow2 \
  --region us-east-1

# Verify upload
aws s3 ls s3://ocpctl-binaries/windows-images/ --region us-east-1
```

**Expected Output:**
```
2026-03-17 20:29:59 24508891136 windows-10-oadp.qcow2
```

### Step 2: Create IAM User with S3 Read Access

Create an IAM user specifically for CDI to download the Windows image:

```bash
# Create IAM user
aws iam create-user --user-name ocpctl-windows-image-reader

# Create and attach policy
aws iam put-user-policy \
  --user-name ocpctl-windows-image-reader \
  --policy-name S3WindowsImageReadOnly \
  --policy-document file://iam-policy.json

# Generate access keys
aws iam create-access-key --user-name ocpctl-windows-image-reader
```

**iam-policy.json:**
```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Action": [
        "s3:GetObject",
        "s3:ListBucket"
      ],
      "Resource": [
        "arn:aws:s3:::ocpctl-binaries/windows-images/*",
        "arn:aws:s3:::ocpctl-binaries"
      ]
    }
  ]
}
```

**Save the access keys** - you'll need them in the next step.

### Step 3: Configure AWS Credentials Secret

Edit `1_s3-credentials-secret.yaml` and replace the placeholder values:

```bash
# Edit the secret
vi 1_s3-credentials-secret.yaml

# Replace:
#   YOUR_AWS_ACCESS_KEY_ID → actual access key ID
#   YOUR_AWS_SECRET_ACCESS_KEY → actual secret access key
```

## Deployment Instructions

Apply the manifests in order:

```bash
# Ensure namespace exists
oc create namespace openshift-virtualization-os-images --dry-run=client -o yaml | oc apply -f -

# Apply credentials secret
oc apply -f 1_s3-credentials-secret.yaml

# Apply DataVolume using the setup script (handles StorageClass automatically)
./2_setup-storageclass.sh

# Apply remaining manifests
oc apply -f 3_datasource-windows.yaml
oc apply -f 4_windows10-template.yaml
```

> **Why use `2_setup-storageclass.sh` instead of `oc apply -f 2_windows-datavolume.yaml` directly?**
>
> CDI (Containerized Data Importer) creates two PVCs during import: the main image PVC and a scratch PVC.
> Both must land in the **same availability zone** or the importer pod will never be schedulable.
> The default `gp3-csi` StorageClass uses `WaitForFirstConsumer` binding, which additionally prevents
> CDI from starting at all. The script detects worker node AZs, finds or creates a zone-pinned
> `Immediate`-binding StorageClass (e.g. `gp3-csi-immediate-us-west-2c`), and applies the DataVolume
> with the correct class injected.

### Monitor DataVolume Import

The DataVolume will create an importer pod to download the Windows image from S3:

```bash
# Apply and watch in one step
./2_setup-storageclass.sh --watch

# Or watch separately
oc get datavolume windows -n openshift-virtualization-os-images -w

# Check importer pod logs
oc logs -f -n openshift-virtualization-os-images -l app=containerized-data-importer

# Check scratch disk usage (proxy for download progress)
oc exec -n openshift-virtualization-os-images \
  $(oc get pods -n openshift-virtualization-os-images -o name | grep importer) \
  -- df -h /scratch

# Expected phases:
# 1. PendingPopulation → PVCs being provisioned
# 2. ImportScheduled   → Importer pod scheduled
# 3. ImportInProgress  → Downloading from S3 (TransferScratch phase, 5-10 min)
# 4. Succeeded         → Ready to use
```

### Verify DataSource

```bash
# Check DataSource status
oc get datasource windows -n openshift-virtualization-os-images

# Should show:
# NAME      AGE
# windows   5m
```

## Creating Windows VMs

Once the DataVolume import completes, you can create Windows VMs from the template:

```bash
# Create a VM named "windows-vm1" in the "default" namespace
oc process windows10-template \
  -n openshift-virtualization-os-images \
  -p VM_NAME=windows-vm1 \
  -p VM_NAMESPACE=default \
  | oc apply -f -

# Start the VM
oc patch vm windows-vm1 -n default --type merge -p '{"spec":{"running":true}}'

# Get VM console URL
virtctl console windows-vm1 -n default
```

## Manifest Files

| File | Purpose |
|------|---------|
| `1_s3-credentials-secret.yaml` | AWS credentials for CDI to access S3 |
| `2_windows-datavolume.yaml` | DataVolume spec (applied via `2_setup-storageclass.sh`) |
| `2_setup-storageclass.sh` | Detects/creates a zone-pinned Immediate StorageClass and applies the DataVolume |
| `3_datasource-windows.yaml` | Reusable reference to the Windows image |
| `4_windows10-template.yaml` | VM template for creating Windows instances |
| `5_launch_vm.sh` | Helper script to create and start a Windows VM from the template |

## Troubleshooting

### DataVolume stuck in "PendingPopulation" or importer pod unschedulable

This is the most common failure mode and is caused by a StorageClass incompatibility.

**Root cause:** The default `gp3-csi` StorageClass uses `WaitForFirstConsumer` binding, which prevents
CDI from provisioning PVCs before a pod is scheduled — a chicken-and-egg problem. Even with an
`Immediate`-binding class, if the main PVC and scratch PVC land in different AZs, the importer pod
will report `0/N nodes available` because no single node can access both volumes.

**Fix:** Use `2_setup-storageclass.sh` instead of applying the DataVolume manually:

```bash
# Auto-detects worker AZs, creates a zone-pinned Immediate StorageClass if needed,
# and applies the DataVolume with the correct class
./2_setup-storageclass.sh

# To preview what it will do without applying:
./2_setup-storageclass.sh --dry-run

# To force a specific AZ:
./2_setup-storageclass.sh --zone us-west-2c
```

**Diagnosing manually:**

```bash
# Check importer pod scheduling events
oc describe pod -n openshift-virtualization-os-images \
  $(oc get pods -n openshift-virtualization-os-images -o name | grep importer)

# Check which AZ each PVC was provisioned in
oc get pvc -n openshift-virtualization-os-images -o json | python3 -c "
import json,sys
pvcs = json.load(sys.stdin)
for p in pvcs['items']:
    print(p['metadata']['name'], '->', p.get('spec',{}).get('storageClassName','?'))
"

# Check available StorageClasses and their binding modes
oc get sc
```

**Other common issues:**
- Incorrect AWS credentials → verify secret values
- Network connectivity → check cluster egress

### S3 Access Denied

```bash
# Verify IAM policy
aws iam get-user-policy \
  --user-name ocpctl-windows-image-reader \
  --policy-name S3WindowsImageReadOnly

# Test credentials manually
AWS_ACCESS_KEY_ID=xxx AWS_SECRET_ACCESS_KEY=yyy \
  aws s3 ls s3://ocpctl-binaries/windows-images/
```

### VM Cloning Failed

```bash
# Check DataSource exists
oc get datasource windows -n openshift-virtualization-os-images

# Verify base PVC is Ready
oc get pvc windows -n openshift-virtualization-os-images
```

## Cost Considerations

**Monthly cost estimate for 10 clusters:**

| Component | Cost |
|-----------|------|
| S3 Storage (23GB) | $0.53/month |
| S3 GET requests (10 downloads) | $0.00 |
| Data Transfer Out (230GB) | $20.70/month |
| **Total** | **~$21.23/month** |

**Note:** Costs are significantly lower than the original estimate ($64.61) because:
- The base image is downloaded once per cluster (not per VM)
- VM cloning is done within the cluster (no S3 egress)

## IRSA Setup (Recommended)

**IRSA (IAM Roles for Service Accounts)** is the recommended approach for production environments. It eliminates static credentials entirely.

### How IRSA Works

```
CDI Importer Pod → Uses ServiceAccount →
OIDC Federation → Assumes IAM Role →
Gets Temporary AWS Credentials → Downloads from S3
```

### Automated Setup Script

We provide a script that automates the entire IRSA setup:

```bash
# Step 1: Get your cluster's infraID and region
./get-cluster-info.sh

# Output example:
# Infrastructure ID: sandersvirt6-abc123
# Region: us-east-1
#
# To setup IRSA, run:
#   ./setup-irsa.sh sandersvirt6-abc123 us-east-1

# Step 2: Run the IRSA setup script
./setup-irsa.sh sandersvirt6-abc123 us-east-1

# This script will:
# ✓ Verify cluster's OIDC provider exists
# ✓ Create IAM role with S3 read-only permissions
# ✓ Configure trust policy for your cluster
# ✓ Generate ServiceAccount manifest (1a_windows-image-serviceaccount.yaml)
# ✓ Generate IRSA-enabled DataVolume manifest (2_windows-datavolume-irsa.yaml)
```

### Apply IRSA Manifests

After running the setup script:

```bash
# Create namespace
oc create namespace openshift-virtualization-os-images

# Apply the generated manifests
oc apply -f 1a_windows-image-serviceaccount.yaml  # ServiceAccount with IAM role
oc apply -f 2_windows-datavolume-irsa.yaml        # DataVolume using IRSA
oc apply -f 3_datasource-windows.yaml             # DataSource
oc apply -f 4_windows10-template.yaml             # VM Template
```

### IRSA Benefits

✅ **No Static Credentials**
- No AWS keys stored in cluster
- Nothing stored in etcd
- No credential rotation needed

✅ **Enhanced Security**
- Temporary credentials (15-minute lifetime)
- Automatic rotation by AWS STS
- Audit trail via CloudTrail

✅ **Fine-Grained Permissions**
- Role scoped to specific ServiceAccount
- Can't be used outside the cluster
- Limited to specific S3 prefix

### Verification

```bash
# Check ServiceAccount
oc get sa windows-image-importer -n openshift-virtualization-os-images

# Check IAM role annotation
oc get sa windows-image-importer -n openshift-virtualization-os-images \
  -o jsonpath='{.metadata.annotations.eks\.amazonaws\.com/role-arn}'

# Monitor DataVolume import
oc get datavolume windows -n openshift-virtualization-os-images -w
```

### Troubleshooting IRSA

**Issue: "Access Denied" errors**
```bash
# Verify OIDC provider exists
aws iam get-open-id-connect-provider \
  --open-id-connect-provider-arn arn:aws:iam::ACCOUNT_ID:oidc-provider/INFRA_ID-oidc.s3.REGION.amazonaws.com

# Check IAM role trust policy
aws iam get-role --role-name ocpctl-windows-image-s3-reader

# Verify role has S3 policy attached
aws iam get-role-policy \
  --role-name ocpctl-windows-image-s3-reader \
  --policy-name S3WindowsImageReadOnly
```

**Issue: Pod not using ServiceAccount**
```bash
# Check importer pod has correct ServiceAccount
oc get pods -n openshift-virtualization-os-images \
  -o jsonpath='{.items[*].spec.serviceAccountName}'

# Should output: windows-image-importer
```

---

## Static Credentials Setup

If you prefer to use static credentials (not recommended for production):

## References

- [Issue #20: Automated Windows VM Deployment](https://github.com/tsanders-rh/ocpctl/issues/20)
- [CDI DataVolume Documentation](https://github.com/kubevirt/containerized-data-importer/blob/main/doc/datavolumes.md)
- [OpenShift Virtualization Documentation](https://docs.openshift.com/container-platform/latest/virt/about-virt.html)
