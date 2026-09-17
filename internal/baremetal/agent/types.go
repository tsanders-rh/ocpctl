// Package agent runs the Agent-Based Installer on the provisioned host: it
// renders agent-config, builds the agent ISO once, boots the masters via their
// BMCs, waits for install completion, and fetches verified credentials.
package agent

import (
	"context"
	"io"
	"os"
	"time"

	"github.com/tsanders-rh/ocpctl/internal/baremetal/host"
)

const (
	defaultIface          = "enp1s0" // ITERATE: q35+virtio; nmstate matches by MAC as a hedge
	defaultInstallTimeout = 2 * time.Hour
	mirrorBase            = "https://mirror.openshift.com/pub/openshift-v4/x86_64/clients/ocp"

	// RemoteWorkDir is the host-side working directory for the agent install. It
	// is absolute (not ~) on purpose: Run executes via `sudo bash` (root) while
	// RunCapture runs as the login user, and whether sudo resets HOME is
	// sudoers-dependent, so ~ is ambiguous. Everything reads/writes under this
	// fixed path, with sudo on the root-owned reads.
	RemoteWorkDir = "/opt/ocpctl-agent"
	remoteBinDir  = RemoteWorkDir + "/bin"
	remoteAuthDir = RemoteWorkDir + "/work/auth"
	// RemoteKubeconfig and RemoteOC are consumed by piece-4 post-install config.
	RemoteKubeconfig = remoteAuthDir + "/kubeconfig"
	RemoteOC         = remoteBinDir + "/oc"

	// recoveryKC is the always-trusted kubeconfig baked into every master; unlike
	// work/auth/kubeconfig it survives cert-generation mismatches.
	recoveryKC = "/etc/kubernetes/static-pod-resources/kube-apiserver-certs/secrets/node-kubeconfigs/localhost-recovery.kubeconfig"
)

// InstallSpec is built by the caller (piece 4) from the profile, the piece-1
// Substrate, and the piece-2 host Provision result.
type InstallSpec struct {
	ClusterName    string
	BaseDomain     string
	OCPVersion     string    // mirror path segment: "stable-4.22" or "4.22.3"
	NetGateway     string    // 192.168.126.1
	RendezvousIP   string    // = first master IP
	Masters        []host.VM // may be the full node list; masters() filters it
	InstallConfig  []byte    // rendered install-config.yaml (existing renderer)
	InstallTimeout time.Duration
}

// Result is what piece 4 persists.
type Result struct {
	Kubeconfig        []byte
	KubeadminPassword []byte
	APIURL            string
	ConsoleURL        string
	Recovered         bool
}

// hostRunner is the subset of *host.Client the install orchestration uses.
type hostRunner interface {
	Run(ctx context.Context, script string) error
	RunCapture(ctx context.Context, cmd string) (string, error)
	Upload(ctx context.Context, content io.Reader, remotePath string, mode os.FileMode) error
}
