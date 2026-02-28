# Policy Package

Request validation engine that enforces cluster profile constraints and policies.

## Components

| File | Purpose |
|------|---------|
| `engine.go` | Main validation engine |
| `request.go` | Request types |
| `errors.go` | Validation errors and results |

## Usage

### 1. Create Policy Engine

```go
import (
    "github.com/tsanders-rh/ocpctl/internal/policy"
    "github.com/tsanders-rh/ocpctl/internal/profile"
)

// Load profiles
loader := profile.NewLoader("internal/profile/definitions")
registry, _ := profile.NewRegistry(loader)

// Create policy engine
engine := policy.NewEngine(registry)
```

### 2. Validate Cluster Creation Request

```go
req := &policy.CreateClusterRequest{
    Name:       "my-cluster",
    Platform:   "aws",
    Version:    "4.20.3",
    Profile:    "aws-minimal-test",
    Region:     "us-east-1",
    BaseDomain: "labs.example.com",
    Owner:      "alice",
    Team:       "platform-team",
    CostCenter: "engineering",
    TTLHours:   24,
    ExtraTags: map[string]string{
        "Purpose": "testing",
    },
}

result, err := engine.ValidateCreateRequest(req)
if err != nil {
    // Engine error
    return err
}

if !result.Valid {
    // Validation failed
    for _, validationErr := range result.Errors {
        fmt.Printf("%s: %s\n", validationErr.Field, validationErr.Message)
    }
    return errors.New("validation failed")
}

// Success - use merged tags
tags := result.MergedTags
destroyAt := result.DestroyAt
```

### 3. Get Profile Defaults

```go
// Get default values from profile
version, _ := engine.GetDefaultVersion("aws-minimal-test")
region, _ := engine.GetDefaultRegion("aws-minimal-test")
baseDomain, _ := engine.GetDefaultBaseDomain("aws-minimal-test")
ttl, _ := engine.GetDefaultTTL("aws-minimal-test")
```

## Validation Rules

The policy engine validates these constraints:

### Cluster Name
- **DNS-compatible**: lowercase alphanumeric and hyphens only
- **Length**: 3-63 characters
- **Pattern**: `^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`

### Platform
- Must match profile platform (aws or ibmcloud)

### Version
- Must be in profile's version allowlist
- Example: `["4.20.3", "4.20.4", "4.20.5"]`

### Region
- Must be in profile's region allowlist
- Example: `["us-east-1", "us-west-2"]`

### Base Domain
- Must be in profile's base domain allowlist
- Example: `["labs.example.com", "dev.example.com"]`

### TTL (Time-to-Live)
- Must be > 0
- Must be <= profile's `maxTTLHours`
- Must match `defaultTTLHours` if `allowCustomTTL` is false

### Tags
- User tags allowed only if profile `allowUserTags` is true
- Cannot override reserved tag keys:
  - ManagedBy, ClusterId, ClusterName, Owner, Team, CostCenter, Environment, TTLExpiry, RequestId, Profile, Platform

### Off-Hours Scaling
- Can only opt-in if profile `features.offHoursScaling` is true

## Tag Merging Strategy

Tags are merged in this order (later overrides earlier):

1. **Profile default tags** - From `tags.defaults`
2. **Profile required tags** - From `tags.required`
3. **User extra tags** - From request `extraTags` (if allowed)
4. **System tags** - Always added (ManagedBy, ClusterName, Owner, Team, etc.)

Example:

```go
// Profile default tags
{"Purpose": "development"}

// Profile required tags
{"Environment": "test"}

// User extra tags (if allowed)
{"CustomTag": "custom-value"}

// System tags (always added)
{
    "ManagedBy": "cluster-control-plane",
    "ClusterName": "my-cluster",
    "Owner": "alice",
    "Team": "platform-team",
    "CostCenter": "engineering",
    "Profile": "aws-minimal-test",
    "Platform": "aws"
}

// Final merged tags
{
    "Purpose": "development",
    "Environment": "test",
    "CustomTag": "custom-value",
    "ManagedBy": "cluster-control-plane",
    "ClusterName": "my-cluster",
    "Owner": "alice",
    "Team": "platform-team",
    "CostCenter": "engineering",
    "Profile": "aws-minimal-test",
    "Platform": "aws"
}
```

