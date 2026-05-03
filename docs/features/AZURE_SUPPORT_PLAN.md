# Azure Platform Support - Implementation Plan

## Overview
Add comprehensive Azure platform support to ocpctl, enabling provisioning and management of OpenShift and Kubernetes clusters on Microsoft Azure. This includes support for Azure Red Hat OpenShift (ARO), Azure Kubernetes Service (AKS), and self-managed OpenShift on Azure VMs, with full feature parity with existing AWS and GCP platforms.

## Scope

### Supported Cluster Types
1. **Azure Red Hat OpenShift (ARO)** - Fully managed OpenShift service on Azure
2. **Azure Kubernetes Service (AKS)** - Managed Kubernetes service on Azure
3. **Self-managed OpenShift on Azure VMs** - OpenShift installed on Azure Virtual Machines using openshift-install

### Feature Parity Requirements
- ✅ Cluster provisioning and destruction
- ✅ Hibernation support (VM stop/start, node pool scaling)
- ✅ Cost tracking via Azure Cost Management API
- ✅ Resource tagging for orphan detection
- ✅ Work hours enforcement
- ✅ TTL-based auto-destruction
- ✅ Profile-based configuration
- ✅ Post-deployment configuration support

---

## Architecture

### Authentication
Azure operations require authentication via **Azure Service Principal**:
- Service Principal credentials stored in environment variables
- Required permissions: Contributor role on subscription
- Authentication via `az login --service-principal`

**Environment Variables:**
```bash
AZURE_SUBSCRIPTION_ID=<subscription-id>
AZURE_TENANT_ID=<tenant-id>
AZURE_CLIENT_ID=<client-id>
AZURE_CLIENT_SECRET=<client-secret>
```

### Platform Abstraction Pattern
Following existing AWS/GCP patterns:
- Type system additions for `PlatformAzure`
- Cluster type enums: `ClusterTypeARO`, `ClusterTypeAKS`
- Platform-specific worker handlers
- Switch-case routing in job processor

---

## Implementation Details

### 1. Type System Updates

**File:** `pkg/types/cluster.go`

Add new platform and cluster type constants:
```go
const (
    // Existing platforms
    PlatformAWS      Platform = "aws"
    PlatformGCP      Platform = "gcp"
    PlatformIBMCloud Platform = "ibmcloud"

    // New Azure platform
    PlatformAzure    Platform = "azure"
)

const (
    // Existing cluster types
    ClusterTypeOpenShift ClusterType = "openshift"
    ClusterTypeEKS       ClusterType = "eks"
    ClusterTypeIKS       ClusterType = "iks"
    ClusterTypeGKE       ClusterType = "gke"

    // New Azure cluster types
    ClusterTypeARO       ClusterType = "aro"
    ClusterTypeAKS       ClusterType = "aks"
)
```

Add Azure-specific configuration structs:
```go
// AzureConfig holds Azure-specific cluster configuration
type AzureConfig struct {
    SubscriptionID  string         `json:"subscription_id"`
    ResourceGroup   string         `json:"resource_group"`
    VNetName        string         `json:"vnet_name,omitempty"`
    SubnetName      string         `json:"subnet_name,omitempty"`
    VMConfig        *AzureVMConfig `json:"vm_config,omitempty"`
    Tags            map[string]string `json:"tags,omitempty"`
}

// AzureVMConfig holds Azure VM-specific configuration (for self-managed OpenShift)
type AzureVMConfig struct {
    MasterVMSize   string `json:"master_vm_size"`   // e.g., "Standard_D8s_v3"
    WorkerVMSize   string `json:"worker_vm_size"`   // e.g., "Standard_D4s_v3"
    MasterCount    int    `json:"master_count"`
    WorkerCount    int    `json:"worker_count"`
    OSDiskSizeGB   int    `json:"os_disk_size_gb"`
}

// AROConfig holds ARO-specific configuration
type AROConfig struct {
    MasterVMSize     string `json:"master_vm_size"`    // e.g., "Standard_D8s_v3"
    WorkerVMSize     string `json:"worker_vm_size"`    // e.g., "Standard_D4s_v3"
    WorkerCount      int    `json:"worker_count"`
    PullSecret       string `json:"pull_secret"`       // Red Hat pull secret
    ServicePrincipal string `json:"service_principal"` // SP credentials
}

// AKSConfig holds AKS-specific configuration
type AKSConfig struct {
    KubernetesVersion string          `json:"kubernetes_version"`
    NodePools         []AKSNodePool   `json:"node_pools"`
    NetworkPlugin     string          `json:"network_plugin"`     // azure, kubenet
    NetworkPolicy     string          `json:"network_policy"`     // azure, calico
}

type AKSNodePool struct {
    Name         string `json:"name"`
    VMSize       string `json:"vm_size"`       // e.g., "Standard_D4s_v3"
    Count        int    `json:"count"`
    MinCount     int    `json:"min_count"`     // For autoscaling
    MaxCount     int    `json:"max_count"`     // For autoscaling
    EnableAutoScale bool `json:"enable_autoscale"`
}
```

---

### 2. Profile Definitions

Create three new YAML profiles for Azure clusters.

#### Profile 1: `azure-standard.yaml` (Self-managed OpenShift)
**File:** `configs/profiles/azure-standard.yaml`

```yaml
name: azure-standard
description: "Standard OpenShift cluster on Azure VMs (3 masters, 3 workers)"
platform: azure
cluster_type: openshift
enabled: true
requires_base_domain: true

cost_controls:
  estimated_hourly_cost: 4.50  # Estimate based on Azure VM pricing
  max_ttl_hours: 168            # 1 week max
  default_ttl_hours: 72         # 3 days default

resource_limits:
  max_clusters_per_user: 3
  max_total_clusters: 50

azure:
  vm_config:
    master_vm_size: "Standard_D8s_v3"    # 8 vCPU, 32 GB RAM
    worker_vm_size: "Standard_D4s_v3"    # 4 vCPU, 16 GB RAM
    master_count: 3
    worker_count: 3
    os_disk_size_gb: 128
  vnet_name: "ocpctl-vnet"
  subnet_name: "ocpctl-subnet"

openshift_version: "4.15"
supported_regions:
  - "eastus"
  - "westus2"
  - "centralus"
  - "northeurope"
  - "westeurope"

default_tags:
  managed-by: ocpctl
  platform: azure
  cluster-type: openshift
```

