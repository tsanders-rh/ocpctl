package store_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tsanders-rh/ocpctl/internal/store"
	"github.com/tsanders-rh/ocpctl/pkg/types"
)

var ctx = context.Background()

// TestAddonStore_Create verifies addon creation with proper defaults
func TestAddonStore_Create(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test")
	}

	t.Run("creates system addon successfully", func(t *testing.T) {
		pool := setupTestDB(t)
		defer pool.Close()
		s := store.New(pool)

		addon := &types.PostConfigAddon{
			AddonID:            "test-addon",
			Name:               "Test Addon",
			Description:        "Test Description",
			Category:           "backup",
			SupportedPlatforms: []string{"openshift"},
			Enabled:            true,
			Version:            "v1.0",
			DisplayName:        "Test Addon v1.0",
			IsDefault:          true,
			AddonSource:        "system",
			IsPublished:        false,
			IsImmutable:        false,
			VersionNumber:      1,
		}

		// Marshal config
		addon.ConfigJSON = []byte(`{"operators":[],"scripts":[],"manifests":[],"helmCharts":[]}`)

		err := s.PostConfigAddons.Create(ctx, addon)
		require.NoError(t, err)
		assert.NotEmpty(t, addon.ID)
		assert.NotZero(t, addon.CreatedAt)

		// Verify
		retrieved, err := s.PostConfigAddons.GetByID(ctx, addon.ID)
		require.NoError(t, err)
		assert.Equal(t, "test-addon", retrieved.AddonID)
		assert.Equal(t, "system", retrieved.AddonSource)
		assert.False(t, retrieved.IsImmutable)
	})

	t.Run("creates user addon successfully", func(t *testing.T) {
		pool := setupTestDB(t)
		defer pool.Close()
		s := store.New(pool)

		userID := "user-123"
		addon := &types.PostConfigAddon{
			AddonID:            "custom-addon",
			Name:               "Custom Addon",
			Description:        "User custom addon",
			Category:           "monitoring",
			SupportedPlatforms: []string{"openshift"},
			Enabled:            true,
			Version:            "draft",
			DisplayName:        "Custom Addon Draft",
			IsDefault:          false,
			AddonSource:        "user",
			CreatedByUserID:    &userID,
			IsPublished:        false,
			IsImmutable:        false,
			VersionNumber:      1,
		}

		addon.ConfigJSON = []byte(`{"operators":[],"scripts":[],"manifests":[],"helmCharts":[]}`)

		err := s.PostConfigAddons.Create(ctx, addon)
		require.NoError(t, err)

		// Verify
		retrieved, err := s.PostConfigAddons.GetByID(ctx, addon.ID)
		require.NoError(t, err)
		assert.Equal(t, "user", retrieved.AddonSource)
		assert.Equal(t, &userID, retrieved.CreatedByUserID)
		assert.False(t, retrieved.IsPublished)
		assert.False(t, retrieved.IsImmutable)
	})
}

// TestAddonStore_GetByAddonIDAndVersion verifies version-specific retrieval
func TestAddonStore_GetByAddonIDAndVersion(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test")
	}

	t.Run("retrieves specific version", func(t *testing.T) {
		pool := setupTestDB(t)
		defer pool.Close()
		s := store.New(pool)

		// Create multiple versions
		for _, version := range []string{"v1.0", "v1.1", "v2.0"} {
			addon := &types.PostConfigAddon{
				AddonID:            "versioned-addon",
				Name:               "Versioned Addon",
				Description:        "Multi-version addon",
				Category:           "backup",
				SupportedPlatforms: []string{"openshift"},
				Enabled:            true,
				Version:            version,
				DisplayName:        "Versioned Addon " + version,
				IsDefault:          version == "v2.0",
				AddonSource:        "system",
				ConfigJSON:         []byte(`{"operators":[]}`),
			}
			err := s.PostConfigAddons.Create(ctx, addon)
			require.NoError(t, err)
		}

		// Retrieve specific version
		addon, err := s.PostConfigAddons.GetByAddonIDAndVersion(ctx, "versioned-addon", "v1.1")
		require.NoError(t, err)
		assert.Equal(t, "v1.1", addon.Version)
		assert.False(t, addon.IsDefault)
	})

	t.Run("returns error for nonexistent version", func(t *testing.T) {
		pool := setupTestDB(t)
		defer pool.Close()
		s := store.New(pool)

		_, err := s.PostConfigAddons.GetByAddonIDAndVersion(ctx, "nonexistent", "v1.0")
		assert.ErrorIs(t, err, store.ErrNotFound)
	})
}

