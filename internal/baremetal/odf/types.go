// Package odf stands up OpenShift Data Foundation in external mode backed by a
// single-VM Ceph cluster on the bare-metal host: it defines a CentOS-Stream ceph
// VM (extra disks -> OSDs) on the host's libvirt network, bootstraps ceph over the
// host->VM tunnel, exports the external-cluster connection details, installs the
// ODF operator, imports the details, and creates an external-mode StorageCluster.
// It is specific to the emulated bare-metal lab (the ceph VM only exists there).
package odf

import (
	"context"
	"io"
	"os"

	"github.com/tsanders-rh/ocpctl/internal/baremetal/agent"
)

const (
	defaultNamespace   = "openshift-storage"
	defaultSSHUser     = "cloud-user" // CentOS-Stream cloud image default user
	defaultOSVariant   = "centos-stream9"
	defaultCephRelease = "squid"
	defaultRBDPool     = "ocs-storagepool"
	defaultExporterURL = "https://raw.githubusercontent.com/rook/rook/master/deploy/examples/create-external-cluster-resources.py"
	defaultCloudImage  = "https://cloud.centos.org/centos/9-stream/x86_64/images/CentOS-Stream-GenericCloud-9-latest.x86_64.qcow2"

	// ocCmd runs oc on the host against the installer kubeconfig (root-owned;
	// executor.Run is sudo bash).
	remoteImages = "/var/lib/libvirt/images"
)

// ocCmd runs oc on the host as root (sudo) so RunCapture (login user) can read
// the root-owned installer kubeconfig, not just Run (sudo bash).
var ocCmd = "sudo " + agent.RemoteOC + " --kubeconfig=" + agent.RemoteKubeconfig

// executor is the piece-2 host client surface odf uses: host-side shell + oc
// (Run/RunCapture/Upload) and the tunneled ceph-VM hop (RunNode/RunNodeCapture).
type executor interface {
	Run(ctx context.Context, script string) error
	RunCapture(ctx context.Context, cmd string) (string, error)
	Upload(ctx context.Context, content io.Reader, remotePath string, mode os.FileMode) error
	RunNode(ctx context.Context, nodeIP, user, script string) error
	RunNodeCapture(ctx context.Context, nodeIP, user, cmd string) (string, error)
}

// Spec carries everything the ODF-external flow needs, built by the caller
// (lifecycle) from the profile, the substrate, and the host topology.
type Spec struct {
	ClusterName string
	BaseDomain  string
	Namespace   string // openshift-storage
	ODFChannel  string // desired channel (stable-<minor>); resolved against the catalog

	// ceph VM
	NodeName      string // <cluster>-ceph-0 (libvirt domain)
	Host          string // ceph-0 (guest hostname)
	IP            string // <net>.10
	MAC           string // 52:54:00:6a:03:00
	LibvirtNet    string // rhwa
	NetGateway    string
	SSHUser       string // cloud-user
	SSHPubKey     string // ephemeral substrate public key, authorized on the VM
	CloudImageURL string
	OSVariant     string
	VCPU          int
	RAMGB         int
	RootDiskGB    int
	OSDCount      int
	OSDDiskGB     int

	// ceph pool
	Release      string // squid
	Image        string // optional quay.io/ceph/ceph:<tag>
	RBDPool      string // ocs-storagepool
	PoolReplica  int
	PoolUsableGB int
	ExporterURL  string
}

func (s Spec) ns() string {
	if s.Namespace != "" {
		return s.Namespace
	}
	return defaultNamespace
}

func (s Spec) sshUser() string {
	if s.SSHUser != "" {
		return s.SSHUser
	}
	return defaultSSHUser
}

func (s Spec) rbdPool() string {
	if s.RBDPool != "" {
		return s.RBDPool
	}
	return defaultRBDPool
}

func (s Spec) exporterURL() string {
	if s.ExporterURL != "" {
		return s.ExporterURL
	}
	return defaultExporterURL
}

func (s Spec) release() string {
	if s.Release != "" {
		return s.Release
	}
	return defaultCephRelease
}

// image returns the ceph container image the cluster runs, derived from the
// release so it matches the release-pinned cephadm (a mismatched image yields a
// mgr-dump ODF's rook cannot parse). An explicit Image wins; an unknown release
// yields "" (cephadm picks its default). Ports rhwa-lab's CEPH_IMAGE derivation.
func (s Spec) image() string {
	if s.Image != "" {
		return s.Image
	}
	tag := map[string]string{
		"pacific": "v16", "quincy": "v17", "reef": "v18", "squid": "v19", "tentacle": "v20",
	}[s.release()]
	if tag == "" {
		return ""
	}
	return "quay.io/ceph/ceph:" + tag
}
