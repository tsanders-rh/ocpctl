# ROSA (Red Hat OpenShift Service on AWS) Implementation Plan

## Executive Summary

Add support for ROSA (Red Hat OpenShift Service on AWS) Classic clusters to ocpctl with full feature parity. ROSA is a fully-managed OpenShift service where AWS manages the control plane, fundamentally differing from IPI OpenShift.

**User Requirements:**
- Full feature parity (provisioning, destruction, machine pool scaling, cost tracking)
- ROSA Classic only (skip HCP for now)
- New ClusterType: `rosa` (dedicated cluster type)
- AWS STS authentication with auto-mode (rosa creates IAM roles automatically)

**Key Architectural Differences: ROSA vs IPI OpenShift**

| Feature | IPI OpenShift | ROSA |
|---------|--------------|------|
| Installation | `openshift-install` | `rosa` CLI |
| Control Plane | User-managed EC2 instances | AWS-managed (invisible) |
| Hibernation | Stop EC2 instances | Scale machine pools to 0* |
| Scaling | MachineSet/MachinePool CRDs | `rosa edit machinepool` |
| IAM | CCO creates roles | rosa auto-creates operator roles |
| Cost | EC2 + EBS + LB | $0.03/hr control plane + workers |
| Cleanup | `openshift-install destroy` + CCO | `rosa delete cluster` (handles all) |

*Control plane cannot be stopped (AWS-managed), so hibernated cost is $0.03/hr vs $0 for IPI

---

## Phase 1: Type System & Database

### 1.1 Add ClusterTypeROSA Constant

**File**: `pkg/types/cluster.go`

**Add to ClusterType enum:**
```go
const (
    ClusterTypeOpenShift ClusterType = "openshift" // OpenShift IPI (self-managed)
    ClusterTypeROSA      ClusterType = "rosa"      // Red Hat OpenShift Service on AWS (managed)
    ClusterTypeEKS       ClusterType = "eks"       // AWS Elastic Kubernetes Service
    ClusterTypeIKS       ClusterType = "iks"       // IBM Cloud Kubernetes Service
    ClusterTypeGKE       ClusterType = "gke"       // Google Kubernetes Engine
)
```

**Rationale**: ROSA requires dedicated cluster type because:
- Different tooling (`rosa` vs `openshift-install`)
- Different lifecycle (no true hibernation, different scaling)
- Different resource model (AWS-managed control plane)
- Different cost structure (service fee + compute)

### 1.2 Database Migration

**New Migration**: `migrations/XXX_add_rosa_support.sql`

```sql
-- Add ROSA cluster type to enum
ALTER TYPE cluster_type ADD VALUE IF NOT EXISTS 'rosa';

-- Add machine pool metadata column for storing replica counts
ALTER TABLE clusters ADD COLUMN IF NOT EXISTS machine_pool_metadata JSONB;

-- Index for faster queries
CREATE INDEX IF NOT EXISTS idx_clusters_machine_pool_metadata
ON clusters USING GIN (machine_pool_metadata);
```

**Purpose**: Store ROSA machine pool state for resume operations:
```json
{
  "machine_pools": [
    {
      "name": "worker",
      "original_replicas": 3,
      "current_replicas": 0,
      "instance_type": "m5.xlarge"
    }
  ],
  "last_scaled_at": "2026-05-02T10:30:00Z"
}
```

---

## Phase 2: ROSA Installer Implementation

### 2.1 Create ROSA Installer Wrapper

**New File**: `internal/installer/rosa.go`

**Pattern**: Follow existing installer wrappers:
- `internal/installer/eksctl.go` - EKS cluster management
- `internal/installer/ibmcloud.go` - IBM Cloud clusters

**Key Components:**