## Validation Result

The `ValidationResult` struct contains:

```go
type ValidationResult struct {
    Valid       bool                // Overall validation status
    Errors      []ValidationError   // List of validation errors
    MergedTags  map[string]string  // Final merged tags
    DestroyAt   string             // ISO8601 timestamp for TTL expiry
}
```

Methods:
- `result.HasErrors()` - Check if any errors exist
- `result.FirstError()` - Get first error message
- `result.AddError(field, message)` - Add validation error

## Error Types

### ValidationError

```go
type ValidationError struct {
    Field   string  // Field that failed validation
    Message string  // Error message
}
```

Common error examples:

```go
// Name validation
ValidationError{
    Field: "name",
    Message: "cluster name must be DNS-compatible: lowercase alphanumeric and hyphens, 3-63 characters"
}

// Version validation
ValidationError{
    Field: "version",
    Message: "version 4.19.0 not in profile allowlist: [4.20.3 4.20.4]"
}

// TTL validation
ValidationError{
    Field: "ttlHours",
    Message: "TTL 100 hours exceeds profile max 72 hours"
}

// Tag validation
ValidationError{
    Field: "extraTags",
    Message: "cannot override reserved tag key: ManagedBy"
}
```

## Testing

```bash
# Run policy tests
go test ./internal/policy/...

# Run with verbose output
go test -v ./internal/policy/...

# Test specific validation
go test ./internal/policy/... -run TestEngine_ValidateCreateRequest
```

## Integration Example

```go
// In API handler
func (h *Handler) CreateCluster(w http.ResponseWriter, r *http.Request) {
    var req policy.CreateClusterRequest
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        http.Error(w, "invalid request", 400)
        return
    }

    // Validate request
    result, err := h.policyEngine.ValidateCreateRequest(&req)
    if err != nil {
        http.Error(w, "validation engine error", 500)
        return
    }

    if !result.Valid {
        // Return validation errors
        response := map[string]interface{}{
            "code": "VALIDATION_ERROR",
            "message": "Request validation failed",
            "errors": result.Errors,
        }
        w.WriteHeader(422)
        json.NewEncoder(w).Encode(response)
        return
    }

    // Create cluster with merged tags
    cluster := &types.Cluster{
        ID:            types.GenerateClusterID(),
        Name:          req.Name,
        EffectiveTags: result.MergedTags,
        DestroyAt:     parseTime(result.DestroyAt),
        // ... other fields
    }

    // Store in database
    h.store.Clusters.Create(r.Context(), cluster)

    // Success response
    w.WriteHeader(202)
    json.NewEncoder(w).Encode(map[string]string{
        "clusterId": cluster.ID,
        "status": "PENDING",
    })
}
```

## Policy Evolution

When adding new validation rules:

1. Add validation function in `engine.go`
2. Call from `ValidateCreateRequest`
3. Add test cases in `engine_test.go`
4. Update this README

Example:

```go
// Add new validation rule
func (e *Engine) validateNetworking(req *CreateClusterRequest, prof *profile.Profile, result *ValidationResult) {
    // Custom networking validation
    if req.CustomCIDR != "" {
        // Validate CIDR format
        if !isValidCIDR(req.CustomCIDR) {
            result.AddError("customCIDR", "invalid CIDR format")
        }
    }
}

// Call in ValidateCreateRequest
func (e *Engine) ValidateCreateRequest(req *CreateClusterRequest) (*ValidationResult, error) {
    // ... existing validations
    e.validateNetworking(req, prof, result)
    // ...
}
```