// TestAddonStore_Update verifies update logic and immutability enforcement
func TestAddonStore_Update(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test")
	}

	t.Run("updates system addon successfully", func(t *testing.T) {
		pool := setupTestDB(t)
		defer pool.Close()
		s := store.New(pool)

		// Create system addon
		addon := &types.PostConfigAddon{
			AddonID:            "updatable-system",
			Name:               "Original Name",
			Description:        "Original Description",
			Category:           "backup",
			SupportedPlatforms: []string{"openshift"},
			Enabled:            true,
			Version:            "v1.0",
			DisplayName:        "Original",
			AddonSource:        "system",
			IsImmutable:        false,
			ConfigJSON:         []byte(`{"operators":[]}`),
		}
		err := s.PostConfigAddons.Create(ctx, addon)
		require.NoError(t, err)

		// Update it
		updated := &types.PostConfigAddon{
			Name:        "Updated Name",
			Description: "Updated Description",
			Category:    "backup",
			ConfigJSON:  []byte(`{"operators":[{"name":"updated"}]}`),
		}
		err = s.PostConfigAddons.Update(ctx, addon.ID, updated)
		require.NoError(t, err)

		// Verify
		retrieved, err := s.PostConfigAddons.GetByID(ctx, addon.ID)
		require.NoError(t, err)
		assert.Equal(t, "Updated Name", retrieved.Name)
		assert.Equal(t, "Updated Description", retrieved.Description)
	})

	t.Run("updates unpublished user addon successfully", func(t *testing.T) {
		pool := setupTestDB(t)
		defer pool.Close()
		s := store.New(pool)

		userID := "user-123"
		addon := &types.PostConfigAddon{
			AddonID:         "user-draft",
			Name:            "Draft Addon",
			Description:     "Draft",
			Category:        "monitoring",
			AddonSource:     "user",
			CreatedByUserID: &userID,
			IsPublished:     false,
			IsImmutable:     false,
			ConfigJSON:      []byte(`{}`),
		}
		err := s.PostConfigAddons.Create(ctx, addon)
		require.NoError(t, err)

		// Update draft
		updated := &types.PostConfigAddon{
			Name:        "Updated Draft",
			Description: "Updated",
			Category:    "monitoring",
			ConfigJSON:  []byte(`{"updated":true}`),
		}
		err = s.PostConfigAddons.Update(ctx, addon.ID, updated)
		require.NoError(t, err)

		// Verify
		retrieved, err := s.PostConfigAddons.GetByID(ctx, addon.ID)
		require.NoError(t, err)
		assert.Equal(t, "Updated Draft", retrieved.Name)
	})

	t.Run("prevents update of published immutable user addon", func(t *testing.T) {
		pool := setupTestDB(t)
		defer pool.Close()
		s := store.New(pool)

		userID := "user-123"
		addon := &types.PostConfigAddon{
			AddonID:         "published-addon",
			Name:            "Published Addon",
			Description:     "Published",
			Category:        "backup",
			AddonSource:     "user",
			CreatedByUserID: &userID,
			IsPublished:     true,
			IsImmutable:     true,
			ConfigJSON:      []byte(`{}`),
		}
		err := s.PostConfigAddons.Create(ctx, addon)
		require.NoError(t, err)

		// Attempt update
		updated := &types.PostConfigAddon{
			Name:       "Should Not Update",
			ConfigJSON: []byte(`{"updated":true}`),
		}
		err = s.PostConfigAddons.Update(ctx, addon.ID, updated)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "cannot update published addon")
	})
}

