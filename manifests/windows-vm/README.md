# Windows VM Deployment

Deploy a Windows 10 VM on OpenShift Virtualization in ~10 minutes. The Windows image is
downloaded once from S3 and stored as a PVC; each VM is a fast in-cluster clone.

## Prerequisites

- OpenShift Virtualization operator installed and running
- `oc` and `virtctl` CLIs logged into the cluster
- AWS credentials with read access to `s3://ocpctl-binaries/windows-images/`

---

## Quickstart

### Step 1 — Create the S3 credentials secret

```bash
./create-s3-secret.sh
```

Prompts for your AWS Access Key ID and Secret Access Key, verifies them against AWS,
and creates the `windows-image-s3-creds` secret in `openshift-virtualization-os-images`.

### Step 2 — Import the Windows image

```bash
./2_setup-storageclass.sh --watch
```

This script handles everything needed to start the CDI import:
- Detects worker node availability zones
- Creates a zone-pinned `Immediate`-binding StorageClass if one does not exist
- Applies `2_windows-datavolume.yaml` with the correct StorageClass injected
- Watches progress until the import completes (~5-10 minutes)

You will see the DataVolume move through these phases:

```
PendingPopulation → ImportScheduled → ImportInProgress → Succeeded
```

### Step 3 — Apply the DataSource and VM template

```bash
oc apply -f 3_datasource-windows.yaml
oc apply -f 4_windows10-template.yaml
```

### Step 4 — Launch a Windows VM

```bash
./5_launch_vm.sh windows-vm1
```

Default namespace is `windows-oadp-test`. The script creates the VM, prints
the start and console commands, and exits. To start and connect:

```bash
virtctl start windows-vm1 -n windows-oadp-test
virtctl console windows-vm1 -n windows-oadp-test
```

---

## Files

| File | Purpose |
|------|---------|
| `create-s3-secret.sh` | Prompts for AWS credentials and creates the S3 secret |
| `2_windows-datavolume.yaml` | DataVolume spec (applied via `2_setup-storageclass.sh`) |
| `2_setup-storageclass.sh` | Ensures correct StorageClass and applies the DataVolume |
| `3_datasource-windows.yaml` | DataSource reference used for VM cloning |
| `4_windows10-template.yaml` | OpenShift VM template (4 vCPU, 8GB RAM) |
| `5_launch_vm.sh` | Creates a VM from the template |
| `get-cluster-info.sh` | Prints cluster infraID and region (used for IRSA setup) |
| `setup-irsa.sh` | Sets up IRSA authentication (alternative to static credentials) |
| `auto-setup-irsa.sh` | Fully automated IRSA setup for `aws-virt-windows-minimal` profile |

---

## Monitoring

```bash
# DataVolume phase and progress
oc get datavolume windows -n openshift-virtualization-os-images -w

# Importer pod logs
oc logs -f -n openshift-virtualization-os-images -l app=containerized-data-importer

# Scratch disk usage (proxy for download progress)
oc exec -n openshift-virtualization-os-images \
  $(oc get pods -n openshift-virtualization-os-images -o name | grep importer) \
  -- df -h /scratch

# All resources in the namespace
oc get datavolume,pvc,pods -n openshift-virtualization-os-images
```

---

## Troubleshooting

### DataVolume stuck in PendingPopulation / importer pod unschedulable

**Cause:** CDI creates two PVCs (main image + scratch). If they land in different
availability zones the importer pod will never schedule. The default `gp3-csi`
StorageClass uses `WaitForFirstConsumer` binding, which also prevents CDI from
starting at all.

**Fix:** `2_setup-storageclass.sh` handles this automatically. If you applied
the DataVolume manually, delete it and re-run the script:

```bash
oc delete datavolume windows -n openshift-virtualization-os-images
./2_setup-storageclass.sh --watch

# Preview without applying:
./2_setup-storageclass.sh --dry-run

# Force a specific AZ:
./2_setup-storageclass.sh --zone us-west-2c
```

Diagnose manually:

```bash
# Scheduling events on the importer pod
oc describe pod -n openshift-virtualization-os-images \
  $(oc get pods -n openshift-virtualization-os-images -o name | grep importer)

# Which AZ each PVC landed in
oc get pvc -n openshift-virtualization-os-images \
  -o custom-columns='NAME:.metadata.name,SC:.spec.storageClassName,STATUS:.status.phase'

# Available StorageClasses and binding modes
oc get sc
```

### S3 access denied

```bash
# Test credentials directly
AWS_ACCESS_KEY_ID=xxx AWS_SECRET_ACCESS_KEY=yyy \
  aws s3 ls s3://ocpctl-binaries/windows-images/

# Verify the IAM policy attached to the user
aws iam get-user-policy \
  --user-name ocpctl-windows-image-reader \
  --policy-name S3WindowsImageReadOnly
```

### VM fails to start / DataSource not ready

```bash
# Confirm the DataVolume succeeded
oc get datavolume windows -n openshift-virtualization-os-images

# Confirm the DataSource is present
oc get datasource windows -n openshift-virtualization-os-images

# Confirm the base PVC is Bound
oc get pvc windows -n openshift-virtualization-os-images
```

---

## Reference

