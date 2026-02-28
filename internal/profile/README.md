# Profile Package

Cluster profile management for ocpctl - loads YAML profile definitions, validates requests, and renders install-config.yaml for OpenShift installer.

## Components

| File | Purpose |
|------|---------|
| `types.go` | Profile struct matching YAML schema |
| `loader.go` | Load and validate YAML profiles |
| `registry.go` | In-memory profile cache |
| `renderer.go` | Generate install-config.yaml |
| `definitions/` | YAML profile files |

## Usage

### 1. Load Profiles

```go
import "github.com/tsanders-rh/ocpctl/internal/profile"

// Create loader pointing to profile directory
loader := profile.NewLoader("internal/profile/definitions")

// Load single profile
prof, err := loader.Load("aws-minimal-test")

// Load all profiles
profiles, err := loader.LoadAll()
```

### 2. Create Registry

```go
// Registry loads all profiles and caches them
registry, err := profile.NewRegistry(loader)

// Get profile by name
prof, err := registry.Get("aws-minimal-test")

// List all enabled profiles
profiles := registry.List()

// List by platform
awsProfiles := registry.ListByPlatform(types.PlatformAWS)

// Check if profile exists
exists := registry.Exists("aws-standard")

// Reload from disk
err = registry.Reload()
```

### 3. Render install-config.yaml

```go
renderer := profile.NewRenderer(registry)

req := &policy.CreateClusterRequest{
    Name:       "demo-cluster",
    Platform:   "aws",
    Version:    "4.20.3",
    Profile:    "aws-minimal-test",
    Region:     "us-east-1",
    BaseDomain: "labs.example.com",
    // ...
}

pullSecret := `{"auths":{"cloud.openshift.com":{"auth":"..."}}}`
tags := map[string]string{"Team": "platform"}

installConfig, err := renderer.RenderInstallConfig(req, pullSecret, tags)

// Write to file for openshift-install
os.WriteFile("install-config.yaml", installConfig, 0644)
```

## Profile Schema

Profiles are YAML files matching this structure:

```yaml
name: aws-minimal-test
displayName: AWS Minimal Test Cluster
description: Compact 3-node cluster
platform: aws
enabled: true

openshiftVersions:
  allowlist:
    - "4.20.3"
    - "4.20.4"
  default: "4.20.3"

regions:
  allowlist:
    - us-east-1
    - us-west-2
  default: us-east-1

baseDomains:
  allowlist:
    - labs.example.com
  default: labs.example.com

compute:
  controlPlane:
    replicas: 3
    instanceType: m6i.xlarge
    schedulable: true
  workers:
    replicas: 0
    minReplicas: 0
    maxReplicas: 3
    instanceType: m6i.2xlarge

lifecycle:
  maxTTLHours: 72
  defaultTTLHours: 24
  allowCustomTTL: true

tags:
  required:
    Environment: test
  defaults:
    ManagedBy: cluster-control-plane
  allowUserTags: true

features:
  offHoursScaling: true
  fipsMode: false

costControls:
  estimatedHourlyCost: 2.50
  maxMonthlyCost: 1800

platformConfig:
  aws:
    rootVolume:
      type: gp3
      size: 120
      iops: 3000
```

See `definitions/SCHEMA.md` for complete schema documentation.

## Validation Rules

The loader validates profiles against these rules:

1. **Control plane replicas must be odd** (1, 3, 5) for etcd quorum
2. **Default version must be in allowlist**
3. **Default region must be in allowlist**
4. **Default base domain must be in allowlist**
5. **Platform-specific config must match platform** (aws → platformConfig.aws)
6. **Profile name must start with platform prefix** (aws-*, ibmcloud-*)
7. **Worker maxReplicas >= minReplicas**
8. **Worker replicas within min/max bounds**

## Reserved Tag Keys

These tag keys cannot be overridden by user tags:

- `ManagedBy`
- `ClusterId`
- `ClusterName`
- `Owner`
- `Team`
- `CostCenter`
- `Environment`
- `TTLExpiry`
- `RequestId`
- `Profile`
- `Platform`

## Testing

```bash
# Run profile tests
go test ./internal/profile/...

# Run with verbose output
go test -v ./internal/profile/...

# Test specific function
go test ./internal/profile/... -run TestLoader_LoadProfile
```

## Adding New Profiles

1. Create YAML file in `definitions/` directory
2. Follow naming convention: `{platform}-{size}-{purpose}.yaml`
3. Validate against schema in `definitions/SCHEMA.md`
4. Run tests to verify it loads correctly
5. Update documentation

Example:
```bash
# Create new profile
cat > internal/profile/definitions/aws-large-prod.yaml <<EOF
name: aws-large-prod
displayName: AWS Large Production Cluster
platform: aws
enabled: true
# ... rest of config
EOF

# Test it loads
go test ./internal/profile/... -run TestLoader_LoadProfile
```

## Integration with API

The API service uses profiles like this:

```go
// Initialize at startup
loader := profile.NewLoader("internal/profile/definitions")
registry, _ := profile.NewRegistry(loader)
policyEngine := policy.NewEngine(registry)
renderer := profile.NewRenderer(registry)

// In API handler
func HandleCreateCluster(w http.ResponseWriter, r *http.Request) {
    var req policy.CreateClusterRequest
    json.NewDecoder(r.Body).Decode(&req)

    // Validate against profile
    result, _ := policyEngine.ValidateCreateRequest(&req)
    if !result.Valid {
        http.Error(w, result.FirstError(), 422)
        return
    }

    // Render install-config
    pullSecret := getPullSecret()
    installConfig, _ := renderer.RenderInstallConfig(&req, pullSecret, result.MergedTags)

    // Create cluster...
}
```
