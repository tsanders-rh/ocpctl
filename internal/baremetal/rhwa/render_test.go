package rhwa

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tsanders-rh/ocpctl/internal/baremetal/host"
)

func testOperators() []Operator {
	return []Operator{
		{Name: "node-healthcheck-operator", Channel: "stable", Source: "redhat-operators"},
		{Name: "fence-agents-remediation", Channel: "stable", Source: "redhat-operators"},
		{Name: "self-node-remediation", Channel: "stable", Source: "redhat-operators"},
	}
}

func testSpec() Spec {
	return Spec{
		NetGateway: "192.168.126.1",
		SushyPort:  8000,
		SushyUser:  "sushy",
		SushyPass:  "s3cr3t",
		Operators:  testOperators(),
		Nodes: []host.VM{
			{Name: "c-master-0", Host: "master-0", Role: "master", MAC: "52:54:00:6a:01:00"},
			{Name: "c-worker-0", Host: "worker-0", Role: "worker", MAC: "52:54:00:6a:02:00"},
			{Name: "c-worker-1", Host: "worker-1", Role: "worker", MAC: "52:54:00:6a:02:01", Spare: true},
		},
		UUIDs: map[string]string{
			"c-master-0": "uuid-m0",
			"c-worker-0": "uuid-w0",
			"c-worker-1": "uuid-w1",
		},
	}
}

func TestRenderOperators(t *testing.T) {
	out, err := renderOperators(testSpec())
	require.NoError(t, err)
	assert.Contains(t, out, "kind: Namespace")
	assert.Contains(t, out, "name: openshift-workload-availability")
	assert.Contains(t, out, "spec: {}") // AllNamespaces OperatorGroup
	for _, op := range testOperators() {
		assert.Contains(t, out, "name: "+op.Name)
		assert.Contains(t, out, "source: "+op.Source)
	}
	assert.Contains(t, out, "channel: stable")
}

func TestRenderFencing(t *testing.T) {
	out, err := renderFencing(testSpec())
	require.NoError(t, err)
	assert.Contains(t, out, "agent: fence_redfish")
	assert.Contains(t, out, `"--ip": "192.168.126.1"`)
	assert.Contains(t, out, `"--ipport": "8000"`)
	assert.Contains(t, out, `"--username": "sushy"`)
	// systems-uri entries for cluster nodes only (spare excluded).
	assert.Contains(t, out, `"master-0": "/redfish/v1/Systems/uuid-m0"`)
	assert.Contains(t, out, `"worker-0": "/redfish/v1/Systems/uuid-w0"`)
	assert.NotContains(t, out, "worker-1")
	// NHC selects worker-role, non-control-plane nodes.
	assert.Contains(t, out, "node-role.kubernetes.io/worker")
	assert.Contains(t, out, "DoesNotExist")
	assert.Equal(t, 1, strings.Count(out, "kind: NodeHealthCheck"))
}
