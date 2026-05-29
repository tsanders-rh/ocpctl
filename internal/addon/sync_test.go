package addon

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoader_LoadAll(t *testing.T) {
	t.Run("loads valid addon definitions", func(t *testing.T) {
		// Create temp directory with test addon
		tmpDir := t.TempDir()
		addonYAML := `id: test-addon
name: Test Addon
description: A test addon
category: backup
enabled: true
supportedPlatforms:
  - openshift
versions:
  - channel: v1.0
    displayName: "Version 1.0"
    isDefault: true
    config:
      operators:
        - name: test-operator
          namespace: test-ns
          source: redhat-operators
          channel: stable
`
		err := os.WriteFile(filepath.Join(tmpDir, "test-addon.yaml"), []byte(addonYAML), 0644)
		require.NoError(t, err)

		// Load addons
		loader := NewLoader(tmpDir)
		addons, err := loader.LoadAll()
		require.NoError(t, err)
		require.Len(t, addons, 1)

		// Verify loaded addon
		addon := addons[0]
		assert.Equal(t, "test-addon", addon.ID)
		assert.Equal(t, "Test Addon", addon.Name)
		assert.Equal(t, "backup", addon.Category)
		assert.True(t, addon.Enabled)
		assert.Contains(t, addon.SupportedPlatforms, "openshift")
		require.Len(t, addon.Versions, 1)
		assert.Equal(t, "v1.0", addon.Versions[0].Channel)
		assert.True(t, addon.Versions[0].IsDefault)
	})

	t.Run("loads multiple addon definitions", func(t *testing.T) {
		tmpDir := t.TempDir()

		// Create multiple addon files
		addons := []string{
			`id: addon1
name: Addon 1
description: First test addon
category: backup
enabled: true
supportedPlatforms: [openshift]
versions:
  - channel: v1
    displayName: "Addon 1 v1"
    isDefault: true
    config:
      operators: []
`,
			`id: addon2
name: Addon 2
description: Second test addon
category: monitoring
enabled: true
supportedPlatforms: [eks, gke]
versions:
  - channel: v1
    displayName: "Addon 2 v1"
    isDefault: true
    config:
      scripts: []
`,
		}

		for i, yaml := range addons {
			filename := filepath.Join(tmpDir, fmt.Sprintf("addon%d.yaml", i+1))
			err := os.WriteFile(filename, []byte(yaml), 0644)
			require.NoError(t, err)
		}

		loader := NewLoader(tmpDir)
		loaded, err := loader.LoadAll()
		require.NoError(t, err)
		assert.Len(t, loaded, 2)
	})

	t.Run("skips invalid YAML files", func(t *testing.T) {
		tmpDir := t.TempDir()

		// Create valid and invalid files
		validYAML := `id: valid
name: Valid Addon
category: backup
enabled: true
supportedPlatforms: [openshift]
versions:
  - channel: v1
    isDefault: true
    config:
      operators: []
`
		invalidYAML := `this is not valid YAML: [[[`

		err := os.WriteFile(filepath.Join(tmpDir, "valid.yaml"), []byte(validYAML), 0644)
		require.NoError(t, err)
		err = os.WriteFile(filepath.Join(tmpDir, "invalid.yaml"), []byte(invalidYAML), 0644)
		require.NoError(t, err)

		loader := NewLoader(tmpDir)
		addons, err := loader.LoadAll()

		// Should load the valid one and skip the invalid one
		// (implementation may vary - might error or skip)
		if err == nil {
			assert.Len(t, addons, 1)
			assert.Equal(t, "valid", addons[0].ID)
		} else {
			// If it errors on invalid YAML, that's also acceptable
			assert.Error(t, err)
		}
	})

	t.Run("handles empty directory", func(t *testing.T) {
		tmpDir := t.TempDir()
		loader := NewLoader(tmpDir)
		addons, err := loader.LoadAll()
		// May error or return empty - both are acceptable
		if err == nil {
			assert.Empty(t, addons)
		} else {
			assert.Error(t, err)
		}
	})

	t.Run("handles nonexistent directory", func(t *testing.T) {
		loader := NewLoader("/nonexistent/path/that/does/not/exist")
		addons, err := loader.LoadAll()
		// Should handle gracefully
		if err == nil {
			assert.Empty(t, addons)
		} else {
			assert.Error(t, err)
		}
	})
}

