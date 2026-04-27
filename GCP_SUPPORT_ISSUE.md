# Add GCP (Google Cloud Platform) Support

## Overview
Add support for deploying and managing Kubernetes clusters on Google Cloud Platform, including both OpenShift clusters (via openshift-install) and native GKE (Google Kubernetes Engine) clusters.

## Motivation
Expand platform coverage to support Google Cloud, enabling users to:
- Deploy OpenShift clusters on GCP using openshift-install
- Create and manage GKE clusters
- Test multi-cloud scenarios and migrations
- Manage GCP resource lifecycle (create, destroy, hibernate, resume)
- Track costs across GCP deployments

## Scope

### Core Platform Support

#### 1. Type System Updates (`pkg/types/cluster.go`)
- [ ] Add `PlatformGCP Platform = "gcp"` constant
- [ ] Add `ClusterTypeGKE ClusterType = "gke"` constant
- [ ] Update validation logic to accept GCP platform
- [ ] Update API documentation

#### 2. Profile System (`internal/profile/`)
- [ ] Add `GCPConfig` struct to `types.go`:
  ```go
  type GCPConfig struct {
      Project              string              `yaml:"project"`
      Network              string              `yaml:"network,omitempty"`
      Subnetwork           string              `yaml:"subnetwork,omitempty"`
      ControlPlane         *GCPMachineConfig   `yaml:"controlPlane"`
      Compute              *GCPMachineConfig   `yaml:"compute"`
      ServiceAccount       string              `yaml:"serviceAccount,omitempty"`
      SecureBootPolicy     string              `yaml:"secureBootPolicy,omitempty"`
  }

  type GCPMachineConfig struct {
      MachineType          string  `yaml:"machineType"`
      DiskSizeGB           int     `yaml:"diskSizeGB"`
      DiskType             string  `yaml:"diskType,omitempty"`
  }
  ```
- [ ] Add `GCP *GCPConfig` field to `PlatformConfig` struct
- [ ] Create sample profiles (4.18-gcp-standard, 4.19-gcp-large, etc.)
- [ ] Add profile validation for GCP-specific fields

#### 3. Installer Integration (`internal/installer/`)
- [ ] Create `gke.go` - GKE cluster installer using gcloud CLI
  - [ ] Implement `NewGKEInstaller()` constructor
  - [ ] Implement `CreateCluster()` - create GKE cluster
  - [ ] Implement `DestroyCluster()` - delete GKE cluster
  - [ ] Implement `GetClusterInfo()` - fetch cluster details
  - [ ] Support GKE auto-scaling configuration
  - [ ] Support GKE Workload Identity for OIDC
- [ ] Update `installer.go` (OpenShift) for GCP platform support:
  - [ ] Add GCP credentials management (service account JSON)
  - [ ] Support GCP-specific install-config.yaml generation
  - [ ] Handle GCP resource tagging
  - [ ] Implement GCP metadata extraction

#### 4. Worker Handlers (`internal/worker/`)
- [ ] Create `handler_create_gcp.go`:
  - [ ] OpenShift cluster creation using openshift-install
  - [ ] GKE cluster creation using gcloud
  - [ ] VPC/subnet creation if needed
  - [ ] Service account setup
  - [ ] Resource tagging for cost tracking
- [ ] Create `handler_destroy_gcp.go`:
  - [ ] Cluster deletion
  - [ ] VPC/subnet cleanup
  - [ ] Service account cleanup
  - [ ] Orphaned resource verification
- [ ] Create `handler_hibernate_gcp.go`:
  - [ ] **OpenShift**: Stop all VM instances
  - [ ] **GKE**: Scale node pools to zero
  - [ ] Store instance/node pool metadata for resume
- [ ] Create `handler_resume_gcp.go`:
  - [ ] **OpenShift**: Start VM instances, wait for cluster health
  - [ ] **GKE**: Scale node pools back to original size
  - [ ] Verify cluster accessibility
- [ ] Update `worker.go` to route GCP platform jobs

### Infrastructure Management

#### 5. GCP Authentication
- [ ] Service account JSON key management
  - [ ] Store in environment variable `GOOGLE_APPLICATION_CREDENTIALS`
  - [ ] Support per-project service accounts
- [ ] gcloud CLI integration
  - [ ] Auto-detect gcloud installation
  - [ ] Version compatibility checks
  - [ ] Project selection and activation
- [ ] Workload Identity setup for GKE clusters