```go
package installer

import (
    "context"
    "encoding/json"
    "fmt"
    "os/exec"
    "time"
)

// ROSAInstaller wraps the rosa CLI
type ROSAInstaller struct {
    binaryPath string
    timeout    time.Duration
}

// ROSAClusterConfig represents rosa cluster configuration
type ROSAClusterConfig struct {
    Name              string
    Region            string
    Version           string
    ComputeMachineType string
    ComputeNodes      int
    MultiAZ           bool
    PrivateLink       bool
    Tags              map[string]string
}

// ROSAClusterInfo represents cluster info from 'rosa describe cluster'
type ROSAClusterInfo struct {
    ID      string `json:"id"`
    Name    string `json:"name"`
    State   string `json:"state"` // "ready", "installing", "error", "uninstalling"
    API     struct {
        URL string `json:"url"`
    } `json:"api"`
    Console struct {
        URL string `json:"url"`
    } `json:"console"`
    Region  string `json:"region"`
    Version string `json:"version"`
}

// ROSAMachinePool represents a ROSA machine pool
type ROSAMachinePool struct {
    ID           string            `json:"id"`
    Replicas     int               `json:"replicas"`
    InstanceType string            `json:"instance_type"`
    Labels       map[string]string `json:"labels"`
}

func NewROSAInstaller() *ROSAInstaller {
    return &ROSAInstaller{
        binaryPath: "/usr/local/bin/rosa",
        timeout:    90 * time.Minute,
    }
}

func (r *ROSAInstaller) CreateCluster(ctx context.Context, config *ROSAClusterConfig) (string, error) {
    // Build rosa create cluster command with STS auto-mode
    args := []string{
        "create", "cluster",
        "--cluster-name", config.Name,
        "--region", config.Region,
        "--version", config.Version,
        "--compute-machine-type", config.ComputeMachineType,
        "--compute-nodes", fmt.Sprintf("%d", config.ComputeNodes),
        "--sts",           // STS authentication
        "--mode", "auto",  // Auto-create IAM roles
        "--yes",           // Skip confirmation
    }

    if config.MultiAZ {
        args = append(args, "--multi-az")
    }

    if config.PrivateLink {
        args = append(args, "--private-link")
    }

    // Add tags
    for k, v := range config.Tags {
        args = append(args, "--tags", fmt.Sprintf("%s=%s", k, v))
    }

    cmd := exec.CommandContext(ctx, r.binaryPath, args...)
    output, err := cmd.CombinedOutput()
    if err != nil {
        return "", fmt.Errorf("rosa create cluster failed: %w (output: %s)", err, output)
    }

    return string(output), nil
}

func (r *ROSAInstaller) DestroyCluster(ctx context.Context, clusterName string) (string, error) {
    cmd := exec.CommandContext(ctx, r.binaryPath,
        "delete", "cluster",
        "--cluster", clusterName,
        "--yes",
    )

    output, err := cmd.CombinedOutput()
    if err != nil {
        // Check if cluster not found (already deleted)
        if strings.Contains(string(output), "not found") {
            return string(output), nil
        }
        return "", fmt.Errorf("rosa delete cluster failed: %w (output: %s)", err, output)
    }

    return string(output), nil
}

func (r *ROSAInstaller) DescribeCluster(ctx context.Context, clusterName string) (*ROSAClusterInfo, error) {
    cmd := exec.CommandContext(ctx, r.binaryPath,
        "describe", "cluster",
        "--cluster", clusterName,
        "--output", "json",
    )

    output, err := cmd.Output()
    if err != nil {
        return nil, fmt.Errorf("rosa describe cluster failed: %w", err)
    }

    var info ROSAClusterInfo
    if err := json.Unmarshal(output, &info); err != nil {
        return nil, fmt.Errorf("parse cluster info: %w", err)
    }

    return &info, nil
}

func (r *ROSAInstaller) ListMachinePools(ctx context.Context, clusterName string) ([]ROSAMachinePool, error) {
    cmd := exec.CommandContext(ctx, r.binaryPath,
        "list", "machinepools",
        "--cluster", clusterName,
        "--output", "json",
    )

    output, err := cmd.Output()
    if err != nil {
        return nil, fmt.Errorf("rosa list machinepools failed: %w", err)
    }

    var pools []ROSAMachinePool
    if err := json.Unmarshal(output, &pools); err != nil {
        return nil, fmt.Errorf("parse machine pools: %w", err)
    }

    return pools, nil
}

func (r *ROSAInstaller) ScaleMachinePool(ctx context.Context, clusterName, poolName string, replicas int) error {
    cmd := exec.CommandContext(ctx, r.binaryPath,
        "edit", "machinepool",
        "--cluster", clusterName,
        "--replicas", fmt.Sprintf("%d", replicas),
        poolName,
    )

    output, err := cmd.CombinedOutput()
    if err != nil {
        return fmt.Errorf("rosa scale machinepool failed: %w (output: %s)", err, output)
    }

    return nil
}

func (r *ROSAInstaller) WaitForClusterReady(ctx context.Context, clusterName string, timeout time.Duration) error {
    ctx, cancel := context.WithTimeout(ctx, timeout)
    defer cancel()

    ticker := time.NewTicker(30 * time.Second)
    defer ticker.Stop()

    for {
        select {
        case <-ctx.Done():
            return fmt.Errorf("timeout waiting for cluster ready: %w", ctx.Err())
        case <-ticker.C:
            info, err := r.DescribeCluster(ctx, clusterName)
            if err != nil {
                log.Printf("Error describing cluster: %v", err)
                continue
            }

            if info.State == "ready" {
                return nil
            }

            if info.State == "error" || info.State == "uninstalling" {
                return fmt.Errorf("cluster entered error state: %s", info.State)
            }

            log.Printf("Cluster state: %s (waiting for ready)", info.State)
        }
    }
}
```

**Critical rosa CLI Commands:**
```bash
# Create cluster (auto-mode STS)
rosa create cluster --cluster-name=<name> --region=<region> \
  --compute-machine-type=<type> --compute-nodes=<count> \
  --sts --mode=auto --yes

# Delete cluster
rosa delete cluster --cluster=<name> --yes

# Describe cluster (JSON)
rosa describe cluster --cluster=<name> --output=json

# List machine pools
rosa list machinepools --cluster=<name> --output=json

# Scale machine pool
rosa edit machinepool --cluster=<name> --replicas=<count> <pool-name>
```