#### Profile 2: `azure-aro-standard.yaml` (Azure Red Hat OpenShift)
**File:** `configs/profiles/azure-aro-standard.yaml`

```yaml
name: azure-aro-standard
description: "Azure Red Hat OpenShift (ARO) - Managed OpenShift service"
platform: azure
cluster_type: aro
enabled: true
requires_base_domain: false  # ARO provides default domain

cost_controls:
  estimated_hourly_cost: 5.00  # ARO managed service premium
  max_ttl_hours: 168
  default_ttl_hours: 72

resource_limits:
  max_clusters_per_user: 2
  max_total_clusters: 30

aro:
  master_vm_size: "Standard_D8s_v3"
  worker_vm_size: "Standard_D4s_v3"
  worker_count: 3
  openshift_version: "4.15"

supported_regions:
  - "eastus"
  - "eastus2"
  - "westus"
  - "westus2"
  - "centralus"
  - "northeurope"

default_tags:
  managed-by: ocpctl
  platform: azure
  cluster-type: aro
```

#### Profile 3: `azure-aks-standard.yaml` (Azure Kubernetes Service)
**File:** `configs/profiles/azure-aks-standard.yaml`

```yaml
name: azure-aks-standard
description: "Azure Kubernetes Service (AKS) - Managed Kubernetes"
platform: azure
cluster_type: aks
enabled: true
requires_base_domain: false  # AKS provides default domain

cost_controls:
  estimated_hourly_cost: 2.50  # AKS is cheaper than ARO
  max_ttl_hours: 168
  default_ttl_hours: 72

resource_limits:
  max_clusters_per_user: 5
  max_total_clusters: 100

aks:
  kubernetes_version: "1.28"
  network_plugin: "azure"      # Azure CNI
  network_policy: "azure"
  node_pools:
    - name: "system"
      vm_size: "Standard_D4s_v3"
      count: 3
      enable_autoscale: false
    - name: "worker"
      vm_size: "Standard_D4s_v3"
      count: 3
      min_count: 1
      max_count: 10
      enable_autoscale: true

supported_regions:
  - "eastus"
  - "eastus2"
  - "westus"
  - "westus2"
  - "centralus"
  - "northeurope"
  - "westeurope"

default_tags:
  managed-by: ocpctl
  platform: azure
  cluster-type: aks
```

---

### 3. Installer Implementations

#### ARO Installer
**File:** `internal/installer/aro.go`

```go
package installer

import (
    "context"
    "encoding/json"
    "fmt"
    "os/exec"
    "path/filepath"

    "github.com/tsanders-rh/ocpctl/pkg/types"
)

// AROInstaller handles Azure Red Hat OpenShift cluster provisioning
type AROInstaller struct {
    workDir string
}

func NewAROInstaller(workDir string) *AROInstaller {
    return &AROInstaller{workDir: workDir}
}

// Install provisions an ARO cluster using az aro CLI
func (i *AROInstaller) Install(ctx context.Context, cluster *types.Cluster, profile *profile.Profile) error {
    clusterWorkDir := filepath.Join(i.workDir, cluster.ID)

    // Extract ARO config from profile
    aroConfig := profile.ARO
    if aroConfig == nil {
        return fmt.Errorf("profile missing ARO configuration")
    }

    // Generate resource group name
    resourceGroup := fmt.Sprintf("ocpctl-%s-rg", cluster.Name)

    // Create resource group
    cmd := exec.CommandContext(ctx, "az", "group", "create",
        "--name", resourceGroup,
        "--location", cluster.Region,
        "--tags", formatAzureTags(cluster.EffectiveTags))

    if err := cmd.Run(); err != nil {
        return fmt.Errorf("create resource group: %w", err)
    }

    // Create ARO cluster
    cmd = exec.CommandContext(ctx, "az", "aro", "create",
        "--resource-group", resourceGroup,
        "--name", cluster.Name,
        "--master-vm-size", aroConfig.MasterVMSize,
        "--worker-vm-size", aroConfig.WorkerVMSize,
        "--worker-count", fmt.Sprintf("%d", aroConfig.WorkerCount),
        "--version", aroConfig.OpenShiftVersion,
        "--pull-secret", "@"+filepath.Join(clusterWorkDir, "pull-secret.json"),
        "--tags", formatAzureTags(cluster.EffectiveTags))

    output, err := cmd.CombinedOutput()
    if err != nil {
        return fmt.Errorf("create ARO cluster: %w (output: %s)", err, output)
    }

    // Save cluster metadata
    metadata := map[string]string{
        "resource_group": resourceGroup,
        "cluster_name":   cluster.Name,
        "region":         cluster.Region,
    }

    if err := saveMetadata(clusterWorkDir, metadata); err != nil {
        return fmt.Errorf("save metadata: %w", err)
    }

    // Download kubeconfig
    cmd = exec.CommandContext(ctx, "az", "aro", "get-admin-kubeconfig",
        "--resource-group", resourceGroup,
        "--name", cluster.Name,
        "--file", filepath.Join(clusterWorkDir, "auth", "kubeconfig"))

    if err := cmd.Run(); err != nil {
        return fmt.Errorf("download kubeconfig: %w", err)
    }

    return nil
}

// Destroy removes an ARO cluster and its resource group
func (i *AROInstaller) Destroy(ctx context.Context, cluster *types.Cluster) error {
    clusterWorkDir := filepath.Join(i.workDir, cluster.ID)

    // Load metadata to get resource group
    metadata, err := loadMetadata(clusterWorkDir)
    if err != nil {
        return fmt.Errorf("load metadata: %w", err)
    }

    resourceGroup := metadata["resource_group"]

    // Delete ARO cluster
    cmd := exec.CommandContext(ctx, "az", "aro", "delete",
        "--resource-group", resourceGroup,
        "--name", cluster.Name,
        "--yes")

    if err := cmd.Run(); err != nil {
        return fmt.Errorf("delete ARO cluster: %w", err)
    }

    // Delete resource group
    cmd = exec.CommandContext(ctx, "az", "group", "delete",
        "--name", resourceGroup,
        "--yes")

    if err := cmd.Run(); err != nil {
        return fmt.Errorf("delete resource group: %w", err)
    }

    return nil
}

func formatAzureTags(tags map[string]string) string {
    var parts []string
    for k, v := range tags {
        parts = append(parts, fmt.Sprintf("%s=%s", k, v))
    }
    return strings.Join(parts, " ")
}

func saveMetadata(workDir string, metadata map[string]string) error {
    data, err := json.MarshalIndent(metadata, "", "  ")
    if err != nil {
        return err
    }
    return os.WriteFile(filepath.Join(workDir, "metadata.json"), data, 0644)
}

func loadMetadata(workDir string) (map[string]string, error) {
    data, err := os.ReadFile(filepath.Join(workDir, "metadata.json"))
    if err != nil {
        return nil, err
    }

    var metadata map[string]string
    if err := json.Unmarshal(data, &metadata); err != nil {
        return nil, err
    }

    return metadata, nil
}
```