#### 6. Networking
- [ ] VPC creation and management
  - [ ] Auto-create VPC per cluster or reuse shared VPC
  - [ ] Subnet allocation
  - [ ] Firewall rule configuration
- [ ] Load balancer integration
  - [ ] TCP/UDP load balancers for OpenShift
  - [ ] HTTP(S) load balancers for ingress
  - [ ] Health check configuration
- [ ] Cloud DNS integration
  - [ ] Automatic DNS zone creation
  - [ ] A/AAAA record management

#### 7. Storage
- [ ] Persistent Disk support
  - [ ] Standard, SSD, Balanced disk types
  - [ ] Storage class discovery and display
  - [ ] Volume snapshot support
- [ ] Google Cloud Storage (GCS) integration
  - [ ] Bucket creation for cluster artifacts
  - [ ] Lifecycle management
- [ ] Filestore integration (optional)
  - [ ] Shared NFS storage for migration testing
  - [ ] Similar to AWS EFS implementation

#### 8. Resource Tagging & Cost Tracking
- [ ] Implement GCP label strategy:
  ```
  managed-by: ocpctl
  cluster-name: <cluster-name>
  profile: <profile-name>
  cluster-id: <cluster-id>
  infra-id: <infra-id>
  created-at: <timestamp>
  ocpctl-version: <version>
  ```
- [ ] Tag all resources:
  - [ ] VM instances
  - [ ] Persistent disks
  - [ ] VPCs and subnets
  - [ ] Load balancers
  - [ ] Service accounts
  - [ ] GCS buckets
  - [ ] Cloud DNS zones
- [ ] Cost tracking integration
  - [ ] Query GCP Billing API for cluster costs
  - [ ] Aggregate by cluster/profile/owner
  - [ ] Display in admin dashboard

#### 9. Orphaned Resource Detection (`internal/janitor/`)
- [ ] Create `orphaned_gcp.go`:
  - [ ] Scan for VM instances with `managed-by=ocpctl` label but no DB entry
  - [ ] Detect orphaned disks
  - [ ] Detect orphaned VPCs
  - [ ] Detect orphaned load balancers
  - [ ] Detect orphaned service accounts
  - [ ] Detect orphaned GCS buckets
  - [ ] Report in admin console
- [ ] Add cleanup automation (optional, with safeguards)

### API & Database

#### 10. API Layer (`internal/api/`)
- [ ] Update `handler_clusters.go`:
  - [ ] Add GCP platform validation in `Create()`
  - [ ] Add GCP region validation
  - [ ] Support GCP-specific parameters
- [ ] Add GCP instance information endpoint:
  - [ ] `GET /clusters/:id/instances` for GCP VMs
  - [ ] Return instance type, zone, status, IP addresses
- [ ] Add GCP storage classes endpoint (similar to current implementation)

#### 11. Database
- [ ] Update validation constraints:
  - [ ] Add "gcp" to platform enum
  - [ ] Add "gke" to cluster_type enum
- [ ] Migration for existing data (if needed)

### Frontend (`web/`)

#### 12. UI Components
- [ ] Update cluster creation form:
  - [ ] Add GCP platform option
  - [ ] Add GCP region selector (us-central1, us-east1, europe-west1, etc.)
  - [ ] Add GCP project ID field
  - [ ] Add GKE vs OpenShift selection
  - [ ] Add GCP-specific profile filters
- [ ] Update cluster detail page:
  - [ ] Display GCP-specific information
  - [ ] Show GKE node pool details
  - [ ] Show GCP VM instance information
  - [ ] Display GCP costs
- [ ] Add GCP instance card component (similar to EC2InstancesCard)

#### 13. Profile Definitions
Create profiles in `internal/profile/definitions/gcp/`:
- [ ] `gke-standard.yaml` - Standard GKE cluster (e2-medium)
- [ ] `gke-large.yaml` - Large GKE cluster (e2-standard-4)
- [ ] `4.18-gcp-standard.yaml` - OpenShift 4.18 on GCP
- [ ] `4.19-gcp-standard.yaml` - OpenShift 4.19 on GCP
- [ ] `4.20-gcp-high-availability.yaml` - HA OpenShift on GCP

### Testing & Documentation

#### 14. Testing
- [ ] Unit tests for GKE installer
- [ ] Unit tests for GCP worker handlers
- [ ] Integration tests for GKE cluster lifecycle
- [ ] Integration tests for OpenShift on GCP
- [ ] Test hibernate/resume workflows
- [ ] Test orphaned resource detection

