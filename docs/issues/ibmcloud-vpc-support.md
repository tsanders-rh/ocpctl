# Enhancement: Add IBM Cloud VPC Support

## Summary

Add support for provisioning OpenShift clusters on IBM Cloud VPC using installer-provisioned infrastructure (IPI). This will enable ocpctl to deploy clusters on IBM Cloud alongside the existing AWS support.

## Current Status

- IBM Cloud profiles exist but are disabled (`enabled: false`)
- Profiles: `ibmcloud-standard` and `ibmcloud-minimal-test`
- Comment indicates "Will be enabled in Phase 2"

## Background

### IBM Cloud VPC IPI Support

Based on research as of March 2026:

- **OpenShift 4.20** fully supports IBM Cloud VPC with IPI mode
- **NOT Technology Preview** - Production-ready for versions 4.10+
- Uses `openshift-installer` with `platform: ibmcloud` in install-config.yaml
- Similar workflow to AWS, but with IBM Cloud-specific requirements

### Key Differences from AWS

1. **Credential Management**: Must use Cloud Credential Operator (CCO) in manual mode with `ccoctl`
2. **Authentication**: IBM Cloud API keys instead of AWS access keys
3. **Instance Types**: `bx2-*` family instead of `m6i.*` (e.g., `bx2-4x16` = 4 vCPU, 16GB RAM)
4. **Regions**: Different naming (e.g., `us-south`, `us-east`, `eu-de`)
5. **VPC Resources**: IBM Cloud VPC Gen2 instead of AWS VPC
6. **Object Storage**: IBM Cloud Object Storage (COS) instead of S3

## Implementation Plan

### Phase 1: Infrastructure and Prerequisites

#### 1.1 IBM Cloud SDK and Credentials

**File: `internal/ibmcloud/client.go`**
- Create IBM Cloud SDK client wrapper
- Support authentication via:
  - API key (environment variable `IC_API_KEY`)
  - Service ID credentials
- Implement credential validation

**File: `internal/ibmcloud/credentials.go`**
- Detect if running on IBM Cloud VPC instance (IMDS equivalent)
- Support IAM-based authentication for worker instances
- Validate required IAM permissions

**Required IAM Permissions:**
```
- VPC Infrastructure Services: Administrator
- Cloud Object Storage: Manager
- IAM Identity Service: Operator
- Resource Group: Viewer
```

#### 1.2 CCO and ccoctl Integration

**File: `internal/installer/ccoctl.go`**
- Extend existing ccoctl support for IBM Cloud
- Generate IBM Cloud credential requests
- Create service IDs and API keys for cluster operators

**Process:**
1. Extract CredentialsRequests from OpenShift release image
2. Run `ccoctl ibmcloud create-service-id` for each operator
3. Store generated credentials for cluster installation
4. Clean up service IDs on cluster destruction

**File: `internal/installer/installer.go`**
- Update `NewInstallerForVersion()` to support IBM Cloud
- Verify `ccoctl-{version}` binary exists for IBM Cloud operations

#### 1.3 Install-Config Rendering

**File: `internal/profile/renderer_ibmcloud.go`**
- Create IBM Cloud-specific install-config template
- Support platform-specific fields:
  - `resourceGroupName`: Resource group for cluster resources
  - `region`: IBM Cloud region (e.g., `us-south`)
  - `networkResourceGroupName`: Existing VPC resource group (optional)
  - `vpcName`: Existing VPC name (optional)
  - `controlPlaneSubnets`: Existing subnet IDs (optional)
  - `computeSubnets`: Existing subnet IDs (optional)

