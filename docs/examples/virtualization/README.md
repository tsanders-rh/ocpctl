# OpenShift Virtualization - Windows VM Setup Guide

This guide covers setting up Windows virtual machines on OpenShift Virtualization clusters provisioned with the `aws-virt-windows-minimal` profile.

## Table of Contents

- [Overview](#overview)
- [Prerequisites](#prerequisites)
- [Step 1: Install OpenShift Virtualization Operator](#step-1-install-openshift-virtualization-operator)
- [Step 2: Create HyperConverged Instance](#step-2-create-hyperconverged-instance)
- [Step 3: Upload Windows ISO](#step-3-upload-windows-iso)
- [Step 4: Create Windows VM](#step-4-create-windows-vm)
- [Step 5: Access Windows VM](#step-5-access-windows-vm)
- [Troubleshooting](#troubleshooting)
- [Cost Optimization](#cost-optimization)

## Overview

The `aws-virt-windows-minimal` profile provides:
- **1x m5zn.metal worker** (48 vCPU, 192GB RAM) for nested virtualization
- **500GB EBS storage** for Windows VM images
- **Support for 10-15 concurrent Windows VMs**
- **Cost**: $4.54/hr (~$835/month with work hours hibernation)

## Prerequisites

Before starting, ensure you have:

1. **Cluster provisioned** with `aws-virt-windows-minimal` profile
2. **OpenShift CLI (oc)** installed locally
3. **virtctl** installed (kubectl plugin for VMs)
4. **Cluster admin access**
5. **Windows ISO files**:
   - Windows 10/11 or Server 2019/2022 ISO
   - [virtio-win drivers ISO](https://github.com/virtio-win/virtio-win-pkg-scripts/blob/master/README.md)

### Install virtctl

```bash
# Download virtctl
VERSION=v1.0.0  # Check latest version
curl -L -o virtctl https://github.com/kubevirt/kubevirt/releases/download/${VERSION}/virtctl-${VERSION}-linux-amd64
chmod +x virtctl
sudo mv virtctl /usr/local/bin/
```

## Step 1: Install OpenShift Virtualization Operator

### Via OpenShift Console

1. Navigate to **Operators → OperatorHub**
2. Search for **"OpenShift Virtualization"**
3. Click **Install**
4. Select **stable** channel
5. Choose installation mode: **All namespaces on the cluster**
6. Click **Install**

### Via CLI

```bash
# Create openshift-cnv namespace
oc create namespace openshift-cnv

# Create OperatorGroup
cat <<EOF | oc apply -f -
apiVersion: operators.coreos.com/v1
kind: OperatorGroup
metadata:
  name: kubevirt-hyperconverged-group
  namespace: openshift-cnv
spec:
  targetNamespaces:
    - openshift-cnv
EOF

# Create Subscription
cat <<EOF | oc apply -f -
apiVersion: operators.coreos.com/v1alpha1
kind: Subscription
metadata:
  name: hco-operatorhub
  namespace: openshift-cnv
spec:
  source: redhat-operators
  sourceNamespace: openshift-marketplace
  name: kubevirt-hyperconverged
  channel: "stable"
EOF

# Wait for operator to be ready
oc wait --for=condition=ready pod -l name=hyperconverged-cluster-operator -n openshift-cnv --timeout=5m
```

## Step 2: Create HyperConverged Instance

The HyperConverged CR deploys all OpenShift Virtualization components.

```bash
cat <<EOF | oc apply -f -
apiVersion: hco.kubevirt.io/v1beta1
kind: HyperConverged
metadata:
  name: kubevirt-hyperconverged
  namespace: openshift-cnv
spec: {}
EOF

# Wait for HyperConverged to be ready (may take 5-10 minutes)
oc wait --for=condition=Available hyperconverged kubevirt-hyperconverged -n openshift-cnv --timeout=15m
```

Verify installation:

```bash
# Check all components are running
oc get pods -n openshift-cnv

# Verify kubevirt is ready
oc get kubevirt -n openshift-cnv
```

## Step 3: Upload Windows ISO

### Option A: Upload via virtctl (Recommended)

```bash
# Create PVC for Windows 10 ISO (adjust size as needed)
cat <<EOF | oc apply -f -
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: windows-10-iso
  namespace: default
spec:
  accessModes:
    - ReadWriteOnce
  resources:
    requests:
      storage: 6Gi
  storageClassName: gp3
EOF

# Upload ISO file
virtctl image-upload \
  pvc windows-10-iso \
  --size=6Gi \
  --image-path=/path/to/windows10.iso \
  --uploadproxy-url=https://$(oc get route -n openshift-cnv cdi-uploadproxy -o jsonpath='{.spec.host}') \
  --insecure

# Upload virtio-win drivers
cat <<EOF | oc apply -f -
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: virtio-win-iso
  namespace: default
spec:
  accessModes:
    - ReadWriteOnce
  resources:
    requests:
      storage: 500Mi
  storageClassName: gp3
EOF

virtctl image-upload \
  pvc virtio-win-iso \
  --size=500Mi \
  --image-path=/path/to/virtio-win.iso \
  --uploadproxy-url=https://$(oc get route -n openshift-cnv cdi-uploadproxy -o jsonpath='{.spec.host}') \
  --insecure
```

### Option B: Upload via HTTP

```bash
# Create DataVolume with HTTP source
cat <<EOF | oc apply -f -
apiVersion: cdi.kubevirt.io/v1beta1
kind: DataVolume
metadata:
  name: windows-10-iso
  namespace: default
spec:
  source:
    http:
      url: "https://your-server.com/windows10.iso"
  storage:
    accessModes:
      - ReadWriteOnce
    resources:
      requests:
        storage: 6Gi
    storageClassName: gp3
EOF
```

## Step 4: Create Windows VM

### Create Windows 10 VM

```bash
# Apply the Windows 10 VM template
oc apply -f windows-10-vm.yaml

# Start the VM
virtctl start windows-10-test

# Watch VM status
oc get vmi -w
```

### Create Windows Server 2022 VM

```bash
# Apply the Windows Server template
oc apply -f windows-server-2022-vm.yaml

# Start the VM
virtctl start windows-server-2022
```

## Step 5: Access Windows VM

### Access via VNC Console

```bash
# Open VNC console
virtctl vnc windows-10-test
```

This opens a VNC viewer to the VM console where you can complete Windows installation.

### Access via OpenShift Console

1. Navigate to **Virtualization → VirtualMachines**
2. Click on your VM
3. Click **Console** tab
4. Select **VNC Console**

### Complete Windows Installation

1. **Boot from ISO**: The VM will boot from the Windows ISO
2. **Load virtio drivers**:
   - During installation, click "Load driver"
   - Browse to the virtio-drivers CD
   - Select the appropriate driver for your Windows version
   - Install Red Hat VirtIO SCSI controller driver
3. **Complete Windows installation**:
   - Follow the standard Windows installation wizard
   - Create user account
   - Configure network settings

### Install virtio Drivers Post-Installation

After Windows boots:

1. Open Device Manager
2. Update drivers for unknown devices
3. Point to the virtio-win CD drive
4. Install:
   - Network adapter driver (virtio-net)
   - Balloon driver (virtio-balloon)
   - Serial driver (virtio-serial)
   - RNG driver (virtio-rng)

## Troubleshooting

### VM Won't Start

Check VM events:
```bash
oc describe vmi windows-10-test
```

Common issues:
- **Insufficient resources**: Check worker node capacity
- **ISO not found**: Verify PVCs are bound
- **Image pull errors**: Check CDI pods are running

### VM Performance Issues

```bash
# Check worker node resources
oc describe node <metal-worker-node>

# Check VM resource allocation
oc get vmi windows-10-test -o yaml | grep -A 10 resources
```

### Can't Access VM Console

```bash
# Check virtctl version
virtctl version

# Verify VM is running
oc get vmi

# Check for network issues
oc get svc -n openshift-cnv | grep virt-vnc
```

### Windows Installation Hangs

- **Common cause**: virtio drivers not loaded
- **Solution**: Use "Load driver" during installation and select virtio-scsi driver

## Cost Optimization

### Work Hours Hibernation

The metal worker costs $3.96/hr. With work hours hibernation:

**Always-on**: $3,269/month
**Work hours (8am-6pm, Mon-Fri)**: $835/month
**Savings**: $2,434/month (74%)

Configure work hours in your user profile or cluster settings.

### Right-Size VMs

Don't over-allocate resources:
- **Testing**: 2 vCPU, 4GB RAM
- **Development**: 4 vCPU, 8GB RAM
- **Performance testing**: 8 vCPU, 16GB RAM

### Delete Unused VMs

```bash
# List all VMs
oc get vm

# Delete VM and its disk
oc delete vm windows-10-test
oc delete pvc windows-10-disk
```

## Capacity Planning

### Single m5zn.metal Worker Limits

- **Total**: 48 vCPU, 192GB RAM
- **OpenShift overhead**: ~4-8 vCPU, ~20GB RAM
- **Available for VMs**: ~40 vCPU, ~170GB RAM

### Recommended VM Allocations

| VM Size | vCPU | RAM | Max Concurrent VMs |
|---------|------|-----|-------------------|
| Small   | 2    | 4GB | 20                |
| Medium  | 4    | 8GB | 10-12             |
| Large   | 8    | 16GB| 5-6               |

### Storage Planning

- **Windows 10/11**: 40-50GB per VM
- **Windows Server**: 50-80GB per VM
- **500GB total storage** = ~10 Windows VMs

## Next Steps

- [Configure networking for external access](./networking.md)
- [Set up automated VM templates](./templates.md)
- [Integrate with CI/CD pipelines](./cicd.md)
- [Monitoring and metrics](./monitoring.md)

## References

- [OpenShift Virtualization Documentation](https://docs.openshift.com/container-platform/latest/virt/about_virt/about-virt.html)
- [KubeVirt User Guide](https://kubevirt.io/user-guide/)
- [VirtIO Windows Drivers](https://github.com/virtio-win/virtio-win-pkg-scripts)
- [Windows on OpenShift Virtualization](https://access.redhat.com/articles/6994974)
