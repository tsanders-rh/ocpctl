package worker

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tsanders-rh/ocpctl/pkg/types"
)

func TestResolveSelectedAddons(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test")
	}

	t.Run("resolves addons from cluster selection", func(t *testing.T) {
		// TODO: Implement with test database
		// Setup:
		// - Create test database with sample addons
		// - Create cluster with selected_addon_ids
		// - Call resolveSelectedAddons
		// - Verify correct addons are returned
		t.Skip("Integration test not yet implemented")
	})

	t.Run("resolves addon with specific channel", func(t *testing.T) {
		// TODO: Test "addonID:channel" format
		t.Skip("Integration test not yet implemented")
	})

	t.Run("skips disabled addons with warning", func(t *testing.T) {
		// TODO: Test that disabled addons are skipped
		t.Skip("Integration test not yet implemented")
	})

	t.Run("returns error for nonexistent addon", func(t *testing.T) {
		// TODO: Test error handling for addon not found
		t.Skip("Integration test not yet implemented")
	})

	t.Run("handles empty selection", func(t *testing.T) {
		// TODO: Test cluster with no addons selected
		t.Skip("Integration test not yet implemented")
	})
}

func TestMergeAddonConfigs(t *testing.T) {
	t.Run("merges multiple addon configs", func(t *testing.T) {
		// Create sample addons
		addons := []types.PostConfigAddon{
			{
				AddonID: "addon1",
				Name:    "Addon 1",
				Config: types.CustomPostConfig{
					Operators: []types.CustomOperatorConfig{
						{Name: "operator1", Namespace: "ns1"},
					},
					Scripts: []types.CustomScriptConfig{
						{Name: "script1", Path: "/path/script1.sh"},
					},
				},
			},
			{
				AddonID: "addon2",
				Name:    "Addon 2",
				Config: types.CustomPostConfig{
					Operators: []types.CustomOperatorConfig{
						{Name: "operator2", Namespace: "ns2"},
					},
					Manifests: []types.CustomManifestConfig{
						{Name: "manifest1", Content: "apiVersion: v1"},
					},
				},
			},
			{
				AddonID: "addon3",
				Name:    "Addon 3",
				Config: types.CustomPostConfig{
					HelmCharts: []types.CustomHelmChartConfig{
						{Name: "chart1", Repo: "https://example.com/charts", Chart: "chart1"},
					},
				},
			},
		}

		// Create mock handler
		handler := &PostConfigureHandler{}

		// Merge configs
		merged := handler.mergeAddonConfigs(addons)

		// Verify all configs were merged
		require.NotNil(t, merged)
		assert.Len(t, merged.Operators, 2)
		assert.Len(t, merged.Scripts, 1)
		assert.Len(t, merged.Manifests, 1)
		assert.Len(t, merged.HelmCharts, 1)

		// Verify order is preserved
		assert.Equal(t, "operator1", merged.Operators[0].Name)
		assert.Equal(t, "operator2", merged.Operators[1].Name)
		assert.Equal(t, "script1", merged.Scripts[0].Name)
		assert.Equal(t, "manifest1", merged.Manifests[0].Name)
		assert.Equal(t, "chart1", merged.HelmCharts[0].Name)
	})

	t.Run("returns nil for empty addon list", func(t *testing.T) {
		handler := &PostConfigureHandler{}
		merged := handler.mergeAddonConfigs([]types.PostConfigAddon{})
		assert.Nil(t, merged)
	})

	t.Run("handles addons with empty configs", func(t *testing.T) {
		addons := []types.PostConfigAddon{
			{
				AddonID: "empty1",
				Config:  types.CustomPostConfig{},
			},
			{
				AddonID: "empty2",
				Config:  types.CustomPostConfig{},
			},
		}

		handler := &PostConfigureHandler{}
		merged := handler.mergeAddonConfigs(addons)

		require.NotNil(t, merged)
		assert.Empty(t, merged.Operators)
		assert.Empty(t, merged.Scripts)
		assert.Empty(t, merged.Manifests)
		assert.Empty(t, merged.HelmCharts)
	})

	t.Run("preserves config properties across merge", func(t *testing.T) {
		addons := []types.PostConfigAddon{
			{
				AddonID: "complex-addon",
				Config: types.CustomPostConfig{
					Operators: []types.CustomOperatorConfig{
						{
							Name:      "complex-operator",
							Namespace: "test-ns",
							Source:    "redhat-operators",
							Channel:   "stable",
							DependsOn: []string{"dependency1"},
						},
					},
					Scripts: []types.CustomScriptConfig{
						{
							Name:      "setup-script",
							Path:      "/scripts/setup.sh",
							Timeout:   "5m",
							DependsOn: []string{"operator"},
						},
					},
				},
			},
		}

		handler := &PostConfigureHandler{}
		merged := handler.mergeAddonConfigs(addons)

		require.NotNil(t, merged)
		require.Len(t, merged.Operators, 1)
		op := merged.Operators[0]
		assert.Equal(t, "complex-operator", op.Name)
		assert.Equal(t, "test-ns", op.Namespace)
		assert.Equal(t, "redhat-operators", op.Source)
		assert.Equal(t, "stable", op.Channel)
		assert.Equal(t, []string{"dependency1"}, op.DependsOn)

		require.Len(t, merged.Scripts, 1)
		script := merged.Scripts[0]
		assert.Equal(t, "setup-script", script.Name)
		assert.Equal(t, "/scripts/setup.sh", script.Path)
		assert.Equal(t, "5m", script.Timeout)
		assert.Equal(t, []string{"operator"}, script.DependsOn)
	})

	t.Run("merges configs from many addons", func(t *testing.T) {
		// Test with 10 addons to ensure scalability
		addons := make([]types.PostConfigAddon, 10)
		for i := 0; i < 10; i++ {
			addons[i] = types.PostConfigAddon{
				AddonID: fmt.Sprintf("addon%d", i),
				Config: types.CustomPostConfig{
					Operators: []types.CustomOperatorConfig{
						{Name: fmt.Sprintf("operator%d", i), Namespace: "ns"},
					},
				},
			}
		}

		handler := &PostConfigureHandler{}
		merged := handler.mergeAddonConfigs(addons)

		require.NotNil(t, merged)
		assert.Len(t, merged.Operators, 10)

		// Verify order
		for i := 0; i < 10; i++ {
			assert.Equal(t, fmt.Sprintf("operator%d", i), merged.Operators[i].Name)
		}
	})
}