---

## Phase 3: Worker Handlers

### 3.1 Create Handler

**File**: `internal/worker/handler_create.go`

**Modification**: Add ROSA case to `Handle()` method (around line 70)

```go
func (h *CreateHandler) Handle(ctx context.Context, job *types.Job) error {
    cluster, err := h.store.Clusters.GetByID(ctx, job.ClusterID)
    // ... existing code

    switch cluster.ClusterType {
    case types.ClusterTypeOpenShift:
        return h.handleOpenShiftCreate(ctx, job, cluster)
    case types.ClusterTypeROSA:  // NEW
        return h.handleROSACreate(ctx, job, cluster)
    case types.ClusterTypeEKS:
        return h.handleEKSCreate(ctx, job, cluster)
    // ... other cases
    }
}
```

**New Method**: `handleROSACreate(ctx context.Context, job *types.Job, cluster *types.Cluster) error`

**Pattern**: Follow `handleEKSCreate()` which demonstrates:
- Creating work directory
- Loading profile
- Building cluster config
- Running external CLI
- Log streaming
- Polling for completion
- Extracting outputs

**Implementation:**

```go
func (h *CreateHandler) handleROSACreate(ctx context.Context, job *types.Job, cluster *types.Cluster) error {
    log.Printf("[JOB %s] Starting ROSA cluster creation: %s", job.ID, cluster.Name)

    // Update status to CREATING
    if err := h.store.Clusters.UpdateStatus(ctx, nil, cluster.ID, types.ClusterStatusCreating); err != nil {
        return fmt.Errorf("update status: %w", err)
    }

    // Load profile
    prof, err := h.profileRegistry.Get(cluster.Profile)
    if err != nil {
        return fmt.Errorf("load profile: %w", err)
    }

    // Build ROSA config from profile
    rosaConfig := &installer.ROSAClusterConfig{
        Name:              cluster.Name,
        Region:            cluster.Region,
        Version:           prof.OpenshiftVersions.Default,
        ComputeMachineType: prof.PlatformConfig.ROSA.ComputeMachineType,
        ComputeNodes:      prof.PlatformConfig.ROSA.ComputeNodes,
        MultiAZ:           prof.PlatformConfig.ROSA.MultiAZ,
        PrivateLink:       prof.PlatformConfig.ROSA.PrivateLink,
        Tags:              cluster.EffectiveTags,
    }

    // Create installer
    inst := installer.NewROSAInstaller()

    // Start log streaming
    logWriter := NewJobLogWriter(h.store, job.ID)
    defer logWriter.Close()

    // Run rosa create cluster
    logWriter.Write([]byte("Creating ROSA cluster with STS auto-mode...\n"))
    output, err := inst.CreateCluster(ctx, rosaConfig)
    if err != nil {
        logWriter.Write([]byte(fmt.Sprintf("ERROR: %v\n", err)))
        return fmt.Errorf("create cluster: %w", err)
    }
    logWriter.Write([]byte(output))

    // Wait for cluster ready (30-40 minutes typical)
    logWriter.Write([]byte("Waiting for cluster to be ready (this takes 30-40 minutes)...\n"))
    if err := inst.WaitForClusterReady(ctx, cluster.Name, 60*time.Minute); err != nil {
        return fmt.Errorf("wait for ready: %w", err)
    }

    // Get cluster info
    info, err := inst.DescribeCluster(ctx, cluster.Name)
    if err != nil {
        return fmt.Errorf("describe cluster: %w", err)
    }

    // Save cluster outputs
    outputs := &types.ClusterOutputs{
        ID:         uuid.New().String(),
        ClusterID:  cluster.ID,
        APIURL:     &info.API.URL,
        ConsoleURL: &info.Console.URL,
        CreatedAt:  time.Now(),
        UpdatedAt:  time.Now(),
    }

    if err := h.store.ClusterOutputs.Upsert(ctx, outputs); err != nil {
        return fmt.Errorf("save outputs: %w", err)
    }

    // Update status to READY
    if err := h.store.Clusters.UpdateStatus(ctx, nil, cluster.ID, types.ClusterStatusReady); err != nil {
        return fmt.Errorf("update status: %w", err)
    }

    log.Printf("[JOB %s] ROSA cluster creation completed successfully", job.ID)

    // Trigger post-deployment if configured
    if !cluster.SkipPostDeployment {
        return h.createPostDeploymentJob(ctx, cluster)
    }

    return nil
}
```

**Key Differences from IPI**:
- Uses `rosa` CLI instead of `openshift-install`
- No kubeconfig/kubeadmin files (ROSA uses AWS IAM auth)
- Polling for completion (rosa returns immediately)
- No infraID metadata needed

### 3.2 Destroy Handler

