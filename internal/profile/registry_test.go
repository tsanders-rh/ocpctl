package profile_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tsanders-rh/ocpctl/internal/profile"
	"github.com/tsanders-rh/ocpctl/pkg/types"
)

func TestRegistry_Get(t *testing.T) {
	loader := profile.NewLoader("definitions")
	registry, err := profile.NewRegistry(loader)
	require.NoError(t, err)

	t.Run("retrieves existing profile", func(t *testing.T) {
		prof, err := registry.Get("aws-minimal-test")
		require.NoError(t, err)
		assert.Equal(t, "aws-minimal-test", prof.Name)
	})

	t.Run("returns error for non-existent profile", func(t *testing.T) {
		_, err := registry.Get("non-existent")
		assert.Error(t, err)
	})

	t.Run("returns error for disabled profile", func(t *testing.T) {
		// IBM profiles are disabled in Phase 1
		_, err := registry.Get("ibmcloud-minimal-test")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "disabled")
	})
}

func TestRegistry_List(t *testing.T) {
	loader := profile.NewLoader("definitions")
	registry, err := profile.NewRegistry(loader)
	require.NoError(t, err)

	profiles := registry.List()
	assert.NotEmpty(t, profiles)

	// All returned profiles should be enabled
	for _, prof := range profiles {
		assert.True(t, prof.Enabled, "profile %s should be enabled", prof.Name)
	}
}

func TestRegistry_ListByPlatform(t *testing.T) {
	loader := profile.NewLoader("definitions")
	registry, err := profile.NewRegistry(loader)
	require.NoError(t, err)

	t.Run("lists AWS profiles", func(t *testing.T) {
		profiles := registry.ListByPlatform(types.PlatformAWS)
		assert.NotEmpty(t, profiles)

		for _, prof := range profiles {
			assert.Equal(t, types.PlatformAWS, prof.Platform)
			assert.True(t, prof.Enabled)
		}
	})

	t.Run("lists IBM Cloud profiles", func(t *testing.T) {
		profiles := registry.ListByPlatform(types.PlatformIBMCloud)
		// IBM profiles are disabled in Phase 1
		assert.Empty(t, profiles)
	})
}

func TestRegistry_Exists(t *testing.T) {
	loader := profile.NewLoader("definitions")
	registry, err := profile.NewRegistry(loader)
	require.NoError(t, err)

	assert.True(t, registry.Exists("aws-minimal-test"))
	assert.True(t, registry.Exists("aws-standard"))
	assert.False(t, registry.Exists("non-existent"))
	assert.False(t, registry.Exists("ibmcloud-minimal-test")) // Disabled
}

func TestRegistry_Count(t *testing.T) {
	loader := profile.NewLoader("definitions")
	registry, err := profile.NewRegistry(loader)
	require.NoError(t, err)

	total := registry.Count()
	enabled := registry.CountEnabled()

	assert.GreaterOrEqual(t, total, 4) // 4 profiles defined
	assert.GreaterOrEqual(t, enabled, 2) // At least 2 AWS profiles enabled
	assert.LessOrEqual(t, enabled, total) // Enabled <= total
}