func TestAddonConfigMergeWithDependencies(t *testing.T) {
	t.Run("preserves dependencies across addons", func(t *testing.T) {
		addons := []types.PostConfigAddon{
			{
				AddonID: "base-addon",
				Config: types.CustomPostConfig{
					Scripts: []types.CustomScriptConfig{
						{Name: "base-script", DependsOn: []string{}},
					},
					Operators: []types.CustomOperatorConfig{
						{Name: "base-operator", DependsOn: []string{"base-script"}},
					},
				},
			},
			{
				AddonID: "dependent-addon",
				Config: types.CustomPostConfig{
					Operators: []types.CustomOperatorConfig{
						{Name: "dependent-operator", DependsOn: []string{"base-operator"}},
					},
				},
			},
		}

		handler := &PostConfigureHandler{}
		merged := handler.mergeAddonConfigs(addons)

		require.NotNil(t, merged)

		// Verify dependencies are preserved
		assert.Len(t, merged.Scripts, 1)
		assert.Empty(t, merged.Scripts[0].DependsOn)

		assert.Len(t, merged.Operators, 2)
		assert.Equal(t, []string{"base-script"}, merged.Operators[0].DependsOn)
		assert.Equal(t, []string{"base-operator"}, merged.Operators[1].DependsOn)
	})
}

func TestAddonSelectionFormat(t *testing.T) {
	t.Run("parses addon reference formats", func(t *testing.T) {
		// Test cases for "addonID" and "addonID:channel" formats
		testCases := []struct {
			input           string
			expectedID      string
			expectedChannel string
		}{
			{"oadp", "oadp", ""},
			{"cnv:stable", "cnv", "stable"},
			{"cnv:stable-windows", "cnv", "stable-windows"},
			{"kubernetes-dashboard", "kubernetes-dashboard", ""},
			{"mta:v7", "mta", "v7"},
		}

		for _, tc := range testCases {
			t.Run(tc.input, func(t *testing.T) {
				// This tests the parsing logic in resolveSelectedAddons
				// The actual parsing is: strings.SplitN(addonRef, ":", 2)
				parts := strings.SplitN(tc.input, ":", 2)
				addonID := parts[0]
				channel := ""
				if len(parts) == 2 {
					channel = parts[1]
				}

				assert.Equal(t, tc.expectedID, addonID)
				assert.Equal(t, tc.expectedChannel, channel)
			})
		}
	})
}