**Template Structure:**
```yaml
apiVersion: v1
baseDomain: {{.BaseDomain}}
metadata:
  name: {{.ClusterName}}
platform:
  ibmcloud:
    region: {{.Region}}
    resourceGroupName: {{.ResourceGroup}}
    {{- if .VPCName}}
    vpcName: {{.VPCName}}
    {{- end}}
    {{- if .ControlPlaneSubnets}}
    controlPlaneSubnets:
    {{- range .ControlPlaneSubnets}}
      - {{.}}
    {{- end}}
    {{- end}}
    {{- if .ComputeSubnets}}
    computeSubnets:
    {{- range .ComputeSubnets}}
      - {{.}}
    {{- end}}
    {{- end}}
controlPlane:
  name: master
  replicas: {{.ControlPlaneReplicas}}
  platform:
    ibmcloud:
      type: {{.ControlPlaneInstanceType}}
      {{- if .BootVolume}}
      bootVolume:
        encryptionKey: {{.BootVolumeEncryptionKey}}
      {{- end}}
      {{- if .DedicatedHosts}}
      dedicatedHosts:
      {{- range .DedicatedHosts}}
        - name: {{.Name}}
          profile: {{.Profile}}
      {{- end}}
      {{- end}}
compute:
  - name: worker
    replicas: {{.WorkerReplicas}}
    platform:
      ibmcloud:
        type: {{.WorkerInstanceType}}
        {{- if .WorkerBootVolume}}
        bootVolume:
          encryptionKey: {{.WorkerBootVolumeEncryptionKey}}
        {{- end}}
        {{- if .WorkerZones}}
        zones:
        {{- range .WorkerZones}}
          - {{.}}
        {{- end}}
        {{- end}}
networking:
  networkType: {{.NetworkType}}
  clusterNetwork:
  {{- range .ClusterNetworks}}
    - cidr: {{.CIDR}}
      hostPrefix: {{.HostPrefix}}
  {{- end}}
  serviceNetwork:
  {{- range .ServiceNetworks}}
    - {{.}}
  {{- end}}
  machineNetwork:
  {{- range .MachineNetworks}}
    - cidr: {{.CIDR}}
  {{- end}}
pullSecret: '{{.PullSecret}}'
{{- if .SSHKey}}
sshKey: '{{.SSHKey}}'
{{- end}}
{{- if .Publish}}
publish: {{.Publish}}
{{- end}}
```

### Phase 2: Worker Handler Updates

#### 2.1 Create Handler

**File: `internal/worker/handler_create.go`**
- Update `Handle()` to support IBM Cloud platform
- Add CCO credential generation step before cluster creation
- Store CCO-generated credentials in work directory
- Pass credentials to openshift-installer via environment variables

**CCO Workflow:**
```go
// Before running openshift-install create cluster
if cluster.Platform == types.PlatformIBMCloud {
    // 1. Run ccoctl to create service IDs
    if err := h.generateIBMCloudCredentials(ctx, workDir, cluster); err != nil {
        return fmt.Errorf("generate IBM Cloud credentials: %w", err)
    }

    // 2. Set environment variables for installer
    os.Setenv("IC_API_KEY", ibmCloudAPIKey)
}
```

**File: `internal/worker/handler_create_ibmcloud.go`**
- Implement `generateIBMCloudCredentials()`
- Extract CredentialsRequests from release image
- Run `ccoctl ibmcloud create-service-id`
- Parse generated manifests and secrets
- Store credentials for cluster operators

#### 2.2 Destroy Handler

**File: `internal/worker/handler_destroy.go`**
- Update to clean up IBM Cloud-specific resources
- Delete CCO-generated service IDs and API keys
- Remove VPC resources if created by installer

**File: `internal/worker/handler_destroy_ibmcloud.go`**
- Implement `cleanupIBMCloudCredentials()`
- Call `ccoctl ibmcloud delete-service-id` for each operator
- Verify service IDs are removed from IBM Cloud IAM

#### 2.3 Cluster Outputs

**File: `internal/worker/handler_create.go`**
- Update `extractClusterOutputs()` for IBM Cloud
- Parse `metadata.json` for IBM Cloud-specific values:
  - VPC ID
  - Subnet IDs
  - Load balancer IDs
  - Public/private endpoints
  - Cloud Object Storage bucket

### Phase 3: API and Validation

#### 3.1 Request Validation