// TestAddonStore_PublishAddon verifies publish workflow
func TestAddonStore_PublishAddon(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test")
	}

	t.Run("publishes user addon successfully", func(t *testing.T) {
		pool := setupTestDB(t)
		defer pool.Close()
		s := store.New(pool)

		userID := "user-123"
		addon := &types.PostConfigAddon{
			AddonID:         "publish-test",
			Name:            "To Be Published",
			Category:        "monitoring",
			AddonSource:     "user",
			CreatedByUserID: &userID,
			IsPublished:     false,
			IsImmutable:     false,
			ConfigJSON:      []byte(`{}`),
		}
		err := s.PostConfigAddons.Create(ctx, addon)
		require.NoError(t, err)

		// Publish
		err = s.PostConfigAddons.PublishAddon(ctx, addon.ID)
		require.NoError(t, err)

		// Verify
		retrieved, err := s.PostConfigAddons.GetByID(ctx, addon.ID)
		require.NoError(t, err)
		assert.True(t, retrieved.IsPublished)
		assert.True(t, retrieved.IsImmutable)
		assert.NotNil(t, retrieved.PublishedAt)
	})

	t.Run("prevents double publish", func(t *testing.T) {
		pool := setupTestDB(t)
		defer pool.Close()
		s := store.New(pool)

		userID := "user-123"
		addon := &types.PostConfigAddon{
			AddonID:         "already-published",
			Name:            "Already Published",
			Category:        "backup",
			AddonSource:     "user",
			CreatedByUserID: &userID,
			IsPublished:     true,
			IsImmutable:     true,
			ConfigJSON:      []byte(`{}`),
		}
		err := s.PostConfigAddons.Create(ctx, addon)
		require.NoError(t, err)

		// Attempt second publish
		err = s.PostConfigAddons.PublishAddon(ctx, addon.ID)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "already published")
	})

	t.Run("rejects publishing system addon", func(t *testing.T) {
		pool := setupTestDB(t)
		defer pool.Close()
		s := store.New(pool)

		addon := &types.PostConfigAddon{
			AddonID:     "system-addon",
			Name:        "System Addon",
			Category:    "backup",
			AddonSource: "system",
			IsPublished: false,
			ConfigJSON:  []byte(`{}`),
		}
		err := s.PostConfigAddons.Create(ctx, addon)
		require.NoError(t, err)

		// Attempt publish
		err = s.PostConfigAddons.PublishAddon(ctx, addon.ID)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not a user addon")
	})
}

// TestAddonStore_CloneAddon verifies addon cloning with version lineage
func TestAddonStore_CloneAddon(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test")
	}

	t.Run("clones published addon successfully", func(t *testing.T) {
		pool := setupTestDB(t)
		defer pool.Close()
		s := store.New(pool)

		originalUserID := "user-123"
		cloneUserID := "user-456"

		// Create and publish original
		original := &types.PostConfigAddon{
			AddonID:         "original",
			Name:            "Original Addon",
			Description:     "Original Description",
			Category:        "backup",
			AddonSource:     "user",
			CreatedByUserID: &originalUserID,
			IsPublished:     true,
			IsImmutable:     true,
			VersionNumber:   1,
			ConfigJSON:      []byte(`{"key":"value"}`),
		}
		err := s.PostConfigAddons.Create(ctx, original)
		require.NoError(t, err)

		// Clone it
		clone, err := s.PostConfigAddons.CloneAddon(ctx, original.ID, cloneUserID)
		require.NoError(t, err)

		// Verify clone properties
		assert.NotEqual(t, original.ID, clone.ID)
		assert.Equal(t, "original-v2", clone.AddonID)
		assert.Equal(t, "Original Addon", clone.Name)
		assert.Equal(t, "backup", clone.Category)
		assert.Equal(t, "user", clone.AddonSource)
		assert.Equal(t, &cloneUserID, clone.CreatedByUserID)
		assert.False(t, clone.IsPublished)
		assert.False(t, clone.IsImmutable)
		assert.False(t, clone.IsDefault)
		assert.Equal(t, 2, clone.VersionNumber)
		assert.Equal(t, &original.ID, clone.ParentVersionID)

		// Config should be copied
		assert.Equal(t, `{"key":"value"}`, string(clone.ConfigJSON))
	})

	t.Run("maintains version lineage", func(t *testing.T) {
		pool := setupTestDB(t)
		defer pool.Close()
		s := store.New(pool)

		userID := "user-123"

		// Create v1 and publish
		v1 := &types.PostConfigAddon{
			AddonID:         "lineage-test",
			Name:            "Lineage Test",
			Category:        "monitoring",
			AddonSource:     "user",
			CreatedByUserID: &userID,
			VersionNumber:   1,
			ConfigJSON:      []byte(`{}`),
		}
		err := s.PostConfigAddons.Create(ctx, v1)
		require.NoError(t, err)
		err = s.PostConfigAddons.PublishAddon(ctx, v1.ID)
		require.NoError(t, err)

		// Clone to create v2
		v2, err := s.PostConfigAddons.CloneAddon(ctx, v1.ID, userID)
		require.NoError(t, err)
		assert.Equal(t, 2, v2.VersionNumber)
		assert.Equal(t, &v1.ID, v2.ParentVersionID)

		// Publish v2
		err = s.PostConfigAddons.PublishAddon(ctx, v2.ID)
		require.NoError(t, err)

		// Clone to create v3
		v3, err := s.PostConfigAddons.CloneAddon(ctx, v2.ID, userID)
		require.NoError(t, err)
		assert.Equal(t, 3, v3.VersionNumber)
		assert.Equal(t, &v2.ID, v3.ParentVersionID)
	})
}

