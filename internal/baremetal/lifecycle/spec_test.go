package lifecycle

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tsanders-rh/ocpctl/internal/baremetal/host"
	"github.com/tsanders-rh/ocpctl/internal/profile"
)

func testInput() Input {
	return Input{
		ClusterID:   "id-1",
		ClusterName: "rhwa-lab",
		Region:      "us-east-1",
		BaseDomain:  "migration.redhat.com",
		Version:     "4.22",
		CreatedAt:   time.Date(2026, 9, 9, 0, 0, 0, 0, time.UTC),
		BareMetal: &profile.BareMetalConfig{
			HostInstanceType: "m8i.12xlarge",
			HostAMIOwner:     "125523088429",
			FedoraRelease:    "44",
			NodeDiskGB:       120,
			SpareWorkerCount: 3,
			NetworkCIDR:      "192.168.126.0/24",
			APIVIP:           "192.168.126.5",
			IngressVIP:       "192.168.126.6",
			SushyPort:        8000,
			HostVolumeGB:     1000,
		},
		Compute: &profile.ComputeConfig{
			ControlPlane: &profile.ControlPlaneConfig{Replicas: 3, InstanceType: "vm-8vcpu-20gb"},
			Workers:      &profile.WorkersConfig{Replicas: 0, InstanceType: "vm-4vcpu-16gb"},
		},
	}
}

func TestParseNodeSize(t *testing.T) {
	v, r := parseNodeSize("vm-8vcpu-20gb", 1, 1)
	assert.Equal(t, 8, v)
	assert.Equal(t, 20, r)

	v, r = parseNodeSize("m8i.12xlarge", 4, 16) // not a vm-* name -> fallback
	assert.Equal(t, 4, v)
	assert.Equal(t, 16, r)
}

func TestNetGatewayAndMirrorVersion(t *testing.T) {
	assert.Equal(t, "192.168.126.1", netGateway("192.168.126.0/24"))
	assert.Equal(t, "stable-4.22", mirrorVersion("4.22"))
	assert.Equal(t, "4.22.3", mirrorVersion("4.22.3"))
}

func TestGenerateSushyCreds(t *testing.T) {
	u1, p1, err := generateSushyCreds()
	require.NoError(t, err)
	assert.Equal(t, "sushy", u1)
	assert.NotEmpty(t, p1)
	_, p2, err := generateSushyCreds()
	require.NoError(t, err)
	assert.NotEqual(t, p1, p2, "passwords must be random")
}

func TestBuildLaunchSpec(t *testing.T) {
	s := buildLaunchSpec(testInput())
	assert.Equal(t, "rhwa-lab", s.ClusterName)
	assert.Equal(t, "us-east-1", s.Region)
	assert.Equal(t, "m8i.12xlarge", s.InstanceType)
	assert.Equal(t, "125523088429", s.AMIOwner)
	assert.Equal(t, 1000, s.HostVolumeGB)
	assert.Empty(t, s.AllowCIDRs, "no profile allow-list leaves the substrate ingress defaults")
}

func TestBuildLaunchSpec_AllowCIDRs(t *testing.T) {
	in := testInput()
	in.BareMetal.AllowCIDRs = []string{"203.0.113.0/24", "198.51.100.7/32"}
	assert.Equal(t, []string{"203.0.113.0/24", "198.51.100.7/32"}, buildLaunchSpec(in).AllowCIDRs)
}

func TestBuildTopology(t *testing.T) {
	tp := buildTopology(testInput())
	assert.Equal(t, 3, tp.ControlPlaneCount)
	assert.Equal(t, 0, tp.WorkerCount)
	assert.Equal(t, 3, tp.SpareCount)
	assert.Equal(t, 8, tp.CPVCPU)
	assert.Equal(t, 20, tp.CPRAMGB)
	assert.Equal(t, 4, tp.WKVCPU)
	assert.Equal(t, 16, tp.WKRAMGB)
	assert.Equal(t, "192.168.126.0/24", tp.NetCIDR)

	// ComputeNodes turns the topology into 3 masters + 3 spares (no real workers).
	nodes := host.ComputeNodes(tp)
	masters, spares := 0, 0
	for _, n := range nodes {
		if n.Role == "master" {
			masters++
		}
		if n.Spare {
			spares++
		}
	}
	assert.Equal(t, 3, masters)
	assert.Equal(t, 3, spares)
}

func TestHostVolumeGB_ODFHeadroom(t *testing.T) {
	in := testInput()
	// ODF disabled: base default.
	assert.Equal(t, 1000, hostVolumeGB(in))

	// ODF enabled: 1000 + osdCount(3) * osdDiskGB.
	in.BareMetal.HostVolumeGB = 0
	in.BareMetal.ODF = &profile.ODFConfig{Enabled: true} // defaults: 3 OSDs, 200 GB usable, replica 3
	assert.Equal(t, 236, odfOSDDiskGB(in))               // 200*3*118/(3*100)
	assert.Equal(t, 1000+3*236, hostVolumeGB(in))        // 1708

	// A larger explicit profile value is respected.
	in.BareMetal.HostVolumeGB = 5000
	assert.Equal(t, 5000, hostVolumeGB(in))
}

func TestBuildODFSpec(t *testing.T) {
	in := testInput()
	in.BareMetal.ODF = &profile.ODFConfig{Enabled: true}
	s := buildODFSpec(in, "ssh-ed25519 AAAA ephemeral")

	assert.Equal(t, "rhwa-lab-ceph-0", s.NodeName)
	assert.Equal(t, "192.168.126.10", s.IP) // .10 of the machine network
	assert.Equal(t, "52:54:00:6a:03:00", s.MAC)
	assert.Equal(t, "192.168.126.1", s.NetGateway)
	assert.Equal(t, "stable-4.22", s.ODFChannel) // desired from version 4.22
	assert.Equal(t, "ssh-ed25519 AAAA ephemeral", s.SSHPubKey)
	assert.Equal(t, 3, s.OSDCount)
	assert.Equal(t, 236, s.OSDDiskGB)
	assert.Equal(t, 3, s.PoolReplica)
	assert.Equal(t, 4, s.VCPU) // ceph defaults
	assert.Equal(t, 16, s.RAMGB)
}

func TestBuildHostAndSpecs(t *testing.T) {
	in := testInput()
	nodes := host.ComputeNodes(buildTopology(in))

	hs := buildHostSpec(in, nodes, "sushy", "pw")
	assert.Equal(t, "rhwa", hs.LibvirtNet)
	assert.Equal(t, "192.168.126.1", hs.NetGateway)
	assert.Equal(t, "192.168.126.5", hs.APIVIP)
	assert.Equal(t, 8000, hs.SushyPort)
	assert.Equal(t, "sushy", hs.SushyUser)

	is := buildInstallSpec(in, nodes, []byte("cfg"))
	assert.Equal(t, "stable-4.22", is.OCPVersion)
	assert.Equal(t, "192.168.126.11", is.RendezvousIP) // first master
	assert.Equal(t, []byte("cfg"), is.InstallConfig)

	uuids := map[string]string{}
	m3 := buildMetal3Spec(in, nodes, uuids, "sushy", "pw")
	assert.Equal(t, 0, m3.WorkerCount)
	assert.Equal(t, "pw", m3.BMCPass)

	rs := buildRHWASpec(in, nodes, uuids, "sushy", "pw")
	assert.Equal(t, "192.168.126.1", rs.NetGateway)
	assert.Equal(t, "sushy", rs.SushyUser)
}
