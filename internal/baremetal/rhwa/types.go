// Package rhwa installs Red Hat Workload Availability (RHWA / Medik8s) — the Node
// Health Check, Fence Agents Remediation, Self Node Remediation, Node Maintenance
// and Machine Deletion Remediation operators — and configures fence_redfish
// fencing against the cluster's Redfish BMCs. The generic metal3 BareMetalHost and
// worker provisioning it builds on lives in internal/baremetal/metal3.
package rhwa

import (
	"context"

	"github.com/tsanders-rh/ocpctl/internal/baremetal/agent"
	"github.com/tsanders-rh/ocpctl/internal/baremetal/host"
)

const defaultNamespace = "openshift-workload-availability"

// ocCmd runs oc on the host as root (sudo). The installer kubeconfig is
// root-owned (0600), so oc must run as root even from RunCapture (which runs as
// the login user, not sudo bash).
var ocCmd = "sudo " + agent.RemoteOC + " --kubeconfig=" + agent.RemoteKubeconfig

// Operator is one OLM operator to subscribe to. The set (names, channels,
// sources) is supplied by the caller from the ocpctl `rhwa` addon definition — the
// single declarative source of RHWA operator versions — not hardcoded here.
type Operator struct {
	Name    string
	Channel string
	Source  string
}

// runner is the subset of *host.Client the post-install steps use.
type runner interface {
	Run(ctx context.Context, script string) error
	RunCapture(ctx context.Context, cmd string) (string, error)
}

// Spec carries the substrate-derived values the operator and fencing manifests need.
type Spec struct {
	NetGateway string // Redfish BMC host (sushy), e.g. 192.168.126.1
	SushyPort  int    // Redfish BMC port
	SushyUser  string // fence_redfish credentials
	SushyPass  string
	Namespace  string            // operator/fencing namespace
	Operators  []Operator        // from the rhwa addon definition
	Nodes      []host.VM         // masters + workers + spares
	UUIDs      map[string]string // domain name -> libvirt UUID (Redfish system id)
}

func (s Spec) ns() string {
	if s.Namespace != "" {
		return s.Namespace
	}
	return defaultNamespace
}

// clusterNodes are the installed nodes (masters + real workers), excluding spares.
func clusterNodes(nodes []host.VM) []host.VM {
	out := make([]host.VM, 0, len(nodes))
	for _, n := range nodes {
		if !n.Spare {
			out = append(out, n)
		}
	}
	return out
}
