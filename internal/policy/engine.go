package policy

import (
	"fmt"
	"regexp"
	"time"

	"github.com/tsanders-rh/ocpctl/internal/profile"
)

// Engine validates cluster creation requests against profile policies
type Engine struct {
	registry *profile.Registry
}

// NewEngine creates a new policy validation engine
func NewEngine(registry *profile.Registry) *Engine {
	return &Engine{
		registry: registry,
	}
}

// ValidateCreateRequest validates a cluster creation request
func (e *Engine) ValidateCreateRequest(req *CreateClusterRequest) (*ValidationResult, error) {
	result := &ValidationResult{
		Valid:      true,
		Errors:     []ValidationError{},
		MergedTags: make(map[string]string),
	}

	// Get profile
	prof, err := e.registry.Get(req.Profile)
	if err != nil {
		result.AddError("profile", fmt.Sprintf("invalid profile: %s", err))
		return result, nil
	}

	// Validate each field against profile
	e.validateName(req, result)
	e.validatePlatform(req, prof, result)
	e.validateVersion(req, prof, result)
	e.validateRegion(req, prof, result)
	e.validateBaseDomain(req, prof, result)
	e.validateTTL(req, prof, result)
	e.validateTags(req, prof, result)
	e.validateOffhoursOptIn(req, prof, result)

	// Calculate destroy_at timestamp
	if result.Valid {
		destroyAt := time.Now().Add(time.Duration(req.TTLHours) * time.Hour)
		result.DestroyAt = destroyAt.Format(time.RFC3339)
	}

	return result, nil
}

// validateName checks cluster name is DNS-compatible
func (e *Engine) validateName(req *CreateClusterRequest, result *ValidationResult) {
	if req.Name == "" {
		result.AddError("name", "cluster name is required")
		return
	}

	// DNS-compatible: lowercase alphanumeric and hyphens, max 63 chars
	dnsPattern := regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)
	if !dnsPattern.MatchString(req.Name) {
		result.AddError("name", "cluster name must be DNS-compatible: lowercase alphanumeric and hyphens, 3-63 characters")
		return
	}

	if len(req.Name) < 3 || len(req.Name) > 63 {
		result.AddError("name", "cluster name must be between 3 and 63 characters")
	}
}

// validatePlatform checks platform matches profile
func (e *Engine) validatePlatform(req *CreateClusterRequest, prof *profile.Profile, result *ValidationResult) {
	if req.Platform != string(prof.Platform) {
		result.AddError("platform", fmt.Sprintf("platform %s does not match profile platform %s", req.Platform, prof.Platform))
	}
}

// validateVersion checks version is in profile allowlist
func (e *Engine) validateVersion(req *CreateClusterRequest, prof *profile.Profile, result *ValidationResult) {
	if req.Version == "" {
		result.AddError("version", "OpenShift version is required")
		return
	}

	// Check if version is in allowlist
	found := false
	for _, v := range prof.OpenshiftVersions.Allowlist {
		if req.Version == v {
			found = true
			break
		}
	}

	if !found {
		result.AddError("version", fmt.Sprintf("version %s not in profile allowlist: %v", req.Version, prof.OpenshiftVersions.Allowlist))
	}
}

// validateRegion checks region is in profile allowlist
func (e *Engine) validateRegion(req *CreateClusterRequest, prof *profile.Profile, result *ValidationResult) {
	if req.Region == "" {
		result.AddError("region", "region is required")
		return
	}

	// Check if region is in allowlist
	found := false
	for _, r := range prof.Regions.Allowlist {
		if req.Region == r {
			found = true
			break
		}
	}

	if !found {
		result.AddError("region", fmt.Sprintf("region %s not in profile allowlist: %v", req.Region, prof.Regions.Allowlist))
	}
}