#### AKS Installer
**File:** `internal/installer/aks.go`

```go
package installer

import (
    "context"
    "fmt"
    "os/exec"
    "path/filepath"

    "github.com/tsanders-rh/ocpctl/pkg/types"
)

// AKSInstaller handles Azure Kubernetes Service cluster provisioning
type AKSInstaller struct {
    workDir string
}

func NewAKSInstaller(workDir string) *AKSInstaller {
    return &AKSInstaller{workDir: workDir}
}

// Install provisions an AKS cluster using az aks CLI
func (i *AKSInstaller) Install(ctx context.Context, cluster *types.Cluster, profile *profile.Profile) error {
    clusterWorkDir := filepath.Join(i.workDir, cluster.ID)

    // Extract AKS config from profile
    aksConfig := profile.AKS
    if aksConfig == nil {
        return fmt.Errorf("profile missing AKS configuration")
    }

    // Generate resource group name
    resourceGroup := fmt.Sprintf("ocpctl-%s-rg", cluster.Name)

    // Create resource group
    cmd := exec.CommandContext(ctx, "az", "group", "create",
        "--name", resourceGroup,
        "--location", cluster.Region,
        "--tags", formatAzureTags(cluster.EffectiveTags))

    if err := cmd.Run(); err != nil {
        return fmt.Errorf("create resource group: %w", err)
    }

    // Create AKS cluster with system node pool
    systemPool := aksConfig.NodePools[0]
    cmd = exec.CommandContext(ctx, "az", "aks", "create",
        "--resource-group", resourceGroup,
        "--name", cluster.Name,
        "--kubernetes-version", aksConfig.KubernetesVersion,
        "--node-count", fmt.Sprintf("%d", systemPool.Count),
        "--node-vm-size", systemPool.VMSize,
        "--network-plugin", aksConfig.NetworkPlugin,
        "--network-policy", aksConfig.NetworkPolicy,
        "--tags", formatAzureTags(cluster.EffectiveTags),
        "--generate-ssh-keys")

    output, err := cmd.CombinedOutput()
    if err != nil {
        return fmt.Errorf("create AKS cluster: %w (output: %s)", err, output)
    }

    // Add additional node pools
    for _, pool := range aksConfig.NodePools[1:] {
        cmd = exec.CommandContext(ctx, "az", "aks", "nodepool", "add",
            "--resource-group", resourceGroup,
            "--cluster-name", cluster.Name,
            "--name", pool.Name,
            "--node-count", fmt.Sprintf("%d", pool.Count),
            "--node-vm-size", pool.VMSize)

        if pool.EnableAutoScale {
            cmd.Args = append(cmd.Args,
                "--enable-cluster-autoscaler",
                "--min-count", fmt.Sprintf("%d", pool.MinCount),
                "--max-count", fmt.Sprintf("%d", pool.MaxCount))
        }

        if err := cmd.Run(); err != nil {
            return fmt.Errorf("add node pool %s: %w", pool.Name, err)
        }
    }

    // Save cluster metadata
    metadata := map[string]string{
        "resource_group": resourceGroup,
        "cluster_name":   cluster.Name,
        "region":         cluster.Region,
    }

    if err := saveMetadata(clusterWorkDir, metadata); err != nil {
        return fmt.Errorf("save metadata: %w", err)
    }

    // Download kubeconfig
    cmd = exec.CommandContext(ctx, "az", "aks", "get-credentials",
        "--resource-group", resourceGroup,
        "--name", cluster.Name,
        "--file", filepath.Join(clusterWorkDir, "auth", "kubeconfig"),
        "--admin")

    if err := cmd.Run(); err != nil {
        return fmt.Errorf("download kubeconfig: %w", err)
    }

    return nil
}

// Destroy removes an AKS cluster and its resource group
func (i *AKSInstaller) Destroy(ctx context.Context, cluster *types.Cluster) error {
    clusterWorkDir := filepath.Join(i.workDir, cluster.ID)

    // Load metadata to get resource group
    metadata, err := loadMetadata(clusterWorkDir)
    if err != nil {
        return fmt.Errorf("load metadata: %w", err)
    }

    resourceGroup := metadata["resource_group"]

    // Delete AKS cluster
    cmd := exec.CommandContext(ctx, "az", "aks", "delete",
        "--resource-group", resourceGroup,
        "--name", cluster.Name,
        "--yes")

    if err := cmd.Run(); err != nil {
        return fmt.Errorf("delete AKS cluster: %w", err)
    }

    // Delete resource group
    cmd = exec.CommandContext(ctx, "az", "group", "delete",
        "--name", resourceGroup,
        "--yes")

    if err := cmd.Run(); err != nil {
        return fmt.Errorf("delete resource group: %w", err)
    }

    return nil
}
```

