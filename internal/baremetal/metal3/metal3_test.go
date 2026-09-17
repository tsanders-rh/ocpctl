package metal3

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tsanders-rh/ocpctl/internal/baremetal/host"
)

func testSpec() Spec {
	return Spec{
		NetGateway: "192.168.126.1",
		SushyPort:  8000,
		BMCUser:    "sushy",
		BMCPass:    "s3cr3t",
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

type fakeRunner struct {
	runs     []string
	captures []string
	capture  func(cmd string) (string, error)
}

func (f *fakeRunner) Run(_ context.Context, script string) error {
	f.runs = append(f.runs, script)
	return nil
}

func (f *fakeRunner) RunCapture(_ context.Context, cmd string) (string, error) {
	f.captures = append(f.captures, cmd)
	if f.capture != nil {
		return f.capture(cmd)
	}
	return "", nil
}

func (f *fakeRunner) ranContaining(sub string) int {
	n := 0
	for _, s := range f.runs {
		if strings.Contains(s, sub) {
			n++
		}
	}
	return n
}

func TestRenderMasterBMH(t *testing.T) {
	s := testSpec()
	out, err := renderMasterBMH(s, s.Nodes[0])
	require.NoError(t, err)
	assert.Contains(t, out, "externallyProvisioned: true")
	assert.Contains(t, out, "redfish-virtualmedia://192.168.126.1:8000/redfish/v1/Systems/uuid-m0")
	assert.Contains(t, out, "master-0-bmc-secret")
	assert.NotContains(t, out, "bootMACAddress")
	assert.NotContains(t, out, "rootDeviceHints")
}

func TestRenderWorkerBMH(t *testing.T) {
	s := testSpec()
	out, err := renderWorkerBMH(s, s.Nodes[1])
	require.NoError(t, err)
	assert.Contains(t, out, "bootMACAddress: 52:54:00:6a:02:00")
	assert.Contains(t, out, "deviceName: /dev/vda")
	assert.Contains(t, out, "redfish-virtualmedia://192.168.126.1:8000/redfish/v1/Systems/uuid-w0")
	assert.NotContains(t, out, "externallyProvisioned")
}

func TestConfigureBMH_PerNodeMasterWorkerAndSkip(t *testing.T) {
	spec := testSpec()
	delete(spec.UUIDs, "c-worker-1") // spare skipped for want of a UUID

	f := &fakeRunner{}
	require.NoError(t, ConfigureBMH(context.Background(), f, spec))

	assert.Len(t, f.runs, 2)
	assert.Equal(t, 1, f.ranContaining("externallyProvisioned: true"))
	assert.Equal(t, 1, f.ranContaining("bootMACAddress"))
	assert.Equal(t, 0, f.ranContaining("worker-1"))
}

func TestProvisionWorkers_ZeroIsNoop(t *testing.T) {
	spec := testSpec()
	spec.WorkerCount = 0
	f := &fakeRunner{}
	require.NoError(t, provisionWorkers(context.Background(), f, spec, func(time.Duration) {}))
	assert.Empty(t, f.runs)
}

func TestProvisionWorkers_ScalesWaitsUnschedules(t *testing.T) {
	spec := testSpec()
	spec.WorkerCount = 2
	f := &fakeRunner{capture: func(cmd string) (string, error) {
		switch {
		case strings.Contains(cmd, "get machineset"):
			return "cluster-abc-worker-0", nil
		case strings.Contains(cmd, "get nodes"):
			return "2", nil
		}
		return "", nil
	}}
	require.NoError(t, provisionWorkers(context.Background(), f, spec, func(time.Duration) {}))
	assert.Equal(t, 1, f.ranContaining("scale machineset cluster-abc-worker-0 --replicas=2"))
	assert.Equal(t, 1, f.ranContaining(`"mastersSchedulable":false`))
	assert.Equal(t, 1, f.ranContaining("stranded on the control plane")) // daemonset eviction
}

func TestProvisionWorkers_NoMachineSetErrors(t *testing.T) {
	spec := testSpec()
	spec.WorkerCount = 1
	f := &fakeRunner{capture: func(string) (string, error) { return "", nil }}
	require.Error(t, provisionWorkers(context.Background(), f, spec, func(time.Duration) {}))
}