**File**: `internal/worker/handler_destroy.go`

**Modification**: Add ROSA case to `Handle()` method (around line 67)

```go
case types.ClusterTypeROSA:
    return h.handleROSADestroy(ctx, job, cluster)
```

**New Method**: `handleROSADestroy(ctx context.Context, job *types.Job, cluster *types.Cluster) error`

**Implementation:**

```go
func (h *DestroyHandler) handleROSADestroy(ctx context.Context, job *types.Job, cluster *types.Cluster) error {
    log.Printf("[JOB %s] Starting ROSA cluster destruction: %s", job.ID, cluster.Name)

    // Create installer
    inst := installer.NewROSAInstaller()

    // Start log streaming
    logWriter := NewJobLogWriter(h.store, job.ID)
    defer logWriter.Close()

    // Run rosa delete cluster
    logWriter.Write([]byte("Deleting ROSA cluster...\n"))
    output, err := inst.DestroyCluster(ctx, cluster.Name)
    logWriter.Write([]byte(output))

    if err != nil {
        logWriter.Write([]byte(fmt.Sprintf("ERROR: %v\n", err)))
        return fmt.Errorf("destroy cluster: %w", err)
    }

    logWriter.Write([]byte("ROSA cluster deleted successfully\n"))
    return nil
}
```

**Key Points**:
- Much simpler than IPI (no CCO cleanup needed)
- rosa delete cluster handles all cleanup:
  - VPC, subnets, security groups
  - Load balancers
  - IAM operator roles and account roles
  - OIDC provider and S3 bucket
  - Machine pools
- Handles "not found" error gracefully (already deleted)

### 3.3 Hibernate Handler

**File**: `internal/worker/handler_hibernate.go`

**Modification**: Add ROSA case to `Handle()` method (around line 62)

```go
case types.ClusterTypeROSA:
    return h.hibernateROSA(ctx, cluster, job)
```

**New Method**: `hibernateROSA(ctx context.Context, cluster *types.Cluster, job *types.Job) error`

**Pattern**: Follow `hibernateEKS()` which scales node groups to 0

**Implementation:**

```go
func (h *HibernateHandler) hibernateROSA(ctx context.Context, cluster *types.Cluster, job *types.Job) error {
    log.Printf("[JOB %s] Starting ROSA hibernation (scaling machine pools to 0)", job.ID)

    inst := installer.NewROSAInstaller()

    // List all machine pools
    pools, err := inst.ListMachinePools(ctx, cluster.Name)
    if err != nil {
        return fmt.Errorf("list machine pools: %w", err)
    }

    // Store original replica counts
    machinePoolMetadata := make([]map[string]interface{}, 0)
    for _, pool := range pools {
        machinePoolMetadata = append(machinePoolMetadata, map[string]interface{}{
            "id":               pool.ID,
            "original_replicas": pool.Replicas,
            "instance_type":    pool.InstanceType,
        })

        // Scale to 0
        if err := inst.ScaleMachinePool(ctx, cluster.Name, pool.ID, 0); err != nil {
            return fmt.Errorf("scale machine pool %s: %w", pool.ID, err)
        }

        log.Printf("[JOB %s] Scaled machine pool %s to 0 (was %d)", job.ID, pool.ID, pool.Replicas)
    }

    // Save metadata for resume
    job.Metadata["machine_pool_metadata"] = machinePoolMetadata
    if err := h.store.Jobs.UpdateMetadata(ctx, job.ID, job.Metadata); err != nil {
        return fmt.Errorf("save metadata: %w", err)
    }

    log.Printf("[JOB %s] ROSA hibernation completed (control plane still running at $0.03/hr)", job.ID)
    return nil
}
```

**Critical Note**:
- ROSA control plane CANNOT be stopped (AWS-managed)
- Hibernated cost: $0.03/hr (control plane only)
- Savings: ~93% (workers stopped, control plane continues)

### 3.4 Resume Handler

**File**: `internal/worker/handler_resume.go`

**Modification**: Add ROSA case to `Handle()` method (around line 68)

```go
case types.ClusterTypeROSA:
    return h.resumeROSA(ctx, cluster, job)
```

**New Method**: `resumeROSA(ctx context.Context, cluster *types.Cluster, job *types.Job) error`

**Implementation:**

