# Automated Windows VM Deployment

## Overview

The `aws-virt-windows-minimal` profile now provides **fully automated Windows VM deployment** with zero manual steps required.

## What's Automated

When you create a cluster with the `aws-virt-windows-minimal` profile, the following happens automatically:

### 1. Cluster Provisioning (15-20 minutes)
- 3 control plane nodes (m6i.xlarge)
- 1 bare metal worker (m5zn.metal for nested virtualization)
- 500GB root volume for Windows images

### 2. Post-Deployment (Automatic)

#### OpenShift Virtualization Operator
- Installs `kubevirt-hyperconverged` operator
- Creates HyperConverged custom resource
- Enables nested virtualization support

#### Windows Image Infrastructure (IRSA)
The `auto-setup-irsa.sh` script runs automatically and:

1. **Creates Per-Cluster IAM Role**
   ```
   Role: ocpctl-windows-image-s3-reader-<CLUSTER_ID>
   Trust: Cluster's OIDC provider
   Permissions: S3 read-only for windows-images/*
   ```

2. **Applies Kubernetes Resources**
   - Namespace: `openshift-virtualization-os-images`
   - ServiceAccount: `windows-image-importer` (with IAM role annotation)
   - DataVolume: `windows` (downloads from S3)
   - DataSource: `windows` (for VM cloning)
   - Template: `windows10-oadp-vm` (for creating VMs)

3. **Starts Windows Image Download**
   - Source: `s3://ocpctl-binaries/windows-images/windows-10-oadp.qcow2`
   - Size: 23GB
   - Duration: 5-10 minutes
   - Security: IRSA (no credentials stored)

### 3. Ready to Use! (25-35 minutes total)

Once the cluster is ready, Windows VMs can be created instantly.

## Usage

### Create a Cluster

```bash
# Using ocpctl CLI or web UI
# Select profile: aws-virt-windows-minimal
# Wait for cluster to be ready (~25-35 minutes)
```

### Monitor Progress

```bash
# Check cluster status
oc get clusters

# Check post-deployment status
oc get clusterconfigurations

# Monitor Windows image download
oc get datavolume windows -n openshift-virtualization-os-images -w
```

### Create Windows VMs

Once the cluster is ready and the DataVolume shows "Succeeded":

```bash
# Recommended: use the launch script
./5_launch_vm.sh my-windows-vm

# Or manually:
oc process windows10-oadp-vm \
  -n openshift-virtualization-os-images \
  -p VM_NAME=my-windows-vm \
  -p VM_NAMESPACE=windows-oadp-test \
  | oc apply -f -

# Start the VM
virtctl start my-windows-vm -n windows-oadp-test

# Connect to the VM console
virtctl console my-windows-vm -n windows-oadp-test
```

## What Gets Created

### AWS Resources
- **IAM Role**: `ocpctl-windows-image-s3-reader-<CLUSTER_ID>`
  - Trust policy for cluster OIDC provider
  - S3 read-only permissions
  - Tagged with cluster metadata

### Kubernetes Resources
- **Namespace**: `openshift-virtualization-os-images`
- **ServiceAccount**: `windows-image-importer`
  - Annotation: `eks.amazonaws.com/role-arn: <IAM_ROLE_ARN>`
- **DataVolume**: `windows`
  - Downloads Windows 10 image from S3
  - 70Gi PVC (23GB actual image size)
- **DataSource**: `windows`
  - Reference for cloning VMs
- **Template**: `windows10-oadp-vm`
  - Pre-configured Windows 10 VM template
  - 4 vCPUs, 8GB RAM

## Security

### IRSA (IAM Roles for Service Accounts)
- ✅ No static credentials stored in cluster
- ✅ Temporary credentials (15-min lifetime)
- ✅ Auto-rotating via AWS STS
- ✅ Per-cluster IAM role isolation
- ✅ OIDC trust scoped to specific ServiceAccount
- ✅ Full CloudTrail audit trail

### Permissions
The IAM role has minimal permissions:
```json
{
  "Action": ["s3:GetObject", "s3:ListBucket"],
  "Resource": [
    "arn:aws:s3:::ocpctl-binaries/windows-images/*",
    "arn:aws:s3:::ocpctl-binaries"
  ]
}
```

### Isolation
- Each cluster gets a unique IAM role
- Role can only be assumed from that cluster's OIDC provider
- ServiceAccount-scoped (cannot be used by other pods)

## Troubleshooting

### DataVolume Stuck in Pending or Importer Pod Unschedulable

