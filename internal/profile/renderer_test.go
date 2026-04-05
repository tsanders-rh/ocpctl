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

	t.Run("includes subnets when specified in profile", func(t *testing.T) {
		req := &policy.CreateClusterRequest{
			Name:       "test-shared-vpc",
			Platform:   "aws",
			Version:    "4.20.3",
			Profile:    "aws-sno-shared",
			Region:     "us-east-1",
			BaseDomain: "mg.dog8code.com",
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
		// Should contain subnets array
		assert.Contains(t, configStr, "subnets:")
		assert.Contains(t, configStr, "subnet-0656a5d299c74b0eb")
		assert.Contains(t, configStr, "subnet-04b32b2bd4012eb32")

		// Verify YAML structure
		var installConfig map[string]interface{}
		err = yaml.Unmarshal(config, &installConfig)
		require.NoError(t, err)

		platform := installConfig["platform"].(map[string]interface{})
		aws := platform["aws"].(map[string]interface{})
		subnets := aws["subnets"].([]interface{})
		assert.Len(t, subnets, 6, "should have 6 subnets configured")
	})

	t.Run("omits subnets when not specified in profile", func(t *testing.T) {
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
		// Should NOT contain subnets array
		assert.NotContains(t, configStr, "subnets:")

		// Verify YAML structure
		var installConfig map[string]interface{}
		err = yaml.Unmarshal(config, &installConfig)
		require.NoError(t, err)

		platform := installConfig["platform"].(map[string]interface{})
		aws := platform["aws"].(map[string]interface{})
		_, hasSubnets := aws["subnets"]
		assert.False(t, hasSubnets, "should not have subnets field")
	})

	t.Run("includes publish field set to External for public clusters", func(t *testing.T) {
		req := &policy.CreateClusterRequest{
			Name:       "test-public-cluster",
			Platform:   "aws",
			Version:    "4.20.3",
			Profile:    "aws-sno-test", // Non-private cluster
			Region:     "us-east-1",
			BaseDomain: "mg.dog8code.com",
			Owner:      "test-user",
			Team:       "platform-team",
			CostCenter: "engineering",
			TTLHours:   24,
		}

		pullSecret := `{"auths":{}}`
		tags := map[string]string{}

		config, err := renderer.RenderInstallConfig(req, pullSecret, tags)
		require.NoError(t, err)

		// Verify YAML structure
		var installConfig map[string]interface{}
		err = yaml.Unmarshal(config, &installConfig)
		require.NoError(t, err)

		// Verify publish field is set to External
		publish, ok := installConfig["publish"]
		assert.True(t, ok, "install-config must have publish field")
		assert.Equal(t, "External", publish, "public clusters should have publish: External")
	})


	// Test all AWS profiles to ensure they all have publish field
	t.Run("all AWS profiles include publish field", func(t *testing.T) {
		// Get all profiles
		profiles := registry.List()

		pullSecret := `{"auths":{}}`
		tags := map[string]string{}

		for _, prof := range profiles {
			// Only test AWS OpenShift profiles (not EKS/IKS)
			if prof.Platform != "aws" {
				continue
			}
			// Skip EKS profiles - they use a different template
			if prof.ClusterType == "eks" {
				continue
			}

			t.Run(prof.Name, func(t *testing.T) {
				// Get default version - should be OpenShift versions
				if prof.OpenshiftVersions == nil {
					t.Skip("Profile has no OpenShift version configuration")
					return
				}
				defaultVersion := prof.OpenshiftVersions.Default

				req := &policy.CreateClusterRequest{
					Name:       "test-" + prof.Name,
					Platform:   string(prof.Platform),
					Version:    defaultVersion,
					Profile:    prof.Name,
					Region:     prof.Regions.Default,
					BaseDomain: "test.example.com",
					Owner:      "test-user",
					Team:       "test-team",
					CostCenter: "test-cost-center",
					TTLHours:   24,
				}

				config, err := renderer.RenderInstallConfig(req, pullSecret, tags)
				require.NoError(t, err, "profile %s should render successfully", prof.Name)

				// Parse and verify
				var installConfig map[string]interface{}
				err = yaml.Unmarshal(config, &installConfig)
				require.NoError(t, err, "profile %s should produce valid YAML", prof.Name)

				// Verify publish field exists
				publish, ok := installConfig["publish"]
				assert.True(t, ok, "profile %s install-config must have publish field", prof.Name)

				// Verify publish value is valid
				publishStr, ok := publish.(string)
				assert.True(t, ok, "publish field should be a string")
				assert.Contains(t, []string{"External", "Internal"}, publishStr,
					"profile %s publish field must be External or Internal, got: %v", prof.Name, publish)

				// Verify it matches the privateCluster setting
				expectedPublish := "External"
				if prof.Features.PrivateCluster {
					expectedPublish = "Internal"
				}
				assert.Equal(t, expectedPublish, publishStr,
					"profile %s with privateCluster=%v should have publish=%s",
					prof.Name, prof.Features.PrivateCluster, expectedPublish)
			})
		}
	})
}