```go
func (h *ResumeHandler) resumeROSA(ctx context.Context, cluster *types.Cluster, job *types.Job) error {
    log.Printf("[JOB %s] Starting ROSA resume (scaling machine pools back up)", job.ID)

    // Get last successful HIBERNATE job
    hibernateJob, err := h.store.Jobs.GetMostRecentByTypeAndStatus(
        ctx, cluster.ID, types.JobTypeHibernate, types.JobStatusSucceeded)
    if err != nil {
        return fmt.Errorf("get hibernate job: %w", err)
    }

    // Parse machine pool metadata
    machinePoolMetadata, ok := hibernateJob.Metadata["machine_pool_metadata"].([]interface{})
    if !ok {
        return fmt.Errorf("invalid machine_pool_metadata format")
    }

    inst := installer.NewROSAInstaller()

    // Restore each machine pool
    for _, poolData := range machinePoolMetadata {
        pool := poolData.(map[string]interface{})
        poolID := pool["id"].(string)
        originalReplicas := int(pool["original_replicas"].(float64))

        if err := inst.ScaleMachinePool(ctx, cluster.Name, poolID, originalReplicas); err != nil {
            return fmt.Errorf("scale machine pool %s: %w", poolID, err)
        }

        log.Printf("[JOB %s] Restored machine pool %s to %d replicas", job.ID, poolID, originalReplicas)
    }

    // Wait for nodes to provision (~2-5 minutes)
    time.Sleep(2 * time.Minute)

    // Verify cluster is ready
    info, err := inst.DescribeCluster(ctx, cluster.Name)
    if err != nil {
        return fmt.Errorf("describe cluster: %w", err)
    }

    if info.State != "ready" {
        return fmt.Errorf("cluster not ready after resume: %s", info.State)
    }

    log.Printf("[JOB %s] ROSA resume completed successfully", job.ID)
    return nil
}
```

---

## Phase 4: Profile Definitions

### 4.1 ROSA Minimal Profile

**New File**: `internal/profile/definitions/aws-rosa-minimal.yaml`

**Pattern**: Follow `internal/profile/definitions/eks-standard.yaml`

```yaml
name: aws-rosa-minimal
description: "ROSA (Red Hat OpenShift Service on AWS) - Minimal development cluster"
platform: aws
clusterType: rosa
enabled: true
requiresBaseDomain: false  # ROSA uses AWS-managed DNS

# OpenShift versions for ROSA
openshiftVersions:
  allowlist:
    - "4.18.35"
    - "4.19.23"
    - "4.20.3"
    - "4.21.8"
  default: "4.20.3"

# No base domains for ROSA
baseDomains:
  allowlist: []
  default: ""

# Worker node configuration
compute:
  workers:
    replicas: 2
    minReplicas: 0   # Can scale to 0 for hibernation
    maxReplicas: 10
    instanceType: m5.xlarge

# ROSA-specific configuration
platformConfig:
  rosa:
    sts: true           # STS authentication (required)
    mode: auto          # Auto-create IAM roles
    multiAZ: false      # Single-AZ for cost savings
    privateLink: false  # No AWS PrivateLink
    computeMachineType: m5.xlarge
    computeNodes: 2

# Supported AWS regions
supportedRegions:
  - us-east-1
  - us-east-2
  - us-west-2
  - eu-west-1
  - eu-central-1
  - ap-southeast-1

# Cost controls
costControls:
  estimatedHourlyCost: 0.414  # $0.03 control plane + (2 × $0.192 m5.xlarge)
  maxTTLHours: 168             # 1 week max
  defaultTTLHours: 72          # 3 days default
  maxMonthlyCost: 299          # ~$299/month

# Resource limits
resourceLimits:
  maxClustersPerUser: 3
  maxTotalClusters: 50

# Default tags
defaultTags:
  managed-by: ocpctl
  platform: aws
  cluster-type: rosa
```

### 4.2 ROSA Standard Profile

**New File**: `internal/profile/definitions/aws-rosa-standard.yaml`

```yaml
name: aws-rosa-standard
description: "ROSA - Standard production-ready cluster with high availability"
platform: aws
clusterType: rosa
enabled: true
requiresBaseDomain: false

openshiftVersions:
  allowlist:
    - "4.18.35"
    - "4.19.23"
    - "4.20.3"
    - "4.21.8"
  default: "4.20.3"

baseDomains:
  allowlist: []
  default: ""

compute:
  workers:
    replicas: 3
    minReplicas: 0
    maxReplicas: 20
    instanceType: m5.2xlarge

platformConfig:
  rosa:
    sts: true
    mode: auto
    multiAZ: true       # Multi-AZ for HA
    privateLink: false
    computeMachineType: m5.2xlarge
    computeNodes: 3

supportedRegions:
  - us-east-1
  - us-east-2
  - us-west-2
  - eu-west-1
  - eu-central-1
  - ap-southeast-1

costControls:
  estimatedHourlyCost: 1.188  # $0.03 + (3 × $0.384 m5.2xlarge) + $0.135 NAT gateways
  maxTTLHours: 336            # 2 weeks max
  defaultTTLHours: 168        # 1 week default
  maxMonthlyCost: 858         # ~$858/month

resourceLimits:
  maxClustersPerUser: 2
  maxTotalClusters: 30

defaultTags:
  managed-by: ocpctl
  platform: aws
  cluster-type: rosa
  tier: production
```

### 4.3 Profile Type System Updates

**File**: `internal/profile/profile.go`

**Add ROSAConfig struct:**

