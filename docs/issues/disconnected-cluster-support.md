# Enhancement: Add Disconnected/Air-Gapped OpenShift Cluster Support

## Summary

Add support for provisioning disconnected (air-gapped) OpenShift clusters for testing layered products in environments without direct internet access. This enables OCPCTL to deploy clusters that pull container images from internal mirror registries instead of public registries like quay.io and registry.redhat.io.

## Use Case

**Testing Scenario**: Layered product teams (CNV, MTA, MTC, OADP, etc.) need to test their products in disconnected environments to validate:
- Installation from mirror registries
- Image pull behavior with custom CA certificates
- Operator catalog mirroring
- Upgrade workflows in disconnected mode
- Network policy restrictions

These scenarios are critical for customers deploying OpenShift in secure, air-gapped environments.

## Current Status

**Partial Foundation Exists**:
- ✅ Custom pull secret support (migration 00036) - can add mirror registry credentials
- ✅ Pull secret merging logic in `internal/worker/handler_create.go:152-161`
- ❌ No support for `imageContentSources` (registry mirroring configuration)
- ❌ No support for `additionalTrustBundle` (custom CA certificates)

**What's Working**:
```go
// Can already merge custom registry credentials
if cluster.CustomPullSecret != nil && *cluster.CustomPullSecret != "" {
    mergedSecret, err := mergePullSecrets(pullSecret, *cluster.CustomPullSecret)
    // ...
}
```

**What's Missing**:
- Image content sources configuration in install-config.yaml
- Additional trust bundle injection
- Disconnected-specific validation

## Background

### OpenShift Disconnected Installation Requirements

OpenShift disconnected installations require three components in `install-config.yaml`:

#### 1. Pull Secret (✅ Already Supported)
```yaml
pullSecret: '{"auths":{"registry.mirror.example.com:5000":{"auth":"..."}}}'
```

#### 2. Image Content Sources (❌ Missing)
```yaml
imageContentSources:
- source: quay.io/openshift-release-dev/ocp-release
  mirrors:
  - registry.mirror.example.com:5000/ocp4/openshift4
- source: quay.io/openshift-release-dev/ocp-v4.0-art-dev
  mirrors:
  - registry.mirror.example.com:5000/ocp4/openshift4
- source: registry.redhat.io/ubi8
  mirrors:
  - registry.mirror.example.com:5000/ubi8
```

#### 3. Additional Trust Bundle (❌ Missing)
```yaml
additionalTrustBundle: |
  -----BEGIN CERTIFICATE-----
  MIIDXTCCAkWgAwIBAgIJAKnL4UEDMN8jMA0GCSqGSIb3DQEBCwUAMEUxCzAJBgNV
  ... (PEM-encoded CA certificate for mirror registry)
  -----END CERTIFICATE-----
```