---

### 4. Worker Handlers

#### Create Handler
**File:** `internal/worker/handler_create_azure.go`

```go
package worker

import (
    "context"
    "fmt"
    "log"
    "path/filepath"

    "github.com/tsanders-rh/ocpctl/internal/installer"
    "github.com/tsanders-rh/ocpctl/pkg/types"
)

// handleCreateAzure provisions Azure clusters (ARO, AKS, or self-managed OpenShift)
func (w *Worker) handleCreateAzure(ctx context.Context, job *types.Job, cluster *types.Cluster) error {
    log.Printf("[JOB %s] Starting Azure cluster creation: %s (type: %s, region: %s)",
        job.ID, cluster.Name, cluster.ClusterType, cluster.Region)

    // Get profile configuration
    prof, err := w.profileRegistry.Get(cluster.Profile)
    if err != nil {
        return fmt.Errorf("get profile: %w", err)
    }

    // Route to cluster-type specific installer
    var installer interface {
        Install(context.Context, *types.Cluster, *profile.Profile) error
    }

    switch cluster.ClusterType {
    case types.ClusterTypeARO:
        installer = installer.NewAROInstaller(w.workDir)
    case types.ClusterTypeAKS:
        installer = installer.NewAKSInstaller(w.workDir)
    case types.ClusterTypeOpenShift:
        // Self-managed OpenShift on Azure VMs using openshift-install
        installer = installer.NewOpenshiftInstaller(w.workDir)
    default:
        return fmt.Errorf("unsupported Azure cluster type: %s", cluster.ClusterType)
    }

    // Run installation
    if err := installer.Install(ctx, cluster, prof); err != nil {
        return fmt.Errorf("install cluster: %w", err)
    }

    log.Printf("[JOB %s] Azure cluster creation completed successfully", job.ID)
    return nil
}
```

#### Destroy Handler
**File:** `internal/worker/handler_destroy_azure.go`

```go
package worker

import (
    "context"
    "fmt"
    "log"

    "github.com/tsanders-rh/ocpctl/internal/installer"
    "github.com/tsanders-rh/ocpctl/pkg/types"
)

// handleDestroyAzure destroys Azure clusters and cleans up resources
func (w *Worker) handleDestroyAzure(ctx context.Context, job *types.Job, cluster *types.Cluster) error {
    log.Printf("[JOB %s] Starting Azure cluster destruction: %s (type: %s)",
        job.ID, cluster.Name, cluster.ClusterType)

    // Route to cluster-type specific destroyer
    var destroyer interface {
        Destroy(context.Context, *types.Cluster) error
    }

    switch cluster.ClusterType {
    case types.ClusterTypeARO:
        destroyer = installer.NewAROInstaller(w.workDir)
    case types.ClusterTypeAKS:
        destroyer = installer.NewAKSInstaller(w.workDir)
    case types.ClusterTypeOpenShift:
        destroyer = installer.NewOpenshiftInstaller(w.workDir)
    default:
        return fmt.Errorf("unsupported Azure cluster type: %s", cluster.ClusterType)
    }

    // Run destruction
    if err := destroyer.Destroy(ctx, cluster); err != nil {
        return fmt.Errorf("destroy cluster: %w", err)
    }

    log.Printf("[JOB %s] Azure cluster destruction completed successfully", job.ID)
    return nil
}
```

#### Hibernate Handler
**File:** `internal/worker/handler_hibernate_azure.go`