```go
type PlatformConfig struct {
    AWS      *AWSConfig      `yaml:"aws,omitempty"`
    IBMCloud *IBMCloudConfig `yaml:"ibmcloud,omitempty"`
    GCP      *GCPConfig      `yaml:"gcp,omitempty"`
    ROSA     *ROSAConfig     `yaml:"rosa,omitempty"` // NEW
    EKS      *EKSConfig      `yaml:"eks,omitempty"`
    IKS      *IKSConfig      `yaml:"iks,omitempty"`
    GKE      *GKEConfig      `yaml:"gke,omitempty"`
}

// ROSAConfig holds ROSA-specific configuration
type ROSAConfig struct {
    STS                bool   `yaml:"sts"`                // Always true for ROSA
    Mode               string `yaml:"mode"`               // "auto" (rosa creates IAM roles)
    MultiAZ            bool   `yaml:"multiAZ"`            // Multi-AZ deployment
    PrivateLink        bool   `yaml:"privateLink"`        // AWS PrivateLink
    ComputeMachineType string `yaml:"computeMachineType"` // Worker instance type
    ComputeNodes       int    `yaml:"computeNodes"`       // Number of workers
}
```

---

## Phase 5: API & Frontend Updates

### 5.1 API Validation

**File**: `internal/api/handler_clusters.go`

**Update `Create()` method validation (around line 147):**

```go
// Custom validation: base_domain required for OpenShift IPI, not for ROSA
if req.ClusterType == "openshift" && req.BaseDomain == "" {
    return ErrorBadRequest(c, "base_domain is required for OpenShift clusters")
}

// ROSA doesn't use base_domain (AWS-managed DNS)
if req.ClusterType == "rosa" && req.BaseDomain != "" {
    return ErrorBadRequest(c, "base_domain is not supported for ROSA clusters (AWS-managed DNS)")
}

// Platform/ClusterType validation
if req.Platform == "aws" {
    validTypes := []string{"openshift", "rosa", "eks"}
    if !contains(validTypes, req.ClusterType) {
        return ErrorBadRequest(c, "AWS platform only supports: openshift, rosa, eks")
    }
}
```

**Update `CreateClusterRequest` struct (line 72):**

```go
type CreateClusterRequest struct {
    Name        string `json:"name" validate:"required,min=3,max=63,cluster_name"`
    Platform    string `json:"platform" validate:"required,oneof=aws ibmcloud gcp"`
    ClusterType string `json:"cluster_type" validate:"required,oneof=openshift rosa eks iks gke"`
    // ... rest of fields
}
```

### 5.2 Frontend Updates

**File**: `web/app/(dashboard)/clusters/create/page.tsx`

**Update cluster type selector:**

```typescript
const getClusterTypeOptions = (platform: string) => {
  const options = {
    aws: [
      { value: 'openshift', label: 'OpenShift (IPI - Self-Managed)', description: 'Full control over control plane and workers' },
      { value: 'rosa', label: 'ROSA (Managed OpenShift)', description: 'AWS-managed control plane, auto-scaling workers' },
      { value: 'eks', label: 'Amazon EKS', description: 'Managed Kubernetes service' },
    ],
    gcp: [
      { value: 'openshift', label: 'OpenShift (IPI)', description: 'Self-managed OpenShift' },
      { value: 'gke', label: 'Google Kubernetes Engine', description: 'Managed Kubernetes' },
    ],
    ibmcloud: [
      { value: 'openshift', label: 'OpenShift (IPI)', description: 'Self-managed OpenShift' },
      { value: 'iks', label: 'IBM Cloud Kubernetes Service', description: 'Managed Kubernetes' },
    ],
  };

  return options[platform] || [];
};
```

**Form behavior:**
- When `cluster_type === 'rosa'`: Hide base_domain field
- Show hibernation disclaimer: "Note: ROSA hibernation scales workers to 0. Control plane remains running at $0.03/hr."
- Show Multi-AZ toggle (from profile)
- Show PrivateLink toggle (from profile)

**File**: `web/lib/api/endpoints/clusters.ts`

**No changes needed** - existing types support platform and cluster_type

---

## Phase 6: Cost Tracking

### 6.1 ROSA Cost Calculator

**File**: `internal/api/handler_clusters.go`

**Update `calculateEffectiveCost()` method (around line 2199):**

```go
func (h *ClusterHandler) calculateEffectiveCost(cluster *types.Cluster, prof *profile.Profile) float64 {
    baseCost := prof.CostControls.EstimatedHourlyCost

    // ROSA-specific cost calculation
    if cluster.ClusterType == types.ClusterTypeROSA {
        // Control plane: $0.03/hour (always running, even when hibernated)
        controlPlaneCost := 0.03

        // If hibernated, only control plane cost
        if cluster.Status == types.ClusterStatusHibernated {
            return controlPlaneCost
        }

        // Normal operation: control plane + workers
        // (baseCost already includes both from profile)
        return baseCost
    }

    // If cluster is hibernated, calculate reduced cost based on cluster type
    if cluster.Status == types.ClusterStatusHibernated {
        switch cluster.ClusterType {
        case types.ClusterTypeOpenShift:
            // OpenShift: All instances stopped, only storage remains (~10%)
            return baseCost * 0.10
        case types.ClusterTypeEKS:
            // EKS: Control plane still runs at $0.10/hr
            return 0.10
        // ... other cluster types
        }
    }

    // For all other states, use full cost
    return baseCost
}
```

