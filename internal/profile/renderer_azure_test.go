package profile_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tsanders-rh/ocpctl/internal/profile"
	"github.com/tsanders-rh/ocpctl/pkg/types"
	"gopkg.in/yaml.v3"
)

// TestRenderer_AzureZonePinning verifies the capacity pre-flight overrides
// (region, zones, SKUs) flow into the rendered install-config.
func TestRenderer_AzureZonePinning(t *testing.T) {
	loader := profile.NewLoader("definitions")
	registry, err := profile.NewRegistry(loader)
	require.NoError(t, err)
	renderer := profile.NewRenderer(registry)

	pullSecret := `{"auths":{"example.com":{"auth":"secret"}}}`

	baseReq := func() *types.CreateClusterRequest {
		return &types.CreateClusterRequest{
			Name:       "az-zone-test",
			Platform:   "azure",
			Version:    "4.22.0",
			Profile:    "azure-standard",
			Region:     "eastus",
			BaseDomain: "azure.mg.dog8code.com",
			Owner:      "test-user",
			TTLHours:   24,
		}
	}

	t.Run("no overrides omits zones and uses profile SKUs", func(t *testing.T) {
		cfg, err := renderer.RenderInstallConfig(baseReq(), pullSecret, nil)
		require.NoError(t, err)

		var ic map[string]interface{}
		require.NoError(t, yaml.Unmarshal(cfg, &ic))

		cp := ic["controlPlane"].(map[string]interface{})
		cpAzure := cp["platform"].(map[string]interface{})["azure"].(map[string]interface{})
		assert.Equal(t, "Standard_D8s_v5", cpAzure["type"])
		assert.Nil(t, cpAzure["zones"], "no zones should be emitted without overrides")
	})

	t.Run("overrides pin region, zones, and SKUs", func(t *testing.T) {
		req := baseReq()
		req.Region = "eastus2"
		req.AzureZones = []string{"1", "3"}
		req.AzureControlPlaneType = "Standard_D8s_v5"
		req.AzureWorkerType = "Standard_D4s_v5"

		cfg, err := renderer.RenderInstallConfig(req, pullSecret, nil)
		require.NoError(t, err)

		var ic map[string]interface{}
		require.NoError(t, yaml.Unmarshal(cfg, &ic))

		azure := ic["platform"].(map[string]interface{})["azure"].(map[string]interface{})
		assert.Equal(t, "eastus2", azure["region"])

		cpAzure := ic["controlPlane"].(map[string]interface{})["platform"].(map[string]interface{})["azure"].(map[string]interface{})
		assert.Equal(t, "Standard_D8s_v5", cpAzure["type"])
		assert.Equal(t, []interface{}{"1", "3"}, cpAzure["zones"])

		compute := ic["compute"].([]interface{})[0].(map[string]interface{})
		workerAzure := compute["platform"].(map[string]interface{})["azure"].(map[string]interface{})
		assert.Equal(t, "Standard_D4s_v5", workerAzure["type"])
		assert.Equal(t, []interface{}{"1", "3"}, workerAzure["zones"])
	})
}
