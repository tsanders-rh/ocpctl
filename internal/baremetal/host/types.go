package host

// VM is one libvirt domain backing an OpenShift node. Spares carry Role
// "worker" but are left unconsumed (defined and BMC-addressable, never booted
// during install).
type VM struct {
	Name  string // libvirt domain name, e.g. rhwa-lab-master-0
	Host  string // hostname / DHCP reservation name, e.g. master-0
	Role  string // master | worker
	IP    string
	MAC   string
	VCPU  int
	RAMGB int
	Spare bool
}

// HostSpec is the full input to Provision, built by the caller from the
// profile and the AWS substrate output (piece 1).
type HostSpec struct {
	ClusterName string
	LibvirtNet  string
	NetCIDR     string
	NetGateway  string
	APIVIP      string
	IngressVIP  string
	SushyPort   int
	SushyUser   string
	SushyPass   string
	NodeDiskGB  int
	Nodes       []VM

	// CephReservation, when non-nil, adds a DHCP host reservation for the
	// external-Ceph VM (ODF) to the libvirt network so it lands on its pinned IP
	// even if cloud-init's static config does not apply. Mirrors rhwa-lab
	// host_libvirt_network's CEPH_ENABLED block. Only Host, IP and MAC are used;
	// the ceph VM is not an OpenShift node and never appears in Nodes.
	CephReservation *VM
}

// Result is what Provision returns: domain UUIDs (Redfish system ids) keyed by
// domain name, and the computed topology for piece 3.
type Result struct {
	UUIDs map[string]string
	Nodes []VM
}

// Topology is the input to ComputeNodes.
type Topology struct {
	ClusterName       string
	NetCIDR           string
	ControlPlaneCount int
	WorkerCount       int
	SpareCount        int
	CPVCPU            int
	CPRAMGB           int
	WKVCPU            int
	WKRAMGB           int
}