**File: `internal/api/handler_clusters.go`**
- Add IBM Cloud platform validation
- Validate IBM Cloud regions against supported list
- Validate instance types (bx2-*, cx2-*, mx2-*)
- Validate resource group names

**IBM Cloud Instance Type Validation:**
```go
var ibmCloudInstanceTypes = map[string]bool{
    "bx2-2x8":   true,  // 2 vCPU, 8 GB RAM
    "bx2-4x16":  true,  // 4 vCPU, 16 GB RAM
    "bx2-8x32":  true,  // 8 vCPU, 32 GB RAM
    "bx2-16x64": true,  // 16 vCPU, 64 GB RAM
    "bx2-32x128": true, // 32 vCPU, 128 GB RAM
    "bx2-48x192": true, // 48 vCPU, 192 GB RAM
    "cx2-2x4":   true,  // Compute-optimized
    "cx2-4x8":   true,
    "cx2-8x16":  true,
    "mx2-2x16":  true,  // Memory-optimized
    "mx2-4x32":  true,
    "mx2-8x64":  true,
}
```

#### 3.2 Profile Updates

**File: `internal/profile/definitions/ibmcloud-standard.yaml`**
- Set `enabled: true`
- Update regions to supported IBM Cloud regions
- Update base domains to actual IBM Cloud DNS zones
- Verify instance types are valid

**File: `internal/profile/definitions/ibmcloud-minimal-test.yaml`**
- Set `enabled: true`
- Configure for minimal resource testing
- Update cost estimates based on IBM Cloud pricing

### Phase 4: Database Schema Updates

#### 4.1 Platform Enum

**File: `internal/store/migrations/00001_initial_schema.sql`**
- Verify `platform` enum includes `ibmcloud` (already present)

#### 4.2 IBM Cloud Metadata

**File: `internal/store/migrations/00013_add_ibmcloud_metadata.sql`**
- Add IBM Cloud-specific cluster metadata fields:
  - `vpc_id`: VPC identifier
  - `resource_group_id`: Resource group identifier
  - `cos_bucket_name`: Object storage bucket for registry
  - `service_ids`: JSON array of CCO-generated service IDs

```sql
ALTER TABLE clusters ADD COLUMN ibmcloud_vpc_id VARCHAR(64);
ALTER TABLE clusters ADD COLUMN ibmcloud_resource_group_id VARCHAR(64);
ALTER TABLE clusters ADD COLUMN ibmcloud_cos_bucket_name VARCHAR(128);
ALTER TABLE clusters ADD COLUMN ibmcloud_service_ids JSONB;

CREATE INDEX idx_clusters_ibmcloud_vpc ON clusters(ibmcloud_vpc_id)
  WHERE platform = 'ibmcloud';
```

### Phase 5: Configuration and Deployment

#### 5.1 Environment Variables

**File: `/etc/ocpctl/worker.env` (on EC2 instance)**

Add IBM Cloud credentials:
```bash
# IBM Cloud Authentication
IC_API_KEY=your-ibm-cloud-api-key
IC_REGION=us-south

# IBM Cloud Resource Configuration
IC_RESOURCE_GROUP=default
IC_VPC_NAME=ocpctl-vpc  # Optional: existing VPC

# IBM Cloud Object Storage
IC_COS_INSTANCE_CRN=crn:v1:bluemix:public:cloud-object-storage:global:a/...
```

#### 5.2 Worker IAM Permissions

If running ocpctl worker on IBM Cloud VPC instance:
- Attach service ID with required permissions
- Enable metadata service for credential retrieval
- Document required IAM policies

#### 5.3 Binary Installation

Update multi-version installer script:
```bash
# scripts/install-multiversion-binaries.sh
# Verify ccoctl supports ibmcloud subcommand
/usr/local/bin/ccoctl-4.20 ibmcloud --help
```

### Phase 6: Testing and Documentation

#### 6.1 Integration Tests

