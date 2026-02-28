package profile_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tsanders-rh/ocpctl/internal/profile"
)

func TestLoader_LoadProfile(t *testing.T) {
	loader := profile.NewLoader("definitions")

	t.Run("loads aws-minimal-test profile", func(t *testing.T) {
		prof, err := loader.Load("aws-minimal-test")
		require.NoError(t, err)
		require.NotNil(t, prof)

		assert.Equal(t, "aws-minimal-test", prof.Name)
		assert.Equal(t, "aws", string(prof.Platform))
		assert.True(t, prof.Enabled)
		assert.Equal(t, 3, prof.Compute.ControlPlane.Replicas)
		assert.Equal(t, 0, prof.Compute.Workers.Replicas)
		assert.True(t, prof.Compute.ControlPlane.Schedulable)
		assert.Equal(t, 72, prof.Lifecycle.MaxTTLHours)
		assert.Equal(t, 24, prof.Lifecycle.DefaultTTLHours)
	})

	t.Run("loads aws-standard profile", func(t *testing.T) {
		prof, err := loader.Load("aws-standard")
		require.NoError(t, err)
		require.NotNil(t, prof)

		assert.Equal(t, "aws-standard", prof.Name)
		assert.Equal(t, "aws", string(prof.Platform))
		assert.True(t, prof.Enabled)
		assert.Equal(t, 3, prof.Compute.ControlPlane.Replicas)
		assert.Equal(t, 3, prof.Compute.Workers.Replicas)
		assert.False(t, prof.Compute.ControlPlane.Schedulable)
		assert.Equal(t, 168, prof.Lifecycle.MaxTTLHours)
	})

	t.Run("returns error for non-existent profile", func(t *testing.T) {
		_, err := loader.Load("non-existent")
		assert.Error(t, err)
	})
}

func TestLoader_LoadAll(t *testing.T) {
	loader := profile.NewLoader("definitions")

	profiles, err := loader.LoadAll()
	require.NoError(t, err)
	require.NotEmpty(t, profiles)

	// Should load all 4 profiles
	assert.GreaterOrEqual(t, len(profiles), 2) // At least AWS profiles

	// Verify each profile is valid
	for _, prof := range profiles {
		assert.NotEmpty(t, prof.Name)
		assert.NotEmpty(t, prof.Platform)
		assert.NotEmpty(t, prof.OpenshiftVersions.Allowlist)
	}
}

func TestLoader_Validate(t *testing.T) {
	loader := profile.NewLoader("definitions")

	t.Run("validates control plane replicas are odd", func(t *testing.T) {
		prof := &profile.Profile{
			Name:     "test-invalid",
			Platform: "aws",
			Compute: profile.ComputeConfig{
				ControlPlane: profile.ControlPlaneConfig{
					Replicas:     2, // Even number - invalid!
					InstanceType: "m6i.xlarge",
				},
				Workers: profile.WorkersConfig{
					Replicas:     0,
					MinReplicas:  0,
					MaxReplicas:  3,
					InstanceType: "m6i.2xlarge",
				},
			},
			OpenshiftVersions: profile.VersionConfig{
				Allowlist: []string{"4.20.3"},
				Default:   "4.20.3",
			},
			Regions: profile.RegionConfig{
				Allowlist: []string{"us-east-1"},
				Default:   "us-east-1",
			},
			BaseDomains: profile.BaseDomainConfig{
				Allowlist: []string{"labs.example.com"},
				Default:   "labs.example.com",
			},
			Lifecycle: profile.LifecycleConfig{
				MaxTTLHours:     72,
				DefaultTTLHours: 24,
			},
			Tags: profile.TagsConfig{
				Required:      map[string]string{},
				Defaults:      map[string]string{},
				AllowUserTags: true,
			},
		}

		err := loader.Validate(prof)
		assert.Error(t, err)
	})

	t.Run("validates default version in allowlist", func(t *testing.T) {
		prof := &profile.Profile{
			Name:        "aws-test",
			DisplayName: "Test Profile",
			Description: "Test description",
			Platform:    "aws",
			Enabled:     true,
			Compute: profile.ComputeConfig{
				ControlPlane: profile.ControlPlaneConfig{
					Replicas:     3,
					InstanceType: "m6i.xlarge",
				},
				Workers: profile.WorkersConfig{
					Replicas:     0,
					MinReplicas:  0,
					MaxReplicas:  3,
					InstanceType: "m6i.2xlarge",
				},
			},
			OpenshiftVersions: profile.VersionConfig{
				Allowlist: []string{"4.20.3"},
				Default:   "4.20.4", // Not in allowlist!
			},
			Regions: profile.RegionConfig{
				Allowlist: []string{"us-east-1"},
				Default:   "us-east-1",
			},
			BaseDomains: profile.BaseDomainConfig{
				Allowlist: []string{"labs.example.com"},
				Default:   "labs.example.com",
			},
			Lifecycle: profile.LifecycleConfig{
				MaxTTLHours:     72,
				DefaultTTLHours: 24,
			},
			Tags: profile.TagsConfig{
				Required:      map[string]string{},
				Defaults:      map[string]string{},
				AllowUserTags: true,
			},
			PlatformConfig: profile.PlatformConfig{
				AWS: &profile.AWSConfig{},
			},
		}

		err := loader.Validate(prof)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not in allowlist")
	})
}