**Key Points:**
1. ROSA control plane costs $0.03/hr (cannot be stopped)
2. Hibernated ROSA = $0.03/hr (control plane only)
3. Normal ROSA = $0.03/hr + worker costs
4. Multi-AZ adds NAT gateway overhead (~$0.135/hr)

---

## Phase 7: Testing Strategy

### 7.1 Unit Tests

**New Test File**: `internal/installer/rosa_test.go`

**Test Coverage:**
- `TestROSAInstaller_CreateCluster`: Mock rosa CLI, verify args
- `TestROSAInstaller_DestroyCluster`: Test deletion flow
- `TestROSAInstaller_ScaleMachinePool`: Test scaling logic
- `TestROSAInstaller_WaitForClusterReady`: Test polling behavior

### 7.2 Integration Tests

**Test Scenarios:**
1. Create ROSA cluster → Verify rosa CLI called with correct args
2. Hibernate → Verify machine pools scaled to 0
3. Resume → Verify machine pools scaled back up
4. Destroy → Verify rosa delete cluster successful
5. Cost calculation → Verify control plane + worker costs

### 7.3 End-to-End Test Plan

**Manual Testing Checklist:**
- [ ] Create aws-rosa-minimal cluster
- [ ] Verify cluster reaches READY status (30-40 minutes)
- [ ] Verify API URL and Console URL accessible
- [ ] Hibernate cluster
- [ ] Verify cost drops to $0.03/hr (control plane only)
- [ ] Verify workers scaled to 0
- [ ] Resume cluster
- [ ] Verify workers scaled back up
- [ ] Verify cluster becomes accessible
- [ ] Test post-deployment configuration
- [ ] Destroy cluster
- [ ] Verify all AWS resources deleted (IAM roles, OIDC, VPC)

---

## Phase 8: Deployment & Prerequisites

### 8.1 Infrastructure Requirements

**rosa CLI Installation** on worker nodes:
```bash
# Download rosa CLI
curl -L https://mirror.openshift.com/pub/openshift-v4/clients/rosa/latest/rosa-linux.tar.gz | tar xvz

# Install to system path
sudo mv rosa /usr/local/bin/rosa
sudo chmod +x /usr/local/bin/rosa

# Verify installation
rosa version
```

**AWS Permissions** for rosa CLI:
- Full ROSA permissions (rosa CLI documents exact IAM policy needed)
- STS permissions: `sts:AssumeRole`
- IAM permissions: `iam:CreateRole`, `iam:DeleteRole`, `iam:TagRole`
- IAM OIDC: `iam:CreateOpenIDConnectProvider`, `iam:DeleteOpenIDConnectProvider`
- EC2: Full permissions for VPC, instances, load balancers
- ELB: Full permissions for load balancers

**Environment Variables:**
```bash
export ROSA_BINARY=/usr/local/bin/rosa
export AWS_REGION=us-east-1
# AWS credentials already configured via instance profile or AWS_ACCESS_KEY_ID/AWS_SECRET_ACCESS_KEY
```

### 8.2 Database Migration

**Apply Migration:**
```bash
# Production
psql $DATABASE_URL -f migrations/XXX_add_rosa_support.sql

# Development
make migrate
```

### 8.3 Deployment Checklist

**Pre-deployment:**
- [ ] Install rosa CLI on worker nodes
- [ ] Verify AWS permissions
- [ ] Test rosa CLI manually
- [ ] Run database migration

**Deployment:**
- [ ] Deploy backend with ROSA support
- [ ] Restart workers to pick up new handlers
- [ ] Deploy frontend with ROSA cluster type option
- [ ] Verify profile registry loads ROSA profiles

**Post-deployment:**
- [ ] Create test ROSA cluster
- [ ] Monitor cluster creation logs
- [ ] Verify hibernation/resume works
- [ ] Test destruction cleanup
- [ ] Monitor AWS costs

---

## Implementation Sequence

**Recommended Implementation Order:**

### Week 1: Core Infrastructure
- **Day 1**: Type system (ClusterTypeROSA constant)
- **Day 2**: Database migration
- **Day 3**: ROSA installer wrapper (rosa.go)
- **Day 4-5**: Unit tests for installer

### Week 2: Worker Handlers
- **Day 1-2**: Create handler (handleROSACreate)
- **Day 3**: Destroy handler (handleROSADestroy)
- **Day 4**: Hibernate handler (hibernateROSA)
- **Day 5**: Resume handler (resumeROSA)