// validateBaseDomain checks base domain is in profile allowlist
func (e *Engine) validateBaseDomain(req *CreateClusterRequest, prof *profile.Profile, result *ValidationResult) {
	if req.BaseDomain == "" {
		result.AddError("baseDomain", "base domain is required")
		return
	}

	// Check if base domain is in allowlist
	found := false
	for _, d := range prof.BaseDomains.Allowlist {
		if req.BaseDomain == d {
			found = true
			break
		}
	}

	if !found {
		result.AddError("baseDomain", fmt.Sprintf("base domain %s not in profile allowlist: %v", req.BaseDomain, prof.BaseDomains.Allowlist))
	}
}

// validateTTL checks TTL is within profile limits
func (e *Engine) validateTTL(req *CreateClusterRequest, prof *profile.Profile, result *ValidationResult) {
	if req.TTLHours <= 0 {
		result.AddError("ttlHours", "TTL must be greater than 0")
		return
	}

	if req.TTLHours > prof.Lifecycle.MaxTTLHours {
		result.AddError("ttlHours", fmt.Sprintf("TTL %d hours exceeds profile max %d hours", req.TTLHours, prof.Lifecycle.MaxTTLHours))
	}

	if !prof.Lifecycle.AllowCustomTTL && req.TTLHours != prof.Lifecycle.DefaultTTLHours {
		result.AddError("ttlHours", fmt.Sprintf("custom TTL not allowed, must use default %d hours", prof.Lifecycle.DefaultTTLHours))
	}
}

// validateTags merges required, default, and user tags
func (e *Engine) validateTags(req *CreateClusterRequest, prof *profile.Profile, result *ValidationResult) {
	// Start with default tags
	for k, v := range prof.Tags.Defaults {
		result.MergedTags[k] = v
	}

	// Add required tags (override defaults)
	for k, v := range prof.Tags.Required {
		result.MergedTags[k] = v
	}

	// Add user tags if allowed
	if prof.Tags.AllowUserTags {
		for k, v := range req.ExtraTags {
			// Check if trying to override reserved key
			if profile.IsReservedTagKey(k) {
				result.AddError("extraTags", fmt.Sprintf("cannot override reserved tag key: %s", k))
				continue
			}

			result.MergedTags[k] = v
		}
	} else if len(req.ExtraTags) > 0 {
		result.AddError("extraTags", "user-defined tags not allowed by profile")
	}

	// Add system tags (always override)
	result.MergedTags["ManagedBy"] = "cluster-control-plane"
	result.MergedTags["ClusterName"] = req.Name
	result.MergedTags["Owner"] = req.Owner
	result.MergedTags["Team"] = req.Team
	result.MergedTags["CostCenter"] = req.CostCenter
	result.MergedTags["Profile"] = req.Profile
	result.MergedTags["Platform"] = req.Platform
}

// validateOffhoursOptIn checks if off-hours scaling is supported
func (e *Engine) validateOffhoursOptIn(req *CreateClusterRequest, prof *profile.Profile, result *ValidationResult) {
	if req.OffhoursOptIn && !prof.Features.OffHoursScaling {
		result.AddError("offhoursOptIn", "off-hours scaling not supported by profile")
	}
}

// GetDefaultVersion returns the default version for a profile
func (e *Engine) GetDefaultVersion(profileName string) (string, error) {
	prof, err := e.registry.Get(profileName)
	if err != nil {
		return "", err
	}
	return prof.OpenshiftVersions.Default, nil
}

// GetDefaultRegion returns the default region for a profile
func (e *Engine) GetDefaultRegion(profileName string) (string, error) {
	prof, err := e.registry.Get(profileName)
	if err != nil {
		return "", err
	}
	return prof.Regions.Default, nil
}

// GetDefaultBaseDomain returns the default base domain for a profile
func (e *Engine) GetDefaultBaseDomain(profileName string) (string, error) {
	prof, err := e.registry.Get(profileName)
	if err != nil {
		return "", err
	}
	return prof.BaseDomains.Default, nil
}

// GetDefaultTTL returns the default TTL for a profile
func (e *Engine) GetDefaultTTL(profileName string) (int, error) {
	prof, err := e.registry.Get(profileName)
	if err != nil {
		return 0, err
	}
	return prof.Lifecycle.DefaultTTLHours, nil
}
