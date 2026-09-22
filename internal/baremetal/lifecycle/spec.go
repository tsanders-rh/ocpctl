package lifecycle

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/tsanders-rh/ocpctl/internal/baremetal/agent"
	"github.com/tsanders-rh/ocpctl/internal/baremetal/host"
	"github.com/tsanders-rh/ocpctl/internal/baremetal/metal3"
	"github.com/tsanders-rh/ocpctl/internal/baremetal/odf"
	"github.com/tsanders-rh/ocpctl/internal/baremetal/rhwa"
	"github.com/tsanders-rh/ocpctl/internal/baremetal/substrate"
)

const (
	libvirtNet    = "rhwa"
	sushyUsername = "sushy"

	fallbackCPVCPU = 8
	fallbackCPRAM  = 20
	fallbackWKVCPU = 4
	fallbackWKRAM  = 16

	baseHostVolumeGB = 1000 // EC2 root disk before ODF OSD headroom

	// ODF/Ceph defaults (match rhwa-lab); used when the profile leaves them zero.
	defaultCephOSDCount   = 3
	defaultCephPoolUsable = 200
	defaultCephReplica    = 3
	defaultCephVCPU       = 4
	defaultCephRAMGB      = 16
	defaultCephRootGB     = 40
	cephFullRatioPct      = 118 // per-OSD headroom for Ceph full-ratio + BlueStore overhead
)

var (
	nodeSizeRE = regexp.MustCompile(`^vm-(\d+)vcpu-(\d+)gb$`)
	minorRE    = regexp.MustCompile(`^\d+\.\d+$`)
)

// parseNodeSize reads vCPU/RAM from a "vm-<N>vcpu-<M>gb" instance-type name,
// falling back to the given defaults when the name does not match.
func parseNodeSize(instanceType string, fbVCPU, fbRAM int) (int, int) {
	m := nodeSizeRE.FindStringSubmatch(instanceType)
	if m == nil {
		return fbVCPU, fbRAM
	}
	v, verr := strconv.Atoi(m[1])
	r, rerr := strconv.Atoi(m[2])
	if verr != nil || rerr != nil {
		return fbVCPU, fbRAM
	}
	return v, r
}

// generateSushyCreds returns a fixed username and a random password for the
// per-cluster sushy-tools BMC. Not persisted (the host is terminated on destroy).
func generateSushyCreds() (user, pass string, err error) {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		return "", "", fmt.Errorf("generate sushy password: %w", err)
	}
	return sushyUsername, hex.EncodeToString(b), nil
}

// netGateway returns the .1 gateway of a machine-network CIDR.
func netGateway(cidr string) string {
	ip := cidr
	if i := strings.IndexByte(ip, '/'); i >= 0 {
		ip = ip[:i]
	}
	if i := strings.LastIndexByte(ip, '.'); i >= 0 {
		return ip[:i] + ".1"
	}
	return ip
}

func firstMasterIP(nodes []host.VM) string {
	for _, n := range nodes {
		if n.Role == "master" {
			return n.IP
		}
	}
	return ""
}

// mirrorVersion maps an ocpctl version to a mirror.openshift.com path segment: a
// bare "X.Y" becomes the moving "stable-X.Y" channel; a full "X.Y.Z" passes
// through. Ports rhwa-lab's rhwaOCPVersion.
func mirrorVersion(v string) string {
	if minorRE.MatchString(v) {
		return "stable-" + v
	}
	return v
}

func controlPlaneCount(in Input) int {
	if in.Compute != nil && in.Compute.ControlPlane != nil {
		return in.Compute.ControlPlane.Replicas
	}
	return 0
}

func workerCount(in Input) int {
	if in.Compute != nil && in.Compute.Workers != nil {
		return in.Compute.Workers.Replicas
	}
	return 0
}

func cpInstanceType(in Input) string {
	if in.Compute != nil && in.Compute.ControlPlane != nil {
		return in.Compute.ControlPlane.InstanceType
	}
	return ""
}

func wkInstanceType(in Input) string {
	if in.Compute != nil && in.Compute.Workers != nil {
		return in.Compute.Workers.InstanceType
	}
	return ""
}