**File: `internal/worker/handler_create_ibmcloud_test.go`**
- Test CCO credential generation
- Test install-config rendering with IBM Cloud templates
- Mock IBM Cloud API calls
- Verify cleanup on failure

**File: `internal/ibmcloud/client_test.go`**
- Test credential detection (API key vs instance metadata)
- Test IAM permission validation
- Test VPC resource enumeration

#### 6.2 E2E Tests

**Manual Test Plan:**
1. Create `ibmcloud-minimal-test` cluster
2. Verify cluster reaches READY state
3. Access OpenShift console
4. Run workload
5. Destroy cluster
6. Verify all resources cleaned up

#### 6.3 Documentation

**File: `docs/deployment/IBMCLOUD_QUICKSTART.md`**
- IBM Cloud account setup
- API key generation
- Required IAM permissions
- Resource group configuration
- VPC prerequisites (optional)
- DNS zone configuration
- Cluster creation walkthrough
- Troubleshooting guide

**File: `docs/setup/IBMCLOUD_CREDENTIALS.md`**
- CCO manual mode explanation
- Service ID creation and management
- API key rotation procedures
- Instance metadata service setup
- Security best practices

**File: `README.md`**
- Update supported platforms to include IBM Cloud
- Add IBM Cloud to feature matrix
- Update cost estimates for IBM Cloud profiles

## Implementation Phases Summary

### Phase 1: Foundation (Week 1-2)
- IBM Cloud SDK integration
- CCO/ccoctl support
- Install-config templates

### Phase 2: Worker Handlers (Week 2-3)
- Create handler IBM Cloud support
- Destroy handler IBM Cloud support
- Credential lifecycle management

### Phase 3: API and Validation (Week 3)
- Request validation
- Profile updates
- Error handling

### Phase 4: Database (Week 4)
- Schema updates for IBM Cloud metadata
- Migration testing

### Phase 5: Deployment (Week 4-5)
- Environment configuration
- IAM setup
- Binary verification

### Phase 6: Testing and Documentation (Week 5-6)
- Integration tests
- E2E testing
- Documentation

## Acceptance Criteria

- [ ] `ibmcloud-standard` profile enabled and working
- [ ] `ibmcloud-minimal-test` profile enabled and working
- [ ] CCO credential generation automated
- [ ] Cluster creation succeeds for all supported versions (4.18, 4.19, 4.20)
- [ ] Cluster destruction cleans up all IBM Cloud resources
- [ ] Service IDs properly created and deleted
- [ ] API validation enforces IBM Cloud constraints
- [ ] Documentation complete for setup and usage
- [ ] Integration tests pass
- [ ] E2E test creates and destroys cluster successfully

## Dependencies

**External:**
- IBM Cloud account with billing enabled
- IBM Cloud API key with Administrator permissions
- Resource group created
- DNS zone configured (Route 53 or IBM Cloud DNS)
- Cloud Object Storage instance (can be created by installer)

**Internal:**
- Multi-version ccoctl binaries (4.18, 4.19, 4.20)
- Profile system supports IBM Cloud platform
- Database schema supports platform-specific metadata

## Risks and Mitigations

### Risk 1: CCO Credential Complexity
**Impact:** High - Required for installation, complex workflow
**Mitigation:**
- Automate CCO credential generation in worker
- Provide detailed documentation with examples
- Add validation to detect credential issues early

### Risk 2: IBM Cloud API Rate Limits
**Impact:** Medium - Could slow down provisioning
**Mitigation:**
- Implement exponential backoff
- Cache API responses where appropriate
- Document rate limits in troubleshooting

### Risk 3: Existing VPC Support
**Impact:** Medium - Users may want to use existing VPCs
**Mitigation:**
- Support both new VPC creation and existing VPC usage
- Validate subnet requirements for existing VPCs
- Document subnet prerequisites clearly

### Risk 4: Cost Management
**Impact:** Medium - IBM Cloud costs differ from AWS
**Mitigation:**
- Update cost estimates in profiles
- Document IBM Cloud pricing model
- Add cost alerts for IBM Cloud resources

