package profile_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tsanders-rh/ocpctl/internal/policy"
	"github.com/tsanders-rh/ocpctl/internal/profile"
	"gopkg.in/yaml.v3"
)

func TestRenderer_RenderInstallConfig_BareMetal(t *testing.T) {
	loader := profile.NewLoader("definitions")
	registry, err := profile.NewRegistry(loader)
	require.NoError(t, err)

	renderer := profile.NewRenderer(registry)

	req := &policy.CreateClusterRequest{
		Name:       "rhwa-lab",
		Platform:   "baremetal",
		Version:    "4.22",
		Profile:    "baremetal-rhwa-lab",
		Region:     "us-east-1",
		BaseDomain: "migration.redhat.com",
		Owner:      "test-user",
		Team:       "platform-team",
		CostCenter: "engineering",
		TTLHours:   24,
	}

	pullSecret := `{"auths":{"example.com":{"auth":"secret"}}}`
	sshKey := "ssh-ed25519 AAAAC3... user@example.com"
	req.SSHPublicKey = &sshKey

	tags := map[string]string{"Environment": "lab"}

	config, err := renderer.RenderInstallConfig(req, pullSecret, tags)
	require.NoError(t, err)
	require.NotEmpty(t, config)

	var installConfig map[string]interface{}
	require.NoError(t, yaml.Unmarshal(config, &installConfig))

	assert.Equal(t, "v1", installConfig["apiVersion"])
	assert.Equal(t, "migration.redhat.com", installConfig["baseDomain"])

	metadata := installConfig["metadata"].(map[string]interface{})
	assert.Equal(t, "rhwa-lab", metadata["name"])

	// Control plane is installed; workers join post-install via a MachineSet.
	// The profile's worker replicas (3, the post-install scale target) is
	// decoupled from the install-config, which must always request 0.
	controlPlane := installConfig["controlPlane"].(map[string]interface{})
	assert.Equal(t, 3, controlPlane["replicas"])
	compute := installConfig["compute"].([]interface{})
	workers := compute[0].(map[string]interface{})
	assert.Equal(t, 0, workers["replicas"])

	// baremetal platform carries the API/ingress VIPs from the profile.
	platform := installConfig["platform"].(map[string]interface{})
	baremetal := platform["baremetal"].(map[string]interface{})
	apiVIPs := baremetal["apiVIPs"].([]interface{})
	ingressVIPs := baremetal["ingressVIPs"].([]interface{})
	assert.Equal(t, []interface{}{"192.168.126.5"}, apiVIPs)
	assert.Equal(t, []interface{}{"192.168.126.6"}, ingressVIPs)

	networking := installConfig["networking"].(map[string]interface{})
	assert.Equal(t, "OVNKubernetes", networking["networkType"])
	machineNetwork := networking["machineNetwork"].([]interface{})
	assert.Equal(t, "192.168.126.0/24", machineNetwork[0].(map[string]interface{})["cidr"])

	assert.Equal(t, pullSecret, installConfig["pullSecret"])
	assert.Equal(t, sshKey, installConfig["sshKey"])

	// baremetal has no cloud publish strategy.
	_, hasPublish := installConfig["publish"]
	assert.False(t, hasPublish, "baremetal install-config should not set publish")
}
