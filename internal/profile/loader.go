package profile

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-playground/validator/v10"
	"gopkg.in/yaml.v3"
)

// Loader loads cluster profiles from YAML files
type Loader struct {
	profilesDir string
	validate    *validator.Validate
}

// NewLoader creates a new profile loader
func NewLoader(profilesDir string) *Loader {
	v := validator.New()

	// Register custom validator for odd numbers (control plane replicas)
	v.RegisterValidation("odd", func(fl validator.FieldLevel) bool {
		value := fl.Field().Int()
		return value%2 == 1
	})

	return &Loader{
		profilesDir: profilesDir,
		validate:    v,
	}
}

// Load loads a single profile by name
func (l *Loader) Load(name string) (*Profile, error) {
	filename := filepath.Join(l.profilesDir, name+".yaml")

	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("read profile file %s: %w", filename, err)
	}

	var profile Profile
	if err := yaml.Unmarshal(data, &profile); err != nil {
		return nil, fmt.Errorf("parse profile YAML %s: %w", filename, err)
	}

	// Validate profile
	if err := l.Validate(&profile); err != nil {
		return nil, fmt.Errorf("validate profile %s: %w", name, err)
	}

	return &profile, nil
}

// LoadAll loads all profiles from the profiles directory
func (l *Loader) LoadAll() ([]*Profile, error) {
	entries, err := os.ReadDir(l.profilesDir)
	if err != nil {
		return nil, fmt.Errorf("read profiles directory: %w", err)
	}

	profiles := []*Profile{}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		// Skip non-YAML files and the schema file
		if !strings.HasSuffix(entry.Name(), ".yaml") && !strings.HasSuffix(entry.Name(), ".yml") {
			continue
		}
		if entry.Name() == "SCHEMA.md" {
			continue
		}

		// Extract profile name from filename
		name := strings.TrimSuffix(entry.Name(), ".yaml")
		name = strings.TrimSuffix(name, ".yml")

		profile, err := l.Load(name)
		if err != nil {
			return nil, fmt.Errorf("load profile %s: %w", name, err)
		}

		profiles = append(profiles, profile)
	}

	if len(profiles) == 0 {
		return nil, fmt.Errorf("no profiles found in %s", l.profilesDir)
	}

	return profiles, nil
}

// Validate validates a profile against the schema
func (l *Loader) Validate(profile *Profile) error {
	if err := l.validate.Struct(profile); err != nil {
		return fmt.Errorf("validation failed: %w", err)
	}

	// Additional custom validations

	// 1. Default version must be in allowlist
	if !contains(profile.OpenshiftVersions.Allowlist, profile.OpenshiftVersions.Default) {
		return fmt.Errorf("default version %s not in allowlist", profile.OpenshiftVersions.Default)
	}

	// 2. Default region must be in allowlist
	if !contains(profile.Regions.Allowlist, profile.Regions.Default) {
		return fmt.Errorf("default region %s not in allowlist", profile.Regions.Default)
	}

	// 3. Default base domain must be in allowlist
	if !contains(profile.BaseDomains.Allowlist, profile.BaseDomains.Default) {
		return fmt.Errorf("default base domain %s not in allowlist", profile.BaseDomains.Default)
	}

	// 4. Platform-specific config must match platform
	if profile.Platform == "aws" && profile.PlatformConfig.AWS == nil {
		return fmt.Errorf("aws platform requires platformConfig.aws")
	}
	if profile.Platform == "ibmcloud" && profile.PlatformConfig.IBMCloud == nil {
		return fmt.Errorf("ibmcloud platform requires platformConfig.ibmcloud")
	}

	// 5. Profile name must match platform prefix
	expectedPrefix := string(profile.Platform) + "-"
	if !strings.HasPrefix(profile.Name, expectedPrefix) {
		return fmt.Errorf("profile name %s must start with %s", profile.Name, expectedPrefix)
	}

	// 6. Worker max replicas must be >= min replicas (already validated by struct tag, but explicit check)
	if profile.Compute.Workers.MaxReplicas < profile.Compute.Workers.MinReplicas {
		return fmt.Errorf("worker maxReplicas (%d) must be >= minReplicas (%d)",
			profile.Compute.Workers.MaxReplicas, profile.Compute.Workers.MinReplicas)
	}

	// 7. Worker replicas must be within bounds
	if profile.Compute.Workers.Replicas < profile.Compute.Workers.MinReplicas {
		return fmt.Errorf("worker replicas (%d) must be >= minReplicas (%d)",
			profile.Compute.Workers.Replicas, profile.Compute.Workers.MinReplicas)
	}
	if profile.Compute.Workers.Replicas > profile.Compute.Workers.MaxReplicas {
		return fmt.Errorf("worker replicas (%d) must be <= maxReplicas (%d)",
			profile.Compute.Workers.Replicas, profile.Compute.Workers.MaxReplicas)
	}

	return nil
}

// contains checks if a slice contains a string
func contains(slice []string, str string) bool {
	for _, s := range slice {
		if s == str {
			return true
		}
	}
	return false
}