### Risk 5: Multi-Region Support
**Impact:** Low - Different regions may have different capabilities
**Mitigation:**
- Test in primary regions first (us-south, us-east, eu-de)
- Document region-specific limitations
- Validate instance type availability per region

## Cost Estimates

### ibmcloud-minimal-test (3 nodes, schedulable masters)
- **Control Plane**: 3 × bx2-4x16 = ~$0.40/hour
- **Workers**: 0
- **Storage**: ~$0.10/hour (boot volumes)
- **Networking**: ~$0.05/hour (load balancers, egress)
- **Total**: ~$0.55/hour (~$400/month)

### ibmcloud-standard (6 nodes)
- **Control Plane**: 3 × bx2-4x16 = ~$0.40/hour
- **Workers**: 3 × bx2-8x32 = ~$0.80/hour
- **Storage**: ~$0.20/hour (boot volumes)
- **Networking**: ~$0.10/hour (load balancers, egress)
- **Total**: ~$1.50/hour (~$1,080/month)

## References

**Red Hat Documentation:**
- [Installing on IBM Cloud (4.20)](https://docs.redhat.com/en/documentation/openshift_container_platform/4.20/html/installing_on_ibm_cloud/index)
- [Installing on IBM Cloud VPC (4.12)](https://docs.redhat.com/en/documentation/openshift_container_platform/4.12/html-single/installing_on_ibm_cloud_vpc/index)
- [Configuring an IBM Cloud account](https://docs.redhat.com/en/documentation/openshift_container_platform/4.20/html/installing_on_ibm_cloud/installing-ibm-cloud-account)

**IBM Cloud Documentation:**
- [Red Hat OpenShift on IBM Cloud](https://cloud.ibm.com/docs/openshift)
- [IBM Cloud Terraform Modules](https://github.com/terraform-ibm-modules/terraform-ibm-base-ocp-vpc)
- [IBM Cloud VPC Documentation](https://cloud.ibm.com/docs/vpc)

**Terraform Modules:**
- [terraform-ibm-base-ocp-vpc](https://registry.terraform.io/modules/terraform-ibm-modules/base-ocp-vpc/ibm/latest)
- [IBM Cloud Terraform Provider](https://registry.terraform.io/providers/IBM-Cloud/ibm/latest/docs)

## Success Metrics

- **Deployment Success Rate**: >95% for cluster creation
- **Average Creation Time**: <45 minutes (similar to AWS)
- **Resource Cleanup**: 100% of resources deleted on cluster destroy
- **Cost Accuracy**: Profile cost estimates within 10% of actual
- **Documentation Completeness**: All setup steps documented and tested

## Future Enhancements (Out of Scope)

- IBM Cloud Power Virtual Server support
- IBM Cloud Classic infrastructure support
- Multi-zone VPC configuration
- Dedicated hosts for compliance requirements
- Encrypted boot volumes with BYOK (Bring Your Own Key)
- VPC VPN connectivity for private clusters
- Direct Link integration for hybrid cloud
- IBM Cloud Satellite integration

## Questions for Discussion

1. **VPC Strategy**: Should we create new VPCs per cluster (like AWS) or support shared VPC model by default?
2. **Cost Center Mapping**: How should IBM Cloud resource groups map to our cost center field?
3. **Region Selection**: Which IBM Cloud regions should we prioritize for initial testing?
4. **DNS Provider**: Should we support both Route 53 and IBM Cloud DNS, or Route 53 only initially?
5. **Object Storage**: Should we create COS instance per cluster or share across clusters?
6. **Credential Rotation**: How should we handle IBM Cloud API key rotation for long-lived clusters?

---

**Labels:** `enhancement`, `platform:ibmcloud`, `phase-2`, `infrastructure`
**Milestone:** Phase 2 - Multi-Platform Support
**Estimated Effort:** 4-6 weeks (1 full-time engineer)
**Priority:** Medium
**Assignee:** TBD
