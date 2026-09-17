package agent

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tsanders-rh/ocpctl/internal/baremetal/host"
)

func testInstallSpec() InstallSpec {
	return InstallSpec{
		ClusterName:  "rhwa-lab",
		BaseDomain:   "example.com",
		OCPVersion:   "stable-4.22",
		NetGateway:   "192.168.126.1",
		RendezvousIP: "192.168.126.11",
		Masters: []host.VM{
			{Name: "rhwa-lab-master-0", Host: "master-0", Role: "master", IP: "192.168.126.11", MAC: "52:54:00:6a:01:00"},
			{Name: "rhwa-lab-master-1", Host: "master-1", Role: "master", IP: "192.168.126.12", MAC: "52:54:00:6a:01:01"},
			{Name: "rhwa-lab-master-2", Host: "master-2", Role: "master", IP: "192.168.126.13", MAC: "52:54:00:6a:01:02"},
		},
	}
}

func TestRenderAgentConfig(t *testing.T) {
	out, err := renderAgentConfig(testInstallSpec())
	require.NoError(t, err)

	assert.Contains(t, out, "kind: AgentConfig")
	assert.Contains(t, out, "rendezvousIP: 192.168.126.11")
	// One host block per master, with MAC -> IP -> hostname wiring.
	assert.Equal(t, 3, strings.Count(out, "- hostname: master-"))
	assert.Contains(t, out, "hostname: master-0")
	assert.Contains(t, out, "macAddress: 52:54:00:6a:01:00")
	assert.Contains(t, out, "ip: 192.168.126.11")
	assert.Contains(t, out, "next-hop-address: 192.168.126.1")
	assert.Contains(t, out, "name: enp1s0")
	// nmstate matches by mac-address (hedge for NIC-name drift).
	assert.Contains(t, out, "mac-address: 52:54:00:6a:01:02")
}

func TestMastersFiltersWorkersAndSpares(t *testing.T) {
	spec := testInstallSpec()
	spec.Masters = append(spec.Masters,
		host.VM{Name: "rhwa-lab-worker-0", Host: "worker-0", Role: "worker", IP: "192.168.126.21", MAC: "52:54:00:6a:02:00"},
		host.VM{Name: "rhwa-lab-worker-1", Host: "worker-1", Role: "worker", IP: "192.168.126.22", MAC: "52:54:00:6a:02:01", Spare: true},
	)
	out, err := renderAgentConfig(spec)
	require.NoError(t, err)
	// Workers/spares never appear in agent-config (they join via MachineSet).
	assert.NotContains(t, out, "worker-0")
	assert.NotContains(t, out, "worker-1")
	assert.Equal(t, 3, strings.Count(out, "- hostname: master-"))
}