#### 15. Documentation
- [ ] Update README with GCP support information
- [ ] Create GCP setup guide:
  - [ ] Service account creation
  - [ ] Required API enablement
  - [ ] IAM permissions setup
  - [ ] gcloud CLI installation
- [ ] Document GCP profile configuration
- [ ] Add GCP cost estimation guide
- [ ] Document GKE-specific features

### Dependencies

#### 16. External Tools
- [ ] gcloud CLI (Google Cloud SDK)
  - [ ] Version requirements
  - [ ] Installation script updates
- [ ] kubectl (already present)
- [ ] openshift-install (already present, GCP support in 4.18+)

#### 17. Go Packages
- [ ] `cloud.google.com/go/compute` - Compute Engine API
- [ ] `cloud.google.com/go/container` - GKE API
- [ ] `cloud.google.com/go/storage` - Cloud Storage API
- [ ] `cloud.google.com/go/billing` - Billing API (for cost tracking)
- [ ] `google.golang.org/api/dns/v1` - Cloud DNS API

## Implementation Plan

### Phase 1: Foundation (Week 1-2)
1. Add GCP types and validation
2. Implement GKE installer (basic create/destroy)
3. Add GCP authentication handling
4. Create basic GKE profile

### Phase 2: Core Features (Week 3-4)
5. Implement OpenShift on GCP support
6. Add hibernate/resume handlers for GKE
7. Implement GCP resource tagging
8. Add GCP instance information API

### Phase 3: Advanced Features (Week 5-6)
9. Add orphaned resource detection
10. Implement cost tracking
11. Add networking management (VPC, load balancers)
12. Add storage integration (GCS, Persistent Disk)

### Phase 4: UI & Polish (Week 7-8)
13. Update frontend for GCP support
14. Create comprehensive documentation
15. Add integration tests
16. Performance optimization

## Testing Strategy

### Manual Testing Checklist
- [ ] Create GKE cluster via UI
- [ ] Create OpenShift cluster on GCP via UI
- [ ] Hibernate and resume GKE cluster
- [ ] Hibernate and resume OpenShift cluster
- [ ] Destroy cluster and verify cleanup
- [ ] Verify orphaned resource detection
- [ ] Test cost tracking accuracy
- [ ] Test with multiple GCP projects

### Automated Testing
- [ ] Unit tests for all new handlers
- [ ] Integration tests for cluster lifecycle
- [ ] E2E tests for UI workflows
- [ ] Performance tests for large clusters

## Security Considerations

1. **Service Account Keys**
   - Store securely in environment variables
   - Support key rotation
   - Audit service account usage

2. **API Permissions**
   - Document minimum required IAM permissions
   - Use least-privilege principle
   - Support custom service accounts per cluster

3. **Workload Identity**
   - Use Workload Identity for GKE clusters instead of service account keys
   - Automatic setup during cluster creation

4. **Resource Isolation**
   - Support separate VPCs per cluster
   - Firewall rule best practices
   - Network policy enforcement

## Cost Estimation

Estimated costs for GCP clusters (us-central1):
- **GKE Standard** (3 e2-medium nodes): ~$73/month
- **GKE Large** (3 e2-standard-4 nodes): ~$219/month
- **OpenShift Standard** (similar to AWS): ~$300-400/month

## Open Questions

1. Should we support GCP Autopilot GKE mode?
2. Do we need Cloud Armor integration for DDoS protection?
3. Should we support GCP Anthos for hybrid deployments?
4. What's the priority for Filestore vs basic GCS?
5. Should we implement Cloud NAT for outbound connectivity?

## Success Criteria

- [ ] Users can create GKE clusters via web UI
- [ ] Users can create OpenShift clusters on GCP via web UI
- [ ] Hibernate/resume works reliably for both cluster types
- [ ] All GCP resources are properly tagged for cost tracking
- [ ] Orphaned resources are automatically detected
- [ ] Cost tracking shows accurate GCP spend by cluster
- [ ] Documentation is complete and tested
- [ ] Zero security vulnerabilities in GCP integration

## References

- [GKE Documentation](https://cloud.google.com/kubernetes-engine/docs)
- [OpenShift on GCP Installation](https://docs.openshift.com/container-platform/4.18/installing/installing_gcp/preparing-to-install-on-gcp.html)
- [GCP Best Practices](https://cloud.google.com/docs/enterprise/best-practices-for-enterprise-organizations)
- [gcloud CLI Reference](https://cloud.google.com/sdk/gcloud/reference)
- [Workload Identity Setup](https://cloud.google.com/kubernetes-engine/docs/how-to/workload-identity)