```go
package worker

import (
    "context"
    "encoding/json"
    "fmt"
    "log"
    "os/exec"
    "path/filepath"

    "github.com/tsanders-rh/ocpctl/pkg/types"
)

// handleHibernateAzure hibernates Azure clusters to reduce costs
// - ARO/AKS: Scale node pools to 0
// - Self-managed OpenShift: Stop all VMs
func (w *Worker) handleHibernateAzure(ctx context.Context, job *types.Job, cluster *types.Cluster) error {
    log.Printf("[JOB %s] Starting Azure cluster hibernation: %s (type: %s)",
        job.ID, cluster.Name, cluster.ClusterType)

    clusterWorkDir := filepath.Join(w.workDir, cluster.ID)

    // Load metadata
    metadata, err := loadMetadata(clusterWorkDir)
    if err != nil {
        return fmt.Errorf("load metadata: %w", err)
    }

    resourceGroup := metadata["resource_group"]

    switch cluster.ClusterType {
    case types.ClusterTypeARO:
        return hibernateARO(ctx, resourceGroup, cluster.Name, job)
    case types.ClusterTypeAKS:
        return hibernateAKS(ctx, resourceGroup, cluster.Name, job)
    case types.ClusterTypeOpenShift:
        return hibernateAzureVMs(ctx, resourceGroup, cluster, job)
    default:
        return fmt.Errorf("unsupported cluster type: %s", cluster.ClusterType)
    }
}

func hibernateARO(ctx context.Context, resourceGroup, clusterName string, job *types.Job) error {
    // ARO doesn't support direct node pool scaling like AKS
    // Instead, we scale the worker MachineSet to 0
    // This requires using oc/kubectl commands

    // Get kubeconfig path
    kubeconfig := filepath.Join(w.workDir, job.ClusterID, "auth", "kubeconfig")

    // List MachineSets
    cmd := exec.CommandContext(ctx, "oc", "--kubeconfig", kubeconfig,
        "get", "machineset", "-n", "openshift-machine-api",
        "-o", "jsonpath={.items[*].metadata.name}")

    output, err := cmd.Output()
    if err != nil {
        return fmt.Errorf("list machinesets: %w", err)
    }

    machineSets := strings.Fields(string(output))

    // Save original replica counts to job metadata
    replicaCounts := make(map[string]int)
    for _, ms := range machineSets {
        cmd = exec.CommandContext(ctx, "oc", "--kubeconfig", kubeconfig,
            "get", "machineset", ms, "-n", "openshift-machine-api",
            "-o", "jsonpath={.spec.replicas}")

        output, err := cmd.Output()
        if err != nil {
            continue
        }

        var replicas int
        fmt.Sscanf(string(output), "%d", &replicas)
        replicaCounts[ms] = replicas

        // Scale to 0
        cmd = exec.CommandContext(ctx, "oc", "--kubeconfig", kubeconfig,
            "scale", "machineset", ms, "--replicas=0",
            "-n", "openshift-machine-api")

        if err := cmd.Run(); err != nil {
            return fmt.Errorf("scale machineset %s: %w", ms, err)
        }
    }

    // Save replica counts for resume
    job.Metadata["aro_replica_counts"] = replicaCounts

    return nil
}

func hibernateAKS(ctx context.Context, resourceGroup, clusterName string, job *types.Job) error {
    // List all node pools
    cmd := exec.CommandContext(ctx, "az", "aks", "nodepool", "list",
        "--resource-group", resourceGroup,
        "--cluster-name", clusterName,
        "--query", "[].{name:name,count:count}",
        "-o", "json")

    output, err := cmd.Output()
    if err != nil {
        return fmt.Errorf("list node pools: %w", err)
    }

    var nodePools []struct {
        Name  string `json:"name"`
        Count int    `json:"count"`
    }

    if err := json.Unmarshal(output, &nodePools); err != nil {
        return fmt.Errorf("parse node pools: %w", err)
    }

    // Save original node counts to job metadata for resume
    nodeCounts := make(map[string]int)
    for _, pool := range nodePools {
        nodeCounts[pool.Name] = pool.Count

        // Scale node pool to 0
        cmd = exec.CommandContext(ctx, "az", "aks", "nodepool", "scale",
            "--resource-group", resourceGroup,
            "--cluster-name", clusterName,
            "--name", pool.Name,
            "--node-count", "0")

        if err := cmd.Run(); err != nil {
            return fmt.Errorf("scale node pool %s: %w", pool.Name, err)
        }

        log.Printf("[JOB %s] Scaled node pool %s to 0 (was %d)", job.ID, pool.Name, pool.Count)
    }

    job.Metadata["aks_node_counts"] = nodeCounts

    return nil
}

func hibernateAzureVMs(ctx context.Context, resourceGroup string, cluster *types.Cluster, job *types.Job) error {
    // List all VMs in resource group with cluster tag
    cmd := exec.CommandContext(ctx, "az", "vm", "list",
        "--resource-group", resourceGroup,
        "--query", fmt.Sprintf("[?tags.\"kubernetes.io/cluster/%s\"=='owned'].name", cluster.Name),
        "-o", "json")

    output, err := cmd.Output()
    if err != nil {
        return fmt.Errorf("list VMs: %w", err)
    }

    var vmNames []string
    if err := json.Unmarshal(output, &vmNames); err != nil {
        return fmt.Errorf("parse VM names: %w", err)
    }

    // Stop all VMs
    for _, vmName := range vmNames {
        cmd = exec.CommandContext(ctx, "az", "vm", "stop",
            "--resource-group", resourceGroup,
            "--name", vmName)

        if err := cmd.Run(); err != nil {
            return fmt.Errorf("stop VM %s: %w", vmName, err)
        }

        // Deallocate to stop billing
        cmd = exec.CommandContext(ctx, "az", "vm", "deallocate",
            "--resource-group", resourceGroup,
            "--name", vmName)

        if err := cmd.Run(); err != nil {
            return fmt.Errorf("deallocate VM %s: %w", vmName, err)
        }

        log.Printf("[JOB %s] Stopped and deallocated VM: %s", job.ID, vmName)
    }

    job.Metadata["azure_vm_names"] = vmNames

    return nil
}
```

#### Resume Handler
**File:** `internal/worker/handler_resume_azure.go`