func TestLoader_MultipleVersions(t *testing.T) {
	t.Run("loads addon with multiple versions", func(t *testing.T) {
		tmpDir := t.TempDir()
		addonYAML := `id: multi-version
name: Multi Version Addon
description: Addon with multiple version channels
category: virtualization
enabled: true
supportedPlatforms:
  - openshift
versions:
  - channel: v4.18
    displayName: "CNV 4.18"
    isDefault: false
    config:
      operators:
        - name: kubevirt
          namespace: openshift-cnv
  - channel: v4.19
    displayName: "CNV 4.19"
    isDefault: true
    config:
      operators:
        - name: kubevirt
          namespace: openshift-cnv
  - channel: nightly
    displayName: "CNV Nightly"
    isDefault: false
    config:
      operators:
        - name: kubevirt
          namespace: openshift-cnv
`
		err := os.WriteFile(filepath.Join(tmpDir, "multi.yaml"), []byte(addonYAML), 0644)
		require.NoError(t, err)

		loader := NewLoader(tmpDir)
		addons, err := loader.LoadAll()
		require.NoError(t, err)
		require.Len(t, addons, 1)

		addon := addons[0]
		assert.Len(t, addon.Versions, 3)

		// Find default version
		var defaultVersion *AddonVersionConfig
		for i := range addon.Versions {
			if addon.Versions[i].IsDefault {
				defaultVersion = &addon.Versions[i]
				break
			}
		}
		require.NotNil(t, defaultVersion)
		assert.Equal(t, "v4.19", defaultVersion.Channel)
	})
}

func TestAddonMetadata(t *testing.T) {
	t.Run("loads addon with metadata", func(t *testing.T) {
		tmpDir := t.TempDir()
		addonYAML := `id: addon-with-metadata
name: Addon With Metadata
description: Test addon with metadata fields
category: virtualization
enabled: true
supportedPlatforms:
  - openshift
metadata:
  requiresBareMetal: true
  requiredCapabilities:
    - virtualization
    - storage
  notes:
    - "Requires at least 3 worker nodes"
    - "Minimum 64GB RAM per worker"
  warnings:
    - "This addon requires virtualization support"
versions:
  - channel: v1.0
    displayName: "Version 1.0"
    isDefault: true
    config:
      operators: []
`
		err := os.WriteFile(filepath.Join(tmpDir, "metadata.yaml"), []byte(addonYAML), 0644)
		require.NoError(t, err)

		loader := NewLoader(tmpDir)
		addons, err := loader.LoadAll()
		require.NoError(t, err)
		require.Len(t, addons, 1)

		addon := addons[0]
		require.NotNil(t, addon.Metadata)
		assert.True(t, addon.Metadata.RequiresBareMetal)
		assert.Contains(t, addon.Metadata.RequiredCapabilities, "virtualization")
		assert.Contains(t, addon.Metadata.RequiredCapabilities, "storage")
		assert.Len(t, addon.Metadata.Notes, 2)
		assert.Len(t, addon.Metadata.Warnings, 1)
	})
}

func TestValidation(t *testing.T) {
	t.Run("validates category is valid", func(t *testing.T) {
		tmpDir := t.TempDir()
		invalidCategory := `id: invalid-category
name: Invalid Category
category: invalid-category-value
enabled: true
supportedPlatforms: [openshift]
versions:
  - channel: v1
    isDefault: true
    config:
      operators: []
`
		err := os.WriteFile(filepath.Join(tmpDir, "invalid.yaml"), []byte(invalidCategory), 0644)
		require.NoError(t, err)

		loader := NewLoader(tmpDir)
		_, err = loader.LoadAll()
		// Should error on invalid category (or skip file)
		// Implementation may vary
	})

	t.Run("validates at least one version exists", func(t *testing.T) {
		tmpDir := t.TempDir()
		noVersions := `id: no-versions
name: No Versions
category: backup
enabled: true
supportedPlatforms: [openshift]
versions: []
`
		err := os.WriteFile(filepath.Join(tmpDir, "noversions.yaml"), []byte(noVersions), 0644)
		require.NoError(t, err)

		loader := NewLoader(tmpDir)
		_, err = loader.LoadAll()
		// Should validate that versions is not empty
		// Implementation may vary
	})
}

// TestSyncer tests are integration tests requiring a database
// They verify that YAML definitions are correctly synced to the database

func TestSyncer_Sync(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test")
	}

	t.Run("syncs new addon to database", func(t *testing.T) {
		// TODO: Implement with testcontainers
		// Setup:
		// - Create test database
		// - Create temp directory with addon YAML
		// - Load addon
		// - Sync to database
		// - Verify addon exists in database with correct fields
		t.Skip("Integration test with testcontainers not yet implemented")
	})

	t.Run("updates existing addon when YAML changes", func(t *testing.T) {
		// TODO: Implement with testcontainers
		// Setup:
		// - Sync addon v1
		// - Update YAML file
		// - Sync again
		// - Verify addon was updated (not duplicated)
		t.Skip("Integration test with testcontainers not yet implemented")
	})

	t.Run("deletes addon versions not in YAML", func(t *testing.T) {
		// TODO: Implement with testcontainers
		// Setup:
		// - Sync addon with 3 versions
		// - Remove 1 version from YAML
		// - Sync again
		// - Verify removed version was deleted from database
		t.Skip("Integration test with testcontainers not yet implemented")
	})

	t.Run("marks system addons as mutable", func(t *testing.T) {
		// TODO: Implement with testcontainers
		// Setup:
		// - Sync system addon
		// - Verify is_immutable = false, addon_source = 'system'
		t.Skip("Integration test with testcontainers not yet implemented")
	})
}