### Reference Documentation
- [OpenShift 4.21 Disconnected Installation](https://docs.openshift.com/container-platform/4.21/installing/disconnected_install/index.html)
- [Mirroring images for disconnected installation](https://docs.openshift.com/container-platform/4.21/installing/disconnected_install/installing-mirroring-installation-images.html)

## Implementation Options

### Option 1: Full Dynamic Support (Recommended for Long-Term)

**Pros**:
- Maximum flexibility - users can specify any mirror registry per-cluster
- Supports both internal mirror registries and external disconnected scenarios
- No hardcoded infrastructure assumptions

**Cons**:
- Requires database schema changes
- More complex API validation
- Additional fields in cluster creation UI

**Implementation Scope**: Medium (2-3 days)

### Option 2: Profile-Based Approach (Quick Win)

**Pros**:
- No database schema changes required
- Leverages existing profile system
- Quick to implement (< 1 day)
- Infrastructure-as-code approach

**Cons**:
- Less flexible - requires predefined profiles for each mirror registry
- Changing mirror registry requires profile updates
- Not suitable if users need different mirror registries per cluster

**Implementation Scope**: Small (4-6 hours)

## Recommended Implementation: Option 1 (Full Dynamic Support)

### Phase 1: Database Schema

**Migration: `internal/store/migrations/000XX_add_disconnected_support.sql`**

```sql
-- +goose Up
ALTER TABLE clusters
ADD COLUMN image_content_sources JSONB,
ADD COLUMN additional_trust_bundle TEXT;

COMMENT ON COLUMN clusters.image_content_sources IS 'Registry mirror configuration for disconnected installations. Format: [{"source": "quay.io/...", "mirrors": ["mirror.example.com/..."]}]';
COMMENT ON COLUMN clusters.additional_trust_bundle IS 'PEM-encoded CA certificate bundle for custom registry certificates';

-- +goose Down
ALTER TABLE clusters
DROP COLUMN image_content_sources,
DROP COLUMN additional_trust_bundle;
```

### Phase 2: Type Definitions

**File: `pkg/types/cluster.go`**

```go
// Add to Cluster struct
type Cluster struct {
    // ... existing fields ...
    CustomPullSecret      *string               `db:"custom_pull_secret" json:"custom_pull_secret,omitempty"`
    ImageContentSources   []ImageContentSource  `db:"image_content_sources" json:"image_content_sources,omitempty"`   // NEW
    AdditionalTrustBundle *string               `db:"additional_trust_bundle" json:"additional_trust_bundle,omitempty"` // NEW
}

// NEW: Image content source for registry mirroring
type ImageContentSource struct {
    Source  string   `json:"source" validate:"required"`   // e.g., "quay.io/openshift-release-dev/ocp-release"
    Mirrors []string `json:"mirrors" validate:"required,min=1"` // e.g., ["mirror.example.com:5000/ocp4"]
}

// Implement database marshaling for ImageContentSource slice
func (i ImageContentSource) Value() (driver.Value, error) { ... }
func (i *ImageContentSource) Scan(value interface{}) error { ... }
```

**File: `internal/profile/types.go`**

```go
// Add to FeaturesConfig
type FeaturesConfig struct {
    OffHoursScaling bool `yaml:"offHoursScaling" json:"off_hours_scaling"`
    FIPSMode        bool `yaml:"fipsMode" json:"fips_mode"`
    PrivateCluster  bool `yaml:"privateCluster" json:"private_cluster"`
    DisconnectedInstall bool `yaml:"disconnectedInstall" json:"disconnected_install"` // NEW
}
```

### Phase 3: API Handler Updates

**File: `internal/api/handler_clusters.go`**

```go
// Update CreateClusterRequest
type CreateClusterRequest struct {
    // ... existing fields ...
    CustomPullSecret      *string               `json:"custom_pull_secret,omitempty"`
    ImageContentSources   []types.ImageContentSource `json:"image_content_sources,omitempty"`   // NEW
    AdditionalTrustBundle *string               `json:"additional_trust_bundle,omitempty"` // NEW
}

// Add validation
func validateDisconnectedConfig(req *CreateClusterRequest) error {
    if req.ImageContentSources != nil && len(req.ImageContentSources) > 0 {
        // Validate that additionalTrustBundle is provided if using custom registry
        // (optional if registry uses public CA)

        // Validate PEM format if additionalTrustBundle provided
        if req.AdditionalTrustBundle != nil {
            if err := validatePEMCertificate(*req.AdditionalTrustBundle); err != nil {
                return fmt.Errorf("invalid additional trust bundle: %w", err)
            }
        }

        // Validate source and mirror URLs
        for i, ics := range req.ImageContentSources {
            if ics.Source == "" {
                return fmt.Errorf("imageContentSources[%d]: source is required", i)
            }
            if len(ics.Mirrors) == 0 {
                return fmt.Errorf("imageContentSources[%d]: at least one mirror is required", i)
            }
        }
    }
    return nil
}

func validatePEMCertificate(pem string) error {
    // Parse PEM to ensure it's valid certificate format
    block, _ := pem.Decode([]byte(pem))
    if block == nil {
        return fmt.Errorf("failed to parse PEM certificate")
    }
    if _, err := x509.ParseCertificate(block.Bytes); err != nil {
        return fmt.Errorf("invalid x509 certificate: %w", err)
    }
    return nil
}
```

### Phase 4: Install-Config Renderer

**File: `internal/profile/renderer.go`**

Update `InstallConfigData` struct:
```go
type InstallConfigData struct {
    // ... existing fields ...
    ImageContentSources   []types.ImageContentSource // NEW
    AdditionalTrustBundle string                     // NEW
}
```

Update template population in `RenderInstallConfig`:
```go
func (r *Renderer) RenderInstallConfig(req *types.CreateClusterRequest, pullSecret string, mergedTags map[string]string) ([]byte, error) {
    // ... existing code ...

    data := InstallConfigData{
        // ... existing fields ...
        ImageContentSources:   cluster.ImageContentSources,   // NEW
        AdditionalTrustBundle: stringValue(cluster.AdditionalTrustBundle), // NEW
    }

    // ... rest of function
}
```

Update `awsInstallConfigTemplate` (and other platform templates):
```go
const awsInstallConfigTemplate = `apiVersion: v1
baseDomain: {{.BaseDomain}}
metadata:
  name: {{.ClusterName}}
{{if .CredentialsMode}}
credentialsMode: {{.CredentialsMode}}
{{end}}
platform:
  aws:
    region: {{.Region}}
    # ... existing AWS config ...
pullSecret: '{{.PullSecret}}'
{{if .SSHKey}}
sshKey: {{.SSHKey}}
{{end}}
# ... existing compute, networking, etc ...

{{if .ImageContentSources}}
imageContentSources:
{{range .ImageContentSources}}
- source: {{.Source}}
  mirrors:
{{range .Mirrors}}
  - {{.}}
{{end}}
{{end}}
{{end}}
{{if .AdditionalTrustBundle}}
additionalTrustBundle: |
{{.AdditionalTrustBundle | indent 2}}
{{end}}
`
```

### Phase 5: Store Updates

**File: `internal/store/clusters.go`**

Update `CreateCluster` to handle new fields:
```go
func (s *ClusterStore) CreateCluster(ctx context.Context, tx pgx.Tx, cluster *types.Cluster) error {
    query := `
        INSERT INTO clusters (
            id, name, platform, cluster_type, version, profile, region, base_domain,
            owner, owner_id, team, cost_center, status, created_at, updated_at,
            ttl_hours, expires_at, ssh_public_key, effective_tags, request_tags,
            custom_post_config, preserve_on_failure, credentials_mode, custom_pull_secret,
            image_content_sources, additional_trust_bundle  -- NEW
        ) VALUES (
            $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15,
            $16, $17, $18, $19, $20, $21, $22, $23, $24, $25, $26  -- NEW
        )
    `
    // ... add image_content_sources and additional_trust_bundle to exec args
}
```

### Phase 6: Documentation

**File: `docs/user-guide/disconnected-clusters.md`** (NEW)

```markdown
# Disconnected OpenShift Clusters

## Overview

OCPCTL supports provisioning disconnected (air-gapped) OpenShift clusters that pull container images from internal mirror registries instead of public registries.

## Prerequisites

1. **Mirror Registry**: Set up a container registry accessible from the cluster network
2. **Mirrored Images**: Mirror OpenShift release images and operator catalogs to your registry
3. **CA Certificate**: If using self-signed certificates, obtain the registry's CA certificate in PEM format
4. **Pull Secret**: Create credentials for accessing the mirror registry

## Creating a Disconnected Cluster

### Example API Request

```bash
curl -X POST https://ocpctl.<BASE_DOMAIN>/api/v1/clusters \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "disconnected-test",
    "platform": "aws",
    "profile": "aws-standard",
    "region": "us-east-1",
    "version": "4.21",
    "customPullSecret": "{\"auths\":{\"mirror.example.com:5000\":{\"auth\":\"dXNlcjpwYXNz\"}}}",
    "imageContentSources": [
      {
        "source": "quay.io/openshift-release-dev/ocp-release",
        "mirrors": ["mirror.example.com:5000/ocp4/openshift4"]
      },
      {
        "source": "quay.io/openshift-release-dev/ocp-v4.0-art-dev",
        "mirrors": ["mirror.example.com:5000/ocp4/openshift4"]
      }
    ],
    "additionalTrustBundle": "-----BEGIN CERTIFICATE-----\nMIID...\n-----END CERTIFICATE-----"
  }'
```

### Mirror Registry Setup

See [OpenShift Documentation](https://docs.openshift.com/container-platform/4.21/installing/disconnected_install/installing-mirroring-installation-images.html) for mirroring release images.

Common mirror sources:
- `quay.io/openshift-release-dev/ocp-release` - Release images
- `quay.io/openshift-release-dev/ocp-v4.0-art-dev` - Development artifacts
- `registry.redhat.io/ubi8` - Universal Base Images
- `registry.redhat.io/rhel8` - RHEL base images

## Validation

OCPCTL validates:
- ✅ PEM certificate format for `additionalTrustBundle`
- ✅ At least one mirror per image source
- ✅ Non-empty source and mirror values
- ✅ Pull secret JSON format

## Troubleshooting

### Cluster stuck in CREATING with ImagePullBackOff
- Verify mirror registry is accessible from cluster network
- Check pull secret credentials are correct
- Ensure all required images are mirrored

### Certificate validation errors
- Verify `additionalTrustBundle` contains the correct CA certificate
- Check PEM encoding (must include BEGIN/END markers)
- Ensure certificate is not expired
```

### Phase 7: Web UI Updates (Optional)

**File: `web/app/(dashboard)/clusters/new/page.tsx`**

Add advanced section for disconnected configuration:
```typescript
// Add state
const [imageContentSources, setImageContentSources] = useState<ImageContentSource[]>([]);
const [additionalTrustBundle, setAdditionalTrustBundle] = useState<string>('');

// Add UI section (collapsed by default)
<Accordion>
  <AccordionItem title="Disconnected Installation (Advanced)">
    <p className="text-sm text-gray-600 mb-4">
      Configure registry mirroring for air-gapped environments
    </p>

    {/* Image Content Sources */}
    <div className="space-y-2">
      <Label>Image Content Sources</Label>
      {imageContentSources.map((ics, idx) => (
        <div key={idx} className="border p-4 rounded">
          <Input
            label="Source Registry"
            placeholder="quay.io/openshift-release-dev/ocp-release"
            value={ics.source}
            onChange={(e) => updateImageContentSource(idx, 'source', e.target.value)}
          />
          <Input
            label="Mirror Registry"
            placeholder="mirror.example.com:5000/ocp4/openshift4"
            value={ics.mirrors[0]}
            onChange={(e) => updateImageContentSource(idx, 'mirrors', [e.target.value])}
          />
        </div>
      ))}
      <Button onClick={() => addImageContentSource()}>Add Mirror</Button>
    </div>

    {/* Additional Trust Bundle */}
    <div className="mt-4">
      <Label>Additional Trust Bundle (PEM)</Label>
      <Textarea
        rows={10}
        placeholder="-----BEGIN CERTIFICATE-----&#10;...&#10;-----END CERTIFICATE-----"
        value={additionalTrustBundle}
        onChange={(e) => setAdditionalTrustBundle(e.target.value)}
      />
    </div>
  </AccordionItem>
</Accordion>
```

## Testing Plan

### Test Cases

1. **Disconnected SNO Cluster**
   - Profile: `aws-sno-ga`
   - Mirror: Internal registry with self-signed cert
   - Validate: Install completes, all images pulled from mirror

2. **Disconnected Multi-Node Cluster**
   - Profile: `aws-standard`
   - Mirror: Internal registry with public CA
   - Validate: HA control plane, all nodes ready

3. **Invalid Certificate Format**
   - Provide malformed PEM certificate
   - Expected: API validation error with clear message

4. **Missing Mirror**
   - Provide `imageContentSources` without corresponding mirrored images
   - Expected: Cluster creation fails with ImagePullBackOff

5. **Operator Installation on Disconnected Cluster**
   - Create disconnected cluster
   - Install CNV addon (requires additional operator catalog mirroring)
   - Validate: Operator CSV becomes Available

### Manual Testing Steps

```bash
# 1. Set up test mirror registry (using Red Hat Quay)
docker run -d --name mirror-registry \
  -p 5000:5000 \
  -v /opt/quay:/var/lib/quay \
  quay.io/projectquay/quay:latest

# 2. Mirror OpenShift 4.21 release
oc adm release mirror \
  --from=quay.io/openshift-release-dev/ocp-release:4.21.0-x86_64 \
  --to=localhost:5000/ocp4/openshift4 \
  --to-release-image=localhost:5000/ocp4/openshift4:4.21.0-x86_64

# 3. Create disconnected cluster via OCPCTL
curl -X POST http://localhost:8080/api/v1/clusters \
  -H "Content-Type: application/json" \
  -d @disconnected-cluster-request.json

# 4. Monitor cluster creation
watch 'ocpctl clusters list | grep disconnected-test'

# 5. Validate image sources on created cluster
oc get imagecontentsourcepolicy -o yaml
oc get nodes -o json | jq '.items[].status.images[] | select(.names[] | contains("mirror.example.com"))'
```

## Rollout Plan

### Phase 1: Backend Implementation (Week 1)
- Database migration
- Type definitions
- API handler updates
- Install-config renderer
- Store updates

### Phase 2: Documentation & Testing (Week 1-2)
- User guide documentation
- API documentation (Swagger)
- Unit tests
- Integration tests

### Phase 3: Web UI (Week 2-3)
- UI form components
- Validation feedback
- Examples/presets

### Phase 4: Production Deployment (Week 3)
- Deploy to dev environment
- Smoke tests
- Deploy to production
- Announce feature

## Estimated Effort

- **Backend Implementation**: 2 days
- **Documentation**: 0.5 days
- **Testing**: 1 day
- **Web UI**: 1.5 days
- **Total**: ~5 days (1 week with buffer)

## Success Criteria

- ✅ Disconnected clusters provision successfully with mirrored images
- ✅ Custom CA certificates are trusted by cluster nodes
- ✅ Image pulls occur from mirror registry (verified via node image list)
- ✅ Post-deployment addons work with mirrored operator catalogs
- ✅ Documentation includes complete end-to-end example
- ✅ API validation prevents invalid configurations

## Alternative: Profile-Based Quick Win

If full implementation is deferred, create disconnected-specific profiles:

**File: `internal/profile/definitions/aws-sno-disconnected.yaml`**
```yaml
name: aws-sno-disconnected
displayName: AWS SNO (Disconnected)
description: Single node OpenShift with pre-configured mirror registry
platform: aws
clusterType: openshift
# ... standard SNO config ...
features:
  disconnectedInstall: true
metadata:
  notes:
  - "Uses mirror.<BASE_DOMAIN>:5000 for container images"
  - "Requires MIRROR_REGISTRY_CA environment variable"
  - "Requires MIRROR_PULL_SECRET environment variable"
```

Modify `internal/worker/handler_create.go` to inject mirror configuration from environment variables when profile has `features.disconnectedInstall: true`.

**Pros**: Ships in < 1 day
**Cons**: Less flexible, hardcoded mirror registry

## Related Issues

- Custom pull secret support (implemented in migration 00036)
- Private cluster support (existing `features.privateCluster`)
- Operator catalog mirroring (future enhancement)

## References

- [OpenShift 4.21 Disconnected Installation](https://docs.openshift.com/container-platform/4.21/installing/disconnected_install/index.html)
- [Mirroring images for a disconnected installation](https://docs.openshift.com/container-platform/4.21/installing/disconnected_install/installing-mirroring-installation-images.html)
- [About disconnected installation mirroring](https://docs.openshift.com/container-platform/4.21/installing/disconnected_install/installing-mirroring-disconnected.html)