The most common cause is a StorageClass incompatibility: CDI needs an `Immediate`-binding StorageClass
and both the main PVC and scratch PVC must land in the same AZ. Use `2_setup-storageclass.sh` to fix:

```bash
# Auto-detect and apply the correct StorageClass, then re-apply the DataVolume
cd manifests/windows-vm
./2_setup-storageclass.sh --watch

# Preview what it will do without changing anything
./2_setup-storageclass.sh --dry-run
```

For deeper inspection:

```bash
# Check importer pod scheduling events
oc describe pod -n openshift-virtualization-os-images \
  $(oc get pods -n openshift-virtualization-os-images -o name | grep importer)

# Check pod logs
oc logs -f -n openshift-virtualization-os-images -l app=containerized-data-importer

# Check events
oc get events -n openshift-virtualization-os-images --sort-by='.lastTimestamp'
```

### IAM Role Issues

```bash
# Verify IAM role exists
CLUSTER_ID=$(oc get infrastructure cluster -o jsonpath='{.status.infrastructureName}' | cut -d'-' -f1-2)
aws iam get-role --role-name ocpctl-windows-image-s3-reader-${CLUSTER_ID}

# Check ServiceAccount annotation
oc get sa windows-image-importer -n openshift-virtualization-os-images \
  -o jsonpath='{.metadata.annotations.eks\.amazonaws\.com/role-arn}'
```

### S3 Access Denied

```bash
# Verify OIDC provider
INFRA_ID=$(oc get infrastructure cluster -o jsonpath='{.status.infrastructureName}')
REGION=$(oc get infrastructure cluster -o jsonpath='{.status.platformStatus.aws.region}')
AWS_ACCOUNT_ID=$(aws sts get-caller-identity --query Account --output text)

aws iam get-open-id-connect-provider \
  --open-id-connect-provider-arn \
  "arn:aws:iam::${AWS_ACCOUNT_ID}:oidc-provider/${INFRA_ID}-oidc.s3.${REGION}.amazonaws.com"
```

## Cost Considerations

### Cluster Costs
- Control plane: 3 × m6i.xlarge = ~$0.75/hr
- Worker: 1 × m5zn.metal = ~$4.54/hr
- **Total: ~$5.29/hr or $3,808/month (if always on)**

### Cost Savings
- Enable work-hours hibernation: ~$835/month
- Use for testing only: destroy when not needed

### S3 Costs (Per Cluster)
- Storage: 23GB × $0.023/GB = $0.53/month
- Data transfer: 1 download × 23GB × $0.09/GB = $2.07 one-time
- **Per-cluster cost: ~$2.60 total**

## Comparison

### Manual Setup (Old)
1. Create cluster (15-20 min)
2. Login to cluster
3. Run get-cluster-info.sh
4. Run setup-irsa.sh with parameters
5. Apply ServiceAccount manifest
6. Apply DataVolume manifest
7. Apply DataSource manifest
8. Apply Template manifest
9. Wait for import (5-10 min)
10. Create VMs

**Total: 8 manual steps, ~40+ minutes**

### Automated (New)
1. Create cluster with aws-virt-windows-minimal profile (25-35 min)
2. Create VMs

**Total: 2 steps, ~25-35 minutes (fully automated!)**

## Manual IRSA Setup (Other Clusters)

If you want to use Windows VMs on clusters **not** created with the `aws-virt-windows-minimal` profile:

```bash
cd manifests/windows-vm
oc login <your-cluster>
./get-cluster-info.sh
./setup-irsa.sh <infraID> <region>
oc apply -f 1a_windows-image-serviceaccount.yaml
./2_setup-storageclass.sh   # handles StorageClass + DataVolume apply
oc apply -f 3_datasource-windows.yaml
oc apply -f 4_windows10-template.yaml
```

See [QUICKSTART-IRSA.md](QUICKSTART-IRSA.md) for detailed manual setup instructions.

## References

- [Issue #20: Automated Windows VM Deployment](https://github.com/tsanders-rh/ocpctl/issues/20)
- [QUICKSTART-IRSA.md](QUICKSTART-IRSA.md) - Manual IRSA setup
- [README.md](README.md) - Complete documentation
- [OpenShift Virtualization Docs](https://docs.openshift.com/container-platform/latest/virt/about-virt.html)
- [AWS IRSA Documentation](https://docs.aws.amazon.com/eks/latest/userguide/iam-roles-for-service-accounts.html)