```go
package worker

import (
    "context"
    "fmt"
    "log"
    "os/exec"
    "path/filepath"

    "github.com/tsanders-rh/ocpctl/pkg/types"
)

// handleResumeAzure resumes hibernated Azure clusters
// - ARO/AKS: Restore node pool sizes
// - Self-managed OpenShift: Start all VMs
func (w *Worker) handleResumeAzure(ctx context.Context, job *types.Job, cluster *types.Cluster) error {
    log.Printf("[JOB %s] Starting Azure cluster resume: %s (type: %s)",
        job.ID, cluster.Name, cluster.ClusterType)

    clusterWorkDir := filepath.Join(w.workDir, cluster.ID)

    // Load metadata
    metadata, err := loadMetadata(clusterWorkDir)
    if err != nil {
        return fmt.Errorf("load metadata: %w", err)
    }

    resourceGroup := metadata["resource_group"]

    // Get most recent HIBERNATE job to restore original sizes
    hibernateJob, err := w.store.Jobs.GetMostRecentByTypeAndStatus(
        ctx, cluster.ID, types.JobTypeHibernate, types.JobStatusSucceeded)
    if err != nil {
        return fmt.Errorf("get hibernate job: %w", err)
    }

    switch cluster.ClusterType {
    case types.ClusterTypeARO:
        return resumeARO(ctx, cluster, hibernateJob, job)
    case types.ClusterTypeAKS:
        return resumeAKS(ctx, resourceGroup, cluster.Name, hibernateJob, job)
    case types.ClusterTypeOpenShift:
        return resumeAzureVMs(ctx, resourceGroup, hibernateJob, job)
    default:
        return fmt.Errorf("unsupported cluster type: %s", cluster.ClusterType)
    }
}

func resumeARO(ctx context.Context, cluster *types.Cluster, hibernateJob *types.Job, job *types.Job) error {
    kubeconfig := filepath.Join(w.workDir, cluster.ID, "auth", "kubeconfig")

    // Get saved replica counts from hibernate job
    replicaCounts := hibernateJob.Metadata["aro_replica_counts"].(map[string]int)

    // Restore each MachineSet to original replica count
    for ms, replicas := range replicaCounts {
        cmd := exec.CommandContext(ctx, "oc", "--kubeconfig", kubeconfig,
            "scale", "machineset", ms,
            fmt.Sprintf("--replicas=%d", replicas),
            "-n", "openshift-machine-api")

        if err := cmd.Run(); err != nil {
            return fmt.Errorf("scale machineset %s: %w", ms, err)
        }

        log.Printf("[JOB %s] Restored machineset %s to %d replicas", job.ID, ms, replicas)
    }

    return nil
}

func resumeAKS(ctx context.Context, resourceGroup, clusterName string, hibernateJob *types.Job, job *types.Job) error {
    // Get saved node counts from hibernate job
    nodeCounts := hibernateJob.Metadata["aks_node_counts"].(map[string]int)

    // Restore each node pool to original size
    for poolName, count := range nodeCounts {
        cmd := exec.CommandContext(ctx, "az", "aks", "nodepool", "scale",
            "--resource-group", resourceGroup,
            "--cluster-name", clusterName,
            "--name", poolName,
            "--node-count", fmt.Sprintf("%d", count))

        if err := cmd.Run(); err != nil {
            return fmt.Errorf("scale node pool %s: %w", poolName, err)
        }

        log.Printf("[JOB %s] Restored node pool %s to %d nodes", job.ID, poolName, count)
    }

    return nil
}

func resumeAzureVMs(ctx context.Context, resourceGroup string, hibernateJob *types.Job, job *types.Job) error {
    // Get VM names from hibernate job
    vmNames := hibernateJob.Metadata["azure_vm_names"].([]string)

    // Start all VMs
    for _, vmName := range vmNames {
        cmd := exec.CommandContext(ctx, "az", "vm", "start",
            "--resource-group", resourceGroup,
            "--name", vmName)

        if err := cmd.Run(); err != nil {
            return fmt.Errorf("start VM %s: %w", vmName, err)
        }

        log.Printf("[JOB %s] Started VM: %s", job.ID, vmName)
    }

    return nil
}
```

---

### 5. Cost Tracking Integration

**File:** `internal/cost/azure.go`

```go
package cost

import (
    "context"
    "encoding/json"
    "fmt"
    "os/exec"
    "time"

    "github.com/tsanders-rh/ocpctl/pkg/types"
)

// AzureCostTracker fetches actual cluster costs from Azure Cost Management API
type AzureCostTracker struct {
    subscriptionID string
}

func NewAzureCostTracker(subscriptionID string) *AzureCostTracker {
    return &AzureCostTracker{
        subscriptionID: subscriptionID,
    }
}

// GetClusterCost fetches actual cost for a cluster from Azure Cost Management
func (t *AzureCostTracker) GetClusterCost(ctx context.Context, cluster *types.Cluster, startDate, endDate time.Time) (float64, error) {
    // Build cost management query
    // Filter by resource group tag or cluster name tag
    query := fmt.Sprintf(`{
        "type": "ActualCost",
        "timeframe": "Custom",
        "timePeriod": {
            "from": "%s",
            "to": "%s"
        },
        "dataset": {
            "granularity": "Daily",
            "aggregation": {
                "totalCost": {
                    "name": "Cost",
                    "function": "Sum"
                }
            },
            "filter": {
                "tags": {
                    "name": "managed-by",
                    "operator": "In",
                    "values": ["ocpctl"]
                },
                "dimensions": {
                    "name": "ResourceGroup",
                    "operator": "In",
                    "values": ["ocpctl-%s-rg"]
                }
            }
        }
    }`, startDate.Format("2006-01-02"), endDate.Format("2006-01-02"), cluster.Name)

    // Execute az costmanagement query
    cmd := exec.CommandContext(ctx, "az", "costmanagement", "query",
        "--type", "ActualCost",
        "--scope", fmt.Sprintf("/subscriptions/%s", t.subscriptionID),
        "--dataset-configuration", query)

    output, err := cmd.Output()
    if err != nil {
        return 0, fmt.Errorf("query cost management: %w", err)
    }

    // Parse response
    var response struct {
        Properties struct {
            Rows [][]interface{} `json:"rows"`
        } `json:"properties"`
    }

    if err := json.Unmarshal(output, &response); err != nil {
        return 0, fmt.Errorf("parse cost response: %w", err)
    }

    // Sum all costs
    var totalCost float64
    for _, row := range response.Properties.Rows {
        if len(row) > 0 {
            if cost, ok := row[0].(float64); ok {
                totalCost += cost
            }
        }
    }

    return totalCost, nil
}

// GetDailyCost returns daily cost for a cluster
func (t *AzureCostTracker) GetDailyCost(ctx context.Context, cluster *types.Cluster) (float64, error) {
    now := time.Now()
    yesterday := now.Add(-24 * time.Hour)

    return t.GetClusterCost(ctx, cluster, yesterday, now)
}

// GetMonthlyCost returns monthly cost for a cluster
func (t *AzureCostTracker) GetMonthlyCost(ctx context.Context, cluster *types.Cluster) (float64, error) {
    now := time.Now()
    monthAgo := now.Add(-30 * 24 * time.Hour)

    return t.GetClusterCost(ctx, cluster, monthAgo, now)
}
```