func buildLaunchSpec(in Input) substrate.LaunchSpec {
	return substrate.LaunchSpec{
		ClusterID:     in.ClusterID,
		ClusterName:   in.ClusterName,
		Region:        in.Region,
		BaseDomain:    in.BaseDomain,
		InstanceType:  in.BareMetal.HostInstanceType,
		AMIOwner:      in.BareMetal.HostAMIOwner,
		FedoraRelease: in.BareMetal.FedoraRelease,
		HostVolumeGB:  hostVolumeGB(in),
		AllowCIDRs:    in.BareMetal.AllowCIDRs,
		CreatedAt:     in.CreatedAt,
	}
}

// hostVolumeGB sizes the EC2 root disk. When ODF is enabled it must also hold the
// ceph OSD qcow2s, so it is at least 1000 + osdCount*osdDiskGB (rhwa-lab's
// formula); an explicit, larger profile value is respected.
func hostVolumeGB(in Input) int {
	base := in.BareMetal.HostVolumeGB
	if base == 0 {
		base = baseHostVolumeGB
	}
	if odfEnabled(in) {
		if need := baseHostVolumeGB + odfOSDCount(in)*odfOSDDiskGB(in); need > base {
			base = need
		}
	}
	return base
}

func buildTopology(in Input) host.Topology {
	cpV, cpR := parseNodeSize(cpInstanceType(in), fallbackCPVCPU, fallbackCPRAM)
	wkV, wkR := parseNodeSize(wkInstanceType(in), fallbackWKVCPU, fallbackWKRAM)
	return host.Topology{
		ClusterName:       in.ClusterName,
		NetCIDR:           in.BareMetal.NetworkCIDR,
		ControlPlaneCount: controlPlaneCount(in),
		WorkerCount:       workerCount(in),
		SpareCount:        in.BareMetal.SpareWorkerCount,
		CPVCPU:            cpV,
		CPRAMGB:           cpR,
		WKVCPU:            wkV,
		WKRAMGB:           wkR,
	}
}

func buildHostSpec(in Input, nodes []host.VM, sushyUser, sushyPass string) host.HostSpec {
	hs := host.HostSpec{
		ClusterName: in.ClusterName,
		LibvirtNet:  libvirtNet,
		NetCIDR:     in.BareMetal.NetworkCIDR,
		NetGateway:  netGateway(in.BareMetal.NetworkCIDR),
		APIVIP:      in.BareMetal.APIVIP,
		IngressVIP:  in.BareMetal.IngressVIP,
		SushyPort:   in.BareMetal.SushyPort,
		SushyUser:   sushyUser,
		SushyPass:   sushyPass,
		NodeDiskGB:  in.BareMetal.NodeDiskGB,
		Nodes:       nodes,
	}
	// Pin the external-Ceph VM's IP in the libvirt network (rhwa-lab does this
	// when CEPH_ENABLED): a DHCP reservation is the fallback that lands the VM on
	// .10 if cloud-init's static network-config does not apply.
	if odfEnabled(in) {
		hs.CephReservation = &host.VM{
			Name: in.ClusterName + "-ceph-0",
			Host: "ceph-0",
			IP:   cephIP(in.BareMetal.NetworkCIDR),
			MAC:  cephMAC,
		}
	}
	return hs
}

func buildInstallSpec(in Input, nodes []host.VM, installConfig []byte) agent.InstallSpec {
	return agent.InstallSpec{
		ClusterName:   in.ClusterName,
		BaseDomain:    in.BaseDomain,
		OCPVersion:    mirrorVersion(in.Version),
		NetGateway:    netGateway(in.BareMetal.NetworkCIDR),
		RendezvousIP:  firstMasterIP(nodes),
		Masters:       nodes, // agent.Install filters to masters
		InstallConfig: installConfig,
	}
}

func buildMetal3Spec(in Input, nodes []host.VM, uuids map[string]string, user, pass string) metal3.Spec {
	return metal3.Spec{
		NetGateway:  netGateway(in.BareMetal.NetworkCIDR),
		SushyPort:   in.BareMetal.SushyPort,
		BMCUser:     user,
		BMCPass:     pass,
		Nodes:       nodes,
		UUIDs:       uuids,
		WorkerCount: workerCount(in),
	}
}