### Architecture

```
S3 Bucket (ocpctl-binaries)
  └── windows-images/windows-10-oadp.qcow2  (23GB)
           ↓  CDI import (~5-10 min)
      PVC: windows (70Gi)  in openshift-virtualization-os-images
           ↓
      DataSource: windows
           ↓  in-cluster clone per VM
      VM Disk PVC
```

### 5_launch_vm.sh options

```bash
./5_launch_vm.sh                               # auto-generated name, windows-oadp-test namespace
./5_launch_vm.sh windows-vm1                   # named VM, windows-oadp-test namespace
./5_launch_vm.sh windows-vm1 my-namespace      # named VM, custom namespace
./5_launch_vm.sh windows-vm1 my-ns gp3-csi    # named VM, custom namespace and storage class
```

### 2_setup-storageclass.sh options

```bash
./2_setup-storageclass.sh                      # auto-detect AZ, apply DataVolume
./2_setup-storageclass.sh --watch              # apply and watch until Succeeded/Failed
./2_setup-storageclass.sh --dry-run            # print manifest, make no changes
./2_setup-storageclass.sh --zone us-west-2c    # pin to a specific AZ
./2_setup-storageclass.sh --sc my-sc           # use an existing StorageClass by name
```

### Manual oc process (without 5_launch_vm.sh)

```bash
oc process windows10-oadp-vm \
  -n openshift-virtualization-os-images \
  -p VM_NAME=windows-vm1 \
  -p VM_NAMESPACE=windows-oadp-test \
  | oc apply -f -

virtctl start windows-vm1 -n windows-oadp-test
virtctl console windows-vm1 -n windows-oadp-test
```

### IRSA authentication (alternative to static credentials)

IRSA (IAM Roles for Service Accounts) eliminates static credentials from the cluster.
Use it for production environments or when you don't want AWS keys stored in a Secret.

```bash
# 1. Get cluster info
./get-cluster-info.sh

# 2. Create the IAM role and generate manifests
./setup-irsa.sh <infraID> <region>

# 3. Apply generated manifests
oc apply -f 1a_windows-image-serviceaccount.yaml
./2_setup-storageclass.sh --watch   # handles StorageClass + DataVolume
oc apply -f 3_datasource-windows.yaml
oc apply -f 4_windows10-template.yaml

# 4. Verify IRSA is active
oc get sa windows-image-importer -n openshift-virtualization-os-images 2>/dev/null \
  && echo "IRSA active" \
  || echo "Not using IRSA (static credentials path)"
```

IRSA troubleshooting:

```bash
# Verify OIDC provider
aws iam get-open-id-connect-provider \
  --open-id-connect-provider-arn \
  arn:aws:iam::ACCOUNT_ID:oidc-provider/INFRA_ID-oidc.s3.REGION.amazonaws.com

# Verify IAM role and trust policy
aws iam get-role --role-name ocpctl-windows-image-s3-reader
aws iam get-role-policy \
  --role-name ocpctl-windows-image-s3-reader \
  --policy-name S3WindowsImageReadOnly
```

### One-time admin setup: upload image to S3

Only needed if the image is not already in the bucket.

```bash
aws s3 cp ~/Downloads/windows-10-oadp.qcow2 \
  s3://ocpctl-binaries/windows-images/windows-10-oadp.qcow2 \
  --region us-east-1

aws s3 ls s3://ocpctl-binaries/windows-images/ --region us-east-1
```

### One-time admin setup: create IAM user for static credentials

```bash
aws iam create-user --user-name ocpctl-windows-image-reader

aws iam put-user-policy \
  --user-name ocpctl-windows-image-reader \
  --policy-name S3WindowsImageReadOnly \
  --policy-document file://iam-policy.json

aws iam create-access-key --user-name ocpctl-windows-image-reader
```

`iam-policy.json` grants read-only access to the Windows images prefix:

```json
{
  "Version": "2012-10-17",
  "Statement": [{
    "Effect": "Allow",
    "Action": ["s3:GetObject", "s3:ListBucket"],
    "Resource": [
      "arn:aws:s3:::ocpctl-binaries/windows-images/*",
      "arn:aws:s3:::ocpctl-binaries"
    ]
  }]
}
```

### Cost estimate (10 clusters)

| Component | Cost |
|-----------|------|
| S3 storage (23GB) | $0.53/month |
| Data transfer per cluster (23GB × $0.09) | $2.07 one-time |
| **Per-cluster total** | **~$2.60** |

The base image is downloaded once per cluster; VM clones are in-cluster and incur no S3 egress.

---

## Links

- [Issue #20: Automated Windows VM Deployment](https://github.com/tsanders-rh/ocpctl/issues/20)
- [CDI DataVolume Documentation](https://github.com/kubevirt/containerized-data-importer/blob/main/doc/datavolumes.md)
- [OpenShift Virtualization Documentation](https://docs.openshift.com/container-platform/latest/virt/about-virt.html)
- [QUICKSTART-IRSA.md](QUICKSTART-IRSA.md) — detailed IRSA setup walkthrough
- [AUTOMATED-DEPLOYMENT.md](AUTOMATED-DEPLOYMENT.md) — fully automated profile deployment