---

### 6. Install-Config Template (Self-managed OpenShift)

**File:** `internal/installer/templates/azure-install-config.yaml`

```yaml
apiVersion: v1
baseDomain: {{ .BaseDomain }}
metadata:
  name: {{ .Name }}
platform:
  azure:
    region: {{ .Region }}
    baseDomainResourceGroupName: {{ .BaseDomainResourceGroup }}
    cloudName: AzurePublicCloud
    outboundType: Loadbalancer
    networkResourceGroupName: {{ .NetworkResourceGroup }}
    virtualNetwork: {{ .VNetName }}
    controlPlaneSubnet: {{ .ControlPlaneSubnet }}
    computeSubnet: {{ .ComputeSubnet }}
controlPlane:
  name: master
  replicas: {{ .MasterCount }}
  platform:
    azure:
      osDisk:
        diskSizeGB: {{ .OSDiskSizeGB }}
      type: {{ .MasterVMSize }}
compute:
- name: worker
  replicas: {{ .WorkerCount }}
  platform:
    azure:
      osDisk:
        diskSizeGB: {{ .OSDiskSizeGB }}
      type: {{ .WorkerVMSize }}
networking:
  networkType: OVNKubernetes
  clusterNetwork:
  - cidr: 10.128.0.0/14
    hostPrefix: 23
  serviceNetwork:
  - 172.30.0.0/16
pullSecret: {{ .PullSecret }}
sshKey: {{ .SSHPublicKey }}
```

---

## Integration Points

### 1. Worker Job Routing
**File:** `internal/worker/worker.go`

Update job handler routing:
```go
func (w *Worker) handleJob(ctx context.Context, job *types.Job) error {
    cluster, err := w.store.Clusters.GetByID(ctx, job.ClusterID)
    if err != nil {
        return fmt.Errorf("get cluster: %w", err)
    }

    switch job.JobType {
    case types.JobTypeCreate:
        switch cluster.Platform {
        case types.PlatformAWS:
            return w.handleCreateAWS(ctx, job, cluster)
        case types.PlatformGCP:
            return w.handleCreateGCP(ctx, job, cluster)
        case types.PlatformAzure:
            return w.handleCreateAzure(ctx, job, cluster)
        // ... other platforms
        }
    case types.JobTypeDestroy:
        switch cluster.Platform {
        case types.PlatformAWS:
            return w.handleDestroyAWS(ctx, job, cluster)
        case types.PlatformGCP:
            return w.handleDestroyGCP(ctx, job, cluster)
        case types.PlatformAzure:
            return w.handleDestroyAzure(ctx, job, cluster)
        // ... other platforms
        }
    case types.JobTypeHibernate:
        switch cluster.Platform {
        case types.PlatformAWS:
            return w.handleHibernateAWS(ctx, job, cluster)
        case types.PlatformGCP:
            return w.handleHibernateGCP(ctx, job, cluster)
        case types.PlatformAzure:
            return w.handleHibernateAzure(ctx, job, cluster)
        // ... other platforms
        }
    case types.JobTypeResume:
        switch cluster.Platform {
        case types.PlatformAWS:
            return w.handleResumeAWS(ctx, job, cluster)
        case types.PlatformGCP:
            return w.handleResumeGCP(ctx, job, cluster)
        case types.PlatformAzure:
            return w.handleResumeAzure(ctx, job, cluster)
        // ... other platforms
        }
    }

    return fmt.Errorf("unsupported job type or platform")
}
```

### 2. Profile Registry
**File:** `internal/profile/registry.go`

Update profile parsing to support Azure-specific fields:
```go
type Profile struct {
    // ... existing fields

    // Azure-specific configuration
    Azure *AzureConfig `yaml:"azure,omitempty"`
    ARO   *AROConfig   `yaml:"aro,omitempty"`
    AKS   *AKSConfig   `yaml:"aks,omitempty"`
}
```

### 3. Frontend Updates
**File:** `web/app/(dashboard)/clusters/create/page.tsx`

Add Azure platform to cluster creation form:
- Platform dropdown: Add "Azure" option
- Cluster type dropdown: Show "aro", "aks", "openshift" when Azure selected
- Region dropdown: Populate with Azure regions from profile
- Hide base_domain field for ARO/AKS (not required)

**File:** `web/lib/api/endpoints/clusters.ts`

No changes needed - existing types already support platform and cluster_type fields.

---

## Testing Strategy

### Manual Testing Checklist

#### ARO Clusters
- [ ] Create ARO cluster with azure-aro-standard profile
- [ ] Verify cluster provisions successfully
- [ ] Hibernate ARO cluster (scale workers to 0)
- [ ] Resume ARO cluster (restore worker count)
- [ ] Destroy ARO cluster
- [ ] Verify resource group deleted

#### AKS Clusters
- [ ] Create AKS cluster with azure-aks-standard profile
- [ ] Verify cluster provisions with multiple node pools
- [ ] Hibernate AKS cluster (scale all pools to 0)
- [ ] Resume AKS cluster (restore pool sizes)
- [ ] Verify autoscaling configuration preserved
- [ ] Destroy AKS cluster
- [ ] Verify resource group deleted

#### Self-managed OpenShift on Azure
- [ ] Create OpenShift cluster with azure-standard profile
- [ ] Verify openshift-install runs successfully
- [ ] Verify VMs created with correct tags
- [ ] Hibernate cluster (stop/deallocate all VMs)
- [ ] Resume cluster (start all VMs)
- [ ] Destroy cluster
- [ ] Verify all Azure resources deleted

#### Cost Tracking
- [ ] Query Azure Cost Management API for cluster costs
- [ ] Verify cost attribution by tags
- [ ] Compare estimated vs actual costs
- [ ] Verify hibernation reduces costs

