package profile_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tsanders-rh/ocpctl/internal/policy"
	"github.com/tsanders-rh/ocpctl/internal/profile"
	"gopkg.in/yaml.v3"
)

func TestRenderer_RenderInstallConfig(t *testing.T) {
	loader := profile.NewLoader("definitions")
	registry, err := profile.NewRegistry(loader)
	require.NoError(t, err)

	renderer := profile.NewRenderer(registry)

	t.Run("renders AWS minimal test config", func(t *testing.T) {
		req := &policy.CreateClusterRequest{
			Name:       "test-cluster-01",
			Platform:   "aws",
			Version:    "4.20.3",
			Profile:    "aws-minimal-test",
			Region:     "us-east-1",
			BaseDomain: "labs.example.com",
			Owner:      "test-user",
			Team:       "platform-team",
			CostCenter: "engineering",
			TTLHours:   24,
		}

		pullSecret := `{"auths":{"example.com":{"auth":"secret"}}}`
		sshKey := "ssh-ed25519 AAAAC3... user@example.com"
		req.SSHPublicKey = &sshKey

		tags := map[string]string{
			"Environment": "test",
			"Team":        "platform-team",
		}

		config, err := renderer.RenderInstallConfig(req, pullSecret, tags)
		require.NoError(t, err)
		require.NotEmpty(t, config)

		// Parse YAML to verify structure
		var installConfig map[string]interface{}
		err = yaml.Unmarshal(config, &installConfig)
		require.NoError(t, err)

		// Verify basic structure
		assert.Equal(t, "v1", installConfig["apiVersion"])
		assert.Equal(t, "labs.example.com", installConfig["baseDomain"])

		metadata := installConfig["metadata"].(map[string]interface{})
		assert.Equal(t, "test-cluster-01", metadata["name"])

		// Verify platform
		platform := installConfig["platform"].(map[string]interface{})
		aws := platform["aws"].(map[string]interface{})
		assert.Equal(t, "us-east-1", aws["region"])

		// Verify compute
		controlPlane := installConfig["controlPlane"].(map[string]interface{})
		assert.Equal(t, 3, controlPlane["replicas"])

		compute := installConfig["compute"].([]interface{})
		workers := compute[0].(map[string]interface{})
		assert.Equal(t, 0, workers["replicas"]) // Minimal test has 0 workers

		// Verify networking
		networking := installConfig["networking"].(map[string]interface{})
		assert.Equal(t, "OVNKubernetes", networking["networkType"])

		// Verify pull secret
		assert.Equal(t, pullSecret, installConfig["pullSecret"])
		assert.Equal(t, sshKey, installConfig["sshKey"])
	})

	t.Run("renders AWS standard config", func(t *testing.T) {
		req := &policy.CreateClusterRequest{
			Name:       "prod-cluster-01",
			Platform:   "aws",
			Version:    "4.20.3",
			Profile:    "aws-standard",
			Region:     "us-west-2",
			BaseDomain: "staging.example.com",
			Owner:      "prod-team",
			Team:       "production",
			CostCenter: "ops",
			TTLHours:   72,
		}

		pullSecret := `{"auths":{}}`
		tags := map[string]string{}

		config, err := renderer.RenderInstallConfig(req, pullSecret, tags)
		require.NoError(t, err)
		require.NotEmpty(t, config)

		// Parse YAML
		var installConfig map[string]interface{}
		err = yaml.Unmarshal(config, &installConfig)
		require.NoError(t, err)

		// Verify workers
		compute := installConfig["compute"].([]interface{})
		workers := compute[0].(map[string]interface{})
		assert.Equal(t, 3, workers["replicas"]) // Standard has 3 workers

		// Verify control plane not schedulable
		controlPlane := installConfig["controlPlane"].(map[string]interface{})
		assert.Equal(t, 3, controlPlane["replicas"])
	})

	t.Run("includes root volume configuration", func(t *testing.T) {
		req := &policy.CreateClusterRequest{
			Name:       "test-cluster-01",
			Platform:   "aws",
			Version:    "4.20.3",
			Profile:    "aws-minimal-test",
			Region:     "us-east-1",
			BaseDomain: "labs.example.com",
			Owner:      "test-user",
			Team:       "platform-team",
			CostCenter: "engineering",
			TTLHours:   24,
		}

		pullSecret := `{"auths":{}}`
		tags := map[string]string{}

		config, err := renderer.RenderInstallConfig(req, pullSecret, tags)
		require.NoError(t, err)

		configStr := string(config)
		assert.Contains(t, configStr, "rootVolume")
		assert.Contains(t, configStr, "type: gp3")
		assert.Contains(t, configStr, "size: 120")
		assert.Contains(t, configStr, "iops: 3000")
	})

	t.Run("handles missing SSH key", func(t *testing.T) {
		req := &policy.CreateClusterRequest{
			Name:       "test-cluster-01",
			Platform:   "aws",
			Version:    "4.20.3",
			Profile:    "aws-minimal-test",
			Region:     "us-east-1",
			BaseDomain: "labs.example.com",
			Owner:      "test-user",
			Team:       "platform-team",
			CostCenter: "engineering",
			TTLHours:   24,
			SSHPublicKey: nil, // No SSH key
		}

		pullSecret := `{"auths":{}}`
		tags := map[string]string{}

		config, err := renderer.RenderInstallConfig(req, pullSecret, tags)
		require.NoError(t, err)

		configStr := string(config)
		// Should not contain sshKey field
		assert.NotContains(t, configStr, "sshKey")
	})

	t.Run("returns error for invalid profile", func(t *testing.T) {
		req := &policy.CreateClusterRequest{
			Name:       "test-cluster-01",
			Platform:   "aws",
			Profile:    "non-existent",
			Region:     "us-east-1",
			BaseDomain: "labs.example.com",
		}

		pullSecret := `{"auths":{}}`
		tags := map[string]string{}

		_, err := renderer.RenderInstallConfig(req, pullSecret, tags)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "profile")
	})

	t.Run("renders valid YAML", func(t *testing.T) {
		req := &policy.CreateClusterRequest{
			Name:       "yaml-test",
			Platform:   "aws",
			Version:    "4.20.3",
			Profile:    "aws-minimal-test",
			Region:     "us-east-1",
			BaseDomain: "labs.example.com",
			Owner:      "test",
			Team:       "test",
			CostCenter: "test",
			TTLHours:   24,
		}

		pullSecret := `{"auths":{}}`
		tags := map[string]string{}

		config, err := renderer.RenderInstallConfig(req, pullSecret, tags)
		require.NoError(t, err)

		// Should be parseable YAML
		var parsed map[string]interface{}
		err = yaml.Unmarshal(config, &parsed)
		assert.NoError(t, err, "generated config should be valid YAML")

		// No tabs (YAML doesn't like tabs)
		assert.NotContains(t, string(config), "\t")

		// Should have proper indentation
		lines := strings.Split(string(config), "\n")
		for _, line := range lines {
			if len(line) > 0 && line[0] == ' ' {
				// Indentation should be multiples of 2
				spaces := 0
				for _, ch := range line {
					if ch == ' ' {
						spaces++
					} else {
						break
					}
				}
				assert.Equal(t, 0, spaces%2, "line should have even indentation: %s", line)
			}
		}
	})
}
