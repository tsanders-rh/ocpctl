# Windows VM Management Script

Bulk management tool for creating and managing Windows 10 VMs on OpenShift Virtualization (CNV/KubeVirt) clusters.

## Overview

This script automates the deployment and lifecycle management of 5 Windows 10 test VMs, useful for:
- OADP (OpenShift API for Data Protection) backup/restore testing
- Migration Toolkit for Containers (MTC) testing
- OpenShift Virtualization feature validation
- Multi-namespace disaster recovery scenarios

## Prerequisites

### Required

1. **OpenShift cluster with CNV installed**
   - OpenShift Virtualization operator deployed
   - Sufficient resources (see [Resource Requirements](#resource-requirements))

2. **Windows 10 VM Template**
   - Template name: `windows10-oadp-vm`
   - Namespace: `openshift-virtualization-os-images`
   - Created automatically by ocpctl CNV addon with Windows support

3. **oc CLI** with cluster access
   ```bash
   oc login https://api.your-cluster.com:6443
   oc whoami
   ```

4. **jq** (for JSON parsing in status command)
   ```bash
   # macOS
   brew install jq

   # RHEL/CentOS
   sudo yum install jq
   ```

### Verify Prerequisites

```bash
# Check if template exists
oc get template windows10-oadp-vm -n openshift-virtualization-os-images

# Check storage class exists
oc get storageclass gp3-csi-wfc

# Verify you have permissions
oc auth can-i create vm -n default
```

## Installation

```bash
# Clone repository
git clone https://github.com/tsanders-rh/ocpctl.git
cd ocpctl

# Make script executable
chmod +x scripts/manage-windows-vms.sh
```

## Quick Start

### Basic Deployment (all VMs in default namespace)

```bash
# Deploy everything (create + wait + start)
./scripts/manage-windows-vms.sh deploy

# Check status
./scripts/manage-windows-vms.sh status

# Stop VMs when done
./scripts/manage-windows-vms.sh stop
```

### Separate Namespaces (each VM in its own namespace)

```bash
# Deploy VMs in separate namespaces
./scripts/manage-windows-vms.sh -s deploy

# Check status (shows namespace column)
./scripts/manage-windows-vms.sh -s status

# Clean up (deletes VMs and namespaces)
./scripts/manage-windows-vms.sh -s delete
```

## Usage

```
./scripts/manage-windows-vms.sh [-s|--separate-namespaces] {command}
```

### Flags

| Flag | Description |
|------|-------------|
| `-s, --separate-namespaces` | Create each VM in its own namespace (`windows-test-{1-5}-ns`) |

### Commands

| Command | Description | Duration |
|---------|-------------|----------|
| `create` | Create VMs without starting them | ~1 min |
| `wait` | Wait for VM disks to finish provisioning | 30-60 min |
| `start` | Start all VMs | ~1 min |
| `stop` | Stop all VMs | ~1 min |
| `delete` | Delete all VMs and disks (and namespaces if `-s` used) | ~2 min |
| `status` | Show current status of all VMs | Instant |
| `deploy` | All-in-one: create + wait + start | 30-60 min |

## Examples

### Standard Workflow

```bash
# 1. Deploy all VMs
./scripts/manage-windows-vms.sh deploy

# Output:
# Creating 5 Windows VMs in namespace: default...
# ✓ Created VM: windows-test-1 in namespace: default
# ...
# Waiting for VMs to provision (disk cloning)...
#   Progress: 25% | Elapsed: 5m
# ✓ All VMs provisioned successfully
# ✓ All VMs started

# 2. Check status
./scripts/manage-windows-vms.sh status

# Output:
# VM Status:
# NAME                 STATUS          READY      DISK PHASE
# ----                 ------          -----      ----------
# windows-test-1       Running         true       Succeeded
# windows-test-2       Running         true       Succeeded
# ...
# Resource Usage (for running VMs):
#   Running VMs: 5
#   Total vCPUs: 20
#   Total RAM:   40Gi

# 3. Stop VMs to save resources
./scripts/manage-windows-vms.sh stop

# 4. Restart when needed
./scripts/manage-windows-vms.sh start

# 5. Clean up
./scripts/manage-windows-vms.sh delete
```

### Separate Namespaces Workflow

```bash
# Deploy VMs in separate namespaces (useful for OADP namespace-scoped backups)
./scripts/manage-windows-vms.sh -s deploy

# Check status (includes namespace column)
./scripts/manage-windows-vms.sh -s status

# Output:
# VM Status:
# NAME                 NAMESPACE            STATUS          READY      DISK PHASE
# ----                 ---------            ------          -----      ----------
# windows-test-1       windows-test-1-ns    Running         true       Succeeded
# windows-test-2       windows-test-2-ns    Running         true       Succeeded
# ...

# Delete VMs and their namespaces
./scripts/manage-windows-vms.sh -s delete
# Output:
# ⚠ This will delete all 5 VMs, their disks, and their namespaces
# Are you sure? (yes/no): yes
```

### Step-by-Step Deployment

```bash
# 1. Create VMs (disks start cloning in background)
./scripts/manage-windows-vms.sh create

# 2. Do other work while disks provision...

# 3. Check if provisioning is done
./scripts/manage-windows-vms.sh status

# 4. Wait for provisioning to complete
./scripts/manage-windows-vms.sh wait

# 5. Start VMs
./scripts/manage-windows-vms.sh start
```

## Configuration

Edit the script to customize these settings:

```bash
VM_COUNT=5                    # Number of VMs to create
VM_PREFIX="windows-test"      # VM name prefix
VM_NAMESPACE="default"        # Default namespace (when not using -s)
TEMPLATE_NAME="windows10-oadp-vm"
TEMPLATE_NAMESPACE="openshift-virtualization-os-images"
STORAGE_CLASS="gp3-csi-wfc"   # Storage class for PVCs
```

## Resource Requirements

### Per VM
- **vCPUs**: 4
- **Memory**: 8 GiB
- **Disk**: 60 GiB (cloned from template)

### Total (5 VMs)
- **vCPUs**: 20
- **Memory**: 40 GiB
- **Disk**: 300 GiB

### Cluster Requirements
- Ensure worker nodes have sufficient CPU/memory
- For AWS: Recommend `m5.4xlarge` or larger worker nodes
- For bare metal: Ensure adequate resources on nodes with CNV workloads

## Use Cases

### 1. OADP Backup/Restore Testing

**Namespace-scoped backups** (test backup of single namespace):
```bash
# Deploy VMs in separate namespaces
./scripts/manage-windows-vms.sh -s deploy

# Create backup for one VM's namespace
cat <<EOF | oc apply -f -
apiVersion: velero.io/v1
kind: Backup
metadata:
  name: windows-test-1-backup
  namespace: openshift-adp
spec:
  includedNamespaces:
  - windows-test-1-ns
  storageLocation: default
  ttl: 720h0m0s
EOF

# Simulate disaster: delete the namespace
oc delete namespace windows-test-1-ns

# Restore from backup
cat <<EOF | oc apply -f -
apiVersion: velero.io/v1
kind: Restore
metadata:
  name: windows-test-1-restore
  namespace: openshift-adp
spec:
  backupName: windows-test-1-backup
EOF
```

**Cluster-wide backups** (test backup of all VMs):
```bash
# Deploy VMs in default namespace
./scripts/manage-windows-vms.sh deploy

# Create cluster backup including default namespace
cat <<EOF | oc apply -f -
apiVersion: velero.io/v1
kind: Backup
metadata:
  name: windows-vms-backup
  namespace: openshift-adp
spec:
  includedNamespaces:
  - default
  storageLocation: default
EOF
```

### 2. Migration Testing (MTC/Crane)

**Multi-namespace migration**:
```bash
# Deploy VMs in separate namespaces on source cluster
./scripts/manage-windows-vms.sh -s deploy

# Create MigPlan to migrate all 5 namespaces
# Test namespace mapping during migration
```

**Single-namespace migration**:
```bash
# Deploy in default namespace
./scripts/manage-windows-vms.sh deploy

# Test migrating just the default namespace
```

### 3. Performance Testing

```bash
# Deploy VMs
./scripts/manage-windows-vms.sh deploy

# Monitor cluster resource usage
watch 'oc get nodes -o json | jq ".items[] | {name: .metadata.name, cpu: .status.allocatable.cpu, memory: .status.allocatable.memory}"'

# Test VM performance with 5 concurrent Windows VMs
```

## Troubleshooting

### Disk Provisioning Stuck

**Symptom**: VM disk stays in `ImportInProgress` or `WaitForFirstConsumer`

**Solution**:
```bash
# Check DataVolume status
oc get datavolume windows-test-1-disk -n default -o yaml

# Check PVC events
oc describe pvc windows-test-1-disk -n default

# Check if CSI driver is healthy
oc get csidriver
oc get pods -n openshift-cluster-csi-drivers
```

### VM Won't Start

**Symptom**: VM stays in `Starting` or `Provisioning` state

**Solution**:
```bash
# Check VM events
oc describe vm windows-test-1 -n default

# Check virt-launcher pod
oc get pods -n default | grep windows-test-1
oc logs -n default virt-launcher-windows-test-1-xxxxx

# Check if VM has enough resources
oc get vm windows-test-1 -n default -o yaml
```

### Template Not Found

**Symptom**: `Error from server (NotFound): templates.template.openshift.io "windows10-oadp-vm" not found`

**Solution**:
```bash
# Check if CNV addon was deployed with Windows support
# The template is created by the CNV addon during post-deployment

# Verify CNV operator is installed
oc get csv -n openshift-cnv

# Check if template exists in any namespace
oc get templates --all-namespaces | grep windows

# Re-run CNV addon deployment if needed
```

### Out of Resources

**Symptom**: VMs stuck in `Pending`, events show "Insufficient cpu" or "Insufficient memory"

**Solution**:
```bash
# Check node capacity
oc describe nodes | grep -A 5 "Allocated resources"

# Reduce VM count in script
# Edit script: VM_COUNT=3 (instead of 5)

# Or scale down worker nodes
```

## Advanced Usage

### Custom Storage Class

Edit the script to use a different storage class:
```bash
# For NFS storage
STORAGE_CLASS="nfs-client"

# For local storage
STORAGE_CLASS="local-storage"
```

### Different VM Template

Edit the script to use a custom template:
```bash
TEMPLATE_NAME="my-custom-windows-template"
TEMPLATE_NAMESPACE="my-namespace"
```

### Change VM Resources

Edit the template parameters or create a custom template with different resources:
```yaml
# Custom template with 8 vCPU, 16 GiB RAM
cpu: 8
memory: 16Gi
```

## Cleanup

### Delete VMs Only (keep namespaces)
```bash
./scripts/manage-windows-vms.sh delete
```

### Delete VMs and Namespaces
```bash
./scripts/manage-windows-vms.sh -s delete
```

### Force Delete Stuck VMs
```bash
# If VMs won't delete normally
for i in {1..5}; do
  oc delete vm windows-test-$i -n default --force --grace-period=0
done
```

## Related Documentation

- [OpenShift Virtualization Documentation](https://docs.openshift.com/container-platform/latest/virt/about-virt.html)
- [OADP Documentation](https://docs.openshift.com/container-platform/latest/backup_and_restore/application_backup_and_restore/oadp-intro.html)
- [MTC Documentation](https://docs.openshift.com/container-platform/latest/migration_toolkit_for_containers/about-mtc.html)

## Support

For issues or questions:
- GitHub Issues: https://github.com/tsanders-rh/ocpctl/issues
- Internal: Contact ocpctl team

## License

See main repository LICENSE file.
