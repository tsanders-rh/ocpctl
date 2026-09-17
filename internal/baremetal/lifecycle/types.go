// Package lifecycle wires the native bare-metal pieces (substrate, host, agent,
// metal3, rhwa) into a single Create/Destroy orchestration, and holds the pure
// profile-to-spec builders. It is the only baremetal package the worker imports.
package lifecycle

import (
	"time"

	"github.com/tsanders-rh/ocpctl/internal/profile"
	"github.com/tsanders-rh/ocpctl/pkg/types"
)

// Input is built by the worker handler from the cluster + profile + pull secret.
type Input struct {
	ClusterID   string
	ClusterName string
	Region      string
	BaseDomain  string
	Version     string // ocpctl cluster version, e.g. "4.22"
	CreatedAt   time.Time
	WorkDir     string // per-cluster work dir

	BareMetal  *profile.BareMetalConfig
	Compute    *profile.ComputeConfig
	Networking *profile.NetworkingConfig

	// Operators is the RHWA operator set from the ocpctl `rhwa` addon definition
	// (the declarative source of operator versions), applied natively during
	// create so it precedes the substrate-specific fence_redfish fencing.
	Operators []types.CustomOperatorConfig

	// RenderInstallConfig renders install-config.yaml with the given SSH public
	// key (the ephemeral substrate key, known only after Launch). Provided by the
	// caller so lifecycle stays out of the renderer/types details.
	RenderInstallConfig func(sshPubKey string) ([]byte, error)
}

// Result is what the handler persists.
type Result struct {
	Kubeconfig        []byte
	KubeadminPassword []byte
	APIURL            string
	ConsoleURL        string
	Recovered         bool
}

// DestroyInput identifies a cluster's substrate for teardown.
type DestroyInput struct {
	Region      string
	ClusterName string
	BaseDomain  string
	ZoneID      string
}
