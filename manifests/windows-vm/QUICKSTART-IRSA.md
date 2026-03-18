# Quick Start: IRSA Setup for Windows VM

This guide shows you how to set up IRSA (IAM Roles for Service Accounts) for secure, credential-free Windows image access.

## ✅ Prerequisites Completed

- ✅ Windows image uploaded to S3: `s3://ocpctl-binaries/windows-images/windows-10-oadp.qcow2`
- ✅ Static IAM user created (for fallback): `ocpctl-windows-image-reader`
- ✅ IRSA setup scripts created and ready to use

## 🚀 Quick Start (3 Steps)

### Step 1: Get Cluster Information

```bash
cd manifests/windows-vm

# Login to your cluster first
oc login <your-cluster-url>

# Get cluster info
./get-cluster-info.sh
```

**Example Output:**
```
Cluster Information:
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
Cluster Name: sandersvirt6
Infrastructure ID: sandersvirt6-abc123
Region: us-east-1
OIDC Issuer: https://sandersvirt6-abc123-oidc.s3.us-east-1.amazonaws.com
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

To setup IRSA, run:
  ./setup-irsa.sh sandersvirt6-abc123 us-east-1
```

### Step 2: Run IRSA Setup

```bash
# Use the infraID and region from Step 1
./setup-irsa.sh sandersvirt6-abc123 us-east-1
```

**What This Does:**
1. ✓ Verifies your cluster's OIDC provider exists
2. ✓ Creates IAM role `ocpctl-windows-image-s3-reader`
3. ✓ Configures trust relationship with your cluster
4. ✓ Attaches S3 read-only policy
5. ✓ Generates `1a_windows-image-serviceaccount.yaml`
6. ✓ Generates `2_windows-datavolume-irsa.yaml`

### Step 3: Apply Manifests to Cluster

```bash
# Create namespace
oc create namespace openshift-virtualization-os-images

# Apply IRSA manifests (in order)
oc apply -f 1a_windows-image-serviceaccount.yaml  # ServiceAccount with IAM role
oc apply -f 2_windows-datavolume-irsa.yaml        # DataVolume using IRSA (no credentials!)
oc apply -f 3_datasource-windows.yaml             # DataSource for cloning
oc apply -f 4_windows10-template.yaml             # VM template

# Watch the import progress (5-10 minutes)
oc get datavolume windows -n openshift-virtualization-os-images -w
```

## 🎯 Verify It's Working

```bash
# Check ServiceAccount exists and has IAM role annotation
oc get sa windows-image-importer -n openshift-virtualization-os-images \
  -o jsonpath='{.metadata.annotations.eks\.amazonaws\.com/role-arn}'

# Should output something like:
# arn:aws:iam::346869059911:role/ocpctl-windows-image-s3-reader

# Monitor the import
oc get datavolume windows -n openshift-virtualization-os-images

# Check importer pod logs
oc logs -f $(oc get pods -n openshift-virtualization-os-images \
  -l cdi.kubevirt.io/dataVolume=windows -o name)
```

## 🎉 Success Indicators

When the DataVolume shows `Succeeded`:

```bash
NAME      PHASE      PROGRESS   RESTARTS   AGE
windows   Succeeded  100.0%     0          8m23s
```

You can now create Windows VMs instantly:

```bash
# Create a VM from the template
oc process windows10-template \
  -n openshift-virtualization-os-images \
  -p VM_NAME=my-windows-vm \
  -p VM_NAMESPACE=default \
  | oc apply -f -

# Start the VM
oc patch vm my-windows-vm -n default --type merge -p '{"spec":{"running":true}}'
```

## 🔒 Security Comparison

### ❌ Static Credentials (Old Way)
```yaml
# Credentials stored in Secret
accessKeyId: "AKIA..."
secretKey: "wJalrXUtnFEMI/K7MDENG/bPxRfiCY..."
```
- Stored in etcd
- Manual rotation required
- Long-lived credentials

### ✅ IRSA (New Way)
```yaml
# No credentials in cluster!
# Just an annotation:
annotations:
  eks.amazonaws.com/role-arn: arn:aws:iam::xxx:role/ocpctl-windows-image-s3-reader
```
- Nothing stored in etcd
- Auto-rotating temporary credentials
- 15-minute credential lifetime
- Can't be exfiltrated from cluster

## 🆘 Troubleshooting

### Issue: "Access Denied" when importing

```bash
# Check OIDC provider exists in AWS
INFRA_ID=$(oc get infrastructure cluster -o jsonpath='{.status.infrastructureName}')
REGION=$(oc get infrastructure cluster -o jsonpath='{.status.platformStatus.aws.region}')
AWS_ACCOUNT_ID=$(aws sts get-caller-identity --query Account --output text)

aws iam get-open-id-connect-provider \
  --open-id-connect-provider-arn \
  "arn:aws:iam::${AWS_ACCOUNT_ID}:oidc-provider/${INFRA_ID}-oidc.s3.${REGION}.amazonaws.com"
```

### Issue: Pod not using ServiceAccount

```bash
# Check CDI is configured to use ServiceAccount
oc get datavolume windows -n openshift-virtualization-os-images -o yaml | grep serviceAccountName

# Should show:
#   serviceAccountName: windows-image-importer
```

### Fallback to Static Credentials

If IRSA isn't working, you can fall back to static credentials:

```bash
# Use the original manifest with static credentials
oc apply -f 1_s3-credentials-secret.yaml
oc apply -f 2_windows-datavolume.yaml  # Uses secretRef instead of ServiceAccount
```

## 📋 Files Created

After running the IRSA setup:

```
manifests/windows-vm/
├── 1_s3-credentials-secret.yaml          # Static credentials (fallback)
├── 1a_windows-image-serviceaccount.yaml  # IRSA ServiceAccount (generated)
├── 2_windows-datavolume.yaml             # Static credentials version
├── 2_windows-datavolume-irsa.yaml        # IRSA version (generated)
├── 3_datasource-windows.yaml             # Same for both methods
├── 4_windows10-template.yaml             # Same for both methods
├── get-cluster-info.sh                   # Helper script
├── setup-irsa.sh                         # IRSA setup automation
├── iam-policy.json                       # S3 read-only policy
└── README.md                             # Full documentation
```

## 🎓 Learn More

- Full documentation: [README.md](README.md)
- Issue #20: https://github.com/tsanders-rh/ocpctl/issues/20
- OpenShift IRSA Docs: https://docs.openshift.com/container-platform/latest/authentication/managing_cloud_provider_credentials/cco-mode-sts.html
