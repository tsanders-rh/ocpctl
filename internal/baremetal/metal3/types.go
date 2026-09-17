// Package metal3 configures a running OpenShift cluster as a metal3-managed
// bare-metal cluster: it creates the per-node BareMetalHost objects (against
// emulated or physical Redfish BMCs) and provisions workers via the Machine API.
// It is workload-agnostic — reusable for any bare-metal cluster, emulated or
// physical — and knows nothing about RHWA/Medik8s (see internal/baremetal/rhwa).
package metal3

import (
	"context"
	"fmt"

	"github.com/tsanders-rh/ocpctl/internal/baremetal/agent"
	"github.com/tsanders-rh/ocpctl/internal/baremetal/host"
)

const machineAPINS = "openshift-machine-api"

// ocCmd runs oc on the host against the installer kubeconfig. runner.Run executes
// via sudo bash (root), which owns the kubeconfig.
var ocCmd = "sudo " + agent.RemoteOC + " --kubeconfig=" + agent.RemoteKubeconfig

// runner is the subset of *host.Client the post-install steps use.
type runner interface {
	Run(ctx context.Context, script string) error
	RunCapture(ctx context.Context, cmd string) (string, error)
}

// Spec carries the substrate-derived values the BareMetalHost + provisioning
// manifests need.
type Spec struct {
	NetGateway  string // Redfish BMC host (sushy), e.g. 192.168.126.1
	SushyPort   int    // Redfish BMC port
	BMCUser     string // BMC credentials (sushy)
	BMCPass     string
	Nodes       []host.VM         // masters + workers + spares
	UUIDs       map[string]string // domain name -> libvirt UUID (Redfish system id)
	WorkerCount int               // real workers to provision post-install (0 = none)
}

func bmcAddress(gw string, port int, uuid string) string {
	return fmt.Sprintf("redfish-virtualmedia://%s:%d/redfish/v1/Systems/%s", gw, port, uuid)
}