### Week 3: Profiles & API
- **Day 1**: ROSA profile definitions (YAML files)
- **Day 2**: Profile type system updates (ROSAConfig)
- **Day 3**: API validation updates
- **Day 4**: Cost calculation for ROSA
- **Day 5**: Integration tests

### Week 4: Frontend & Testing
- **Day 1-2**: Frontend cluster creation form updates
- **Day 3**: Frontend cluster detail page updates
- **Day 4**: End-to-end testing
- **Day 5**: Documentation

### Week 5: Deployment
- **Day 1**: Deploy to staging
- **Day 2**: Smoke testing in staging
- **Day 3**: Deploy to production
- **Day 4**: Monitor first production ROSA clusters
- **Day 5**: User documentation and training

**Total Time**: 5 weeks (25 working days)

---

## Success Criteria

Implementation is complete when:

- [ ] Users can create ROSA clusters from frontend
- [ ] ROSA clusters reach READY status (30-40 minutes)
- [ ] Hibernation reduces cost from $0.41/hr to $0.03/hr (93% worker savings)
- [ ] Resume restores cluster to full functionality
- [ ] Destruction removes all AWS resources (verified via AWS console)
- [ ] Cost tracking accurately reflects ROSA pricing model
- [ ] Post-deployment configuration works with ROSA
- [ ] Admin dashboard shows ROSA cluster statistics
- [ ] Frontend clearly differentiates ROSA from IPI OpenShift
- [ ] Documentation explains ROSA vs IPI trade-offs

---

## Critical Files for Implementation

**Top 5 Most Critical Files:**

1. **`pkg/types/cluster.go`**
   - Add ClusterTypeROSA constant
   - Foundation for all ROSA support

2. **`internal/installer/rosa.go`** (NEW)
   - ROSA installer wrapper
   - Core rosa CLI integration

3. **`internal/worker/handler_create.go`**
   - Add handleROSACreate() method
   - Main cluster provisioning logic

4. **`internal/worker/handler_hibernate.go`**
   - Add hibernateROSA() method
   - Cost-saving machine pool scaling

5. **`internal/profile/definitions/aws-rosa-minimal.yaml`** (NEW)
   - ROSA cluster configuration template
   - User-facing profile definition

---

## Key Design Decisions

### Why add ClusterTypeROSA instead of using profile flag?
- **Clarity**: Explicit in UI, database, and code
- **Routing**: Cleaner handler routing logic
- **Validation**: Type-specific validation rules
- **Future-proofing**: Easier to add ROSA HCP later

### Why use machine pool scaling instead of true hibernation?
- **Technical limitation**: ROSA control plane is AWS-managed (cannot be stopped)
- **Cost savings**: Still achieves 93% cost reduction (workers are 93% of cost)
- **User experience**: Functionally similar to hibernation (cluster unavailable, costs reduced)

### Why STS auto-mode instead of manual mode?
- **Simplicity**: rosa creates IAM roles automatically
- **Reliability**: No manual role setup errors
- **Multi-tenancy**: Each cluster gets isolated roles
- **Security**: Least-privilege principle enforced

### Why separate profiles for ROSA instead of reusing OpenShift profiles?
- **Different requirements**: No base_domain, different cost model
- **Clear separation**: Users understand ROSA vs IPI choice
- **Profile validation**: Different validation rules
- **Cost accuracy**: ROSA-specific cost estimates

---

## ROSA vs IPI Trade-offs

**Advantages of ROSA:**
- **Managed control plane**: No control plane maintenance
- **Faster provisioning**: 30-40 minutes vs 45-60 minutes
- **Automatic updates**: AWS handles control plane updates
- **SLA**: AWS-backed uptime SLA

**Disadvantages of ROSA:**
- **Higher cost**: $0.03/hr control plane fee
- **Cannot stop control plane**: Hibernation only scales workers
- **Less control**: Cannot customize control plane
- **AWS lock-in**: Tied to AWS (no multi-cloud portability)

**When to use ROSA:**
- Production workloads requiring SLA
- Teams without OpenShift expertise
- Quick cluster provisioning needed
- Managed services preferred

**When to use IPI OpenShift:**
- Full control over cluster needed
- Multi-cloud strategy
- Cost optimization priority (can stop everything)
- Custom control plane configuration required

---

## Verification Checklist

After implementation, verify:

- [ ] ROSA clusters create successfully (30-40 minutes)
- [ ] API URL and Console URL accessible
- [ ] Hibernation scales machine pools to 0
- [ ] Cost drops to $0.03/hr when hibernated
- [ ] Resume scales machine pools back up
- [ ] Cluster becomes accessible after resume
- [ ] Destruction removes all AWS resources (IAM, OIDC, VPC)
- [ ] Cost calculation matches actual AWS costs
- [ ] Post-deployment configuration works
- [ ] Profile validation enforces ROSA-specific rules
- [ ] Frontend shows ROSA as separate cluster type
- [ ] Admin dashboard shows ROSA statistics
- [ ] Work hours enforcement works with ROSA
- [ ] TTL-based auto-destruction works
