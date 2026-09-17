package odf

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testSpec() Spec {
	return Spec{
		ClusterName:  "rhwa-lab",
		BaseDomain:   "migration.redhat.com",
		NodeName:     "rhwa-lab-ceph-0",
		Host:         "ceph-0",
		IP:           "192.168.126.10",
		MAC:          "52:54:00:6a:03:00",
		LibvirtNet:   "rhwa",
		NetGateway:   "192.168.126.1",
		SSHPubKey:    "ssh-ed25519 AAAA...EPHEMERAL... ocpctl",
		VCPU:         4,
		RAMGB:        16,
		RootDiskGB:   40,
		OSDCount:     3,
		OSDDiskGB:    236,
		Release:      "squid",
		Image:        "quay.io/ceph/ceph:v19",
		PoolReplica:  3,
		PoolUsableGB: 200,
	}
}

func TestRenderSeed(t *testing.T) {
	ud, err := renderUserData(testSpec())
	require.NoError(t, err)
	assert.Contains(t, ud, "ssh-ed25519 AAAA...EPHEMERAL... ocpctl") // ephemeral key authorized
	assert.Contains(t, ud, "hostname: ceph-0")

	nc, err := renderNetworkConfig(testSpec())
	require.NoError(t, err)
	assert.Contains(t, nc, `macaddress: "52:54:00:6a:03:00"`) // matched by MAC
	assert.Contains(t, nc, "- 192.168.126.10/24")             // static IP
	assert.Contains(t, nc, "via: 192.168.126.1")
}

func TestRenderDefine(t *testing.T) {
	out, err := renderDefine(testSpec())
	require.NoError(t, err)
	// 3 OSD data disks (vdb..vdd) created + attached, plus the root and seed.
	for _, dev := range []string{"vdb", "vdc", "vdd"} {
		assert.Contains(t, out, "rhwa-lab-ceph-0-"+dev+".qcow2")
	}
	assert.NotContains(t, out, "vde") // only 3 OSDs
	assert.Contains(t, out, "--memory 16384")
	assert.Contains(t, out, "--vcpus 4")
	assert.Contains(t, out, "--os-variant centos-stream9")
	assert.Contains(t, out, "network=rhwa,mac='52:54:00:6a:03:00'")
	assert.Contains(t, out, "genisoimage")
	assert.Contains(t, out, "/tmp/rhwa-lab-ceph-0-seed/user-data")
}

func TestRenderBootstrap(t *testing.T) {
	out, err := renderBootstrap(testSpec())
	require.NoError(t, err)
	assert.Contains(t, out, "centos-release-ceph-squid")
	assert.Contains(t, out, "--image 'quay.io/ceph/ceph:v19' bootstrap")
	assert.Contains(t, out, "--mon-ip '192.168.126.10'")
	assert.Contains(t, out, "--single-host-defaults")
	assert.Contains(t, out, "-ge 3 ]]") // waits for 3 OSDs up
	assert.Contains(t, out, "ceph osd pool set 'ocs-storagepool' size 3")
	assert.Contains(t, out, "mgr module enable prometheus")
}

func TestRenderBootstrap_ImageDerivedFromRelease(t *testing.T) {
	// Image unset but a known release still pins the matching container image.
	s := testSpec()
	s.Image = ""
	s.Release = "reef"
	out, err := renderBootstrap(s)
	require.NoError(t, err)
	assert.Contains(t, out, "--image 'quay.io/ceph/ceph:v18' bootstrap")
}

func TestRenderBootstrap_NoImageForUnknownRelease(t *testing.T) {
	// An unknown release yields no --image (cephadm picks its default).
	s := testSpec()
	s.Image = ""
	s.Release = "dev-unknown"
	out, err := renderBootstrap(s)
	require.NoError(t, err)
	assert.Contains(t, out, `"$CEPHADM" bootstrap`)
	assert.NotContains(t, out, "--image")
}

func TestRenderOperatorAndStorageCluster(t *testing.T) {
	op, err := renderOperator(testSpec(), "stable-4.22")
	require.NoError(t, err)
	assert.Contains(t, op, "name: odf-operator")
	assert.Contains(t, op, "channel: stable-4.22") // resolved channel injected
	assert.Contains(t, op, "namespace: openshift-storage")
	assert.Contains(t, op, "apply -f - <<'YAML'")

	sc, err := renderStorageCluster(testSpec())
	require.NoError(t, err)
	assert.Contains(t, sc, "kind: StorageCluster")
	assert.Contains(t, sc, "name: ocs-external-storagecluster")
	assert.Contains(t, sc, "enable: true")
	assert.Equal(t, 1, strings.Count(sc, "externalStorage:"))
}