// TestAddonStore_GetUserAddons verifies user-specific addon retrieval
func TestAddonStore_GetUserAddons(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test")
	}

	t.Run("retrieves only user's addons", func(t *testing.T) {
		pool := setupTestDB(t)
		defer pool.Close()
		s := store.New(pool)

		user1ID := "user-111"
		user2ID := "user-222"

		// Create addons for user1
		for i := 0; i < 3; i++ {
			addon := &types.PostConfigAddon{
				AddonID:         fmt.Sprintf("user1-addon-%d", i),
				Name:            fmt.Sprintf("User1 Addon %d", i),
				Category:        "backup",
				AddonSource:     "user",
				CreatedByUserID: &user1ID,
				ConfigJSON:      []byte(`{}`),
			}
			err := s.PostConfigAddons.Create(ctx, addon)
			require.NoError(t, err)
		}

		// Create addons for user2
		for i := 0; i < 2; i++ {
			addon := &types.PostConfigAddon{
				AddonID:         fmt.Sprintf("user2-addon-%d", i),
				Name:            fmt.Sprintf("User2 Addon %d", i),
				Category:        "monitoring",
				AddonSource:     "user",
				CreatedByUserID: &user2ID,
				ConfigJSON:      []byte(`{}`),
			}
			err := s.PostConfigAddons.Create(ctx, addon)
			require.NoError(t, err)
		}

		// Get user1's addons
		user1Addons, err := s.PostConfigAddons.GetUserAddons(ctx, user1ID)
		require.NoError(t, err)
		assert.Len(t, user1Addons, 3)
		for _, addon := range user1Addons {
			assert.Equal(t, &user1ID, addon.CreatedByUserID)
		}

		// Get user2's addons
		user2Addons, err := s.PostConfigAddons.GetUserAddons(ctx, user2ID)
		require.NoError(t, err)
		assert.Len(t, user2Addons, 2)
		for _, addon := range user2Addons {
			assert.Equal(t, &user2ID, addon.CreatedByUserID)
		}
	})
}

// TestAddonStore_ListVersions verifies version listing
func TestAddonStore_ListVersions(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test")
	}

	t.Run("lists all versions for addon", func(t *testing.T) {
		pool := setupTestDB(t)
		defer pool.Close()
		s := store.New(pool)

		// Create multiple versions
		versions := []string{"v1.0", "v1.1", "v2.0"}
		for _, version := range versions {
			addon := &types.PostConfigAddon{
				AddonID:     "multi-version",
				Name:        "Multi Version Addon",
				Category:    "backup",
				Version:     version,
				IsDefault:   version == "v2.0",
				AddonSource: "system",
				ConfigJSON:  []byte(`{}`),
			}
			err := s.PostConfigAddons.Create(ctx, addon)
			require.NoError(t, err)
		}

		// List versions
		results, err := s.PostConfigAddons.ListVersions(ctx, "multi-version")
		require.NoError(t, err)
		assert.Len(t, results, 3)

		// Verify default is first
		assert.True(t, results[0].IsDefault)
		assert.Equal(t, "v2.0", results[0].Version)
	})
}

// Helper functions are in clusters_test.go to avoid duplication