const cephMAC = "52:54:00:6a:03:00" // role byte 03 (masters 01, workers 02) so it can't collide

// odfEnabled reports whether the profile turns on ODF-external.
func odfEnabled(in Input) bool {
	return in.BareMetal != nil && in.BareMetal.ODF != nil && in.BareMetal.ODF.Enabled
}

func firstPositive(v, def int) int {
	if v > 0 {
		return v
	}
	return def
}

func odfOSDCount(in Input) int {
	return firstPositive(in.BareMetal.ODF.CephOSDCount, defaultCephOSDCount)
}
func odfPoolUsable(in Input) int {
	return firstPositive(in.BareMetal.ODF.CephPoolUsableGB, defaultCephPoolUsable)
}
func odfReplica(in Input) int { return firstPositive(in.BareMetal.ODF.CephReplica, defaultCephReplica) }

// odfOSDDiskGB sizes each OSD's raw disk to yield the target usable capacity at
// the pool replica across the OSDs, plus headroom (rhwa-lab's formula):
// raw_per_osd = usable * replica * 118% / osdCount.
func odfOSDDiskGB(in Input) int {
	return odfPoolUsable(in) * odfReplica(in) * cephFullRatioPct / (odfOSDCount(in) * 100)
}

// cephIP places the ceph VM at .10 of the machine network.
func cephIP(cidr string) string {
	ip := cidr
	if i := strings.IndexByte(ip, '/'); i >= 0 {
		ip = ip[:i]
	}
	if i := strings.LastIndexByte(ip, '.'); i >= 0 {
		return ip[:i] + ".10"
	}
	return ip
}

// odfChannel is the desired ODF Subscription channel (resolved against the
// catalog at install time): the explicit profile channel, else stable-<minor>.
func odfChannel(in Input) string {
	if c := in.BareMetal.ODF.Channel; c != "" {
		return c
	}
	return "stable-" + minorVersion(in.Version)
}

// minorVersion returns the X.Y of an OCP version ("4.22.3" -> "4.22").
func minorVersion(v string) string {
	parts := strings.SplitN(v, ".", 3)
	if len(parts) >= 2 {
		return parts[0] + "." + parts[1]
	}
	return v
}

func buildODFSpec(in Input, sshPubKey string) odf.Spec {
	o := in.BareMetal.ODF
	return odf.Spec{
		ClusterName:   in.ClusterName,
		BaseDomain:    in.BaseDomain,
		ODFChannel:    odfChannel(in),
		NodeName:      in.ClusterName + "-ceph-0",
		Host:          "ceph-0",
		IP:            cephIP(in.BareMetal.NetworkCIDR),
		MAC:           cephMAC,
		LibvirtNet:    libvirtNet,
		NetGateway:    netGateway(in.BareMetal.NetworkCIDR),
		SSHPubKey:     sshPubKey,
		CloudImageURL: o.CloudImageURL,
		VCPU:          firstPositive(o.CephVCPU, defaultCephVCPU),
		RAMGB:         firstPositive(o.CephRAMGB, defaultCephRAMGB),
		RootDiskGB:    firstPositive(o.CephRootDiskGB, defaultCephRootGB),
		OSDCount:      odfOSDCount(in),
		OSDDiskGB:     odfOSDDiskGB(in),
		Release:       o.CephRelease,
		PoolReplica:   odfReplica(in),
		PoolUsableGB:  odfPoolUsable(in),
	}
}

func buildRHWASpec(in Input, nodes []host.VM, uuids map[string]string, user, pass string) rhwa.Spec {
	ops := make([]rhwa.Operator, 0, len(in.Operators))
	for _, o := range in.Operators {
		ops = append(ops, rhwa.Operator{Name: o.Name, Channel: o.Channel, Source: o.Source})
	}
	return rhwa.Spec{
		NetGateway: netGateway(in.BareMetal.NetworkCIDR),
		SushyPort:  in.BareMetal.SushyPort,
		SushyUser:  user,
		SushyPass:  pass,
		Operators:  ops,
		Nodes:      nodes,
		UUIDs:      uuids,
	}
}