#### Work Hours & Auto-destruction
- [ ] Enable work hours on Azure cluster
- [ ] Verify auto-hibernation during off-hours
- [ ] Verify auto-resume during work hours
- [ ] Set TTL on cluster
- [ ] Verify cluster auto-destroyed after TTL expires

---

## Deployment Checklist

### Prerequisites
- [ ] Azure subscription with Contributor role
- [ ] Service Principal created with required permissions
- [ ] Azure CLI (az) installed on worker nodes
- [ ] Environment variables configured:
  - AZURE_SUBSCRIPTION_ID
  - AZURE_TENANT_ID
  - AZURE_CLIENT_ID
  - AZURE_CLIENT_SECRET

### Database Changes
- [ ] No schema changes required

### Configuration Files
- [ ] Add azure-standard.yaml to configs/profiles/
- [ ] Add azure-aro-standard.yaml to configs/profiles/
- [ ] Add azure-aks-standard.yaml to configs/profiles/

### Code Deployment
- [ ] Deploy backend with Azure support
- [ ] Deploy worker with Azure handlers
- [ ] Deploy frontend with Azure platform option
- [ ] Verify profile registry loads Azure profiles
- [ ] Restart workers to pick up new handlers

### Monitoring
- [ ] Add Azure-specific metrics to monitoring dashboard
- [ ] Monitor Azure Cost Management API quota usage
- [ ] Track ARO/AKS/OpenShift cluster success rates
- [ ] Alert on Azure authentication failures

---

## Cost Estimates

### ARO (azure-aro-standard)
- **Control Plane**: ~$2.50/hr (managed by Microsoft)
- **Workers**: 3 × Standard_D4s_v3 @ $0.336/hr = $1.01/hr
- **Storage**: ~$0.20/hr (persistent disks)
- **Total**: ~$3.71/hr ($89/day, $2,670/month)

**Hibernated Cost**: ~$0.20/hr (storage only) = **95% savings**

### AKS (azure-aks-standard)
- **Control Plane**: FREE (AKS Free tier)
- **System Pool**: 3 × Standard_D4s_v3 @ $0.336/hr = $1.01/hr
- **Worker Pool**: 3 × Standard_D4s_v3 @ $0.336/hr = $1.01/hr
- **Storage**: ~$0.10/hr
- **Total**: ~$2.12/hr ($51/day, $1,530/month)

**Hibernated Cost**: ~$0.10/hr (storage only) = **95% savings**

### Self-managed OpenShift (azure-standard)
- **Masters**: 3 × Standard_D8s_v3 @ $0.672/hr = $2.02/hr
- **Workers**: 3 × Standard_D4s_v3 @ $0.336/hr = $1.01/hr
- **Storage**: ~$0.30/hr
- **Networking**: ~$0.20/hr
- **Total**: ~$3.53/hr ($85/day, $2,550/month)

**Hibernated Cost**: ~$0.30/hr (storage only) = **91% savings**

---

## Security Considerations

### Service Principal Permissions
- Use principle of least privilege
- Grant only Contributor role on specific resource groups (not subscription-wide)
- Rotate client secrets regularly (90 days)
- Store secrets in environment variables, never commit to git

### Network Security
- Create dedicated VNet for ocpctl clusters
- Use Network Security Groups (NSGs) to restrict traffic
- Enable Azure Private Link for ARO/AKS (production)
- Consider Azure Firewall for egress filtering

### Resource Tagging
- All resources tagged with `managed-by: ocpctl`
- Cluster-specific tags for cost attribution
- Owner tags for audit trail
- Automated orphan detection via tags

### Compliance
- Enable Azure Policy for governance
- Use Azure Cost Management budgets
- Export cost data for chargeback
- Maintain audit logs of all cluster operations

---

## Future Enhancements

### Phase 2 (Optional)
1. **Azure Private Clusters**: Support for private ARO/AKS clusters
2. **Azure AD Integration**: Use Azure AD for cluster authentication
3. **Azure Monitor Integration**: Export metrics to Azure Monitor
4. **Spot Instances**: Support for Azure Spot VMs to reduce costs
5. **Multi-region Deployments**: Geographic redundancy
6. **Azure DevOps Integration**: Pipeline integration for GitOps

### Cost Optimization
1. **Reserved Instances**: Purchase Azure Reserved VM Instances for long-term clusters
2. **Autoscaling**: Dynamic node pool sizing based on workload
3. **Cost Alerts**: Automated alerts when cluster costs exceed thresholds
4. **Rightsizing**: Analyze VM utilization and recommend smaller sizes

---

## Success Criteria

Implementation is considered complete when:

- [ ] All three cluster types (ARO, AKS, Self-managed) provision successfully
- [ ] Hibernation reduces costs by >90% for all cluster types
- [ ] Azure Cost Management API integration provides accurate cost data
- [ ] All existing features (work hours, TTL, post-deploy, tagging) work on Azure
- [ ] Frontend shows Azure as platform option with correct cluster types
- [ ] Profile validation enforces Azure-specific requirements
- [ ] Orphan detection identifies untagged Azure resources
- [ ] Documentation updated with Azure-specific setup instructions
- [ ] Admin dashboard shows Azure cluster statistics
- [ ] Integration tests pass for Azure workflows

---

## References

- **Azure Red Hat OpenShift**: https://docs.microsoft.com/en-us/azure/openshift/
- **Azure Kubernetes Service**: https://docs.microsoft.com/en-us/azure/aks/
- **OpenShift on Azure**: https://docs.openshift.com/container-platform/latest/installing/installing_azure/
- **Azure Cost Management API**: https://docs.microsoft.com/en-us/rest/api/cost-management/
- **Azure CLI**: https://docs.microsoft.com/en-us/cli/azure/
